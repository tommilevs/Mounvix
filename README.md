# Mounvix — NAS в телефоне

Собственный (не клон) продукт: Android-приложение для работы с файлами и медиа на своих серверах + легкий серверный агент (Go) с Web-UI. Спецификация: [docs/specs/2026-11-17-mounvix-design.md](docs/specs/2026-11-17-mounvix-design.md).

## Структура
- `server/` — Go-сервер (health, Web-UI, WebDAV, API, бэкап, плагины) — этапы M1+
- `web/` — шаблоны/статика Web-UI (встраиваются в бинарник, M1)
- `android/` — Android-приложение (Kotlin/Compose, база FileBox, M2+)
- `packaging/` — init.d/systemd и сборка opkg/deb/tar.gz
- `plugins/` — манифест и примеры плагинов (M7)
- `scripts/` — локальные сборки/QA
- `docs/` — спецификация и планы

## Быстрый старт (WSL/Ubuntu)
```bash
export PATH="$HOME/tools/go/bin:$PATH"   # Go установлен локально в ~/tools/go
cd server && go test ./...               # тесты
VERSION=0.0.1 ./scripts/build-server.sh  # кросс-сборка 6 арок -> dist/server/
```
Проверка сервера:
```bash
./dist/server/linux_amd64/mounvix-server -addr 127.0.0.1:48100 &
curl -s http://127.0.0.1:48100/health
```

## Статус
M0 — скелет репозитория и сборок. Лицензия: GPL-3.0 (см. `LICENSE`, `NOTICE`).
