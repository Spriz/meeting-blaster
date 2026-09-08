// Package google implements calendar.Provider against the Google Calendar
// API v3.
package google

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/oauth2"
	calendarapi "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"

	"github.com/spriz/meeting-blaster/internal/calendar"
	"github.com/spriz/meeting-blaster/internal/meetlink"
)

// Account is the keyring key under which the Google token is stored. A
// single signed-in Google account is supported for now; adding more means
// making this a per-account value rather than a constant.
const Account = "google"

// TokenStore persists OAuth tokens. internal/tokens.Store satisfies it.
type TokenStore interface {
	Save(account string, tok *oauth2.Token) error
	Load(account string) (*oauth2.Token, error)
}

// Provider reads events from Google Calendar.
type Provider struct {
	svc *calendarapi.Service
}

// New builds a Provider from a stored token, refreshing it as needed and
// writing refreshed tokens back to the store.
func New(ctx context.Context, creds Credentials, store TokenStore) (*Provider, error) {
	tok, err := store.Load(Account)
	if err != nil {
		return nil, fmt.Errorf("load stored Google token: %w", err)
	}

	// RedirectURL is irrelevant for refresh, so a placeholder is fine.
	conf := oauthConfig(creds, "http://127.0.0.1")
	source := &savingSource{
		inner:   conf.TokenSource(ctx, tok),
		store:   store,
		account: Account,
		last:    tok,
	}

	svc, err := calendarapi.NewService(ctx, option.WithTokenSource(oauth2.ReuseTokenSource(nil, source)))
	if err != nil {
		return nil, fmt.Errorf("create calendar service: %w", err)
	}
	return &Provider{svc: svc}, nil
}

// Name implements calendar.Provider.
func (p *Provider) Name() string { return "Google Calendar" }

// Calendars implements calendar.Provider.
func (p *Provider) Calendars(ctx context.Context) ([]calendar.Calendar, error) {
	var out []calendar.Calendar

	call := p.svc.CalendarList.List().MaxResults(250).ShowHidden(false)
	err := call.Pages(ctx, func(page *calendarapi.CalendarList) error {
		for _, item := range page.Items {
			// Calendars the user cannot read produce permission errors
			// on every poll, so drop them at the source.
			if item.AccessRole == "freeBusyReader" || item.Deleted {
				continue
			}
			name := item.SummaryOverride
			if name == "" {
				name = item.Summary
			}
			out = append(out, calendar.Calendar{
				ID:      item.Id,
				Name:    name,
				Primary: item.Primary,
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list calendars: %w", err)
	}
	return out, nil
}

// Events implements calendar.Provider. It queries every calendar the account
// can see; filtering to the user's selection is the caller's job.
func (p *Provider) Events(ctx context.Context, from, to time.Time) ([]calendar.Event, error) {
	cals, err := p.Calendars(ctx)
	if err != nil {
		return nil, err
	}
	return p.EventsIn(ctx, cals, from, to)
}

// EventsIn fetches events from the given calendars only, which avoids
// re-listing calendars on every poll.
func (p *Provider) EventsIn(ctx context.Context, cals []calendar.Calendar, from, to time.Time) ([]calendar.Event, error) {
	var out []calendar.Event

	for _, cal := range cals {
		// SingleEvents expands recurring series into instances, which is
		// what "my next meeting" needs; without it a weekly standup is
		// one event with a recurrence rule we would have to expand.
		call := p.svc.Events.List(cal.ID).
			SingleEvents(true).
			OrderBy("startTime").
			TimeMin(from.Format(time.RFC3339)).
			TimeMax(to.Format(time.RFC3339)).
			MaxResults(250)

		err := call.Pages(ctx, func(page *calendarapi.Events) error {
			for _, item := range page.Items {
				ev, ok := convert(item, cal.ID)
				if ok {
					out = append(out, ev)
				}
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("list events in %q: %w", cal.Name, err)
		}
	}

	calendar.SortByStart(out)
	return out, nil
}

// convert maps an API event onto the domain model, reporting false for
// events that cannot be shown (cancelled, or with no usable time).
func convert(item *calendarapi.Event, calendarID string) (calendar.Event, bool) {
	if item.Status == "cancelled" || item.Start == nil || item.End == nil {
		return calendar.Event{}, false
	}

	start, allDay, err := parseTime(item.Start)
	if err != nil {
		return calendar.Event{}, false
	}
	end, _, err := parseTime(item.End)
	if err != nil {
		return calendar.Event{}, false
	}

	title := item.Summary
	if title == "" {
		title = "(no title)"
	}

	ev := calendar.Event{
		ID:         item.Id,
		CalendarID: calendarID,
		Title:      title,
		Start:      start,
		End:        end,
		AllDay:     allDay,
		Location:   item.Location,
		Notes:      item.Description,
		MeetingURL: joinURL(item),
		Declined:   declined(item),
	}
	return ev, true
}

// joinURL prefers Google's structured conference data, which is exact, and
// falls back to scanning text fields for a pasted link.
func joinURL(item *calendarapi.Event) string {
	if item.ConferenceData != nil {
		for _, ep := range item.ConferenceData.EntryPoints {
			if ep.EntryPointType == "video" && ep.Uri != "" {
				return ep.Uri
			}
		}
	}
	if item.HangoutLink != "" {
		return item.HangoutLink
	}
	return meetlink.Detect(item.Location, item.Description).URL
}

// declined reports whether the signed-in user responded "no". The API marks
// the user's own attendee record with Self.
func declined(item *calendarapi.Event) bool {
	for _, att := range item.Attendees {
		if att.Self && att.ResponseStatus == "declined" {
			return true
		}
	}
	return false
}

// parseTime handles the API's split representation: DateTime for timed
// events, Date for all-day ones.
func parseTime(t *calendarapi.EventDateTime) (when time.Time, allDay bool, err error) {
	if t.DateTime != "" {
		parsed, err := time.Parse(time.RFC3339, t.DateTime)
		if err != nil {
			return time.Time{}, false, fmt.Errorf("parse event time %q: %w", t.DateTime, err)
		}
		return parsed.Local(), false, nil
	}
	if t.Date != "" {
		// All-day events are floating dates; interpret them in local time
		// so "today" means the user's today.
		parsed, err := time.ParseInLocation("2006-01-02", t.Date, time.Local)
		if err != nil {
			return time.Time{}, false, fmt.Errorf("parse event date %q: %w", t.Date, err)
		}
		return parsed, true, nil
	}
	return time.Time{}, false, fmt.Errorf("event has neither dateTime nor date")
}

// savingSource writes the token back to the keyring whenever the underlying
// source mints a new one. Without this, a refreshed access token is lost on
// exit and long-lived sessions silently need a fresh sign-in.
type savingSource struct {
	inner   oauth2.TokenSource
	store   TokenStore
	account string
	last    *oauth2.Token
}

func (s *savingSource) Token() (*oauth2.Token, error) {
	tok, err := s.inner.Token()
	if err != nil {
		return nil, fmt.Errorf("refresh Google token (sign in again from the tray menu): %w", err)
	}

	if s.last == nil || tok.AccessToken != s.last.AccessToken {
		// A refresh response may omit the refresh token, meaning "keep
		// using the one you have"; carry it forward so it is not lost.
		if tok.RefreshToken == "" && s.last != nil {
			tok.RefreshToken = s.last.RefreshToken
		}
		if err := s.store.Save(s.account, tok); err != nil {
			// Saving is best effort: a working token in memory is more
			// useful than a hard failure here.
			logSaveFailure(err)
		}
		s.last = tok
	}
	return tok, nil
}

// logSaveFailure is a seam so the engine can route this to its logger
// without this package importing one.
var logSaveFailure = func(err error) {
	fmt.Println("meeting-blaster: warning: could not persist refreshed token:", strings.TrimSpace(err.Error()))
}
