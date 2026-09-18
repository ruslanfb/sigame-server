# SIGame Server

Бекенд для игры «Своя игра» (совместим с форматом паков SIGame `.siq`): библиотека паков с медиа,
игра по локальной сети и через интернет по WebSocket, REST API с документацией OpenAPI, **честная кнопка,
не зависящая от пинга**, и ИИ-ведущий (OpenRouter). Только сервер — клиент (веб/мобильный) подключается по API.

Один статический бинарник на Go (Windows / macOS / Linux), SQLite, без внешних сервисов.

## Возможности

- **Паки**: свой JSON-формат (надмножество SIQ v5) с полным редактированием по REST (раунды, темы, вопросы),
  импорт `.siq` v3/v4/v5 и экспорт `.siq` v5, отчёт совместимости; картинки/гифки, аудио, видео, HTML.
- **Все типы вопросов** SIGame: обычный (с кнопкой), со ставкой (аукцион), кот в мешке (три варианта), без риска,
  для всех, для всех со ставкой (финал), пользовательские; типы ответов: текст, выбор варианта, число, точка.
- **Полные правила**: раунды и финал с удалением тем, чузер, фальстарты, штрафы, апелляции («Я прав» / «Я против»),
  пауза, управление ведущего (правка счёта, пропуск, возврат вопроса), режимы «классика / упрощённая / квиз / по очереди».
- **Честная кнопка (AnchoredHybrid)**: кнопка зажигается у всех в один и тот же момент по синхронизированным часам,
  ранжирование по реакции игрока, а не по времени прихода пакета; RTT «якорится» величинами, которые нельзя подделать
  из браузера (WebSocket ping/pong и `TCP_INFO` ядра); античит, шкала доверия, аудит каждого нажатия. Подробнее — [docs/buzzer.md](docs/buzzer.md).
- **Ведущий**: человек, ИИ (OpenRouter, по умолчанию `tencent/hy4-preview`) или гибрид (ИИ предлагает, человек подтверждает);
  нечёткое сравнение ответов как запасной вариант. Подробнее — [docs/ai-showman.md](docs/ai-showman.md).
- **LAN**: при старте печатает адреса для подключения; комнаты с кодом из 5 символов и паролем; переподключение с восстановлением состояния.
- **API-документация**: OpenAPI 3.1 (`/openapi.json`) и интерактивный UI (`/docs`).

## Быстрый старт

```bash
make build          # → bin/sigame  (нужен Go 1.27; ffprobe из ffmpeg — по желанию)
./bin/sigame
```

```
SIGame server запущен. Адреса для подключения:
  http://192.168.1.5:8080   (API docs: http://192.168.1.5:8080/docs)
```

Импортировать пак и создать комнату:

```bash
curl -F file=@my-pack.siq http://localhost:8080/api/v1/packs/import
# → {"pack":{"id":"…"}, "report":{…}}

curl -X POST http://localhost:8080/api/v1/rooms \
  -H 'Content-Type: application/json' \
  -d '{"packId":"<id>","name":"Пятничная игра","showman":"human","buzzerPreset":"wifiParty"}'
# → {"room":{"code":"K7M3P",…}, "hostToken":"…"}

curl -X POST http://localhost:8080/api/v1/rooms/K7M3P/join \
  -H 'Content-Type: application/json' -d '{"name":"Аня","role":"player"}'
# → {"sessionToken":"…","personId":"…","wsUrl":"/ws?room=K7M3P&token=…"}
```

Дальше клиент открывает WebSocket `ws://host:8080/ws?room=K7M3P&token=…` — протокол описан в [docs/protocol.md](docs/protocol.md).

ИИ-ведущий: задайте `OPENROUTER_API_KEY` и создайте комнату с `"showman":"ai"` (или `"hybrid"`).

## Документация

| Документ | Содержание |
|---|---|
| [docs/api.md](docs/api.md) | Руководство по REST API с примерами (источник истины — `/openapi.json`) |
| [docs/protocol.md](docs/protocol.md) | WebSocket-протокол: подключение, сообщения, диаграммы, гайд для клиента по кнопке |
| [docs/protocol-engine.md](docs/protocol-engine.md) | События и команды игрового движка (все типы вопросов, таймеры) |
| [docs/buzzer.md](docs/buzzer.md) | Дизайн честной кнопки, константы, модель угроз, пояснение для ведущего |
| [docs/packs.md](docs/packs.md) | Модель пака, типы вопросов, совместимость с `.siq` |
| [docs/ai-showman.md](docs/ai-showman.md) | Настройка ИИ-ведущего |
| [docs/ops.md](docs/ops.md) | Конфигурация, LAN/интернет, Docker, systemd, бэкапы |
| [docs/design/00-constitution.md](docs/design/00-constitution.md), [docs/adr/](docs/adr/) | Архитектурные решения |
| [docs/research/](docs/research/) | Исследование правил SIGame, формата `.siq`, реализаций, netcode |

## Архитектура

```
cmd/sigame            точка входа
internal/httpapi      REST (chi + huma → OpenAPI 3.1, Scalar UI)
internal/ws           WebSocket-транспорт: envelope, лимиты, якоря RTT (ping/pong, TCP_INFO)
internal/room         актор комнаты: сессии, таймеры, интеграция движка и кнопки, ИИ-ведущий, resume
internal/engine       чистый детерминированный движок игры (все правила SIGame)
internal/buzzer       чистое ядро честной кнопки + детерминированный симулятор
internal/packs        модель паков, валидация, репозиторий (SQLite)
internal/siq          импорт/экспорт .siq
internal/media        хранилище медиа (sha256), сниффинг, ffprobe, HTTP Range
internal/ai           судья ответов: OpenRouter + нечёткое сравнение
internal/db, config, lan, clock
```

Движок и кнопка не делают ввод-вывод и не знают о времени — комната (одна горутина на комнату) подаёт им команды
и рассылает события по ролям; игроки никогда не получают правильные ответы и скрытые данные до их раскрытия.

## Разработка

```bash
make test     # go test -race ./...
make vet
make cross    # dist/sigame-{linux,darwin,windows}-*
make openapi  # docs/openapi.json
```

Тесты включают: сценарные тесты движка, симулятор кнопки с моделями сетей (LAN/WiFi/WAN, потери) и
противниками (подделка меток, задержка sync/pong, боты), импорт 58 официальных паков `.siq`, HTTP/WebSocket-интеграцию.

## Лицензия и благодарности

Формат `.siq` и правила — проект [SIGame](https://github.com/VladimirKhil/SI) Владимира Хиля (MIT); тестовые паки взяты из него.
