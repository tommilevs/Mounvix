#!/usr/bin/env bash
# Генерация opkg-фида из *.ipk: dist/feed/Packages(.gz) + копии пакетов.
# Использование: make-feed.sh <version> [ipk_dir] [feed_dir]
set -euo pipefail
VERSION="${1:?version required}"
IPKDIR="${2:-dist/ipk}"
FEED="${3:-dist/feed}"
mkdir -p "$FEED"
: > "$FEED/Packages"
count=0
for ipk in "$IPKDIR"/*_"$VERSION"_*.ipk; do
  [ -e "$ipk" ] || continue
  tmp="$(mktemp -d)"
  tar -xzf "$ipk" -C "$tmp"
  ctrl="$(tar -xOzf "$tmp/control.tar.gz" ./control)"
  name="$(printf '%s\n' "$ctrl" | awk -F': ' '/^Package:/{print $2; exit}')"
  arch="$(printf '%s\n' "$ctrl" | awk -F': ' '/^Architecture:/{print $2; exit}')"
  ver="$(printf '%s\n' "$ctrl" | awk -F': ' '/^Version:/{print $2; exit}')"
  depends="$(printf '%s\n' "$ctrl" | awk -F': ' '/^Depends:/{print $2; exit}')"
  desc="$(printf '%s\n' "$ctrl" | awk -F': ' '/^Description:/{print $2; exit}')"
  size="$(stat -c%s "$ipk")"
  sha="$(sha256sum "$ipk" | awk '{print $1}')"
  base="$(basename "$ipk")"
  cp "$ipk" "$FEED/$base"
  {
    echo "Package: $name"
    echo "Version: $ver"
    echo "Architecture: $arch"
    [ -n "$depends" ] && echo "Depends: $depends"
    echo "Filename: $base"
    echo "Size: $size"
    echo "SHA256sum: $sha"
    [ -n "$desc" ] && echo "Description: $desc"
    echo ""
  } >> "$FEED/Packages"
  count=$((count + 1))
  rm -rf "$tmp"
done
if [ "$count" -eq 0 ]; then
  echo "no ipk for version $VERSION in $IPKDIR" >&2
  exit 1
fi
gzip -9 -c "$FEED/Packages" > "$FEED/Packages.gz"
echo "feed: $FEED ($count packages)"
