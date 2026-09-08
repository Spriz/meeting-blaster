package ics

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	ical "github.com/arran4/golang-ical"

	"github.com/spriz/meeting-blaster/internal/calendar"
	"github.com/spriz/meeting-blaster/internal/meetlink"
)

// Properties golang-ical has no constant for. ical.ComponentPropertyExtended
// cannot be used: at v0.3.6 it calls strings.TrimPrefix with its arguments
// reversed and returns "X-X-" for every input.
const (
	propConference       ical.ComponentProperty = "CONFERENCE"
	propGoogleConference ical.ComponentProperty = "X-GOOGLE-CONFERENCE"
)

// event maps one VEVENT onto the domain model at the given instance start.
// recurrenceID is the original start for a recurring instance; zero denotes
// a single event. Returns false for events that cannot be shown.
func event(ev *ical.VEvent, src Source, start time.Time, dur time.Duration, isAllDay bool, recurrenceID time.Time) (calendar.Event, bool) {
	if strings.EqualFold(text(ev, ical.ComponentPropertyStatus), string(ical.ObjectStatusCancelled)) {
		return calendar.Event{}, false
	}

	title := text(ev, ical.ComponentPropertySummary)
	if title == "" {
		title = "(no title)"
	}

	uid := text(ev, ical.ComponentPropertyUniqueId)
	location := text(ev, ical.ComponentPropertyLocation)
	notes := text(ev, ical.ComponentPropertyDescription)

	return calendar.Event{
		// Keep the feed-local ID separate from shared UID/RecurrenceID identity.
		ID:           uid + "/" + start.UTC().Format("20060102T150405Z"),
		CalendarID:   src.ID,
		Title:        title,
		Start:        start,
		End:          start.Add(dur),
		AllDay:       isAllDay,
		Location:     location,
		Notes:        notes,
		UID:          uid,
		RecurrenceID: recurrenceID.UTC(),
		MeetingURL:   joinURL(ev),
		Declined:     declined(ev, src.Email),
	}, true
}

// text reads a TEXT property, unescaping \n, \, and \; . Returns "" when the
// property is absent. Reading Value raw would leave those escapes visible
// in overlay titles and tray labels.
func text(ev *ical.VEvent, prop ical.ComponentProperty) string {
	p := ev.GetProperty(prop)
	if p == nil {
		return ""
	}
	return ical.FromText(p.Value)
}

// allDay reports whether DTSTART carries VALUE=DATE, which is how a
// date-only event is distinguished from a midnight one.
func allDay(ev *ical.VEvent) bool {
	p := ev.GetProperty(ical.ComponentPropertyDtStart)
	if p == nil {
		return false
	}
	for _, v := range p.ICalParameters["VALUE"] {
		if strings.EqualFold(v, "DATE") {
			return true
		}
	}
	return false
}

// duration is the event's length: DTEND minus DTSTART, else the DURATION
// property, else a whole day for a date-only event, else zero.
func duration(ev *ical.VEvent, start time.Time, isAllDay bool) time.Duration {
	if ev.HasProperty(ical.ComponentPropertyDtEnd) {
		var (
			end time.Time
			err error
		)
		if isAllDay {
			end, err = ev.GetAllDayEndAt()
		} else {
			end, err = ev.GetEndAt()
		}
		if err == nil && end.After(start) {
			return end.Sub(start)
		}
	}
	if p := ev.GetProperty(ical.ComponentPropertyDuration); p != nil {
		if d, err := parseICalDuration(p.Value); err == nil && d > 0 {
			return d
		}
	}
	if isAllDay {
		return 24 * time.Hour
	}
	return 0
}

// parseICalDuration reads an RFC 5545 DURATION: "PT30M", "P1DT2H",
// "-PT15M", "P2W". golang-ical has SetDuration but no getter or parser.
func parseICalDuration(s string) (time.Duration, error) {
	rest := strings.TrimSpace(s)
	if rest == "" {
		return 0, fmt.Errorf("empty duration")
	}

	sign := time.Duration(1)
	switch rest[0] {
	case '-':
		sign, rest = -1, rest[1:]
	case '+':
		rest = rest[1:]
	}

	if len(rest) == 0 || (rest[0] != 'P' && rest[0] != 'p') {
		return 0, fmt.Errorf("duration %q does not start with P", s)
	}
	rest = rest[1:]

	// Units are ordered W|D then T then H|M|S; a unit may appear at most
	// once, and anything else is a malformed value rather than something
	// to guess at.
	units := map[byte]time.Duration{
		'W': 7 * 24 * time.Hour,
		'D': 24 * time.Hour,
		'H': time.Hour,
		'M': time.Minute,
		'S': time.Second,
	}
	afterT := false
	seen := map[byte]bool{}

	var total time.Duration
	for len(rest) > 0 {
		if rest[0] == 'T' {
			if afterT {
				return 0, fmt.Errorf("duration %q has two time sections", s)
			}
			afterT = true
			rest = rest[1:]
			continue
		}

		digits := 0
		for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
			digits++
		}
		if digits == 0 || digits == len(rest) {
			return 0, fmt.Errorf("malformed duration %q", s)
		}
		n, err := strconv.Atoi(rest[:digits])
		if err != nil {
			return 0, fmt.Errorf("malformed duration %q", s)
		}

		unit := rest[digits]
		scale, ok := units[unit]
		if !ok || seen[unit] {
			return 0, fmt.Errorf("malformed duration %q", s)
		}
		// H, M and S are only legal after the T separator; W and D only
		// before it. Without this, "P30M" (30 months? minutes?) would
		// silently become 30 minutes.
		if timeUnit := unit == 'H' || unit == 'M' || unit == 'S'; timeUnit != afterT {
			return 0, fmt.Errorf("malformed duration %q", s)
		}
		seen[unit] = true

		total += time.Duration(n) * scale
		rest = rest[digits+1:]
	}

	if len(seen) == 0 {
		return 0, fmt.Errorf("duration %q has no components", s)
	}
	return sign * total, nil
}

// joinURL prefers structured conference data, then scans free text for a
// meeting link.
func joinURL(ev *ical.VEvent) string {
	confs := ev.GetProperties(propConference)
	for _, p := range confs {
		for _, feature := range p.ICalParameters["FEATURE"] {
			if strings.Contains(strings.ToUpper(feature), "VIDEO") && p.Value != "" {
				return p.Value
			}
		}
	}
	if len(confs) > 0 && confs[0].Value != "" {
		return confs[0].Value
	}
	// Non-standard, but what Google's own ICS exports carry.
	if p := ev.GetProperty(propGoogleConference); p != nil && p.Value != "" {
		return p.Value
	}
	return meetlink.Detect(
		text(ev, ical.ComponentPropertyLocation),
		text(ev, ical.ComponentPropertyDescription),
	).URL
}

// declined reports whether the configured address responded "no". An
// anonymous feed carries no identity, so with no address nothing is
// declined.
func declined(ev *ical.VEvent, email string) bool {
	if email == "" {
		return false
	}
	for _, att := range ev.Attendees() {
		if strings.EqualFold(att.Email(), email) &&
			att.ParticipationStatus() == ical.ParticipationStatusDeclined {
			return true
		}
	}
	return false
}
