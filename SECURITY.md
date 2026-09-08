# Security Policy

## Reporting a vulnerability

Please report security issues privately through GitHub's
[private vulnerability reporting](https://github.com/Spriz/meeting-blaster/security/advisories/new)
rather than opening a public issue.

This is a hobby project maintained by one person, so please be patient with
response times.

## What this app handles

meeting-blaster reads your calendar. That means it touches two sensitive things:

- **OAuth tokens.** Stored in the operating system's credential store —
  Secret Service on Linux, Keychain on macOS, Credential Manager on Windows —
  via `internal/tokens`. Tokens are never written to a file or logged.
- **Your OAuth client** (`credentials.json`). Kept in your XDG config
  directory and gitignored. For an installed-application client this is not
  confidential in the cryptographic sense — such a client ships inside every
  copy of a distributed binary, and PKCE rather than the secret is what
  protects the exchange — but it is account-specific and should not be shared.

Calendar scopes requested are read-only. The app never writes to your
calendar.

## Scope notes

- The OAuth flow uses PKCE with a loopback redirect on a kernel-assigned port,
  and verifies the `state` parameter on the callback.
- Meeting join links come from your calendar and are opened in your browser.
  A malicious calendar invitation could therefore put an arbitrary URL in front
  of you — the same exposure any calendar client has. Links are opened, never
  fetched or executed.
