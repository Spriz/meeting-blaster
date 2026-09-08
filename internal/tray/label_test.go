package tray

import (
	"errors"
	"testing"
	"time"

	"github.com/spriz/meeting-blaster/internal/calendar"
	"github.com/spriz/meeting-blaster/internal/config"
	"github.com/spriz/meeting-blaster/internal/engine"
)

var now = time.Date(2026, 9, 8, 9, 0, 0, 0, time.UTC)

func TestShortDuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m"},
		{12 * time.Minute, "12m"},
		{time.Hour, "1h"},
		{90 * time.Minute, "1h30m"},
		{26 * time.Hour, "1d"},
		{-5 * time.Second, "0s"},
	}
	for _, tc := range tests {
		if got := ShortDuration(tc.in); got != tc.want {
			t.Errorf("ShortDuration(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in   string
		max  int
		want string
	}{
		{"Standup", 30, "Standup"},
		{"Quarterly planning review session", 20, "Quarterly planning…"},
		{"Ünicode títle that is quite long", 10, "Ünicode t…"},
		{"anything", 0, "anything"},
	}
	for _, tc := range tests {
		if got := Truncate(tc.in, tc.max); got != tc.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
		if tc.max > 0 && len([]rune(Truncate(tc.in, tc.max))) > tc.max {
			t.Errorf("Truncate(%q, %d) exceeded max", tc.in, tc.max)
		}
	}
}

func TestLabel(t *testing.T) {
	cfg := config.Default()

	upcoming := calendar.Event{Title: "Standup", Start: now.Add(12 * time.Minute), End: now.Add(27 * time.Minute)}
	running := calendar.Event{Title: "Design review", Start: now.Add(-10 * time.Minute), End: now.Add(20 * time.Minute)}

	tests := []struct {
		name  string
		state engine.State
		want  string
	}{
		{"upcoming", engine.State{Next: &upcoming}, "Standup in 12m"},
		{"in progress shows time left", engine.State{Next: &running}, "Design review · 20m left"},
		{"clear day", engine.State{}, "No meetings"},
		{"error surfaces", engine.State{Err: errors.New("boom")}, "Calendar unavailable"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Label(tc.state, cfg, now); got != tc.want {
				t.Errorf("Label() = %q, want %q", got, tc.want)
			}
		})
	}
}
