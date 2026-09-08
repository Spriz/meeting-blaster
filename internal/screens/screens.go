// Package screens enumerates monitors and pins full-screen windows to them.
//
// Fyne has no monitor or window-positioning API at all: a window lands
// wherever the window manager puts it, and CenterOnScreen only centres on the
// monitor it already occupies. The alert has to be able to target a specific
// display, so this package goes underneath Fyne to the platform.
//
// Only Linux/X11 is implemented. Other platforms report ErrUnsupported and
// callers fall back to letting the window manager choose, which still yields
// a working single-monitor alert.
package screens

import "errors"

// ErrUnsupported reports that monitor control is not implemented for this
// platform or display server.
var ErrUnsupported = errors.New("monitor control not supported on this platform")

// Monitor is one physical display in the desktop layout.
type Monitor struct {
	Name string

	// Index is the position in the server's monitor list, which is what
	// _NET_WM_FULLSCREEN_MONITORS expects.
	Index int

	X, Y          int
	Width, Height int
	Primary       bool
}

// List returns the connected monitors, ordered by the display server's own
// indexing.
func List() ([]Monitor, error) { return list() }

// Primary returns the primary monitor, falling back to the first one when no
// monitor is flagged primary.
func Primary() (Monitor, error) {
	monitors, err := List()
	if err != nil {
		return Monitor{}, err
	}
	if len(monitors) == 0 {
		return Monitor{}, errors.New("no monitors found")
	}
	for _, m := range monitors {
		if m.Primary {
			return m, nil
		}
	}
	return monitors[0], nil
}

// PinFullscreen moves an already-fullscreen window onto the given monitor.
// The window is identified by its X11 id, which Find returns.
func PinFullscreen(windowID uint32, m Monitor) error { return pinFullscreen(windowID, m) }

// Find returns the ids of mapped windows whose title matches exactly. The
// overlay uses it to locate the window Fyne just created, so it can be pinned
// to a monitor.
func Find(title string) ([]uint32, error) { return find(title) }
