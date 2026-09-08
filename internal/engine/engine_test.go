package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/spriz/meeting-blaster/internal/calendar"
	"github.com/spriz/meeting-blaster/internal/config"
)

var base = time.Date(2026, 9, 8, 9, 0, 0, 0, time.UTC)

func ev(id string, startOffset, dur time.Duration) calendar.Event {
	return calendar.Event{
		ID:    id,
		Title: id,
		Start: base.Add(startOffset),
		End:   base.Add(startOffset + dur),
	}
}

func TestNextMeeting(t *testing.T) {
	standup := ev("standup", 30*time.Minute, 15*time.Minute)
	review := ev("review", 2*time.Hour, time.Hour)

	t.Run("picks soonest upcoming", func(t *testing.T) {
		got := NextMeeting([]calendar.Event{standup, review}, base)
		if got == nil || got.ID != "standup" {
			t.Fatalf("got %v, want standup", got)
		}
	})

	t.Run("in-progress beats upcoming", func(t *testing.T) {
		running := ev("running", -10*time.Minute, 30*time.Minute)
		got := NextMeeting([]calendar.Event{running, standup}, base)
		if got == nil || got.ID != "running" {
			t.Fatalf("got %v, want the in-progress meeting", got)
		}
	})

	t.Run("nil when all are past", func(t *testing.T) {
		past := ev("past", -3*time.Hour, time.Hour)
		if got := NextMeeting([]calendar.Event{past}, base); got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})

	t.Run("nil when empty", func(t *testing.T) {
		if got := NextMeeting(nil, base); got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})
}

func TestDue(t *testing.T) {
	lead := 5 * time.Minute
	meeting := ev("m", 5*time.Minute, time.Hour)

	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"well before the window", base.Add(-10 * time.Minute), false},
		{"exactly at the lead boundary", base, true},
		{"inside the window", base.Add(4 * time.Minute), true},
		{"one second before start", base.Add(5*time.Minute - time.Second), true},
		{"exactly at start does not alert", base.Add(5 * time.Minute), false},
		{"after start does not alert", base.Add(10 * time.Minute), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Due(meeting, tc.now, lead); got != tc.want {
				t.Errorf("Due(now=%v) = %v, want %v", tc.now.Sub(base), got, tc.want)
			}
		})
	}
}

func TestFilter(t *testing.T) {
	cfg := config.Default()
	cfg.CalendarIDs = []string{"work"}

	events := []calendar.Event{
		{ID: "a", CalendarID: "work", Title: "keep"},
		{ID: "b", CalendarID: "personal", Title: "wrong calendar"},
		{ID: "c", CalendarID: "work", Title: "all day", AllDay: true},
		{ID: "d", CalendarID: "work", Title: "declined", Declined: true},
	}

	got := Filter(events, cfg)
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("got %d events (%v), want just \"a\"", len(got), ids(got))
	}

	t.Run("empty selection watches everything", func(t *testing.T) {
		all := config.Default()
		all.HideDeclined = false
		got := Filter(events, all)
		if len(got) != 3 {
			t.Fatalf("got %v, want 3 non-all-day events", ids(got))
		}
	})
}

func TestFilterDeduplicatesSharedOccurrences(t *testing.T) {
	cfg := config.Default()
	cfg.HideDeclined = false
	loc := time.FixedZone("UTC+2", 2*60*60)
	rid := time.Date(2026, 9, 8, 10, 0, 0, 0, loc)
	events := []calendar.Event{
		{ID: "declined", CalendarID: "acal", UID: "shared", RecurrenceID: rid, Start: base.Add(2 * time.Hour), End: base.Add(3 * time.Hour), Declined: true, MeetingURL: "https://meet.example/shared"},
		{ID: "accepted-no-link", CalendarID: "bcal", UID: "shared", RecurrenceID: rid.UTC(), Start: base.Add(90 * time.Minute), End: base.Add(150 * time.Minute)},
		{ID: "accepted-link", CalendarID: "zcal", UID: "shared", RecurrenceID: rid, Start: base.Add(90 * time.Minute), End: base.Add(150 * time.Minute), MeetingURL: "https://meet.example/shared"},
		{ID: "other", CalendarID: "other", UID: "other-uid", Title: "shared", Start: base.Add(90 * time.Minute), End: base.Add(2*time.Hour + 30*time.Minute)},
		{ID: "same", CalendarID: "zcal", UID: "stable", Start: base.Add(4 * time.Hour), End: base.Add(5 * time.Hour)},
		{ID: "first", CalendarID: "acal", UID: "stable", Start: base.Add(4 * time.Hour), End: base.Add(5 * time.Hour)},
		{ID: "unknown-1", CalendarID: "one", Start: base.Add(6 * time.Hour), End: base.Add(7 * time.Hour)},
		{ID: "unknown-2", CalendarID: "two", Start: base.Add(6 * time.Hour), End: base.Add(7 * time.Hour)},
	}

	got := Filter(events, cfg)
	gotIDs := ids(got)
	if len(got) != 5 || gotIDs[0] != "accepted-link" || gotIDs[1] != "other" || gotIDs[2] != "first" || gotIDs[3] != "unknown-1" || gotIDs[4] != "unknown-2" {
		t.Fatalf("got %v, want shared representative and all distinct unknown events", gotIDs)
	}
	if got[0].MeetingURL != "https://meet.example/shared" || got[0].Declined || got[0].ID != "accepted-link" {
		t.Fatalf("shared representative = %+v, want nondeclined copy with link", got[0])
	}
}

func TestFilterDeduplicatesAfterSelection(t *testing.T) {
	cfg := config.Default()
	cfg.CalendarIDs = []string{"selected"}
	events := []calendar.Event{
		{ID: "ignored", CalendarID: "ignored", UID: "shared", Start: base, End: base.Add(time.Hour), MeetingURL: "https://meet.example/ignored"},
		{ID: "all-day", CalendarID: "selected", UID: "shared", AllDay: true},
		{ID: "declined", CalendarID: "selected", UID: "shared", Declined: true, Start: base, End: base.Add(time.Hour)},
		{ID: "selected", CalendarID: "selected", UID: "shared", Start: base.Add(time.Hour), End: base.Add(2 * time.Hour)},
	}
	got := Filter(events, cfg)
	if len(got) != 1 || got[0].ID != "selected" {
		t.Fatalf("got %v, want selected copy only", ids(got))
	}
}

type sequenceProvider struct {
	events [][]calendar.Event
	calls  int
}

func (p *sequenceProvider) Name() string                                           { return "test" }
func (p *sequenceProvider) Calendars(context.Context) ([]calendar.Calendar, error) { return nil, nil }
func (p *sequenceProvider) Events(context.Context, time.Time, time.Time) ([]calendar.Event, error) {
	index := p.calls
	if index >= len(p.events) {
		index = len(p.events) - 1
	}
	p.calls++
	return p.events[index], nil
}

func TestSharedCopyAlertsOnceAcrossPolls(t *testing.T) {
	cfg := config.Default()
	cfg.AlertLead = config.Duration(5 * time.Minute)
	cfg.NotifyLead = config.Duration(5 * time.Minute)
	start := base.Add(5 * time.Minute)
	copyA := calendar.Event{ID: "source-a", CalendarID: "calendar-a", UID: "shared", Start: start, End: start.Add(time.Hour)}
	copyB := copyA
	copyB.ID = "source-b"
	copyB.CalendarID = "calendar-b"
	copyB.MeetingURL = "https://meet.example/shared"

	var alerts, notifies []calendar.Event
	e := New(&sequenceProvider{events: [][]calendar.Event{{copyA}, {copyB}}}, cfg, Callbacks{
		OnAlert:  func(ev calendar.Event) { alerts = append(alerts, ev) },
		OnNotify: func(ev calendar.Event) { notifies = append(notifies, ev) },
	}, nil)
	e.now = func() time.Time { return base }
	e.poll(context.Background())
	e.poll(context.Background())

	if len(alerts) != 1 || alerts[0].ID != copyA.ID || len(notifies) != 1 || notifies[0].ID != copyA.ID {
		t.Fatalf("alerts = %v, notifies = %v, want one of each for the first available copy", alerts, notifies)
	}
}

func TestSharedOccurrenceRescheduleAlertsAgain(t *testing.T) {
	cfg := config.Default()
	cfg.AlertLead = config.Duration(10 * time.Minute)
	cfg.NotifyLead = 0
	first := calendar.Event{ID: "first", CalendarID: "calendar", UID: "shared", Start: base.Add(5 * time.Minute), End: base.Add(time.Hour)}
	rescheduled := first
	rescheduled.ID = "replacement"
	rescheduled.Start = base.Add(6 * time.Minute)
	rescheduled.End = base.Add(2 * time.Hour)

	var alerts []calendar.Event
	e := New(&sequenceProvider{events: [][]calendar.Event{{first}, {rescheduled}}}, cfg, Callbacks{
		OnAlert: func(ev calendar.Event) { alerts = append(alerts, ev) },
	}, nil)
	e.now = func() time.Time { return base }
	e.poll(context.Background())
	e.poll(context.Background())

	if len(alerts) != 2 || alerts[0].ID != first.ID || alerts[1].ID != rescheduled.ID {
		t.Fatalf("alerts = %v, want one alert per start time", alerts)
	}
}

func TestFallbackAlertIdentityIncludesCalendar(t *testing.T) {
	cfg := config.Default()
	cfg.AlertLead = config.Duration(5 * time.Minute)
	cfg.NotifyLead = 0
	first := calendar.Event{ID: "same-id", CalendarID: "calendar-a", Start: base.Add(5 * time.Minute), End: base.Add(time.Hour)}
	second := first
	second.CalendarID = "calendar-b"

	var alerts []calendar.Event
	e := New(nil, cfg, Callbacks{
		OnAlert: func(ev calendar.Event) { alerts = append(alerts, ev) },
	}, nil)
	e.state.Events = []calendar.Event{first, second}
	e.now = func() time.Time { return base }
	e.tick()

	if len(alerts) != 2 {
		t.Fatalf("alerts = %v, want both independent fallback events", alerts)
	}
}

func TestAlertFiresExactlyOnce(t *testing.T) {
	cfg := config.Default()
	cfg.AlertLead = config.Duration(5 * time.Minute)
	cfg.NotifyLead = 0

	var alerts []string
	e := New(nil, cfg, Callbacks{
		OnAlert: func(ev calendar.Event) { alerts = append(alerts, ev.ID) },
	}, nil)

	meeting := ev("standup", 5*time.Minute, 15*time.Minute)
	e.state.Events = []calendar.Event{meeting}

	// Tick every second across the whole alert window; the overlay must
	// appear once, not 300 times.
	for i := 0; i <= 300; i++ {
		at := base.Add(time.Duration(i) * time.Second)
		e.now = func() time.Time { return at }
		e.tick()
	}

	if len(alerts) != 1 {
		t.Fatalf("fired %d alerts %v, want exactly 1", len(alerts), alerts)
	}
}

type eventsThenErrorProvider struct {
	events []calendar.Event
	calls  int
}

func (p *eventsThenErrorProvider) Name() string { return "test" }
func (p *eventsThenErrorProvider) Calendars(context.Context) ([]calendar.Calendar, error) {
	return nil, nil
}
func (p *eventsThenErrorProvider) Events(context.Context, time.Time, time.Time) ([]calendar.Event, error) {
	p.calls++
	if p.calls == 1 {
		return p.events, nil
	}
	return nil, errors.New("calendar unavailable")
}

type blockingProvider struct {
	events  []calendar.Event
	started chan struct{}
	release chan struct{}
}

func (p *blockingProvider) Name() string { return "test" }
func (p *blockingProvider) Calendars(context.Context) ([]calendar.Calendar, error) {
	return nil, nil
}
func (p *blockingProvider) Events(ctx context.Context, _ time.Time, _ time.Time) ([]calendar.Event, error) {
	close(p.started)
	select {
	case <-p.release:
		return p.events, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestSetConfigFiltersCachedEventsAfterFailedRefresh(t *testing.T) {
	cfg := config.Default()
	cfg.AlertLead = config.Duration(5 * time.Minute)
	cfg.NotifyLead = 0
	work := ev("work", 10*time.Minute, time.Hour)
	work.CalendarID = "work"
	personal := ev("personal", 6*time.Minute, time.Hour)
	personal.CalendarID = "personal"

	var states []State
	var alerts []string
	e := New(&eventsThenErrorProvider{events: []calendar.Event{work, personal}}, cfg, Callbacks{
		OnState: func(state State) { states = append(states, state) },
		OnAlert: func(event calendar.Event) { alerts = append(alerts, event.ID) },
	}, nil)
	e.now = func() time.Time { return base }
	e.poll(context.Background())
	states = nil

	onlyWork := cfg
	onlyWork.CalendarIDs = []string{"work"}
	e.SetConfig(onlyWork)

	if len(states) != 1 {
		t.Fatalf("SetConfig published %d states, want 1", len(states))
	}
	if got := ids(states[0].Events); len(got) != 1 || got[0] != "work" {
		t.Fatalf("published events = %v, want [work]", got)
	}
	if states[0].Next == nil || states[0].Next.ID != "work" {
		t.Fatalf("published next = %v, want work", states[0].Next)
	}

	e.poll(context.Background())
	state := e.Snapshot()
	if state.Err == nil {
		t.Fatal("failed refresh did not report an error")
	}
	if got := ids(state.Events); len(got) != 1 || got[0] != "work" {
		t.Fatalf("events after failed refresh = %v, want [work]", got)
	}
	if state.Next == nil || state.Next.ID != "work" {
		t.Fatalf("next after failed refresh = %v, want work", state.Next)
	}

	e.now = func() time.Time { return base.Add(5 * time.Minute) }
	e.tick()
	if len(alerts) != 1 || alerts[0] != "work" {
		t.Fatalf("alerts after selection change = %v, want [work]", alerts)
	}
}

func TestPollUsesLatestConfigAfterSelectionChangesDuringFetch(t *testing.T) {
	cfg := config.Default()
	cfg.AlertLead = config.Duration(5 * time.Minute)
	cfg.NotifyLead = 0
	work := ev("work", 5*time.Minute, time.Hour)
	work.CalendarID = "work"
	personal := ev("personal", 5*time.Minute, time.Hour)
	personal.CalendarID = "personal"
	provider := &blockingProvider{
		events:  []calendar.Event{work, personal},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	defer func() {
		select {
		case <-provider.release:
		default:
			close(provider.release)
		}
	}()

	var alerts []string
	e := New(provider, cfg, Callbacks{
		OnAlert: func(event calendar.Event) { alerts = append(alerts, event.ID) },
	}, nil)
	e.now = func() time.Time { return base }
	done := make(chan struct{})
	go func() {
		e.poll(context.Background())
		close(done)
	}()

	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("poll did not begin its fetch")
	}

	onlyWork := cfg
	onlyWork.CalendarIDs = []string{"work"}
	e.SetConfig(onlyWork)
	close(provider.release)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("poll did not finish after fetch was released")
	}

	state := e.Snapshot()
	if got := ids(state.Events); len(got) != 1 || got[0] != "work" {
		t.Fatalf("events after in-flight poll = %v, want [work]", got)
	}
	if len(alerts) != 1 || alerts[0] != "work" {
		t.Fatalf("alerts after in-flight poll = %v, want [work]", alerts)
	}
}
func TestStatePublicationCoalescesToLatestState(t *testing.T) {
	all := config.Default()
	work := ev("work", 10*time.Minute, time.Hour)
	work.CalendarID = "work"
	personal := ev("personal", 5*time.Minute, time.Hour)
	personal.CalendarID = "personal"

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var releaseOnce sync.Once
	defer func() { releaseOnce.Do(func() { close(releaseFirst) }) }()

	var mu sync.Mutex
	var published []State
	active := 0
	overlapped := false
	calls := 0
	e := New(nil, all, Callbacks{
		OnState: func(state State) {
			mu.Lock()
			active++
			if active > 1 {
				overlapped = true
			}
			calls++
			call := calls
			mu.Unlock()

			if call == 1 {
				close(firstStarted)
				<-releaseFirst
			}

			mu.Lock()
			published = append(published, state)
			active--
			mu.Unlock()
		},
	}, nil)
	e.now = func() time.Time { return base }
	e.state.Events = []calendar.Event{personal, work}
	e.state.Next = NextMeeting(e.state.Events, base)

	firstDone := make(chan struct{})
	go func() {
		e.SetConfig(all)
		close(firstDone)
	}()
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first state publication did not start")
	}

	onlyWork := all
	onlyWork.CalendarIDs = []string{"work"}
	onlyWorkDone := make(chan struct{})
	go func() {
		e.SetConfig(onlyWork)
		close(onlyWorkDone)
	}()
	select {
	case <-onlyWorkDone:
	case <-time.After(time.Second):
		t.Fatal("SetConfig blocked behind an active state callback")
	}

	none := all
	none.CalendarSelectionExplicit = true
	noneDone := make(chan struct{})
	go func() {
		e.SetConfig(none)
		close(noneDone)
	}()
	select {
	case <-noneDone:
	case <-time.After(time.Second):
		t.Fatal("second SetConfig blocked behind an active state callback")
	}

	releaseOnce.Do(func() { close(releaseFirst) })
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("initial SetConfig did not finish after publication was released")
	}

	mu.Lock()
	defer mu.Unlock()
	if overlapped {
		t.Error("OnState callbacks overlapped")
	}
	if len(published) != 2 {
		t.Fatalf("published %d states, want initial and latest only", len(published))
	}
	if len(published[len(published)-1].Events) != 0 {
		t.Fatalf("last published events = %v, want no hidden events", ids(published[len(published)-1].Events))
	}
}

func TestStatePublicationAllowsReentrantSetConfig(t *testing.T) {
	all := config.Default()
	work := ev("work", 10*time.Minute, time.Hour)
	work.CalendarID = "work"

	var e *Engine
	calls := 0
	active := 0
	overlapped := false
	e = New(nil, all, Callbacks{
		OnState: func(State) {
			active++
			if active > 1 {
				overlapped = true
			}
			calls++
			if calls == 1 {
				none := all
				none.CalendarSelectionExplicit = true
				e.SetConfig(none)
			}
			active--
		},
	}, nil)
	e.now = func() time.Time { return base }
	e.state.Events = []calendar.Event{work}
	e.state.Next = NextMeeting(e.state.Events, base)

	done := make(chan struct{})
	go func() {
		e.SetConfig(all)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reentrant SetConfig deadlocked state publication")
	}

	if overlapped {
		t.Error("reentrant SetConfig invoked OnState concurrently")
	}
	if calls != 2 {
		t.Errorf("OnState calls = %d, want 2", calls)
	}
	if state := e.Snapshot(); len(state.Events) != 0 {
		t.Fatalf("state after reentrant selection = %v, want no events", ids(state.Events))
	}
}
func ids(events []calendar.Event) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.ID
	}
	return out
}
