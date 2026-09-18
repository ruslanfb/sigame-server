# Survey: open-source SIGame / «Своя игра» / Jeopardy implementations with online play — and lessons for a new backend

Scope: reference implementation (VladimirKhil SI ecosystem, incl. the SICore wire protocol), JS/TS and other clones found via GitHub topic `sigame` and searches in RU/EN, Jeopardy clones, and party-game (Jackbox/Kahoot-style) room-code architectures. Special focus: the buzzer-race / ping-fairness problem, media delivery, and pack formats.

---

## 1. Reference implementation: VladimirKhil SI ecosystem (C#/.NET + TypeScript)

**Repos**
- `VladimirKhil/SI` (209★, C#, .NET 10): SIGame desktop client, SIQuester editor, SImulator, and libraries SIPackages → SIEngine.Core → SIEngine → SICore (+ SICore.Network, SICore.Connections, SI.GameServer.Contract, SI.GameServer.Client). https://github.com/VladimirKhil/SI
- `VladimirKhil/SIOnline` (TS, React/Redux/SignalR/Tauri): web + Steam client. https://github.com/VladimirKhil/SIOnline
- `VladimirKhil/SIContentService` (.NET minimal API): upload/serve packages & avatars. https://github.com/VladimirKhil/SIContentService
- `VladimirKhil/SIStorage` (.NET + PostgreSQL): package metadata search (does not store files). https://github.com/VladimirKhil/SIStorage
- `VladimirKhil/SIStatisticsService`, `AccountService`, `AppRegistryService` (auxiliary).
- **Important: the online game server itself (the SignalR host that runs games) is NOT published** — only the contract (`SI.GameServer.Contract`), the client (`SI.GameServer.Client`) and the runtime library (SICore, which is what the server runs). The full repo list on the profile confirms no server repo. https://github.com/VladimirKhil?tab=repositories

**Layering (ARCHITECTURE.md)**: SIPackages (SIQ read/write) → SIEngine.Core (one question as a state machine: steps `SetAnswerer, AnnouncePrice, SetPrice, SetTheme, SetAnswerType, ShowContent, AskAnswer, Accept`) → SIEngine (package/round/question sequencing; selection strategies `SelectByPlayer`, `Sequential`, `RemoveOtherThemes` for the final; stages Begin→GameThemes→Round→SelectingQuestion→QuestionType→Question→EndRound→EndGame) → SICore (players/showman/viewers, timers, network messaging; `GameData` = central session state, `QuestionPlayState` = per-question state, `GameController` = coordinator, `GameActions` = outgoing message helpers) → apps. Engine callbacks return `bool` (true = pause and wait for a decision).

**Transport**
- SignalR with **MessagePack** hub protocol. Two hubs: `{server}/sihost` (in-game) and `{server}/sionline` (lobby).
  - `sihost` client→server: `TryGetGameInfo(gameId)`, `JoinGame2(JoinGameRequest2{GameId, UserName, Role, Sex, AuthorizationMode, AuthTicket?, Password?, Pin?})`, `SendMessage(message)`. Server→client callbacks (`ISIHostClient`): `Receive(Message)`, `Disconnect()`, `GamePersonsChanged(gameId, persons[])`.
  - `sionline`: `GetGamesSlice(fromId)`; callbacks `GameCreated/GameChanged/GameDeleted`. The .NET client additionally has an **SSE** games stream (`OpenGamesStreamAsync`: initial snapshot in chunks, then updates).
  - REST: `POST /api/v1/games` (RunGameRequest{GameSettings, PackageInfo, ComputerAccounts}), `POST /api/v1/games/auto`, `GET /api/v1/games?pin=`, `GET /api/v1/info/host`, `/api/v1/info/bots`, `/api/v1/info/storage-filter/{id}`. Games are joined by numeric id + optional **PIN**/password; roles = Showman / Player / Viewer.
  - Automatic reconnect with exponential backoff (1–5 s) in SIOnline's `SIHostClient.ts`.
- **In-game message envelope**: `{ Text, IsSystem, Sender, Receiver }` with `Receiver='@'` for system messages to the game and `'*'` for chat. `Text` is a newline-separated string: `MESSAGE_TYPE\nARG1\nARG2...` (no JSON!). This is the SICore "agent protocol" documented in `src/SICore/SICore/GAME_AGENT_DOCUMENTATION.md`.

**Game state representation**: authoritative in-memory `GameData` on the server per game; clients rebuild their view from a stream of messages (`INFO2`, `TABLO2`, `SUMS`, `STAGE`, `TIMER`, …). There is no snapshot/state-sync message beyond `INFO2` (persons) and idempotent re-sends of `TABLO2/ROUNDSNAMES/OPTIONS2`; late joiners rely on the server replaying these. Time units are 1/10 s in `TIMER`, but internal helpers differ (`RunTaskTimer` ×100 vs `RescheduleTask` raw ms — the root of bug #311).

**Buzzer race** (SICore `Clients/Game/Game.cs`, `SI.Contracts/Models/ButtonPressMode.cs`):
```csharp
public enum ButtonPressMode { RandomWithinInterval, FirstWins, FirstWinsClient }
```
- Server sends `TRY` when the button opens; client presses with `I\n<deltaTime>`; server replies `YOUTRY` to the winner and `ENDTRY\n<playerIdx|A>` to all; press when not allowed → `PLAYER_STATE Lost` and `LastBadTryTime` set (button blocked for `TimeSettings.ButtonBlocking` seconds).
- SIOnline computes `deltaTime = Date.now() - table.canPressUpdateTime` (time since TRY was received) and disables the button locally for `timeForBlockingButton` after a press.
- Server logic (verbatim):
```csharp
case FirstWins:            ProcessAnswerer_FirstWinsServer(i)  // first packet to arrive wins, Stop(StopReason.Answer)
case RandomWithinInterval: PendingAnswererIndicies.Add(i); WaitInterval = ButtonsAccepting/100; Stop(StopReason.Wait)  // then random pick
case FirstWinsClient:
    if (PendingAnswererIndex == -1 || pressDurationMs > 0 && pressDurationMs < AnswererPressDuration) {
        PendingAnswererIndex = i; AnswererPressDuration = pressDurationMs > 0 ? pressDurationMs : ButtonsAccepting;
    } else PendingAnswererIndicies.Add(i);
    if (!IsDeferringAnswer) { WaitInterval = ButtonsAccepting/100; Stop(StopReason.Wait); }
```
- i.e. `FirstWinsClient` = "who pressed first *by the client's own reaction time*": after the first press the server waits `ButtonsAccepting` (default 300 ms) and picks the smallest client-reported reaction. This neutralises latency differences **but trusts the client** (a script can send `I\n1`). Auto-clickers already exist for SIGame: `archievega/SIGame_script`, `lennoximus/sigame-clicker`.
- Known issues: #305 (open) claims `ButtonsAccepting/100` yields a 3 ms window, making FirstWinsClient/Random modes ineffective for international play; SIOnline #284 (open) says the collection window is 300 ms and asks to make it configurable because VPN/remote players still lose; #227 (closed, milestone sigame-7.12) introduced the client-backed press time; #27 (open since 2020) proposes **pre-caching the whole pack encrypted per question and sending only the key** so that the question appears simultaneously regardless of server load (the `SI-Remastered` fork planned exactly this plus master/slave time sync). Steam guide: "режим нажатия кнопки — по версии клиента или сервера, либо случайный из нажавших; выбирайте то, что лучше работает в вашем случае; случайный не рекомендуется для обычной игры."

**Media**
- Packs are uploaded as ZIP to SIContentService: `POST /api/v1/content/packages` (multipart, size cap `MaxQualityPackageSizeMb`≈100–150 MB), `GET /api/v1/content/packages/{hash}/{name}` → path `/packages/{file}`; avatars similar (1 MB cap); `PUT /api/v1/import/packages` imports from URL; **content.xml is restricted to authorised users** (so players cannot read answers); GC every 30 min, TTL 3 h packages / 4 h avatars; drive-space thresholds reduce limits.
- In-game content arrives as `CONTENT_SHAPE` / `CONTENT\n<place: screen|replic|background>\n<layout>\n<type: text|image|audio|video|html>\n<value|URI>` (+ `CONTENT_APPEND`, `CONTENT_STATE`), external URIs allowed (client shows an "external media" warning).
- Preload: at round start the server sends `ROUNDCONTENT\n<uri…>`; SIOnline `contentPreloader.ts` fetches **sequentially** ("to avoid triggering the server rate limiter") with 500 ms base delay, 3 retries with backoff, 30 s timeout per file; audio is cached as ArrayBuffer in memory, images/video via DOM elements (browser cache); progress reported with `MEDIA_PRELOAD_PROGRESS\n<0..100>`, and `MEDIALOADED` is relayed to the showman.
- Playback sync: each client sends `MEDIA_COMPLETED`; the server counts completions vs total. Issue #311 (open): the counter includes host/viewers, is fixed at media start, does not verify sender (one client can send many completions), and a units bug makes the fallback delay 30 ms instead of 3 s → anyone can cut video for everyone. #325: media format handling bugs. #238: "partial image" logic should be decided server-side, not by 5 client-side conditions.

**Pack format (SIQ)**: ZIP with `content.xml` (schema `assets/siq_5.xsd`), `[Content_Types].xml`, folders `Images/ Audio/ Video/ Html/`. Hierarchy `package{name, version=5, id, date, publisher, difficulty 0-10, language, tags, info{authors, sources, comments, showmanComments}}` → `rounds/round{name, type=standard|final}` → `themes/theme{name}` → `questions/question{price, type}` with `params/param{name,type}` containing `item{type=text|image|audio|video|html, isRef, placement=screen|background|replic, duration=HH:MM:SS, waitForFinish}` and optional `numberSet{minimum,maximum,step}`, `right/answer*`, `wrong/answer*`. Question `typeName`: `simple` (default), `stake`, `secret`, `secretPublicPrice`, `secretNoQuestion`, `noRisk`, `forAll`, `stakeAll` (community editors also list: cat-in-bag, sealed-price cat, all-in, auction). Answer types: text, number (with `ANSWER_DEVIATION`), select (multiple choice via `LAYOUT`). v4 used `<scenario>` with `atom type=text|say|image|voice|video|marker` and awkward `groupStart/groupEnd` markers; v5 separates `script` (playback logic) from `params` (data); the author kept XML for compatibility and validation (LinkedIn article). Issue #310: URL-encoding Cyrillic filenames in the ZIP triples length, breaks the 255-char limit and the mapping content.xml→file; C# and TS disagree on what to encode.

**Other known problems**: #306 unknown send error; #346 "Pass before countdown not counted"; #345 cheating by looking up the pack on sibrowser and opening in SIQuester (proposal: hide package name/author until the end); habr guide: web client "могут быть баги, тормоза и дисконнекты", disconnects under server load; no LAN mode (Steam thread; developer: "Good idea — where would I find the time"). SImulator has websocket "web buttons" for phones on a LAN (issue #240) but it is a single-PC presentation app, not a game server.

### 1a. SICore in-game message protocol (from GAME_AGENT_DOCUMENTATION.md + Messages.cs)
Format: `TYPE\narg1\narg2…` inside `Message.Text`. Key messages:
- Lobby/session: `CONNECTED name role isMale index`, `DISCONNECTED name`, `INFO2 …`, `GAMEMETADATA game package contactUri`, `OPTIONS2 k v …` (falseStart, buttonBlockingTime, useApellations, displaySources, displayAnswerOptions…), `SET_OPTIONS`, `SETJOINMODE`, `SETHOST`, `KICK/BAN/UNBAN/BANNED/BANNEDLIST/YOU_ARE_KICKED`, `READY(+/-)`, `START`, `PAUSE +/- t0 t1 t2`, `MOVE 1|3 idx`, `CONFIG ADDTABLE|DELETETABLE|CHANGETYPE|FREE`, `PIN`, `AVATAR name mime uri`, `HOSTNAME`, `COMPUTERACCOUNTS`.
- Package/round: `STAGE BeforeGame|<round>|After`, `STAGE_INFO Round name idx`, `ROUNDSNAMES`, `PACKAGE`, `PACKAGE_AUTHORS/DATE/SOURCES/COMMENTS`, `GAMETHEMES`, `ROUND_AUTHORS/SOURCES/COMMENTS`, `ROUND_THEMES_COMMENTS`, `THEME2`, `TABLO2 themeCt qCt data`, `ROUNDCONTENT uri…`, `TOGGLE t q price`, `DELETE t`, `OUT`.
- Selection: `SETCHOOSER pIdx`, `SHOWTABLO`, `ASK_SELECT_PLAYER type min max`, `SELECT_PLAYER idx`, `CHOICE t q` (both directions).
- Question: `QUESTION price`, `QTYPE type isDefault isNoRisk` (legacy names simple|cat|bagcat|auction|sponsored), `QUESTIONCAPTION`, `CONTENT_SHAPE`, `CONTENT`, `CONTENT2`, `CONTENT_APPEND`, `CONTENT_STATE place idx normal|active|right|wrong`, `LAYOUT options columns`, `QUESTION_ANSWERS` (showman only), `QUESTION_AUTHORS/SOURCES/COMMENTS`, `QUESTION_COUNTER`, `QUESTION_PRICE_RANGE`, `STOP_PLAY`, `RESUME`, `QUESTION_END`.
- Buzzer/answer: `TRY`, `I deltaMs` (client), `YOUTRY`, `ENDTRY pIdx|A`, `PLAYER_STATE pIdx state`, `ANSWER text|number|select` (server) / `ANSWER text` (client), `ANSWER_DEVIATION`, `ORAL_ANSWER`, `ANSWER_VERSION pIdx prelim`, `PLAYER_ANSWER pIdx text`, `PASS pIdx`, `ASK_VALIDATE pIdx answer aiSuggestion`, `VALIDATE answer +/- factor`, `VALIDATION2`, `ISRIGHT +/- factor`, `PERSON +/- pIdx points`, `RIGHT_ANSWER_START`, `RIGHTANSWER text`, `SUMS s…`, `CHANGE pIdx sum`, `PLAYER_SCORE_CHANGED pIdx sum reason`, `APELLATE +/-`, `APPELLATION mode pIdx`, `PLAYER_APPELLATING`.
- Stakes/final: `ASK_STAKE type min max step`, `SET_STAKE Stake|AllIn|Pass value`, `PERSONSTAKE pIdx type amount`, `PERSONFINALSTAKE pIdx`, `FINALTHINK secondsX10`, `ROUND_END reason`, `STOP`, `WINNER pIdx|-1`, `GAME_STATISTICS`, `ASK_REVIEW url`, `LEADERBOARD`.
- Timers: `TIMER idx GO|PAUSE|RESUME|STOP|MAXTIME duration type` (idx 0=round, 1=thinking, 2=decision; duration in 1/10 s).
- Media: `MEDIALOADED`, `MEDIA_PRELOAD_PROGRESS pct`, `MEDIA_COMPLETED` / legacy `ATOM`.
- Chat/replics: `REPLIC source text`, `SHOWMAN_REPLIC seed code args`, `SHOWMAN_COMMENTS`; errors `USER_ERROR code params` (CannotKickYourself, OversizedFile, AppellationFailedTooFewPlayers…), `GAME_ERROR`.
- Obsolete but present: `CONNECT`, `FALSESTART`, `WRONGTRY`, `TIMEOUT`, `HINT`, `READINGSPEED`, `BUTTON_BLOCKING_TIME`, `ROUNDTHEMES`, `THEME_COMMENTS`, `FIRSTSTAKE/FIRSTDELETE`, `PERSONFINALANSWER`, `GAMEINFO`, `TEXTSHAPE`.
Full constant list is in `src/SICore/SICore/Messages.cs`.

---

## 2. JS/TS and other clones

| Project | Stack / transport | State | Buzzer & latency | Media | Packs | Notes |
|---|---|---|---|---|---|---|
| **alikzilla/svoyak** (TS monorepo: React/Vite + Node ws) https://github.com/alikzilla/svoyak | raw WebSocket (socket.io-style events), room code + QR on LAN, Cloudflare Tunnel/ngrok for remote via `PUBLIC_URL`, `ALLOWED_HOSTS` | pure reducer `reduce(state, action) → {state, effects}`; no timers/network in core; per-role **projections** (players never receive answers); persisted to `data/`; reconnect by token in localStorage; undo | **«Честная кнопка»**: client pings `clock:ping {t0}` every 2 s, keeps best of last 10 samples (min RTT), NTP-style `offset = tServer + rtt/2 − t1`; press sends `{clientTime, clockOffset, minRtt}`; server: `inServerTime = clientTime+offset`, clamped to `[receivedAt − minRtt − 250ms, receivedAt]`; waits 150 ms after first press; earliest wins; `lockedUntil` for false starts, `spentPlayerIds` | `uploads/<pack>/`, images/audio/video ≤ 50 MB, HTTP `mediaApi.ts` | JSON native; `.siq` import with compatibility report (`packs/siq/importSiq.ts`) | Best-in-class fairness design among clones, but the client supplies its own `clockOffset/minRtt` (should be server-side stats) |
| **OpenQuester/OpenQuester** (TS: Node/Express/Socket.IO/TypeORM/PostgreSQL/Redis/MinIO; Flutter client) https://github.com/OpenQuester/OpenQuester | Socket.IO + REST (OpenAPI SDK) | game state, timers, socket sessions in **Redis**; **per-game action queue + Redis lock** (`game:action:queue:{id}`, `SET NX EX 10`), Lua drain; handlers return declarative `DataMutation[]` applied in fixed order (redis → sockets → stats → disconnects → completions → broadcasts); timer expirations are queued actions too | strict FIFO serialization per game; order = arrival at Redis (no latency compensation documented) | MinIO S3; **media download sync**: question sent with `mediaDownloaded:false`, clients emit `MEDIA_DOWNLOADED`, server broadcasts `MEDIA_DOWNLOAD_STATUS {allPlayersReady}`; UI shows who is still loading; currently not enforced, 10 s timeout proposed | native `.oq`; `.siq` import WIP | Most complete modern TS backend; Grafana, Docker Compose |
| **kharkovdenys/sigame-server** (+ `sigame` client, TS) | Express + Socket.IO, hosted on Glitch | in-memory | not documented (arrival order) | uses `get-video-duration`/`get-audio-duration` to time media | parses `.siq` with `adm-zip` + `fast-xml-parser` | Small hobby project |
| **mezidia/SIGame** (plain JS) | Node `ws`, singleton server with rooms | in-memory | README admits the button "как-то странно работает" | — | — | Repo now 404 on fetch; info from search snippets |
| **gurland/SIGame_web** (Python, archived 2020) | Flask (users/rooms/upload) + aiohttp (gameplay/chat) + nginx, docker-compose | — | — | — | — | Split REST vs realtime services |
| **donebd/SIGame** (TS → single HTML) | no server: BroadcastChannel (same browser) or WebRTC via PeerJS (LAN remote control) | `GameStateManager`, command pattern | none (host-driven, no player buttons) | local files | folder structure `Round/Category - Points/{question.txt, audio.mp3…}` and `.siq` | Offline-party oriented |
| **Kartavichh/SIgame-linux** (Rust core + headless server + Tauri client) | TCP + JSON on :7777, HTTP media on :7778, binds 0.0.0.0 (LAN) | pure core crate (packs + rules), no net/UI | first packet wins | HTTP from server | `.siq` import (classic and v5) and export | Clean core/server/client split |
| **arsdehnel/rwsdk-jeopardy** (RedwoodSDK, Cloudflare Durable Objects) | WebSocket via DO, `useSyncedState` | shared synced object incl. buzzer queue | queue order = arrival | — | TS constants | Roles Host / Display / Contestant |
| **tpavlek/Jeopardy** (PHP ReactPHP + WAMP WS) | WS :9001 behind mod_proxy_wstunnel | `$board` in process memory (lost on restart) | **client records timestamps** at buzzer-armed and at buzz; server waits **500 ms** after first buzz, shortest client-measured time wins; README: spoofable, and >500 ms one-way ping loses | — | JSON scraped from J-Archive | Same idea as SIGame FirstWinsClient |
| **theGrue/jeopardy** (Node/Express/Angular/Socket.IO) | Socket.IO | scores/round/control in memory, logged to files | physical buzzers (BYOB) | images proxied from J-Archive, audio/video mis-rendered | J-Archive via cheerio | |
| Editors/tools: **muirch/SIQuesterPlus** (TS, byte-identical content.xml re-emit, CLI `siq new/validate/stats/pack`, media optimize), **kkqr77/siqeditor** (browser, JSZip, SIQ4→5 auto-convert, deflate-9), **Spring3/siq2json** (siq→JSON + imagemin), **si-convert** (SIQ↔YAML, PyPI), **GoldensFire/SI-HYX** (ffmpeg compression for packs) | | | | | | Useful for import/export parity |
| Bots: **goosescout/sigamebot** (Discord, Python/Flask REST for packs), **ffPerchik/Sigame** (Telegram), **Montece/HoloQuestions** (VK) | | | | | | Show demand for a pack REST API |

Party-game references:
- **Jackbox** (ecast / API v2, reverse-engineered by `InvoxiPlayGames/johnbox`, `tjhorner/JackboxGPT3`, `kklash/jackbox`): host runs the game; phones open jackbox.tv, `GET https://ecast.jackboxgames.com/api/v2/rooms/{code}` returns `{appId, roomCode, audienceEnabled, passwordProtected, server, joinAs}`; then a WebSocket with a JSON envelope `{seq, opcode, result/body}`; state is a set of keyed entities (`text`, `object`, `number`…) e.g. `bc:room`, `bc:customer:<id>`, updated with `*/update` opcodes; roles host/player/audience/moderator; 4-letter room codes; johnbox lacks reconnection, multi-room, passcodes — typical hard parts.
- **Kahoot** (`idiidk/kahoot-api`, clones): PIN → REST reserve-session (session token + JS challenge) → CometD/Bayeux over WebSocket; server timestamps drive scoring; a well-built clone notes: "client can never set its own score; every answer is checked against the socket bound to that participant, not an id in the payload".
- **asaf-shitrit/openpartygames** (Cloudflare Workers + Durable Objects + D1): one DO per room, first joiner is VIP/host, phones reconnect with a token after DO restarts ("short Reconnecting…"), games as versioned JSON content packs + SDK with seeded RNG.
- **Buzzonk** (commercial buzzer): order = packet arrival; refuses client timestamps ("player clocks are not trustworthy"); admits remote players with better routing win; regional servers SF/NYC/Frankfurt.
- **godot-phone-mass-controllers #6**: measured 26–56 ms extra RTT through a Cloudflare tunnel; recommends LAN mode when co-located, timestamp inputs on the phone with server-clock offset (`serverNow()`), host decides "compensation bounded by RTT", and exposing per-player RTT.
- Theory: arXiv 2602.09148 "Probabilistic Fair Ordering of Events" (model per-clock sync error, compare noisy timestamps probabilistically, resolve intransitivity with social-choice ranking) and US patent 11611510 "Network latency fairness in multi-user gaming platforms" (timestamp uplink packets, estimate max uplink delay across clients, equalize).

---

## 3. The buzzer-fairness problem: approaches compared

| # | Approach | Who uses it | Pros | Cons |
|---|---|---|---|---|
| A | Server arrival order (`FirstWins`) | SIGame default, Buzzonk, OpenQuester, rwsdk, kharkovdenys, SIgame-linux | trivial, uncheatable by timestamps | lowest ping always wins (the user's killer use case) |
| B | Client-measured reaction time since button-open (`FirstWinsClient`, `I\n<Δms>`), collect window 300 ms | SIGame 7.12+, tpavlek (500 ms) | removes both uplink and downlink asymmetry if honest; simple | fully spoofable; presses arriving after the window are lost; SIGame's window too small for intercontinental play (#305/#284) |
| C | Random among all who pressed in a window (`RandomWithinInterval`) | SIGame option | removes skill/ping entirely | "not recommended for normal play"; still needs a wide enough window |
| D | Synced clock + client timestamp clamped to plausibility `[recv − RTT − slack, recv]`, collection window, earliest wins | svoyak («честная кнопка») | bounded cheating (can gain at most ~RTT+slack), fair across pings, no trust in raw clocks | needs continuous ping; svoyak lets the client report its own offset/RTT (fixable) |
| E | Scheduled synchronous open: server broadcasts `openAt = T_server` in advance and pre-delivered/pre-cached content, each client unlocks locally at `T` | proposed in SI #27 and SI-Remastered (encrypted precache + key), godot #6 | eliminates the *downlink* skew for seeing the question/media, which timestamps alone don't fix | needs clock sync and preloading; cache must be encrypted/segmented so answers/next questions cannot be read |
| F | Delay equalisation (hold server events until the slowest client's expected arrival) | patent 11611510 | equal experience | adds latency to all, jitter not compensated |
| G | Probabilistic ordering with per-client error model; ties → random | arXiv 2602.09148 | principled | overkill for ≤ 12 players; implement as "ties within ε → random" |

**Recommended composite for the new backend** (D + E + anti-cheat, with A/B/C as host-selectable fallbacks):
1. Server-authoritative monotonic clock. Every connection runs an NTP-like ping (1–2 s, keep min-RTT sample of the last 10; store `offset`, `minRtt`, jitter **on the server**, never accept them from the client).
2. Before enabling the button, server sends `BUTTON_ARM {openAt: T_server, questionId}` at least `max(RTT/2)+jitter` ahead; clients unlock at `openAt` converted to local time; the content for that question must already be preloaded (see §4). Any press with `t < openAt` is a false start → `lockedUntil`.
3. Client sends `PRESS {questionId, clientTs}` (plus a monotonic seq). Server computes `tPress = clamp(clientTs + offset, recv − minRtt − slack, recv)`; `reaction = tPress − openAt`.
4. After the first press, collect for `W = min(cap≈400 ms, maxRTT_of_players + jitter)`; then pick min `tPress`; if the top two are within ε (10–20 ms) pick randomly (or by tie-break rule chosen by host). Broadcast the result with each candidate's reaction so the UI can show "you were 12 ms late".
5. Anti-cheat: rate-limit presses; ignore reactions below a human floor (≈60–80 ms after `openAt`, configurable) or flag them; server-side statistics on suspiciously constant reactions; bind every action to the socket/session, not to an id in the payload; hide pack identity until game end (#345); never send right answers/next content to player roles (per-role projections).
6. Expose per-player RTT/jitter in the lobby and let the host choose `fair | firstArrival | random` and the window; log per-press telemetry for tuning.

---

## 4. Concrete lessons for a new backend

**Architecture / state**
- Keep a pure, deterministic game core (`reduce(state, action) → {state, effects}`; svoyak, SIgame-linux, SIEngine) with no timers or sockets inside; drive it from a per-room actor that serializes actions (OpenQuester's per-game queue + lock, or a Durable-Object/actor per room). Timer expirations must be actions in the same queue.
- Per-role state projections (host/showman, player, viewer/screen) — players must never receive answers, showman comments, or content of unopened questions.
- Explicit snapshot + event log for reconnect (`token in localStorage`, re-join gets the projection); SI's replay-of-messages approach makes late-join/reconnect fragile.
- Prefer JSON (or MessagePack) typed events over SI's `"TYPE\narg\narg"` strings; generate OpenAPI + AsyncAPI/JSON-schema for both REST and WS events (OpenQuester ships an OpenAPI SDK). Keep the SI message list as a checklist of required semantics (stakes, secret questions, final round theme deletion, appellations, pause/resume with three timers, media completion, preload progress, player states, join modes, ban lists).
- Units: pick one time unit (ms) everywhere — SI's mix of 1/10 s, 100 ms ticks and ms produced real bugs (#311, #305).

**Rooms / LAN**
- Room code + QR (svoyak, Jackbox, openpartygames), server bound to `0.0.0.0` for LAN, optional mDNS/UDP broadcast discovery, optional tunnel for remote friends with an allow-list of public hosts. SIGame has no LAN mode — this is a differentiator.
- Roles: showman (host), players (max 6–12), viewers/screen ("Display" role in rwsdk); allow bots/"computer accounts" like SI.

**Buzzer**: see §3.

**Media**
- Store media by content hash in object storage (MinIO/S3 or local disk with immutable cache headers), keep original filename only in the manifest — avoids the URL-encoding/255-char issue (#310); serve via HTTP with Range support for video; gifs are just images.
- On import: ffprobe durations (needed for timers — SI's `duration` attribute, kharkovdenys uses `get-*-duration`), optional transcode/downscale (SI-HYX, siq2json, SIQuesterPlus optimise); enforce per-file limits (SIQuester warns >1 MB; svoyak 50 MB; SIContentService ~100–150 MB per pack).
- Preload per round (`ROUNDCONTENT` idea) with progress + readiness barrier before a media question (OpenQuester `MEDIA_DOWNLOAD_STATUS`, SI `MEDIALOADED`), with a host-visible list of who is still loading and a timeout (10 s). Preload in parallel with a small concurrency limit instead of SI's sequential 500 ms-spaced fetches.
- Protect content: signed/opaque URLs per game, don't expose `content.xml`/answers to players (SIContentService restricts it); if pre-caching future questions for fairness (#27), encrypt per question and release keys at reveal time.
- Media-completed sync must count only players, verify the sender, ignore duplicates, and have a wall-clock fallback (#311).

**Packs**
- Model the native format on SIQ v5 semantics: package → rounds (standard|final) → themes → questions {price, typeName, params (question/answer content lists with placement/duration/waitForFinish), right[], wrong[], answer type text|number±deviation|select}; keep `typeName` + params extensible (SI's stated reason for v5). Store as JSON in DB + media references; import `.siq` v4 and v5 (both are common in the wild) and export `.siq` for compatibility with SIGame/SIQuester (SIQuesterPlus proves byte-level parity is achievable); consider YAML/JSON text import for bulk authoring (si-convert, svoyak `price | question | answer`).
- Provide a pack REST API (search/filter/tags/language like SIStorage; CRUD like sigamebot's Flask API) and a browser editor that validates before a room can be created (svoyak).

**Ops / anti-abuse**: rate limits on uploads and press events, GC of unused uploads (SIContentService: TTL + disk thresholds), telemetry for press timings, integration tests for the buzzer window under simulated latency (svoyak has `buzz.test.ts`, `socket.integration.test.ts`).


## Key facts
- VladimirKhil's online SIGame game server is not open source; only SI (SICore runtime, contracts, client), SIOnline (web client), SIContentService and SIStorage are public.
- SIGame transport: SignalR + MessagePack, hubs /sihost (TryGetGameInfo, JoinGame2, SendMessage; callbacks Receive/Disconnect/GamePersonsChanged) and /sionline (GetGamesSlice; GameCreated/Changed/Deleted), REST /api/v1/games (POST run, GET ?pin=), /api/v1/info/host, SSE games stream.
- In-game messages are strings 'TYPE\narg1\narg2' in {Text, IsSystem, Sender, Receiver}; full list in SICore Messages.cs and GAME_AGENT_DOCUMENTATION.md (I, TRY, YOUTRY, ENDTRY, CHOICE, ANSWER, SET_STAKE, VALIDATE, CONTENT, ROUNDCONTENT, MEDIA_COMPLETED, TIMER, PAUSE, etc.).
- SIGame ButtonPressMode enum: RandomWithinInterval, FirstWins (server arrival), FirstWinsClient (client sends 'I\n<ms since TRY>', server collects for ButtonsAccepting≈300 ms and picks the smallest reaction) — spoofable; auto-clickers exist (SIGame_script, sigame-clicker).
- Open SI issues: #305 (WaitInterval = ButtonsAccepting/100 too small for international play), SIOnline #284 (300 ms window insufficient for VPN/remote players, make configurable), #27 (pre-cache encrypted pack, send only keys so questions appear simultaneously), #311 (MEDIA_COMPLETED sync exploitable: counts viewers, no sender check, 30 ms vs 3 s units bug), #310 (URL-encoded Cyrillic filenames break packs), #345 (players look up pack on sibrowser to cheat).
- alikzilla/svoyak implements the fairest buzzer found: NTP-like clock sync every 2 s (best of 10 samples), press carries client timestamp, server clamps to [recv − minRtt − 250 ms, recv], waits 150 ms after first press, earliest server-time wins; also per-role projections and pure reducer core with LAN QR/room-code join.
- OpenQuester (Node/TS/Socket.IO/Redis/Postgres/MinIO/Flutter) serializes all actions per game via Redis queue + lock with declarative DataMutations, and implements a media-download readiness barrier (MEDIA_DOWNLOADED / MEDIA_DOWNLOAD_STATUS allPlayersReady).
- tpavlek/Jeopardy (PHP) uses the same client-timestamp + 500 ms window idea as SIGame FirstWinsClient and documents its spoofability and loss of >500 ms presses; Buzzonk deliberately uses packet-arrival order and admits remote players with better routing have an edge.
- SIQ format: ZIP with content.xml (siq_5.xsd) + Images/Audio/Video/Html; package→rounds(standard|final)→themes→questions{price,typeName,params(items text|image|audio|video|html with placement/duration/waitForFinish),right,wrong}; typeNames simple, stake, secret, secretPublicPrice, secretNoQuestion, noRisk, forAll, stakeAll; v4 used <scenario>/atoms, v5 split script vs params.
- SIContentService media API: POST /api/v1/content/packages (multipart, ~100–150 MB cap), GET /api/v1/content/packages/{hash}/{name}, avatars 1 MB, PUT /api/v1/import/packages from URL, content.xml restricted to authorised users, GC every 30 min, TTL 3–4 h.
- SIOnline preloads round media sequentially with 500 ms spacing (to avoid the server rate limiter), 3 retries, 30 s timeout, audio cached as ArrayBuffer, and reports MEDIA_PRELOAD_PROGRESS in 10% steps.
- Jackbox ecast: REST room lookup by 4-letter code (ecast.jackboxgames.com/api/v2/rooms/{code}), then WebSocket with {seq, opcode, body} and keyed entities (bc:room, bc:customer:<id>) updated via */update opcodes; Kahoot: PIN → session token/challenge → CometD over WebSocket, server timestamps for scoring.
- Recommended design: server clock sync per connection (server-side offset/RTT), scheduled BUTTON_ARM openAt in server time with content preloaded, client timestamps clamped to plausibility, collection window = min(~400 ms, maxRTT+jitter), ties within ε random, human-floor/anti-automation checks, per-player RTT shown in lobby, host-selectable modes.
- Modern references for room-per-actor design: Cloudflare Durable Objects (rwsdk-jeopardy, openpartygames with reconnect tokens), OpenQuester Redis queue+lock; SIGame itself has no LAN mode (developer acknowledged the request but has no time).

## Sources
- https://github.com/VladimirKhil/SI
- https://github.com/VladimirKhil/SI/blob/master/README.md
- https://raw.githubusercontent.com/VladimirKhil/SI/master/ARCHITECTURE.md
- https://raw.githubusercontent.com/VladimirKhil/SI/master/src/SICore/SICore/README.md
- https://raw.githubusercontent.com/VladimirKhil/SI/master/src/SICore/SICore/Messages.cs
- https://raw.githubusercontent.com/VladimirKhil/SI/master/src/SICore/SICore/GAME_AGENT_DOCUMENTATION.md
- https://raw.githubusercontent.com/VladimirKhil/SI/master/src/SICore/SICore/Clients/Game/Game.cs
- https://raw.githubusercontent.com/VladimirKhil/SI/master/src/SICore/SICore/Clients/Game/GameController.cs
- https://raw.githubusercontent.com/VladimirKhil/SI/master/src/SICore/SICore/Clients/Game/QuestionPlayState.cs
- https://raw.githubusercontent.com/VladimirKhil/SI/master/src/SICore/SI.Contracts/Models/ButtonPressMode.cs
- https://raw.githubusercontent.com/VladimirKhil/SI/master/src/Common/SIPackages/README.md
- https://raw.githubusercontent.com/VladimirKhil/SI/master/src/Common/SIEngine/DOCUMENTATION.md
- https://raw.githubusercontent.com/VladimirKhil/SI/master/src/Common/SIEngine.Core/DOCUMENTATION.md
- https://raw.githubusercontent.com/VladimirKhil/SI/master/assets/siq_5.xsd
- https://raw.githubusercontent.com/VladimirKhil/SI/master/src/Common/SI.GameServer.Contract/ISIHostClient.cs
- https://raw.githubusercontent.com/VladimirKhil/SI/master/src/Common/SI.GameServer.Contract/ISIOnlineClient.cs
- https://raw.githubusercontent.com/VladimirKhil/SI/master/src/Common/SI.GameServer.Contract/GameRules.cs
- https://raw.githubusercontent.com/VladimirKhil/SI/master/src/Common/SI.GameServer.Contract/RunGameRequest.cs
- https://raw.githubusercontent.com/VladimirKhil/SI/master/src/Common/SI.GameServer.Contract/JoinGameRequest2.cs
- https://raw.githubusercontent.com/VladimirKhil/SI/master/src/Common/SI.GameServer.Client/IGameServerClient.cs
- https://raw.githubusercontent.com/VladimirKhil/SI/master/src/Common/SI.GameServer.Client/IGamesApi.cs
- https://github.com/VladimirKhil/SI/tree/master/src/Common/SI.GameServer.Contract
- https://raw.githubusercontent.com/VladimirKhil/SI/master/src/SIQuester/README.md
- https://raw.githubusercontent.com/VladimirKhil/SI/master/src/SImulator/README.md
- https://github.com/VladimirKhil/SI/issues
- https://github.com/VladimirKhil/SI/issues/27
- https://github.com/VladimirKhil/SI/issues/227
- https://github.com/VladimirKhil/SI/issues/238
- https://github.com/VladimirKhil/SI/issues/240
- https://github.com/VladimirKhil/SI/issues/305
- https://github.com/VladimirKhil/SI/issues/310
- https://github.com/VladimirKhil/SI/issues/311
- https://github.com/VladimirKhil?tab=repositories
- https://github.com/VladimirKhil/SIOnline
- https://github.com/VladimirKhil/SIOnline/issues
- https://github.com/VladimirKhil/SIOnline/issues/96
- https://github.com/VladimirKhil/SIOnline/issues/284
- https://github.com/VladimirKhil/SIOnline/issues/345
- https://raw.githubusercontent.com/VladimirKhil/SIOnline/master/src/client/SIHostClient.ts
- https://raw.githubusercontent.com/VladimirKhil/SIOnline/master/src/client/GameServerClient.ts
- https://raw.githubusercontent.com/VladimirKhil/SIOnline/master/src/client/game/GameClient.ts
- https://raw.githubusercontent.com/VladimirKhil/SIOnline/master/src/client/game/Messages.ts
- https://raw.githubusercontent.com/VladimirKhil/SIOnline/master/src/logic/messageProcessor.ts
- https://raw.githubusercontent.com/VladimirKhil/SIOnline/master/src/logic/ClientController.ts
- https://raw.githubusercontent.com/VladimirKhil/SIOnline/master/src/logic/contentPreloader.ts
- https://raw.githubusercontent.com/VladimirKhil/SIOnline/master/src/state/room2Slice.ts
- https://github.com/VladimirKhil/SIContentService
- https://raw.githubusercontent.com/VladimirKhil/SIContentService/main/src/SIContentService/EndpointDefinitions/ContentEndpointDefinitions.cs
- https://raw.githubusercontent.com/VladimirKhil/SIContentService/main/src/SIContentService/EndpointDefinitions/ImportEndpointDefinitions.cs
- https://github.com/VladimirKhil/SIStorage
- https://www.linkedin.com/pulse/sigame-package-format-evolution-vladimir-khil-okxof
- https://steamcommunity.com/sharedfiles/filedetails/?id=3456423604
- https://steamcommunity.com/app/3553500/discussions/0/565910456029687808/
- https://habr.com/ru/companies/timeweb/articles/920442/
- https://github.com/CrafterKolyan/SI-Remastered
- https://github.com/topics/sigame
- https://github.com/alikzilla/svoyak
- https://raw.githubusercontent.com/alikzilla/svoyak/main/server/src/engine/buzz.ts
- https://raw.githubusercontent.com/alikzilla/svoyak/main/server/src/io/registerSocketHandlers.ts
- https://raw.githubusercontent.com/alikzilla/svoyak/main/client/src/net/useClock.ts
- https://github.com/OpenQuester/OpenQuester
- https://github.com/OpenQuester/OpenQuester/tree/main/server
- https://raw.githubusercontent.com/OpenQuester/OpenQuester/main/server/docs/game-action-executor.md
- https://raw.githubusercontent.com/OpenQuester/OpenQuester/main/server/docs/media-download-sync.md
- https://github.com/kharkovdenys/sigame-server
- https://github.com/mezidia/SIGame
- https://github.com/gurland/SIGame_web
- https://github.com/donebd/SIGame
- https://github.com/Kartavichh/SIgame-linux
- https://github.com/muirch/SIQuesterPlus
- https://github.com/kkqr77/siqeditor
- https://github.com/Spring3/siq2json
- https://pypi.org/project/si-convert/
- https://github.com/goosescout/sigamebot
- https://github.com/archievega/SIGame_script
- https://github.com/tpavlek/Jeopardy
- https://github.com/theGrue/jeopardy
- https://github.com/arsdehnel/rwsdk-jeopardy
- https://github.com/InvoxiPlayGames/johnbox
- https://github.com/tjhorner/JackboxGPT3
- https://pkg.go.dev/github.com/kklash/jackbox
- https://github.com/asaf-shitrit/openpartygames
- https://deepwiki.com/idiidk/kahoot-api/2-core-architecture
- https://github.com/lloupp/kahoot
- https://buzzonk.com/
- https://github.com/splatterfacegames/godot-phone-mass-controllers/issues/6
- https://arxiv.org/abs/2602.09148
- https://image-ppubs.uspto.gov/dirsearch-public/print/downloadPdf/11611510
- https://www.gabrielgambetta.com/lag-compensation.html
