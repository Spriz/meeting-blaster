//go:build !windows

package console

// Unix processes inherit standard handles from whatever started them, so
// there is nothing to reattach.
func attach() {}
