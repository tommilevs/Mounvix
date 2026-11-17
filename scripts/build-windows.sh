#!/usr/bin/env bash
# Build a Windows test binary (for local browser QA of the panel).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO="${GO:-go}"
if ! command -v "$GO" >/dev/null 2>&1; then
  GO="$HOME/tools/go/bin/go"
fi
OUT="$ROOT/dist/server/win"
mkdir -p "$OUT"
cd "$ROOT/server"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 "$GO" build -trimpath \
  -ldflags "-s -w -X github.com/tommilevs/Mounvix/server/internal/version.Version=0.0.1" \
  -o "$OUT/mounvix-server.exe" ./cmd/mounvix-server
echo "built: $OUT/mounvix-server.exe"
