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
    ics/               iCalendar subscriptions (webcal / http(s) / .ics files)
    multi/             fans Provider out over several sources at once
  engine/              poll loop, "what is next", when to alert
  overlay/             the full-screen alert
  screens/             monitor enumeration and placement (X11)
  tray/                tray label + menu
  prefs/               settings window
  meetlink/            find join links in event text
  config/              settings and subscription URLs on disk
  notify/  browser/    per-OS shims
```

### Calendar subscriptions and providers

The current input is iCalendar subscriptions over `webcal://`, `http://`, or
`https://`, plus local `.ics` files. Google Calendar remains compatible as a
source of its private iCal feed; no Google API project or account sign-in is
part of this path.

Keep `calendar.Provider` provider-neutral. The `multi` wrapper fans out over
several sources and is retained even while ICS is the only concrete provider.
If another provider is added later, implement `calendar.Provider` in a sibling
package and add it to the `multi.Source` list in `main`.

`Event.MeetingURL` should use structured conference properties in the feed,
including `CONFERENCE` and `X-GOOGLE-CONFERENCE`, falling back to
`meetlink.Detect` on location and description.

Calendar IDs are namespaced by source (`ics:` for existing subscriptions) in
`internal/calendar/multi`. Preserve those IDs and watch selections; do not
derive a new user-facing identifier from a private feed URL.

Both `multi` and `ics` treat partial failure as success: `engine.poll`
discards the whole event set when the provider errors, so one dead feed must
not blank out a working source. Only when every source fails do they return an
error. Preserve that.

## Subscription data

Remote subscription URLs are bearer credentials: the path or query may be the
only thing protecting a private calendar. Keep them out of commits, screenshots,
issues, and logs. The URL is necessarily stored in `config.json` and supplied
when the subscription is added, but subscription lists and CLI confirmations
use the configured feed name, with a safe generic fallback, rather than showing
the full URL. Fetch errors and logs should likewise avoid the URL path and query.

When no subscriptions are configured, startup prints only subscription setup
guidance and exits nonzero. No account sign-in is involved.

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
each launch a copy. `internal/singleton` is the backstop, and its one real
requirement is that the lock dies with the process: an flock on a file in
`XDG_RUNTIME_DIR` on Unix, a named kernel mutex on Windows. A lock *file* is
the wrong primitive on Windows - nothing deletes it after a crash, and the
app then refuses to start for good, saying only that another copy is
running. `TestLockDiesWithTheProcessHoldingIt` kills a child that holds the
lock and then retakes it; keep it passing.

The systemd unit itself is Linux-only; macOS and Windows have no equivalent
yet.

## Platform support

Linux is the target that actually gets used. Every release publishes Linux,
macOS (arm64, amd64, and a universal `MeetingBlaster.app`) and Windows
binaries, each built natively by the matrix in
`.github/workflows/release.yml`, and the `native` job in `ci.yml` compiles
and tests macOS and Windows on every PR. If a native job fails, fix the
source; do not narrow the job back to a package subset.

Build flags, archive layout and bundle assembly belong in
`scripts/release.sh` (exposed as the `release:*` mise tasks), not in the
workflow. The workflow decides *what runs where*; the script decides what
an asset is, so a maintainer can reproduce one without pushing a tag. A
per-target difference added straight to the YAML is a difference nobody
can debug locally.

macOS has been run end to end from the bundle: tray, alert, auto-dismiss,
and the process still polling afterwards. Windows has never been run by
anyone. CI compiling and testing it is not the same thing, so do not
describe it as working.

Three per-OS traps, each already paid for once:

- GLFW forces `NSApplicationActivationPolicyRegular` (`cocoa_init.m`)
  whenever its menubar hint is set, which overrides `LSUIElement` and gives
  a menu-bar app a Dock icon. `respectBundleActivationPolicy` in
  `cmd/meeting-blaster` clears the hint, but only when the executable sits
  inside a `.app`: an unbundled binary has no plist to fall back on and
  would end up with no activation policy and no way to focus its windows.
- The Windows binary is linked with `-H=windowsgui`, so no console window
  sits behind the tray app. That also leaves the process with no standard
  handles, which is why `internal/console` attaches to the launching
  terminal. Without it every `fmt.Print` in `cmd/`, the setup guidance
  included, goes nowhere.
- `systray.SetTitle` is documented as Mac and Linux only. Windows has no
  text in the tray at all, so `setLabel` there folds the countdown into the
  tooltip (`internal/tray/setlabel_windows.go`). Any feature that assumes a
  readable tray label needs a Windows answer.

### Release assets

`mise use -g github:Spriz/meeting-blaster` resolves through ubi, which
filters assets by OS, then by architecture, and if more than one still
survives sorts by name and takes the first. It has to land on the per-arch
tarballs, which is why the bundle is published as
`MeetingBlaster-<tag>-macos-universal.zip`: "universal" is not an
architecture token ubi knows, so the bundle drops out before the tie-break.
Renaming it to anything containing `all`, `arm64` or `x86_64` would start
handing people a `.app` directory where they expect a binary.

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
