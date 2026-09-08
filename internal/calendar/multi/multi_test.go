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
	g := stub{
		name: "Google Calendar",
		cals: []calendar.Calendar{{ID: "me@example.com", Name: "Work", Primary: true}},
		// Deliberately out of order, and out of order relative to the
		// ICS source too.
		events: []calendar.Event{
			{ID: "g2", CalendarID: "me@example.com", Start: at(14)},
			{ID: "g1", CalendarID: "me@example.com", Start: at(9)},
		},
	}
	i := stub{
		name:   "iCalendar subscriptions",
		cals:   []calendar.Calendar{{ID: "9f2a1c0b", Name: "Team"}},
		events: []calendar.Event{{ID: "i1", CalendarID: "9f2a1c0b", Start: at(11)}},
	}

	p := New(quiet(),
		Source{Key: config.SourceGoogle, Provider: g},
		Source{Key: config.SourceICS, Provider: i},
	)

	cals, err := p.Calendars(context.Background())
	if err != nil {
		t.Fatalf("Calendars: %v", err)
	}
	wantIDs := []string{"google:me@example.com", "ics:9f2a1c0b"}
	if len(cals) != len(wantIDs) {
		t.Fatalf("got %d calendars, want %d", len(cals), len(wantIDs))
	}
	for n, want := range wantIDs {
		if cals[n].ID != want {
			t.Errorf("calendar %d ID = %q, want %q", n, cals[n].ID, want)
		}
	}
	// With two sources the provider name disambiguates identical titles.
	if cals[0].Name != "Work  (Google Calendar)" {
		t.Errorf("calendar 0 name = %q, want the provider suffix", cals[0].Name)
	}

	events, err := p.Events(context.Background(), at(0), at(23))
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	wantOrder := []struct{ id, calendarID string }{
		{"g1", "google:me@example.com"},
		{"i1", "ics:9f2a1c0b"},
		{"g2", "google:me@example.com"},
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
	boom := errors.New("token needs re-auth")
	good := stub{
		name:   "iCalendar subscriptions",
		events: []calendar.Event{{ID: "i1", CalendarID: "feed", Start: at(11)}},
	}

	p := New(quiet(),
		Source{Key: config.SourceGoogle, Provider: stub{name: "Google Calendar", err: boom}},
		Source{Key: config.SourceICS, Provider: good},
	)

	// engine.poll discards everything on error, so a stale Google token
	// must not blank out working subscriptions.
	events, err := p.Events(context.Background(), at(0), at(23))
	if err != nil {
		t.Fatalf("Events with one failing child returned error: %v", err)
	}
	if len(events) != 1 || events[0].CalendarID != "ics:feed" {
		t.Fatalf("got %+v, want the surviving child's single event", events)
	}

	all := New(quiet(),
		Source{Key: config.SourceGoogle, Provider: stub{name: "Google Calendar", err: boom}},
		Source{Key: config.SourceICS, Provider: stub{name: "iCalendar subscriptions", err: boom}},
	)
	if _, err := all.Events(context.Background(), at(0), at(23)); err == nil {
		t.Error("Events with every child failing returned nil error")
	}
}

func TestNameJoinsChildren(t *testing.T) {
	p := New(quiet(),
		Source{Key: config.SourceGoogle, Provider: stub{name: "Google Calendar"}},
		Source{Key: config.SourceICS, Provider: stub{name: "iCalendar subscriptions"}},
	)
	if got, want := p.Name(), "Google Calendar, iCalendar subscriptions"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}
