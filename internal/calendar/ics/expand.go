package ics

import (
	"time"

	ical "github.com/arran4/golang-ical"
	"github.com/teambition/rrule-go"

	"github.com/spriz/meeting-blaster/internal/calendar"
)

// overrideSet holds one UID's RECURRENCE-ID overrides. Keys are Unix
// seconds: a time.Time map key also compares its monotonic clock and
// location, so two identical instants would not match.
type overrideSet struct {
	byRecurrenceID map[int64]*ical.VEvent
	order          []int64
	consumed       map[int64]bool
}

// instances turns the VEVENTs of one feed into concrete events overlapping
// [from, to), applying RRULE/RDATE/EXDATE and RECURRENCE-ID overrides.
//
// A method on *Provider only so it can log recoverable problems; it mutates
// no provider state.
func (p *Provider) instances(cal *ical.Calendar, src Source, from, to time.Time) []calendar.Event {
	masters, overrides, overrideUIDs := p.partition(cal)

	var out []calendar.Event

	for _, ev := range masters {
		uid := text(ev, ical.ComponentPropertyUniqueId)

		isAllDay := allDay(ev)
		start, err := startOf(ev, isAllDay)
		if err != nil {
			p.log.Warn("skipping calendar event with unreadable start", "uid", uid, "error", err)
			continue
		}
		dur := duration(ev, start, isAllDay)

		isRecurring := ev.HasProperty(ical.ComponentPropertyRrule) || ev.HasProperty(ical.ComponentPropertyRdate)
		for _, occ := range p.occurrences(ev, uid, start, dur, from, to) {
			inst, instStart, instDur, instAllDay := ev, occ, dur, isAllDay
			recurrenceID := time.Time{}
			if isRecurring {
				recurrenceID = occ
			}

			if ov := overrides.take(uid, occ); ov != nil {
				ovAllDay := allDay(ov)
				ovStart, err := startOf(ov, ovAllDay)
				if err != nil {
					p.log.Warn("skipping calendar override with unreadable start", "uid", uid, "error", err)
					continue
				}
				inst, instStart, instAllDay = ov, ovStart, ovAllDay
				instDur = duration(ov, ovStart, ovAllDay)
				recurrenceID = occ
			}

			// event() drops a CANCELLED instance, which is how a
			// RECURRENCE-ID override cancels a single occurrence.
			if e, ok := event(inst, src, instStart, instDur, instAllDay, recurrenceID); ok {
				out = append(out, e)
			}
		}
	}

	// An override with no matching occurrence is an instance moved out of
	// the base series, or an orphan whose master is not in this feed.
	// Either way it is a real meeting and belongs on screen.
	for _, uid := range overrideUIDs {
		set := overrides[uid]
		for _, rid := range set.order {
			if set.consumed[rid] {
				continue
			}
			ov := set.byRecurrenceID[rid]
			isAllDay := allDay(ov)
			start, err := startOf(ov, isAllDay)
			if err != nil {
				p.log.Warn("skipping calendar override with unreadable start", "uid", uid, "error", err)
				continue
			}
			if e, ok := event(ov, src, start, duration(ov, start, isAllDay), isAllDay, time.Unix(rid, 0)); ok {
				out = append(out, e)
			}
		}
	}

	return overlapping(out, from, to)
}

// overrideIndex maps UID to that series' overrides.
type overrideIndex map[string]*overrideSet

// take returns the override for one occurrence, marking it used so it is
// not also emitted as an orphan.
func (idx overrideIndex) take(uid string, occ time.Time) *ical.VEvent {
	set, ok := idx[uid]
	if !ok {
		return nil
	}
	ev, ok := set.byRecurrenceID[occ.Unix()]
	if !ok {
		return nil
	}
	set.consumed[occ.Unix()] = true
	return ev
}

// partition splits a feed's VEVENTs into series masters and RECURRENCE-ID
// overrides, preserving feed order so output is deterministic.
func (p *Provider) partition(cal *ical.Calendar) ([]*ical.VEvent, overrideIndex, []string) {
	var (
		masters      []*ical.VEvent
		overrides    = overrideIndex{}
		overrideUIDs []string
	)

	for _, ev := range cal.Events() {
		if !ev.HasProperty(ical.ComponentPropertyRecurrenceId) {
			masters = append(masters, ev)
			continue
		}

		uid := text(ev, ical.ComponentPropertyUniqueId)
		rid, err := ev.GetRecurrenceID()
		if err != nil {
			p.log.Warn("skipping calendar override with unreadable RECURRENCE-ID", "uid", uid, "error", err)
			continue
		}

		set := overrides[uid]
		if set == nil {
			set = &overrideSet{
				byRecurrenceID: map[int64]*ical.VEvent{},
				consumed:       map[int64]bool{},
			}
			overrides[uid] = set
			overrideUIDs = append(overrideUIDs, uid)
		}
		if _, dup := set.byRecurrenceID[rid.Unix()]; !dup {
			set.order = append(set.order, rid.Unix())
		}
		set.byRecurrenceID[rid.Unix()] = ev
	}

	return masters, overrides, overrideUIDs
}

// startOf reads DTSTART, interpreting a date-only value as local midnight
// the way the Google provider does.
func startOf(ev *ical.VEvent, isAllDay bool) (time.Time, error) {
	if isAllDay {
		return ev.GetAllDayStartAt()
	}
	return ev.GetStartAt()
}

// occurrences lists the instance starts of one master VEVENT that could
// overlap [from, to).
func (p *Provider) occurrences(ev *ical.VEvent, uid string, start time.Time, dur time.Duration, from, to time.Time) []time.Time {
	hasRRule := ev.HasProperty(ical.ComponentPropertyRrule)
	hasRDate := ev.HasProperty(ical.ComponentPropertyRdate)
	if !hasRRule && !hasRDate {
		return []time.Time{start}
	}

	set := rrule.Set{}
	set.DTStart(start)

	if hasRRule {
		// The raw property value goes straight to rrule-go: golang-ical's
		// GetRRules() parses an RRULE into a struct that generates no
		// occurrences, and a struct-to-struct conversion would only lose
		// fidelity. Multiple RRULEs are deprecated in RFC 5545, so the
		// first one wins.
		raw := ev.GetProperties(ical.ComponentPropertyRrule)[0].Value
		rule, err := parseRRule(raw, start)
		if err != nil {
			// Degrading to the single DTSTART occurrence keeps the
			// meeting visible; dropping the event would not.
			p.log.Warn("unusable recurrence rule, showing only the first occurrence",
				"uid", uid, "rrule", raw, "error", err)
			return []time.Time{start}
		}
		set.RRule(rule)
	} else {
		// rrule.Set.DTStart only records metadata: Set.Iterator builds
		// its generator list from the RRULE and the RDATEs, never from
		// dtstart. With no RRULE the first instance would therefore be
		// lost, but RFC 5545 makes DTSTART part of the recurrence set,
		// so add it as an RDATE - which also leaves EXDATE able to
		// remove it.
		set.RDate(start)
	}

	// GetRDates and GetExDates split comma-separated values and merge
	// repeated properties, which is the whole reason for this library.
	if rdates, err := ev.GetRDates(); err != nil {
		p.log.Warn("unreadable RDATE", "uid", uid, "error", err)
	} else {
		for _, t := range rdates {
			set.RDate(t)
		}
	}
	if exdates, err := ev.GetExDates(); err != nil {
		p.log.Warn("unreadable EXDATE", "uid", uid, "error", err)
	} else {
		for _, t := range exdates {
			set.ExDate(t)
		}
	}

	// Widened by the event duration so a meeting that started before
	// `from` and is still running is not lost: engine.NextMeeting prefers
	// an in-progress meeting, so dropping it would be a visible
	// regression.
	return set.Between(from.Add(-dur).Add(-time.Second), to, true)
}

func parseRRule(raw string, start time.Time) (*rrule.RRule, error) {
	opt, err := rrule.StrToROption(raw)
	if err != nil {
		return nil, err
	}
	opt.Dtstart = start
	return rrule.NewRRule(*opt)
}

// overlapping keeps only events that intersect [from, to). All-day events
// survive here and are dropped later by engine.Filter, which is the one
// place that decides they are never "the next meeting".
func overlapping(events []calendar.Event, from, to time.Time) []calendar.Event {
	out := events[:0]
	for _, ev := range events {
		if ev.Start.Before(to) && ev.End.After(from) {
			out = append(out, ev)
		}
	}
	return out
}
