//go:build linux || darwin

package singleton

import (
	"fmt"
	"os"
	"sync"
	"syscall"
)

func acquire(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", path, err)
	}

	// LOCK_EX|LOCK_NB fails immediately rather than waiting, and the kernel
	// drops the lock when this process exits however it exits.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, ErrAlreadyRunning{Path: path}
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			_ = f.Close()
		})
	}, nil
}
