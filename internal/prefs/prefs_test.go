package prefs

import (
	"strings"
	"testing"

	"github.com/spriz/meeting-blaster/internal/config"
)

func TestSubscriptionLabelKeepsFeedURLsPrivate(t *testing.T) {
	const secretURL = "https://calendar.example/secret-feed-token.ics"
	src := config.ICSSource{ID: "b2f38c1a", URL: secretURL}

	tests := []struct {
		name          string
		source        config.ICSSource
		calendarNames map[string]string
		want          string
	}{
		{
			name:   "configured name wins",
			source: config.ICSSource{ID: src.ID, URL: secretURL, Name: "Personal"},
			calendarNames: map[string]string{
				"ics:" + src.ID: "Feed calendar",
			},
			want: "Personal",
		},
		{
			name: "feed calendar name",
			calendarNames: map[string]string{
				"ics:" + src.ID: "Feed calendar",
			},
			want: "Feed calendar",
		},
		{
			name:          "safe fallback when no name is available",
			calendarNames: map[string]string{},
			want:          "Calendar " + src.ID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := tt.source
			if source.ID == "" {
				source = src
			}
			got := subscriptionLabel(source, tt.calendarNames)
			if got != tt.want {
				t.Errorf("subscriptionLabel() = %q, want %q", got, tt.want)
			}
			if strings.Contains(got, secretURL) {
				t.Errorf("subscriptionLabel() exposed feed URL: %q", got)
			}
		})
	}
}
