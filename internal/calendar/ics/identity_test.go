package ics

import (
	"slices"
	"testing"
	"time"

	"github.com/spriz/meeting-blaster/internal/config"
	"github.com/spriz/meeting-blaster/internal/engine"
)

func TestSharedMovedOccurrencesRemainDistinct(t *testing.T) {
	src := writeFeed(t, `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
BEGIN:VEVENT
UID:moved@example.com
DTSTAMP:20260501T120000Z
DTSTART:20260601T090000Z
DTEND:20260601T093000Z
RRULE:FREQ=DAILY;COUNT=2
SUMMARY:Original series
END:VEVENT
BEGIN:VEVENT
UID:moved@example.com
DTSTAMP:20260501T120000Z
RECURRENCE-ID:20260601T090000Z
DTSTART:20260610T090000Z
DTEND:20260610T093000Z
SUMMARY:First moved occurrence
END:VEVENT
BEGIN:VEVENT
UID:moved@example.com
DTSTAMP:20260501T120000Z
RECURRENCE-ID:20260602T090000Z
DTSTART:20260610T090000Z
DTEND:20260610T093000Z
SUMMARY:Second moved occurrence
END:VEVENT
END:VCALENDAR
`)
	copy := writeFeed(t, `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
BEGIN:VEVENT
UID:moved@example.com
DTSTAMP:20260501T120000Z
RECURRENCE-ID:20260601T090000Z
DTSTART:20260610T090000Z
DTEND:20260610T093000Z
SUMMARY:Orphan copy of first occurrence
END:VEVENT
END:VCALENDAR
`)
	src.ID, copy.ID = "a", "b"
	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	events := append(eventsFrom(t, src, from, to), eventsFrom(t, copy, from, to)...)

	// The orphan copy must merge with the matching master/override instance,
	// not with a separate occurrence moved onto the very same start time.
	var got []string
	for _, ev := range engine.Filter(events, config.Default()) {
		got = append(got, ev.Title)
	}
	want := []string{"First moved occurrence", "Second moved occurrence"}
	if !slices.Equal(got, want) {
		t.Fatalf("agenda = %v, want %v", got, want)
	}
}

func TestEventsWithoutUIDRemainSeparate(t *testing.T) {
	src := writeFeed(t, `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
BEGIN:VEVENT
DTSTAMP:20260501T120000Z
DTSTART:20260610T080000Z
DTEND:20260610T083000Z
SUMMARY:Unidentified meeting
END:VEVENT
BEGIN:VEVENT
DTSTAMP:20260501T120000Z
DTSTART:20260610T080000Z
DTEND:20260610T083000Z
SUMMARY:Unidentified meeting
END:VEVENT
END:VCALENDAR
`)
	from := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	events := engine.Filter(eventsFrom(t, src, from, to), config.Default())
	if len(events) != 2 {
		t.Fatalf("got %d agenda entries, want both unidentified events: %s", len(events), describe(events))
	}
}
