//go:build windows

package notify

import (
	"fmt"
	"os/exec"
	"strings"
)

func send(title, body string) error {
	// Uses the built-in Windows Runtime toast APIs via PowerShell, which
	// avoids shipping a native dependency for an ancillary feature.
	script := fmt.Sprintf(`
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null
$xml = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent(
    [Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$texts = $xml.GetElementsByTagName("text")
$texts.Item(0).AppendChild($xml.CreateTextNode(%s)) > $null
$texts.Item(1).AppendChild($xml.CreateTextNode(%s)) > $null
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("meeting-blaster").Show($toast)
`, quote(title), quote(body))

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("powershell toast: %w", err)
	}
	return nil
}

// quote renders a Go string as a PowerShell single-quoted literal.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
