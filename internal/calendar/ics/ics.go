// Package ics implements calendar.Provider against iCalendar
// subscriptions: webcal:// or https:// feed URLs, and .ics files on disk.
//
// It is deliberately forgiving: a feed that will not fetch, a timezone
// nobody has heard of, or a property the spec does not mention must not stop
// the other feeds from producing a meeting.
//
// Authenticated feeds are out of scope. Every real-world subscription URL
// carries its secret in the URL itself; anything needing an auth header is
// CalDAV's job.
package ics

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/spriz/meeting-blaster/internal/calendar"
)

// Source is one subscription the provider polls.
type Source struct {
	ID    string
	Name  string
	URL   string
	Email string
}

// Provider reads events from iCalendar subscriptions. Safe for concurrent
// use: the engine polls from a background goroutine while preferences may
// be replacing the source list.
type Provider struct {
	client *http.Client
	log    *slog.Logger

	mu      sync.Mutex
	sources []Source
	cache   map[string]*cached // keyed by Source.URL
}

// New builds a Provider over the given subscriptions.
func New(sources []Source, log *slog.Logger) *Provider {
	if log == nil {
		log = slog.Default()
	}
	return &Provider{
		client:  &http.Client{Timeout: 20 * time.Second},
		log:     log,
		sources: sources,
		cache:   make(map[string]*cached),
	}
}

// SetSources replaces the subscription list. The fetch cache is kept, so
// saving preferences does not throw away the ETags of unchanged feeds.
func (p *Provider) SetSources(sources []Source) {
	p.mu.Lock()
	p.sources = sources
	p.mu.Unlock()
}

// Name implements calendar.Provider.
func (p *Provider) Name() string { return "iCalendar subscriptions" }

// snapshot copies the source list so a poll is not holding the mutex while
// it does network I/O.
func (p *Provider) snapshot() []Source {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Source, len(p.sources))
	copy(out, p.sources)
	return out
}

// Calendars implements calendar.Provider. One row per subscription. A
// source that fails to fetch still gets a row so it stays tickable in
// preferences.
func (p *Provider) Calendars(ctx context.Context) ([]calendar.Calendar, error) {
	sources := p.snapshot()
	if len(sources) == 0 {
		return nil, nil
	}

	results := p.loadAll(ctx, sources)

	out := make([]calendar.Calendar, 0, len(sources))
	for i, src := range sources {
		name := src.Name
		if name == "" && results[i].cal != nil {
			name = results[i].cal.name
		}
		if name == "" {
			name = "Calendar " + src.ID
		}
		out = append(out, calendar.Calendar{ID: src.ID, Name: name})
	}
	return out, nil
}

// Events implements calendar.Provider.
//
// Partial failure is success: engine.poll discards the whole event set when
// the provider errors, so one dead feed must not blank out a working one.
// Only when every source fails is an error returned.
func (p *Provider) Events(ctx context.Context, from, to time.Time) ([]calendar.Event, error) {
	sources := p.snapshot()
	if len(sources) == 0 {
		return nil, nil
	}

	results := p.loadAll(ctx, sources)

	var (
		out    []calendar.Event
		errs   []error
		loaded int
	)
	for i, src := range sources {
		if results[i].err != nil {
			// Identified by source ID and host only: the rest of the
			// URL is the feed's credential. See logURL.
			p.log.Warn("calendar feed failed",
				"source", src.ID, "host", logURL(src.URL), "error", results[i].err)
			errs = append(errs, results[i].err)
		}
		if results[i].cal == nil {
			continue
		}
		loaded++
		out = append(out, p.instances(results[i].cal.cal, src, from, to)...)
	}

	if loaded == 0 {
		return nil, errors.Join(errs...)
	}

	calendar.SortByStart(out)
	return out, nil
}

// result is one source's outcome. cal is non-nil whenever a parsed calendar
// is available, including a stale cached one alongside a fetch error.
type result struct {
	cal *cached
	err error
}

// loadAll refreshes every source concurrently, returning results positioned
// to match sources.
func (p *Provider) loadAll(ctx context.Context, sources []Source) []result {
	results := make([]result, len(sources))

	var wg sync.WaitGroup
	for i, src := range sources {
		wg.Add(1)
		go func(i int, src Source) {
			defer wg.Done()
			cal, err := p.load(ctx, src)
			results[i] = result{cal: cal, err: err}
		}(i, src)
	}
	wg.Wait()

	return results
}
