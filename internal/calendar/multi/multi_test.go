package multi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/spriz/meeting-blaster/internal/calendar"
	"github.com/spriz/meeting-blaster/internal/config"
)

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// stub is a calendar.Provider with fixed answers. No stub provider exists
// in the repo, so this is the only one.
type stub struct {
	name   string
	cals   []calendar.Calendar
	events []calendar.Event
	err    error
}

func (s stub) Name() string { return s.name }

func (s stub) Calendars(context.Context) ([]calendar.Calendar, error) {
	return s.cals, s.err
}

func (s stub) Events(context.Context, time.Time, time.Time) ([]calendar.Event, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.events, nil
}

func at(hour int) time.Time {
	return time.Date(2026, 6, 10, hour, 0, 0, 0, time.UTC)
}

func TestNamespacesAndMergesSorted(t *testing.T) {
	work := stub{
		name: "iCalendar subscriptions",
		cals: []calendar.Calendar{{ID: "work-feed", Name: "Work", Primary: true}},
		// Deliberately out of order, and out of order relative to the
		// second ICS subscription too.
		events: []calendar.Event{
			{ID: "work-late", CalendarID: "work-feed", Start: at(14)},
			{ID: "work-early", CalendarID: "work-feed", Start: at(9)},
		},
	}
	team := stub{
		name:   "iCalendar subscriptions",
		cals:   []calendar.Calendar{{ID: "team-feed", Name: "Team"}},
		events: []calendar.Event{{ID: "team-midday", CalendarID: "team-feed", Start: at(11)}},
	}

	p := New(quiet(),
		Source{Key: config.SourceICS, Provider: work},
		Source{Key: config.SourceICS, Provider: team},
	)

	cals, err := p.Calendars(context.Background())
	if err != nil {
		t.Fatalf("Calendars: %v", err)
	}
	wantIDs := []string{"ics:work-feed", "ics:team-feed"}
	if len(cals) != len(wantIDs) {
		t.Fatalf("got %d calendars, want %d", len(cals), len(wantIDs))
	}
	for n, want := range wantIDs {
		if cals[n].ID != want {
			t.Errorf("calendar %d ID = %q, want %q", n, cals[n].ID, want)
		}
	}
	// With two sources the provider name disambiguates identical titles.
	if cals[0].Name != "Work  (iCalendar subscriptions)" {
		t.Errorf("calendar 0 name = %q, want the provider suffix", cals[0].Name)
	}

	events, err := p.Events(context.Background(), at(0), at(23))
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	wantOrder := []struct{ id, calendarID string }{
		{"work-early", "ics:work-feed"},
		{"team-midday", "ics:team-feed"},
		{"work-late", "ics:work-feed"},
	}
	if len(events) != len(wantOrder) {
		t.Fatalf("got %d events, want %d", len(events), len(wantOrder))
	}
	for n, want := range wantOrder {
		if events[n].ID != want.id {
			t.Errorf("event %d ID = %q, want %q (merged slice must be sorted by start)", n, events[n].ID, want.id)
		}
		if events[n].CalendarID != want.calendarID {
			t.Errorf("event %d calendar = %q, want %q", n, events[n].CalendarID, want.calendarID)
		}
	}
}

func TestFailingChildDoesNotSuppressTheOther(t *testing.T) {
	boom := errors.New("feed unavailable")
	good := stub{
		name:   "iCalendar subscriptions",
		events: []calendar.Event{{ID: "i1", CalendarID: "feed", Start: at(11)}},
	}

	p := New(quiet(),
		Source{Key: config.SourceICS, Provider: stub{name: "iCalendar subscriptions", err: boom}},
		Source{Key: config.SourceICS, Provider: good},
	)

	// A failing feed must not blank out working subscriptions.
	events, err := p.Events(context.Background(), at(0), at(23))
	if err != nil {
		t.Fatalf("Events with one failing child returned error: %v", err)
	}
	if len(events) != 1 || events[0].CalendarID != "ics:feed" {
		t.Fatalf("got %+v, want the surviving child's single event", events)
	}

	all := New(quiet(),
		Source{Key: config.SourceICS, Provider: stub{name: "iCalendar subscriptions", err: boom}},
		Source{Key: config.SourceICS, Provider: stub{name: "iCalendar subscriptions", err: boom}},
	)
	if _, err := all.Events(context.Background(), at(0), at(23)); err == nil {
		t.Error("Events with every child failing returned nil error")
	}
}

func TestNameJoinsChildren(t *testing.T) {
	p := New(quiet(),
		Source{Key: config.SourceICS, Provider: stub{name: "iCalendar subscriptions"}},
		Source{Key: config.SourceICS, Provider: stub{name: "iCalendar subscriptions"}},
	)
	if got, want := p.Name(), "iCalendar subscriptions, iCalendar subscriptions"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}
