# meeting-blaster

Your next meeting in the system tray, and a full-screen alert before it
starts. A [MeetingBar](https://meetingbar.app) equivalent that runs on Linux.

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
- Full-screen alert you cannot miss, with one-click join — on the primary
  display, on every display at once, or wherever focus happens to be
- Desktop notification at a separate, earlier lead time
- Join links detected for Meet, Zoom, Teams, Webex, Whereby, Jitsi, GoTo,
  BlueJeans, Chime, Around, Discord and Slack huddles
- Tokens stored in the OS keyring, never on disk

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

Download the latest `linux-amd64` tarball from
[Releases](https://github.com/Spriz/meeting-blaster/releases), then:

```sh
tar -xzf meeting-blaster-*-linux-amd64.tar.gz
cd meeting-blaster-*-linux-amd64
install -m 0755 meeting-blaster ~/.local/bin/
```

Checksums are published alongside each release as `checksums.txt`.

### From source

Building needs a Go toolchain (managed by [mise](https://mise.jdx.dev)) and
the X11 development headers, because Fyne renders through GLFW:

```sh
sudo apt install xorg-dev          # Debian/Ubuntu
git clone https://github.com/Spriz/meeting-blaster
cd meeting-blaster
mise run build                     # -> ./bin/meeting-blaster
```

Fedora: `sudo dnf install libX11-devel libXcursor-devel libXrandr-devel
libXinerama-devel libXi-devel mesa-libGL-devel`.
Arch: `sudo pacman -S libx11 libxcursor libxrandr libxinerama libxi mesa`.

### Desktop integration

To get an applications-menu entry and icon rather than a bare binary:

```sh
mise run install              # -> ~/.local, no root needed
mise run install:autostart    # the same, plus start on login
mise run uninstall            # removes all of it again
```

This installs four files under your home directory and nothing else:

```
~/.local/bin/meeting-blaster
~/.local/share/applications/meeting-blaster.desktop
~/.local/share/icons/hicolor/64x64/apps/meeting-blaster.png
~/.config/autostart/meeting-blaster.desktop     (autostart only)
```

## Setup

meeting-blaster talks to Google directly, so it needs its own OAuth client.
This is a one-time setup and everything stays on your machine:

1. Create a project at <https://console.cloud.google.com/projectcreate>
2. Enable the [Google Calendar API](https://console.cloud.google.com/apis/library/calendar-json.googleapis.com)
3. Configure the OAuth consent screen as **External** and add yourself under
   **Test users**
4. Under **Credentials**, create an OAuth client ID of type **Desktop app**
5. Download the JSON to `~/.config/meeting-blaster/credentials.json`

Then:

```sh
./bin/meeting-blaster
```

The first run opens your browser to sign in. Read-only calendar scopes are
requested; the app never writes to your calendar.

## Usage

```sh
meeting-blaster                # run it
meeting-blaster --test-alert   # preview the full-screen alert
meeting-blaster --list-monitors # show connected displays
meeting-blaster --login        # sign in again
meeting-blaster --logout       # forget the stored token
meeting-blaster -v             # debug logging
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
| `calendar_ids` | `[]` | which calendars to watch; empty means all |
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

Linux is built and tested. macOS and Windows sources are present and compile
under their `GOOS`, but have not been run — treat them as unfinished.

On Windows the tray shows no text label (the OS has no such concept), so the
countdown appears in the hover tooltip and the full-screen alert does the
heavy lifting.

## Licence

MIT
