//go:build windows

package console

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// ATTACH_PARENT_PROCESS.
const attachParentProcess = ^uintptr(0)

// x/sys/windows does not wrap AttachConsole.
var procAttachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

// attach borrows the console of the launching shell.
//
// -H=windowsgui stops a console window appearing when the app is started
// from Explorer, at the cost of the process having no console at all, so
// -version, -list-monitors, -add-calendar and the setup guidance would
// otherwise be written into the void.
//
// The shell does not wait for a GUI binary, so output arrives after the
// prompt has already returned.
func attach() {
	ok, _, err := procAttachConsole.Call(attachParentProcess)

	// ERROR_ACCESS_DENIED means we already have a console, as a build
	// without -H=windowsgui does; reopening below is harmless there.
	if ok == 0 && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return
	}

	out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return
	}
	os.Stdout = out
	os.Stderr = out

	// Reassigning the Go variables alone would leave spawned processes
	// pointed at the original handles.
	_ = windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, windows.Handle(out.Fd()))
	_ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(out.Fd()))
}
