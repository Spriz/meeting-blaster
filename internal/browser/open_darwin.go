//go:build darwin

package browser

func command(url string) (string, []string) { return "open", []string{url} }
