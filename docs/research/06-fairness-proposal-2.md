# ServerRewind: серверная RTT-компенсация с ограниченной поправкой, выравненным ARMED, окном одновременности и калиброванным тай-брейком (клиенту не доверяем ничего, кроме факта нажатия) (complexity: medium)

## Mechanism
# ServerRewind — механизм

## 0. Модель доверия

Единственный «вход» от клиента, влияющий на исход, — **момент прихода** его сообщений на сервер. Ни одна клиентская метка времени не участвует в ранжировании. Сервер работает только на своих **монотонных часах** (`performance.now()` / `hrtime` / `Instant::now()`; никогда `Date.now()` для интервалов). Клиентские часы синхронизировать не нужно вообще.

Клиент может влиять только на тайминг трёх сообщений: `PONG`, `ARMED_ACK`, `PRESS`. Для первых двух сервер держит **якорь**, который из JS не подделать (RTT ядра/QUIC или RTT TLS-рукопожатия), и **ограничивает поправку** сверху. Для `PRESS` — это и есть игра.

## 1. Измерение RTT (непрерывно, серверно)

- Сервер шлёт `PING{seq, ts}` каждые **1000 мс** в простое и каждые **200 мс** в фазах `reading` и `armed` («burst», чтобы к моменту ARMED было ≥ 8 свежих замеров). Клиент **немедленно** отвечает `PONG{seq, ts}` (просто эхо, без своих часов). `rtt = now − ts`. Кадры PING/PONG паддятся случайно до 32–96 байт: под TLS их нельзя отличить от других мелких кадров, значит сетевой шейпер читера может задержать только *весь* трафик — а равномерная задержка, как показано в §9, выигрыша не даёт.
- Per-player кольцевой буфер 32 замеров с временем. Статистики:
  - `rttMed` — медиана последних 8;
  - `rttMin30s` — минимум за 30 с;
  - `rttMax16` — максимум последних 16 (для «самой щедрой» оценки в античите);
  - `sigmaRtt = 1.4826 · MAD(последних 16)` (робастная σ); `sigma = clamp(sigmaRtt/√2, 1, 30)` — односторонняя неопределённость (джиттер RTT = сумма двух независимых направлений);
  - `transportRtt` — `tcpi_rtt` из `TCP_INFO` (Linux) / `TCP_CONNECTION_INFO` (macOS) или `smoothed_rtt` QUIC при WebTransport — **опционально**, доступно, если игровой процесс сам терминирует соединение (Go/Rust/uWebSockets с FFI); 
  - `rttHandshake` — время между TCP-accept и `secureConnect` (≈ 1 RTT в TLS 1.3): доступно **в любом стеке**, включая Node/Bun, и формируется TLS-стеком браузера, а не JS.
  - `cold = samples < 5` (после (ре)коннекта).

**Опорное RTT** (только оно идёт в компенсацию):
```
jAllow(p)   = 2·sigmaRtt + 10 мс
rttRef(p)   = min( rttMed,
                   rttMin30s + jAllow(p),                 // «асимметричная адаптация»: вниз — мгновенно, вверх — не быстрее 30 с
                   anchor(p) )
anchor(p)   = transportRtt + 20 мс   если есть RTT ядра/QUIC
            = rttHandshake + 40 мс   иначе (клиент авто-реконнектится, если rttMed > anchor 10 с подряд, — получаем новый якорь)
corr(p)     = clamp(rttRef(p) / 2, 0, C_MAX = 150 мс)    // ограниченная односторонняя поправка
cold ⇒ rttRef = anchor без слака, sigma = SIGMA_COLD = 20
```
Если `rttMed` упирается в `anchor` — ставится флаг `RTT_INFLATED` (см. античит). Смысл: игроку никогда не «дарят» больше задержки, чем подтверждает транспорт; резкий рост пинга (реальный или искусственный) в компенсацию попадает не раньше, чем через 30 с.

## 2. Взведение кнопки: ARMED с выравниванием доставки (bounded delay equalization)

1. Чтение вопроса закончилось в `tReadEnd`. Сервер ждёт случайную паузу `U(150, 900)` мс (убивает антиципацию по ритму чтения) → `armAt`.
2. Для каждого допущенного игрока фиксируется `corrAtArm_i = corr(p_i)`, `sigmaAtArm_i` — **поправка замораживается на весь вопрос**, между ARMED и PRESS её нельзя изменить.
3. `tSeenTarget = armAt + min(max_i corrAtArm_i, E_MAX = 150)`. Игроку `i` `ARMED{nonce, qid, ts, closeInMs}` отправляется в `sendAt_i = tSeenTarget − min(corrAtArm_i, E_MAX)`; момент отправки `armedSentAt_i` записывается с монотонных часов непосредственно перед `send`. Все, у кого `corr ≤ E_MAX`, «видят лампочку» практически одновременно; игрок с `corr > E_MAX` получает ARMED сразу, но увидит его позже — это учитывается через `tSeen_i`.
4. Клиент при получении `ARMED` **сразу** отвечает `ARMED_ACK{nonce}` и включает кнопку. `rttAck_i = ackRecvAt_i − armedSentAt_i` — RTT именно этого обмена.
5. Оценка момента, когда игрок увидел сигнал:
```
tSeen_i = armedSentAt_i + corrAtArm_i + clamp(rttAck_i − 2·corrAtArm_i, 0, jAllow_i)
```
Поздняя доставка ARMED (ретрансмит TCP) прощается **только в пределах jAllow** (≈ 15–30 мс): большего читер задержкой ACK не выиграет, а честный игрок, поймавший ретрансмит на 200 мс, этот вопрос, увы, проигрывает (событие логируется как `lateDelivery`).
6. `nonce` — 16 hex случайных байт, один на «взведение». После неверного ответа кнопка взводится заново **с новым nonce** (без случайной паузы, но с тем же выравниванием).

## 3. Приём нажатия

Клиент шлёт `PRESS{nonce, seq, inputKind, isTrusted, localReactionMs}` (последние три поля — только для статистики, в ранжировании не участвуют). Сервер фиксирует `tRecv` и считает:
```
tPressEst_i = tRecv_i − corrAtArm_i                 // «отмотка» на половину опорного RTT
react_i     = tPressEst_i − tSeen_i                 // оценка реакции от увиденного сигнала
reactLB_i   = tRecv_i − armedSentAt_i − rttMax16_i  // самая щедрая к игроку оценка (для античита)
```
Правила отбраковки: неизвестный/старый `nonce` → `stale` (без штрафа); повтор → `duplicate` (считается первое); `lockoutUntil > tRecv` → `lockedOut`; `reactLB < R_MIN = 100 мс` → `tooEarly` + лок-аут + страйк `SUPERHUMAN` (при рандомизированном ARMED человек не может нажать раньше ~150 мс; 100 — с запасом на джиттер). Принятый PRESS попадает в `presses[q]`.

## 4. Окно решения и окно одновременности

**Метрика ранжирования** `m_i` — по настройке комнаты: `reaction` (по умолчанию; `m_i = react_i`, честно и при выключенном выравнивании) или `pressTime` (`m_i = tPressEst_i`, «ТВ-стиль»: с выравниванием совпадает с `reaction`).

**Окно одновременности** — попарное, из измеренных джиттеров:
```
W(i, j) = clamp( K·sqrt(sigma_i² + sigma_j²) + W_EPS,  W_MIN, W_MAX ),  K = 2, W_EPS = 4, W_MIN = 6, W_MAX = 120 мс
```
На проводной LAN это ≈ 6–8 мс, Wi‑Fi ≈ 12–40, WAN ≈ 15–60. Все, у кого `m_j − m_lead ≤ W(lead, j)`, образуют **спорное множество**: сервер честно признаёт, что не отличает их.

**Окно решения** — динамическое. После каждого принятого PRESS сервер вычисляет для каждого ещё не нажавшего допущенного игрока `j` последний момент, когда его нажатие ещё могло бы сравняться с лидером:
```
deadline_j = tSeen_j + m_lead + W(lead, j) + corrAtArm_j + 3·sigma_j        // для reaction
decideAt   = min( max_j deadline_j,  firstRecv + DECISION_MAX (350 мс) )
```
Если нажали все допущенные — решение сразу. Быстрый игрок ждёт результата не дольше DECISION_MAX (в SIGame и так 300 мс).

## 5. Тай-брейк внутри спорного множества

1. Если в спорном множестве есть игроки без флагов/не `cold` — флагованные и «холодные» из него исключаются (презумпция в пользу чистых).
2. Если остался один — он победил (`contested: false`).
3. Иначе `tieBreak` комнаты:
   - **`likelihood` (по умолчанию)** — лотерея, взвешенная вероятностью того, что игрок был первым по нашей модели шума: `w_i = Π_{j≠i} Φ((m_j − m_i)/sqrt(sigma_i² + sigma_j²))`. Для двух игроков это `Φ(gap/σ_ij)`. Ни низкий пинг, ни низкий джиттер систематического преимущества не дают; лидер по точечной оценке выигрывает чаще (70–99 %), игрокам это неотличимо от «кто первый».
   - `mostLikely` — детерминированно берётся лидер точечной оценки.
   - `random` — как в SIGame `RandomWithinInterval`.
   - `lowestScore` / `fewestButtonsWon` — гандикап отстающему.
   - `allPlay` — все из спорного множества отвечают письменно одновременно.
   ГПСЧ лотереи сидируется `HMAC(serverSecret, nonce)` — результат воспроизводим для аудита ведущим, но непредсказуем.
4. Всем рассылается `BUTTON_RESULT` с `winner`, `contested`, `margin`, `resolution` и (ведущему) полным списком кандидатов с `tPressEst/react/sigma/rttRef/flags`. Решение окончательное, как у NAQT/BuzzIn.Live, но с прозрачным логом.

## 6. Псевдокод серверной части (TypeScript-подобный)

```ts
const P = { PING_IDLE:1000, PING_BURST:200, C_MAX:150, E_MAX:150, K:2, W_EPS:4, W_MIN:6, W_MAX:120,
            DECISION_MAX:350, R_MIN:100, SIGMA_MIN:1, SIGMA_MAX:30, SIGMA_COLD:20,
            TRANSPORT_SLACK:20, HANDSHAKE_SLACK:40, ARM_JITTER:[150,900], ANSWER_WINDOW:5000 };

interface Net { samples:{t:number,rtt:number}[]; rttMed:number; rttMin30s:number; rttMax16:number;
                sigmaRtt:number; sigma:number; transportRtt?:number; rttHandshake:number;
                flags:Set<string>; cold:boolean }

const jAllow = (n:Net) => 2*n.sigmaRtt + 10;
function anchor(n:Net){ return n.transportRtt!==undefined ? n.transportRtt+P.TRANSPORT_SLACK : n.rttHandshake+P.HANDSHAKE_SLACK; }
function rttRef(n:Net){
  if (n.cold) return n.transportRtt ?? n.rttHandshake;
  let ref = Math.min(n.rttMed, n.rttMin30s + jAllow(n));
  if (ref > anchor(n)) { n.flags.add('RTT_INFLATED'); ref = anchor(n); }
  return ref;
}
const corr = (n:Net) => clamp(rttRef(n)/2, 0, P.C_MAX);

function onPong(p:Player, m:Pong, now:number){
  if (!p.net.outstanding.delete(m.seq)) return;          // неизвестный seq → игнор (replay)
  pushSample(p.net, now, now - m.ts); recomputeStats(p.net);   // rttMed/rttMin30s/rttMax16/sigma
  p.net.cold = p.net.samples.length < 5;
}

function arm(q:Question){
  q.nonce = randomHex(16); q.state = 'arming';
  const elig = eligible(q);                                // подключены, не в лок-ауте, не ответили неверно на этот вопрос
  for (const p of elig){ q.corrAtArm[p.id] = corr(p.net); q.sigmaAtArm[p.id] = p.net.cold ? P.SIGMA_COLD : p.net.sigma; }
  const now = mono();
  q.tSeenTarget = now + Math.min(Math.max(...elig.map(p=>q.corrAtArm[p.id])), P.E_MAX);
  for (const p of elig){
    const sendAt = q.tSeenTarget - Math.min(q.corrAtArm[p.id], P.E_MAX);
    schedule(sendAt, () => { q.armedSentAt[p.id] = mono();
      send(p, {type:'ARMED', nonce:q.nonce, qid:q.id, ts:q.armedSentAt[p.id], closeInMs:P.ANSWER_WINDOW}); });
  }
  q.state = 'armed';
  schedule(q.tSeenTarget + P.ANSWER_WINDOW, () => close(q, 'timeout'));
}

function onArmedAck(p, m, now){ if (m.nonce===q.nonce && q.ackRecvAt[p.id]===undefined) q.ackRecvAt[p.id] = now; }

function tSeen(q, p){
  const base = q.armedSentAt[p.id] + q.corrAtArm[p.id];
  const ack = q.ackRecvAt[p.id]; if (ack===undefined) return base;
  const late = (ack - q.armedSentAt[p.id]) - 2*q.corrAtArm[p.id];
  return base + clamp(late, 0, jAllow(p.net));             // прощаем позднюю доставку ограниченно
}

function onPress(p:Player, m:Press, tRecv:number){
  if (!q || q.state!=='armed' || m.nonce!==q.nonce) return ack(p,m,'stale');
  if (p.lockoutUntil > tRecv)                         return ack(p,m,'lockedOut');
  if (q.presses.has(p.id))                            return ack(p,m,'duplicate');
  if (q.armedSentAt[p.id]===undefined)                { strike(p,'PRESS_BEFORE_ARMED'); return ack(p,m,'stale'); }
  const tPressEst = tRecv - q.corrAtArm[p.id];
  const react     = tPressEst - tSeen(q,p);
  const reactLB   = tRecv - q.armedSentAt[p.id] - p.net.rttMax16;
  if (reactLB < P.R_MIN){ strike(p,'SUPERHUMAN'); lockout(p,tRecv); return ack(p,m,'tooEarly'); }
  if (!m.isTrusted) strike(p,'UNTRUSTED_EVENT');
  q.presses.set(p.id, { pid:p.id, tRecv, tPressEst, react, sigma:q.sigmaAtArm[p.id],
                        flagged: p.net.flags.size>0 || p.net.cold });
  q.firstRecv ??= tRecv;
  ack(p,m,'accepted');
  reschedule(q);
}

const metric = (c) => ROOM.rankBy==='pressTime' ? c.tPressEst : c.react;
const W = (a,b) => clamp(P.K*Math.hypot(a.sigma,b.sigma) + P.W_EPS, P.W_MIN, P.W_MAX);

function reschedule(q){
  const lead = minBy([...q.presses.values()], metric);
  const pending = eligible(q).filter(p => !q.presses.has(p.id));
  if (pending.length===0) return decide(q);
  const deadline = Math.max(...pending.map(p => {
    const s = {sigma:q.sigmaAtArm[p.id]};
    const base = ROOM.rankBy==='pressTime' ? 0 : tSeen(q,p);
    return base + metric(lead) + W(lead,s) + q.corrAtArm[p.id] + 3*s.sigma;   // последний момент прихода, чтобы сравняться
  }));
  setDecisionTimer(q, Math.min(deadline, q.firstRecv + P.DECISION_MAX), () => decide(q));
}

function decide(q){
  q.state = 'deciding';
  const ranked = [...q.presses.values()].sort((a,b)=>metric(a)-metric(b));
  const lead = ranked[0];
  let tie = ranked.filter(c => metric(c)-metric(lead) <= W(lead,c));
  const clean = tie.filter(c => !c.flagged); if (clean.length) tie = clean;
  const winner = tie.length===1 ? tie[0] : tieBreak(tie, q);
  broadcast({type:'BUTTON_RESULT', nonce:q.nonce, winner:winner.pid, contested:tie.length>1,
             margin: ranked[1] ? metric(ranked[1])-metric(lead) : null, resolution: ranked[1] ? W(lead,ranked[1]) : null,
             tieBreak: tie.length>1 ? ROOM.tieBreak : 'none', candidates: ranked.map(publicView), decidedAt: mono()});
}

function tieBreak(tie, q){
  const rng = seeded(hmac(SERVER_SECRET, q.nonce));
  switch (ROOM.tieBreak){
    case 'likelihood': { const w = tie.map(i => tie.reduce((acc,j)=> j===i?acc: acc*Phi((metric(j)-metric(i))/Math.hypot(i.sigma,j.sigma)), 1));
                         return weightedPick(tie, w, rng); }
    case 'mostLikely':      return tie[0];
    case 'random':          return tie[rng.int(tie.length)];
    case 'lowestScore':     return minBy(tie, c=>score(c.pid));
    case 'fewestButtonsWon':return minBy(tie, c=>buttonsWon(c.pid));
    case 'allPlay':         return AllPlay(tie);
  }
}
```

## 7. Разобранный пример: RTT 10 / 80 / 250 мс, физические нажатия 1000 / 990 / 985 мс

Игроки: **A** — Ethernet, RTT 10, `sigmaRtt` 0.6 → `sigma` 1 (пол); **B** — Wi‑Fi 5 ГГц, RTT 80, `sigmaRtt` 4.2 → `sigma` 3; **C** — WAN по проводу, RTT 250, `sigmaRtt` 5.7 → `sigma` 4. Опорные RTT равны номиналам, транспортные якоря не срабатывают. `corr` = 5 / 40 / 125 (все ≤ C_MAX, E_MAX).

**Взведение.** Чтение кончилось в 400, случайная пауза 175 → `armAt = 575`. `tSeenTarget = 575 + 125 = 700`. ARMED отправлен: C в 575, B в 660, A в 695. Все получают его в ≈ 700; `ARMED_ACK` приходят в 705 / 740 / 825, `rttAck = rttRef` ⇒ `tSeen = 700` у всех.

**Нажатия.** Физически: A 1000 (реакция 300), B 990 (290), C 985 (285). Аплинк 5 / 40 / 125 ⇒ `tRecv` = **1005 / 1030 / 1110**.

| | A | B | C |
|---|---|---|---|
| tRecv | 1005 | 1030 | 1110 |
| corrAtArm | 5 | 40 | 125 |
| tPressEst = tRecv − corr | **1000** | **990** | **985** |
| react = tPressEst − 700 | 300 | 290 | 285 |
| reactLB (античит, ≥100?) | 1005−695−11 = 299 ✓ | 1030−660−88 = 282 ✓ | 1110−575−256 = 279 ✓ |

Ход решения на сервере:
- **1005**: PRESS A → лидер A (m=300). Дедлайны: B → 700+300+W(A,B)=10.3+40+9 = 1059; C → 700+300+W(A,C)=12.2+125+12 = 1149. Таймер на 1149 (< 1005+350).
- **1030**: PRESS B → лидер B (290). Дедлайн C → 700+290+14+125+12 = 1141. Таймер на 1141.
- **1110**: PRESS C → лидер C (285). Все допущенные нажали → `decide()` немедленно, через **105 мс** после первого прихода.
- Ранжирование: C 285, B 290, A 300. Спорное множество: B — разрыв 5 ≤ W(C,B) = 2·√(16+9)+4 = **14** → внутри; A — разрыв 15 > W(C,A) = 2·√17+4 = 12.2 → вне. Флагов нет.
- Тай-брейк `likelihood`: `w_C = Φ(5/5) = 0.841`, `w_B = 0.159` → **побеждает C** с вероятностью 84 %; `mostLikely` → **C** детерминированно. `BUTTON_RESULT{winner:C, contested:true, margin:5, resolution:14}`.

Для сравнения: чистый порядок прихода (`FirstWins`) отдал бы победу **A**, который физически нажал последним — на 15 мс позже C. `FirstWinsClient` с честными клиентами тоже выбрал бы C, но B, прислав `deltaMs=1`, забрал бы кнопку без всякой проверки.

Честная оговорка, которую алгоритм делает сам: разрыв B–C в 5 мс *меньше разрешающей способности* при Wi‑Fi-джиттере 4 мс и 250 мс RTT — поэтому пара помечена как `contested`, а не выдана за точное измерение. Если реализованный аплинк B оказался +1σ (43 мс), оценка B = 993, разрыв 8, `Φ(8/5)=0.945`; при Wi‑Fi под нагрузкой (`sigmaRtt` 18 → `sigma` 12.7) окно W(C,B) растёт до 31 мс и `Φ(5/13.3)=0.65` — точечный лидер по-прежнему C, но лотерея честно отражает, что мы почти не отличаем их.

## 8. Потери пакетов, реконнект, сбои

- **Потерян PING/PONG**: замер пропускается по `seq`; ≥ 3 подряд → бейдж `NET_DEGRADED`, `sigma` поднимается до SIGMA_COLD до восстановления.
- **Потерян ARMED (WebSocket/TCP)**: ретрансмит по RTO (200+ мс); `rttAck` показывает опоздание, но прощается только `jAllow` — игрок фактически опоздал, событие в логе `lateDelivery`, ведущий его видит. В WAN-профиле на WebTransport ARMED и ARMED_ACK идут датаграммами трижды (0/15/30 мс, один nonce, дедуп) — потеря одной копии ничего не стоит.
- **Потерян PRESS**: по TCP придёт после RTO; если до `decide()` — учитывается с честной оценкой (сервер не знает о потере и не может знать без доверия клиенту); если после — `PRESS_ACK{status:'late'}`. На WebTransport клиент шлёт PRESS трижды за 20 мс с тем же `seq`, считается первая копия.
- **Обрыв и реконнект внутри вопроса**: `RESUME{sessionToken, lastMsgId}` → сервер отвечает `RESUMED` и, если кнопка ещё открыта, повторяет `ARMED` с тем же nonce (`armedSentAt` перезаписывается временем повтора). Сетевая статистика сбрасывается в `cold` (5 замеров burst ≈ 1 с): игрок может нажимать и выигрывать безусловно, но в спорном множестве считается «холодным» (широкая σ, уступает чистым). Дубли через старое и новое соединение режутся по nonce+seq.
- **Стойл event-loop/GC на сервере**: измеряется lag цикла; если во время `armed` он > 10 мс, lag добавляется к `W_EPS` этого вопроса и пишется в лог.
- **Игрок без ARMED_ACK** (потерян или клиент модифицирован): `tSeen = base`, никаких поблажек.
- **Переполнение времени**: все `ts` — u32 мс монотонных часов сервера (wrap через 49 дней обрабатывается вычитанием по модулю).

## 9. Сравнение с клиентскими метками времени

| Критерий | Клиентская дельта (`FirstWinsClient`, патент US5695400, Game Night Buzzer) | Гибрид «дельта в пределах RTT/2+3σ» (§5.2 исследования) | **ServerRewind** |
|---|---|---|---|
| Что доверяем | дельте целиком | дельте, но клампим к серверной оценке | только таймингу PONG/ACK, и то под якорем транспорта |
| Максимальный выигрыш читера | не ограничен (`I 1`) | до `RTT_i/2 + 3σ_i` (≈ 130 мс при RTT 250) | ≤ 20 мс с якорем ядра/QUIC, ≤ 40 мс с якорем рукопожатия — и при этом флаг `RTT_INFLATED` |
| Разрешение для честных игроков | ≈ 1–2 мс (точность таймера клиента) | ≈ 1–2 мс, пока клиент честен | `σ_up + асимметрия`: LAN-кабель ≈ 1 мс, Wi‑Fi 3–20 мс, WAN 5–30 мс |
| Ретрансмит ARMED/PRESS | компенсируется полностью (клиент знает реальный момент) | полностью | только в пределах `jAllow`; иначе игрок проигрывает вопрос |
| Синхронизация часов | не нужна (дельта) | не нужна | не нужна |
| Зависимость от таймеров браузера | да (Firefox 1 мс, `reduceTimerPrecision`, throttling фоновых вкладок) | да | нет |
| Replay / подмена сообщений | нужны nonce и seq | nonce, seq | nonce, seq, плюс серверный порядок |
| Кто в итоге судья | клиент | клиент с серверным лимитом | сервер, решение воспроизводимо из лога |

Почему равномерная задержка не помогает читеру (формально): пусть он добавляет реальную задержку X на аплинк. `RTT` растёт на X, `corr` на X/2; ARMED ему отправят на X/2 раньше, увидит он его на X/2 раньше остальных, а PRESS дойдёт на X позже: `tPressEst = (t0 − X/2 + R + d) + X − (d + X/2) = t0 + R` — ровно как у честного. Выигрыш даёт только расхождение между *измеренным* и *реально испытанным* RTT — то есть задержка PONG/ACK в userland, которую и ловят якоря и асимметричная адаптация.

Что мы **теряем** по честности: (1) разрешение на плохом Wi‑Fi хуже клиентского на 10–40 мс — компенсируется тем, что окно одновременности и калиброванная лотерея превращают эту неопределённость в честный шанс, а не в систематический перевес; (2) честный игрок, поймавший ретрансмит, проигрывает вопрос; (3) игроки с RTT > 300 мс недокомпенсированы (C_MAX) — сознательно, проект LAN-first; (4) реальное ухудшение сети адаптируется в поправку до 30 с.

## Protocol
## Транспорт
WebSocket (JSON, опционально MessagePack) напрямую к игровому процессу — **без** nginx/Cloudflare перед сокетом кнопки, иначе транспортный якорь RTT меряет прокси. WAN-профиль: WebTransport, где `PING/PONG/ARMED/ARMED_ACK/PRESS` идут датаграммами с тройным повтором, остальное — по надёжным стримам. Все `ts` — миллисекунды монотонных часов сервера (u32).

## Сообщения

**S→C**
- `PING {seq:u32, ts:u32, pad:bytes}` — 1000 мс в простое, 200 мс в `reading`/`armed`.
- `ARMED {nonce:hex16, qid, ts:u32, closeInMs:u16}` — кнопка включена; отправляется игроку в `tSeenTarget − corr_i`.
- `PRESS_ACK {nonce, seq, status:'accepted'|'duplicate'|'stale'|'late'|'lockedOut'|'tooEarly'}`.
- `LOCKOUT {untilTs:u32, durationMs:u16, reason:'falseStart'|'tooEarly'}`.
- `BUTTON_RESULT {nonce, winner:pid|pid[], contested:bool, marginMs?, resolutionMs?, tieBreak:'none'|'likelihood'|'mostLikely'|'random'|'lowestScore'|'fewestButtonsWon'|'allPlay', candidates:[{pid, tPressEst, react, sigma, rttRef, cold, flags[]}], decidedAt}` — всем; поле `candidates` полностью только ведущему, игрокам — порядок и `contested`.
- `BUTTON_CLOSED {nonce, reason:'answered'|'timeout'|'cancelled'}`.
- `NET_STATS {players:[{pid, rttRef, rttMed, sigma, transportRtt?, cold, flags[]}]}` — каждые 2 с (бейджи в UI, социальный сдерживающий фактор).
- `ANOMALY {pid, code, details, action:'log'|'conservative'|'arrivalOnly'|'kick'}` — только ведущему.
- `RESUMED {qid, state, armed?:ARMED}` — ответ на RESUME с повтором ARMED, если кнопка открыта.

**C→S**
- `PONG {seq, ts}` — немедленное эхо, без клиентского времени.
- `ARMED_ACK {nonce}` — немедленно при получении ARMED.
- `PRESS {nonce, seq:u16, inputKind:'key'|'pointer'|'touch'|'gamepad', isTrusted:bool, localReactionMs:u16}` — три последних поля только для статистики/античита.
- `MISFIRE {qid}` — честный клиент нажал до ARMED (кнопка визуально заблокирована); влечёт LOCKOUT.
- `RESUME {sessionToken, lastMsgId}`.

## Последовательность
```
Клиент i                                        Сервер (монотонные часы, авторитет)
  |<------------- PING{seq,ts} -------------------|  1 Гц / 5 Гц в reading+armed, паддинг
  |-------------- PONG{seq,ts} ------------------>|  rtt=now−ts → буфер → rttMed, rttMin30s, sigma, якорь → rttRef, corr
  |<------------- QUESTION{qid,...} --------------|  фаза reading (кнопка выключена)
  |-------------- MISFIRE{qid} ------------------>|  → LOCKOUT{untilTs} (1000 мс ×2 при повторе ≤5 с, max 3000)
  |                                               |  reading end + U(150,900) → armAt; corrAtArm_i заморожены
  |                                               |  tSeenTarget = armAt + min(max corr, 150)
  |<------------- ARMED{nonce,qid,ts,closeInMs} --|  в момент tSeenTarget − corr_i (C в 575, B в 660, A в 695)
  |-------------- ARMED_ACK{nonce} -------------->|  rttAck_i → tSeen_i (опоздание прощается ≤ jAllow_i)
  |   игрок нажимает                              |
  |-------------- PRESS{nonce,seq,...} ---------->|  tRecv → tPressEst=tRecv−corr_i, react=tPressEst−tSeen_i, reactLB≥100?
  |<------------- PRESS_ACK{status} --------------|
  |                                               |  reschedule(): ждём отстающих до min(max deadline_j, first+350)
  |<------------- BUTTON_RESULT{winner,...} ------|  broadcast (≤ 350 мс после первого прихода)
  |<------------- BUTTON_CLOSED{nonce,reason} ----|
  |<------------- NET_STATS{...} -----------------|  каждые 2 с
 host <---------- ANOMALY{pid,code,action} -------|
  --- обрыв ---
  |-------------- RESUME{sessionToken,lastMsgId}->|  статистика → cold, якорь = новое рукопожатие
  |<------------- RESUMED{..., armed?} -----------|  повтор ARMED с тем же nonce, если кнопка открыта
```

## Настройки комнаты (в API)
`buttonMode: 'serverRewind' (default) | 'arrival' | 'clientReaction' | 'hybridBounded'`, `rankBy: 'reaction' (default) | 'pressTime'`, `equalizeArming: true`, `tieBreak: 'likelihood' (default) | 'mostLikely' | 'random' | 'lowestScore' | 'fewestButtonsWon' | 'allPlay'`, `netProfile: 'lan' | 'wan'`, `falseStartLockoutMs`, `armingJitterMs:[150,900]`, `answerWindowMs: 5000`.

## Parameters
- PING_IDLE_MS = 1000; PING_BURST_MS = 200 (в фазах reading/armed); паддинг кадров 32–96 байт
- RTT_RING = 32 замера; rttMed по последним 8; rttMin30s — минимум за 30 000 мс; rttMax16 — максимум последних 16
- sigmaRtt = 1.4826·MAD(16); sigma = clamp(sigmaRtt/√2, SIGMA_MIN=1, SIGMA_MAX=30) мс; SIGMA_COLD = 20 мс при < 5 замеров
- jAllow = 2·sigmaRtt + 10 мс (допустимое превышение над rttMin30s и лимит прощения поздней доставки ARMED)
- TRANSPORT_SLACK = 20 мс над tcpi_rtt/QUIC srtt; HANDSHAKE_SLACK = 40 мс над RTT TLS-рукопожатия; авто-реконнект при rttMed > anchor 10 с подряд
- C_MAX = 150 мс — максимальная односторонняя поправка (WAN-профиль); 60 мс в LAN-профиле
- E_MAX = 150 мс — максимальная задержка выравнивания ARMED (WAN); 60 мс в LAN
- ARM_JITTER = U(150, 900) мс случайная пауза перед ARMED
- Окно одновременности: W(i,j) = clamp(2·sqrt(σi²+σj²) + 4, W_MIN=6, W_MAX=120) мс
- DECISION_MAX = 350 мс от первого принятого PRESS (200 в LAN-профиле, 400 в WAN); динамический дедлайн = tSeen_j + m_lead + W + corr_j + 3σ_j
- R_MIN = 100 мс — минимально возможная человеческая реакция (по самой щедрой оценке reactLB); нарушение → tooEarly + лок-аут + страйк
- ANSWER_WINDOW_MS = 5000 — сколько кнопка остаётся взведённой
- Лок-аут за фальстарт: L0 = 1000 мс, ×2 при повторе в течение 5 с, максимум 3000 мс (Jeopardy 250 / НТВ 2000 / SIGame 3000 — настройка)
- Бот-детектор: ≥ 10 нажатий, медиана react < 150 мс и MAD < 15 мс → BOT_SUSPECT; SUPERHUMAN_STRIKES = 3 за игру → режим arrivalOnly
- NET_STATS каждые 2000 мс; ANOMALY ведущему немедленно
- tieBreak по умолчанию 'likelihood'; rankBy 'reaction'; equalizeArming true; ГПСЧ лотереи = HMAC(serverSecret, nonce)

## Anti-cheat
## Поверхность доверия и границы выигрыша

Клиент влияет на исход только таймингом `PONG`, `ARMED_ACK`, `PRESS`. Поля `isTrusted`, `inputKind`, `localReactionMs` — сугубо диагностические. Из этого вытекают доказуемые границы: (а) **сетевой** читер (netem/шейпер) может задержать только весь TLS-трафик целиком (PING паддится и неотличим), а равномерная задержка не даёт выигрыша (§9 механизма); (б) **userland**-читер (модифицированный JS) может задерживать PONG/ACK, но не TCP-ACK ядра, QUIC-ACK и TLS-рукопожатие браузера, поэтому его выигрыш в метрике реакции ограничен `TRANSPORT_SLACK = 20 мс` (с RTT ядра/QUIC) или `HANDSHAKE_SLACK = 40 мс` (только рукопожатие) — и в обоих случаях он получает флаг; (в) поправка `corrAtArm` замораживается в момент ARMED — между ARMED и PRESS изменить ничего нельзя; (г) резкий рост измеренного RTT попадает в компенсацию не раньше чем через 30 с (`rttMin30s + jAllow`), снижение — мгновенно.

## Детекторы (код → порог → действие)

- `RTT_INFLATED`: `rttMed > anchor` (транспорт + 20 мс / рукопожатие + 40 мс) в 3 последовательных окнах по 2 с → компенсация по якорю, игрок помечен «conservative»: проигрывает любое спорное множество чистым игрокам.
- `JITTER_INFLATED`: `sigmaRtt` в 3+ раза выше `tcpi_rttvar` (если есть ядро) или > 30 мс при стабильном якоре → `sigma` клампится к SIGMA_MIN для окна и лотереи (нельзя раздуть окно, чтобы чаще попадать в лотерею).
- `ACK_DELAYED`: `rttAck − rttRef > jAllow` регулярно (≥ 3 вопроса из 5) → `tSeen` без прощения опозданий + флаг.
- `SUPERHUMAN`: `reactLB = tRecv − armedSentAt − rttMax16 < 100 мс` → нажатие отвергается (`tooEarly`), лок-аут как за фальстарт, страйк; 3 страйка за игру → режим `arrivalOnly` (corr = 0, без выравнивания) до конца игры + уведомление ведущего.
- `BOT_SUSPECT`: по ≥ 10 нажатиям медиана `react` < 150 мс и MAD < 15 мс (у человека MAD ≈ 30–40 мс) → уведомление ведущего, «conservative». Целится в автокликеры типа sigame-click (опрос 1 мс по цвету кнопки); рандомизированный ARMED (150–900 мс) лишает их антиципации, поэтому их реакция сваливается к 20–60 мс и ловится R_MIN.
- `UNTRUSTED_EVENT`: `isTrusted=false` или `inputKind` не совпадает с браузерным профилем → только счётчик/лог (поле подделываемо, поэтому решения на нём не строятся).
- `PRESS_BEFORE_ARMED` / `STALE_NONCE`: PRESS с nonce, который игроку ещё не отправляли, или с чужим/старым nonce → игнор + страйк (реплей/модифицированный клиент). Dedup по `(nonce, pid)` — считается первое нажатие, повтор `duplicate` без штрафа.
- `FALSE_START`: `MISFIRE` от честного клиента → `LOCKOUT` 1000 мс, ×2 при повторе в 5 с, max 3000; лок-аут применяется в серверном времени (`lockoutUntil`), клиентская блокировка — лишь UX.

## Лестница санкций
0 — лог и бейдж в `NET_STATS` (все видят RTT/σ/флаги друг друга, как в Protobowl); 1 — «conservative»: компенсация по якорю, проигрыш спорных множеств чистым; 2 — `arrivalOnly` до конца игры; 3 — ведущий может исключить. Каждое решение `BUTTON_RESULT` воспроизводимо из лога (`tRecv`, `corrAtArm`, `tSeen`, `sigma`, seed лотереи = HMAC(secret, nonce)) — решение окончательное, но проверяемое.

## Что античит сознательно не решает
Различия во входном тракте (тач на телефоне 50–90 мс против клавиатуры 10–20 мс), рендер ±16 мс при 60 Гц — это не сеть; честно сообщать игрокам, что на нагруженном Wi‑Fi с джиттером 20–50 мс любая схема ошибается на те же 20–50 мс (позиция NAQT), и советовать кабель/5 ГГц.

## Tradeoffs
**Что покупаем.** Нулевое доверие к клиентским часам и дельтам (закрыт тривиальный чит `I 1` из `FirstWinsClient` и остаточная дыра гибрида ≤ RTT/2+3σ ≈ 130 мс при RTT 250); нет протокола синхронизации часов; нет зависимости от точности/троттлинга браузерных таймеров; выигрыш любого читера ограничен 20–40 мс и сопровождается флагом; решение воспроизводимо из серверного лога; равномерная сетевая задержка формально не даёт преимущества.

**Что платим по честности.** (1) Разрешающая способность — σ_up + асимметрия пути: ≈ 1 мс на кабельной LAN, 3–20 мс на Wi‑Fi, 5–30 мс в WAN, против 1–2 мс у честного клиентского замера; в разобранном примере разрыв B–C в 5 мс алгоритм честно объявляет спорным (лидер C выигрывает 84 %, а не 100 %). (2) Честный игрок, поймавший TCP-ретрансмит ARMED/PRESS (RTO 200+ мс), проигрывает вопрос — прощение ограничено jAllow ≈ 15–30 мс, иначе это стало бы читом; в WAN-профиле лечится датаграммами WebTransport с тройным повтором. (3) Игроки с RTT > 300 мс недокомпенсированы (C_MAX = 150) — сознательное LAN-first решение. (4) Реальное ухудшение сети входит в компенсацию до 30 с (асимметричная адаптация), «холодный» игрок после реконнекта ≈ 1 с играет с широкой σ.

**Что платим по продукту/инфраструктуре.** Быстрый игрок ждёт вердикта до 350 мс после нажатия (SIGame и так ждёт 300); выравнивание ARMED добавляет до E_MAX = 150 мс общей задержки в WAN-комнатах (0–5 мс на LAN). Транспортный якорь требует, чтобы игровой процесс сам терминировал TCP/QUIC (Go/Rust/uWebSockets; на Node/Bun без FFI остаётся только якорь рукопожатия + авто-реконнект — граница чита 40 мс вместо 20). Нужна дисциплина монотонных часов и планировщика с точностью ~1 мс на сервере.

**Тай-брейк.** `likelihood` (по умолчанию) — единственный вариант, при котором ни низкий пинг, ни низкий джиттер не дают систематического перевеса внутри спорного множества, но игрокам придётся объяснить, что «почти одновременно» разыгрывается калиброванной лотереей; `mostLikely` проще объяснить и всегда даёт лидера точечной оценки (в примере — C), ценой того, что асимметрия пути становится маленьким, но постоянным перекосом; `random` — привычная SIGame-семантика, но выкидывает информацию; `lowestScore`/`allPlay` — игровые гандикапы для казуальных комнат.

**Сравнение режимов для API.** `arrival` — на одной проводной LAN практически неотличим от ServerRewind и проще, но на Wi‑Fi/WAN «побеждает пинг»; `clientReaction` — лучшее разрешение, нулевая защита; `hybridBounded` — компромисс, где читер всё ещё может выиграть до половины своего RTT; `serverRewind` — рекомендуемый дефолт для смешанных сетей, `arrival` — допустимый дефолт для LAN-профиля.
