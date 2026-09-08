// Package tray renders the system tray item: the label, the menu, and the
// wiring from menu clicks back to the app.
package tray

import (
	"fmt"
	"strings"
	"time"

	"github.com/spriz/meeting-blaster/internal/config"
	"github.com/spriz/meeting-blaster/internal/engine"
)

// Label renders the tray text, e.g. "Standup in 12m". Space in the top bar
// is scarce, so this stays terse and never wraps.
//
// On Windows the tray has no text at all; see SetLabel for what happens
// there instead.
func Label(state engine.State, cfg config.Config, now time.Time) string {
	if state.Next == nil {
		if state.Err != nil {
			return "Calendar unavailable"
		}
		return "No meetings"
	}

	ev := *state.Next
	title := Truncate(ev.Title, cfg.TitleMaxLen)

	if ev.InProgress(now) {
		left := ev.End.Sub(now)
		return fmt.Sprintf("%s · %s left", title, ShortDuration(left))
	}
	return fmt.Sprintf("%s in %s", title, ShortDuration(ev.Start.Sub(now)))
}

// Tooltip is the hover text, which has room for the full title and times.
// It is the primary readout on Windows.
func Tooltip(state engine.State, cfg config.Config, now time.Time) string {
	if state.Next == nil {
		if state.Err != nil {
			return "meeting-blaster: calendar unavailable"
		}
		return "meeting-blaster: nothing scheduled"
	}
	ev := *state.Next
	return fmt.Sprintf("%s\n%s – %s",
		ev.Title,
		ev.Start.Format(cfg.TimeLayout()),
		ev.End.Format(cfg.TimeLayout()))
}

// ShortDuration renders a duration the way a person would say it: "45s",
// "12m", "1h30m". Anything past a day is not worth a countdown.
func ShortDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < 0 {
		d = 0
	}

	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		hours := int(d / time.Hour)
		mins := int((d % time.Hour) / time.Minute)
		if mins == 0 {
			return fmt.Sprintf("%dh", hours)
		}
		return fmt.Sprintf("%dh%dm", hours, mins)
	default:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
}

// Truncate shortens s to max runes, appending an ellipsis. It counts runes
// rather than bytes so non-ASCII titles are not cut mid-character.
func Truncate(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= max {
		return string(runes)
	}
	if max == 1 {
		return "…"
	}
	return strings.TrimRight(string(runes[:max-1]), " ") + "…"
}
