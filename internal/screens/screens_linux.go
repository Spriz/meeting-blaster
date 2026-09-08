//go:build linux

package screens

import (
	"fmt"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/randr"
	"github.com/jezek/xgb/xproto"
)

// conn is shared: opening an X connection per call would be wasteful when the
// countdown re-checks placement.
var (
	connOnce sync.Once
	connErr  error
	xconn    *xgb.Conn
	xroot    xproto.Window
)

func connect() (*xgb.Conn, xproto.Window, error) {
	connOnce.Do(func() {
		c, err := xgb.NewConn()
		if err != nil {
			connErr = fmt.Errorf("connect to X display: %w", err)
			return
		}
		if err := randr.Init(c); err != nil {
			c.Close()
			connErr = fmt.Errorf("initialise RandR extension: %w", err)
			return
		}
		xconn = c
		xroot = xproto.Setup(c).DefaultScreen(c).Root
	})
	return xconn, xroot, connErr
}

func list() ([]Monitor, error) {
	conn, root, err := connect()
	if err != nil {
		return nil, err
	}

	// GetMonitors reflects the RandR monitor list, which is what the window
	// manager indexes for _NET_WM_FULLSCREEN_MONITORS.
	reply, err := randr.GetMonitors(conn, root, true).Reply()
	if err != nil {
		return nil, fmt.Errorf("query monitors: %w", err)
	}

	monitors := make([]Monitor, 0, len(reply.Monitors))
	for i, m := range reply.Monitors {
		name, err := xproto.GetAtomName(conn, m.Name).Reply()
		label := fmt.Sprintf("monitor-%d", i)
		if err == nil && name != nil {
			label = name.Name
		}
		monitors = append(monitors, Monitor{
			Name:    label,
			Index:   i,
			X:       int(m.X),
			Y:       int(m.Y),
			Width:   int(m.Width),
			Height:  int(m.Height),
			Primary: m.Primary,
		})
	}
	return monitors, nil
}

// pinFullscreen asks the window manager to place an existing fullscreen
// window on one monitor, using the EWMH message designed for exactly this.
// Setting geometry directly does not work: a compositing WM owns the geometry
// of fullscreen windows and would simply put it back.
func pinFullscreen(windowID uint32, m Monitor) error {
	conn, root, err := connect()
	if err != nil {
		return err
	}

	atom, err := internAtom(conn, "_NET_WM_FULLSCREEN_MONITORS")
	if err != nil {
		return err
	}

	edge := uint32(m.Index)
	msg := xproto.ClientMessageEvent{
		Format: 32,
		Window: xproto.Window(windowID),
		Type:   atom,
		// top, bottom, left, right monitor indices, then the source
		// indication: 1 means a normal application.
		Data: xproto.ClientMessageDataUnionData32New([]uint32{edge, edge, edge, edge, 1}),
	}

	err = xproto.SendEventChecked(conn, false, root,
		uint32(xproto.EventMaskSubstructureNotify|xproto.EventMaskSubstructureRedirect),
		string(msg.Bytes())).Check()
	if err != nil {
		return fmt.Errorf("pin window to %s: %w", m.Name, err)
	}
	return nil
}

// find walks the window tree for mapped windows whose title matches exactly.
// Reparenting window managers wrap client windows in frames, so the search
// has to recurse rather than only check the root's direct children.
func find(title string) ([]uint32, error) {
	conn, root, err := connect()
	if err != nil {
		return nil, err
	}

	netWMName, err := internAtom(conn, "_NET_WM_NAME")
	if err != nil {
		return nil, err
	}

	var found []uint32
	var walk func(xproto.Window, int)
	walk = func(win xproto.Window, depth int) {
		if depth > 8 {
			return
		}
		if windowTitle(conn, win, netWMName) == title && isViewable(conn, win) {
			found = append(found, uint32(win))
		}
		tree, err := xproto.QueryTree(conn, win).Reply()
		if err != nil || tree == nil {
			return
		}
		for _, child := range tree.Children {
			walk(child, depth+1)
		}
	}
	walk(root, 0)

	return found, nil
}

// windowTitle reads _NET_WM_NAME, falling back to the legacy WM_NAME.
func windowTitle(conn *xgb.Conn, win xproto.Window, netWMName xproto.Atom) string {
	if reply, err := xproto.GetProperty(conn, false, win, netWMName,
		xproto.GetPropertyTypeAny, 0, 1024).Reply(); err == nil && reply != nil && len(reply.Value) > 0 {
		return string(reply.Value)
	}
	if reply, err := xproto.GetProperty(conn, false, win, xproto.AtomWmName,
		xproto.GetPropertyTypeAny, 0, 1024).Reply(); err == nil && reply != nil && len(reply.Value) > 0 {
		return string(reply.Value)
	}
	return ""
}

func isViewable(conn *xgb.Conn, win xproto.Window) bool {
	attrs, err := xproto.GetWindowAttributes(conn, win).Reply()
	return err == nil && attrs != nil && attrs.MapState == xproto.MapStateViewable
}

func internAtom(conn *xgb.Conn, name string) (xproto.Atom, error) {
	reply, err := xproto.InternAtom(conn, true, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, fmt.Errorf("intern atom %s: %w", name, err)
	}
	return reply.Atom, nil
}
