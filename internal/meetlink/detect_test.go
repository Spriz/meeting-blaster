package meetlink

import "testing"

func TestDetect(t *testing.T) {
	tests := []struct {
		name    string
		fields  []string
		wantURL string
		wantSvc string
	}{
		{
			name:    "bare meet link in location",
			fields:  []string{"https://meet.google.com/abc-defg-hij"},
			wantURL: "https://meet.google.com/abc-defg-hij",
			wantSvc: "Google Meet",
		},
		{
			name:    "zoom with query string",
			fields:  []string{"", "Join: https://apacta.zoom.us/j/98765432101?pwd=Abc123 see you"},
			wantURL: "https://apacta.zoom.us/j/98765432101?pwd=Abc123",
			wantSvc: "Zoom",
		},
		{
			name:    "teams link html-escaped in description",
			fields:  []string{"", `<a href="https://teams.microsoft.com/l/meetup-join/19%3ameeting_X%40thread.v2/0">Join</a>`},
			wantURL: "https://teams.microsoft.com/l/meetup-join/19%3ameeting_X%40thread.v2/0",
			wantSvc: "Microsoft Teams",
		},
		{
			name:    "trailing sentence punctuation is trimmed",
			fields:  []string{"Call on https://whereby.com/standup."},
			wantURL: "https://whereby.com/standup",
			wantSvc: "Whereby",
		},
		{
			name:   "location wins over description",
			fields: []string{"https://meet.google.com/aaa-bbbb-ccc", "https://apacta.zoom.us/j/111"},

			wantURL: "https://meet.google.com/aaa-bbbb-ccc",
			wantSvc: "Google Meet",
		},
		{
			name:   "physical location is not a link",
			fields: []string{"Meeting room 3, 2nd floor", "Bring the printouts"},
		},
		{
			name:   "empty input",
			fields: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Detect(tc.fields...)
			if got.URL != tc.wantURL {
				t.Errorf("URL = %q, want %q", got.URL, tc.wantURL)
			}
			if got.Service != tc.wantSvc {
				t.Errorf("Service = %q, want %q", got.Service, tc.wantSvc)
			}
			if got.Found() != (tc.wantURL != "") {
				t.Errorf("Found() = %v, want %v", got.Found(), tc.wantURL != "")
			}
		})
	}
}
