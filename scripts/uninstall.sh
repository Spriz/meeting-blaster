#!/usr/bin/env bash
# Removes everything install.sh created. Settings and subscriptions are left
# alone; see the notes printed at the end for how to clear those too.
set -euo pipefail

BIN_DIR="${XDG_BIN_HOME:-$HOME/.local/bin}"
APP_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
ICON_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/64x64/apps"
AUTOSTART_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/autostart"

UNIT="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/meeting-blaster.service"
if [ -e "$UNIT" ]; then
  systemctl --user disable --now meeting-blaster.service 2>/dev/null || true
  rm -f "$UNIT"
  systemctl --user daemon-reload 2>/dev/null || true
  echo "Removed the background service"
fi

removed=0
for f in "$BIN_DIR/meeting-blaster" \
         "$APP_DIR/meeting-blaster.desktop" \
         "$ICON_DIR/meeting-blaster.png" \
         "$AUTOSTART_DIR/meeting-blaster.desktop"; do
  if [ -e "$f" ]; then
    rm -f "$f"
    echo "Removed $f"
    removed=1
  fi
done

[ "$removed" = "0" ] && echo "Nothing to remove."

command -v update-desktop-database >/dev/null 2>&1 &&
  update-desktop-database "$APP_DIR" 2>/dev/null || true

echo
echo "Left in place:"
echo "  ${XDG_CONFIG_HOME:-$HOME/.config}/meeting-blaster/  (settings and subscriptions)"
echo
echo "To remove those too:"
echo "  rm -rf ${XDG_CONFIG_HOME:-$HOME/.config}/meeting-blaster"
