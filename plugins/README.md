# plugins/ — плагины Mounvix (этап M7)

Формат: `<id>/plugin.json` (манифест: метаданные, capabilities, menu, settings_schema, pages, jobs, hooks) + статические `pages/*.html` для декларативных плагинов или исполняемый файл для процесс-плагинов (JSON-RPC по unix-сокету). Каталог-реестр: `index.json`. Примеры: hello (декларативный), echo (процесс), rclone-backup.
