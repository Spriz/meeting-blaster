//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/go-gl/glfw/v3.4/glfw"
)

// respectBundleActivationPolicy stops the app claiming a Dock icon and a
// menu bar when it is running from MeetingBlaster.app.
//
// The bundle's Info.plist sets LSUIElement, which is how a menu-bar app
// says it has no place in the Dock. GLFW overrides that: cocoa_init.m
// forces NSApplicationActivationPolicyRegular whenever its menubar hint is
// set, which it is by default, "in case we are unbundled". Clearing the
// hint leaves the plist in charge.
//
// The unbundled binary - what the tarball and the mise install give you -
// keeps GLFW's behaviour, because there is no plist to fall back on and an
// app with no activation policy at all cannot focus its own windows.
//
// Must be called before the first window is created, which is when Fyne
// initialises GLFW.
func respectBundleActivationPolicy() {
	exe, err := os.Executable()
	if err != nil || !insideAppBundle(exe) {
		return
	}
	glfw.InitHint(glfw.CocoaMenubar, glfw.False)
}

// insideAppBundle reports whether exe is the executable of a .app, which
// is the only case where there is an Info.plist to defer to.
func insideAppBundle(exe string) bool {
	dir, file := filepath.Split(exe)
	if file == "" {
		return false
	}
	return strings.HasSuffix(filepath.Clean(dir), ".app/Contents/MacOS")
}
