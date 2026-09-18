# Engine protocol — commands and events of `internal/engine`

The engine is a pure state machine: the room actor feeds it `Command`s and receives `Event`s.
The WebSocket layer maps them 1:1 — every `EvType` is the `t` of a server→client envelope and its
payload struct (camelCase JSON) is `p`; client messages become commands of the same name. This
document is the contract; the Go doc comments in `internal/engine/*.go` are the source of truth.

Conventions: every duration is `int64` milliseconds; `AtMs` is the room's monotonic clock; IDs are
person IDs (session/person UUIDs), theme/question indexes are 0-based positions in the round table.

## Lifecycle

```
lobby ─START─▶ roundIntro ─▶ selecting ─CHOOSE_QUESTION/timeout─▶ question ─▶ (QUESTION_END) ─▶ selecting …
                    │                                                                  │ no cells / round timer
                    └─ final round ─▶ finalThemes ─DELETE_THEME…─▶ question (stakeAll) ─▶ roundEnd ─▶ next round | gameEnd
```

Question sub-states (`STAGE.sub`): `announcing`, `secretTransfer`, `priceSelect`, `stakes`, `content`,
`buttonWait`, `answering`, `validating`, `hiddenAnswering`, `reveal`, `end`.

### Room responsibilities

| Engine output | Room must |
|---|---|
| `TIMER_START{id,kind,durationMs}` | schedule `CmdTimeout{TimerID:id}` after `durationMs` (forward to clients for progress bars). `kind=mediaFallback` with `durationMs` = the group's known minimum (0 if none): pick a duration from media metadata (ffprobe) or a configured maximum. |
| `TIMER_STOP{id}` | cancel the timer. |
| `TIMER_PAUSE{id,remainingMs}` / `TIMER_RESUME{id,remainingMs}` | cancel / reschedule with `remainingMs`. |
| `BUTTON_ARM_REQUEST{questionId,eligiblePlayerIds,thinkingRemainingMs,rearm}` | arm the buzzer (`thinkingRemainingMs=-1` = content still playing, no thinking timer yet); answer with `CmdButtonResult{WinnerID}` or `{Nobody:true}`. |
| `BUTTON_DISARM_REQUEST{questionId}` | close the buzzer; presses become misfires. |
| `VALIDATION_TIMEOUT{personId,answer,rights,wrongs}` | run the fuzzy/AI judge and send `CmdValidate{Actor.Role:system, PersonID, Right, Factor, Source:"auto"\|"ai"}`. |
| `MEDIA_WAIT{mediaId\|url}` | collect `MEDIA_COMPLETED` from human players and forward them as `CmdMediaCompleted{PersonID}` (or one aggregated command with an empty `PersonID`). |

Every command carries `Actor{personId, role, isHost}` and `AtMs`. Rejected commands return an error
wrapping `ErrNotAllowed` (permission), `ErrBadState` (wrong stage) or `ErrBadArgument` (bad field) and
produce no events and no state change; the room maps them to `ERROR`/`USER_ERROR` messages. Stale
`TIMEOUT`, `BUTTON_RESULT` and `MEDIA_COMPLETED` are ignored silently (no error, no events).
`IsInternal()` is true for the three room-only events (`BUTTON_*_REQUEST`, `VALIDATION_TIMEOUT`);
timer events are addressed to everyone but must also be acted on by the room (`IsTimer()`).

## Commands

| Type | Actor | Fields | Allowed when | Effect |
|---|---|---|---|---|
| `START` | host or showman | — | lobby | `OPTIONS`, `PLAYERS`, first round starts |
| `PLAYER_READY` | player | — | any | toggles the ready flag → `PLAYERS` |
| `CHOOSE_QUESTION` | chooser (showman in oral mode) | `theme`, `q` | selecting, no pending tie | question starts |
| `BUTTON_RESULT` | system | `winnerId` \| `nobody` | button armed | winner answers / nobody answered |
| `PASS` | player | — | button question while armed; stake bidding (not the highest bidder, not the opener) | gives up the button / passes in bidding |
| `ANSWER` | answerer (showman in oral mode with `personId`) | `text` \| `optionLabel` \| `number` \| `point` \| `right`+`rightSet` (client type) | answering / hiddenAnswering | answer published (hidden answers go to the showman only) |
| `ANSWER_DRAFT` | answerer | `text` | answering / hiddenAnswering | `ANSWER_DRAFT` to the showman |
| `VALIDATE` | showman or system | `personId` (optional for the pending one), `right`, `factor` (0 → 1; `factorZero` for an explicit 0 = pass), `source` (`showman`\|`ai`\|`auto`\|`appeal`) | validating; oral answering; hidden phases (pre-verdict, applied at the reveal); custom questions (manual scoring, `personId` required) | `VALIDATION`, `PERSON_SCORE`, flow continues |
| `SELECT_PLAYER` | showman (ties) / chooser (secret transfer, showman in oral mode) | `personId` | `ASK_SELECT_PLAYER` pending | decision applied |
| `SET_STAKE` | asked player (showman in oral mode) | `stakeMode` (`nominal`\|`stake`\|`allIn`\|`pass`), `amount` | bidding turn; secret price choice; hidden stakes | `PERSON_STAKE` |
| `DELETE_THEME` | asked deleter (showman in oral mode) | `theme` | finalThemes | `THEME_DELETED` |
| `APPELLATE` | player | `for` (true = "I am right", false = "I disagree") | from the first verdict of a question until the next question starts; `UseAppellations` | queued; starts when the question is over |
| `VOTE_APPEAL` | pending voter | `right` | appeal running | `APPEAL_VOTE`, maybe `APPEAL_RESULT` |
| `TIMEOUT` | system | `timerId` | any | timer-specific behaviour (see Timers) |
| `MEDIA_COMPLETED` | system / player | `personId` ("" = everyone) | content wait | next content group |
| `MEDIA_LOADED` | player | — | any | ignored (informational) |
| `PAUSE` | showman or host | `on` | started game | `PAUSE`, `TIMER_PAUSE`/`TIMER_RESUME` for every timer |
| `MOVE` | showman or host | `dir` (−2 round back, −1 return question to the table, 1 next step, 2 next round, 3 round `round`) | started game | see §12 of the rules |
| `TOGGLE` | showman or host | `theme`, `q` | table shown | removes / restores a cell → `TOGGLE`, `TABLE` |
| `CHANGE_SCORE` | showman or host | `personId`, `newSum` | any | `PERSON_SCORE{reason:correction}`, `SUMS` |
| `SET_CHOOSER` | showman or host | `personId` | started game | `SET_CHOOSER{reason:showman}`, `ASK_CHOOSE` if selecting |
| `PLAYER_LEFT` | system | `personId` | any | `PLAYERS`; hidden phases waiting only for this player complete |
| `PLAYER_JOINED` | system | `personId`, `name` | any | reconnect keeps the score; new IDs get a seat (≤ 12) |
| `KICK_PLAYER` | host | `personId` | any | player marked kicked (cannot rejoin) |
| `SET_OPTIONS` | host or showman | `options` (`oral`, `managed`, `displayAnswerOptionsLabels`, `falseStart`, `readingSpeed`, `partialText`, `useAppellations`, `buttonBlockingMs`, `partialImageMs`) | any | `OPTIONS` |
| `NEXT` | showman or host | — | any waiting point (round intro/end, content group, reveal, custom question) | advances; the only way forward in Managed mode |
| `AI_SUGGESTION` | system | `personId`, `right`, `factor`, `reason` | any | `AI_SUGGESTION` to the showman (hybrid judging) |

## Events

Audience: `all`, `role:showman`, `person:<id>`, `except:<id>`, `room` (internal).

| Type | Audience | Payload | When |
|---|---|---|---|
| `STAGE` | all | `stage`, `sub`, `roundIndex` | every stage / sub-state change |
| `PLAYERS` | all | `players[]{id,name,score,connected,ready,kicked,state,canPress,inGame}` | seat changes, ready toggles, final-round admission |
| `OPTIONS` | all | `rules`, `times` | start, `SET_OPTIONS` |
| `ROUND_START` | all | `index`, `name`, `type`, `themes[]` | round begins |
| `ROUND_CONTENT` | all | `mediaIds[]`, `urls[]` | round begins (preload list; answer media excluded) |
| `TABLE` | all | `themes[]{name,removed,questions[]{price,played,removed}}` | round begins, question starts, toggles, theme deletions |
| `SUMS` | all | `scores[]{personId,score}` | round begins, question ends, corrections, appeals |
| `SET_CHOOSER` | all | `personId`, `reason` (`roundStart`,`rightAnswer`,`stakeWinner`,`secretRecipient`,`rotation`,`appeal`,`showman`) | chooser changes |
| `ASK_CHOOSE` | chooser (+showman in oral mode) | `personId`, `durationMs` | selection step |
| `ASK_SELECT_PLAYER` | decider | `reason` (`chooser`,`staker`,`deleter`,`secretTransfer`), `candidates[]`, `deciderId`, `durationMs` | ties / secret transfer |
| `QUESTION_START` | all | `questionId`, `themeIndex`, `questionIndex`, `theme`, `price`, `type`, `isDefault`, `answerType` | question selected |
| `QUESTION_CAPTION` | all | `theme`, `price` | secret theme/price announce, stake winner, noRisk price, final theme |
| `SHOWMAN_HINT` | showman | `rights[]`, `wrongs[]` (only with `hintShowman`), `showmanComments`, `comments` | question start |
| `CONTENT` | all | `phase`, `index`, `items[]{type,text,mediaId,url,placement,durationMs}`, `waitMs` | each content group |
| `CONTENT_STATE` | all | `state` (`paused`\|`resumed`) | button won during content / re-arm |
| `ANSWER_OPTIONS` | all | `options[]{label,content[]}`, `showLabels`, `oneByOne` | select questions after content |
| `MEDIA_WAIT` | all | `mediaId`, `url` | media of unknown duration |
| `ASK_ANSWER` | answerer(s) | `personId`, `answerType`, `durationMs`, `hidden`, `oral` | answering starts |
| `ANSWER_DRAFT` | showman | `personId`, `text` | live typing |
| `PLAYER_ANSWER` | all (showman only for unrevealed hidden answers) | `personId`, `answer{text,optionLabel,number,point,clientRight}` | answer submitted / revealed |
| `ASK_VALIDATE` | showman | `personId`, `answer`, `rights[]`, `wrongs[]`, `allowFactor`, `oral`, `autoAfterMs`, `preview` | text answer needs a verdict (`preview`: hidden answer, verdict cached until the reveal) |
| `VALIDATION` | all | `personId`, `answer`, `right`, `factor`, `source`, `excludedOption` | verdict applied |
| `AI_SUGGESTION` | showman | `personId`, `right`, `factor`, `reason` | room forwarded an AI verdict |
| `PERSON_SCORE` | all | `personId`, `delta`, `score`, `reason` (`answer`,`penalty`,`secret`,`appeal`,`correction`) | score change |
| `PLAYER_STATE` | all | `personId`, `state` (`none`,`answering`,`lost`,`right`,`wrong`,`hasAnswered`,`pass`) | player state change |
| `PASS` | all | `personId` | voluntary button pass |
| `RIGHT_ANSWER` | all | `text`, `items[]`, `comments` | reveal |
| `QUESTION_END` | all | `themeIndex`, `questionIndex` | question over (also when returned to the table) |
| `ASK_STAKE` | asked player (+showman in oral mode) | `personId`, `modes[]`, `min`, `max`, `step`, `reason` (`stake`,`secretPrice`,`hiddenStake`), `durationMs` | bidding turn / price choice / hidden stake |
| `PERSON_STAKE` | all (`except:showman` + showman copy with the amount for hidden stakes) | `personId`, `mode`, `amount`, `hidden` | stake decision, hidden stake reveal |
| `ASK_DELETE_THEME` | deleter (+showman in oral mode) | `personId`, `themes[]`, `durationMs` | final round deletion turn |
| `THEME_DELETED` | all | `themeIndex`, `personId` | theme removed |
| `FINAL_THINK` | all | `durationMs` | written answering starts |
| `TIMER_START` | all + room | `id`, `kind`, `durationMs`, `personId` | timer requested |
| `TIMER_STOP` | all + room | `id` | timer cancelled |
| `TIMER_PAUSE` / `TIMER_RESUME` | all + room | `id`, `kind`, `remainingMs` | pause / resume / button win / re-arm / appeal |
| `PAUSE` | all | `on` | showman pause |
| `TOGGLE` | all | `themeIndex`, `questionIndex`, `active` | cell removed / restored |
| `ROUND_END` | all | `index`, `reason` (`empty`,`timeout`,`manual`,`noPlayers`) | round over |
| `WINNER` | all | `personId` ("" = tie) | game over |
| `GAME_END` | all | `scores[]`, `winnerId`, `statistics{questionsPlayed,players[]{personId,rightAnswers,wrongAnswers,acceptedAnswers,rejectedAnswers,appeals}}` | game over |
| `APPEAL_START` | all | `kind` (`for`\|`against`), `appellantId`, `answererId`, `answer`, `voters[]`, `durationMs` | appeal opened (timers paused) |
| `ASK_APPEAL_VOTE` | each pending voter | `kind`, `answererId`, `answer`, `rights[]` | appeal opened |
| `APPEAL_VOTE` | all | `personId`, `right` | vote cast |
| `APPEAL_RESULT` | all | `kind`, `answererId`, `accepted`, `for`, `against` | appeal closed (timers resumed) |
| `USER_ERROR` | person | `code`, `message` | reserved for the room's error mapping |
| `BUTTON_ARM_REQUEST` | room | `questionId`, `eligiblePlayerIds[]`, `thinkingRemainingMs`, `rearm` | open the buzzer |
| `BUTTON_DISARM_REQUEST` | room | `questionId` | close the buzzer |
| `VALIDATION_TIMEOUT` | room | `personId`, `answer`, `rights[]`, `wrongs[]` | showman did not decide in time |

## Timers

| kind | default | on `TIMEOUT` |
|---|---|---|
| `questionSelection` | 30 s | random available cell |
| `themeSelection` | 30 s | random remaining theme |
| `playerSelection` | 30 s | random candidate other than the chooser |
| `showmanDecision` | 30 s | tie: random candidate; validation: `VALIDATION_TIMEOUT` to the room |
| `buttonPressing` | 5 s | nobody answered → reveal |
| `answering` / `soloAnswering` | 25 s (`answerDurationMs` overrides) | answer "-" → wrong (oral mode: ignored, the showman decides) |
| `hiddenAnswering` | 45 s | missing answers become "-" → wrong (disconnected players are skipped) |
| `stakeMaking` | 30 s | bidding: pass (nominal for the opener); secret price: minimum; hidden stakes: 1 |
| `content` | text: chars / `readingSpeed` + `reflectionMs`; image/html: `imageMs`; explicit `durationMs` | next content group |
| `mediaFallback` | room-defined (payload carries the known minimum) | next content group |
| `reflection` | `reflectionMs` (or the answer media duration) | `QUESTION_END` |
| `round` | 3600 s | round ends after the current question (immediately while selecting) |
| `appellation` | 30 s | tally the votes |

Pause freezes every timer (`TIMER_PAUSE` with the remaining time); resume re-issues them with
`TIMER_RESUME`. Timers frozen by game logic (the thinking timer while the button winner answers, the
content timer while a press interrupts content, all timers during an appeal) are not resumed by a
showman resume — only by the game step that unfreezes them (re-arm, `APPEAL_RESULT`). A timer started
while paused is emitted as `TIMER_START` immediately followed by `TIMER_PAUSE`. Player commands are
rejected with `ErrBadState` while paused; showman/host controls work.

## Snapshot

`Game.Snapshot(role, personId)` returns the role-projected `Snapshot` (stage, table, players, scores,
rules, timers with `startedAtMs`/`durationMs`/`paused`/`remainingMs`, the current question view and
the prompts outstanding for that person: `askChoose`, `askAnswer`, `askStake`, `askValidate`,
`askDeleteTheme`, `askAppealVote`). Right answers, wrong-answer lists, other players' hidden stakes
and unrevealed written answers are present only for the showman (or the owner).

## Known deviations from the rules document

- `PartialText` / partial images: the engine sends whole content items; clients animate progressive
  reveal with `readingSpeed` / `partialImageMs`. The rules flags are stored and broadcast only.
- `forAll` validation is sequential after the answering phase (answers reach the showman as they
  arrive and may be pre-validated; the verdict is applied at the reveal).
- The final round plays only the first available question of the remaining theme (its other cells
  are removed); `PlayAllQuestionsInFinal` plays every cell in table order as `stakeAll`.
- Disconnected players keep their timers running (no 2 s shortcut); the normal timeout applies.
  Hidden phases that wait only for a disconnected player complete immediately.
- `point` answers: `right[0]` is `x,y[,radius]` in normalised image coordinates; tolerance is
  `max(answerDeviation, radius, 0.02)`.
- "I disagree" appeals that succeed do not move the chooser back.
- Round-start chooser ties are resolved by the showman (`ASK_SELECT_PLAYER chooser`), also in round 1.
