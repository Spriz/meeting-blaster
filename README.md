# meeting-blaster

Your next meeting in the system tray, and a full-screen alert before it
starts. A [MeetingBar](https://meetingbar.app) equivalent, built for Linux,
with macOS and Windows binaries in every release.

**[spriz.github.io/meeting-blaster](https://spriz.github.io/meeting-blaster/)**

```
┌ Top bar ────────────────┐
│ 📅 Standup in 12m       │
└─────────────────────────┘
   ╭─ click ────────────────╮
   │ ▸ Join Standup    9:30 │
   │   Design review  11:00 │
   │   1:1 w/ lead    14:00 │
   │ ───────────────────────│
   │   Refresh now          │
   │   Preferences…         │
   │   Quit                 │
   ╰────────────────────────╯
```

A minute before the meeting, the whole screen goes to this:

```
        MEETING STARTING
         Design review
             0:43
        11:00 – 11:30
     [ Join now ]  [ Dismiss ]
```

## Features

- Next meeting and a live countdown in the tray
- Today's remaining agenda in the menu, click any entry to join
- Events from webcal/http(s) and local .ics subscriptions, including Google
  Calendar's private iCal feeds, with shared events shown and alerted once
  without merging separate recurring occurrences
- Full-screen alert you cannot miss, with one-click join — on the primary
  display, on every display at once, or wherever focus happens to be
- Desktop notification at a separate, earlier lead time
- Join links detected for Meet, Zoom, Teams, Webex, Whereby, Jitsi, GoTo,
  BlueJeans, Chime, Around, Discord and Slack huddles

## Install

### With mise (no compiler needed)

```sh
mise use -g github:Spriz/meeting-blaster
```

This pulls a prebuilt binary from the latest
[release](https://github.com/Spriz/meeting-blaster/releases) and puts it on
your PATH. Nothing to build, no X11 headers required.

If mise reports `no versions found ... matching date filter`, it is holding
back a release that is newer than its `minimum_release_age` setting — a
supply-chain precaution, not a broken download. Either wait, or opt in
explicitly:

```sh
MISE_MINIMUM_RELEASE_AGE=0 mise use -g github:Spriz/meeting-blaster
```

Release archives carry GitHub build provenance, which mise verifies during
install.

### From a release archive

Every release publishes:

| Asset | For |
| --- | --- |
| `meeting-blaster-<tag>-linux-amd64.tar.gz` | Linux |
| `meeting-blaster-<tag>-darwin-arm64.tar.gz` | macOS, Apple Silicon |
| `meeting-blaster-<tag>-darwin-amd64.tar.gz` | macOS, Intel |
| `MeetingBlaster-<tag>-macos-universal.zip` | macOS, as an app bundle |
| `meeting-blaster-<tag>-windows-amd64.zip` | Windows |

Checksums are published alongside as `checksums.txt`, and every archive
carries GitHub build provenance.

Linux:

```sh
tar -xzf meeting-blaster-*-linux-amd64.tar.gz
cd meeting-blaster-*-linux-amd64
install -m 0755 meeting-blaster ~/.local/bin/
```

macOS. The bundle is the better of the two: it keeps the app out of the
Dock, the way a menu-bar app should be. Nothing here is signed by a paid
Apple developer account, so Gatekeeper refuses the first launch until the
quarantine flag is off:

```sh
unzip MeetingBlaster-*-macos-universal.zip
xattr -dr com.apple.quarantine MeetingBlaster.app
mv MeetingBlaster.app /Applications/
```

Windows. Unzip and run `meeting-blaster.exe`; SmartScreen will warn about
an unrecognised app, and **More info → Run anyway** gets past it. The
binary is windowless, so a command line flag like `--version` prints into
the terminal you started it from *after* the prompt has come back.

### From source

Building needs a Go toolchain (managed by [mise](https://mise.jdx.dev)) and,
on Linux, the X11 development headers, because Fyne renders through GLFW:

```sh
sudo apt install xorg-dev          # Debian/Ubuntu
git clone https://github.com/Spriz/meeting-blaster
cd meeting-blaster
mise run build                     # -> ./bin/meeting-blaster
```

Fedora: `sudo dnf install libX11-devel libXcursor-devel libXrandr-devel
libXinerama-devel libXi-devel mesa-libGL-devel`.
Arch: `sudo pacman -S libx11 libxcursor libxrandr libxinerama libxi mesa`.

macOS and Windows need no such headers, only a C compiler — the Xcode
command line tools or Mingw-w64 — and `GOFLAGS` without `-tags=x11`, which
is a Linux-only backend selector. `scripts/macos-bundle.sh <version> <dir>
<binary>` wraps a built binary in `MeetingBlaster.app`.

### Desktop integration

To get an applications-menu entry and icon rather than a bare binary:

```sh
mise run install              # -> ~/.local, no root needed
mise run install:autostart    # the same, plus start with your desktop session
mise run uninstall            # removes all of it again
```

This installs four files under your home directory and nothing else:

```
~/.local/bin/meeting-blaster
~/.local/share/applications/meeting-blaster.desktop
~/.local/share/icons/hicolor/64x64/apps/meeting-blaster.png
~/.config/autostart/meeting-blaster.desktop     (autostart only)
```

### Running it in the background

Rather than keeping a terminal open, run it as a systemd user service. It
starts with your desktop session, restarts if it crashes, and logs to the
journal:

```sh
mise run service:enable     # start now, and with each desktop session
mise run service:status     # is it running?
mise run logs               # follow its output
mise run service:disable    # stop and remove it
```

Enabling the service removes the autostart entry if you installed one, so the
app is not launched twice. If it does get started twice anyway, the second
copy exits with `meeting-blaster is already running` rather than putting a
second icon in your tray.

**This is Linux only.** systemd has no equivalent on macOS or Windows; those
platforms would need a launchd agent and a Startup entry respectively, and
neither is written yet. On those systems, launch the binary yourself.

## Setup

meeting-blaster currently reads iCalendar subscriptions only. There is no API
project or account sign-in to configure. If no subscriptions are configured,
starting the app prints only subscription setup guidance and exits with a nonzero status.

### iCalendar subscriptions

```sh
./bin/meeting-blaster --add-calendar "webcal://example.com/your-calendar.ics"
```

`--add-calendar` is the setup entrypoint. It also takes an `http://` or
`https://` feed URL, or a path to a local `.ics` file. Most calendar services
publish a private feed URL:

| Service | Where to find it |
| --- | --- |
| Google Calendar | Settings -> your calendar -> "Secret address in iCal format" |
| Outlook / M365 | Settings -> Calendar -> Shared calendars -> Publish |
| Nextcloud | Calendar -> ... -> Copy subscription link |

Google-hosted iCal feeds remain supported, including their
`X-GOOGLE-CONFERENCE` properties. Meet links and other common meeting links are
kept when events are read from a feed.

Treat a private feed URL like a password: anyone who has it may be able to read
the calendar. The URL is saved in `config.json` because the app needs it to
fetch the feed. Subscription lists and `--add-calendar` confirmations use the
configured feed name, with a safe generic fallback, and do not show the full
URL.

The catch: published feeds are refreshed by the provider on its own schedule,
often only every few hours, so a meeting added this morning may not appear
today. `poll_interval` cannot improve that — it controls how often the app
refetches the feed, not how often the provider regenerates it.

Subscriptions are also managed from **Preferences…** in the tray menu. Each configured feed has a safe name with **Show** and **Remove** controls; **Show** controls its meetings in the tray, agenda, and alerts. Add a feed below the list, then save to apply the selection immediately—no restart is needed.

## Usage

```sh
meeting-blaster                     # run with configured subscriptions
meeting-blaster --add-calendar URL  # subscribe to an iCalendar feed and exit
meeting-blaster --test-alert        # preview the full-screen alert
meeting-blaster --list-monitors     # show connected displays
meeting-blaster -v                  # debug logging
```

Settings live in `~/.config/meeting-blaster/config.json` and are editable from
**Preferences…** in the tray menu:

| Setting | Default | Meaning |
| --- | --- | --- |
| `alert_lead` | `1m` | how long before a meeting the full-screen alert fires |
| `notify_lead` | `5m` | desktop notification lead time; `0` disables |
| `overlay_timeout` | `0s` | auto-dismiss the overlay; `0` means never |
| `overlay_monitors` | `primary` | which displays the alert blocks: `primary`, `all`, or `active` |
| `poll_interval` | `2m` | how often the calendar is refetched |
| `calendar_ids` | `[]` | exact allowlist selected with **Show** in Preferences. Stable IDs remain namespaced by source, for example `ics:9f2a1c0b`. |
| `calendar_selection_explicit` | `false` | with empty `calendar_ids`, `false` keeps legacy all-calendars behavior; `true` selects none. |
| `ics_sources` | `[]` | iCalendar subscriptions; each is `{"id", "url", "name", "email"}`, and `--add-calendar` fills them in. Lists use the name, not the full URL. |
| `use_24_hour` | `true` | clock format |
| `title_max_len` | `30` | truncation for the tray label |
| `hide_declined` | `true` | skip meetings you declined |
| `join_browser` | `""` | override the browser for join links |

## Which screens the alert blocks

`overlay_monitors` decides where the alert appears:

| Value | Behaviour |
| --- | --- |
| `primary` | Always the primary display. Predictable — the alert is always in the same place. (default) |
| `all` | Every connected display, one window each. The hardest to ignore. |
| `active` | Wherever the window manager opens it, which is usually the focused display. |

`meeting-blaster --list-monitors` shows what is connected:

```
IDX  NAME         GEOMETRY
0    DP-3         3840x2160+2560+0     primary
1    DP-2         2560x1440+6400+336
2    HDMI-1       2560x1440+0+256
```

Monitor targeting is implemented for X11 (including XWayland). Elsewhere the
alert still appears, but always as a single window placed by the window
manager, as though `active` were set.

## Platform support

Linux is the platform this is built for and used on. macOS and Windows are
compiled and tested natively in CI, and macOS has been run end to end - the
tray, the alert, and the app surviving the alert closing. Nobody has used
it on Windows yet.

On Windows the tray shows no text label (the OS has no such concept), so the
countdown appears in the hover tooltip and the full-screen alert does the
heavy lifting.

Starting with your session is Linux-only. The `.desktop` entry, the icon
install, and the systemd service all assume freedesktop conventions; macOS
would need a launchd agent and Windows a Startup or Task Scheduler entry.
Neither is written. `MeetingBlaster.app` does at least make the app a
normal macOS install rather than a loose binary.

## Contributing

Read [CONVENTIONS.md](CONVENTIONS.md) before making changes. It covers the
development workflow, red/green TDD and 100% coverage goal, and architectural
rules to preserve.

## Licence

MIT
