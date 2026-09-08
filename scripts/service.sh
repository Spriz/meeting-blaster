#!/usr/bin/env bash
# Manages the systemd user service, which is how meeting-blaster runs in the
# background: started with your desktop session, restarted if it crashes, and
# logged to the journal.
set -euo pipefail

UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
AUTOSTART="${XDG_CONFIG_HOME:-$HOME/.config}/autostart/meeting-blaster.desktop"
BIN="${XDG_BIN_HOME:-$HOME/.local/bin}/meeting-blaster"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

case "${1:-}" in
  enable)
    if [ ! -x "$BIN" ]; then
      echo "error: $BIN not found. Run 'mise run install' first." >&2
      exit 1
    fi

    mkdir -p "$UNIT_DIR"
    install -m 0644 "$repo_root/packaging/meeting-blaster.service" \
                    "$UNIT_DIR/meeting-blaster.service"

    # The autostart entry and the service would each launch a copy. The
    # service is the better of the two, so it wins.
    if [ -e "$AUTOSTART" ]; then
      rm -f "$AUTOSTART"
      echo "Removed the autostart entry; the service starts it instead."
    fi

    systemctl --user daemon-reload
    systemctl --user enable --now meeting-blaster.service
    echo
    systemctl --user --no-pager --lines=0 status meeting-blaster.service || true
    echo
    echo "Running. It will start again with your next login."
    echo "Logs:  journalctl --user -u meeting-blaster -f"
    ;;

  disable)
    systemctl --user disable --now meeting-blaster.service 2>/dev/null || true
    rm -f "$UNIT_DIR/meeting-blaster.service"
    systemctl --user daemon-reload
    echo "Service stopped and removed."
    ;;

  status)
    systemctl --user --no-pager status meeting-blaster.service || true
    ;;

  logs)
    exec journalctl --user -u meeting-blaster -f
    ;;

  restart)
    systemctl --user restart meeting-blaster.service
    echo "Restarted."
    ;;

  *)
    echo "usage: $(basename "$0") {enable|disable|status|logs|restart}" >&2
    exit 2
    ;;
esac
