package tray

import (
	"strings"
	"testing"
	"time"

	"github.com/spriz/meeting-blaster/internal/calendar"
	"github.com/spriz/meeting-blaster/internal/config"
	"github.com/spriz/meeting-blaster/internal/engine"
)

// TestUpdateBeforeReadyIsIgnored guards the startup ordering: Ready runs on
// systray's goroutine while the engine polls on its own, and a local .ics
// feed returns events in microseconds, so the first Update really can land
// before the menu items exist. Dereferencing them then crashed the app
// before it ever showed a tray icon.
func TestUpdateBeforeReadyIsIgnored(t *testing.T) {
	tr := New(Actions{})

	ev := calendar.Event{
		Title:      "ICS smoke test",
		Start:      now.Add(90 * time.Second),
		End:        now.Add(30 * time.Minute),
		MeetingURL: "https://meet.google.com/abc-defg-hij",
	}
	state := engine.State{Events: []calendar.Event{ev}, Next: &ev, UpdatedAt: now}

	// Must not panic, and must not record state it cannot draw.
	tr.Update(state, config.Default(), now)

	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.nextEv != nil {
		t.Error("nextEv was set before Ready; a Join click would act on an unshown menu")
	}
	if tr.lastSet != "" {
		t.Error("agenda key was recorded before Ready, so the first real Update would skip the rebuild")
	}
}

// TestAgendaShowsOnlyToday guards the menu: the engine fetches through the
// end of tomorrow so a late-night session still alerts for the morning
// standup, but tomorrow's meetings do not belong in today's agenda.
func TestAgendaShowsOnlyToday(t *testing.T) {
	at := func(day, hour int) time.Time { return time.Date(2026, 9, day, hour, 0, 0, 0, time.UTC) }
	mk := func(title string, start, end time.Time) calendar.Event {
		return calendar.Event{ID: title, Title: title, Start: start, End: end}
	}
	events := []calendar.Event{
		mk("overnight", at(7, 22), at(8, 10)),
		mk("finished", at(8, 7), at(8, 8)),
		mk("now", at(8, 9), at(8, 10)),
		mk("tonight", at(8, 23), at(9, 0)),
		mk("tomorrow", at(9, 7), at(9, 8)),
	}

	var got []string
	for _, ev := range todaysAgenda(events, now) {
		got = append(got, ev.Title)
	}
	want := []string{"overnight", "now", "tonight"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("agenda = %v, want %v", got, want)
	}
}

// TestAgendaKeyChangesAtMidnight makes sure the menu is rebuilt when the day
// rolls over even if the fetched events are unchanged; otherwise the
// filtered agenda would keep showing yesterday's view.
func TestAgendaKeyChangesAtMidnight(t *testing.T) {
	state := engine.State{Events: []calendar.Event{{ID: "a", CalendarID: "ics:a"}}}
	if agendaKey(state, now) == agendaKey(state, now.AddDate(0, 0, 1)) {
		t.Error("agenda key is the same on consecutive days")
	}
}

// TestAgendaRowFormatsAllDayWithoutTime: an all-day event has no meaningful
// start time, and "00:00" next to a holiday reads like a midnight meeting.
func TestAgendaRowFormatsAllDayWithoutTime(t *testing.T) {
	cfg := config.Default()
	meeting := calendar.Event{Title: "Standup", Start: now}
	if got, want := agendaTitle(meeting, cfg), now.Format(cfg.TimeLayout())+"  Standup"; got != want {
		t.Errorf("timed row = %q, want %q", got, want)
	}
	holiday := calendar.Event{Title: "Constitution Day", Start: now, AllDay: true}
	if got := agendaTitle(holiday, cfg); got != "All day  Constitution Day" {
		t.Errorf("all-day row = %q, want no time", got)
	}
}

func TestAgendaListsAllDayFirst(t *testing.T) {
	midnight := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	holiday := calendar.Event{ID: "holiday", AllDay: true, Start: midnight, End: midnight.AddDate(0, 0, 1)}
	tomorrow := calendar.Event{ID: "tomorrow", AllDay: true, Start: midnight.AddDate(0, 0, 1), End: midnight.AddDate(0, 0, 2)}
	meeting := calendar.Event{ID: "meeting", Start: now, End: now.Add(time.Hour)}
	state := engine.State{Events: []calendar.Event{meeting}, AllDay: []calendar.Event{holiday, tomorrow}}

	var got []string
	for _, ev := range agendaEvents(state, now) {
		got = append(got, ev.ID)
	}
	if strings.Join(got, ",") != "holiday,meeting" {
		t.Errorf("agenda = %v, want [holiday meeting]", got)
	}
	if agendaKey(state, now) == agendaKey(engine.State{Events: state.Events}, now) {
		t.Error("agenda key ignores all-day events, so adding one would not redraw the menu")
	}
}

// TestAgendaKeyTracksEditedEvents guards the redraw skip: editing an event
// in the calendar keeps its ID, so a key of IDs alone left a newly added
// join link greyed out until something else in the agenda changed.
func TestAgendaKeyTracksEditedEvents(t *testing.T) {
	before := calendar.Event{ID: "a", CalendarID: "ics:a", Title: "hi there"}
	edits := map[string]func(*calendar.Event){
		"link":  func(ev *calendar.Event) { ev.MeetingURL = "https://meet.google.com/abc-defg-hij" },
		"title": func(ev *calendar.Event) { ev.Title = "hi again" },
		"end":   func(ev *calendar.Event) { ev.End = now.Add(time.Hour) },
	}
	for name, edit := range edits {
		after := before
		edit(&after)
		if agendaKey(engine.State{AllDay: []calendar.Event{before}}, now) ==
			agendaKey(engine.State{AllDay: []calendar.Event{after}}, now) {
			t.Errorf("editing the %s did not change the agenda key", name)
		}
	}
}
