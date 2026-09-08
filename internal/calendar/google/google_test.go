package google

import (
	"slices"
	"testing"

	calendarapi "google.golang.org/api/calendar/v3"

	"github.com/spriz/meeting-blaster/internal/calendar"
	"github.com/spriz/meeting-blaster/internal/config"
	"github.com/spriz/meeting-blaster/internal/engine"
)

func googleTimedEvent(id, original string) *calendarapi.Event {
	return &calendarapi.Event{
		Id:                id,
		ICalUID:           "shared@example.com",
		RecurringEventId:  "series-id",
		OriginalStartTime: &calendarapi.EventDateTime{DateTime: original},
		Start:             &calendarapi.EventDateTime{DateTime: "2026-06-10T09:00:00Z"},
		End:               &calendarapi.EventDateTime{DateTime: "2026-06-10T09:30:00Z"},
	}
}

func TestSharedGoogleOccurrencesKeepSeparateMeetings(t *testing.T) {
	// Two occurrences moved to the same time are still two meetings, while
	// calendar copies expressed in different timezones must collapse.
	items := []*calendarapi.Event{
		googleTimedEvent("first", "2026-06-08T09:00:00+02:00"),
		googleTimedEvent("first-copy", "2026-06-08T07:00:00Z"),
		googleTimedEvent("second", "2026-06-09T09:00:00+02:00"),
		googleTimedEvent("single", ""),
		googleTimedEvent("single-copy", ""),
	}
	for _, item := range items[3:] {
		item.ICalUID = "single@example.com"
		item.RecurringEventId = ""
		item.OriginalStartTime = nil
	}

	var events []calendar.Event
	for i, item := range items {
		cal := "a"
		if i == 1 || i == 4 {
			cal = "b"
		}
		ev, ok := convert(item, cal)
		if !ok {
			t.Fatalf("convert rejected %s", item.Id)
		}
		events = append(events, ev)
	}
	var got []string
	for _, ev := range engine.Filter(events, config.Default()) {
		got = append(got, ev.ID)
	}
	if want := []string{"first", "second", "single"}; !slices.Equal(got, want) {
		t.Fatalf("agenda = %v, want %v", got, want)
	}
}

func TestUnidentifiedGoogleOccurrencesRemainSeparate(t *testing.T) {
	missing := googleTimedEvent("missing", "")
	missing.OriginalStartTime = nil
	invalid := googleTimedEvent("invalid", "not-a-time")
	valid := googleTimedEvent("valid", "2026-06-08T07:00:00Z")

	var events []calendar.Event
	for _, item := range []*calendarapi.Event{missing, invalid, valid} {
		ev, ok := convert(item, "calendar")
		if !ok {
			t.Fatalf("usable event %s was dropped because of its occurrence identity", item.Id)
		}
		events = append(events, ev)
	}
	var got []string
	for _, ev := range engine.Filter(events, config.Default()) {
		got = append(got, ev.ID)
	}
	if want := []string{"missing", "invalid", "valid"}; !slices.Equal(got, want) {
		t.Fatalf("agenda = %v, want unidentified occurrences retained separately: %v", got, want)
	}
}
