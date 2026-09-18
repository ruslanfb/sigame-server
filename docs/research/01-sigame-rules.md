# SIGame («Своя игра», Vladimir Khil) — Rules & Game-Flow Specification for a Server Implementation

Research method: the vladimirkhil.com pages are a JS SPA (only the title renders), so the primary source for this spec is the **actual engine source code** of SIGame (repo `VladimirKhil/SI`, .NET 10, MIT) and the web client (`VladimirKhil/SIOnline`), cloned and read locally (paths in *Sources*). Everything below marked *[code]* is taken directly from the engine (`SICore/GameController.cs`, `Game.cs`, `SIEngine`, `SIPackages/ScriptsLibrary.cs`, `siq_5.xsd`, `RulesSettings.cs`, `TimeSettings.cs`). Community guides (Habr, Steam, StopGame, iXBT) and Wikipedia are used for TV-show background and UI wording.

---

## 1. Terminology and the two rule layers

SIGame consists of three layers you must mirror:

| Layer | Repo project | Responsibility |
|---|---|---|
| Package model | `SIPackages` | SIQ v5 package: rounds → themes → questions; question *scripts*; well-known question types expand to scripts via `ScriptsLibrary` |
| Package engine | `SIEngine` + `SIEngine.Core` | Deterministic state machines: game stages (`Begin → GameThemes → Round → SelectingQuestion → QuestionType → Question → EndRound → EndGame`) and per-question script steps |
| Session runtime | `SICore` | Players/showman/viewers, timers, buzzer, stakes, appeals, scoring, network messages (text protocol `MSG\narg1\narg2…`, ~140 message types) |

The TV show background (Russian Wikipedia): 3 rounds + final, 3 players; Round 1 prices 100..500, Round 2 200..1000, Round 3 300..1500; "Кот в мешке" (must be given away), "Вопрос-аукцион" (bidding, ва-банк), final with 7 themes eliminated to one, wagers, written answers; players with ≤0 don't play the final. SIGame generalizes all of this and makes everything configurable.

---

## 2. Roles and permissions *[code: Game.cs handlers, Constants.cs]*

| Role | Can do |
|---|---|
| **Player** (`player`, max **12** tables, `Constants.MaxPlayers`) | `READY`, press button (`I [reactionMs]`), `PASS` (give up the right to press / pass in stakes), `ANSWER`, `ANSWER_VERSION` (live typing preview), `CHOICE theme q` (when chooser), `SELECT_PLAYER` (when giving a secret), `SET_STAKE`, `DELETE theme` (final), `APELLATE [+/-]`, vote in appeals (`ISRIGHT +/-`), `MARK` (complain about a question), chat, `MEDIALOADED`, `MEDIA_PRELOAD_PROGRESS`, `MEDIA_COMPLETED` |
| **Showman / ведущий** (`showman`, human or bot) | Validate answers (`ISRIGHT +/- [factor]`, `VALIDATE answer +/- [factor]` for questions-for-all), select starting player / next staker / next theme-deleter on ties (`SELECT_PLAYER`), `SETCHOOSER`, `CHANGE playerIndex(1-based) newSum`, `PAUSE +/-`, `MOVE dir` (−2 round back, −1 back, 1 next, 2 round next, 3 to round N), `TOGGLE theme q` (remove/restore a question on the table), in *oral* mode performs choices/stakes/answers on behalf of players; receives right/wrong answer lists (`VALIDATION2`, `QUESTION_ANSWERS`) |
| **Host / владелец комнаты** (`HostName`, usually the creator; transferable) | `KICK`, `BAN`, `UNBAN`, `SETHOST`, `SET_OPTIONS` (change rules mid-game), `CONFIG ADDTABLE/DELETETABLE/FREE/SET/CHANGETYPE` (manage seats and bots), `SETJOINMODE` (AnyRole / OnlyViewer / Forbidden), `PAUSE`, `MOVE`, `START` |
| **Viewer / зритель** | Watch everything, chat; no game actions |
| **Bots** | Computer players/showman; cannot be kicked (`CannotKickBots`), cannot become host |

Kick errors: `CannotKickYourSelf`, `CannotKickBots`, `CannotSetHostToYourself`, `CannotSetHostToBots`. Game starts when all main persons are `READY` (or, for automatic games, when all player slots are connected; auto-start timer 300 s).

---

## 3. Package structure (SIQ v5) *[code: siq_5.xsd, SIPackages/Core/*.cs, ScriptsLibrary.cs]*

**File**: ZIP with `content.xml` (required), `Images/`, `Audio/`, `Video/`, `Html/`, `[Content_Types].xml`; optional `<files>` with per-file hashes.

**`<package>` attributes**: `id`, `name` (required), `version`="5" (required), `restriction` (e.g. "18+"), `date`, `publisher`, `difficulty` 0–10, `logo`, `language`, `generator`, `contactUri`. Children: `tags`, `info` (authors, sources, comments, showmanComments, extension), `global` (authors/sources registry), `rounds`.

**Hierarchy**: `round(name, type)` → `themes/theme(name)` → `questions/question(price, type)`. Every level has optional `info` (authors/sources/comments/showmanComments).

**Round types** (`RoundTypes`): `standart` (alias `table`) — table round; `final` (alias `themeList`) — theme-list round. Standard TV template is 3 rounds × 6 themes × 5 questions + final of 7 themes × 1 question (Steam/iXBT guides; recommended final theme count = players×k + 1).

**Question**: `price` (int; `-1` = `InvalidPrice`, empty slot), `type` (see §5), `params`, optional `script`, `right/answer*` (≥1), `wrong/answer*`. Legacy `<type name=…><param name=…>` and `<scenario><atom type=text|say|image|voice|video|marker>` are deprecated and auto-upgraded on load.

**Well-known parameters** (`QuestionParameterNames`): `question` (content), `answer` (content; fallback = first right answer), `answerType` (`text` default | `select` | `number` | `point` | `client`), `answerOptions` (group of labelled content items for select), `answerDeviation` (numeric tolerance), `theme` (secret's own theme), `price` (numberSet), `selectionMode` (`any` | `exceptCurrent`), `answerDuration` (seconds, overrides answer timer).

**Parameter types** (`StepParameterTypes`): `simple` (string), `content` (list of items), `group` (nested params), `numberSet` (`<numberSet minimum= maximum= step=/>`; textual syntax `[min;max]/step`; **`maximum=0` means "minimum or maximum price of the current round"**).

**Content item** attributes: `type` = `text|image|audio|video|html`, `isRef` (value is a file name in the media folder or an absolute URL), `placement` = `screen|replic|background` (background is audio only; replic is showman speech text), `duration` `hh:mm:ss`, `waitForFinish`. Multiple items may be shown at once (complex content). GIF/WebP/PNG/JPG images, MP3 audio, MP4 video, HTML are supported (Steam guide: images ≤1 MB, audio ≤5 MB, video ≤10 MB under "quality control", package ≤100–150 MB — these are hosting limits, not format limits).

**Script steps** (`StepTypes`, executed in order by `QuestionEngine`): `SetAnswerType`, `SetAnswerer(mode, select, stakeVisibility)`, `SetTheme`, `AnnouncePrice`, `SetPrice(mode=select|multiply, numberSet)`, `ShowContent`, `AskAnswer(mode=button|direct, duration)`, `Accept`. A question without `script` uses the predefined script of its `type` (§5). Custom scripts allow authors to build new types; unknown *type names* without a script are skipped with "unsupported question" replic.

---

## 4. Game modes, round strategies, default question types *[code: WellKnownGameRules.cs, GameModes.cs, TurnSwitchingStrategy]*

| Mode (RU label) | Table rounds | Theme-list/final rounds | Turn switching |
|---|---|---|---|
| **Classic / Tv** («Классическая») | `SelectByPlayer`, default type `simple` (с кнопкой); game themes shown before start | `RemoveOtherThemes`, default type `stakeAll` | `ByRightAnswerOnButton`: chooser becomes the player who answered a button question correctly |
| **Simple / Sport** («Упрощённая») | `Sequential` (no table shown, questions in order), default `simple` | `RemoveOtherThemes`, `stakeAll` | never |
| **Quiz** («Квиз») | `Sequential`, default `forAll` | `Sequential`, `stakeAll` | never |
| **TurnTaking** («Игра по очереди») | `SelectByPlayer`, default `noRisk` (для себя) | `RemoveOtherThemes`, `stakeAll` | `Sequentially`: chooser index = (chooser+1) mod players after each question |

A question with empty `type` takes the round's default type; `QTYPE` message carries `isDefault` so clients don't announce default types.

**Engine stages**: `Begin → [GameThemes] → Round → SelectingQuestion → QuestionType → Question → … → EndRound → … → EndGame`. Round end reasons: `Completed` (`empty`), `Timeout`, `Manual`.

---

## 5. Question types — exact scripts and rules

Type ids (`QuestionTypes`), RU names (SIQuester/SIOnline), legacy aliases: `simple`=`withButton` («С кнопкой», legacy default), `stake` («Со ставкой», legacy `auction`), `stakeAll` («Для всех со ставкой»), `secret` («С секретом», legacy `cat`/`bagcat` with knows=after), `secretPublicPrice` («С секретом и стоимостью, объявляемой до передачи», legacy `bagcat` knows=before), `secretNoQuestion` («С секретом без вопроса», legacy `bagcat` knows=never), `noRisk`=`forYourself` («Для себя»/«Без риска», legacy `sponsored`), `forAll` («Для всех»). Legacy registry (vladimirkhil.com/content/docs/QuestionsTypes.xml): simple, auction, cat, bagcat, sponsored.

### 5.1 `simple` / `withButton` — вопрос с кнопкой
Script: `SetAnswerType → ShowContent(question) → AskAnswer(button) → ShowContent(answer)`.
Everyone who `CanPress` competes on the buzzer (§6). Right: +price (`CurPriceRight`); wrong: −price (`CurPriceWrong`, subject to `QuestionWithButtonPenalty`), the player loses the right to press for this question, others may press again. Chooser → correct answerer (Classic).

### 5.2 `stake` — вопрос со ставкой (аукцион) *[code: SetAnswererByHighestVisibleStake, AskStake, DetectNextStaker, OnDecisionStakeMaking, TryDetectStakesWinner, OnSetStake]*
Script: `SetAnswerType → SetAnswerer(mode=stake, select=highest, stakeVisibility=visible) → ShowContent → AskAnswer(direct) → answer`.

Bidding algorithm (nominal = question price; `StakeStep` = largest power of 10 ≤ minimum positive price of the round, e.g. 100 for a 100..500 round):
1. Participants: the chooser always; any other player with `Sum > nominal`. Others are marked `Pass` immediately.
2. Order: chooser bids first. The next bidder is chosen among not-yet-bid participants with the **lowest score**; ties → showman selects (`ASK_SELECT_PLAYER staker`, `ShowmanDecision` timer; on timeout random). Order is then cyclic among players still bidding.
3. Options offered to the active bidder (`ASK_STAKE modes min max step reason`):
   - **Nominal** only (forced automatically, no prompt) when no bid yet and `Sum < nominal`, or `Sum == nominal` and nobody else has more.
   - Otherwise minimum raise = `currentStake + StakeStep` (or nominal if no bid yet), rounded up to a `StakeStep` multiple; max = player's `Sum`; **All-in** is always offered; **Pass** is offered only after at least one bid exists (the opener cannot pass); a custom **Stake** is offered only if nobody went all-in and the player can afford the minimum.
   - Server validates `SET_STAKE`: `min ≤ sum ≤ max`, and `(sum−min) % step == 0` unless `sum == max`.
4. After a bid, every other bidder whose `Sum ≤ stake` is auto-passed. A player may also send `PASS` early (unless they currently hold the highest bid). If a bidder's `Sum ≤ currentStake` when their turn comes, they are auto-passed.
5. **Ва-банк can only be beaten by another ва-банк** (the custom stake option disappears once `AllIn` is set; a later player may all-in with a larger sum).
6. Winner = the last player still bidding, or the current leader when nobody else can raise. Winner becomes chooser and sole answerer; `CurPriceRight = CurPriceWrong = stake`. Timeout on a bid = Pass (or Nominal if pass not allowed).
7. The question is then shown and answered **directly** (no buzzer), `SoloAnswering` timer (25 s). Right +stake, wrong −stake (all-in wrong → 0).

### 5.3 `secret`, `secretPublicPrice`, `secretNoQuestion` — вопрос с секретом («Кот в мешке») *[code: SetAnswererByActive, OnAnnouncePrice, OnSelectPrice, AcceptQuestion]*
Scripts:
- `secret`: `SetAnswerType → SetAnswerer(byCurrent, select=selectionMode) → SetTheme → AnnouncePrice → SetPrice(select) → ShowContent → AskAnswer(direct) → answer` (theme/price revealed **after** the transfer).
- `secretPublicPrice`: `SetAnswerType → SetTheme → AnnouncePrice → SetAnswerer(byCurrent) → SetPrice(select) → …` (theme and price announced **before** choosing the recipient).
- `secretNoQuestion`: `SetAnswerer(byCurrent) → SetTheme → AnnouncePrice → SetPrice(select) → Accept` — no question; the recipient simply **receives** the price ("IncomeWithoutAnswering") and becomes chooser.

Rules: the current chooser must give the question to a player; `selectionMode=any` lets them keep it (SIQuester: «Вопрос можно оставить себе»), `exceptCurrent` forbids keeping it. If only one eligible candidate → automatic. Selection timer `PlayerSelection` (30 s, «Выбор другого игрока»); timeout → random player other than the chooser. Price (`price` numberSet): fixed value; `maximum=0` → "minimum or maximum price of the round" (the recipient picks one of the two; if equal, fixed); range `[min;max]` (step = max−min, i.e. pick min or max) or `[min;max]/step` → the recipient selects a value (`ASK_STAKE` with `StakeMaking` timer; timeout → minimum). The recipient answers directly (`SoloAnswering`), right +price, wrong −price (penalty type is that of `forYourself` class? — no: secrets are flexible-price questions, normal subtraction applies). The recipient becomes chooser. The question's own `theme` replaces the table theme on screen (`QUESTIONCAPTION`).

### 5.4 `noRisk` / `forYourself` — вопрос без риска / для себя *[code: OnMultiplyPrice, QuestionForYourselfFactor/Penalty]*
Script: `SetAnswerType → SetAnswerer(current) → SetPrice(multiply) → ShowContent → AskAnswer(direct) → answer`.
Only the chooser answers, directly. Price is multiplied by `QuestionForYourselfFactor` (default **2**); wrong answer penalty is `QuestionForYourselfPenalty` (default **None**, i.e. `CurPriceWrong = 0`). Classic description: "correct → double reward, wrong → lose nothing". In TurnTaking mode this is the default type with sequential rotation.

### 5.5 `forAll` — вопрос для всех *[code: SetAnswerersAll, AskAnswer multiple, ValidateAfterRightAnswer]*
Script: `SetAnswerType → SetAnswerer(all) → ShowContent → AskAnswer(direct) → answer`.
All connected players answer simultaneously (written; `FINALTHINK`, `HiddenAnswering` = 45 s, «Ответ на вопрос для всех»). Each text answer is sent to the showman for validation as it arrives (`ASK_VALIDATE`), non-text answer types are validated automatically after the right answer is shown. Right +price, wrong −price (`QuestionForAllPenalty`, default SubtractPoints); empty answer on timeout = "-" (wrong). Default type in Quiz mode. Chooser does not change.

### 5.6 `stakeAll` — для всех со ставкой (final-round default) *[code: SetAnswerersByAllHiddenStakes, AskHiddenStakes, AnnounceStake]*
Script: `SetAnswerType → SetAnswerer(stake, select=allPossible, stakeVisibility=hidden) → ShowContent → AskAnswer(direct) → answer`.
Participants: connected players with `Sum > 0`, or **everyone** if `AllowEveryoneToPlayHiddenStakes` (default **true**, «Играют все игроки — даже с 0 или отрицательным счётом»). Each makes a **hidden** stake in `[1, Sum]` step 1 (`ASK_STAKE … final`); a player with `Sum ≤ 1` is auto-assigned stake 1; timeout → 1. `PERSONFINALSTAKE i` announces that a stake was made (amount hidden). Then content, `FINALTHINK` (45 s), written answers. Reveal is sequential per player: `PLAYER_ANSWER`, showman validates (or auto), then `PERSONSTAKE i 1 stake` and ±stake applied. Wrong all-in → 0.

### 5.7 Answer types (any question)
`text` (default; showman validates, fuzzy auto-check on timeout), `select` (options rendered via `LAYOUT`/`CONTENT`, `DisplayAnswerOptionsOneByOne`, labels on/off; after a wrong option it is excluded; when one option remains the question ends; validated automatically), `number` (±`answerDeviation`), `point` (click coordinates on an image, deviation ≥ 0.02), `client` (client decides right/wrong).

---

## 6. Buzzer mechanics *[code: AskToTry, OnButtonPress, DetectAnswererIndex, HandlePlayerMisfire, PrepareForAskAnswer, ContinueQuestion, OnPass]*

1. At question start every player gets `CanPress = true` (button questions only).
2. **When the button becomes active** depends on `FalseStart` (default **true**, «Фальстарты — нельзя нажимать на кнопку до окончания чтения вопроса»):
   - **Enabled**: `AskAnswer(button)` runs after all content → server sends `TRY` (the white frame), `YOUTRY` to each eligible player, and timer 1 `GO` with `ButtonPressing` (default 5 s, «Нажатие кнопки — уменьшение рамки»). Text is read at `ReadingSpeed` (20 chars/s) before that; images shown `Image` = 5 s.
   - **Disabled**: buttons are armed **before** content (`TRY NotFinished`); players can press while text is being revealed. `PartialText` / `PartialImages` (progressive reveal, `PartialImage` = 3 s) only apply in this mode. Engine has a third mode `TextContentOnly` (arm after text, before media) — SICore maps the boolean to Enabled/Disabled only.
3. **Press** = client message `I [reactionMs]`. Ignored while paused. If pressed when the game is not in `Pressing` state (before `TRY`, i.e. **фальстарт**, or after someone was already chosen): the press is a *misfire* — `PLAYER_STATE Lost`, and the player's button is blocked for `ButtonBlocking` seconds (default **3**, max 10, «Блокировка кнопки при фальстарте»). No score penalty for a false start.
4. **Who wins the button** (`ButtonPressMode`, «Режим нажатия кнопки»):
   - `RandomWithinInterval` (**default**, «Рандом из ранних нажавших»): the first press opens a window of `ButtonsAccepting` ms (default **300**, max 1000); all presses within it are collected and the winner is chosen **randomly** among them; losers get `PLAYER_STATE Lost`. *This is SIGame's own mitigation of the ping-unfairness problem.*
   - `FirstWins` («Кто первым нажал — по версии сервера»): first press to reach the server.
   - `FirstWinsClient` («по версии клиента»): each press carries the client-measured reaction time; within the accept window the smallest reported time wins.
5. Winner: `CanPress=false`, timer 1 paused with elapsed thinking time, media paused, `ENDTRY <index>` to all, `ANSWER text|select|number|point` to the player, `Answering` timer (default 25 s, «Ответ после нажатия на кнопку») to type; `ANSWER_VERSION` streams the draft; empty on timeout → answer "-" = wrong (in oral mode the showman decides).
6. Validation (§7). **Wrong**: −`CurPriceWrong`×factor, `PLAYER_STATE Wrong`; if `AnswerValidationFactor == 0` it counts as a pass (no score change). Then `ContinueQuestion`: if any connected player still `CanPress` → `RESUME` media, buttons re-armed (`TRY`; with false starts the thinking timer **resumes with remaining time**, without false starts reading continues), so **others can buzz again**; otherwise → jump to the answer.
7. **PASS** (player voluntarily gives up the button): `CanPress=false`, `PASS i`. When all eligible players passed/answered and nobody is pending → answer is shown.
8. **Nobody answered** («Никто не ответил»): thinking timer expires → `ENDTRY all` → right answer shown; scores unchanged; chooser unchanged (Classic).
9. **Right**: +`CurPriceRight`×factor, `PERSON + i sum`, `PLAYER_STATE Right`, timer stop, chooser := answerer (Classic), answer shown, `QUESTION_END`, `SUMS`.

---

## 7. Answer validation and scoring *[code: AskRight, WaitRight, OnIsRight, OnValidate, OnDecisionAnswerValidating, AnswerChecker]*

- Human text answers are sent to the showman (`VALIDATION2 name answer + allowPriceMod rightCount rights… wrongs…`); the showman replies `ISRIGHT +|- [factor]` (factor ≥ 0, default 1.0; e.g. 0.5 = half credit; 0 = treat as pass). `ShowmanDecision` timer 30 s (max 300); on timeout the server auto-validates with a fuzzy comparison (letter similarity > 0.81, digits > 0.99) against all right answers.
- Auto-validation (no showman prompt): bots, `select`/`number`/`point`/`client` answer types, answers already validated earlier in the same question (cached per answer string).
- Score deltas: right `+CurPriceRight × factor`; wrong `−CurPriceWrong × factor`. `CurPriceRight/Wrong` start at the question price and are overridden by stakes/secret price/multiplier. `CurPriceWrong = 0` when the type's penalty is `None`. **Negative scores are allowed** (no floor).
- Penalty configuration («Штраф за неверный ответ» — «Снимать очки» / «Нет штрафа (без риска)») per class: `QuestionWithButtonPenalty` (default SubtractPoints), `QuestionForYourselfPenalty` (None), `QuestionForAllPenalty` (SubtractPoints). Lobby flag `IgnoreWrong` («Снимать очки за неверный ответ» toggle; GameRules bitmask `FalseStart=1, Oral=2, IgnoreWrong=4`) = the "neutral game" mode from guides.
- Manual edits: showman `CHANGE playerIndex(1-based) newAbsoluteSum` → `PLAYER_SCORE_CHANGED`, `SUMS`. Score change reasons in protocol: answer, penalty, bonus, correction.
- `HintShowman`: send right answers to the showman at question start (streaming use-case).
- Winner at game end: max sum; ties → `WINNER -1` (no single winner). Game report collects accepted non-canonical answers, rejected answers, appealed answers, complaints (`MARK`).

---

## 8. Timers *[code: SI.Contracts/TimeSettings.cs, SIData/TimeSettings.cs, SIOnline TimeSettingsView.tsx, localization.ts]*

Protocol timer indexes: `0` round, `1` thinking/pressing/answering, `2` decisions (choice, stake, deletion). All protocol durations are in **0.1 s**; settings are in seconds unless noted.

| Setting (contract / legacy name) | RU label | Default | UI max | Used for / on timeout |
|---|---|---|---|---|
| `QuestionSelection` / `TimeForChoosingQuestion` | Выбор вопроса | 30 s | 120 | chooser picks a cell; timeout → random active question |
| `ThemeSelection` / `TimeForChoosingFinalTheme` | Выбор темы (удаление) | 30 s | 120 | final theme deletion; timeout → random theme |
| `PlayerSelection` / `TimeForGivingACat` | Выбор другого игрока (передача вопроса) | 30 s | 120 | secret transfer; timeout → random non-chooser |
| `ButtonPressing` / `TimeForThinkingOnQuestion` | Нажатие кнопки (уменьшение рамки) | 5 s | 120 | window to press after `TRY`; timeout → nobody answered |
| `ButtonsAccepting` | Интервал нажатий кнопок | 300 ms | 1000 | random/first-wins-client window |
| `Answering` / `TimeForPrintingAnswer` | Ответ после нажатия на кнопку | 25 s | 120 | typing after winning the button; timeout → "-" |
| `SoloAnswering` / `TimeForThinkingOnSpecial` | Ответ на спецвопрос | 25 s | 120 | stake/secret/noRisk direct answers (overridable by `answerDuration`) |
| `HiddenAnswering` / `TimeForFinalThinking` | Ответ на вопрос для всех | 45 s | 120 | forAll / stakeAll written answers |
| `StakeMaking` / `TimeForMakingStake` | Ставка | 30 s | 120 | each bid, secret price choice, hidden stakes; timeout → pass/nominal/1 |
| `ShowmanDecision` / `TimeForShowmanDecisions` | Решения ведущего | 30 s | 300 | validation, tie selections; timeout → auto-validate / random |
| `Round` / `TimeOfRound` | Раунд | 3600 s | 10800 | round ends (`ROUND_END timeout`) after the current question once elapsed |
| `ButtonBlocking` / `TimeForBlockingButton` | Блокировка кнопки при фальстарте | 3 s | 10 | misfire lockout |
| `Reflection` / `TimeForRightAnswer` | Вывод правильного ответа | 2 s | 10 | extra pause after text content and when showing the answer |
| `Image` / `ImageTime` | Вывод изображения | 5 s | 10 | image display duration |
| `PartialImage` / `PartialImageTime` | Вывод частичных изображений | 3 s | 20 | progressive image reveal |
| `Appellation` | — | 30 s | — | voting window |
| `ReadingSpeed` (rule) | Скорость чтения | 20 chars/s | — | text duration; 0 in Managed mode |

Pause (`PAUSE + t0 t1 t2` / `PAUSE -`): all three timers freeze; timer start times are shifted by the pause duration on resume.

---

## 9. Chooser (who selects the question) *[code: GiveMoveToPlayerWithMinimumScore, WaitFirst, OnDecisionAnswerValidating, PostprocessQuestion]*

- At each table round start: the player with the **lowest score** chooses; if several tie, the **showman selects** among them (`ASK_SELECT_PLAYER chooser`, 30 s; timeout or bot showman → random among tied). Round 1: all zeros → showman/random.
- Classic: after a **correct button answer** the answerer becomes chooser; stake winner and secret recipient become chooser; appeal that flips wrong→right makes the appellant chooser (unless at round start). Wrong/no answer: chooser unchanged.
- TurnTaking: rotate sequentially after every question. Sequential/Quiz: no chooser.
- Showman may override with `SETCHOOSER`. Choice timeout → random active cell. In *oral* mode the showman may make the choice for the player.

---

## 10. Final round (theme-list round) *[code: PlayHandler.ShouldPlayRoundWithRemovableThemes, ThemeDeletersEnumerator, AskToDelete, OnThemeDeleted]*

1. Eligibility: `InGame = Sum > 0`. If nobody is positive and `AllowEveryoneToPlayHiddenStakes` → everyone plays; if still nobody → round skipped («Раунд пропущен, нет игроков»). Non-participants get `PLAYER_STATE Pass`.
2. All themes are shown together. Deletion order: participants grouped by score descending; the order is arranged so that **the player with the highest score deletes last** (the deletion that leaves the final theme); ties → showman picks the next deleter among the tied group (`ASK_SELECT_PLAYER deleter`); the sequence cycles if there are more themes than players. Deleter sends `DELETE themeIndex` (`ThemeSelection` 30 s; timeout → random). `OUT themeIndex` broadcast.
3. When one theme remains → `THEME_INFO`/`QUESTIONCAPTION`, its **first question** is played with the round default type `stakeAll` (§5.6): hidden stakes 1..Sum, `FINALTHINK`, written answers, sequential reveal with showman validation, ±stake.
4. `PlayAllQuestionsInFinalRound` («Играть все вопросы»): play every question of the final round instead of eliminating themes (guides: "alternative final with big-value questions").
5. Then `STOP`, `WINNER`, `STAGE After`, `GAME_STATISTICS`, `ASK_REVIEW`.

---

## 11. Appeals — апелляции *[code: OnAppellation, ProcessNextAppellationRequest, OnStartAppellation, OnIsRight, OnCheckAppellation, UpdatePlayersSumsAfterAppellation]*

Enabled by `UseAppellations` (default **true**, «Апелляции ответов — кнопки "Я прав" и "Я против"»). Requests are collected from the moment answering starts until the question is post-processed; processing pauses the game (`APPELLATION +` … `APPELLATION -`).

- **"Я прав!" (`APELLATE +`)** — a player whose answer on this question was judged wrong. Voters: all connected players + showman (`total = connected + 1`). Pre-filled: appellant = **for**, showman = **against**. Others receive `VALIDATION2 … +` and vote `ISRIGHT +/-` within `Appellation` (30 s). Voting ends early when either side exceeds `total/2`. **Accepted if for > against.** Effect: wrong outcome undone and +`CurPriceRight` granted; every later outcome in this question's history (players who answered after the appellant) is reverted; appellant becomes chooser (if not at round start); pending later appeals dropped. Multi-answerer questions flip ±stake for that player only.
- **"Я против!" (`APELLATE -`)** — any player contests the **right** answer of the last correct answerer (single-answerer questions only). Requires **more than 3 connected players**, else `USER_ERROR AppellationFailedTooFewPlayers` (with ≤3, the answerer + showman already hold a majority). Pre-filled: showman **for**, answerer **for**, caller **against**. **Accepted if against > for.** Effect: right outcome undone, −`CurPriceWrong` applied.
- One appeal per source per question; only one "against" appeal per question; appeals are processed one after another; game report stores appealed answers. Guides describe this as "majority vote; if most players side against the host, points are restored".

---

## 12. Host/showman controls & session management *[code: Game.cs]*

`PAUSE`, `MOVE` (next/back/round jumps; ads cannot be skipped unless Managed), `TOGGLE` (remove a cell from the table or restore it), `SETCHOOSER`, `CHANGE` (absolute sum edit, negatives allowed), `KICK`/`BAN`/`UNBAN` (banned list broadcast), `SETHOST`, `CONFIG ADDTABLE|DELETETABLE|FREE|SET|CHANGETYPE` (add up to 12 seats, remove, free a seat, seat a bot/human, swap player↔showman), `SETJOINMODE`, `SET_OPTIONS` (mid-game changes allowed for: `Oral`, `Managed`, `DisplayAnswerOptionsLabels`, `FalseStart`, `ReadingSpeed`, `PartialText`, `PartialImages`, `PartialImage`, `ButtonBlocking`, `UseAppellations`; broadcast as `OPTIONS2`), `MARK` (report bad question), `PIN` (room pin), `AVATAR` (image/video avatars), chat (`REPLIC` / `SHOWMAN_REPLIC` localized codes: RightAnswer, WrongAnswer, MakeStake, DeleteTheme, AppellationFor/Against, RoundSkippedNoPlayers, …). Disconnected players are skipped/auto-passed (short 2 s waits). Media: `ROUNDCONTENT` preload list, `MEDIA_PRELOAD_PROGRESS`, `MEDIALOADED`, `MEDIA_COMPLETED` (server waits for all humans to finish audio/video when duration unknown).

---

## 13. Complete rules/settings list *[code: RulesSettings.cs, AppSettingsCore.cs, ServerAppSettings.ts, localization.ts]*

| Setting | RU label / hint | Default |
|---|---|---|
| `GameMode` | Тип игры: Классическая / Упрощённая / Квиз / Игра по очереди | Classic |
| `FalseStart` | Фальстарты | true |
| `Oral` | Устная игра — если ведущий человек; игроки делают выбор и дают ответ голосом | false |
| `OralPlayersActions` | Разрешать игрокам делать выбор в устной игре | true |
| `IgnoreWrong` (lobby flag) | Снимать очки за неверный ответ (off = neutral game) | penalties on |
| `QuestionWithButtonPenalty` / `QuestionForYourselfPenalty` / `QuestionForAllPenalty` | Штраф за неверный ответ | Subtract / None / Subtract |
| `QuestionForYourselfFactor` | multiplier for "для себя" | 2 |
| `ButtonPressMode` | Режим нажатия кнопки | RandomWithinInterval |
| `ButtonsAcceptInterval` | Интервал нажатий кнопок | 300 ms |
| `ReadingSpeed` | Скорость чтения | 20 chars/s |
| `PartialText` / `PartialImages` | Частичный вывод текста/изображений (без фальстартов) | false / false |
| `HintShowman` | Сообщать ведущему правильные ответы заранее | false |
| `Managed` | Управляемая игра — продолжается только по кнопке «Дальше» | false |
| `PlayAllThemesInThemesRemovalRound` (`PlayAllQuestionsInFinalRound`) | Играть все вопросы (финал) | false |
| `AllowEveryoneToPlayHiddenStakes` | Играют все игроки (даже с 0 или отрицательным счётом) | true |
| `UseAppellations` | Апелляции ответов | true |
| `PreloadRoundContent` | Предзагружать медиаконтент всем в начале раунда | true |
| `DisplaySources` | Показывать источники вопросов | false |
| `DisplayAnswerOptionsLabels` / `DisplayAnswerOptionsOneByOne` | Показывать буквы вариантов / по очереди | true / true |
| `PrependThemeCommentsToQuestion` / `AppendRightAnswerTextToComplexAnswer` | — | true / true |
| `RandomRoundsCount` / `RandomThemesCount` / `RandomQuestionsBasePrice` | Случайный пакет (`@{random}`) | 3 / 6 / 100 |
| `TimeSettings` | see §8 | — |
| Room: `NetworkGameName`, `NetworkGamePassword`, `NetworkVoiceChat` link, `IsPrivate`, `AllowViewers`, `IsAutomatic`, `JoinMode`, `Culture` | — | — |

---

## 14. Protocol quick reference (for the API design) *[GAME_AGENT_DOCUMENTATION.md, Messages.cs]*

Server→client (selection): `CONNECTED name role`, `INFO2`, `GAMEMETADATA`, `OPTIONS2 k v…`, `STAGE BeforeGame|<round>|After`, `STAGE_INFO`, `ROUNDSNAMES`, `PACKAGE*`, `GAMETHEMES`, `SUMS`, `ROUND_THEMES_COMMENTS`, `THEME2`, `TABLO2`, `SHOWTABLO`, `SETCHOOSER i [+/-] [reason]`, `ASK_SELECT_PLAYER reason +/-…`, `CHOICE t q`, `QUESTION price`, `QTYPE type isDefault isNoRisk`, `QUESTIONCAPTION`, `QUESTION_PRICE_RANGE`, `CONTENT_SHAPE`, `CONTENT`/`CONTENT2`, `CONTENT_APPEND`, `CONTENT_STATE`, `LAYOUT`, `ANSWER_DEVIATION`, `TRY [NotFinished]`, `YOUTRY`, `ENDTRY all|i`, `ANSWER type`, `ORAL_ANSWER`, `ANSWER_VERSION`, `PLAYER_ANSWER`, `ASK_VALIDATE`, `VALIDATION2`, `PERSON +/- i delta`, `PASS i`, `PLAYER_STATE state i…` (Answering/Lost/Right/Wrong/HasAnswered/Pass), `PLAYER_SCORE_CHANGED`, `RIGHT_ANSWER_START`, `RIGHTANSWER type text`, `QUESTION_END`, `ASK_STAKE modes min max step reason`, `PERSONSTAKE i type [sum]` (0 pass, 1 nominal/sum, 2 pass-out, 3 all-in), `PERSONFINALSTAKE i`, `FINALTHINK t`, `OUT themeIndex`, `THEME_INFO`, `TIMER idx GO|PAUSE|RESUME|STOP|MAXTIME [t] [type]`, `PAUSE +/- t0 t1 t2`, `RESUME`, `STOP_PLAY`, `TOGGLE t q price`, `ROUND_END empty|timeout|manual`, `WINNER i|-1`, `GAME_STATISTICS`, `ASK_REVIEW`, `APPELLATION +/-`, `PLAYER_APPELLATING`, `CANCEL`, `USER_ERROR code`, `GAME_ERROR`, `YOU_ARE_KICKED`, `BANNED/UNBANNED/BANNEDLIST`, `SETJOINMODE`, `AVATAR`, `ROUNDCONTENT`, `MEDIALOADED`, `REPLIC`, `SHOWMAN_REPLIC seed code args`.

Client→server: `READY [+/-]`, `START`, `I [ms]`, `PASS`, `ANSWER text|label|+/-`, `ANSWER_VERSION`, `CHOICE t q`, `SELECT_PLAYER i`, `SET_STAKE mode [sum]`, `DELETE t`, `APELLATE [+/-]`, `ISRIGHT +/- [factor]`, `VALIDATE answer +/- [factor]`, `CHANGE i sum`, `SETCHOOSER i`, `PAUSE +/-`, `MOVE dir [round]`, `TOGGLE t q`, `KICK/BAN/UNBAN name`, `SETHOST name`, `SET_OPTIONS k v…`, `CONFIG …`, `SETJOINMODE m`, `MARK`, `AVATAR`, `MEDIALOADED`, `MEDIA_PRELOAD_PROGRESS %`, `MEDIA_COMPLETED`, `PIN`, `LEADERBOARD`.

---

## 15. Notes relevant to the ping-fairness requirement

SIGame already ships two server-side mitigations that your backend should reproduce and can extend: (1) **`RandomWithinInterval`** — collect all presses in a `ButtonsAccepting` window (300 ms default) and pick a random winner; (2) **`FirstWinsClient`** — clients timestamp their reaction relative to the locally observed `TRY`, and the server ranks by reported reaction time inside the window (needs clock/RTT compensation and anti-cheat). The false-start system (`TRY` only after content fully displayed, everyone gets equal reading time; misfire lockout 3 s) is the other half of the fairness design.


## Key facts
- Roles: player (max 12), showman (validates ISRIGHT +/- with optional factor; picks ties; CHANGE sums; PAUSE/MOVE/TOGGLE/SETCHOOSER), host (KICK/BAN/SETHOST/CONFIG tables/SET_OPTIONS/join mode), viewer (chat only), bots.
- SIQ v5: ZIP with content.xml + Images/Audio/Video/Html; package→rounds(type standart|final)→themes→questions(price, type, params, optional script, right/wrong answers); params: question, answer, answerType(text|select|number|point|client), answerOptions, answerDeviation, theme, price(numberSet [min;max]/step; maximum=0 = round min/max), selectionMode(any|exceptCurrent), answerDuration; content items text/image/audio/video/html with placement screen|replic|background, duration, waitForFinish.
- Question type ids: simple/withButton, stake (legacy auction), stakeAll, secret (legacy cat/bagcat), secretPublicPrice, secretNoQuestion, noRisk/forYourself (legacy sponsored), forAll; each expands to a script of steps SetAnswerType/SetAnswerer/SetTheme/AnnouncePrice/SetPrice/ShowContent/AskAnswer/Accept (ScriptsLibrary.cs).
- Stake (auction) rules from code: chooser bids first; participants = chooser + players with Sum > nominal; next bidder = lowest-score unbid participant (ties -> showman); min raise = stake + StakeStep (largest power of 10 <= min round price), aligned; opener cannot pass; pass allowed once a bid exists; all-in always allowed and can only be beaten by another all-in; players with Sum <= current stake auto-pass; timeout = pass (or nominal); winner answers directly, price = stake.
- Secret: chooser gives it to a player (selectionMode any = may keep, exceptCurrent = must give away); theme/price revealed after (secret) or before (secretPublicPrice) transfer; price fixed, round min/max, or range with step chosen by recipient; secretNoQuestion just awards the price. noRisk: chooser answers, price x2 (QuestionForYourselfFactor), wrong = 0 penalty by default. forAll: everyone answers in writing (45 s). stakeAll: hidden stakes 1..Sum, everyone with Sum>0 (or all if AllowEveryoneToPlayHiddenStakes=true), written answers, sequential reveal.
- Buzzer: with FalseStart=true (default) TRY is sent only after all content is shown and a 5 s ButtonPressing window opens; with false starts off buttons are armed before content (TRY NotFinished) and PartialText/PartialImages apply. A press outside the pressing state is a misfire (PLAYER_STATE Lost) that blocks the button for ButtonBlocking=3 s; no score penalty. Winner selection modes: RandomWithinInterval (default; random among presses within ButtonsAccepting=300 ms), FirstWins (server), FirstWinsClient (client-reported reaction time).
- After a wrong answer the player loses the right to press (CanPress=false), score -= price (unless penalty None / IgnoreWrong), media resumes and the remaining players can buzz again (thinking timer resumes); PASS lets a player give up the button; when nobody can press or the timer expires the right answer is shown (nobody answered). Correct answer: += price, answerer becomes chooser (Classic). Negative scores allowed; validation factor allows partial credit; factor 0 = counts as pass.
- Timers (seconds, defaults / UI max): QuestionSelection 30/120, ThemeSelection 30/120, PlayerSelection 30/120, ButtonPressing 5/120, ButtonsAccepting 300 ms/1000, Answering 25/120, SoloAnswering 25/120, HiddenAnswering 45/120, StakeMaking 30/120, ShowmanDecision 30/300, Round 3600/10800, ButtonBlocking 3/10, Reflection 2/10, Image 5/10, PartialImage 3/20, Appellation 30; ReadingSpeed 20 chars/s. Protocol timers 0=round,1=thinking,2=decision, values in 0.1 s. Timeouts: random question/theme/player, pass/nominal stake, hidden stake 1, empty answer = wrong, showman timeout = fuzzy auto-validation (0.81 similarity).
- Final round: participants Sum>0 (or everyone via AllowEveryoneToPlayHiddenStakes); themes deleted in an order where the highest-score player deletes last (ties -> showman picks); last theme's first question plays as stakeAll; PlayAllQuestionsInFinalRound plays all final questions instead. Winner = max sum; tie -> WINNER -1.
- Appeals (UseAppellations default true): 'Я прав' by a player judged wrong — voters = connected players + showman, appellant auto-for, showman auto-against, accepted if for > against, reverts the wrong outcome and all later outcomes of that question and makes the appellant chooser; 'Я против' contests the last right answer — needs >3 connected players, showman+answerer auto-for, caller against, accepted if against > for. One appeal per player per question; voting window 30 s; early stop on majority.
- Game modes: Classic/Tv (table, player selects, default simple; final = stakeAll with theme removal; chooser = last correct button answerer, round start = lowest score, ties -> showman), Simple/Sport (sequential, simple), Quiz (sequential, forAll), TurnTaking (player selects, noRisk, chooser rotates). Settings: FalseStart, Oral (+OralPlayersActions), IgnoreWrong, per-class penalties, ForYourselfFactor, ButtonPressMode, ButtonsAcceptInterval, ReadingSpeed, PartialText/Images, HintShowman, Managed, PlayAllQuestionsInFinalRound, AllowEveryoneToPlayHiddenStakes, UseAppellations, PreloadRoundContent, DisplaySources, DisplayAnswerOptionsLabels/OneByOne, random package (3 rounds x 6 themes, base price 100). Mid-game SET_OPTIONS allowed for Oral, Managed, FalseStart, ReadingSpeed, PartialText/Images, PartialImage time, ButtonBlocking, UseAppellations, answer-option labels.

## Sources
- https://github.com/VladimirKhil/SI (SIGame source; cloned to /private/tmp/claude-501/-Users-ruslan-Desktop-Projects-game/e9469d22-7549-43b6-b848-6869308c73e9/scratchpad/SI)
- /private/tmp/claude-501/-Users-ruslan-Desktop-Projects-game/e9469d22-7549-43b6-b848-6869308c73e9/scratchpad/SI/src/Common/SIPackages/ScriptsLibrary.cs (well-known question type scripts)
- /private/tmp/claude-501/-Users-ruslan-Desktop-Projects-game/e9469d22-7549-43b6-b848-6869308c73e9/scratchpad/SI/src/Common/SIPackages/Core/QuestionTypes.cs, StepParameterValues.cs, StepParameterNames.cs, QuestionParameterNames.cs, StepParameterTypes.cs, RoundTypes.cs, QuestionTypeParams.cs
- /private/tmp/claude-501/-Users-ruslan-Desktop-Projects-game/e9469d22-7549-43b6-b848-6869308c73e9/scratchpad/SI/assets/siq_5.xsd (SIQ v5 schema)
- /private/tmp/claude-501/-Users-ruslan-Desktop-Projects-game/e9469d22-7549-43b6-b848-6869308c73e9/scratchpad/SI/src/Common/SIEngine/DOCUMENTATION.md and Rules/WellKnownGameRules.cs, GameEngine.cs, EngineOptions.cs, Models/RoundEndReason.cs
- /private/tmp/claude-501/-Users-ruslan-Desktop-Projects-game/e9469d22-7549-43b6-b848-6869308c73e9/scratchpad/SI/src/Common/SIEngine.Core/DOCUMENTATION.md, FalseStartMode.cs, QuestionEngineOptions.cs
- /private/tmp/claude-501/-Users-ruslan-Desktop-Projects-game/e9469d22-7549-43b6-b848-6869308c73e9/scratchpad/SI/src/SICore/SICore/GAME_AGENT_DOCUMENTATION.md (messaging protocol)
- /private/tmp/claude-501/-Users-ruslan-Desktop-Projects-game/e9469d22-7549-43b6-b848-6869308c73e9/scratchpad/SI/src/SICore/SICore/Clients/Game/GameController.cs (stakes, buzzer, validation, appeals, final round, timers)
- /private/tmp/claude-501/-Users-ruslan-Desktop-Projects-game/e9469d22-7549-43b6-b848-6869308c73e9/scratchpad/SI/src/SICore/SICore/Clients/Game/Game.cs (client message handlers: I, PASS, ANSWER, ISRIGHT, SET_STAKE, APELLATE, CHANGE, MOVE, KICK/BAN, CONFIG, SET_OPTIONS)
- /private/tmp/claude-501/-Users-ruslan-Desktop-Projects-game/e9469d22-7549-43b6-b848-6869308c73e9/scratchpad/SI/src/SICore/SICore/Clients/Game/PlayHandler.cs, QuestionPlayHandler.cs, ThemeDeletersEnumerator.cs, QuestionPlayState.cs, GameActions.cs
- /private/tmp/claude-501/-Users-ruslan-Desktop-Projects-game/e9469d22-7549-43b6-b848-6869308c73e9/scratchpad/SI/src/SICore/SI.Contracts/RulesSettings.cs and TimeSettings.cs; SIData/AppSettingsCore.cs, TimeSettings.cs, PenaltyType.cs, ButtonPressMode.cs, GameModes.cs; SICore/Enums.cs, Constants.cs, Models/*.cs, Messages.cs
- /private/tmp/claude-501/-Users-ruslan-Desktop-Projects-game/e9469d22-7549-43b6-b848-6869308c73e9/scratchpad/SI/src/SIQuester/SIQuester.ViewModel/Model/QuestionTypesNamesNew.cs and SIQuester Resources.ru-RU.resx (RU type names, secret settings)
- https://github.com/VladimirKhil/SIOnline (web client; cloned to /private/tmp/claude-501/-Users-ruslan-Desktop-Projects-game/e9469d22-7549-43b6-b848-6869308c73e9/scratchpad/SIOnline) — src/model/resources/localization.ts (RU rules text and settings labels), src/client/contracts/GameRules.ts, ServerAppSettings.ts, ServerTimeSettings.ts, src/model/ButtonPressMode.ts, GameType.ts, Role.ts, src/components/settings/TimeSettingsView/TimeSettingsView.tsx (timer maxima)
- https://vladimirkhil.com/content/docs/QuestionsTypes.xml (legacy question type registry: simple, auction, cat, bagcat, sponsored)
- https://habr.com/ru/companies/timeweb/articles/920442/ (SIGame 2025 guide: roles, question types, false starts, appeals, host controls)
- https://steamcommunity.com/sharedfiles/filedetails/?id=3456423604 (Steam guide: settings and recommended timers)
- https://steamcommunity.com/sharedfiles/filedetails/?id=3451696017 (Steam ENG guide: pack creation, media limits, hosting settings)
- https://steamcommunity.com/app/3553500/discussions/0/594038047781335676/ (developer answers on question types)
- https://www.ixbt.com/live/sw/svoya-igra-s-druzyami-kak-sdelat-kachestvennyy-pak-gayd-po-siquester-chast-no1.html (SIQuester guide: secret question variants, price modes, final round theme count)
- https://stopgame.ru/blogs/topic/87929/svoya_igra_chto_takoe_i_kak_ee_est (SIGame overview: roles, types, appeals)
- https://www.forpes.ru/post/197878 (mirror of the Habr guide)
- https://ru.wikipedia.org/wiki/Своя_игра (TV show rules: rounds, prices, Кот в мешке, аукцион, final)
- https://github.com/VladimirKhil/SI/blob/master/README.md and ARCHITECTURE.md
