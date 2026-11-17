#!/usr/bin/env bash
# M0 acceptance: build from a fresh clone directory and smoke /health.
# Usage: ./scripts/qa-clean-clone.sh /mnt/c/.../temp-clone-dir
set -euo pipefail
C="${1:?usage: qa-clean-clone.sh <clone-path>}"
export PATH="$HOME/tools/go/bin:$PATH"
cd "$C"
echo "shebang-bytes:"
head -c 25 scripts/build-server.sh | od -c | head -1
VERSION="${VERSION:-0.0.1}" ./scripts/build-server.sh
echo -n "binaries: "
ls dist/server/linux_*/mounvix-server | wc -l
./dist/server/linux_amd64/mounvix-server -addr 127.0.0.1:48102 >/tmp/mvx-clean-smoke.log 2>&1 &
P=$!
sleep 2
curl -sS http://127.0.0.1:48102/health
echo ""
kill "$P" 2>/dev/null || true
echo "CLEAN CLONE ACCEPTANCE OK"
