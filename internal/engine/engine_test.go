package engine

import (
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

func ids(events []calendar.Event) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.ID
	}
	return out
}
