// Package console gives a windowless GUI binary somewhere to print.
//
// It exists for Windows, where the release binary is linked with
// -H=windowsgui. Everywhere else it is a no-op.
package console

// Attach reconnects os.Stdout and os.Stderr to the terminal that started
// the process, when there is one. Call it before anything prints, and
// before capturing os.Stderr for a logger.
//
// Failure is fine: launched from a desktop shell there is no console, and
// printing nowhere is the right outcome for that launch.
func Attach() { attach() }
