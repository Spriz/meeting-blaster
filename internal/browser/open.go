// Package browser opens URLs in the user's default web browser.
package browser

import (
	"fmt"
	"os/exec"
	"strings"
)

// Open launches url in the default browser. If override is non-empty it is
// used as the executable instead, letting users pin meetings to a specific
// browser or profile.
func Open(url, override string) error {
	if url == "" {
		return fmt.Errorf("no URL to open")
	}

	name, args := command(url)
	if override != "" {
		fields := strings.Fields(override)
		name, args = fields[0], append(fields[1:], url)
	}

	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open %s with %s: %w", url, name, err)
	}
	// Reap the child so it does not linger as a zombie. The browser
	// detaches itself, so this returns almost immediately.
	go func() { _ = cmd.Wait() }()
	return nil
}
