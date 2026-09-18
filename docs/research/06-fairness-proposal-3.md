# FairBuzz: синхронный сигнал по расписанию + ограниченная клиентская дельта + настраиваемые хостом правила честности (Sync-Cue / Bounded-Delta / Tie-Rules) (complexity: medium)

## Mechanism
## 0. Идея в одном абзаце

Кнопка делится на три слоя с одним узким интерфейсом между ними:

1. **Измерение** — сервер непрерывно знает по каждому игроку `rtt`, `σ` (джиттер) и статус синхронизации часов.
2. **Компенсация (неткод)** — «лампочка» загорается у всех в один и тот же момент серверного времени по заранее разосланному расписанию (а не «когда пакет дошёл»), а каждое нажатие превращается в оценку физического момента `tPress ± err` в серверных часах. Клиентская дельта реакции принимается только в пределах, которые сервер может проверить по измеренному RTT (compensation bounded by RTT).
3. **Игровой слой (game design)** — хост выбирает, что делать с нажатиями, которые в пределах `err` неотличимы: считать «одновременными» и разыгрывать по правилу (random / отстающему / по кругу / письменная дуэль), показывать открытый гандикап, или вовсе убрать гонку и дать всем ответить письменно за T секунд (SIQ v5 `forAll`, а также любые типы по решению хоста).

Ключевое свойство композиции: неткод отдаёт наверх `Press{tPress, err, source}`, игровой слой никогда не видит сырых сетевых времён. Даже в самом «жёстком» режиме `first` окно одновременности не может быть меньше суммы ошибок измерения — так мы не выдаём 5-миллисекундную разницу за факт, когда точность 20 мс.

Отличия от оригинальной SIGame (SICore `Game.cs:2353–2434`): там `TRY` рассылается «как дойдёт», клиентская дельта в `FirstWinsClient` принимается без проверки, окно `ButtonsAccepting = 300 мс` фиксировано, RTT не измеряется. У нас — расписание, верификация, динамическое окно и правила тай-брейка.

---

## 1. Синхронизация часов и измерение задержки

- Транспорт: WebSocket (JSON или MessagePack). Сервер использует **монотонные** часы (`performance.now()` / `process.hrtime`), публикуемые как `serverTs` (мс, double).
- При подключении/реконнекте — burst из 8 `PING` с шагом 100 мс, затем 1 `PING`/с. Каждый `PING{seq, t1}` → `PONG{seq, t1, t2, t3}`; клиент фиксирует `t4`.
  - `rtt = (t4 − t1) − (t3 − t2)`, `offset = ((t2 − t1) + (t3 − t4)) / 2` (клиент → серверные часы).
  - Клиент хранит 20 последних замеров; `offset` берётся из замера с **минимальным RTT** в окне (стиль timesync/Protobowl — минимальный RTT = наименьшая асимметрия и наименьшая ошибка).
  - Сервер по игроку хранит `rttMin`, `rttEwma` (α = 1/8), `rttVar` (β = 1/4, схема TCP RTTVAR), `rttLast`, `σ = sqrt(rttVar)`. Клиент в каждом `PING` присылает свой `off` и `offErr` только для отображения/аудита.
- `synced = samples ≥ 5 && σ < 100 мс`. Пока не synced — кнопка работает в режиме «только серверная оценка», UI показывает «синхронизация…».
- Все игроки получают `LATENCY_UPDATE` раз в 2 с: `{playerId, rttMs, jitterMs, handicapMs, mode}` — открытая индикация пинга и гандикапа (требование «per-player latency handicap displayed openly»).

## 2. Arming по расписанию: все видят лампу одновременно

Когда чтение вопроса (и медиа) закончилось, сервер вычисляет момент включения кнопки в **серверном** времени:

```
lead  = clamp(max_j(rttEff_j) + 60, 100, 600)        // чтобы расписание успело дойти до самого медленного
delay = arming == 'randomLamp' ? U(armingDelayMinMs, armingDelayMaxMs)   // 300..1500 мс, скрыто от игроков
      : arming == 'countdown'  ? countdownMs                              // 1000 мс, виден отсчёт 3-2-1
      : 0                                                                 // 'immediate' (совместимость)
armAt = now + lead + delay
```

- Сервер шлёт `BUTTON_SCHEDULE{questionId, armNonce, armAt, countdownVisible, countdownMs, deadlineAt}` всем, кто может жать, сразу (в момент `now`).
- Клиент переводит `armAt` в локальное время через `offset`, ставит таймер и включает лампу **ровно в `armAt`**, запоминая `cueLocal = performance.now()`. Если расписание пришло уже после `armAt` (реконнект, не synced) — включает лампу немедленно и помечает `late = true`.
- В `armAt` сервер дублирует `BUTTON_ARMED{armNonce, serverTs}` — клиенты, уже включившие лампу, игнорируют; несинхронизированные включают по получению.
- Эффект: это **delay equalization без придерживания пакетов** (идея Zander 2005 / US11611510 / `UsePingPenalty`), но никто не ждёт самого медленного — ожидание `lead` спрятано в естественной паузе между чтением и лампой. Ошибка момента лампы: LAN < 1 мс, Wi‑Fi ≈ джиттер, WAN ≤ RTT/2 (теоретический предел NTP).
- В `randomLamp` случайная пауза убивает антиципацию «по ритму чтения» (Jeopardy-эффект < 20 мс). В `countdown` антиципация разрешена и равна для всех (как на ТВ) — поэтому сервер **запрещает** сочетание `countdown + resolution=first` (валидация настроек), иначе выиграет тот, кто лучше угадывает, или бот.

## 3. Метка времени нажатия (гибрид с ограниченным доверием)

Клиент на `pointerdown`/`keydown` (`event.isTrusted === true`) считает `reactionMs = performance.now() − cueLocal` и шлёт `BUZZ{armNonce, reactionMs, seq, late, inputKind, evtTs}`, сразу локально показывая «нажато · 240 мс». Сервер в момент `tRecv`:

```ts
function timestampPress(p: Player, b: Buzz, tRecv: number, arm: Arming, s: BuzzerSettings): Press | 'IGNORED' | 'FALSE_START' {
  if (b.armNonce !== arm.nonce || p.pressedThisArm || p.lockedUntil > tRecv) return 'IGNORED';
  const rttEff = Math.max(p.rttEwma, p.rttLast);        // TCP-столл виден в пинге, ушедшем перед BUZZ
  const sigma  = Math.sqrt(p.rttVar);
  const est    = tRecv - rttEff / 2;                    // серверная оценка (BuzzIn.Live-подобная)
  const claim  = arm.armAt + b.reactionMs;              // клиентская оценка; не зависит от offset — обе метки локальные
  const minReaction = arm.mode === 'countdown' ? 0 : s.minReactionMs;   // 100 мс при скрытой лампе
  if (b.reactionMs < minReaction) { misfire(p, tRecv, 'tooFast'); return 'FALSE_START'; }

  const serverErr = rttEff / 2 + 3 * sigma;
  if (s.timestampSource === 'arrival')  return { tPress: tRecv, err: serverErr, source: 'server' };
  const serverOnly = s.timestampSource === 'serverEstimate' || !p.synced || b.late || p.trust === 'serverOnly';
  if (serverOnly)                        return { tPress: est,   err: serverErr, source: 'server' };

  const tol = rttEff / 2 + 3 * sigma + CLOCK_ERR_MS;    // 10 мс запас
  p.biasEwma = ewma(p.biasEwma, est - claim);           // положительный bias = «клиент систематически раньше сети»
  if (claim < est - tol)         { flag(p, 'earlyClaim');   return { tPress: est, err: serverErr, source: 'rejected' }; }
  if (claim > tRecv + CLOCK_ERR_MS) { flag(p, 'clockAnomaly'); return { tPress: est, err: serverErr, source: 'rejected' }; }
  return { tPress: claim, err: INPUT_ERR_MS /* 20 мс: конвейер ввода + дисплей */, source: 'client' };
}
```

Важно: отвергнутое утверждение заменяется **нейтральной оценкой `est`**, а не границей допуска — читер не может получить больше, чем честный игрок. Максимальная «выгода» от подделки внутри допуска ограничена `rttEff/2 + 3σ + 10` (на проводной LAN ≈ 15–20 мс, на Wi‑Fi ≈ 60–70 мс, на WAN 250 мс ≈ 190 мс — это и есть причина, по которой для WAN рекомендуются `window` + тай-брейк, см. §5 и Anti-cheat).

## 4. Окно сбора (collect window)

При первом принятом нажатии в `tA1`:

```
collectMs = min( W_eff + max_{j armed, not pressed}(rttEff_j/2 + 3σ_j) + 10 , MAX_COLLECT_MS = 600 )
deadline  = tA1 + collectMs
```

- Ждём до `deadline` или до момента, когда все armed-игроки нажали/заблокированы (ранний выход).
- Нажатия после `deadline` → `BUZZ_LATE` (в лог, решение не пересматривается — принцип NAQT «не опротестовывается»); сервер увеличивает `σ` игрока (штрафной замер), чтобы следующее окно было шире.
- Если до `armAt + thinkMs` никто не нажал → `BUTTON_DISARMED{reason:'timeout'}` (аналог `ENDTRY`).

## 5. Игровой слой: решение и режимы честности

```ts
function decide(presses: Press[], s: BuzzerSettings, g: GameState): Decision {
  const adj = presses.map(x => ({ ...x, t: x.tPress + handicapMs(g, x.playerId, s) }))  // гандикап открыт всем
                     .sort((a, b) => a.t - b.t);
  const first = adj[0];
  const wHost = s.resolution === 'first' ? 0 : (s.windowMs ?? autoWindow(g));   // autoWindow = clamp(2·maxσ + 20, 30, 250)
  const tie = adj.filter(x => x.t - first.t <= Math.max(wHost, first.err + x.err)); // неотличимые в пределах ошибки — всегда tie
  if (tie.length === 1) return { winner: first.playerId, tie: [first.playerId], rule: 'first' };
  switch (s.tieBreak) {
    case 'random':           return { winner: pick(tie, g.rng), tie, rule: 'window:random' };
    case 'lowestScore':      return { winner: pick(minBy(tie, p => g.score[p]), g.rng), tie, rule: 'window:lowestScore' };
    case 'fewestButtonWins': return { winner: pick(minBy(tie, p => g.buttonWins[p]), g.rng), tie, rule: 'window:fewestWins' };
    case 'rotate':           return { winner: nextInSeatOrderAfter(g.lastWinner, tie), tie, rule: 'window:rotate' };
    case 'writtenDuel':      return { mode: 'written', participants: tie.map(x => x.playerId), rule: 'window:writtenDuel' };
  }
}
```

Настройки комнаты (`buzzer` в настройках хоста):

| Поле | Значения | Смысл |
|---|---|---|
| `arming` | `randomLamp` (default) / `countdown` / `immediate` | как включается кнопка |
| `timestampSource` | `hybridBounded` (default) / `serverEstimate` / `arrival` | откуда берётся `tPress` |
| `resolution` | `window` (default) / `first` / `allPlay` | что считать победой |
| `windowMs` | `auto` (default) / 30..500 | окно одновременности; `auto` = `clamp(2·maxσ + 20, 30, 250)` |
| `tieBreak` | `random` (default, как SIGame) / `lowestScore` / `fewestButtonWins` / `rotate` / `writtenDuel` | правило внутри окна |
| `handicap` | `none` / `adaptive` / `manual` | +мс к `tPress`; всегда показывается всем |
| `allPlayFor` | `['forAll']` (default) / `['forAll','simple']` / `'all'` | какие типы вопросов идут письменно без гонки |
| `writtenAnswerMs`, `writtenScoring` | 20000; `accuracy` (default) / `speedBonus` | письменный режим |
| `falseStartLockoutMs`, `lockoutGrowth`, `lockoutCapMs` | 2000; ×2 в течение 5 с; 8000 | фальстарты |
| `minReactionMs` | 100 (только при скрытой лампе) | античит-порог |

Пресеты (хост выбирает один, дальше можно крутить поля):

- **`lanWired`** — «Проводная LAN»: `randomLamp 300–800`, `hybridBounded`, `resolution=first` (окно = только ошибка измерения ≈ 20–40 мс), `lockout 1000`.
- **`wifiParty`** (default) — `randomLamp 300–1500`, `hybridBounded`, `window auto (≥ 80)`, `tieBreak random`, `lockout 2000`, `allPlayFor ['forAll']`.
- **`internetFair`** — `randomLamp`, `hybridBounded`, `window auto (≥ 120)`, `tieBreak lowestScore`, `handicap adaptive`, `allPlayFor ['forAll']`, предупреждение хосту при `rtt > 400`.
- **`noRace`** — «Без гонки»: `resolution=allPlay` для всех обычных вопросов, `writtenScoring accuracy`; кнопка отключена, `stake/secret/noRisk` играются по своим правилам без кнопки.

Гандикап: `manual` — хост задаёт `handicapMs[player]` (0..300), как в гольфе; `adaptive` — +15 мс за каждую выигранную кнопку, −15 за проигранную гонку, clamp [0, 120], сброс между играми. Показывается рядом с пингом: «ping 80±8 · гандикап +30».

## 6. Режим «все отвечают письменно» (allPlay)

- Триггеры: тип вопроса ∈ `allPlayFor` (в SIQ v5: `forAll`; при желании хоста также `simple`), или `resolution=allPlay`, или `tieBreak=writtenDuel` для tie-группы.
- Сервер: `WRITTEN_PHASE{questionId, participants, deadlineAt, scoring}`; клиенты показывают поле ввода; ответ `WRITTEN_ANSWER{questionId, text, reactionMs}`; после `deadlineAt` (или когда все ответили) — `WRITTEN_RESULT{answers:[{playerId, text, correct, delta}]}`. Проверка: авто-сопоставление с `right` из пакета (нормализация регистра/ё/пунктуации), хост может переопределить.
- Очки: `accuracy` — верно `+price`, неверно `−price` (или 0, если `softWrong=true`); `speedBonus` — `+price·0.25·(1 − reactionMs/T)` сверху, только для верных (Kahoot-подобно, но время — клиентское локальное, а не по приходу, поэтому пинг не влияет). Порядок ответов не влияет на результат — гонки нет по определению.

## 7. Фальстарт и лок-аут

- Клиент знает `armAt`, поэтому нажатие до лампы он блокирует **сам мгновенно** (рамка краснеет) и шлёт `MISFIRE{questionId, earlyByMs}`; сервер — авторитет: `lockedUntil = tRecv + falseStartLockoutMs · lockoutGrowth^(k−1)` для k-го фальстарта в течение 5 с, cap `lockoutCapMs`; рассылает `BUTTON_LOCKOUT{playerId, untilAt, reason}`.
- `reactionMs < minReactionMs` при скрытой лампе тоже считается фальстартом (человек не реагирует быстрее ~100 мс на случайный сигнал).

## 8. Разобранный пример (3 игрока)

Игроки: **A** — провод, RTT 10, σ 1; **B** — Wi‑Fi, RTT 80, σ 8; **C** — интернет, RTT 250, σ 20. Счёт: A 1200, B 300, C 500. Пресет `wifiParty`.

1. Чтение закончено в `now = 440`. `lead = clamp(250 + 60, 100, 600) = 310`, `delay = 0` (для наглядности берём нижнюю границу случайной паузы как 0) → `armAt = 750`. `BUTTON_SCHEDULE{armAt: 750}` уходит в 440, доходит: A 445, B 480, C 565 — все до 750. В 750 лампа загорается у всех трёх (ошибки < 1 / ≈ 8 / ≈ 20 мс).
2. Физические нажатия (серверные часы): **A 1000, B 990, C 985** → `reactionMs` = 250 / 240 / 235 (C реально был первым).
3. Приход `BUZZ` на сервер: A `1000 + 5 = 1005`, B `990 + 40 = 1030`, C `985 + 125 = 1110`.
   - **Порядок прихода (`FirstWins` SIGame, Buzzonk) отдал бы кнопку A — ошибка.**
4. Метки:
   - A: `est = 1005 − 5 = 1000`, `tol = 5 + 3 + 10 = 18`, `claim = 750 + 250 = 1000` → |0| ≤ 18 → `tPress = 1000`, err 20, `client`.
   - B: `est = 1030 − 40 = 990`, `tol = 40 + 24 + 10 = 74`, `claim = 990` → `tPress = 990`, err 20, `client`.
   - C: `est = 1110 − 125 = 985`, `tol = 125 + 60 + 10 = 195`, `claim = 985` → `tPress = 985`, err 20, `client`.
5. Окно сбора: `W_auto = clamp(2·20 + 20, 30, 250) = 60` (пресет поднимает до 80). `collectMs = 80 + (125 + 60) + 10 = 275` → `deadline = 1005 + 275 = 1280`. B (1030) и C (1110) успевают; после C все трое нажали → ранний выход в 1110.
6. Решение:
   - `resolution = first` (пресет `lanWired`): сортировка 985 (C), 990 (B), 1000 (A). Неявное окно = `err_C + err_B = 40` → B и A попадают в tie-группу с C (разница 5 и 15 мс меньше точности измерения). При `tieBreak=random` — 1/3 каждому; если хост выставил `windowMs=0` явно (режим «верю измерению»), побеждает **C** — физически первый, что и требовалось.
   - `resolution = window`, `W_eff = 80`, `tieBreak = random` (default `wifiParty`): tie = {C, B, A}, случайный выбор — как `RandomWithinInterval` в SIGame, но окно посчитано от реального разброса, а не фиксированные 300 мс.
   - `tieBreak = lowestScore`: побеждает **B** (300). `rotate`: следующий по посадке после прошлого победителя. `writtenDuel`: все трое печатают ответ 10 с.
7. Попытка читерства. A' (модифицированный клиент) шлёт `reactionMs = 100` → `claim = 850`, `|850 − 1000| = 150 > 18` → отвергнуто, `tPress = est = 1000`, флаг `earlyClaim` → C по-прежнему первый. C' на WAN шлёт `reactionMs = 100` → `claim = 850`, `|850 − 985| = 135 ≤ 195` → принято (в пределах RTT/2 — не проверяемо), **но**: (а) при `window` он лишь попадает в tie-группу, а не выигрывает; (б) `biasEwma` C' ≈ +135 > `max(rtt/4 = 62, 3σ = 60)` → после ≥ 8 нажатий флаг `bias`, `trust = serverOnly`, хост уведомлён; (в) при `randomLamp` реакции ≈ 100 мс с MAD ≈ 0 ловятся детектором «robotic».
8. Стол Wi‑Fi: B словил TCP-столл 300 мс на нажатии → `BUZZ` пришёл в 1330, `rttLast` (пинг перед ним) = 380 → `rttEff = 380`, `est = 1140`, `tol = 190 + 24 + 10 = 224`, `claim = 990`, `|990 − 1140| = 150 ≤ 224` → клиентская метка **принята**, B не пострадал; окно сбора при пересчёте с `rttEff` = 380 достаточно широкое (`deadline ≥ 1005 + 80 + 190 + 60 + 10 = 1345`).

## 9. Потеря пакетов, столлы, реконнект

- WebSocket поверх TCP пакеты не теряет, но **задерживает** (head-of-line). Столлы видны в `rttLast`/`σ` (см. пример 8) и расширяют допуск и окно сбора. Для WAN опционально WebTransport-датаграммы для `PING`/`BUZZ` с дублированием по надёжному стриму (принимается первая копия по `seq`).
- Пропущенные `PONG` ≥ 3 подряд → игрок `degraded`: кнопка в `serverOnly`, значок в UI, окно сбора считается по `rttEwma·2`.
- Реконнект: сессионный токен; сервер отдаёт снапшот с текущим `arming` (если `armAt` в прошлом — клиент включает лампу немедленно с `late=true`); кнопка недоступна до `synced` (burst 8 пингов ≈ 1 с); повторный `BUZZ` идемпотентен по `(armNonce, playerId, seq)`.
- `BUZZ` с неизвестным/просроченным `armNonce` → `IGNORED` + `BUZZ_REJECTED{reason:'staleNonce'}`.
- `BUZZ_LATE` (после `deadline`) — фиксируется, игроку показывается «поздно (сеть)», решение не пересматривается; σ игрока увеличивается.
- Если хост отключился, игра ставится на паузу (`arming` отменяется, `BUTTON_DISARMED{reason:'paused'}`), при возврате — новое расписание.

## 10. Машина состояний вопроса (серверная)

`READING → SCHEDULED(armAt, nonce) → ARMED → COLLECTING(deadline) → DECIDED(winner|tie) → ANSWERING | WRITTEN(deadline) → SCORED`; ветки: `SCHEDULED/ARMED --thinkMs--> TIMEOUT`, `любое --pause--> PAUSED`. Все решения логируются: `{questionId, armAt, presses:[{playerId, tRecv, rttEff, σ, est, claim, tol, tPress, source, flags}], windowMs, rule, winner}` — доступно хосту (`GET .../buzz-log`).

## Protocol
## Сообщения WebSocket (все времена — серверные монотонные мс, double; `c→s` клиент→сервер, `s→c` сервер→клиент)

### Синхронизация (всегда)
```
c→s  PING  {seq, t1}                                  // burst 8×100 мс после connect/reconnect, затем 1/с; + off, offErr для аудита
s→c  PONG  {seq, t1, t2, t3}                          // клиент: rtt=(t4−t1)−(t3−t2), offset=((t2−t1)+(t3−t4))/2
s→c  LATENCY_UPDATE {players:[{playerId, rttMs, jitterMs, synced, handicapMs, trust}]}   // раз в 2 с, всем (открытый пинг/гандикап)
```

### Настройки (REST, только хост)
```
GET   /api/v1/buzzer-presets                              → [{id:'wifiParty', settings:{...}}, ...]
PATCH /api/v1/rooms/{roomId}/buzzer-settings              body: BuzzerSettings (см. таблицу в mechanism); 400 при countdown+first
GET   /api/v1/rooms/{roomId}/buzz-log?questionId=...       → полный журнал решения (est/claim/tol/tPress/flags/rule)
```

### Один вопрос с кнопкой (happy path)
```
s→all  QUESTION_READING {questionId, ...}                       // текст/медиа, кнопка неактивна (falseStart=true)
       -- чтение закончено в now --
s→pl   BUTTON_SCHEDULE {questionId, armNonce, armAt, countdownVisible, countdownMs, deadlineAt}   // отправлено в armAt − lead
       (клиент: таймер на armAt по offset; лампа ровно в armAt; cueLocal = performance.now())
s→pl   BUTTON_ARMED    {questionId, armNonce, serverTs}         // в armAt, резерв для несинхронизированных/опоздавших
c→s    BUZZ            {questionId, armNonce, seq, reactionMs, late, inputKind:'pointer'|'key'|'gamepad', evtTs}
s→c    BUZZ_ACK        {seq, accepted:true, tPress, source:'client'|'server'|'rejected'}   // игрок видит «зарегистрировано»
       -- сервер ждёт до deadline = tA1 + collectMs или пока все armed не нажмут --
s→all  BUTTON_RESULT   {questionId, winnerPlayerId, tie:[playerId...], rule:'first'|'window:random'|'window:lowestScore'|...,
                        windowMs, candidates:[{playerId, tPress, err, source, rttMs, jitterMs, flagged}]}   // прозрачность
s→all  ANSWERING       {playerId, answerTimeMs}                  // дальше обычный поток ответа/оценки хоста
```

### Ветки
```
c→s    MISFIRE         {questionId, earlyByMs}                   // нажатие до armAt (клиент уже заблокировал себя локально)
s→all  BUTTON_LOCKOUT  {playerId, untilAt, reason:'falseStart'|'tooFast'|'spam', strike:k}
s→c    BUZZ_REJECTED   {seq, reason:'staleNonce'|'locked'|'late'|'duplicate'}
s→all  BUTTON_DISARMED {questionId, reason:'timeout'|'answered'|'paused'}
s→all  INTEGRITY_ALERT {playerId, kind:'earlyClaim'|'bias'|'robotic'|'clockAnomaly', stats}   // только хосту (и в лог)
```

### Письменный режим (forAll / allPlay / writtenDuel)
```
s→all  WRITTEN_PHASE   {questionId, participants:[playerId...], deadlineAt, scoring:'accuracy'|'speedBonus'}
c→s    WRITTEN_ANSWER  {questionId, text, reactionMs}            // reactionMs локальное — пинг не влияет
s→all  WRITTEN_RESULT  {questionId, answers:[{playerId, text, correct, delta}], right:[...]}
s→host WRITTEN_REVIEW  {questionId, answers:[...]}               // хост может переопределить авто-проверку
```

### Реконнект
```
c→s    RESUME {sessionToken, lastSeq}
s→c    SNAPSHOT {..., buzzer:{state:'SCHEDULED'|'ARMED'|'COLLECTING'|..., armNonce, armAt, deadlineAt, lockedUntil}}
       → burst 8×PING → synced → кнопка активна (если armAt в прошлом — лампа сразу, late=true)
```

### Последовательность на временной шкале (пример из mechanism §8)
```
440  s→A,B,C  BUTTON_SCHEDULE{armAt:750}     (дошло: A 445, B 480, C 565)
750  лампа у всех; s→all BUTTON_ARMED
985  C жмёт (reaction 235)   →  1110 BUZZ на сервере
990  B жмёт (reaction 240)   →  1030 BUZZ на сервере
1000 A жмёт (reaction 250)   →  1005 BUZZ на сервере (первый по приходу!)
1005 COLLECTING, deadline 1280
1110 все нажали → decide(): tPress C 985 < B 990 < A 1000
1110 s→all BUTTON_RESULT{winner: C (first) | random∈{C,B,A} (window:random) | B (window:lowestScore)}
```

## Parameters
- PING_INTERVAL_MS = 1000 (burst 8 × 100 мс после connect/reconnect)
- RTT_WINDOW_SAMPLES = 20; rttEwma α = 1/8; rttVar β = 1/4 (схема TCP RTO); offset берётся из замера с минимальным RTT
- SYNCED: ≥ 5 замеров и σ < 100 мс; иначе кнопка в serverOnly
- SCHEDULE_LEAD_MS = clamp(max rttEff + 60, 100, 600)
- armingDelayMinMs / armingDelayMaxMs = 300 / 1500 (randomLamp, равномерно); countdownMs = 1000 (countdown)
- CLOCK_ERR_MS = 10 (запас в допуске клиентской дельты)
- INPUT_ERR_MS = 20 (ошибка принятой клиентской метки: конвейер ввода 5–20 мс + дисплей ±16 мс @60 Гц)
- ACCEPT_TOL_i = rttEff_i/2 + 3σ_i + CLOCK_ERR_MS, где rttEff = max(rttEwma, rttLast)
- windowMs = auto → clamp(2·max σ_j + INPUT_ERR_MS, 30, 250); пресеты: lanWired 0 (только неявное окно ошибки), wifiParty ≥ 80, internetFair ≥ 120
- COLLECT_MS = min(W_eff + max_{armed, not pressed}(rttEff_j/2 + 3σ_j) + 10, MAX_COLLECT_MS = 600)
- thinkMs (аналог ENDTRY) = 5000 после armAt (настройка хоста 3000–15000)
- minReactionMs = 100 при скрытой лампе (randomLamp/immediate), 0 при countdown
- falseStartLockoutMs = 2000 (SIGame 3000, ТВ ~2000, Jeopardy 250); lockoutGrowth = ×2 при повторе в течение 5 с; lockoutCapMs = 8000
- handicap adaptive: +15 мс за выигранную кнопку, −15 за проигранную гонку, clamp [0, 120]; manual: 0–300 мс, задаёт хост, виден всем
- writtenAnswerMs = 20000; writtenDuelMs = 10000; speedBonus = price·0.25·(1 − reactionMs/T) только для верных
- allPlayFor = ['forAll'] по умолчанию (SIQ v5), опционально ['forAll','simple'] или 'all'
- BIAS_FLAG: biasEwma > max(rttEff/4, 3σ) устойчиво на ≥ 8 нажатиях → trust = serverOnly + INTEGRITY_ALERT
- ROBOTIC_FLAG: медиана reactionMs < 150 и MAD < 12 на ≥ 8 нажатиях при randomLamp
- REJECT_RATIO_FLAG: доля source='rejected' > 30 % на ≥ 10 нажатиях → serverOnly
- DEGRADED: ≥ 3 пропущенных PONG подряд → serverOnly, окно сбора по rttEwma·2
- MAX_RTT_WARN_MS = 400 → предупреждение хосту, рекомендация allPlay для этого игрока
- LATENCY_UPDATE_INTERVAL_MS = 2000
- BUZZ_LATE: нажатие после deadline фиксируется, σ игрока += штрафной замер (например, +20 мс), решение не пересматривается

## Anti-cheat
- **Сервер — единственный арбитр**; все решения по логу `est/claim/tol/tPress/flags` (доступен хосту через `GET .../buzz-log`), решение окончательно (принцип NAQT/BuzzIn.Live «не опротестовывается»).
- **Compensation bounded by RTT**: клиентская дельта принимается только если `claim ≥ est − (rttEff/2 + 3σ + 10)`; иначе подменяется нейтральной серверной оценкой `est` (не границей!) и помечается `earlyClaim`. Выгода подделки внутри допуска ≤ `rtt/2 + 3σ + 10` мс — на LAN 15–20 мс, т.е. ниже точности измерения.
- **Bias-детектор**: для честного игрока `est − claim` колеблется вокруг асимметрии пути (±rtt/4), для читера — стабильно положителен; `biasEwma > max(rtt/4, 3σ)` на ≥ 8 нажатиях → `trust = serverOnly`, уведомление хосту. Шейвинг 100 мс на WAN ловится за ~5–8 вопросов; шейвинг < rtt/4 не ловится, но и не выводит из tie-группы при `window`.
- **Robotic-детектор** против автокликеров типа sigame-click (опрос 1 мс по цвету): при `randomLamp` (случайная пауза 300–1500 мс антиципацию исключает) `reactionMs < minReactionMs = 100` → фальстарт и лок-аут; медиана < 150 мс при MAD < 12 → флаг `robotic`. Утечка `armAt` модифицированному клиенту (расписание приходит за ≤ 600 мс) даёт лишь возможность жать в `armAt + 100` с нулевым разбросом — это и есть сигнатура `robotic`.
- **Игровой слой как античит**: при `resolution=window` + `tieBreak` (random/lowestScore/rotate/writtenDuel) быть первым «на 1 мс» ничего не даёт — цена читерства падает до нуля; сервер запрещает `countdown + first` (бот/антиципация выиграли бы всегда).
- `armNonce` на каждое включение кнопки + `seq` — защита от replay/дублей/нажатий «из прошлого вопроса»; один `BUZZ` на игрока на nonce (первый).
- `event.isTrusted === true`, `inputKind` и `evtTs` (Event.timeStamp) передаются; синтетические события не считаются; расхождение `evtTs` и `performance.now()` > 50 мс → флаг.
- Rate-limit `BUZZ`/`MISFIRE` (≤ 5/с), спам → экспоненциальный лок-аут до 8 с.
- Хост может принудительно перевести любого игрока (или всю комнату) в `serverOnly`/`arrival`, установить ручной гандикап или выгнать; всё это видно в `LATENCY_UPDATE`.
- Ограничение, о котором честно предупреждаем: на WAN с RTT 250 мс доверительный интервал ≈ ±190 мс — ни одна схема (включая BuzzIn.Live) не отличит там 5 мс; поэтому пресет `internetFair` использует широкое окно и `lowestScore`/`allPlay`, а не «первого».

## Tradeoffs
- **Точность ограничена физикой**: ошибка синхронной лампы и метки нажатия ≈ джиттер (Wi‑Fi 5–50 мс) и ≤ RTT/2 на WAN; конвейер ввода/дисплей добавляют ±20 мс. Разница реакций < 20 мс неизмерима — поэтому неявное окно ошибки есть даже в режиме `first`, и часть «побед» неизбежно решается правилом, а не скоростью.
- **Расписание против «сырого» TRY**: +lead (100–600 мс) между концом чтения и лампой — прячется в естественной паузе, но при игроке с RTT 400+ игра ощутимо «вязнет»; для таких — предупреждение и `allPlay`.
- **Утечка armAt**: модифицированный клиент видит момент лампы заранее. Мы принимаем это (защита — minReaction + robotic-детектор + правила тай-брейка), потому что альтернатива — staggered-отправка `ARMED` каждому в `armAt − OWD_i` — на Wi‑Fi даёт ошибку того же порядка, что и джиттер, и не устраняет автокликеры.
- **Окно сбора добавляет задержку** до объявления победителя (до 600 мс на плохих сетях); UX компенсируется мгновенным локальным «нажато» и ранним выходом, когда все нажали.
- **Широкое окно + тай-брейк меняет ощущение игры**: «кнопка» становится менее твитчевой, часть скилла реакции обесценивается. `lowestScore` можно «сэндбэгить» (умышленно проигрывать) — поэтому он рекомендуется для дружеских/интернет-партий, а для турниров — `random`/`rotate` или `lanWired`.
- **allPlay (письменные ответы)** полностью снимает проблему пинга, но это уже другая игра (Kahoot Accuracy/HQ); мы включаем его по умолчанию только там, где так задумал автор пакета (`forAll`), остальное — выбор хоста.
- **Гибрид доверяет клиенту в пределах RTT/2** — на WAN это до ~190 мс; статистические детекторы ловят систематическое читерство, но не разовое. Полностью недоверительный режим (`serverEstimate`/`arrival`) есть, но он честен только в одной LAN.
- **Сложность отладки**: три слоя и динамические окна труднее объяснить игрокам, чем «кто первый нажал» — поэтому обязательны открытый пинг, `BUTTON_RESULT` с кандидатами и правилом, и журнал решения для хоста.
- **TCP head-of-line**: WebSocket-столлы расширяют допуск (через `rttLast`) и могут на один вопрос сделать честного игрока «менее верифицируемым»; WebTransport-датаграммы снижают это, но требуют HTTP/3 и не всегда доступны в LAN без сертификатов.
