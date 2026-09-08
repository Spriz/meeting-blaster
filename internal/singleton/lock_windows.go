//go:build windows

package singleton

import (
	"fmt"
	"os"
	"sync"
)

// Windows has no flock. Opening the file for exclusive access gives the
// same guarantee: the second process cannot open it while the first holds
// it, and the handle is closed when the process dies.
func acquire(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil, ErrAlreadyRunning{Path: path}
		}
		return nil, fmt.Errorf("open lock file %s: %w", path, err)
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			_ = f.Close()
			_ = os.Remove(path)
		})
	}, nil
}
