# packaging/ — пакеты и сервисные скрипты

- `init.d/S99mounvix` — Entware init-скрипт (start/stop/restart, watchdog-обёртка обновления: код 75 — замена `.prev`).
- `systemd/mounvix.service` — unit для deb/Ubuntu.
- Сборка пакетов: `scripts/make-ipk.sh` (legacy gzip-tar, Entware-архитектуры), `scripts/make-deb.sh` (dpkg-deb), `scripts/package-tar.sh` (tar.gz с unit).
