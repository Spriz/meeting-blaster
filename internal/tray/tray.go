package tray

import (
	_ "embed"
	"fmt"
	"sync"
	"time"

	"fyne.io/systray"

	"github.com/spriz/meeting-blaster/internal/calendar"
	"github.com/spriz/meeting-blaster/internal/config"
	"github.com/spriz/meeting-blaster/internal/engine"
)

//go:embed icon.png
var iconPNG []byte

// agendaSlots caps how many of today's meetings appear in the menu. systray
// cannot remove items once added, so a fixed pool is allocated up front and
// individual entries are shown or hidden as the day progresses.
const agendaSlots = 12

// Actions are the callbacks the menu invokes. All are optional.
type Actions struct {
	OnJoin        func(calendar.Event)
	OnRefresh     func()
	OnPreferences func()
	OnQuit        func()
}

// Tray owns the system tray item and its menu.
type Tray struct {
	actions Actions

	mu sync.Mutex
	// The menu items do not exist until Ready runs on systray's own
	// goroutine, while Update is called from the engine's. Publishing
	// them under the mutex behind a ready flag is what makes that safe:
	// a local .ics feed polls in microseconds, so the first Update
	// genuinely can arrive first.
	ready  bool
	status *systray.MenuItem
	join   *systray.MenuItem
	agenda []*systray.MenuItem

	// events maps each agenda slot to the event it currently shows, so a
	// click resolves to the right meeting even as the list shifts.
	events  map[int]calendar.Event
	nextEv  *calendar.Event
	lastSet string
}

// New returns a Tray. Call Ready from systray's onReady callback.
func New(actions Actions) *Tray {
	return &Tray{actions: actions, events: make(map[int]calendar.Event)}
}

// Ready builds the menu. It must run inside systray's onReady callback.
func (t *Tray) Ready() {
	systray.SetIcon(iconPNG)
	setLabel("", "meeting-blaster: starting…")

	status := systray.AddMenuItem("Loading…", "")
	status.Disable()

	join := systray.AddMenuItem("Join next meeting", "Open the next meeting's link")
	join.Hide()
	go t.watch(join, func() {
		t.mu.Lock()
		ev := t.nextEv
		t.mu.Unlock()
		if ev != nil && t.actions.OnJoin != nil {
			t.actions.OnJoin(*ev)
		}
	})

	systray.AddSeparator()

	agenda := make([]*systray.MenuItem, agendaSlots)
	for i := range agenda {
		item := systray.AddMenuItem("", "")
		item.Hide()
		agenda[i] = item

		slot := i
		go t.watch(item, func() {
			t.mu.Lock()
			ev, ok := t.events[slot]
			t.mu.Unlock()
			if ok && t.actions.OnJoin != nil {
				t.actions.OnJoin(ev)
			}
		})
	}

	systray.AddSeparator()

	refresh := systray.AddMenuItem("Refresh now", "Refetch the calendar")
	go t.watch(refresh, func() {
		if t.actions.OnRefresh != nil {
			t.actions.OnRefresh()
		}
	})

	prefs := systray.AddMenuItem("Preferences…", "Choose calendars and formats")
	go t.watch(prefs, func() {
		if t.actions.OnPreferences != nil {
			t.actions.OnPreferences()
		}
	})

	quit := systray.AddMenuItem("Quit", "Exit meeting-blaster")
	go t.watch(quit, func() {
		if t.actions.OnQuit != nil {
			t.actions.OnQuit()
		}
		systray.Quit()
	})

	t.mu.Lock()
	t.status, t.join, t.agenda = status, join, agenda
	t.ready = true
	t.mu.Unlock()
}

// watch invokes fn each time the menu item is clicked, until its channel
// closes at shutdown.
func (t *Tray) watch(item *systray.MenuItem, fn func()) {
	for range item.ClickedCh {
		fn()
	}
}

// Update redraws the label and menu from engine state. It is called about
// once a second, so it avoids redundant work when nothing has changed.
func (t *Tray) Update(state engine.State, cfg config.Config, now time.Time) {
	t.mu.Lock()
	ready := t.ready
	if ready {
		t.nextEv = state.Next
	}
	t.mu.Unlock()

	// Before Ready there is no tray item and no menu to draw into. The
	// engine re-emits state every second, so the countdown catches up on
	// the next tick rather than being lost.
	if !ready {
		return
	}

	label := Label(state, cfg, now)
	setLabel(label, Tooltip(state, cfg, now))

	t.mu.Lock()
	changed := t.lastSet != agendaKey(state)
	t.lastSet = agendaKey(state)
	t.mu.Unlock()

	// The status line carries the countdown, so it updates every tick.
	if state.Err != nil {
		t.status.SetTitle("Calendar unavailable — will retry")
	} else if state.Next == nil {
		t.status.SetTitle("Nothing scheduled")
	} else {
		t.status.SetTitle(label)
	}

	if state.Next != nil && state.Next.HasLink() {
		t.join.SetTitle("Join " + Truncate(state.Next.Title, cfg.TitleMaxLen))
		t.join.Show()
	} else {
		t.join.Hide()
	}

	if !changed {
		return
	}
	t.rebuildAgenda(state, cfg, now)
}

// rebuildAgenda repoints the fixed item pool at today's remaining meetings.
func (t *Tray) rebuildAgenda(state engine.State, cfg config.Config, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	shown := 0
	for _, ev := range state.Events {
		if shown >= agendaSlots {
			break
		}
		// Meetings that finished are noise; keep the menu about what is
		// still ahead.
		if ev.End.Before(now) {
			continue
		}

		item := t.agenda[shown]
		item.SetTitle(fmt.Sprintf("%s  %s",
			ev.Start.Format(cfg.TimeLayout()),
			Truncate(ev.Title, 40)))

		if ev.HasLink() {
			item.SetTooltip("Click to join " + ev.Title)
			item.Enable()
		} else {
			item.SetTooltip(ev.Location)
			item.Disable()
		}
		item.Show()

		t.events[shown] = ev
		shown++
	}

	for i := shown; i < len(t.agenda); i++ {
		t.agenda[i].Hide()
		delete(t.events, i)
	}
}

// agendaKey summarises the agenda so Update can skip rebuilding when only
// the countdown moved.
func agendaKey(state engine.State) string {
	key := make([]byte, 0, 64)
	for _, ev := range state.Events {
		key = append(key, ev.ID...)
		key = append(key, byte('|'))
	}
	return string(key)
}
