# REST API SIGame Server — руководство для разработчика клиента

Документ описывает HTTP API сервера: как хранить паки, загружать медиа, создавать комнаты и подключать
игроков. Всё, что происходит в реальном времени (ход игры, кнопка, чат), идёт по WebSocket и описано в
[`protocol.md`](protocol.md) и [`protocol-engine.md`](protocol-engine.md). Настройка кнопки — в
[`buzzer.md`](buzzer.md), модель пака — в [`packs.md`](packs.md), ИИ-ведущий — в [`ai-showman.md`](ai-showman.md).

## 1. Обзор

| Что | Где |
|---|---|
| Базовый URL JSON-операций | `http://<host>:<port>/api/v1` (порт по умолчанию 8080, см. [`ops.md`](ops.md)) |
| Интерактивная документация | `GET /docs` (Scalar) |
| Спецификация OpenAPI 3.1 | `GET /openapi.json`, `GET /openapi.yaml` — **источник истины** по полям и статусам |
| Стриминг медиа | `GET /media/{id}` (вне `/api/v1`, поддерживает Range) |
| WebSocket | `GET /ws?room=CODE&token=SESSION` |

Соглашения:

- JSON, поля в `camelCase`. Время — `int64` миллисекунды: поля `*At` — Unix ms UTC, поля `*Ms` — длительности.
- Идентификаторы паков/раундов/тем/вопросов/персон — UUIDv7; идентификатор медиа — sha256 hex содержимого.
- Тела запросов проверяются по JSON Schema: неизвестное поле — 422 (`unexpected property`), поле без
  `omitempty` в схеме считается обязательным. Поэтому объекты `rules`, `times`, `buzzer` передаются
  **целиком** (возьмите их из ответа сервера или из `/buzzer-presets` и измените нужное).
- Ответы содержат служебное поле `$schema` (ссылка на схему) — его можно игнорировать.
- Генерируйте клиент из `/openapi.json`: `operationId` каждого эндпоинта указан в таблице в конце документа.

### Ошибки (RFC 9457)

Любая ошибка — документ `application/problem+json` c полями `status`, `title`, `detail`; ошибки валидации
добавляют список `errors`, где `location` — место в запросе (`body.packId`, `header.X-Host-Token`, `query.limit`
или JSON-pointer внутри пака `/rounds/0/themes/2/name`), а `value` — машиночитаемый код проблемы.

```json
{
  "status": 422,
  "title": "Unprocessable Entity",
  "detail": "pack not found",
  "errors": [
    { "location": "body.packId", "message": "pack not found", "value": "packNotFound" }
  ]
}
```

```json
{
  "status": 403,
  "title": "Forbidden",
  "detail": "wrong room password",
  "errors": [
    { "location": "body.password", "message": "bad password", "value": "badPassword" }
  ]
}
```

Типовые статусы: `400` — битый multipart/zip; `401` — нет заголовка `X-Host-Token`; `403` — неверный
токен/пароль/бан; `404` — не найдено; `409` — конфликт версии пака, занято имя/место, недопустимо в текущем
состоянии; `410` — комната закрыта; `412` — испорчен `If-Match`; `413` — превышен лимит размера; `415` —
неподдерживаемый тип медиа; `422` — ошибка валидации; `423` — вход в комнату закрыт ведущим; `503` —
недоступно (ИИ не настроен, достигнут лимит комнат, БД не отвечает); `504` — таймаут.

## 2. Паки

Модель пака (раунды → темы → вопросы, типы вопросов, `ContentItem`, медиа) описана в [`packs.md`](packs.md).

### Список и чтение

`GET /packs?q=&language=&tag=&minDifficulty=&maxDifficulty=&hasMedia=&sort=updatedAt|name|createdAt&order=asc|desc&limit=50&offset=0`
возвращает страницу кратких описаний (без раундов). Поиск `q` регистронезависимый, работает с кириллицей.

```json
{
  "items": [
    { "id": "0192…", "version": 3, "name": "Кино 90-х", "language": "ru-RU", "difficulty": 5,
      "tags": ["кино"], "authors": ["Иван"], "roundCount": 3, "themeCount": 15, "questionCount": 75,
      "hasMedia": true, "createdAt": 1737000000000, "updatedAt": 1737100000000 }
  ],
  "total": 1, "limit": 50, "offset": 0
}
```

`GET /packs/{id}` — полный пак; заголовок `ETag: "3"` — версия пака в кавычках.

### Создание и замена

- `POST /packs` — тело: пак (достаточно `{"name": "…"}`); `id`, `version`, `createdAt`, `updatedAt` игнорируются.
  Пак нормализуется (обрезка строк, типы по умолчанию, difficulty 5) и валидируется; все `mediaId` должны
  быть уже загружены. Ответ `201`, `Location: /api/v1/packs/{id}`, `ETag`.
- `PUT /packs/{id}` — полная замена. Оптимистичная блокировка: заголовок `If-Match: "3"` (в приоритете) или
  `body.version`; без них — безусловно. При расхождении `409` с текущей версией в `errors[0].value`;
  испорченный `If-Match` — `412`. Версия увеличивается, новая — в `ETag`.
- `DELETE /packs/{id}` — `204`; ссылки на медиа освобождаются (сами файлы остаются до удаления/GC).
- `POST /packs/{id}/duplicate` с телом `{"name": "…"}` (необязательно) — глубокая копия с новыми id, `201`.
- `POST /packs/validate` — та же нормализация и валидация без сохранения, всегда `200`:

```json
{ "valid": false, "problems": [ { "path": "/rounds/0/themes/0/questions/0/price", "code": "badPrice", "message": "…" } ] }
```

### Вложенное редактирование

Изменяют один элемент и возвращают **весь** пак с новой версией (`ETag`); при добавлении заголовок
`X-Created-Id` содержит id нового элемента. `If-Match` работает как в `PUT`; без него используется версия,
прочитанная в начале операции (параллельные правки не затирают друг друга, `409` при гонке).

| Метод | Путь | Назначение |
|---|---|---|
| `POST` | `/packs/{id}/rounds?at=` | добавить раунд (`at` — позиция, по умолчанию в конец) |
| `PUT` / `DELETE` | `/packs/{id}/rounds/{roundId}` | заменить / удалить раунд |
| `POST` | `/packs/{id}/rounds/reorder` | `{"ids": […]}` — новый порядок |
| `POST` | `/packs/{id}/rounds/{roundId}/themes?at=` | добавить тему |
| `PUT` / `DELETE` | `…/themes/{themeId}` | заменить / удалить тему |
| `POST` | `…/themes/reorder` | порядок тем |
| `POST` | `…/themes/{themeId}/questions?at=` | добавить вопрос |
| `PUT` / `DELETE` | `…/questions/{questionId}` | заменить / удалить вопрос |
| `POST` | `…/questions/reorder` | порядок вопросов |

### Импорт и экспорт `.siq`

```bash
# импорт (v3/v4/v5), поле формы file
curl -F file=@pack.siq http://localhost:8080/api/v1/packs/import
# сухой прогон: только конвертация и отчёт, ничего не сохраняется
curl -F file=@pack.siq "http://localhost:8080/api/v1/packs/import?dryRun=true"
# экспорт в SIQ v5
curl -OJ http://localhost:8080/api/v1/packs/0192…/export
```

Ответ импорта — `201` (или `200` при `dryRun`) с паком и отчётом совместимости:

```json
{
  "pack": { "id": "0192…", "name": "Кино 90-х", "…": "…" },
  "report": {
    "version": 4,
    "entries": [
      { "level": "warning", "code": "mediaMissing", "path": "/rounds/1/themes/2/questions/0", "message": "Audio/track.mp3 not in archive" },
      { "level": "info", "code": "autoFixed", "path": "/rounds/0/themes/0/questions/3", "message": "numberSet step adjusted" }
    ],
    "stats": { "rounds": 3, "themes": 15, "questions": 75, "…": "…" }
  }
}
```

Медиа, которые не удалось сохранить (нет в архиве, слишком большие, неподдерживаемый тип), отбрасываются и
попадают в отчёт; пак сохраняется. Мелкие расхождения с валидатором чинятся автоматически (`autoFixed`);
неисправимые проблемы — `422`, ничего не сохраняется. Размер архива ограничен `SIGAME_MAX_SIQ_MB` (`413`).
Подробности отчёта и таблица совместимости — [`packs.md`](packs.md) §6–8.

## 3. Медиа

- `POST /media` — `multipart/form-data`, поле `file`; необязательный `?kind=image|audio|video|html`
  (отклонить, если распознанный тип не совпадает). Тип определяется по содержимому, не по имени файла.
  Принимаются png, jpeg, gif, webp, mp3, ogg, m4a, wav, mp4, webm, html (svg — только с `SIGAME_ALLOW_SVG`).
  Одинаковое содержимое хранится один раз: повторная загрузка отвечает `200` вместо `201`.

```bash
curl -F file=@cover.png "http://localhost:8080/api/v1/media?kind=image"
```

```json
{ "id": "9f86d08…", "kind": "image", "mime": "image/png", "ext": "png", "size": 48213,
  "width": 800, "height": 600, "originalName": "cover.png", "refCount": 0, "createdAt": 1737000000000 }
```

- Лимиты по типам — `SIGAME_MAX_IMAGE_MB` / `AUDIO` / `VIDEO` / `HTML`; действующие значения видны в
  `GET /system/info` (`limits`). Превышение — `413`, неподдерживаемый тип — `415`.
- `GET /api/v1/media/{id}` — метаданные; `DELETE /api/v1/media/{id}` — удаление (`409`, если на объект
  ссылается пак).
- `GET /media/{id}` — сами байты с распознанным `Content-Type`. Поддерживаются `Range` (`206`, нужен для
  перемотки аудио/видео), `HEAD`, `If-None-Match` (`304`), `?download=1` (`Content-Disposition: attachment`).
  Ответы неизменяемы: `ETag: "<id>"`, `Cache-Control: public, max-age=31536000, immutable` — кэшируйте
  агрессивно, id меняется вместе с содержимым. HTML отдаётся с CSP-песочницей.

В паке медиа указывается через `mediaId` элемента контента; URL для клиента — `/media/{mediaId}`.

## 4. Комнаты

### Жизненный цикл

```
POST /rooms                         → { room, hostToken, joinUrl }     создатель хранит hostToken
POST /rooms/{code}/join             → { sessionToken, personId, role, isHost, roomCode, wsUrl }
GET  /ws?room={code}&token={token}  → WebSocket (protocol.md)
POST /rooms/{code}/leave            → 204
```

1. **Создать** комнату из сохранённого пака. Ответ содержит `hostToken` — секрет создателя, который
   показывается **только один раз**. Он даёт права ведущего-хоста при входе и авторизует host-эндпоинты.
2. **Войти** под именем и ролью → `sessionToken`. Создатель передаёт `X-Host-Token: <hostToken>` в запросе
   `join`: первый вошедший с ним становится хостом (`isHost: true`), пароль и режим входа для него не
   действуют. Повторный вход с тем же именем и ролью, пока эта персона отключена, возвращает новый токен той
   же персоны (счёт сохраняется).
3. **Подключиться** к `wsUrl` — это относительный URL `/ws?room=CODE&token=SESSION`: добавьте origin
   сервера и замените схему на `ws://` / `wss://`. Дальше — `WELCOME`, `SNAPSHOT` и игра по WebSocket.
4. **Выйти** — `leave` с `sessionToken` (`204`). Игрок идущей игры сохраняет место и счёт (отображается
   отключённым), остальные удаляются из комнаты.

### Создание

`POST /rooms` — обязателен только `packId`:

```json
{
  "packId": "0192…",
  "name": "Пятничная игра",
  "password": "1234",
  "showman": "human",
  "buzzerPreset": "wifiParty",
  "maxPlayers": 6,
  "allowViewers": true
}
```

| Поле | По умолчанию | Примечание |
|---|---|---|
| `name` (≤ 80) | имя пака | |
| `password` | нет | `X-Host-Token` обходит пароль |
| `showman` | `human` | `ai` / `hybrid` требуют настроенного ИИ-судьи, иначе `422` (`aiNotConfigured`) |
| `rules` | `DefaultRules` SIGame | **полный** объект `Rules` |
| `times` | `DefaultTimeSettings` | **полный** объект таймеров (мс) |
| `buzzerPreset` | `wifiParty` | `lanWired`, `wifiParty`, `internetFair`, `tournament`, `noRace` |
| `buzzer` | из пресета | полные настройки кнопки; **заменяют пресет целиком**, валидируются |
| `maxPlayers` | максимум сервера (`/system/info.maxPlayers`) | 1..максимум |
| `allowViewers` | `true` | |
| `hybridConfirmMs` | 8000 | hybrid: через сколько мс вердикт ИИ применяется без решения ведущего |
| `language` | — | подсказка языка для ИИ-судьи |

Ответ `201`, `Location: /api/v1/rooms/{code}`:

```json
{
  "room": {
    "id": "0192…", "code": "K7Q2M", "name": "Пятничная игра", "packId": "0192…", "packName": "Кино 90-х",
    "status": "lobby", "showman": "human", "hasPassword": true, "players": [], "viewers": 0,
    "maxPlayers": 6, "allowViewers": true, "hybridConfirmMs": 8000, "joinMode": "any",
    "createdAt": 1737000000000, "updatedAt": 1737000000000,
    "rules": { "mode": "classic", "falseStart": true, "…": "…" },
    "times": { "questionSelectionMs": 30000, "…": "…" },
    "buzzer": { "mode": "anchoredHybrid", "netProfile": "wifi", "…": "…" }
  },
  "hostToken": "b3JkZXJfb2ZfdGhlX3Bob2VuaXg…",
  "joinUrl": "http://192.168.1.5:8080/?room=K7Q2M"
}
```

`joinUrl` — подсказка для шаринга: первый join-URL сервера (`SIGAME_PUBLIC_URL` или первый LAN-адрес) плюс
`?room=CODE`; клиентская страница может прочитать код из него. Самому API нужен только `code`.

Ошибки: `422` — пак не найден (`body.packId`), в паке нет раундов, неверные параметры/настройки кнопки,
пресет `noRace` (в этой версии письменная дуэль не реализована — `unsupportedBuzzerMode`); `503` — достигнут
`SIGAME_MAX_ROOMS`.

### Просмотр

`GET /rooms` → `{"items": [RoomView…]}` — все открытые комнаты, новые первыми. `GET /rooms/{code}` — одна
комната (код регистронезависим), `404` после закрытия. `RoomView` не содержит секретов; `players` — все
участники:

```json
"players": [
  { "id": "0192…", "name": "Оля", "role": "showman", "connected": true, "score": 0, "isHost": true },
  { "id": "0192…", "name": "Дима", "role": "player", "connected": false, "score": 300, "isHost": false }
]
```

### Вход и роли

`POST /rooms/{code}/join`, тело `{"name": "Дима", "role": "player", "password": "1234"}`; `role` —
`player` (по умолчанию), `showman`, `viewer`. Заголовок `X-Host-Token` — необязательный (см. выше).

```json
{ "sessionToken": "LfwPo6j_…", "personId": "0192…", "role": "player", "isHost": false,
  "roomCode": "K7Q2M", "wsUrl": "/ws?room=K7Q2M&token=LfwPo6j_…" }
```

| Статус | `errors[0].value` | Когда |
|---|---|---|
| `403` | `badPassword` | неверный или отсутствующий пароль |
| `403` | `invalidHostToken` | передан `X-Host-Token`, но он не от этой комнаты |
| `403` | `banned` | имя забанено (kick с `ban: true`) |
| `404` | — | комнаты нет |
| `409` | `nameTaken` | имя занято подключённой персоной или персоной другой роли |
| `409` | `roomFull` | нет свободного места игрока (`maxPlayers`, а в игре — 12) |
| `410` | — | комната закрыта |
| `422` | `badRole` | место ведущего занято, ведущий — ИИ, зрители запрещены |
| `423` | `joinClosed` | `joinMode` = `closed` (или `viewersOnly` для не-зрителей) |

Ведущий (`showman`) — один на комнату; в режиме `ai` его место занимает псевдо-персона `ai-showman`.
Хост (`isHost`) — административная роль поверх любой из трёх; он управляет игрой по WebSocket (`START`,
`KICK`, `SET_HOST`…) и через REST ниже.

### Host-эндпоинты

Требуют заголовок `X-Host-Token` (из `POST /rooms`): без него `401`, с чужим — `403`. Токен привязан к
комнате, а не к персоне: после `transfer-host` он продолжает работать.

- `PATCH /rooms/{code}/settings` → `200 RoomView`. Меняются только переданные поля:
  `name`, `password` (`""` снимает пароль), `joinMode` (`any` | `viewersOnly` | `closed`), `showman` и `times`
  (только в лобби), `rules` — подмножество, допустимое по ходу игры (`RulesPatch`: `oral`, `managed`,
  `falseStart`, `readingSpeed`, `partialText`, `useAppellations`, `displayAnswerOptionsLabels`,
  `buttonBlockingMs`, `partialImageMs`), `buzzer` (полные настройки) **или** `buzzerPreset`. Все участники
  получают `ROOM_SETTINGS`. `409` — недопустимо в текущем состоянии (`badState`), `422` — неверные значения.

```json
{ "name": "Финал", "buzzerPreset": "tournament", "rules": { "oral": true } }
```

- `POST /rooms/{code}/kick` `{"personId": "…", "ban": true}` → `204`. Персона получает `KICKED` и код закрытия
  4002; `ban` блокирует имя (без учёта регистра). Хоста выгнать нельзя (`403`), неизвестная персона — `404`.
- `POST /rooms/{code}/unban` `{"personId": "…"}` → `204`; `404`, если бана нет.
- `POST /rooms/{code}/transfer-host` `{"personId": "…"}` → `204`; комната получает `HOST_CHANGED`.
- `DELETE /rooms/{code}` → `204`: всем `ROOM_CLOSED{reason:"host"}` и закрытие 1000, результат сохраняется,
  код перестаёт находиться (закрытие асинхронное; `GET` вскоре отвечает `404`).
- `GET /rooms/{code}/buzz-log?questionId=` → `{"arms": [ArmRecord…]}` — журнал кнопки: по каждому розыгрышу
  публичный результат (победитель, ранжирование, зазор, правило разрешения ничьей, seed) и аудит каждого
  нажатия (серверное и клиентское время, кредит, итоговое время, источник доверия, флаги, модель часов).
  Это те же данные, что хост получает в `BUTTON_AUDIT`; в лобби список пуст.

### Кнопка: режимы и пресеты

Настройки кнопки (`buzzer.Settings`) описаны полем за полем в `/openapi.json` (схема `Settings`) и в
[`buzzer.md`](buzzer.md). Ключевые поля: `mode`, `netProfile` (`lan` | `wifi` | `wan`), `tieMode`
(`resolution` | `uncertainty`), `tieBreak` (`likelihood`, `mostLikely`, `random`, `lowestScore`,
`fewestButtonsWon`, `rotate`, `allPlay`), `armJitterMinMs`/`armJitterMaxMs`, `pressWindowMs`, `maxCollectMs`,
`tolCapMs`, `falseStartLockoutMs`, `maxRttMs`, `autoTrust`, `strictTrust`, `showPing`.

| `mode` | Поведение |
|---|---|
| `anchoredHybrid` | честная кнопка: запланированная «лампочка» + клиентские метки реакции с привязкой к RTT (по умолчанию) |
| `serverArrival` | SIGame FirstWins: побеждает первое нажатие, дошедшее до сервера |
| `randomWindow` | SIGame RandomWithinInterval: случайный из нажатий в окне `randomWindowMs` |
| `clientReaction` | SIGame FirstWinsClient: доверять реакции, сообщённой клиентом (небезопасно) |
| `writtenAll` | без гонки — все отвечают письменно; **в этой версии не поддерживается** (`422`) |

`GET /buzzer-presets` → `{"default": "wifiParty", "presets": [...]}`; `GET /buzzer-presets/{name}` — один
пресет (`404` для неизвестного имени):

```json
{ "name": "internetFair", "title": "Через интернет / Fair internet",
  "description": "Use when someone joins over the internet: wider collection window and tolerance …",
  "supported": true,
  "settings": { "mode": "anchoredHybrid", "netProfile": "wan", "tieMode": "uncertainty", "tieBreak": "likelihood",
                "maxCollectMs": 600, "tolCapMs": 80, "maxRttMs": 350, "…": "…" } }
```

| Пресет | Когда |
|---|---|
| `lanWired` | все в одной проводной сети: ничьи по разрешению таймера (2 мс), короткие блокировки |
| `wifiParty` | домашняя игра по Wi-Fi (по умолчанию): ничьи по погрешности измерений, пинг виден всем |
| `internetFair` | кто-то играет через интернет: шире окно сбора и допуск, удалённые игроки не в проигрыше |
| `tournament` | турнир в проводной сети: строгая лестница доверия, без кредита за позднюю лампочку, детерминированный `mostLikely` |
| `noRace` | без гонки, письменная дуэль — `supported: false` до реализации |

Рецепт клиента: взять `settings` пресета, поменять нужное и отправить как `buzzer` (в `POST /rooms` или
`PATCH …/settings`).

## 5. ИИ-ведущий

Судья ответов через OpenRouter (см. [`ai-showman.md`](ai-showman.md)); нужен для `showman: ai | hybrid`.

- `GET /ai/status` — без обращения к сети: `configured`, `model`, `baseUrl`, счётчики вызовов, `lastError`,
  `lastLatencyMs`, `totalTokens`.
- `GET /ai/models` — список моделей провайдера (id, имя, контекст, модальности) для выбора `OPENROUTER_MODEL`;
  `503`, если ключ не задан.
- `POST /ai/test` — один реальный вызов судьи. Тело необязательно; без `right` судится встроенный образец:

```json
{ "language": "ru", "questionText": "Столица Франции?", "right": ["Париж"], "playerAnswer": "париж" }
```
```json
{ "right": true, "factor": 1, "uncertain": false, "source": "ai", "reason": "…" }
```

`503` — ИИ не настроен, `502` — ошибка провайдера, `504` — таймаут.

## 6. Система

- `GET /system/health` → `{"status": "ok", "uptimeSec": 120, "dbOk": true}`; при недоступной БД — `503` с тем
  же телом и `status: "degraded"`.
- `GET /system/info` — версия, Go/OS/arch, `startedAt`, `lanAddresses`, `joinUrls` (готовые ссылки для
  игроков, публичный URL первым), `ffprobe`, `aiConfigured`, `limits` (лимиты медиа и `.siq` в МиБ),
  `maxPlayers`.

## 7. Все эндпоинты

Пути JSON-операций — относительно `/api/v1`; `/media/{id}` и `/ws` — от корня.

| Метод | Путь | operationId | Назначение |
|---|---|---|---|
| GET | `/packs` | `listPacks` | страница кратких описаний с фильтрами |
| POST | `/packs` | `createPack` | создать пак |
| GET | `/packs/{id}` | `getPack` | полный пак (+`ETag`) |
| PUT | `/packs/{id}` | `updatePack` | заменить пак (`If-Match`) |
| DELETE | `/packs/{id}` | `deletePack` | удалить пак |
| POST | `/packs/{id}/duplicate` | `duplicatePack` | глубокая копия |
| POST | `/packs/validate` | `validatePack` | проверить без сохранения |
| POST | `/packs/import` | `importSiq` | импорт `.siq` (multipart, `?dryRun`) |
| GET | `/packs/{id}/export` | `exportSiq` | экспорт в `.siq` v5 |
| POST | `/packs/{id}/rounds` | `addRound` | добавить раунд |
| PUT | `/packs/{id}/rounds/{roundId}` | `updateRound` | заменить раунд |
| DELETE | `/packs/{id}/rounds/{roundId}` | `deleteRound` | удалить раунд |
| POST | `/packs/{id}/rounds/reorder` | `reorderRounds` | порядок раундов |
| POST | `/packs/{id}/rounds/{roundId}/themes` | `addTheme` | добавить тему |
| PUT | `…/themes/{themeId}` | `updateTheme` | заменить тему |
| DELETE | `…/themes/{themeId}` | `deleteTheme` | удалить тему |
| POST | `…/themes/reorder` | `reorderThemes` | порядок тем |
| POST | `…/themes/{themeId}/questions` | `addQuestion` | добавить вопрос |
| PUT | `…/questions/{questionId}` | `updateQuestion` | заменить вопрос |
| DELETE | `…/questions/{questionId}` | `deleteQuestion` | удалить вопрос |
| POST | `…/questions/reorder` | `reorderQuestions` | порядок вопросов |
| POST | `/media` | `uploadMedia` | загрузить файл (multipart, `?kind`) |
| GET | `/media/{id}` | `getMediaMeta` | метаданные |
| DELETE | `/media/{id}` | `deleteMedia` | удалить объект |
| GET | `/media/{id}` (корень) | `streamMedia` | байты с Range/ETag/кэшем |
| GET | `/rooms` | `listRooms` | открытые комнаты |
| POST | `/rooms` | `createRoom` | создать комнату → `hostToken` |
| GET | `/rooms/{code}` | `getRoom` | одна комната |
| POST | `/rooms/{code}/join` | `joinRoom` | войти → `sessionToken`, `wsUrl` |
| POST | `/rooms/{code}/leave` | `leaveRoom` | выйти |
| PATCH | `/rooms/{code}/settings` | `updateRoomSettings` | (host) изменить настройки |
| POST | `/rooms/{code}/kick` | `kickPerson` | (host) выгнать / забанить |
| POST | `/rooms/{code}/unban` | `unbanPerson` | (host) снять бан |
| POST | `/rooms/{code}/transfer-host` | `transferHost` | (host) передать права хоста |
| DELETE | `/rooms/{code}` | `closeRoom` | (host) закрыть комнату |
| GET | `/rooms/{code}/buzz-log` | `getBuzzLog` | (host) журнал кнопки |
| GET | `/buzzer-presets` | `listBuzzerPresets` | пресеты кнопки |
| GET | `/buzzer-presets/{name}` | `getBuzzerPreset` | один пресет |
| GET | `/ai/status` | `getAIStatus` | состояние ИИ-судьи |
| GET | `/ai/models` | `listAIModels` | модели провайдера |
| POST | `/ai/test` | `testAI` | тестовый вызов судьи |
| GET | `/system/health` | `getHealth` | health-check |
| GET | `/system/info` | `getSystemInfo` | версия, адреса, лимиты |
| GET | `/ws?room=&token=` | — | WebSocket, см. [`protocol.md`](protocol.md) |

## 8. Дальше

Всё после `join` — WebSocket: конверты `{"t","seq","p"}`, `WELCOME`/`SNAPSHOT`/`RESUME`, чат, синхронизация
часов и кнопка (`SYNC`, `BUTTON_ARM`, `PRESS`, `BUTTON_RESULT`), вердикты ИИ — в [`protocol.md`](protocol.md);
игровые события и команды (`STAGE`, `TABLE`, `CHOOSE`, `ANSWER`, `VALIDATE`…) — в
[`protocol-engine.md`](protocol-engine.md).
