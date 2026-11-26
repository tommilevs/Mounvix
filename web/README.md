# web/ — Web UI

Панель рендерится сервером через Go `html/template`; шаблоны сейчас находятся в `server/internal/app/panel.go`. Общие CSS/JS assets находятся в `server/internal/app/assets/` и встраиваются в серверный бинарник через `go:embed` из того же пакета. Сборка клиентских ресурсов и Node.js на устройстве не требуются.
