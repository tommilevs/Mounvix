# Mounvix M7 — процессные плагины, каталог и журнал заданий

**Дата:** 2026-11-19 (Europe/Moscow)
**Статус:** проект для проверки владельцем; реализация не начата.
**Зависимости:** Android Advanced Mode потребляет контракт inbox, описанный здесь и в отдельной mobile-спецификации.

## 1. Цель и границы

Довести существующий каркас M7 до управляемой системы доверенных process-плагинов: каталог и состояние в веб-панели, проверка совместимости/целостности пакетов, запуск заданий через JSON-RPC 2.0 по Unix-сокету, логи, ограниченный lifecycle, только уже настроенные rclone remotes и сохраняемый журнал исходов заданий.

Входит: manifest/runtime/catalog, серверные административные API/UI, уведомления об исходе plugin/rclone jobs и WebSocket-события для открытого клиента. Не входит: WASM, произвольная загрузка кода из любого URL, внешний push relay/FCM, управление OAuth/rclone-токенами из Mounvix, неограниченные долгоживущие plugin-daemons.

## 2. Текущее состояние и ограничения платформы

- `plugins.Registry` сейчас читает плоские `DataDir/plugins/*.json`, сохраняет список включённых ID и регистрирует встроенный `rclone`; process-процессы и каталог отсутствуют.
- Admin-страница и `/api/v1/plugins` существуют. Текущий rclone helper запускает `rclone` без shell с 10-минутным timeout, но UI передаёт свободные строки `src/dst`.
- Спецификация §22 задаёт директории плагинов, `plugin.json`, capabilities/pages/jobs/hooks/endpoints и `plugins/index.json` с `id/name/version/min_app/arch/assets{ipk,deb,sha256}/home`.
- Source-controlled Debian systemd unit запускает Mounvix от `root`; для Entware также нельзя считать, что доступен непривилегированный runtime. Дочерний process-плагин получает права Mounvix. Отдельный процесс и iframe **не являются OS sandbox**. M7 v1 предназначен только для доверенного кода, устанавливаемого администратором; пользователь должен видеть явное предупреждение о полном доступе плагина к полномочиям сервиса. Не поддерживать непроверенные сторонние плагины до отдельного дизайна privilege separation/sandbox.

## 3. Структура данных и manifest

- Пакет плагина содержит `<id>/plugin.json`, исполняемый файл и необязательные статические страницы. Системная установка остаётся в платформенных каталогах из §22: `/opt/etc/mounvix/plugins/<id>` для Entware, `/etc/mounvix/plugins/<id>` для Debian. Настройка `plugin_dir` переопределяет default root; `DataDir/plugins/<id>` используется для изменяемых данных, `DataDir/plugins-state.json` — для enabled IDs, журналы и runtime-сокеты остаются в `DataDir`.
- Существующий flat-manifest loader и `plugins-state.json` сохраняются как совместимый legacy-вход; новая загрузка не удаляет и не отключает старые манифесты молча. При совпадающем ID новая package manifest имеет приоритет, конфликт записывается в audit log и legacy entry не запускается. ID ограничен `[a-z0-9][a-z0-9-]{0,63}`; все пути разрешаются строго внутри каталога пакета.
- Manifest содержит `id`, `name`, `version`, `api`, `author`, `description`, `capabilities`, `menu`, `settings_schema`, `pages`, `jobs`, `hooks`, `endpoints`. `api` должен равняться поддерживаемой версии. JSON manifest ≤64 KiB; обязательные поля, дубли ID, пути, типы, cron/timeout bounds и совместимость с приложением/архитектурой валидируются до включения.
- `capabilities` показываются администратору при установке и включении. Для v1 это список заявленных возможностей и согласия, а не обещание OS enforcement: запущенный root-процесс может обходить интерфейс Mounvix и обращаться к ОС напрямую.

## 4. Каталог и lifecycle установки

- Источник v1 — официальный `plugins/index.json` основного репозитория по HTTPS и detached `index.json.sig`, подписанный по точным байтам. Signature envelope содержит `key_id` и base64 Ed25519 signature; сервер проверяет её закреплённым public key **до** разбора JSON, затем сверяет SHA-256 артефакта до передачи пакетному менеджеру. Сервер встраивает текущий и заранее опубликованный next public key для плановой ротации. Закрытый ключ хранится только в release secret store, не в репозитории или Basic Memory. Подпись защищает доверие к каталогу; SHA-256 сверяет конкретный artifact. Если подпись/ключ недоступны или проверка не проходит, каталог остаётся read-only и установка блокируется.
- Установка/обновление выполняется только явным действием администратора после показа имени, издателя, версии, архитектуры, signature/hash и заявленных capabilities; код запускается с полномочиями сервера. Свободное поле «установить любой URL» и пользовательские каталоги в v1 отсутствуют. Установка допускается только для артефактов и URL, перечисленных в подписанном индексе.
- Включение разрешено только после проверки исполняемого файла и manifest. Выключение запрещает новые задания и дожидается завершения/отменяет активный вызов по заданной политике. Удаление пакета не удаляет данные плагина автоматически; очистка `DataDir/plugins/<id>` требует отдельного подтверждения.
- Состояния каталога и job lifecycle видимы: available, installing, disabled, enabled, running, failed. Каждое install/update/enable/disable/remove/run записывается в audit log. Операции пакетного менеджера вызываются с фиксированными аргументами, без shell и без передачи пользовательской строки как команды.

## 5. Runtime: JSON-RPC 2.0 через Unix socket

Использовать потоковый Unix domain socket с NDJSON framing: один UTF-8 JSON-RPC объект на строку, `jsonrpc:"2.0"`, обязательный `id` для запросов, максимум 1 MiB на сообщение. Это формат транспорта Mounvix v1, не JSON-RPC HTTP API.

Для каждого вызова ядро:

1. Создаёт одноразовый socket в `DataDir/run/plugins/`, с непредсказуемым именем и mode `0600`; удаляет socket после завершения.
2. Запускает только manifest-разрешённый executable абсолютным путём внутри каталога установленного плагина через `exec.CommandContext`; shell не используется. Передаёт минимальные `MOUNVIX_PLUGIN_ID`, `MOUNVIX_API_VERSION`, `MOUNVIX_SOCKET`, `MOUNVIX_DATA_DIR`; не наследует секреты окружения без необходимости.
3. Принимает одно соединение в течение 5 секунд и отправляет `plugin.initialize` с ID/API/server version, несекретными настройками и согласованными capabilities. Плагин подтверждает ID/API/версию и перечень поддерживаемых методов; несовпадение завершает запуск.
4. Для пользовательского действия вызывает `plugin.invoke`; для расписания — `plugin.runJob`; для подписки из manifest — `plugin.handleEvent`; для страницы — `plugin.renderPage`. `endpoints` manifest задаёт точные HTTP method + относительный путь под `/api/v1/plugins/{id}/...`; handler передаёт только совпавшие endpoint ID/body в `plugin.invoke`. Любой method/endpoint/job ID, отсутствующий в валидированном manifest, отклоняется.
5. Разрешает обратные методы плагина `host.log` и `host.settings.get` (только несекретные настройки). Файловые и сетевые привилегии не выдаются через host API в v1; это не превращает процесс в sandbox.

Ограничения: 5 секунд на соединение/handshake, 30 секунд на интерактивный вызов, до 10 минут на job, не более 1 MiB JSON/ответа, очередь до 100 ожидающих заданий и глобально не более одного одновременно работающего plugin/rclone job на малоресурсном профиле. Совместный stdout/stderr ограничен 1 MiB на запуск; per-plugin ротация хранит максимум 10 файлов по 1 MiB. Секреты удаляются из пользовательских уведомлений. По timeout/cancel дочерний процесс завершается, затем принудительно убивается после grace period. Нет постоянного фонового процесса: plugin запускается по вызову/заданию/событию и освобождает ресурсы после ответа. В одном процессе допускается один outstanding host→plugin request; обратные `host.*` вызовы коррелируются JSON-RPC ID, malformed/error responses закрывают invocation без panic.

Планировщик использует только валидированные jobs из manifest и ограниченный формат расписания. Каждая попытка получает уникальный `run_id`; persistent `DataDir/plugin-jobs.json` хранит queued/running/terminal record (до 1000 записей/30 дней). После рестарта queued jobs сохраняются, running помечаются interrupted и получают terminal event; повтор одной schedule slot/run не запускается дважды. Terminal run record с детерминированным `(run_id,outcome)` сохраняется до публикации hook и notification; startup reconciliation восстанавливает отсутствующие job-outcome records после crash. В M7 добавляются core producers `upload.done` (успешный WebDAV PUT) и `backup.done` (терминальный backup report); payload содержит только нужные ID/relative path/size/status и не содержит file contents, passwords, tokens или stdout. Подписка `file.indexed` отклоняется: в текущем сервере нет устойчивого media indexer, добавлять новый индексатор в M7 не входит; поддержка отложена до отдельного дизайна этого источника. Manifest hooks валидируются по allowlist: `upload.done`, `backup.done`, `plugin.job.{succeeded,failed,cancelled}`, `rclone.sync.{succeeded,failed,cancelled}`. Для каждого `(event_id, subscriber_plugin_id)` delivery state сохраняется; после durable append доставка `handleEvent` at-least-once, plugin обязан дедуплицировать по `event_id`, exactly-once не обещается. Не-job hook append — best effort после завершения операции; ошибка журналируется, но не превращает успешный upload/backup в ложный failure. Plugin не получает собственный job event обратно как hook, чтобы не создать рекурсивный цикл.

## 6. Безопасность страниц и rclone

- Страница process plugin отдаётся только через авторизованный server route и рендерится внутри sandboxed iframe без `allow-same-origin`, top-navigation и доступа к родительскому DOM. В v1 страницы информационные; произвольный JS/нативный bridge и произвольные сетевые запросы не входят.
- Admin-only UI/API сохраняются. Все state-changing plugin/rclone endpoints (включая существующие `/plugins` form и `/api/v1/plugins`) и новые inbox read endpoints проверяют CSRF/Origin. WebSocket принимает только точный same-origin `Origin` и действующую `mvx` admin-сессию. Для HTTPS login/session cookie получает `Secure`; при TLS termination reverse proxy доверяется только явно настроенная схема.
- Rclone остаётся встроенным adapter к уже установленному бинарнику и существующему `rclone.conf`. UI показывает только имена remotes, возвращённые `rclone listremotes`; ввод remote ограничивается этими именами и относительным путём. Mounvix не запрашивает и не хранит новые OAuth-токены и не показывает конфигурацию/секреты. `rclone` вызывается без shell, output ограничен и очищается от секретов. Для `sync`, способного удалять файлы на target, UI показывает направление и требует явного подтверждения перед запуском.

## 7. Журнал заданий и API inbox

Новый server-side журнал отделён от Memories feed: `/api/v1/feeds` остаётся только лентой карточек воспоминаний. Inbox принимает terminal outcomes plugin jobs и rclone runs: `plugin.job.succeeded`, `plugin.job.failed`, `plugin.job.cancelled`, `rclone.sync.succeeded`, `rclone.sync.failed`, `rclone.sync.cancelled`. Event items также включают `plugin_id/job_id` или `rclone`, `run_id` и безопасный `outcome`. События установки/переключения остаются в audit log и не создают мобильное уведомление.

Событие содержит `event_id`, монотонный `seq` (следующий номер сохраняется в journal и переживает рестарт/очистку), `created_at`, `kind`, безопасный `summary`, same-origin `target_route` и `read_by`. API возвращает `is_read` для текущего пользователя. Идентификатор job/run предотвращает дубли для повторной записи того же terminal outcome. Полный stdout/stderr, локальные пути файлов и секреты в inbox/OS notification не попадают; подробности доступны только в защищённом журнале плагина.

Хранилище hooks — `DataDir/plugin-hook-events.json`; конкурентные append/delivery-state сериализуются через mutex + temp-file/atomic rename, retention 30 дней/1000 событий, новые подписчики не получают историю до включения. При лимите сначала удаляются подтверждённые доставки; если очередь всё ещё заполнена, старейшее undelivered событие может быть отброшено только с увеличением `hook_overflow_count` и заметным предупреждением в Plugins. Журнал уведомлений — отдельный `DataDir/plugin-notifications.json`, только terminal outcomes plugin/rclone jobs; поэтому частые `upload.done`/`backup.done` не вытесняют мобильные уведомления. Он ограничен 30 днями/1000 событий; при gap API возвращает `reset`, а UI явно сообщает об истёкшей части истории. Оба журнала диагностируются отдельно и не удаляются молча при parse error. Inbox read state — per-user; API доступен только администраторам. Ошибка парсинга существующего journal не сбрасывает его молча: inbox отключается с видимой ошибкой и сохранением файла для диагностики.

- Веб-страница `/notifications` и nav unread badge дают администраторам историю inbox. `GET /api/v1/notifications` без cursor возвращает последние события по убыванию; `before=<seq>` листает более старые страницы и возвращает `before_cursor`; `after=<seq>&limit=50` возвращает новые события по возрастанию и `next_cursor`. `after` и `before` взаимоисключающие; `1 <= limit <= 100`, default 50. Ответ содержит `items`, `next_cursor`, `before_cursor`, `reset`, `oldest_seq`, `latest_seq`, `unread_count`; event item содержит `event_id/seq/created_at/kind/summary/target_route/is_read`. `target_route` — core-generated relative route under Mounvix UI only, never an absolute URL.
- Неверный cursor/limit → 400, неавторизованный запрос → 401, не-admin → 403, неизвестный event ID → 404. `POST /api/v1/notifications/{event_id}/read` идемпотентно отмечает событие прочитанным для текущего пользователя (204 при успехе).
- `GET /api/v1/notifications/ws` — WebSocket upgrade. Он отправляет только `{type:"notification.created", event_id, seq}`; клиент делает REST catch-up по cursor, поэтому пропуск/обрыв WS не теряет событие.
- Если `after < oldest_seq - 1` или `after > latest_seq`, REST возвращает `reset:true`, `oldest_seq`, `latest_seq`, пустые `items` и `next_cursor=latest_seq`; клиент сообщает, что часть истории недоступна, и не отправляет старые уведомления повторно. При `after == latest_seq` возвращаются пустые `items`, `reset:false` и неизменённый cursor.
- Веб-страница и API используют тот же HttpOnly admin session; API не раскрывает secrets/process output. WebSocket проверяет точный same-origin `Origin` до upgrade.

Job outcome сначала устойчиво сохраняется в `plugin-jobs.json`, затем идемпотентно добавляется в notification journal и, если на тип подписан включённый plugin, в hook journal; startup reconciliation исправляет разрыв после crash. Только notification journal публикуется в WebSocket/API. Ошибка notification append не транслирует неподтверждённое событие. Медленный WS-клиент отключается; данные восстанавливаются через REST. Android polling использует отдельный notification cursor, не hook- или Memories-cursor.

## 8. Критерии приёмки

- Manifest validation: неверный ID/path/API/архитектура/версия/дубли блокируются; legacy flat manifests и enabled-state сохраняются.
- Test-plugin `echo` проходит initialize/invoke/runJob; неверный handshake, JSON-RPC error и некорректный framing обрабатываются без падения сервера; timeout/cancel завершают child process; socket удаляется. Crash между terminal run record и event append покрыт startup reconciliation.
- Контроль размера, concurrency, log rotation, ID/path traversal и недоступного binary покрыт тестами.
- Каталог отклоняет неверную Ed25519 подпись, hash, неподдерживаемую архитектуру и несовместимую версию до запуска пакетного менеджера; произвольный URL не принимается.
- Fake-rclone тесты подтверждают фильтр remote, аргументы без shell, timeout и предупреждение о destructive `sync`; никакие токены не выдаются UI.
- Successful WebDAV PUT и backup terminal report создают допустимые hook events, ошибки/откаты не создают `upload.done`; неподдерживаемый `file.indexed` hook отклоняется. Hook delivery восстанавливается после рестарта и не дублируется по event ID; notification journal отдельно переживает restarts и не содержит upload/backup событий. Signed catalog verification rejects altered/unsigned index before package-manager execution; REST auth/role/CSRF и WebSocket Origin проверены.
- Go unit/integration tests, `go test -race ./...`, `go vet ./...`, server build и cross-platform package checks проходят до deployment. LXC server binary не обновляется в рамках текущего этапа.

## 9. Отложено

WASM, пользовательские/третьи каталоги, произвольные URL-плагины, недоверенный сторонний код, OS-level sandbox/privilege separation, plugin API для произвольного доступа к файловой системе/сети, plugin pages с произвольным JavaScript, `file.indexed` hook до появления устойчивого media indexer, FCM и OAuth/token management внутри Mounvix.
