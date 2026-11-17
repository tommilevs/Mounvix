#!/usr/bin/env bash
# Deploy Mounvix .deb into a fresh Ubuntu LXC on a Proxmox host.
# Env: PVE_HOST PVE_PASS DEB [CTID] [CT_IP] [PVE_USER] [PVE_PORT]
set -euo pipefail
: "${PVE_HOST:?}" "${PVE_PASS:?}" "${DEB:?}"
PVE_USER="${PVE_USER:-root}"
PVE_PORT="${PVE_PORT:-22}"
CTID="${CTID:-25061}"
CT_IP="${CT_IP:-192.168.165.240}"

ssh_pve() { sshpass -p "$PVE_PASS" ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 -p "$PVE_PORT" "$PVE_USER@$PVE_HOST" "$@"; }
scp_pve() { sshpass -p "$PVE_PASS" scp -o StrictHostKeyChecking=no -P "$PVE_PORT" "$@"; }

echo "== template =="
TPL=$(ssh_pve "pveam list local 2>/dev/null | awk '/ubuntu-24.04/ {print \$1; exit}'" || true)
if [ -z "${TPL:-}" ]; then
  echo "no ubuntu-24.04 template, updating list..."
  ssh_pve 'pveam update >/dev/null 2>&1 || true'
  AVAIL=$(ssh_pve "pveam available 2>/dev/null | awk '/ubuntu-24.04/ {print \$2; exit}'" || true)
  if [ -z "${AVAIL:-}" ]; then
    echo "NO_AVAILABLE_UBUNTU_2404_TEMPLATE"
    ssh_pve "pveam available 2>/dev/null | grep -i ubuntu | head -5 || true"
    exit 2
  fi
  echo "download: $AVAIL"
  ssh_pve "pveam download local '$AVAIL' 2>&1 | tail -n 3"
  TPL="local:vztmpl/$AVAIL"
fi
echo "TPL=$TPL"

echo "== ct create =="
ssh_pve "pct status $CTID >/dev/null 2>&1 || pct create $CTID '$TPL' --hostname mounvix-qa --cores 2 --memory 1024 --swap 512 --rootfs local-lvm:4 --net0 name=eth0,bridge=vmbr0,ip=${CT_IP}/24,gw=192.168.165.1 --unprivileged 1 --features nesting=1 --onboot 0 2>&1 | tail -n 4"
ssh_pve "pct start $CTID 2>&1 || true"
for i in 1 2 3 4 5 6 7 8 9 10; do
  if ssh_pve "pct exec $CTID -- sh -c 'true' 2>/dev/null"; then break; fi
  sleep 3
done
echo "== guest =="
ssh_pve "pct exec $CTID -- sh -c 'hostname; . /etc/os-release; echo \$PRETTY_NAME'"

echo "== push deb =="
scp_pve "$DEB" "$PVE_USER@$PVE_HOST:/tmp/mounvix-server.deb"
ssh_pve "pct push $CTID /tmp/mounvix-server.deb /tmp/mounvix-server.deb"
echo "== install =="
ssh_pve "pct exec $CTID -- sh -c 'dpkg -i /tmp/mounvix-server.deb 2>&1 | tail -n 3'"

echo "== config =="
CFG_JSON="{\"port\":48100,\"data_dir\":\"/var/lib/mounvix/data\",\"lang\":\"ru\",\"roots\":[{\"name\":\"share\",\"path\":\"/var/lib/mounvix/share\",\"read_only\":false}]}"
CFG_B64=$(printf '%s' "$CFG_JSON" | base64 -w0)
ssh_pve "pct exec $CTID -- sh -c 'mkdir -p /var/lib/mounvix/share /var/lib/mounvix/data && echo $CFG_B64 | base64 -d > /etc/mounvix/config.json && systemctl restart mounvix.service && sleep 1 && systemctl status mounvix.service --no-pager | head -n 4'"

echo "== health (from PVE) =="
ssh_pve "pct exec $CTID -- sh -c 'curl -sS -m 5 http://127.0.0.1:48100/health || echo NO_HEALTH'"
echo ""
echo "LXC DEPLOY DONE ct=$CTID ip=$CT_IP"
