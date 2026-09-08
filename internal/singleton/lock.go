// Package singleton keeps one copy of the app running at a time.
//
// There are three ways to start meeting-blaster - the applications menu, an
// autostart entry, and the systemd user service - and nothing stops someone
// using two of them. A second copy means two tray icons and, worse, two
// full-screen alerts fighting over the screen.
package singleton

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spriz/meeting-blaster/internal/config"
)

// ErrAlreadyRunning reports that another copy holds the lock.
type ErrAlreadyRunning struct{ Path string }

func (e ErrAlreadyRunning) Error() string {
	return fmt.Sprintf("another copy of meeting-blaster is already running (lock: %s)", e.Path)
}

// Acquire takes the single-instance lock. The returned release function
// frees it; it is safe to call more than once.
//
// The lock lives in the runtime directory, which the system clears on
// reboot, and the underlying lock is released automatically if the process
// dies, so a crash cannot leave a stale lock behind.
func Acquire() (release func(), err error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, config.AppName+".lock")
	return acquire(path)
}
