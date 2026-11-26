#!/usr/bin/env bash
# Build a legacy Entware .ipk (gzip-tar: debian-binary + control.tar.gz + data.tar.gz).
# Usage: ./scripts/make-ipk.sh <goarch> <version>
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GOARCH="${1:?usage: make-ipk.sh <goarch> <version>}"
VERSION="${2:?usage: make-ipk.sh <goarch> <version>}"

declare -A ARCH=( [arm64]=aarch64-3.10 [arm]=armv7-3.2 [mipsle]=mipsel-3.4 [mips]=mips-3.4 [amd64]=x64-3.2 [386]=x86-2.6 )
A="${ARCH[$GOARCH]:-}"
[ -n "$A" ] || { echo "unknown goarch: $GOARCH"; exit 1; }

BIN="$ROOT/dist/server/linux_${GOARCH}/mounvix-server"
[ -x "$BIN" ] || { echo "missing $BIN (run scripts/build-server.sh)"; exit 1; }

W="$(mktemp -d)"
trap 'rm -rf "$W"' EXIT

mkdir -p "$W/data/opt/bin" "$W/data/opt/etc/init.d" "$W/data/opt/etc/mounvix" "$W/data/opt/share/doc/mounvix" "$W/data/opt/var/log" "$W/data/opt/var/run"
cp "$BIN" "$W/data/opt/bin/mounvix-server"
cp "$ROOT/packaging/init.d/S99mounvix" "$W/data/opt/etc/init.d/S99mounvix"
cp "$ROOT/LICENSE" "$ROOT/NOTICE" "$W/data/opt/share/doc/mounvix/"
chmod +x "$W/data/opt/bin/mounvix-server" "$W/data/opt/etc/init.d/S99mounvix"
chmod 644 "$W/data/opt/share/doc/mounvix/LICENSE" "$W/data/opt/share/doc/mounvix/NOTICE"
printf '{"port":48100,"data_dir":"/opt/var/mounvix"}\n' > "$W/data/opt/etc/mounvix/config.json"

mkdir -p "$W/control"
cat > "$W/control/control" <<EOF
Package: mounvix
Version: ${VERSION}
Depends: libc
Architecture: ${A}
Maintainer: tommilevs <tommilevs@gmail.com>
Section: utils
Priority: optional
License: AGPL-3.0-only
Conffiles: /opt/etc/mounvix/config.json
Description: Mounvix NAS server - lightweight NAS web UI and WebDAV
EOF
cat > "$W/control/prerm" <<'EOF'
#!/bin/sh
[ -n "$IPKG_INSTROOT" ] && exit 0
CFG=/opt/etc/mounvix/config.json
[ -f "$CFG" ] && cp -af "$CFG" "$CFG.bak-auto"
exit 0
EOF
cat > "$W/control/postinst" <<'EOF'
#!/bin/sh
[ -n "$IPKG_INSTROOT" ] && exit 0
CFG=/opt/etc/mounvix/config.json
B="$CFG.bak-auto"
if [ -f "$B" ] && [ "$(wc -c < "$CFG" 2>/dev/null || echo 0)" -lt "$(wc -c < "$B")" ]; then
  cp -af "$B" "$CFG"
fi
[ -x /opt/etc/init.d/S99mounvix ] && /opt/etc/init.d/S99mounvix restart
exit 0
EOF
chmod +x "$W/control/prerm" "$W/control/postinst"

( cd "$W/data" && tar -czf "$W/data.tar.gz" . )
( cd "$W/control" && tar -czf "$W/control.tar.gz" . )
printf '2.0\n' > "$W/debian-binary"
mkdir -p "$ROOT/dist/ipk"
OUT="$ROOT/dist/ipk/mounvix_${VERSION}_${A}.ipk"
( cd "$W" && tar -czf "$OUT" ./debian-binary ./control.tar.gz ./data.tar.gz )
echo "ipk: $OUT"
