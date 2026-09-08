//go:build !linux

package screens

// Monitor control is X11-specific for now. On macOS and Windows the alert
// still works; it simply appears wherever the window manager places it.

func list() ([]Monitor, error) { return nil, ErrUnsupported }

func pinFullscreen(windowID uint32, m Monitor) error { return ErrUnsupported }

func find(title string) ([]uint32, error) { return nil, ErrUnsupported }
