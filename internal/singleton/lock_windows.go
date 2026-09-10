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
// deletes the file when a process is killed, so one crash would leave a
// lock nobody owns and every later launch would refuse to start with no
// way for the user to see why.
//
// A named mutex is a kernel object rather than a file. Windows closes the
// handle when the process dies, however it dies, and destroys the object
// with the last handle - the same guarantee flock gives on Unix.
func acquire(path string) (func(), error) {
	name, err := windows.UTF16PtrFromString(mutexName(path))
	if err != nil {
		return nil, fmt.Errorf("lock name for %s: %w", path, err)
	}

	// CreateMutex opens the existing object instead of failing when one is
	// already there, so this branch owns a handle it has to close.
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

// mutexName turns the lock path into a legal kernel object name. Only the
// namespace separator may be a backslash, so the rest of the path is
// flattened. Local scopes the object to the login session, which keeps two
// users on one machine down to one copy each rather than one between them.
func mutexName(path string) string {
	flat := strings.Map(func(r rune) rune {
		if r == '\\' || r == '/' || r == ':' {
			return '_'
		}
		return r
	}, filepath.Clean(path))
	return `Local\` + flat
}
