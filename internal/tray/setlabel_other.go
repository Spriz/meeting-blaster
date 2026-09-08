//go:build !windows

package tray

import "fyne.io/systray"

// setLabel writes the countdown into the menu bar itself. macOS
// (NSStatusItem) and Linux (StatusNotifierItem) both render this text.
func setLabel(label, tooltip string) {
	systray.SetTitle(label)
	systray.SetTooltip(tooltip)
}
