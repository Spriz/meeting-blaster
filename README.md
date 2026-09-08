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
- Full-screen alert you cannot miss, with one-click join
- Desktop notification at a separate, earlier lead time
- Join links detected for Meet, Zoom, Teams, Webex, Whereby, Jitsi, GoTo,
  BlueJeans, Chime, Around, Discord and Slack huddles
- Tokens stored in the OS keyring, never on disk

## Install

Requires [mise](https://mise.jdx.dev) and, on Ubuntu, X11 headers:

```sh
sudo apt install xorg-dev
git clone https://github.com/spriz/meeting-blaster
cd meeting-blaster
mise run build
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
| `poll_interval` | `2m` | how often the calendar is refetched |
| `calendar_ids` | `[]` | which calendars to watch; empty means all |
| `use_24_hour` | `true` | clock format |
| `title_max_len` | `30` | truncation for the tray label |
| `hide_declined` | `true` | skip meetings you declined |
| `join_browser` | `""` | override the browser for join links |

## Platform support

Linux is built and tested. macOS and Windows sources are present and compile
under their `GOOS`, but have not been run — treat them as unfinished.

On Windows the tray shows no text label (the OS has no such concept), so the
countdown appears in the hover tooltip and the full-screen alert does the
heavy lifting.

## Licence

MIT
