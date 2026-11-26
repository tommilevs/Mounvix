#!/usr/bin/env bash
# Build a .deb package for Debian/Ubuntu.
# Usage: ./scripts/make-deb.sh <goarch> <version>
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GOARCH="${1:?usage: make-deb.sh <goarch> <version>}"
VERSION="${2:?usage: make-deb.sh <goarch> <version>}"

declare -A ARCH=( [amd64]=amd64 [arm64]=arm64 [arm]=armhf [386]=i386 [mipsle]=mipsel [mips]=mips )
D="${ARCH[$GOARCH]:-}"
[ -n "$D" ] || { echo "unknown goarch: $GOARCH"; exit 1; }

BIN="$ROOT/dist/server/linux_${GOARCH}/mounvix-server"
[ -x "$BIN" ] || { echo "missing $BIN (run scripts/build-server.sh)"; exit 1; }

W="$(mktemp -d)"
trap 'rm -rf "$W"' EXIT

mkdir -p "$W/DEBIAN" "$W/usr/bin" "$W/etc/mounvix" "$W/lib/systemd/system" "$W/usr/share/doc/mounvix-server" "$W/var/lib/mounvix"
cp "$BIN" "$W/usr/bin/mounvix-server"
chmod 755 "$W/usr/bin/mounvix-server"
printf '{"port":48100,"data_dir":"/var/lib/mounvix"}\n' > "$W/etc/mounvix/config.json"
cp "$ROOT/packaging/systemd/mounvix.service" "$W/lib/systemd/system/mounvix.service"
cp "$ROOT/LICENSE" "$ROOT/NOTICE" "$W/usr/share/doc/mounvix-server/"
chmod 644 "$W/usr/share/doc/mounvix-server/LICENSE" "$W/usr/share/doc/mounvix-server/NOTICE"

cat > "$W/DEBIAN/control" <<EOF
Package: mounvix-server
Version: ${VERSION}
Section: utils
Priority: optional
License: AGPL-3.0-only
Architecture: ${D}
Maintainer: tommilevs <tommilevs@gmail.com>
Description: Mounvix NAS server - lightweight NAS web UI and WebDAV
EOF
cat > "$W/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload || true
  systemctl enable mounvix.service || true
  systemctl restart mounvix.service || true
fi
exit 0
EOF
cat > "$W/DEBIAN/prerm" <<'EOF'
#!/bin/sh
set -e
if command -v systemctl >/dev/null 2>&1; then
  systemctl stop mounvix.service 2>/dev/null || true
  systemctl disable mounvix.service 2>/dev/null || true
fi
exit 0
EOF
chmod 755 "$W/DEBIAN/postinst" "$W/DEBIAN/prerm"

mkdir -p "$ROOT/dist/deb"
OUT="$ROOT/dist/deb/mounvix-server_${VERSION}_${D}.deb"
dpkg-deb --build --root-owner-group "$W" "$OUT"
echo "deb: $OUT"
