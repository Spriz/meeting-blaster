// Package notify sends ordinary desktop notifications.
//
// These are the gentle nudge; the full-screen overlay in internal/overlay is
// the one you cannot miss.
package notify

// Send posts a desktop notification. Failure is not fatal: a missing
// notification daemon should never take down the app, so callers may log and
// carry on.
func Send(title, body string) error { return send(title, body) }
