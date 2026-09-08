package tray

import (
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
