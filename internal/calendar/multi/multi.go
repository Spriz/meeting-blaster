// Package multi presents several calendar providers as one, namespacing
// their calendar IDs so two sources cannot collide.
//
// Prefixing is unconditional, including for a single source, which is what
// config.migrateCalendarIDs compensates for on existing installs.
package multi

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/spriz/meeting-blaster/internal/calendar"
)

// Source pairs a provider with the key that prefixes its calendar IDs.
// Keys come from config: config.SourceGoogle, config.SourceICS.
type Source struct {
	Key      string
	Provider calendar.Provider
}

// Provider aggregates several calendar.Provider implementations.
type Provider struct {
	log     *slog.Logger
	sources []Source
}

// New builds an aggregate over the given sources.
func New(log *slog.Logger, sources ...Source) *Provider {
	if log == nil {
		log = slog.Default()
	}
	return &Provider{log: log, sources: sources}
}

// Name implements calendar.Provider.
func (p *Provider) Name() string {
	names := make([]string, 0, len(p.sources))
	for _, src := range p.sources {
		names = append(names, src.Provider.Name())
	}
	return strings.Join(names, ", ")
}

// Calendars implements calendar.Provider, prefixing each child's calendar
// IDs with its source key.
func (p *Provider) Calendars(ctx context.Context) ([]calendar.Calendar, error) {
	type reply struct {
		cals []calendar.Calendar
		err  error
	}
	replies := make([]reply, len(p.sources))

	var wg sync.WaitGroup
	for i, src := range p.sources {
		wg.Add(1)
		go func(i int, src Source) {
			defer wg.Done()
			cals, err := src.Provider.Calendars(ctx)
			replies[i] = reply{cals: cals, err: err}
		}(i, src)
	}
	wg.Wait()

	// With more than one source the provider name disambiguates two
	// calendars that happen to share a title, matching the "  (primary)"
	// suffix style the settings window already uses.
	label := len(p.sources) > 1

	var (
		out    []calendar.Calendar
		errs   []error
		loaded int
	)
	for i, src := range p.sources {
		if replies[i].err != nil {
			p.log.Warn("could not list calendars", "provider", src.Provider.Name(), "error", replies[i].err)
			errs = append(errs, replies[i].err)
			continue
		}
		loaded++
		for _, cal := range replies[i].cals {
			cal.ID = src.Key + ":" + cal.ID
			if label {
				cal.Name += "  (" + src.Provider.Name() + ")"
			}
			out = append(out, cal)
		}
	}

	if loaded == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// Events implements calendar.Provider.
//
// Partial failure is success: engine.poll discards the whole event set when
// the provider errors, so a Google token that needs re-auth must not blank
// out working ICS feeds. Only when every child fails is an error returned.
func (p *Provider) Events(ctx context.Context, from, to time.Time) ([]calendar.Event, error) {
	type reply struct {
		events []calendar.Event
		err    error
	}
	replies := make([]reply, len(p.sources))

	var wg sync.WaitGroup
	for i, src := range p.sources {
		wg.Add(1)
		go func(i int, src Source) {
			defer wg.Done()
			events, err := src.Provider.Events(ctx, from, to)
			replies[i] = reply{events: events, err: err}
		}(i, src)
	}
	wg.Wait()

	var (
		out    []calendar.Event
		errs   []error
		loaded int
	)
	for i, src := range p.sources {
		if replies[i].err != nil {
			p.log.Warn("calendar source failed", "provider", src.Provider.Name(), "error", replies[i].err)
			errs = append(errs, replies[i].err)
			continue
		}
		loaded++
		for _, ev := range replies[i].events {
			ev.CalendarID = src.Key + ":" + ev.CalendarID
			out = append(out, ev)
		}
	}

	if loaded == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	calendar.SortByStart(out)
	return out, nil
}
