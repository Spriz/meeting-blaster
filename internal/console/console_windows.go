//go:build windows

package console

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// attachParentProcess is ATTACH_PARENT_PROCESS, the (DWORD)-1 that asks for
// the console of whatever started this process.
const attachParentProcess = ^uintptr(0)

// x/sys/windows does not wrap AttachConsole.
var procAttachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

// attach borrows the console of the launching shell.
//
// -H=windowsgui keeps a console window from appearing when the app is
// started from Explorer, which is right for something that lives in the
// tray. The cost is that the process starts with no console at all, so
// -version, -list-monitors, -add-calendar and the setup guidance shown
// when no calendar is configured would otherwise be written into the void.
//
// Because the app is a GUI binary, the shell does not wait for it: output
// arrives after the prompt has already returned.
func attach() {
	ok, _, err := procAttachConsole.Call(attachParentProcess)

	// ERROR_ACCESS_DENIED means this process already has a console, which
	// is the case for a binary built without -H=windowsgui. Reopening the
	// streams below is harmless there and keeps both builds behaving the
	// same way.
	if ok == 0 && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return
	}

	out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return
	}
	os.Stdout = out
	os.Stderr = out

	// The Go variables above only cover this process. Setting the handles
	// too means anything it spawns inherits the same console.
	_ = windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, windows.Handle(out.Fd()))
	_ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(out.Fd()))
}
