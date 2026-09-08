# Conventions

This file contains contributor guidance for Meeting Blaster. Follow it for all code changes.

## Product priorities

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

## Testing: red/green TDD

Use a full red/green/refactor TDD workflow for every feature and bug fix.
Fast, reliable releases depend on tests driving the implementation, not on
adding tests after the code is finished.

1. **Red:** Write a focused test for the desired observable behavior (or a
   regression test for the bug). Run it before changing production code and
   confirm it fails for the expected reason, not a setup or compilation error.
2. **Green:** Make the smallest production change that makes the test pass,
   then run the relevant package tests.
3. **Refactor:** Improve the code with tests staying green. Before declaring
   the change ready to release, run `mise run test` and `mise run lint`.

Aim for **100% test coverage across the codebase**, not just newly added code.
Cover meaningful behavior, boundaries, error paths, and state transitions;
do not inflate the number with assertion-free tests or implementation-detail
assertions. Treat uncovered code as a gap to close, and report any remaining
gaps explicitly rather than silently excluding packages or lowering the goal.

Measure Go statement coverage with:

```
mise exec -- go test -coverpkg=./... -coverprofile=/tmp/meeting-blaster-coverage.out ./...
mise exec -- go tool cover -func=/tmp/meeting-blaster-coverage.out
```

Coverage is a target, not proof of correctness. A local run only measures
code selected for that platform and its build tags; preserve native CI
coverage of the other platforms. UI changes still need verification on the
actual surface, especially the full-screen overlay.

## Architecture

Data flows one way: provider -> engine -> callbacks -> UI.

```
cmd/meeting-blaster    wiring, flags, thread ownership
internal/
  calendar/            Event, Provider - no provider-specific code
    google/            Google Calendar + OAuth
    ics/               iCalendar subscriptions (webcal / https / .ics files)
    multi/             fans Provider out over several sources at once
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

Fyne quits when its last window closes, including on Linux. Our externally
managed systray does not count as a Fyne window, so normal startup retains a
never-shown window to keep the event loop alive after alerts and preferences
close. It has no native window until shown; do not show or close it during
normal operation. Tray Quit and termination signals explicitly call
`ui.Quit()`. The one-shot `--test-alert` path deliberately has no keep-alive
window, so dismissing its overlay still exits.

`tray.Update` runs on the engine goroutine while `tray.Ready` runs on
systray's, so the menu items are published behind a mutex-guarded `ready`
flag and `Update` returns early before that. A local `.ics` feed polls in
microseconds, so the first `Update` really does arrive before the menu
exists; dereferencing it crashed the app before it showed an icon.

### Adding a calendar provider

Implement `calendar.Provider` in a sibling of `internal/calendar/google` and
add it to the `multi.Source` list in `main`. Nothing in `internal/calendar`
may import a concrete provider. `Event.MeetingURL` should come from
structured conference data where the API offers it, falling back to
`meetlink.Detect` on location and description.

Calendar IDs are namespaced by source (`google:`, `ics:`) in
`internal/calendar/multi`, so a new provider needs its own key beside
`config.SourceGoogle` / `config.SourceICS`, and `config.migrateCalendarIDs`
is what keeps an existing watch selection matching after the prefixes
changed.

Both `multi` and `ics` treat partial failure as success: `engine.poll`
discards the whole event set when the provider errors, so one dead feed or a
Google token needing re-auth must not blank out a working source. Only when
every source fails do they return an error. Preserve that.

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

Linux is the only target actually *run* here. macOS and Windows are compiled
and tested natively in CI (the `native` matrix job in `.github/workflows/ci.yml`
builds every package, including `cmd/`, `overlay`, `prefs` and `tray`, with
cgo enabled), but neither is a currently verified runtime target. Do not
describe them as working. If a native job fails, fix the source; do not
narrow the job back to a package subset.

A limited historical run on macOS surfaced that the overlay appeared and the
alert fired, but the process exited when the overlay closed (see Threading
above). This is only a historical observation, not support verification; do
not claim that macOS now works.

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
