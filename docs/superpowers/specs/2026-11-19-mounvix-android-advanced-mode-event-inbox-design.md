# Mounvix Android — Advanced Mode WebView и inbox заданий сервера

**Дата:** 2026-11-19 (Europe/Moscow)
**Статус:** проект для проверки владельцем; Android/server code не менялся.
**Связанная спецификация:** [M7 process plugins and job journal](2026-11-19-mounvix-m7-process-plugins-design.md) задаёт server event API и WebSocket-контракт.

## 1. Цель

Сохранить быстрый нативный Android-клиент и дать пользователю полный серверный Mounvix UI в необязательном Advanced Mode. Нативный слой остаётся ответственным за соединение, SAF, Wi‑Fi-only автозагрузку, фоновые уведомления и состояние WorkManager; серверный Web UI остаётся единственным интерфейсом администрирования и плагинов.

Для plugin/rclone job outcomes используется persistent server inbox: WebSocket обновляет открытый Advanced Mode, а WorkManager получает события в фоне с задержкой. Внешний relay, FCM и постоянный background WebSocket не используются.

## 2. Текущее состояние

- Главный UI построен на Compose; отдельного WebView нет. Нижняя навигация содержит Files/Favorites/Recents/Servers/Memories.
- Server panel уже использует viewport и responsive breakpoints 880px/520px. Для реального WebView всё равно нужны Android QA на узком экране, landscape и вводе с touch-клавиатуры.
- Android уже раз в 15 минут (минимум WorkManager, не гарантированная точность) опрашивает `GET /api/v1/feeds` для Memories, с `NetworkType.CONNECTED` и локальными уведомлениями. Этот API фильтрует карточки без видимых медиа и не является общей лентой событий.
- Feed profile хранит server URL/account; пароль сейчас в обычном `SharedPreferences`. Native API-клиент использует собственную краткоживущую cookie-сессию и может повторно авторизоваться. WebView login ставит HttpOnly `mvx` cookie на 12 часов.
- Текущий cookie handler не выставляет `Secure`; manifest разрешает cleartext traffic приложению. WebView/JavascriptInterface в проекте отсутствуют.

## 3. Режим и навигация

- Простой Compose-режим остаётся режимом по умолчанию и не теряет Files/Favorites/Recents/Servers/Memories.
- В существующем server/profile разделе появляется отдельное действие «Advanced Mode / Открыть панель сервера». Оно открывает WebView на origin настроенного Mounvix server URL; вход выполняется обычной страницей `/login`, без передачи пароля через JS. Запоминается только выбор режима, не веб-пароль. Кнопка «Проверить сервер» повторно читает `/health`, затем проверяет `GET /api/v1/notifications?limit=1`; она снимает `notifications unsupported` только если API реально доступно и профиль имеет admin-доступ.
- WebView показывает весь текущий responsive server UI, включая plugin/rclone pages и notifications inbox. Не создавать параллельные Compose-экраны администрирования.
- Back сначала перемещает историю WebView; затем возвращает пользователя в Compose. Ошибка соединения, `onRenderProcessGone` и пустая загрузка показывают понятный native экран с retry/возвратом в простой режим.
- При остановке Activity закрывается WebSocket; при возврате WebView подключается снова и немедленно делает REST catch-up по cursor. Никакого отдельного background service для WebView нет.

## 4. WebView trust boundary

WebView принимает navigation только к точному настроенному origin (scheme + host + port). Редиректы за его пределы блокируются; обычные внешние ссылки открываются браузером вне WebView. Для основного frame запрещены `file:`, `content:`, произвольные HTTP(S) hosts и mixed content.

Настройки WebView: JavaScript включён только для доверенной same-origin панели (она использует клиентский JS), `allowFileAccess=false`, `allowContentAccess=false`, `mixedContentMode=NEVER`, Safe Browsing включён, third-party cookies отключены, WebView debugging выключен в release. Не добавлять `addJavascriptInterface` и не инжектировать учётные данные/cookies в JavaScript. WebView login использует стандартную HttpOnly cookie-сессию сервера; cookies не читаются из JS.

`WebChromeClient` обслуживает только стандартный file chooser через системный SAF picker, если серверная форма действительно требует загрузить файл; приложение передаёт выбранный `content://` URI с минимальным grant, не раскрывая локальный путь. Никакого прямого доступа HTML/JS к SAF и произвольной файловой системе.

HTTPS рекомендуется для удалённого доступа. Для явно настроенного локального HTTP origin показывается предупреждение о незашифрованной передаче пароля/сессии; такое соединение нельзя объявлять безопасным для публичной сети. Не игнорировать TLS certificate errors. Сервер должен выставлять `Secure` cookie при HTTPS (включая явно настроенный TLS reverse proxy) и проверять `Origin` для новых state-changing endpoints/WebSocket. Существующий app-wide cleartext флаг не расширяется на произвольные hosts; конкретная Android network-security настройка должна сохранить требуемые текущие локальные функции.

## 5. Учётная запись и secrets для фонового клиента

WebView session и native WorkManager session независимы: веб-cookie остаётся в `CookieManager`, native API использует профиль сервера и получает собственную HttpOnly cookie после login. Пароль никогда не передаётся WebView через bridge/JavaScript.

До включения background polling пароль Mounvix-профиля переносится из plaintext preferences в Keystore-backed encrypted secret store. Миграция выполняется идемпотентно: сначала записать и прочитать обратно зашифрованное значение, затем удалить старую plaintext копию. При ошибке шифрования/восстановления ничего не стирать молча — отключить background login и запросить повторную авторизацию. Ключ не экспортируется и не копируется в backup; после restore/потери ключа пользователь входит заново. Секреты не логируются. Широкая миграция несвязанных SMB/WebDAV/AI secrets не входит в эту спецификацию.

Существующий Mounvix server profile используется как единственный источник URL/пользователя для native polling и открытия Advanced Mode; не создавать второй параллельный аккаунт только для плагинов. URL принимается только как нормализованный `http`/`https` base URL без userinfo; сохранённый path prefix учитывается при построении WebView и callback routes. В Compose пароль не должен оставаться в `rememberSaveable`/saved-state после сохранения или выхода с экрана. При отсутствии admin-доступа Android показывает понятное ограничение: plugin/rclone inbox доступен только администратору.

## 6. Фоновый inbox заданий

Пользовательский switch «Задания сервера» (по умолчанию выключен) включает отдельную unique periodic WorkManager-задачу, если профиль сохранён и M7 endpoint поддерживается; интервал — минимум 15 минут, `NetworkType.CONNECTED`. Отключение отменяет worker, но оставляет cursor. При первом включении для нового профиля worker устанавливает baseline на текущий `latest_seq` без массовых уведомлений об исторических jobs; последующее отключение/повторное включение сохраняет прежний cursor и обрабатывает только пропущенные события. Периодический запуск может быть отложен Android Doze/OEM-ограничениями; это не instant push и не гарантия времени доставки. При ручном открытии приложения foreground REST catch-up устраняет пропуски между периодическими запусками.

Cursor хранится в Room-таблице `server_event_cursors`, keyed по стабильной несекретной идентичности server URL + username; пароль и cookie туда не попадают. Для Room используется явная forward-only migration без destructive fallback. Если M4 receipts и этот cursor входят в один APK, обе схемы входят в одну migration; если M4 уже выпущен, cursor добавляется следующим номером версии (например, v2→v3), не создавая конкурирующую миграцию v1→v2.

Каждый профиль хранит свой `last_seq` cursor локально в Room, отдельно от Memories `next_since`. Poller вызывает M7 `GET /api/v1/notifications?after=<last_seq>&limit=100`; endpoint остаётся отдельным от `/api/v1/feeds`. Профили/курсор не смешиваются при смене server URL или username.

- Worker запрашивает страницы с `after=<last_seq>` и обрабатывает до 500 событий за запуск (не более пяти страниц по 100); после каждой завершённой страницы cursor сохраняется после side effects. Уже прочитанное событие cursor продвигает, но не alert-ит. Новый непрочитанный event создаёт локальную Android notification со стабильным ID от `event_id` в канале «Задания сервера»; повтор заменяет то же уведомление без повторного alert.
- Если worker упал внутри страницы, cursor этой страницы не двигается; повтор безопасен благодаря стабильным notification IDs. После HTTP/network failure cursor не двигается. 401 вызывает одну повторную native re-auth; повторный 401 помечает профиль как требующий входа и приостанавливает worker. 403 означает отсутствие admin-доступа и отключает plugin inbox polling, не затрагивая Memories. 404/501 означает, что server version не поддерживает inbox: не retry-loop, показать «обновите сервер» и повторить обнаружение только по кнопке «Проверить сервер» (GET `/health`) или после зафиксированного изменения версии. Network/429/5xx используют WorkManager backoff без продвижения cursor.
- Нажатие notification через отдельный native intent router открывает Advanced Mode на серверном same-origin `target_route`; route проверяется как относительный путь под настроенным base path, без scheme/host/open-redirect. При отсутствии WebView session пользователь проходит обычный login, затем возвращается к событию. После явного tap страница открывает event detail и отмечает событие прочитанным идемпотентным `POST .../read`; только polling не меняет `read_by`. Локальный alert содержит только безопасный тип/итог job и имя задания; локальные пути, filenames, stdout/stderr и secrets остаются в защищённой панели.
- Если Android 13+ notification permission запрещено, cursor всё равно продвигается после успешной обработки: старые события не должны массово всплыть после последующего разрешения; события остаются в серверном inbox и доступны в Web UI. Отказ в разрешении не отключает чтение inbox в приложении.
- Используется существующий native quiet-hours диапазон по локальному часовому поясу устройства (по умолчанию 22:00–07:00), без второго несогласованного расписания. В quiet hours OS alert подавляется, cursor всё равно продвигается после обработки, а событие остаётся непрочитанным в серверном inbox; задним числом alert не отправляется. Текущие Memories уведомления и их cursor остаются независимыми. Локальный M4 upload результат создаётся самим upload worker и не дублируется как server job event.
- При `reset:true` (сервер удалил события старше retention) клиент сообщает о разрыве истории в inbox UI, устанавливает cursor на серверную границу и не уведомляет о неизвестных удалённых событиях.

## 7. Foreground WebSocket

Открытый Advanced Mode подключается к M7 `/api/v1/notifications/ws` только пока экран foreground/visible. Сессия передаётся штатной cookie-механикой WebView; JS получает только event ID/sequence envelope, после чего читает новые детали через авторизованный REST API. WebSocket не передаёт stdout/stderr, пароли или токены. При новом event веб-страница обновляет inbox/badge; она не создаёт параллельную OS notification, чтобы не дублировать alert.

При disconnect/reconnect используется ограниченный exponential backoff, затем REST catch-up по UI cursor. Сервер проверяет точный Origin и admin-сессию. При уходе в background, logout или переключении профиля socket закрывается; никакого Android foreground service и фонового постоянного socket. Если worker выполняется при открытом Advanced Mode, он продвигает native cursor, но пропускает OS alert: событие остаётся в live WebView inbox; если WebSocket потерян, catch-up при reconnect его восстановит.

## 8. Проверки и критерии приёмки

- Простой Compose-режим остаётся первым запуском и не ломает текущую навигацию; Advanced Mode открывает правильный server origin, login и responsive pages.
- WebView тесты блокируют cross-origin redirect, `file:`/`content:` и mixed content; внешний URL открывается вне WebView; у страницы нет native JS bridge; release debugging выключен.
- Web login cookie не читается JavaScript; WebView logout очищает/отзывает только web session; отдельное действие «Отключить профиль» отменяет native worker, удаляет native secret/cursor и очищает WebView cookies для настроенного origin. HTTP-origin всегда показывает предупреждение, TLS errors не обходятся.
- SAF file chooser (если используется) даёт только явно выбранный URI; local path не раскрывается.
- Keystore миграция сохраняет учётную запись после успешного перехода, удаляет plaintext только после read-back, не логирует секреты и безопасно предлагает повторный вход при потере ключа.
- WorkManager polling использует отдельный per-server/account cursor, сохраняет его после side effects, не дублирует уведомления при повторном batch, не продвигает после network/API failure, уважает Android notification permission и quiet hours; 404/501 не создаёт retry-loop, а ручная проверка версии снова обнаруживает capability.
- WorkManager событие задержки не обещает точную доставку; тесты имитируют задержку/отсутствие сети, first-enable baseline, cursor reset, 401/403/404, quiet hours, permission denied, прочитанное событие и повтор одного event ID.
- WebSocket подключается только в foreground, сверяет Origin/session, восстанавливается после потери сети и догоняет события через REST; background-доставка выполняется только WorkManager.
- UI проверяется на Android emulator: ширины 320/375 dp, landscape, back stack, screen recreation, закрытие WS при background, установка/обновление APK и реальный notification tap.

## 9. Не входит

FCM или иной relay, гарантированно немедленная доставка в фоне, постоянный WebSocket/service, замена нативного файлового менеджера, доступ WebView к произвольной файловой системе, отдельный OAuth/token manager, дублирование server admin UI в Compose, дублирование Memories в plugin inbox.
