package ics

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	ical "github.com/arran4/golang-ical"

	"github.com/spriz/meeting-blaster/internal/calendar"
)

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// eventsFrom expands one source over [from, to), failing the test on error.
func eventsFrom(t *testing.T, src Source, from, to time.Time) []calendar.Event {
	t.Helper()
	events, err := New([]Source{src}, quiet()).Events(context.Background(), from, to)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	return events
}

// writeFeed puts an inline calendar on disk and returns a source for it.
func writeFeed(t *testing.T, body string) Source {
	t.Helper()
	path := filepath.Join(t.TempDir(), "feed.ics")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write feed: %v", err)
	}
	return Source{ID: "feed", URL: path}
}

func TestExpandsRecurrenceWithExdateAndOverride(t *testing.T) {
	cph, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Skipf("no tzdata for Europe/Copenhagen: %v", err)
	}

	from := time.Date(2026, 6, 1, 0, 0, 0, 0, cph)
	to := time.Date(2026, 7, 1, 0, 0, 0, 0, cph)

	events := eventsFrom(t, Source{ID: "feed", URL: "testdata/recurring.ics"}, from, to)

	// The series runs weekly for six occurrences from 1 June. Two are
	// excluded by the multi-value EXDATE, one is moved by an override,
	// one is cancelled by an override, and the sixth is past `to`.
	want := []struct {
		start time.Time
		title string
	}{
		{time.Date(2026, 6, 1, 9, 0, 0, 0, cph), "Standup"},
		{time.Date(2026, 6, 22, 10, 0, 0, 0, cph), "Standup (moved)"},
	}

	if len(events) != len(want) {
		t.Fatalf("got %d events, want %d: %s", len(events), len(want), describe(events))
	}
	for i, w := range want {
		if !events[i].Start.Equal(w.start) {
			t.Errorf("event %d start = %s, want %s", i, events[i].Start, w.start)
		}
		if events[i].Title != w.title {
			t.Errorf("event %d title = %q, want %q", i, events[i].Title, w.title)
		}
	}
}

// TestRDateOnlySeriesKeepsDtstart guards a trap in rrule-go: Set.DTStart
// records metadata, but Set.Iterator builds its generator list from the
// RRULE and the RDATEs only. With RDATEs and no RRULE, DTSTART - which
// RFC 5545 makes the first instance of the recurrence set - would vanish.
func TestRDateOnlySeriesKeepsDtstart(t *testing.T) {
	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	events := eventsFrom(t, Source{ID: "feed", URL: "testdata/rdate_only.ics"}, from, to)

	want := []struct {
		start time.Time
		title string
	}{
		// DTSTART, then both RDATEs.
		{time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC), "Irregular series"},
		// The second event's DTSTART is removed by its own EXDATE, so
		// only its RDATE survives - DTSTART must be excludable, not
		// unconditional.
		{time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC), "Irregular series"},
		{time.Date(2026, 6, 13, 14, 0, 0, 0, time.UTC), "First instance excluded"},
		{time.Date(2026, 6, 15, 8, 0, 0, 0, time.UTC), "Irregular series"},
	}

	if len(events) != len(want) {
		t.Fatalf("got %d events, want %d: %s", len(events), len(want), describe(events))
	}
	for i, w := range want {
		if !events[i].Start.Equal(w.start) {
			t.Errorf("event %d start = %s, want %s", i, events[i].Start.UTC(), w.start)
		}
		if events[i].Title != w.title {
			t.Errorf("event %d title = %q, want %q", i, events[i].Title, w.title)
		}
	}
}

func TestWindowsTimezoneResolves(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	events := eventsFrom(t, Source{ID: "feed", URL: "testdata/tzid_windows.ics"}, from, to)
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2: %s", len(events), describe(events))
	}

	// 14:00 in "W. Europe Standard Time" is Europe/Berlin, so the offset
	// differs by season. A fixed-offset fallback would get one of these
	// an hour wrong.
	want := []time.Time{
		time.Date(2026, 1, 10, 13, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC),
	}
	for i, w := range want {
		if !events[i].Start.Equal(w) {
			t.Errorf("event %d start = %s, want %s", i, events[i].Start.UTC(), w)
		}
	}
}

func TestUnknownTimezoneFallsBackToUTC(t *testing.T) {
	src := writeFeed(t, `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
BEGIN:VEVENT
UID:mars@example.com
DTSTAMP:20260501T120000Z
DTSTART;TZID=Mars/Olympus:20260610T140000
DTEND;TZID=Mars/Olympus:20260610T150000
SUMMARY:Off-world sync
END:VEVENT
END:VCALENDAR
`)

	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	events := eventsFrom(t, src, from, to)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 interpreted as UTC rather than dropped: %s",
			len(events), describe(events))
	}
	want := time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)
	if !events[0].Start.Equal(want) {
		t.Errorf("start = %s, want %s", events[0].Start.UTC(), want)
	}
}

func TestAllDayEventIsMarked(t *testing.T) {
	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.Local)
	to := time.Date(2026, 7, 1, 0, 0, 0, 0, time.Local)

	events := eventsFrom(t, Source{ID: "feed", URL: "testdata/allday.ics"}, from, to)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1: %s", len(events), describe(events))
	}

	ev := events[0]
	if !ev.AllDay {
		t.Error("AllDay = false, want true; engine.Filter would keep this as a meeting")
	}
	wantStart := time.Date(2026, 6, 10, 0, 0, 0, 0, time.Local)
	if !ev.Start.Equal(wantStart) {
		t.Errorf("start = %s, want local midnight %s", ev.Start, wantStart)
	}
	if got := ev.Duration(); got != 24*time.Hour {
		t.Errorf("duration = %s, want 24h", got)
	}
}

func TestJoinLinkPrecedence(t *testing.T) {
	from := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)

	events := eventsFrom(t, Source{ID: "feed", URL: "testdata/links.ics"}, from, to)
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3: %s", len(events), describe(events))
	}

	want := []string{
		"https://whereby.com/structured",                  // CONFERENCE;FEATURE=VIDEO
		"https://meet.google.com/abc-defg-hij",            // X-GOOGLE-CONFERENCE
		"https://example.zoom.us/j/9876543210?pwd=secret", // meetlink.Detect
	}
	for i, w := range want {
		if events[i].MeetingURL != w {
			t.Errorf("event %d (%s) link = %q, want %q", i, events[i].Title, events[i].MeetingURL, w)
		}
	}
}

func TestConditionalGetSkipsReparse(t *testing.T) {
	data, err := os.ReadFile("testdata/recurring.ics")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	const etag = `"v1"`
	var (
		mu       sync.Mutex
		requests int
		bodies   int
		matched  bool
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		n := requests
		if inm := r.Header.Get("If-None-Match"); inm == etag {
			matched = true
		}
		mu.Unlock()

		if n > 1 {
			if !matched {
				t.Errorf("request %d carried If-None-Match %q, want %q",
					n, r.Header.Get("If-None-Match"), etag)
			}
			w.WriteHeader(http.StatusNotModified)
			return
		}

		mu.Lock()
		bodies++
		mu.Unlock()
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	cph, err := time.LoadLocation("Europe/Copenhagen")
	if err != nil {
		t.Skipf("no tzdata for Europe/Copenhagen: %v", err)
	}
	from := time.Date(2026, 6, 1, 0, 0, 0, 0, cph)
	to := time.Date(2026, 7, 1, 0, 0, 0, 0, cph)

	p := New([]Source{{ID: "feed", URL: srv.URL + "/cal.ics"}}, quiet())

	first, err := p.Events(context.Background(), from, to)
	if err != nil {
		t.Fatalf("first Events: %v", err)
	}
	second, err := p.Events(context.Background(), from, to)
	if err != nil {
		t.Fatalf("second Events: %v", err)
	}

	if len(first) == 0 {
		t.Fatal("first Events returned nothing")
	}
	if len(second) != len(first) {
		t.Errorf("second Events returned %d events, want %d from the cache", len(second), len(first))
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 2 {
		t.Errorf("server saw %d requests, want 2", requests)
	}
	if bodies != 1 {
		t.Errorf("server served %d bodies, want 1: the 304 must reuse the parsed calendar", bodies)
	}
}

func TestPartialSourceFailureStillReturnsEvents(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer dead.Close()

	from := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)

	good := Source{ID: "good", URL: "testdata/links.ics"}
	bad := Source{ID: "bad", URL: dead.URL + "/cal.ics"}

	// engine.poll throws away the whole event set on error, so one dead
	// feed must not blank out a working one.
	events, err := New([]Source{good, bad}, quiet()).Events(context.Background(), from, to)
	if err != nil {
		t.Fatalf("Events with one dead source returned error: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events from the working source, want 3: %s", len(events), describe(events))
	}

	if _, err := New([]Source{bad}, quiet()).Events(context.Background(), from, to); err == nil {
		t.Error("Events with every source failing returned nil error")
	}
}

func TestMalformedFeedIsAnError(t *testing.T) {
	src := writeFeed(t, "not a calendar\r\nBEGIN:VEVENT\r\n")

	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	_, err := New([]Source{src}, quiet()).Events(context.Background(), from, to)
	if err == nil {
		t.Fatal("Events on a malformed feed returned nil error")
	}
	var malformed *ical.MalformedError
	if !errors.As(err, &malformed) {
		t.Errorf("error %v does not unwrap to *ical.MalformedError", err)
	}
}

func TestEscapedTextIsUnescaped(t *testing.T) {
	src := writeFeed(t, `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
BEGIN:VEVENT
UID:escaped@example.com
DTSTAMP:20260501T120000Z
DTSTART:20260610T080000Z
DTEND:20260610T083000Z
SUMMARY:Standup\, daily
DESCRIPTION:line one\nline two
END:VEVENT
END:VCALENDAR
`)

	from := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)

	events := eventsFrom(t, src, from, to)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if got := events[0].Title; got != "Standup, daily" {
		t.Errorf("title = %q, want %q", got, "Standup, daily")
	}
	if got := events[0].Notes; got != "line one\nline two" {
		t.Errorf("notes = %q, want a real newline", got)
	}
}

func TestParseICalDuration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"PT30M", 30 * time.Minute, true},
		{"P1DT2H", 26 * time.Hour, true},
		{"-PT15M", -15 * time.Minute, true},
		{"P2W", 14 * 24 * time.Hour, true},
		{"PT1H30M0S", 90 * time.Minute, true},
		{"P30M", 0, false}, // months are not a DURATION unit
		{"PT", 0, false},
		{"30M", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, err := parseICalDuration(c.in)
		if (err == nil) != c.ok {
			t.Errorf("parseICalDuration(%q) error = %v, want ok=%v", c.in, err, c.ok)
			continue
		}
		if c.ok && got != c.want {
			t.Errorf("parseICalDuration(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestCalendarsNameSources(t *testing.T) {
	cals, err := New([]Source{
		{ID: "a", URL: "testdata/recurring.ics"},          // X-WR-CALNAME
		{ID: "b", URL: "testdata/links.ics"},              // no name: use the file
		{ID: "c", Name: "Mine", URL: "testdata/nope.ics"}, // unfetchable, still listed
	}, quiet()).Calendars(context.Background())
	if err != nil {
		t.Fatalf("Calendars: %v", err)
	}

	want := []string{"Team calendar", "links", "Mine"}
	if len(cals) != len(want) {
		t.Fatalf("got %d calendars, want %d", len(cals), len(want))
	}
	for i, w := range want {
		if cals[i].Name != w {
			t.Errorf("calendar %d name = %q, want %q", i, cals[i].Name, w)
		}
	}
}

func describe(events []calendar.Event) string {
	var b strings.Builder
	for _, ev := range events {
		b.WriteString("\n\t")
		b.WriteString(ev.Start.Format(time.RFC3339))
		b.WriteString(" ")
		b.WriteString(ev.Title)
	}
	return b.String()
}
