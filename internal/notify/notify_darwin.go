//go:build darwin

package notify

import (
	"fmt"
	"os/exec"
	"strings"
)

func send(title, body string) error {
	script := fmt.Sprintf("display notification %s with title %s", quote(body), quote(title))
	if err := exec.Command("osascript", "-e", script).Run(); err != nil {
		return fmt.Errorf("osascript notification: %w", err)
	}
	return nil
}

// quote renders a Go string as an AppleScript string literal.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
