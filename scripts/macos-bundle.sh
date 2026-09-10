#!/usr/bin/env bash
# Assembles MeetingBlaster.app around one or more meeting-blaster binaries.
# Several binaries are lipo'd into one universal executable, so a single
# bundle runs on both Apple Silicon and Intel.
#
#   scripts/macos-bundle.sh <version> <output-dir> <binary> [binary...]
#
# The release workflow calls this, and so can you: building the bundle
# locally is the only way to check it before a tag exists.
set -euo pipefail

if [ "$#" -lt 3 ]; then
  echo "usage: $0 <version> <output-dir> <binary> [binary...]" >&2
  exit 1
fi

version="$1"
out_dir="$2"
shift 2

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
app="$out_dir/MeetingBlaster.app"

# CFBundleShortVersionString has to be a dotted number. Tags are "v0.1.1",
# and a local build may pass something like "dev", which Launch Services
# would reject.
plist_version="${version#v}"
if ! [[ "$plist_version" =~ ^[0-9]+(\.[0-9]+)*$ ]]; then
  plist_version="0.0.0"
fi

rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"

# lipo -create accepts a single input, so one architecture works the same
# way as two and the local path does not need a special case.
lipo -create "$@" -output "$app/Contents/MacOS/meeting-blaster"
# Artifact downloads lose the executable bit.
chmod 0755 "$app/Contents/MacOS/meeting-blaster"

sed "s/@VERSION@/$plist_version/g" \
  "$repo_root/packaging/macos/Info.plist" > "$app/Contents/Info.plist"

# The source icon is 64x64, so every size here is a downscale or a copy.
# Upscaling to the 512 and 1024 slices macOS would also take produces a
# blurry icon, which is worse than letting macOS scale it on demand.
iconset="$(mktemp -d)/AppIcon.iconset"
mkdir -p "$iconset"
sips -z 16 16 "$repo_root/assets/icon.png" --out "$iconset/icon_16x16.png" >/dev/null
sips -z 32 32 "$repo_root/assets/icon.png" --out "$iconset/icon_16x16@2x.png" >/dev/null
cp "$iconset/icon_16x16@2x.png" "$iconset/icon_32x32.png"
cp "$repo_root/assets/icon.png" "$iconset/icon_32x32@2x.png"
iconutil -c icns "$iconset" -o "$app/Contents/Resources/AppIcon.icns"

# An unsigned bundle is refused outright on Apple Silicon, so ad-hoc sign
# it. This is not notarisation: Gatekeeper still warns on first launch.
codesign --force --sign - --timestamp=none "$app" >/dev/null 2>&1 ||
  echo "warning: could not ad-hoc sign the bundle" >&2

echo "$app"
