//go:build windows

package singleton

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
)

// Windows has no flock, and a lock file is the wrong substitute: nothing
// deletes it when a process is killed, so one crash leaves a lock nobody
// owns and the app refuses to start for good. A named mutex is a kernel
// object, destroyed with its last handle, which process death closes.
func acquire(path string) (func(), error) {
	name, err := windows.UTF16PtrFromString(mutexName(path))
	if err != nil {
		return nil, fmt.Errorf("lock name for %s: %w", path, err)
	}

	// CreateMutex opens the existing object rather than failing, so this
	// branch owns a handle it has to close.
	handle, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(handle)
		return nil, ErrAlreadyRunning{Path: path}
	}
	if err != nil {
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}

	var once sync.Once
	return func() {
		once.Do(func() { _ = windows.CloseHandle(handle) })
	}, nil
}

// mutexName flattens the lock path into a legal object name: only the
// namespace separator may be a backslash. Local scopes the mutex to the
// login session, so two users on one machine get a copy each.
func mutexName(path string) string {
	flat := strings.Map(func(r rune) rune {
		if r == '\\' || r == '/' || r == ':' {
			return '_'
		}
		return r
	}, filepath.Clean(path))
	return `Local\` + flat
}
