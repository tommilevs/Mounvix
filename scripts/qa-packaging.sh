#!/usr/bin/env bash
# M0 packaging QA: build ipk/deb/tar for amd64 and verify their structure.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${1:-${VERSION:-0.0.1}}"
cd "$ROOT"

echo "== ipk build =="
./scripts/make-ipk.sh amd64 "$VERSION"
echo "== ipk root =="
tar -tzf "dist/ipk/mounvix_${VERSION}_x64-3.2.ipk"
echo "== ipk control =="
IPK_CONTROL=$(tar -xzOf "dist/ipk/mounvix_${VERSION}_x64-3.2.ipk" ./control.tar.gz | tar -xzO ./control)
printf '%s\n' "$IPK_CONTROL"
printf '%s\n' "$IPK_CONTROL" | grep -qx 'License: AGPL-3.0-only' || { echo 'ipk is missing License: AGPL-3.0-only' >&2; exit 1; }
IPK_DATA_FILES=$(tar -xzOf "dist/ipk/mounvix_${VERSION}_x64-3.2.ipk" ./data.tar.gz | tar -tz)
printf '%s\n' "$IPK_DATA_FILES" | grep -Fqx './opt/share/doc/mounvix/LICENSE' || { echo 'ipk is missing /opt/share/doc/mounvix/LICENSE' >&2; exit 1; }
printf '%s\n' "$IPK_DATA_FILES" | grep -Fqx './opt/share/doc/mounvix/NOTICE' || { echo 'ipk is missing /opt/share/doc/mounvix/NOTICE' >&2; exit 1; }
echo "== deb build =="
./scripts/make-deb.sh amd64 "$VERSION"
dpkg-deb -I "dist/deb/mounvix-server_${VERSION}_amd64.deb" | sed -n '1,12p'
DEB_LICENSE=$(dpkg-deb -f "dist/deb/mounvix-server_${VERSION}_amd64.deb" License)
[ "$DEB_LICENSE" = "AGPL-3.0-only" ] || { echo "deb License=$DEB_LICENSE, expected AGPL-3.0-only" >&2; exit 1; }
DEB_FILES=$(dpkg-deb -c "dist/deb/mounvix-server_${VERSION}_amd64.deb")
printf '%s\n' "$DEB_FILES" | grep -Fq './usr/share/doc/mounvix-server/LICENSE' || { echo 'deb is missing its installed LICENSE' >&2; exit 1; }
printf '%s\n' "$DEB_FILES" | grep -Fq './usr/share/doc/mounvix-server/NOTICE' || { echo 'deb is missing its installed NOTICE' >&2; exit 1; }
echo "== tar build =="
./scripts/package-tar.sh amd64 "$VERSION"
tar -tzf "dist/tar/mounvix-server_${VERSION}_linux_amd64.tar.gz"
echo "== PACKAGING QA OK =="
