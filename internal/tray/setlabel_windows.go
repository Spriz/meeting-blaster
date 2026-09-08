//go:build windows

package tray

import "fyne.io/systray"

// setLabel falls back to the tooltip on Windows, whose notification area
// shows an icon only - there is no text label to set. The countdown is
// therefore visible on hover rather than at a glance, and the full-screen
// overlay carries more of the load.
func setLabel(label, tooltip string) {
	if label != "" {
		tooltip = label + "\n" + tooltip
	}
	systray.SetTooltip(tooltip)
}
