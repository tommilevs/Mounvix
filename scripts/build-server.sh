#!/usr/bin/env bash
# Cross-build mounvix-server for all Mounvix target architectures.
# Usage: VERSION=0.0.1 ./scripts/build-server.sh   (GO=... to override go binary)
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${VERSION:-0.0.1}"
GO="${GO:-go}"
OUT="$ROOT/dist/server"
mkdir -p "$OUT"
cd "$ROOT/server"

archs=(arm64 arm mipsle mips amd64 386)
for goarch in "${archs[@]}"; do
  dir="$OUT/linux_${goarch}"
  mkdir -p "$dir"
  GOOS=linux GOARCH="$goarch" CGO_ENABLED=0 "$GO" build -trimpath \
    -ldflags "-s -w -X github.com/tommilevs/Mounvix/server/internal/version.Version=${VERSION}" \
    -o "$dir/mounvix-server" ./cmd/mounvix-server
  echo "built linux/${goarch} -> ${dir}/mounvix-server"
done

( cd "$OUT" && sha256sum linux_*/mounvix-server > sha256sums.txt )
echo "== sha256sums =="
cat "$OUT/sha256sums.txt"
