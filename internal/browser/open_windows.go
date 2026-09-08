//go:build windows

package browser

func command(url string) (string, []string) {
	// "start" is a cmd builtin, and the empty string is the window title
	// argument - without it a quoted URL is treated as the title.
	return "cmd", []string{"/c", "start", "", url}
}
