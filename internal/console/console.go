// Package console gives a windowless GUI binary somewhere to print.
//
// It exists for Windows, where the release binary is linked with
// -H=windowsgui so that launching it does not flash up a console window.
// Everywhere else the process already has usable standard handles and this
// is a no-op.
package console

// Attach reconnects os.Stdout and os.Stderr to the terminal that started
// the process, when there is one. Call it before anything prints, and
// before capturing os.Stderr for a logger.
//
// Failure is not an error: launched from a desktop shell there is no
// console to attach to, and a tray app printing nowhere is the intended
// outcome of that launch.
func Attach() { attach() }
