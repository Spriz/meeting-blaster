#!/usr/bin/env bash
# Everything the release workflow does to turn a checkout into publishable
# assets. It lives here rather than inside .github/workflows so it can be
# read, run and debugged without pushing a tag:
#
#   scripts/release.sh build     <version> <target> [dist-dir]
#   scripts/release.sh package   <version> <target> [dist-dir]
#   scripts/release.sh bundle    <version> <dist-dir> <binary> [binary...]
#   scripts/release.sh checksums [dist-dir]
#
# Targets: linux-amd64, darwin-arm64, darwin-amd64, windows-amd64. Builds
# are native - cgo means every target is built on its own runner - so the
# GOOS/GOARCH below are an assertion that the runner matches the target,
# not a cross-compile.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

die() {
  echo "error: $*" >&2
  exit 1
}

# Fills in the per-target build settings. Keeping them here rather than in
# the workflow matrix is what lets a local run produce the same artefact as
# a tagged one.
target_settings() {
  goflags=""
  ldflags=""
  binary="meeting-blaster"
  case "$1" in
    linux-amd64)
      goos=linux goarch=amd64 archive=tar.gz
      # Fyne's GLFW builds both X11 and Wayland backends unless told
      # otherwise, and the Wayland headers are not installed anywhere.
      goflags="-tags=x11"
      ;;
    darwin-arm64) goos=darwin goarch=arm64 archive=tar.gz ;;
    darwin-amd64) goos=darwin goarch=amd64 archive=tar.gz ;;
    windows-amd64)
      goos=windows goarch=amd64 archive=zip binary="meeting-blaster.exe"
      # Without this a console window sits behind the tray app for the
      # whole session. internal/console is what keeps the command line
      # flags printing anyway.
      ldflags="-H=windowsgui"
      ;;
    *) die "unknown target: $1" ;;
  esac
}

cmd_build() {
  [ "$#" -ge 2 ] || die "usage: $0 build <version> <target> [dist-dir]"
  local version="$1" target="$2" dist="${3:-dist}"
  target_settings "$target"

  mkdir -p "$dist"
  CGO_ENABLED=1 GOOS="$goos" GOARCH="$goarch" GOFLAGS="$goflags" \
    go build -trimpath \
    -ldflags "-s -w -X main.version=$version $ldflags" \
    -o "$dist/$binary" "$repo_root/cmd/meeting-blaster"
  echo "$dist/$binary"
}

cmd_package() {
  [ "$#" -ge 2 ] || die "usage: $0 package <version> <target> [dist-dir]"
  local version="$1" target="$2" dist="${3:-dist}"
  target_settings "$target"

  local name="meeting-blaster-$version-$target"
  rm -rf "${dist:?}/$name"
  mkdir -p "$dist/$name"
  cp "$dist/$binary" "$repo_root/LICENSE" "$repo_root/README.md" "$dist/$name/"

  # The systemd unit and desktop entry assume freedesktop conventions;
  # macOS ships MeetingBlaster.app instead and Windows has neither.
  if [ "$goos" = "linux" ]; then
    mkdir -p "$dist/$name/packaging"
    cp "$repo_root/packaging/meeting-blaster.desktop" \
      "$repo_root/packaging/meeting-blaster.service" "$dist/$name/packaging/"
  fi

  if [ "$archive" = "zip" ]; then
    (cd "$dist" && zip_dir "$name")
    echo "$dist/$name.zip"
  else
    tar -czf "$dist/$name.tar.gz" -C "$dist" "$name"
    echo "$dist/$name.tar.gz"
  fi
}

# Windows runners ship 7-Zip; a developer machine is likelier to have zip.
zip_dir() {
  local name="$1"
  rm -f "$name.zip"
  if command -v 7z > /dev/null 2>&1; then
    7z a -tzip "$name.zip" "$name" > /dev/null
  elif command -v zip > /dev/null 2>&1; then
    zip -qr "$name.zip" "$name"
  else
    die "need 7z or zip to package $name"
  fi
}

# Assembles MeetingBlaster.app around one or more binaries. Several are
# lipo'd into one universal executable, so a single bundle runs on both
# Apple Silicon and Intel.
cmd_bundle() {
  [ "$#" -ge 3 ] || die "usage: $0 bundle <version> <dist-dir> <binary> [binary...]"
  local version="$1" dist="$2"
  shift 2
  local app="$dist/MeetingBlaster.app"

  # CFBundleShortVersionString has to be a dotted number. Tags are
  # "v0.1.1", and a local build may pass something like "dev", which
  # Launch Services would reject.
  local plist_version="${version#v}"
  if ! [[ $plist_version =~ ^[0-9]+(\.[0-9]+)*$ ]]; then
    plist_version="0.0.0"
  fi

  rm -rf "$app"
  mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"

  # lipo -create accepts a single input, so one architecture needs no
  # special case here.
  lipo -create "$@" -output "$app/Contents/MacOS/meeting-blaster"
  # Artifact downloads lose the executable bit.
  chmod 0755 "$app/Contents/MacOS/meeting-blaster"

  sed "s/@VERSION@/$plist_version/g" \
    "$repo_root/packaging/macos/Info.plist" > "$app/Contents/Info.plist"

  # The source icon is 64x64, so every size here is a downscale or a copy.
  # Upscaling to the 512 and 1024 slices macOS would also take produces a
  # blurry icon, which is worse than letting macOS scale it on demand.
  local iconset
  iconset="$(mktemp -d)/AppIcon.iconset"
  mkdir -p "$iconset"
  sips -z 16 16 "$repo_root/assets/icon.png" --out "$iconset/icon_16x16.png" > /dev/null
  sips -z 32 32 "$repo_root/assets/icon.png" --out "$iconset/icon_16x16@2x.png" > /dev/null
  cp "$iconset/icon_16x16@2x.png" "$iconset/icon_32x32.png"
  cp "$repo_root/assets/icon.png" "$iconset/icon_32x32@2x.png"
  iconutil -c icns "$iconset" -o "$app/Contents/Resources/AppIcon.icns"

  # An unsigned bundle is refused outright on Apple Silicon, so ad-hoc
  # sign it. This is not notarisation: Gatekeeper still warns on first
  # launch.
  codesign --force --sign - --timestamp=none "$app" > /dev/null 2>&1 ||
    echo "warning: could not ad-hoc sign the bundle" >&2

  # ditto is the archiver that preserves bundle structure; a plain zip
  # mangles symlinks and extended attributes.
  #
  # The name deliberately carries no architecture token that ubi
  # recognises, which is how "mise use -g github:Spriz/meeting-blaster"
  # keeps picking the per-arch tarballs over this.
  (cd "$dist" && rm -f "MeetingBlaster-$version-macos-universal.zip" &&
    ditto -c -k --keepParent MeetingBlaster.app \
      "MeetingBlaster-$version-macos-universal.zip")
  echo "$dist/MeetingBlaster-$version-macos-universal.zip"
}

cmd_checksums() {
  local dist="${1:-dist}"
  cd "$dist"
  local sha=(sha256sum)
  command -v sha256sum > /dev/null 2>&1 || sha=(shasum -a 256)
  # shellcheck disable=SC2035 # bare globs keep the names in checksums.txt
  # matching the published asset names.
  "${sha[@]}" -- *.tar.gz *.zip > checksums.txt
  cat checksums.txt
}

case "${1:-}" in
  build | package | bundle | checksums)
    command="$1"
    shift
    "cmd_$command" "$@"
    ;;
  *)
    sed -n '2,14p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
    exit 1
    ;;
esac
