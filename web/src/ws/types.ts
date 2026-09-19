/**
 * Hand-written message types of the SIGame WebSocket protocol.
 *
 * Sources: docs/protocol.md (room messages, envelope), docs/protocol-engine.md
 * (engine events/commands), internal/room/messages.go, internal/engine/event.go,
 * internal/engine/snapshot.go, internal/buzzer/arm.go + conn.go.
 *
 * Only what the client uses today is typed precisely; everything else keeps an
 * `unknown` payload so the dispatcher still routes it.
 */

// ---- envelope --------------------------------------------------------------

export interface Envelope<T extends string = string, P = unknown> {
  t: T;
  seq?: number;
  p?: P;
}

// ---- shared enums ----------------------------------------------------------

export type Role = 'showman' | 'player' | 'viewer';
export type ShowmanMode = 'human' | 'ai' | 'hybrid';
export type Quality = 'unsynced' | 'good' | 'fair' | 'poor';
export type TrustLevel = 'full' | 'conservative' | 'serverOnly' | 'arrivalOnly';

export type Stage =
  | 'lobby'
  | 'roundIntro'
  | 'selecting'
  | 'question'
  | 'roundEnd'
  | 'finalThemes'
  | 'gameEnd';

export type QSub =
  | 'announcing'
  | 'secretTransfer'
  | 'priceSelect'
  | 'stakes'
  | 'content'
  | 'buttonWait'
  | 'answering'
  | 'validating'
  | 'hiddenAnswering'
  | 'reveal'
  | 'end';

export type PlayerState =
  | 'none'
  | 'answering'
  | 'lost'
  | 'right'
  | 'wrong'
  | 'hasAnswered'
  | 'pass';

export type StakeMode = 'nominal' | 'stake' | 'allIn' | 'pass';
export type RoundType = 'standard' | 'final';
export type QuestionType =
  | 'simple'
  | 'stake'
  | 'stakeAll'
  | 'secret'
  | 'secretPublicPrice'
  | 'secretNoQuestion'
  | 'noRisk'
  | 'forAll'
  | 'custom';
export type AnswerType = 'text' | 'select' | 'number' | 'point' | 'client';
export type ContentType = 'text' | 'image' | 'audio' | 'video' | 'html';
export type Placement = 'screen' | 'replic' | 'background';

// ---- engine payloads (docs/protocol-engine.md) ------------------------------

export interface Rules {
  mode: 'classic' | 'simple' | 'quiz' | 'turnTaking';
  falseStart: boolean;
  oral: boolean;
  ignoreWrong: boolean;
  buttonPenalty: 'subtract' | 'none';
  forYourselfPenalty: 'subtract' | 'none';
  forAllPenalty: 'subtract' | 'none';
  forYourselfFactor: number;
  readingSpeed: number;
  partialText: boolean;
  hintShowman: boolean;
  managed: boolean;
  playAllQuestionsInFinal: boolean;
  allowEveryoneToPlayHiddenStakes: boolean;
  useAppellations: boolean;
  displayAnswerOptionsLabels: boolean;
  displayAnswerOptionsOneByOne: boolean;
  prependThemeCommentsToQuestion: boolean;
}

export interface TimeSettings {
  questionSelectionMs: number;
  themeSelectionMs: number;
  playerSelectionMs: number;
  buttonPressingMs: number;
  answeringMs: number;
  soloAnsweringMs: number;
  hiddenAnsweringMs: number;
  stakeMakingMs: number;
  showmanDecisionMs: number;
  roundMs: number;
  buttonBlockingMs: number;
  reflectionMs: number;
  imageMs: number;
  partialImageMs: number;
  appellationMs: number;
}

export interface StagePayload {
  stage: Stage;
  sub?: QSub;
  roundIndex: number;
}

export interface PlayerInfo {
  id: string;
  name: string;
  score: number;
  connected: boolean;
  ready: boolean;
  kicked?: boolean;
  state: PlayerState;
  canPress: boolean;
  inGame: boolean;
}

export interface PlayersPayload {
  players: PlayerInfo[];
}

export interface OptionsPayload {
  rules: Rules;
  times: TimeSettings;
}

export interface RoundStartPayload {
  index: number;
  name: string;
  type: RoundType;
  themes: string[];
}

export interface RoundContentPayload {
  mediaIds: string[];
  urls?: string[];
}

export interface TableCell {
  price: number;
  played: boolean;
  removed: boolean;
}

export interface TableTheme {
  name: string;
  removed: boolean;
  questions: TableCell[];
}

export interface TablePayload {
  themes: TableTheme[];
}

export interface ScoreEntry {
  personId: string;
  score: number;
}

export interface SumsPayload {
  scores: ScoreEntry[];
}

export interface SetChooserPayload {
  personId: string;
  reason:
    | 'roundStart'
    | 'rightAnswer'
    | 'stakeWinner'
    | 'secretRecipient'
    | 'rotation'
    | 'appeal'
    | 'showman';
}

export interface AskChoosePayload {
  personId: string;
  durationMs: number;
}

export interface AskSelectPlayerPayload {
  reason: 'chooser' | 'staker' | 'deleter' | 'secretTransfer';
  candidates: string[];
  deciderId: string;
  durationMs: number;
}

export interface QuestionStartPayload {
  questionId: string;
  themeIndex: number;
  questionIndex: number;
  theme: string;
  price: number;
  type: QuestionType;
  isDefault: boolean;
  answerType: AnswerType;
}

export interface QuestionCaptionPayload {
  theme: string;
  price: number;
}

export interface ShowmanHintPayload {
  rights?: string[];
  wrongs?: string[];
  showmanComments?: string;
  comments?: string;
}

export interface ContentItemView {
  type: ContentType;
  text?: string;
  mediaId?: string;
  url?: string;
  placement: Placement;
  durationMs?: number;
}

export interface ContentPayload {
  phase: 'question' | 'answer';
  index: number;
  items: ContentItemView[];
  waitMs: number;
}

export interface ContentStatePayload {
  state: 'paused' | 'resumed';
}

export interface AnswerOptionView {
  label: string;
  content: ContentItemView[];
}

export interface AnswerOptionsPayload {
  options: AnswerOptionView[];
  showLabels: boolean;
  oneByOne: boolean;
}

export interface MediaWaitPayload {
  mediaId?: string;
  url?: string;
}

export interface AskAnswerPayload {
  personId: string;
  answerType: AnswerType;
  durationMs: number;
  hidden: boolean;
  oral: boolean;
}

export interface AnswerDraftPayload {
  personId: string;
  text: string;
}

export interface Point {
  x: number;
  y: number;
}

export interface AnswerView {
  text?: string;
  optionLabel?: string;
  number?: number;
  point?: Point;
  clientRight?: boolean;
}

export interface PlayerAnswerPayload {
  personId: string;
  answer: AnswerView;
}

export interface AskValidatePayload {
  personId: string;
  answer: string;
  rights: string[];
  wrongs?: string[];
  allowFactor: boolean;
  oral: boolean;
  autoAfterMs: number;
  preview: boolean;
}

export interface ValidationPayload {
  personId: string;
  answer: string;
  right: boolean;
  factor: number;
  source: string;
  excludedOption?: string;
}

export interface AISuggestionPayload {
  personId: string;
  right: boolean;
  factor: number;
  reason?: string;
}

export interface PersonScorePayload {
  personId: string;
  delta: number;
  score: number;
  reason: 'answer' | 'penalty' | 'secret' | 'appeal' | 'correction' | 'bonus';
}

export interface PlayerStatePayload {
  personId: string;
  state: PlayerState;
}

export interface PassPayload {
  personId: string;
}

export interface RightAnswerPayload {
  text: string;
  items?: ContentItemView[];
  comments?: string;
}

export interface QuestionEndPayload {
  themeIndex: number;
  questionIndex: number;
}

export interface AskStakePayload {
  personId: string;
  modes: StakeMode[];
  min: number;
  max: number;
  step: number;
  reason: 'stake' | 'secretPrice' | 'hiddenStake';
  durationMs: number;
}

export interface PersonStakePayload {
  personId: string;
  mode: StakeMode;
  amount: number;
  hidden: boolean;
}

export interface AskDeleteThemePayload {
  personId: string;
  themes: number[];
  durationMs: number;
}

export interface ThemeDeletedPayload {
  themeIndex: number;
  personId: string;
}

export interface FinalThinkPayload {
  durationMs: number;
}

export interface TimerStartPayload {
  id: string;
  kind: string;
  durationMs: number;
  personId?: string;
}

export interface TimerStopPayload {
  id: string;
}

export interface TimerPausePayload {
  id: string;
  kind: string;
  remainingMs: number;
}

export interface PausePayload {
  on: boolean;
}

export interface TogglePayload {
  themeIndex: number;
  questionIndex: number;
  active: boolean;
}

export interface RoundEndPayload {
  index: number;
  reason: 'empty' | 'timeout' | 'manual' | 'noPlayers';
}

export interface WinnerPayload {
  personId: string;
}

export interface PlayerStats {
  personId: string;
  rightAnswers: number;
  wrongAnswers: number;
  acceptedAnswers?: string[];
  rejectedAnswers?: string[];
  appeals: number;
}

export interface GameEndPayload {
  scores: ScoreEntry[];
  winnerId: string;
  statistics: { questionsPlayed: number; players: PlayerStats[] };
}

export interface AppealStartPayload {
  kind: 'for' | 'against';
  appellantId: string;
  answererId: string;
  answer: string;
  voters: string[];
  durationMs: number;
}

export interface AskAppealVotePayload {
  kind: 'for' | 'against';
  answererId: string;
  answer: string;
  rights: string[];
}

export interface AppealVotePayload {
  personId: string;
  right: boolean;
}

export interface AppealResultPayload {
  kind: 'for' | 'against';
  answererId: string;
  accepted: boolean;
  for: number;
  against: number;
}

export interface UserErrorPayload {
  code: string;
  message: string;
}

// ---- snapshot (internal/engine/snapshot.go) ---------------------------------

export interface TimerView {
  id: string;
  kind: string;
  durationMs: number;
  startedAtMs: number;
  paused: boolean;
  remainingMs: number;
  personId?: string;
}

export interface PendingView {
  kind: 'chooser' | 'staker' | 'deleter' | 'secretTransfer';
  deciderId: string;
  candidates: string[];
}

export interface AppealView {
  kind: 'for' | 'against';
  appellantId: string;
  answererId: string;
  answer: string;
  for: number;
  against: number;
  pending: string[];
}

export interface OutcomeView {
  personId: string;
  answer?: string;
  right: boolean;
  factor: number;
  delta: number;
  reverted?: boolean;
}

export interface StakesView {
  nominal: number;
  step: number;
  stake: number;
  leader?: string;
  current?: string;
  bids: Record<string, number>;
  passed: string[];
  allIn: string[];
}

export interface QuestionView {
  questionId: string;
  themeIndex: number;
  questionIndex: number;
  theme: string;
  price: number;
  curPrice: number;
  type: QuestionType;
  isDefault: boolean;
  answerType: AnswerType;
  sub: QSub;
  answererId?: string;
  answerers?: string[];
  content: ContentPayload[];
  options?: AnswerOptionsPayload;
  excludedOptions?: string[];
  rightAnswer?: RightAnswerPayload;
  stakes?: StakesView;
  hiddenStakes?: Record<string, number>;
  hiddenAnswers?: Record<string, AnswerView>;
  rights?: string[];
  wrongs?: string[];
  history: OutcomeView[];
  armed: boolean;
  thinkingRemainingMs: number;
  eligiblePlayerIds?: string[];
}

export interface GameSnapshot {
  role: Role;
  personId: string;
  stage: Stage;
  sub?: QSub;
  roundIndex: number;
  roundName?: string;
  roundType?: RoundType;
  table: TablePayload;
  chooserId?: string;
  players: PlayerInfo[];
  scores: ScoreEntry[];
  rules: Rules;
  times: TimeSettings;
  paused: boolean;
  waitNext: boolean;
  timers: TimerView[];
  winner?: string;
  ended: boolean;
  question?: QuestionView;
  pending?: PendingView;
  appeal?: AppealView;
  askChoose?: AskChoosePayload;
  askAnswer?: AskAnswerPayload;
  askStake?: AskStakePayload;
  askValidate?: AskValidatePayload;
  askDeleteTheme?: AskDeleteThemePayload;
  askAppealVote?: AskAppealVotePayload;
}

// ---- room payloads (internal/room/messages.go) -----------------------------

export interface PersonView {
  id: string;
  name: string;
  role: Role;
  connected: boolean;
  score: number;
  isHost: boolean;
}

/** buzzer.Settings — only the fields the client reads are typed. */
export interface BuzzerSettings {
  mode: 'anchoredHybrid' | 'serverArrival' | 'randomWindow' | 'clientReaction' | 'writtenAll';
  netProfile: 'lan' | 'wifi' | 'wan';
  pressWindowMs: number;
  falseStartLockoutMs: number;
  maxRttMs: number;
  showPing: boolean;
  [key: string]: unknown;
}

export interface RoomInfo {
  id: string;
  code: string;
  name: string;
  packId: string;
  packName: string;
  status: 'lobby' | 'playing' | 'finished' | 'closed';
  showman: ShowmanMode;
  hasPassword: boolean;
  players: PersonView[];
  viewers: number;
  maxPlayers: number;
  allowViewers: boolean;
  language?: string;
  hybridConfirmMs: number;
  createdAt: number;
  updatedAt: number;
  rules: Rules;
  times: TimeSettings;
  buzzer: BuzzerSettings;
  joinMode: 'any' | 'viewersOnly' | 'closed';
}

export interface SyncPlan {
  burst: number;
  intervalMs: number;
  steadyMs: number;
}

export interface WelcomePayload {
  personId: string;
  name: string;
  role: Role;
  isHost: boolean;
  roomCode: string;
  showman: ShowmanMode;
  serverTimeMs: number;
  serverWallMs: number;
  syncPlan: SyncPlan;
  buzzerSettings: BuzzerSettings;
}

export interface BuzzerSnapshot {
  state: 'idle' | 'armed' | 'collecting' | 'draining' | 'resolved';
  armId?: string;
  armAt?: number;
  armAtLocal?: number;
  deadlineAt?: number;
  lockedUntil?: number;
}

export interface SnapshotPayload {
  room: RoomInfo;
  game: GameSnapshot | null;
  buzzer: BuzzerSnapshot;
  lastSeq: number;
}

export interface ResumePayload {
  fromSeq: number;
  count: number;
  covered: boolean;
}

export interface RoomPersonsPayload {
  persons: PersonView[];
}

export interface HostChangedPayload {
  personId: string;
}

export interface ChatPayload {
  personId: string;
  name: string;
  text: string;
  atMs: number;
}

export interface RoomClosedPayload {
  reason: 'host' | 'ttl' | 'shutdown';
}

export interface KickedPayload {
  banned: boolean;
}

export type ErrorCode =
  | 'notAllowed'
  | 'badState'
  | 'badArgument'
  | 'unknownType'
  | 'rateLimited'
  | 'badPayload'
  | 'badToken';

export interface ErrorPayload {
  code: ErrorCode | string;
  message: string;
  ref?: number;
}

/** buzzer.Model — the public clock model carried by SYNC_ACK. */
export interface ClockModel {
  offsetMs: number;
  rttRefMs: number;
  rttWsMs: number;
  rttKernelMs: number;
  sigmaMs: number;
  tolMs: number;
  uMs: number;
  leadMs: number;
  quality: Quality;
  flags?: string[];
  samples: number;
}

export interface SyncAckPayload {
  seq: number;
  c1: number;
  s2: number;
  s3: number;
  model: ClockModel;
}

export interface ButtonArmPayload {
  armId: string;
  armAt: number;
  armAtLocal: number;
  mode: 'scheduled' | 'onReceipt';
  deadlineAt: number;
  lockoutMs: number;
}

export type PressStatus =
  | 'pending'
  | 'rejected'
  | 'duplicate'
  | 'late'
  | 'stale'
  | 'lockedOut'
  | 'tooEarly'
  | 'falseStart';

export interface PressAckPayload {
  armId: string;
  status: PressStatus;
  reason?: string;
  lockoutUntil?: number;
  reactionMs?: number;
}

export interface PressPendingPayload {
  armId: string;
  playerId: string;
}

/** Public BUTTON_RESULT (players/viewers). Showman/host get the full buzzer.Result on top. */
export interface ButtonResultPayload {
  armId: string;
  winnerId?: string;
  kind: 'winner' | 'nobody' | 'allPlay';
  contested: boolean;
  marginMs: number;
  rule: string;
  reactionMs?: number;
  source?: 'client' | 'clamped' | 'server' | 'arrival';
  // full variant (staff only)
  tieBreak?: string;
  ranking?: unknown[];
  tie?: string[];
  resolvedAt?: number;
  collectMs?: number;
  seed?: string;
}

export interface LockoutPayload {
  playerId: string;
  untilAt: number;
  durationMs: number;
  reason: 'falseStart' | 'tooEarly' | 'misfire';
}

export interface ConnQualityEntry {
  playerId: string;
  rttMs?: number;
  jitterMs?: number;
  uMs?: number;
  quality: Quality;
  connected: boolean;
  flags?: string[];
  trust?: unknown;
}

export interface ConnQualityPayload {
  players: ConnQualityEntry[];
}

export interface AIVerdictPayload {
  personId: string;
  right?: boolean;
  factor?: number;
  uncertain?: boolean;
  reason?: string;
  source: string;
  applied: boolean;
}

// ---- server → client map ---------------------------------------------------

export interface ServerMessages {
  // room lifecycle
  WELCOME: WelcomePayload;
  SNAPSHOT: SnapshotPayload;
  RESUME: ResumePayload;
  ROOM_PERSONS: RoomPersonsPayload;
  ROOM_SETTINGS: RoomInfo;
  HOST_CHANGED: HostChangedPayload;
  CHAT: ChatPayload;
  KICKED: KickedPayload;
  SESSION_REPLACED: Record<string, never> | undefined;
  ROOM_CLOSED: RoomClosedPayload;
  ERROR: ErrorPayload;
  // buzzer
  SYNC_ACK: SyncAckPayload;
  BUTTON_ARM: ButtonArmPayload;
  PRESS_ACK: PressAckPayload;
  PRESS_PENDING: PressPendingPayload;
  BUTTON_RESULT: ButtonResultPayload;
  LOCKOUT: LockoutPayload;
  CONN_QUALITY: ConnQualityPayload;
  BUTTON_AUDIT: unknown;
  AI_VERDICT: AIVerdictPayload;
  // engine events
  STAGE: StagePayload;
  PLAYERS: PlayersPayload;
  OPTIONS: OptionsPayload;
  ROUND_START: RoundStartPayload;
  ROUND_CONTENT: RoundContentPayload;
  TABLE: TablePayload;
  SUMS: SumsPayload;
  SET_CHOOSER: SetChooserPayload;
  ASK_CHOOSE: AskChoosePayload;
  ASK_SELECT_PLAYER: AskSelectPlayerPayload;
  QUESTION_START: QuestionStartPayload;
  QUESTION_CAPTION: QuestionCaptionPayload;
  SHOWMAN_HINT: ShowmanHintPayload;
  CONTENT: ContentPayload;
  CONTENT_STATE: ContentStatePayload;
  ANSWER_OPTIONS: AnswerOptionsPayload;
  MEDIA_WAIT: MediaWaitPayload;
  ASK_ANSWER: AskAnswerPayload;
  ANSWER_DRAFT: AnswerDraftPayload;
  PLAYER_ANSWER: PlayerAnswerPayload;
  ASK_VALIDATE: AskValidatePayload;
  VALIDATION: ValidationPayload;
  AI_SUGGESTION: AISuggestionPayload;
  PERSON_SCORE: PersonScorePayload;
  PLAYER_STATE: PlayerStatePayload;
  PASS: PassPayload;
  RIGHT_ANSWER: RightAnswerPayload;
  QUESTION_END: QuestionEndPayload;
  ASK_STAKE: AskStakePayload;
  PERSON_STAKE: PersonStakePayload;
  ASK_DELETE_THEME: AskDeleteThemePayload;
  THEME_DELETED: ThemeDeletedPayload;
  FINAL_THINK: FinalThinkPayload;
  TIMER_START: TimerStartPayload;
  TIMER_STOP: TimerStopPayload;
  TIMER_PAUSE: TimerPausePayload;
  TIMER_RESUME: TimerPausePayload;
  PAUSE: PausePayload;
  TOGGLE: TogglePayload;
  ROUND_END: RoundEndPayload;
  WINNER: WinnerPayload;
  GAME_END: GameEndPayload;
  APPEAL_START: AppealStartPayload;
  ASK_APPEAL_VOTE: AskAppealVotePayload;
  APPEAL_VOTE: AppealVotePayload;
  APPEAL_RESULT: AppealResultPayload;
  USER_ERROR: UserErrorPayload;
}

export type ServerMessageType = keyof ServerMessages;

/** An inbound message after decoding, stamped with the local receive time. */
export type ServerMessage = {
  [T in ServerMessageType]: { t: T; seq: number; p: ServerMessages[T]; tRecv: number };
}[ServerMessageType];

/** A message whose type is not in the map (forward compatibility). */
export interface UnknownServerMessage {
  t: string;
  seq: number;
  p: unknown;
  tRecv: number;
}

// ---- client → server map ---------------------------------------------------

export interface SyncIn {
  seq: number;
  c1: number;
  prevSeq?: number;
  prevC4?: number;
}

export interface PressIn {
  armId: string;
  seq: number;
  pressLocal: number;
  litLocal: number;
  src: 'pointer' | 'key';
}

export interface AnswerIn {
  personId?: string;
  text?: string;
  optionLabel?: string;
  number?: number;
  point?: Point;
  right?: boolean;
}

export interface ValidateIn {
  personId?: string;
  right: boolean;
  factor?: number;
  factorZero?: boolean;
}

export type RulesPatch = Partial<
  Pick<
    Rules,
    | 'oral'
    | 'managed'
    | 'displayAnswerOptionsLabels'
    | 'falseStart'
    | 'readingSpeed'
    | 'partialText'
    | 'useAppellations'
  > & { buttonBlockingMs: number; partialImageMs: number }
>;

export interface ClientMessages {
  HELLO: { lastSeq: number };
  SYNC: SyncIn;
  ARM_ACK: { armId: string; recvLocal: number };
  PRESS: PressIn;
  MISFIRE: { armId?: string };
  VISIBLE: Record<string, never>;
  CHAT: { text: string };
  READY: undefined;
  START: undefined;
  CHOOSE_QUESTION: { theme: number; q: number };
  PASS: undefined;
  ANSWER: AnswerIn;
  ANSWER_DRAFT: { text: string };
  VALIDATE: ValidateIn;
  SELECT_PLAYER: { personId: string };
  SET_STAKE: { stakeMode: StakeMode; amount?: number };
  DELETE_THEME: { theme: number };
  APPELLATE: { for: boolean };
  VOTE_APPEAL: { right: boolean };
  MEDIA_LOADED: undefined;
  MEDIA_COMPLETED: undefined;
  PAUSE: { on: boolean };
  MOVE: { dir: -2 | -1 | 1 | 2 | 3; round?: number };
  TOGGLE: { theme: number; q: number };
  CHANGE_SCORE: { personId: string; newSum: number };
  SET_CHOOSER: { personId: string };
  SET_OPTIONS: { options: RulesPatch };
  NEXT: undefined;
  KICK: { personId: string; ban?: boolean };
  SET_HOST: { personId: string };
  BUTTON_REOPEN: { armId?: string };
  SET_TRUST: { personId: string; level: TrustLevel | '' };
}

export type ClientMessageType = keyof ClientMessages;

/** Close codes (docs/protocol.md §1). */
export const CloseCode = {
  Normal: 1000,
  GoingAway: 1001,
  Policy: 1008,
  BadToken: 4000,
  SessionReplaced: 4001,
  Kicked: 4002,
} as const;
