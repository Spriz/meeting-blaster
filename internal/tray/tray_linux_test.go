//go:build linux

package tray

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/spriz/meeting-blaster/internal/calendar"
	"github.com/spriz/meeting-blaster/internal/config"
	"github.com/spriz/meeting-blaster/internal/engine"
)

func TestAgendaRefreshesSameIDCalendarCopy(t *testing.T) {
	// systray owns process-global menu state and Ready starts click watchers.
	// Isolate both from other tests. On Linux the menu can be built without
	// registering on D-Bus, so this needs neither a display nor a session bus.
	const child = "MEETING_BLASTER_TEST_TRAY_COPY"
	if os.Getenv(child) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAgendaRefreshesSameIDCalendarCopy$")
		cmd.Env = append(os.Environ(), child+"=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tray copy-switch regression: %v\n%s", err, out)
		}
		return
	}

	joined := make(chan calendar.Event, 1)
	tr := New(Actions{OnJoin: func(ev calendar.Event) { joined <- ev }})
	tr.Ready()
	cfg := config.Default()
	original := calendar.Event{
		ID:         "shared@example.com/20260908T090500Z",
		UID:        "shared@example.com",
		CalendarID: "ics:a",
		Title:      "Shared meeting",
		Start:      now.Add(5 * time.Minute),
		End:        now.Add(time.Hour),
	}
	tr.Update(engine.State{Events: []calendar.Event{original}, Next: &original}, cfg, now)
	if !tr.agenda[0].Disabled() {
		t.Fatal("meeting without a join link should have a disabled agenda row")
	}

	linked := original
	linked.CalendarID = "ics:b"
	linked.MeetingURL = "https://meet.google.com/abc-defg-hij"
	events := engine.Filter([]calendar.Event{original, linked}, cfg)
	tr.Update(engine.State{Events: events, Next: &events[0]}, cfg, now)
	if tr.agenda[0].Disabled() {
		t.Fatal("same-ID copy with a join link left the agenda row disabled")
	}

	select {
	case tr.agenda[0].ClickedCh <- struct{}{}:
	case <-time.After(5 * time.Second):
		t.Fatal("agenda click was not received")
	}
	select {
	case ev := <-joined:
		if ev.CalendarID != linked.CalendarID || ev.MeetingURL != linked.MeetingURL {
			t.Fatalf("agenda joined stale calendar copy: %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("agenda click did not invoke OnJoin")
	}
}
