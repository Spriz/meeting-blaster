# AGENTS.md

Guidance for AI coding agents working in this repository. `CLAUDE.md` is a
symlink to this file, so Claude Code reads the same instructions.

## What this is

A cross-platform MeetingBar (meetingbar.app) equivalent: your next meeting in
the system tray, and a full-screen alert before it starts.

The full-screen overlay is the point of the app, not a secondary feature. A
tray label is easy to miss; the overlay is the thing that stops you missing a
meeting. Weigh changes to `internal/overlay` accordingly.

## Toolchain

`mise` owns the toolchain; the Go version is pinned in `mise.toml`. Prefix Go
commands with `mise exec --`, or use the tasks:

```
mise run build      # -> ./bin/meeting-blaster
mise run test       # go test ./...
mise run lint       # go vet + gofmt check
mise run alert      # show a sample overlay without waiting for a meeting
mise run login      # re-run the Google OAuth flow
```

Single package or test:

```
mise exec -- go test ./internal/engine/
mise exec -- go test ./internal/engine/ -run TestAlertFiresExactlyOnce -v
```

### The x11 build tag is required on Linux

`mise.toml` sets `GOFLAGS=-tags=x11`. Without it, Fyne's GLFW compiles both
X11 and Wayland backends and the build fails on missing
`wayland-client-core.h`. Building X11-only also means the overlay goes through
XWayland, which lets a window raise itself to the top - Wayland restricts
that, which would undermine the whole point of the alert.

Build dependency on Ubuntu: `sudo apt install xorg-dev`.

## Architecture

Data flows one way: provider -> engine -> callbacks -> UI.

```
cmd/meeting-blaster    wiring, flags, thread ownership
internal/
  calendar/            Event, Provider - no provider-specific code
    google/            Google Calendar + OAuth (the only provider today)
  engine/              poll loop, "what is next", when to alert
  overlay/             the full-screen alert
  screens/             monitor enumeration and placement (X11)
  tray/                tray label + menu
  prefs/               settings window
  meetlink/            find join links in event text
  config/  tokens/     settings on disk, tokens in the OS keyring
  notify/  browser/    per-OS shims
```

### Threading, which is the easiest thing to get wrong here

Fyne owns the main thread and `ui.Run()` blocks it. systray therefore runs via
`systray.RunWithExternalLoop`, not `systray.Run`.

The engine calls its callbacks from a background goroutine. Anything touching
Fyne must go through `fyne.Do`. `overlay` and `prefs` already wrap their own
public methods, so they are safe to call from anywhere; new UI code must do
the same.

`ui.Run()` keeps running with zero windows open, so no hidden keep-alive
window is needed.

### Adding a calendar provider

Implement `calendar.Provider` in a sibling of `internal/calendar/google` and
construct it in `main`. Nothing in `internal/calendar` may import a concrete
provider. `Event.MeetingURL` should come from structured conference data where
the API offers it, falling back to `meetlink.Detect` on location and
description.

### Alerting rules worth preserving

- `engine.Due` deliberately returns false once a meeting has started;
  otherwise every event still on screen at launch would alert.
- The engine tracks fired alerts per `(event, start, kind)` so an alert fires
  exactly once even though `tick` re-evaluates every second.
  `TestAlertFiresExactlyOnce` guards this - keep it passing.
- `Filter` drops all-day events; they are never "the next meeting".

## Running in the background

`packaging/meeting-blaster.service` is a systemd **user** unit, wanted by
`graphical-session.target` - the app needs a display and the session D-Bus,
both of which are already in the systemd user environment on GNOME. It is
installed and controlled through `scripts/service.sh`.

Enabling the service deletes the autostart `.desktop`, because the two would
each launch a copy. `internal/singleton` is the backstop: an flock on a file
in `XDG_RUNTIME_DIR`, released by the kernel when the process dies, so a
crash cannot strand a stale lock. Without it, two copies means two tray icons
and two full-screen alerts.

None of this exists for macOS or Windows.

## Platform support

Linux is the only target built and tested here. macOS and Windows code is
written and vets under `GOOS`, but has never been run - do not describe it as
working.

The known asymmetry: `systray.SetTitle` is documented as Mac and Linux only.
Windows has no text in the tray at all, so `setLabel` there folds the
countdown into the tooltip (`internal/tray/setlabel_windows.go`). Any feature
that assumes a readable tray label needs a Windows answer.

## Monitor targeting

Fyne has no monitor enumeration and no window positioning whatsoever;
`CenterOnScreen` only centres on the monitor a window already occupies. So
`internal/screens` goes underneath Fyne to X11 via `github.com/jezek/xgb`
(pure Go, no CGO).

Placement uses the EWMH `_NET_WM_FULLSCREEN_MONITORS` client message, not
window geometry. A compositing window manager owns the geometry of fullscreen
windows and will simply undo a direct move.

`config.MonitorsAll` opens one window per monitor. Each window is located
after creation by diffing the X11 window list for the title
`overlay.windowTitle`, so that constant must stay stable and unique.

Every failure path degrades to a single window placed by the window manager:
an alert in the wrong place still beats no alert. `screens` returns
`ErrUnsupported` on non-Linux, which is the same path.

## Display scaling

Fyne reports a scale of 1 under XWayland even on a HiDPI panel. The overlay
therefore sizes its type as a fraction of measured screen height
(`internal/overlay/scale.go`) rather than in fixed pixels, and scales the
widget theme to match. Hard-coded `TextSize` values in the overlay will render
too small on a 4K display to serve their purpose.

## Credentials

`credentials.json` (the Google OAuth client) lives in the XDG config dir and
is gitignored. OAuth tokens go to the OS keyring via `internal/tokens`, never
to disk. Running with no credentials prints setup instructions and exits 1 -
that path returns `errSetupPrinted` so `main` does not log a duplicate error.
