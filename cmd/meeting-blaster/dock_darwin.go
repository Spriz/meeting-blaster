//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/go-gl/glfw/v3.4/glfw"
)

// respectBundleActivationPolicy stops the app claiming a Dock icon when
// it runs from MeetingBlaster.app.
//
// The bundle's Info.plist sets LSUIElement, but GLFW overrides it:
// cocoa_init.m forces NSApplicationActivationPolicyRegular whenever its
// menubar hint is set, which it is by default, "in case we are unbundled".
//
// The unbundled binary keeps GLFW's behaviour: there is no plist to fall
// back on, and an app with no activation policy cannot focus its windows.
//
// Must run before the first window, which is when Fyne initialises GLFW.
func respectBundleActivationPolicy() {
	exe, err := os.Executable()
	if err != nil || !insideAppBundle(exe) {
		return
	}
	glfw.InitHint(glfw.CocoaMenubar, glfw.False)
}

// insideAppBundle reports whether exe is the executable of a .app, the
// only case with an Info.plist to defer to.
func insideAppBundle(exe string) bool {
	dir, file := filepath.Split(exe)
	if file == "" {
		return false
	}
	return strings.HasSuffix(filepath.Clean(dir), ".app/Contents/MacOS")
}
