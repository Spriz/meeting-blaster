//go:build linux

package notify

import (
	"fmt"
	"os/exec"
)

func send(title, body string) error {
	// -a sets the app name so the notification is attributed correctly in
	// GNOME's message tray.
	cmd := exec.Command("notify-send", "-a", "meeting-blaster", "-u", "normal", title, body)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("notify-send: %w", err)
	}
	return nil
}
