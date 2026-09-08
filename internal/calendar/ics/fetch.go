package ics

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	ical "github.com/arran4/golang-ical"
)

// maxFeedBytes caps a feed body. A subscription is text; anything past this
// is a misconfiguration or a hostile server, and parsing it would block the
// poll and eat the heap.
const maxFeedBytes = 16 << 20

// cached is the last successful fetch of one source. Keeping the parsed
// calendar, not just the bytes, means a 304 skips reparsing too.
type cached struct {
	cal      *ical.Calendar
	name     string // X-WR-CALNAME
	etag     string
	lastMod  string
	fileTime time.Time
}

func (p *Provider) cachedFor(key string) *cached {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cache[key]
}

func (p *Provider) store(key string, c *cached) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cache[key] = c
}

// load refreshes one source, returning the parsed calendar. On failure with
// a cached copy available, both the stale copy and the error are returned:
// a feed that is briefly 503 keeps working through the partial-failure
// policy in Events.
func (p *Provider) load(ctx context.Context, src Source) (*cached, error) {
	prev := p.cachedFor(src.URL)

	if path, ok := localPath(src.URL); ok {
		return p.loadFile(src.URL, path, prev)
	}

	return p.loadHTTP(ctx, src.URL, prev)
}

// logURL is the only form of a source location safe to record.
//
// A subscription URL is a bearer credential: Google's "secret address in
// iCal format" and Outlook's publish links carry the secret in the path,
// share links carry it in the query, and none of them need an auth header -
// which is exactly why this provider works without one. Errors and warnings
// reach the systemd journal and get pasted into bug reports, so only the
// host goes in. A local .ics path is not a credential, and naming it is the
// only way to find the file.
func logURL(raw string) string {
	if path, ok := localPath(raw); ok {
		return path
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "<unparseable feed url>"
	}
	return u.Scheme + "://" + u.Host
}

// scrubbed strips the URL out of a transport error. net/http reports
// failures as *url.Error, whose message quotes the whole request URL, so
// wrapping one verbatim would leak the credential that logURL just
// removed. The underlying cause still names the host and the syscall.
func scrubbed(err error) error {
	var uerr *url.Error
	if errors.As(err, &uerr) && uerr.Err != nil {
		return uerr.Err
	}
	return err
}

// localPath reports whether the source lives on disk, and where. Anything
// without an http(s) scheme is a path: config.NormalizeCalendarURL has
// already turned a bare path into an absolute one.
func localPath(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return raw, true
	}
	switch u.Scheme {
	case "http", "https":
		return "", false
	case "file":
		if u.Path != "" {
			return u.Path, true
		}
		return u.Opaque, true
	default:
		return raw, true
	}
}

func (p *Provider) loadFile(key, path string, prev *cached) (*cached, error) {
	info, err := os.Stat(path)
	if err != nil {
		return prev, fmt.Errorf("read calendar file %s: %w", path, err)
	}
	if prev != nil && info.ModTime().Equal(prev.fileTime) {
		return prev, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return prev, fmt.Errorf("read calendar file %s: %w", path, err)
	}
	cal, err := parse(data)
	if err != nil {
		return prev, fmt.Errorf("parse calendar file %s: %w", path, err)
	}

	next := &cached{cal: cal, name: calendarName(cal), fileTime: info.ModTime()}
	p.store(key, next)
	return next, nil
}

func (p *Provider) loadHTTP(ctx context.Context, rawURL string, prev *cached) (*cached, error) {
	// Every error below names the host, never the URL: the path and query
	// are the feed's credential. See logURL.
	safe := logURL(rawURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return prev, fmt.Errorf("fetch %s: %w", safe, scrubbed(err))
	}
	req.Header.Set("Accept", "text/calendar")
	if prev != nil {
		if prev.etag != "" {
			req.Header.Set("If-None-Match", prev.etag)
		}
		if prev.lastMod != "" {
			req.Header.Set("If-Modified-Since", prev.lastMod)
		}
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return prev, fmt.Errorf("fetch %s: %w", safe, scrubbed(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified && prev != nil {
		return prev, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return prev, fmt.Errorf("fetch %s: %s", safe, resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBytes+1))
	if err != nil {
		return prev, fmt.Errorf("fetch %s: %w", safe, scrubbed(err))
	}
	if int64(len(data)) > maxFeedBytes {
		return prev, fmt.Errorf("calendar feed %s is larger than 16 MiB", safe)
	}

	cal, err := parse(data)
	if err != nil {
		return prev, fmt.Errorf("parse %s: %w", safe, err)
	}

	next := &cached{
		cal:     cal,
		name:    calendarName(cal),
		etag:    resp.Header.Get("ETag"),
		lastMod: resp.Header.Get("Last-Modified"),
	}
	p.store(rawURL, next)
	return next, nil
}

// parse decodes one VCALENDAR. Feeds that concatenate several are rejected
// by the library and are not supported: subscription feeds publish exactly
// one.
func parse(data []byte) (*ical.Calendar, error) {
	// AcceptUnknownPropertyHandler is the library's opt-in for properties
	// appearing between components - non-standard, but exactly the
	// population of feeds this provider has to survive. The default
	// handler rejects the whole calendar.
	return ical.ParseCalendarWithOptions(bytes.NewReader(data),
		ical.WithTimezoneMapper(mapTimezone),
		ical.WithUnknownPropertyHandler(ical.AcceptUnknownPropertyHandler),
	)
}

// calendarName reads X-WR-CALNAME, the de facto feed title. There is no
// getter for it on *ical.Calendar.
func calendarName(cal *ical.Calendar) string {
	for _, prop := range cal.CalendarProperties {
		if prop.IANAToken == string(ical.PropertyXWRCalName) {
			return ical.FromText(prop.Value)
		}
	}
	return ""
}

// mapTimezone resolves a TZID to a location for every date-time in the feed.
// Returning a non-nil location unconditionally is deliberate: the library
// otherwise falls back to time.LoadLocation and fails the whole property on
// an unknown zone, which would silently drop the event. A meeting shown at
// the wrong time is recoverable; an invisible one is not.
func mapTimezone(tzid string) *time.Location {
	if loc, err := time.LoadLocation(tzid); err == nil {
		return loc
	}
	if loc := ical.WindowsTimezoneToIANA(tzid); loc != nil {
		return loc
	}
	return time.UTC
}
