#!/usr/bin/env bash
# Deploy Mounvix .ipk to an Entware/Keenetic router over SSH.
# Env: ROUTER_HOST ROUTER_PORT ROUTER_USER ROUTER_PASS IPK
set -euo pipefail
: "${ROUTER_HOST:?}" "${ROUTER_USER:?}" "${ROUTER_PASS:?}" "${IPK:?}"
PORT="${ROUTER_PORT:-22}"

rssh() {
  sshpass -p "$ROUTER_PASS" ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 -p "$PORT" "$ROUTER_USER@$ROUTER_HOST" "$@"
}

echo "== target =="
rssh 'uname -m; . /opt/etc/profile 2>/dev/null || true; opkg --version | head -n1; ls -d /opt 2>/dev/null || true'

REMOTE="/tmp/$(basename "$IPK")"
echo "== upload =="
sshpass -p "$ROUTER_PASS" scp -O -o StrictHostKeyChecking=no -P "$PORT" "$IPK" "$ROUTER_USER@$ROUTER_HOST:$REMOTE"

echo "== install =="
rssh "opkg install --force-reinstall '$REMOTE' 2>&1 | tail -n 6"

echo "== config =="
rssh 'mkdir -p /opt/var/mounvix/share /opt/var/mounvix/data'
cat <<'EOF' | rssh 'cat > /opt/etc/mounvix/config.json'
{
  "port": 48100,
  "data_dir": "/opt/var/mounvix/data",
  "lang": "ru",
  "roots": [
    {"name": "share", "path": "/opt/var/mounvix/share", "read_only": false}
  ]
}
EOF

echo "== service =="
rssh '/opt/etc/init.d/S99mounvix stop || true'
rssh '/opt/etc/init.d/S99mounvix start'
sleep 2
rssh '/opt/etc/init.d/S99mounvix status'
echo "== local health on router =="
rssh 'curl -sS -m 5 http://127.0.0.1:48100/health || echo NO_CURL_OR_FAIL'
echo ""
echo "ROUTER DEPLOY DONE"
