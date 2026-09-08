# Contributing

Thanks for taking an interest. This is a small project, so the process is
light.

## Getting set up

You need [mise](https://mise.jdx.dev), which pins the Go toolchain, plus X11
development headers for the GUI:

```sh
sudo apt install xorg-dev          # Debian/Ubuntu
git clone https://github.com/Spriz/meeting-blaster
cd meeting-blaster
mise run build
```

Fedora uses `sudo dnf install libX11-devel libXcursor-devel libXrandr-devel
libXinerama-devel libXi-devel mesa-libGL-devel`; Arch uses
`sudo pacman -S libx11 libxcursor libxrandr libxinerama libxi mesa`.

## Everyday commands

```sh
mise run build     # -> ./bin/meeting-blaster
mise run test      # go test ./...
mise run lint      # go vet + gofmt check
mise run alert     # preview the full-screen alert, no meeting required
mise run install   # install into ~/.local for real-world testing
mise run uninstall # and remove it again
```

A single package or a single test:

```sh
mise exec -- go test ./internal/engine/
mise exec -- go test ./internal/engine/ -run TestAlertFiresExactlyOnce -v
```

`mise run lint` and `mise run test` are what CI runs. Get both green before
opening a pull request.

## Things worth knowing before you change code

[AGENTS.md](AGENTS.md) is the architecture guide — it covers the threading
model, the `x11` build tag, and the display-scaling problem. It is written for
AI agents but it is the same briefing a human wants. Read it first; it will
save you an afternoon.

Three rules that are easy to break by accident:

- **The full-screen overlay is the point of the app.** A tray label is easy to
  miss. Anything that makes the alert less noticeable needs a good argument.
- **Fyne owns the main thread.** Anything touching the UI from a background
  goroutine must go through `fyne.Do`. The engine calls its callbacks from a
  goroutine, so this comes up constantly.
- **Alerts must fire exactly once.** `TestAlertFiresExactlyOnce` guards this.
  An alert that fires every second is worse than no alert at all.

## Pull requests

- One logical change per PR.
- Add a test when you fix a bug or add behaviour that can be tested without a
  screen. The pure logic — `meetlink`, `engine`, `tray/label` — is well covered
  and easy to extend.
- Match the surrounding style. `gofmt` is enforced; comments explain *why*,
  since the code already says what.
- Say what you actually verified. "Builds and tests pass" and "I ran it and
  watched the overlay fire" are different claims, and the second is worth more.

## Adding a calendar provider

Google is the only provider today, but the seam is deliberate. Implement
`calendar.Provider` in a sibling package of `internal/calendar/google` and wire
it up in `main`. Nothing under `internal/calendar` may import a concrete
provider.

Prefer structured conference data from the API over scraping text; fall back to
`meetlink.Detect` only when the provider offers nothing better.

## Platform support

Linux is the only platform that is built and tested. macOS and Windows sources
exist and compile under their `GOOS`, but have never been run. If you have one
of those machines, reports of what actually happens are genuinely useful —
that gap is the project's biggest unknown.

## Reporting bugs

Include your OS and desktop environment, whether you are on X11 or Wayland,
your calendar provider, and what `meeting-blaster -v` prints. For anything
involving the overlay, a screenshot or a description of what the screen did is
worth a lot.

Never paste the contents of `credentials.json` or an OAuth token into an issue.
