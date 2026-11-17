# scripts/ — локальные сборки и QA

- `build-server.sh` — кросс-компиляция `mounvix-server` (linux: arm64, arm, mipsle, mips, amd64, 386) → `dist/server/`, sha256.
- `make-ipk.sh`, `make-deb.sh`, `package-tar.sh` — пакеты (M0/M1).
- `dev-server.sh`, `qa.sh` — запуск сервера и авто-тесты (M1+).
Требования: WSL/Ubuntu, Go (в этой среде `~/tools/go/bin/go`), bash.
