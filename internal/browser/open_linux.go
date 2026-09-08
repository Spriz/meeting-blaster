//go:build linux

package browser

func command(url string) (string, []string) { return "xdg-open", []string{url} }
