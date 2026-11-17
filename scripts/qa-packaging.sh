#!/usr/bin/env bash
# M0 packaging QA: build ipk/deb/tar for amd64 and verify their structure.
set -euo pipefail
ROOT=/mnt/c/workspace/dsh/Mounvix
VERSION=0.0.1
cd "$ROOT"

echo "== ipk build =="
./scripts/make-ipk.sh amd64 "$VERSION"
echo "== ipk root =="
tar -tzf "dist/ipk/mounvix_${VERSION}_x64-3.2.ipk"
echo "== ipk control =="
tar -xzOf "dist/ipk/mounvix_${VERSION}_x64-3.2.ipk" ./control.tar.gz | tar -xzO ./control
echo "== deb build =="
./scripts/make-deb.sh amd64 "$VERSION"
dpkg-deb -I "dist/deb/mounvix-server_${VERSION}_amd64.deb" | sed -n '1,12p'
echo "== tar build =="
./scripts/package-tar.sh amd64 "$VERSION"
tar -tzf "dist/tar/mounvix-server_${VERSION}_linux_amd64.tar.gz"
echo "== PACKAGING QA OK =="
