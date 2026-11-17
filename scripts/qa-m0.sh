#!/usr/bin/env bash
set -euo pipefail
export PATH="$HOME/tools/go/bin:$PATH"
ROOT=/mnt/c/workspace/dsh/Mounvix
cd "$ROOT/server"
echo "== go test =="
go test ./...
cd "$ROOT"
echo "== cross build =="
VERSION=0.0.1 ./scripts/build-server.sh
echo "== smoke =="
./dist/server/linux_amd64/mounvix-server -addr 127.0.0.1:48101 >/tmp/mvx.log 2>&1 &
PID=$!
sleep 1
curl -sS http://127.0.0.1:48101/health
echo ""
kill "$PID" 2>/dev/null || true
echo "== file checks =="
file dist/server/linux_mipsle/mounvix-server | sed 's/.*: //'
file dist/server/linux_arm64/mounvix-server | sed 's/.*: //'
ls dist/server