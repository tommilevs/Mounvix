#!/usr/bin/env bash
# Build a generic tar.gz (binary + systemd unit + config example).
# Usage: ./scripts/package-tar.sh <goarch> <version>
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GOARCH="${1:?usage: package-tar.sh <goarch> <version>}"
VERSION="${2:?usage: package-tar.sh <goarch> <version>}"
BIN="$ROOT/dist/server/linux_${GOARCH}/mounvix-server"
[ -x "$BIN" ] || { echo "missing $BIN (run scripts/build-server.sh)"; exit 1; }

W="$(mktemp -d)"
trap 'rm -rf "$W"' EXIT
mkdir -p "$W/mounvix"
cp "$BIN" "$W/mounvix/mounvix-server"
cp "$ROOT/packaging/systemd/mounvix.service" "$W/mounvix/mounvix.service"
printf '{"port":48100,"data_dir":"/var/lib/mounvix"}\n' > "$W/mounvix/config.example.json"
mkdir -p "$ROOT/dist/tar"
OUT="$ROOT/dist/tar/mounvix-server_${VERSION}_linux_${GOARCH}.tar.gz"
( cd "$W" && tar -czf "$OUT" ./mounvix )
echo "tar: $OUT"
