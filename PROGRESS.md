# SIGame Server — прогресс работ (состояние на 2026-09-18, ~23:10)

Задача: бекенд игры «Своя игра» (SIGame-совместимый) — паки с вопросами (свои + импорт/экспорт `.siq`),
игра онлайн по локальной сети, современный стек 2026, API-документация, честная кнопка при разном пинге,
поддержка аудио/видео/картинок/гифок. Только бекенд (без UI).

## 1. Принятые решения (подтверждены пользователем)
| Вопрос | Решение |
|---|---|
| Стек | **Go 1.27** + chi v5.3.2 + huma v2.39.1 (OpenAPI 3.1 + Scalar UI) + coder/websocket v1.8.15 + modernc.org/sqlite v1.59.0 (CGO_ENABLED=0) + goose v3 + uuid v7 + slog |
| Формат паков | Свой JSON-формат (надмножество SIQ v5) + **импорт .siq v4/v5 + экспорт .siq v5** |
| Ведущий | Человек-ведущий **и** ИИ-ведущий через OpenRouter, модель `tencent/hy4-preview` (уточнён ID: "Tencent: Hy4 preview", 1M контекст, только текст), настраивается через `OPENROUTER_MODEL`; fuzzy-match как fallback |
| Клиент | Только бекенд + API-доки (без тестового веб-клиента) |
| Честная кнопка | **AnchoredHybrid** — вывод панели из 3 независимых дизайнов + 3 адверсариальных судей (см. ниже) |

Полный список обязательных архитектурных решений — `docs/design/00-constitution.md`
(структура репозитория `cmd/sigame`, `internal/{config,httpapi,ws,room,engine,buzzer,packs,siq,media,ai,db,lan,clock}`,
конвенции времени (мс), ID (UUIDv7), envelope WS `{"t","seq","p"}`, проекции по ролям, room = single-goroutine actor).

## 2. Что сделано
1. **Исследование** (воркфлоу `sigame-research`, 11 агентов, 564 вызова инструментов, ~43 мин) — результаты в `docs/research/`:
   - `01-sigame-rules.md` — полная спецификация правил из исходников оригинального движка (VladimirKhil/SI):
     роли, все типы вопросов (simple/stake/stakeAll/secret/secretPublicPrice/secretNoQuestion/noRisk/forAll/custom) со скриптами,
     алгоритм торгов, кот в мешке, финал, апелляции, таймеры с дефолтами, настройки, протокол SICore (~140 сообщений).
   - `02-siq-format.md` — спецификация .siq v4 и v5 для импортёра/экспортёра (ZIP-раскладка, content.xml, URL-энкодинг имён медиа,
     миграция v4→v5, лимиты, чек-лист экспорта, список существующих парсеров).
   - `03-existing-implementations.md` — обзор реализаций (оригинальный сервер SIGame закрыт; svoyak, OpenQuester и др.) и уроки.
   - `04-stack-2026.md` — сравнение стеков с проверенными версиями (Go выбран пользователем).
   - `05-netcode-research.md` — исследование проблемы пинга (SIGame: RandomWithinInterval/FirstWins/FirstWinsClient — последний подделывается).
   - `06-fairness-proposal-{1,2,3}.md` — три независимых дизайна честной кнопки (SyncedHybrid / ServerRewind / FairBuzz).
   - `07-fairness-judge-{1,2,3}.md` — оценки судей + **синтез AnchoredHybrid** (все трое сошлись): планируемое зажигание кнопки
     по синхронизированным часам + клиентская метка реакции (точна для честных), якоря RTT, которые нельзя подделать из JS
     (WS ping/pong + **kernel TCP_INFO — доступен в Go**), reject-not-clamp, окно сбора, tie-break с HMAC-RNG, trust-ladder,
     пресеты (lanWired/wifiParty/internetFair/tournament/noRace), письменный режим для forAll.
2. **Конституция проекта** — `docs/design/00-constitution.md` (написана мной, обязательна для всех спецификаций и кода).
3. **Фикстуры**: 58 официальных тест-паков (уникальных по имени; в репо SI 61 путь, 3 дубля) `.siq` (все v5, все типы вопросов и медиа) — `testdata/siq/`; XSD-схемы
   `siq_5.xsd`, `ygpackage3.1.xsd`, `QuestionsTypes.xml`, README формата — `testdata/siq-spec/`.
4. **Окружение**: Go 1.27.1 установлен (`/opt/homebrew/bin/go`); ffmpeg/ffprobe есть; кэш модулей прогрет
   (chi, huma, coder/websocket, modernc sqlite, goose, uuid, testify, caarlos0/env, x/sys) — сборка с `CGO_ENABLED=0` проверена.
5. **Проверено в исходниках**: `coder/websocket` даёт `Conn.Ping(ctx)` и колбэк `OnPongReceived` (метка pong без участия JS);
   `golang.org/x/sys/unix` даёт `GetsockoptTCPInfo` (Linux) и `GetsockoptTCPConnectionInfo` (macOS, `Srtt/Rttvar/Rttcur`);
   сырой `net.Conn` до апгрейда берётся через `http.Server.ConnContext`.
6. **Воркфлоу проектирования** `sigame-design` был запущен (8 авторов спецификаций → 3 критика → фиксеры) и **остановлен по просьбе
   пользователя** до завершения — ни одна из 8 спецификаций не была дописана. Скрипт сохранён:
   `~/.claude/projects/-Users-ruslan-Desktop-Projects-game/e9469d22-7549-43b6-b848-6869308c73e9/workflows/scripts/sigame-design-wf_b9113946-4d7.js`
   (запускать заново целиком; кэш результатов пуст).

## 2a. Решения от 2026-09-18 (вечер, второй раунд вопросов)
- Документация: код/OpenAPI/комментарии — английский; руководства (README, кнопка, ops) — русский.
- Git: инициализирован (`main`), коммиты по этапам.
- Порядок: сразу код по конституции + исследованию (без отдельного этапа спецификаций); контракты зафиксированы в
  `internal/packs/model.go`, `internal/media/media.go`, `internal/clock`, `CLAUDE.md`; зависимости запинены в `go.mod`/`tools.go`.
- Сеть: LAN по умолчанию (пресет кнопки wifiParty, mDNS/QR), интернет поддерживается (пресет internetFair, внешний URL).
- Блокер с доступом к Desktop **снят** (доступ выдан).

## 3. Блокер (снят)
**macOS не даёт процессу терминала доступ к `~/Desktop`** (TCC): файлы в `~/Desktop/Projects/game` создаются, но **не читаются**
(`Operation not permitted`), поэтому собирать/тестировать код там нельзя. Варианты:
- Системные настройки → Конфиденциальность и безопасность → «Файлы и папки» (или «Полный доступ к диску») → включить
  «Рабочий стол» для приложения-терминала (Terminal/iTerm/Warp/VS Code), затем перезапустить `claude`;
- либо перенести проект, например в `~/Projects/game` (`~/Documents`, `~/Downloads` читаются нормально).

Резервная копия всех артефактов (research, design, .siq-фикстуры, XSD, тестовый модуль modwarm): `~/Documents/sigame-scratch-backup/`.

## 4. Следующие шаги
1. Решить блокер с доступом к Desktop (или переместить проект).
2. Перезапустить воркфлоу проектирования (скрипт выше) → получить `docs/design/10..80-*.md`
   (домен+БД, REST API, WS-протокол+движок, спецификация кнопки, кодек .siq, ИИ-ведущий, медиа, ops/тесты) → прочитать, утвердить.
3. Реализация по этапам (каждый — отдельный воркфлоу с ревью): скелет + config/db/media → packs + siq import/export + REST (OpenAPI) →
   engine (все типы вопросов, финал, апелляции) с табличными тестами → buzzer AnchoredHybrid + детерминированный симулятор →
   ws/room actor + resume → ai judge (OpenRouter, fake-сервер в тестах) → интеграционный тест «3 игрока + ИИ-судья» → docs.
4. Документация: `docs/README.md`, `api.md` (OpenAPI — источник истины, `/docs` Scalar), `protocol.md`, `buzzer.md`, `packs.md`, `ops.md`.

## 5. Заметки
- Модуль Go: `sigame` (без VCS-хоста). Время на проводе — мс (int64). Медиа — content-addressed (sha256) на диске, HTTP Range через `http.ServeContent`.
- `index.html` в корне — отдельная мини-игра (Happy Wheels-like на Matter.js), к бекенду отношения не имеет.
