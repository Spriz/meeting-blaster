# Security Policy

## Reporting a vulnerability

Please report security issues privately through GitHub's
[private vulnerability reporting](https://github.com/Spriz/meeting-blaster/security/advisories/new)
rather than opening a public issue.

This is a hobby project maintained by one person, so please be patient with
response times.

## What this app handles

meeting-blaster reads events from iCalendar subscriptions:

- Remote `webcal://`, `http://`, and `https://` feed URLs.
- Local `.ics` files.

A private remote feed URL is a bearer credential. Anyone who obtains it may be
able to read the calendar, so treat it like a password: do not share it, put it
in screenshots or issues, or commit it to source control. The URL is necessarily
kept in the app's `config.json` and supplied as setup input. Subscription lists
and CLI confirmations identify a feed by its configured name, with a safe
generic fallback, and do not display the full URL. Remote fetch errors and logs
also avoid the URL path and query, which commonly contain the feed secret.

Google Calendar remains supported when used as the host for a private iCal feed
(its "Secret address in iCal format"). No Google API project or account sign-in
is required for subscriptions. The app only fetches the feed and does not
write to the source calendar.

Feed contents are provider-controlled. Published feeds may refresh only every
few hours; the app's `poll_interval` controls refetching, not how quickly the
provider regenerates a feed. A newly created or changed event may therefore be
absent until the provider publishes an updated feed.

## Scope notes

- Structured `CONFERENCE` and `X-GOOGLE-CONFERENCE` properties are supported,
  with common meeting links also detected in event text. Meeting links come
  from the feed and are opened in your browser.
- A malicious calendar invitation could therefore put an arbitrary URL in front
  of you — the same exposure any calendar client has. Links are opened, never
  fetched or executed.
