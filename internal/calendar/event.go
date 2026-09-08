// Package calendar defines the provider-agnostic calendar model.
//
// Providers live in sibling packages and implement this shared model.
package calendar

import (
	"context"
	"time"
)

// Event is a single calendar entry, normalised across providers.
type Event struct {
	ID         string
	CalendarID string
	Title      string
	Start      time.Time
	End        time.Time
	AllDay     bool
	Location   string
	Notes      string

	// UID is the RFC 5545 UID shared by copies across calendars and
	// providers. Empty means no reliable cross-calendar identity is known.
	UID string

	// RecurrenceID is the original start of an expanded recurring instance,
	// even when it was rescheduled. Zero for non-recurring events. Providers
	// must leave UID empty if a recurring instance cannot be identified.
	RecurrenceID time.Time

	// MeetingURL is the join link, when one was found. Providers set this
	// from structured conference data where available, falling back to
	// scanning free text (see internal/meetlink).
	MeetingURL string

	// Declined reports that the source marked the event as declined.
	// Declined events are kept so the UI can choose to hide them.
	Declined bool
}

// Duration is the event's wall-clock length.
func (e Event) Duration() time.Duration { return e.End.Sub(e.Start) }

// InProgress reports whether now falls inside the event.
func (e Event) InProgress(now time.Time) bool {
	return !now.Before(e.Start) && now.Before(e.End)
}

// Upcoming reports whether the event has not started yet.
func (e Event) Upcoming(now time.Time) bool { return now.Before(e.Start) }

// HasLink reports whether the event can be joined.
func (e Event) HasLink() bool { return e.MeetingURL != "" }

// Calendar is one selectable calendar exposed by a provider.
type Calendar struct {
	ID      string
	Name    string
	Primary bool
}

// Provider is a source of events. Implementations must be safe for
// concurrent use; the engine calls Events from a background goroutine.
type Provider interface {
	// Name identifies the provider in logs and the settings UI.
	Name() string

	// Calendars lists the calendars exposed by the source.
	Calendars(ctx context.Context) ([]Calendar, error)

	// Events returns events overlapping [from, to), expanding recurrences
	// into individual instances, ordered by start time.
	Events(ctx context.Context, from, to time.Time) ([]Event, error)
}

// SortByStart orders events ascending by start time, in place.
func SortByStart(events []Event) {
	for i := 1; i < len(events); i++ {
		for j := i; j > 0 && events[j].Start.Before(events[j-1].Start); j-- {
			events[j], events[j-1] = events[j-1], events[j]
		}
	}
}
