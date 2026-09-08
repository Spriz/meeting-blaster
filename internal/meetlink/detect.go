// Package meetlink extracts video-call join links from calendar event text.
//
// Structured conference data (Google's conferenceData, Teams' onlineMeeting)
// is always preferable and providers should use it first. This package is the
// fallback for the very common case of a bare link pasted into the location
// or description field.
package meetlink

import (
	"regexp"
	"strings"
)

// Service is a recognised video-conferencing product.
type Service struct {
	Name string
	re   *regexp.Regexp
}

// services is ordered: the first match wins. Keep more specific patterns
// above more general ones (Teams before the generic microsoft.com host).
var services = []Service{
	{Name: "Google Meet", re: regexp.MustCompile(`https://meet\.google\.com/[a-z0-9\-]+`)},
	{Name: "Zoom", re: regexp.MustCompile(`https://[a-zA-Z0-9\-.]*zoom\.us/(?:j|s|w|my)/[a-zA-Z0-9?&=_.\-/]+`)},
	{Name: "Microsoft Teams", re: regexp.MustCompile(`https://teams\.microsoft\.com/l/meetup-join/[^\s<>"]+`)},
	{Name: "Microsoft Teams", re: regexp.MustCompile(`https://teams\.live\.com/meet/[^\s<>"]+`)},
	{Name: "Webex", re: regexp.MustCompile(`https://[a-zA-Z0-9\-.]*webex\.com/[a-zA-Z0-9?&=_.\-/#]+`)},
	{Name: "Whereby", re: regexp.MustCompile(`https://whereby\.com/[a-zA-Z0-9\-_]+`)},
	{Name: "Jitsi", re: regexp.MustCompile(`https://meet\.jit\.si/[a-zA-Z0-9\-_]+`)},
	{Name: "GoTo Meeting", re: regexp.MustCompile(`https://(?:www\.)?(?:gotomeet\.me|goto\.com/meeting)/[a-zA-Z0-9\-_/]+`)},
	{Name: "BlueJeans", re: regexp.MustCompile(`https://[a-zA-Z0-9\-.]*bluejeans\.com/[0-9a-zA-Z/]+`)},
	{Name: "Amazon Chime", re: regexp.MustCompile(`https://chime\.aws/[0-9]+`)},
	{Name: "Around", re: regexp.MustCompile(`https://meet\.around\.co/r/[a-zA-Z0-9\-_]+`)},
	{Name: "Discord", re: regexp.MustCompile(`https://discord\.(?:gg|com)/[a-zA-Z0-9\-_/]+`)},
	{Name: "Slack", re: regexp.MustCompile(`https://app\.slack\.com/huddle/[a-zA-Z0-9/]+`)},
}

// Result is a detected join link.
type Result struct {
	URL     string
	Service string
}

// Found reports whether a link was detected.
func (r Result) Found() bool { return r.URL != "" }

// Detect scans each field in order and returns the first join link found.
// Fields are searched in the order given, so callers should pass the most
// authoritative field (location) before free text (description).
func Detect(fields ...string) Result {
	for _, field := range fields {
		if field == "" {
			continue
		}
		text := unescape(field)
		for _, svc := range services {
			if m := svc.re.FindString(text); m != "" {
				return Result{URL: strings.TrimRight(m, ".,;:)]}\"'"), Service: svc.Name}
			}
		}
	}
	return Result{}
}

// unescape undoes the HTML entity encoding Google applies to description
// fields, so links inside <a href="..."> markup are still matchable.
var entities = strings.NewReplacer(
	"&amp;", "&",
	"&lt;", "<",
	"&gt;", ">",
	"&quot;", `"`,
	"&#39;", "'",
	"&nbsp;", " ",
)

func unescape(s string) string { return entities.Replace(s) }
