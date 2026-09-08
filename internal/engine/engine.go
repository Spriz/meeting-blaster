// Package engine is the scheduling core: it polls the calendar, tracks which
// meeting is next, and decides when to alert.
//
// It owns no UI. OnState callbacks are serialized, but SetConfig can invoke
// them synchronously on its caller's goroutine. Callers must marshal UI work
// onto their own UI thread.
package engine

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/spriz/meeting-blaster/internal/calendar"
	"github.com/spriz/meeting-blaster/internal/config"
)

// State is an immutable snapshot of what the engine currently knows.
type State struct {
	// Events are today's watched events, ordered by start time.
	Events []calendar.Event

	// Next is the meeting the countdown refers to: the one in progress,
	// or else the soonest upcoming. Nil when the day is clear.
	Next *calendar.Event

	// Err is the last polling error, if the most recent poll failed.
	// Events still holds the last good data.
	Err error

	// UpdatedAt is when Events was last refreshed successfully.
	UpdatedAt time.Time
}

// Callbacks receive engine output. Any may be nil.
type Callbacks struct {
	// OnState publishes the latest state after ticks, polls, and configuration
	// changes. Calls are serialized and overlapping updates coalesce; SetConfig
	// may invoke it synchronously. It drives the tray.
	OnState func(State)

	// OnAlert fires once per event, AlertLead before it starts. This
	// drives the full-screen overlay.
	OnAlert func(calendar.Event)

	// OnNotify fires once per event, NotifyLead before it starts.
	OnNotify func(calendar.Event)
}

// Engine polls a provider and drives callbacks.
type Engine struct {
	provider calendar.Provider
	cb       Callbacks
	log      *slog.Logger

	// now is swappable in tests.
	now func() time.Time

	mu    sync.RWMutex
	cfg   config.Config
	state State

	// publishing and pending serialize OnState callbacks without holding mu
	// during a callback.
	publishing bool
	pending    bool

	// fired records which alerts have already been delivered, keyed by
	// event identity, so an alert shows exactly once even though the tick
	// loop re-evaluates every second.
	fired map[key]bool

	refresh chan struct{}
}

type occurrenceIdentity struct {
	uid          string
	recurrenceID time.Time
	calendarID   string
	eventID      string
}

type key struct {
	identity occurrenceIdentity
	start    time.Time
	kind     string
}

func occurrenceOf(ev calendar.Event) occurrenceIdentity {
	if ev.UID != "" {
		return occurrenceIdentity{uid: ev.UID, recurrenceID: ev.RecurrenceID.UTC()}
	}
	return occurrenceIdentity{calendarID: ev.CalendarID, eventID: ev.ID}
}

func preferEvent(candidate, current calendar.Event) bool {
	if candidate.Declined != current.Declined {
		return !candidate.Declined
	}
	if candidate.HasLink() != current.HasLink() {
		return candidate.HasLink()
	}
	if candidate.CalendarID != current.CalendarID {
		return candidate.CalendarID < current.CalendarID
	}
	return candidate.ID < current.ID
}

// New builds an Engine. cfg may be replaced later with SetConfig.
func New(provider calendar.Provider, cfg config.Config, cb Callbacks, log *slog.Logger) *Engine {
	if log == nil {
		log = slog.Default()
	}
	return &Engine{
		provider: provider,
		cb:       cb,
		log:      log,
		now:      time.Now,
		cfg:      cfg,
		fired:    make(map[key]bool),
		refresh:  make(chan struct{}, 1),
	}
}

// Snapshot returns the current state.
func (e *Engine) Snapshot() State {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.state
}

// Config returns the active settings.
func (e *Engine) Config() config.Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg
}

// SetConfig replaces the settings, filters cached events to the new selection,
// and triggers an immediate refetch.
func (e *Engine) SetConfig(cfg config.Config) {
	e.mu.Lock()
	e.cfg = cfg
	e.state.Events = Filter(e.state.Events, cfg)
	e.state.Next = NextMeeting(e.state.Events, e.now())
	e.mu.Unlock()

	e.emit()
	e.Refresh()
}

// Refresh requests an immediate poll. It never blocks.
func (e *Engine) Refresh() {
	select {
	case e.refresh <- struct{}{}:
	default: // a refresh is already pending
	}
}

// Run drives the engine until ctx is cancelled. It blocks.
func (e *Engine) Run(ctx context.Context) {
	e.poll(ctx)

	// Two cadences: a slow one that hits the network, and a fast one that
	// only recomputes the countdown and alert decisions from cached data.
	pollTicker := time.NewTicker(e.Config().PollInterval.Std())
	defer pollTicker.Stop()

	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-pollTicker.C:
			e.poll(ctx)
		case <-e.refresh:
			e.poll(ctx)
		case <-tick.C:
			e.tick()
		}
	}
}

// poll fetches events and republishes state.
func (e *Engine) poll(ctx context.Context) {
	now := e.now()
	from, to := dayWindow(now)

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	events, err := e.provider.Events(ctx, from, to)
	if err != nil {
		e.log.Warn("calendar poll failed", "error", err)
		e.mu.Lock()
		e.state.Err = err
		e.mu.Unlock()
		e.emit()
		return
	}

	e.mu.Lock()
	events = Filter(events, e.cfg)
	e.state = State{
		Events:    events,
		Next:      NextMeeting(events, e.now()),
		Err:       nil,
		UpdatedAt: e.now(),
	}
	e.mu.Unlock()

	e.log.Debug("calendar polled", "events", len(events))
	e.emit()
	e.tick()
}

// tick recomputes the next meeting and fires any due alerts.
func (e *Engine) tick() {
	now := e.now()

	e.mu.Lock()
	cfg := e.cfg
	e.state.Next = NextMeeting(e.state.Events, now)
	events := e.state.Events

	var alerts, notifies []calendar.Event
	for _, ev := range events {
		if Due(ev, now, cfg.AlertLead.Std()) {
			k := key{identity: occurrenceOf(ev), start: ev.Start.UTC(), kind: "alert"}
			if !e.fired[k] {
				e.fired[k] = true
				alerts = append(alerts, ev)
			}
		}
		if cfg.NotifyLead.Std() > 0 && Due(ev, now, cfg.NotifyLead.Std()) {
			k := key{identity: occurrenceOf(ev), start: ev.Start.UTC(), kind: "notify"}
			if !e.fired[k] {
				e.fired[k] = true
				notifies = append(notifies, ev)
			}
		}
	}
	e.pruneFired(now)
	e.mu.Unlock()

	e.emit()

	for _, ev := range notifies {
		if e.cb.OnNotify != nil {
			e.cb.OnNotify(ev)
		}
	}
	for _, ev := range alerts {
		if e.cb.OnAlert != nil {
			e.cb.OnAlert(ev)
		}
	}
}

func (e *Engine) emit() {
	callback := e.cb.OnState
	if callback == nil {
		return
	}

	e.mu.Lock()
	if e.publishing {
		e.pending = true
		e.mu.Unlock()
		return
	}
	e.publishing = true
	state := e.state
	e.mu.Unlock()

	for {
		callback(state)

		e.mu.Lock()
		if !e.pending {
			e.publishing = false
			e.mu.Unlock()
			return
		}
		e.pending = false
		state = e.state
		e.mu.Unlock()
	}
}

// pruneFired drops bookkeeping for events that ended well in the past, so
// the map does not grow without bound in a long-running session. Callers
// must hold e.mu.
func (e *Engine) pruneFired(now time.Time) {
	for k := range e.fired {
		if now.Sub(k.start) > 12*time.Hour {
			delete(e.fired, k)
		}
	}
}

// Filter applies selection and declined-event preferences, drops all-day
// events, then deduplicates reliable shared occurrences.
func Filter(events []calendar.Event, cfg config.Config) []calendar.Event {
	out := make([]calendar.Event, 0, len(events))
	var representatives map[occurrenceIdentity]int
	for _, ev := range events {
		if ev.AllDay || !cfg.Watches(ev.CalendarID) || (cfg.HideDeclined && ev.Declined) {
			continue
		}
		if ev.UID == "" {
			out = append(out, ev)
			continue
		}
		if representatives == nil {
			representatives = make(map[occurrenceIdentity]int, len(events))
		}
		identity := occurrenceOf(ev)
		if index, ok := representatives[identity]; ok {
			if preferEvent(ev, out[index]) {
				out[index] = ev
			}
			continue
		}
		representatives[identity] = len(out)
		out = append(out, ev)
	}
	calendar.SortByStart(out)
	return out
}

// NextMeeting picks the meeting the UI should count down to: one in progress
// wins over one that has not started, because that is the one you are late
// for. Returns nil when nothing is current or upcoming.
func NextMeeting(events []calendar.Event, now time.Time) *calendar.Event {
	for i := range events {
		if events[i].InProgress(now) {
			return &events[i]
		}
	}
	for i := range events {
		if events[i].Upcoming(now) {
			return &events[i]
		}
	}
	return nil
}

// Due reports whether an event has entered its alert window: within lead of
// starting, but not yet started. Alerting on an already-started meeting
// would fire for every event still on screen at launch.
func Due(ev calendar.Event, now time.Time, lead time.Duration) bool {
	if !now.Before(ev.Start) {
		return false
	}
	return ev.Start.Sub(now).Round(time.Second) <= lead
}

// dayWindow is the fetch range: from a little in the past, so a meeting that
// started before launch is still known, through the end of tomorrow, so a
// late-night session still sees the morning standup.
func dayWindow(now time.Time) (from, to time.Time) {
	from = now.Add(-4 * time.Hour)
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	to = end.AddDate(0, 0, 2)
	return from, to
}
