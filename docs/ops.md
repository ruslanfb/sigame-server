# Эксплуатация SIGame Server

Сервер — один статический бинарник (Go, без CGO). Хранит данные в SQLite и файлах медиа в каталоге данных.
Играть можно по локальной сети (по умолчанию) или через интернет (за reverse-proxy / с публичным URL).

> **Важно про безопасность.** Настройки по умолчанию рассчитаны на **локальную сеть среди своих**: REST API не требует
> аутентификации (любой в сети может создавать/удалять паки и комнаты, загружать медиа, вызывать тестовый запрос к ИИ),
> CORS открыт, лимиты на число комнат/загрузок мягкие. Для публичного интернета ставьте сервер за reverse-proxy с
> аутентификацией (basic auth / OAuth-proxy) хотя бы для `/api/v1/packs*`, `/api/v1/media*`, `/api/v1/ai/*` и `POST /api/v1/rooms`,
> ограничивайте `SIGAME_CORS_ORIGINS`, включайте лимиты прокси на частоту запросов и размер тела, уменьшайте `SIGAME_MAX_SIQ_MB`.

## Быстрый старт

```bash
# сборка
make build            # → bin/sigame
# запуск
./bin/sigame          # слушает :8080, данные в ./data
```

Флаги: `--version` — версия; `--print-openapi` — вывести спецификацию OpenAPI в JSON и выйти (так собирается `docs/openapi.json`).

При старте сервер печатает адреса для подключения, например:

```
SIGame server запущен. Адреса для подключения:
  http://192.168.1.5:8080   (API docs: http://192.168.1.5:8080/docs)
```

- `GET /docs` — интерактивная документация API (Scalar), `GET /openapi.json` — спецификация OpenAPI 3.1.
- `GET /api/v1/system/info` — версия, адреса LAN, join-URL, наличие ffprobe и ИИ-ведущего.

Нужен `ffprobe` (из состава ffmpeg) в `PATH` — тогда сервер определяет длительность аудио/видео и размеры картинок при загрузке.
Без него всё работает, но длительность медиа будет неизвестна (клиенты сообщают об окончании воспроизведения сами).

## Конфигурация

Источник настроек: значения по умолчанию → файл `config.yaml` (или путь из `SIGAME_CONFIG`) → переменные окружения.

| Переменная | Ключ YAML | По умолчанию | Описание |
|---|---|---|---|
| `SIGAME_ADDR` | `addr` | `:8080` | Адрес прослушивания. `:8080` = все интерфейсы (нужно для LAN). |
| `SIGAME_PUBLIC_URL` | `publicUrl` | — | Публичный URL (за прокси / для интернета). Попадает в join-URL первым. |
| `SIGAME_CORS_ORIGINS` | `corsOrigins` | пусто = все | Список origin через запятую. В LAN можно оставить пустым. |
| `SIGAME_TRUST_PROXY` | `trustProxy` | `false` | Доверять `X-Forwarded-*` (только за своим reverse-proxy). |
| `SIGAME_MDNS` | `mdns` | `true` | Анонс сервиса в локальной сети (зарезервировано; в v1 не реализовано). |
| `SIGAME_DATA_DIR` | `dataDir` | `./data` | Каталог данных: `sigame.db` (SQLite) и `media/`. |
| `SIGAME_LOG_LEVEL` | `logLevel` | `info` | `debug` / `info` / `warn` / `error`. |
| `SIGAME_LOG_FORMAT` | `logFormat` | `text` | `text` / `json`. |
| `SIGAME_MAX_IMAGE_MB` | `maxImageMB` | `20` | Лимит размера картинки/гифки. |
| `SIGAME_MAX_AUDIO_MB` | `maxAudioMB` | `100` | Лимит аудио. |
| `SIGAME_MAX_VIDEO_MB` | `maxVideoMB` | `512` | Лимит видео. |
| `SIGAME_MAX_HTML_MB` | `maxHTMLMB` | `1` | Лимит HTML-вопроса. |
| `SIGAME_MAX_SIQ_MB` | `maxSIQMB` | `1024` | Лимит загружаемого `.siq`. |
| `SIGAME_FFPROBE` | `ffprobe` | ищется в PATH | Путь к `ffprobe`. |
| `SIGAME_ALLOW_HTML_SCRIPTS` | `allowHtmlScripts` | `true` | Разрешить скрипты в HTML-вопросах (в песочнице `sandbox allow-scripts`). |
| `SIGAME_ALLOW_SVG` | `allowSvg` | `false` | Принимать SVG (риск XSS — по умолчанию выключено). |
| `SIGAME_MEDIA_GC_GRACE` | `mediaGcGrace` | `24h` | Через сколько удалять медиа, на которые не ссылается ни один пак. |
| `SIGAME_ROOM_TTL` | `roomTtl` | `6h` | Закрывать простаивающие комнаты. |
| `SIGAME_MAX_ROOMS` | `maxRooms` | `50` | Максимум одновременных комнат. |
| `SIGAME_MAX_PLAYERS` | `maxPlayers` | `12` | Максимум игроков в комнате (1..12). |
| `SIGAME_WS_MAX_MESSAGE_BYTES` | `wsMaxMessageBytes` | `65536` | Максимальный размер WebSocket-сообщения. |
| `SIGAME_WS_MESSAGES_PER_SEC` | `wsMessagesPerSec` | `40` | Лимит сообщений в секунду на соединение. |
| `OPENROUTER_API_KEY` | `openRouterApiKey` | — | Ключ OpenRouter — включает ИИ-ведущего. |
| `OPENROUTER_MODEL` | `openRouterModel` | `google/gemini-3-flash-preview` | Модель-судья (быстрая, ~2 с). `tencent/hy4-preview` тоже работает, но ~9–16 с на вердикт. |
| `OPENROUTER_BASE_URL` | `openRouterBaseUrl` | `https://openrouter.ai/api/v1` | Базовый URL API. |
| `SIGAME_AI_TIMEOUT` | `aiTimeout` | `25s` | Таймаут запроса к ИИ (с одним повтором); после него применяется нечёткое сравнение. |
| `SIGAME_AI_TEMPERATURE` | `aiTemperature` | `0` | Температура модели. |
| `SIGAME_AI_MAX_TOKENS` | `aiMaxTokens` | `2000` | Лимит токенов ответа (у reasoning-моделей сюда входят скрытые «рассуждения»). |
| `SIGAME_AI_STRUCTURED` | `aiStructured` | `auto` | Слать ли `response_format: json_schema`: `on` / `off` / `auto` (для `tencent/hy*` — off, они не завершают ответ со схемой). |
| `SIGAME_DEV` | `dev` | `false` | Режим разработки (подробные ошибки). |
| `SIGAME_PPROF` | `pprof` | `false` | Включить `/debug/pprof`. |

Пример `config.yaml`:

```yaml
addr: ":8080"
dataDir: /var/lib/sigame
logFormat: json
openRouterApiKey: sk-or-v1-...
openRouterModel: google/gemini-3-flash-preview
```

## Локальная сеть

1. Запустите сервер на любом компьютере в сети (ноутбук ведущего).
2. Убедитесь, что порт 8080 открыт в брандмауэре (macOS спросит при первом запуске; Windows — «Разрешить доступ»).
3. Игроки открывают адрес из баннера (например `http://192.168.1.5:8080`) — клиент подключается к REST и WebSocket.
4. Для честной кнопки важно, чтобы **процесс сервера сам принимал TCP-соединения** (без промежуточного прокси): тогда
   доступен «ядерный» якорь RTT (`TCP_INFO` на Linux, `TCP_CONNECTION_INFO` на macOS). За прокси используется только
   якорь по WebSocket ping/pong — это тоже работает, но с более широкими допусками. Подробнее — `docs/buzzer.md`.

## Интернет

- Задайте `SIGAME_PUBLIC_URL=https://quiz.example.com` и поставьте TLS-терминатор (Caddy/nginx) перед сервером,
  **с пробросом WebSocket** (`Upgrade`/`Connection`) и без буферизации ответов для `/media/` (Range-запросы).
- Включите `SIGAME_TRUST_PROXY=true`, ограничьте `SIGAME_CORS_ORIGINS`.
- В настройках комнаты выбирайте пресет кнопки `internetFair` (или `noRace` — письменные ответы) — см. `docs/buzzer.md`.

Пример Caddy:

```
quiz.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

## Docker

```bash
docker compose up -d --build
```

Образ содержит `ffmpeg` (для `ffprobe`); данные — в томе `sigame-data`.

## systemd

```ini
[Unit]
Description=SIGame server
After=network-online.target

[Service]
ExecStart=/usr/local/bin/sigame
Environment=SIGAME_ADDR=:8080 SIGAME_DATA_DIR=/var/lib/sigame SIGAME_LOG_FORMAT=json
User=sigame
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

## Данные и резервные копии

```
data/
  sigame.db          # SQLite (WAL: рядом sigame.db-wal, sigame.db-shm)
  media/ab/abcdef…   # файлы медиа, content-addressed (sha256)
  media/tmp/         # временные файлы загрузок
```

Бэкап: остановить сервер или выполнить `sqlite3 data/sigame.db ".backup backup.db"` и скопировать `media/`.
Медиа без ссылок из паков удаляются сборщиком мусора через `SIGAME_MEDIA_GC_GRACE`.

## Ограничения и безопасность

- Аутентификации аккаунтов нет: комнаты защищены кодом комнаты (+ пароль по желанию), администрирование комнаты —
  токеном хоста (`X-Host-Token`), выдаваемым при создании комнаты.
- Игроки никогда не получают правильные ответы, комментарии для ведущего и медиа ответов до их показа (проекции по ролям).
- Загрузки ограничены по размеру и типу (сниффинг MIME, не расширение); HTML-вопросы отдаются в песочнице (CSP `sandbox`).
- `.siq` проверяется на zip-бомбы и path traversal.
- Ключ OpenRouter хранится только в конфигурации сервера; ИИ получает только текст вопроса, эталонные ответы и ответ игрока.

## Сборка и релизы

```bash
make test      # go test -race ./...
make cross     # dist/sigame-{linux,darwin,windows}-*
```

Версия вшивается через `-ldflags -X main.version=…` (см. `Makefile`); CI (GitHub Actions) собирает и тестирует на Linux/macOS/Windows.
