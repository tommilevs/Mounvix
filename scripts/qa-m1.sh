#!/usr/bin/env bash
# M1 QA: deps, tests, live panel+WebDAV smoke (temp config, port 48111).
set -euo pipefail
export PATH="$HOME/tools/go/bin:$PATH"
ROOT=/mnt/c/workspace/dsh/Mounvix
cd "$ROOT/server"

echo "== go mod tidy =="
go mod tidy 2>&1 | tail -n 3
cat go.mod
echo "== go test =="
go test ./...

echo "== build amd64 =="
export VERSION=0.0.1
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X github.com/tommilevs/Mounvix/server/internal/version.Version=${VERSION}" \
  -o "$ROOT/dist/server/linux_amd64/mounvix-server" ./cmd/mounvix-server

TMP="$(mktemp -d)"
mkdir -p "$TMP/share" "$TMP/data"
cat > "$TMP/config.json" <<EOF
{"port":48111,"data_dir":"$TMP/data","lang":"ru","roots":[{"name":"share","path":"$TMP/share","read_only":false}]}
EOF

"$ROOT/dist/server/linux_amd64/mounvix-server" -addr 127.0.0.1:48111 -config "$TMP/config.json" >/tmp/mvx-m1.log 2>&1 &
PID=$!
sleep 1
trap 'kill $PID 2>/dev/null || true; rm -rf "$TMP"' EXIT

echo "== health =="
curl -sS http://127.0.0.1:48111/health; echo
echo "== login page =="
curl -sS -o /tmp/mvx-login.html -w 'login_code=%{http_code}\n' http://127.0.0.1:48111/login
grep -q 'Mounvix' /tmp/mvx-login.html && echo login_brand=yes
echo "== login flow =="
curl -sS -c /tmp/mvx.jar -o /dev/null -w 'post_login=%{http_code}\n' -d 'username=admin&password=1234567890' http://127.0.0.1:48111/login
curl -sS -b /tmp/mvx.jar -o /tmp/mvx-dash.html -w 'dash=%{http_code}\n' http://127.0.0.1:48111/
grep -q 'Обзор' /tmp/mvx-dash.html && echo dash_ru=yes
curl -sS -b /tmp/mvx.jar -o /dev/null -w 'users_page=%{http_code}\n' http://127.0.0.1:48111/users
echo "== webdav =="
curl -sS -u admin:1234567890 -X PROPFIND -H 'Depth: 0' -o /dev/null -w 'propfind=%{http_code}\n' http://127.0.0.1:48111/dav/share/
printf 'hello-webdav' | curl -sS -u admin:1234567890 -T - -o /dev/null -w 'put=%{http_code}\n' http://127.0.0.1:48111/dav/share/x.txt
echo -n 'get_body='
curl -sS -u admin:1234567890 http://127.0.0.1:48111/dav/share/x.txt; echo
echo "== M1 QA OK =="
