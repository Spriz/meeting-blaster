//go:build darwin

package main

import "testing"

// Which side of this predicate a path lands on decides the app's macOS
// activation policy: inside a bundle, the Info.plist keeps the app out of
// the Dock; outside one, GLFW has to make it a foreground app or its
// windows can never take focus.
func TestInsideAppBundle(t *testing.T) {
	cases := []struct {
		name string
		exe  string
		want bool
	}{
		{"bundled", "/Applications/MeetingBlaster.app/Contents/MacOS/meeting-blaster", true},
		{"bundled under a path with spaces", "/Users/me/My Apps/MeetingBlaster.app/Contents/MacOS/meeting-blaster", true},
		{"loose binary from the tarball", "/usr/local/bin/meeting-blaster", false},
		{"binary merely living beside a bundle", "/Applications/MeetingBlaster.app/meeting-blaster", false},
		{"directory named like a bundle path", "/opt/x.app/Contents/Resources/meeting-blaster", false},
		{"empty", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := insideAppBundle(tc.exe); got != tc.want {
				t.Errorf("insideAppBundle(%q) = %v, want %v", tc.exe, got, tc.want)
			}
		})
	}
}
