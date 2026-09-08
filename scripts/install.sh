#!/usr/bin/env bash
# Installs meeting-blaster into the user's home - no root, no system paths.
# Everything lands under the XDG user directories so uninstalling is just
# removing the same four files.
set -euo pipefail

BIN_DIR="${XDG_BIN_HOME:-$HOME/.local/bin}"
APP_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
ICON_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/64x64/apps"
AUTOSTART_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/autostart"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [ ! -x "$repo_root/bin/meeting-blaster" ]; then
  echo "error: bin/meeting-blaster not found. Run 'mise run build' first." >&2
  exit 1
fi

mkdir -p "$BIN_DIR" "$APP_DIR" "$ICON_DIR"

install -m 0755 "$repo_root/bin/meeting-blaster" "$BIN_DIR/meeting-blaster"
install -m 0644 "$repo_root/assets/icon.png"     "$ICON_DIR/meeting-blaster.png"
install -m 0644 "$repo_root/packaging/meeting-blaster.desktop" \
                "$APP_DIR/meeting-blaster.desktop"

# Refresh the caches so the launcher entry and icon appear without a re-login.
command -v update-desktop-database >/dev/null 2>&1 &&
  update-desktop-database "$APP_DIR" 2>/dev/null || true
command -v gtk-update-icon-cache >/dev/null 2>&1 &&
  gtk-update-icon-cache -qtf "${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor" 2>/dev/null || true

echo "Installed:"
echo "  $BIN_DIR/meeting-blaster"
echo "  $APP_DIR/meeting-blaster.desktop"
echo "  $ICON_DIR/meeting-blaster.png"

if [ "${AUTOSTART:-0}" = "1" ]; then
  mkdir -p "$AUTOSTART_DIR"
  install -m 0644 "$repo_root/packaging/meeting-blaster.desktop" \
                  "$AUTOSTART_DIR/meeting-blaster.desktop"
  echo "  $AUTOSTART_DIR/meeting-blaster.desktop  (starts on login)"
fi

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo
     echo "note: $BIN_DIR is not on your PATH."
     echo "      Add it, or launch meeting-blaster from your applications menu." ;;
esac
