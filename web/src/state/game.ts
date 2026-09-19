/**
 * Game store — a reducer over engine events (docs/protocol-engine.md) plus the
 * authoritative SNAPSHOT. Everything here is a projection for rendering: the
 * server is the source of truth, so SNAPSHOT always resets the state.
 */
import { create } from 'zustand';
import type {
  AnswerOptionsPayload,
  AnswerView,
  AppealView,
  AskAnswerPayload,
  AskAppealVotePayload,
  AskChoosePayload,
  AskDeleteThemePayload,
  AskSelectPlayerPayload,
  AskStakePayload,
  AskValidatePayload,
  ContentPayload,
  GameEndPayload,
  GameSnapshot,
  MediaWaitPayload,
  OutcomeView,
  PendingView,
  PersonStakePayload,
  PlayerInfo,
  QSub,
  QuestionType,
  AnswerType,
  RightAnswerPayload,
  Role,
  RoundType,
  Rules,
  ServerMessages,
  ShowmanHintPayload,
  Stage,
  StakesView,
  TablePayload,
  TimeSettings,
  ValidationPayload,
} from '../ws/types.ts';

export interface TimerState {
  id: string;
  kind: string;
  durationMs: number;
  /** Start instant on the local performance.now() clock. */
  startedAtLocal: number;
  paused: boolean;
  /** Valid while paused. */
  remainingMs: number;
  personId?: string;
}

export interface CurrentQuestion {
  questionId: string;
  themeIndex: number;
  questionIndex: number;
  theme: string;
  price: number;
  type: QuestionType;
  answerType: AnswerType;
  isDefault: boolean;
  content: ContentPayload[];
  contentState: 'paused' | 'resumed' | null;
  mediaWait: MediaWaitPayload | null;
  options: AnswerOptionsPayload | null;
  excludedOptions: string[];
  rightAnswer: RightAnswerPayload | null;
  answererId: string | null;
  answers: Record<string, AnswerView>;
  drafts: Record<string, string>;
  validations: ValidationPayload[];
  history: OutcomeView[];
  stakes: StakesView | null;
  stakeLog: PersonStakePayload[];
  hint: ShowmanHintPayload | null;
  ended: boolean;
}

export interface Prompts {
  askChoose?: AskChoosePayload;
  askAnswer?: AskAnswerPayload;
  askStake?: AskStakePayload;
  askValidate?: AskValidatePayload;
  askDeleteTheme?: AskDeleteThemePayload;
  askSelectPlayer?: AskSelectPlayerPayload;
  askAppealVote?: AskAppealVotePayload;
}

export interface GameState {
  hasGame: boolean;
  stage: Stage;
  sub: QSub | null;
  roundIndex: number;
  roundName: string;
  roundType: RoundType | null;
  table: TablePayload;
  chooserId: string | null;
  players: PlayerInfo[];
  scores: Record<string, number>;
  rules: Rules | null;
  times: TimeSettings | null;
  paused: boolean;
  waitNext: boolean;
  question: CurrentQuestion | null;
  prompts: Prompts;
  timers: Record<string, TimerState>;
  appeal: (AppealView & { votes: Record<string, boolean> }) | null;
  pending: PendingView | null;
  winnerId: string | null;
  ended: boolean;
  gameEnd: GameEndPayload | null;
}

export interface ReduceContext {
  me: string;
  role: Role;
  /** Local receive instant of the message. */
  tRecv: number;
  /** Server monotonic → local clock. */
  toLocal: (serverMs: number) => number;
}

export const initialGameState: GameState = {
  hasGame: false,
  stage: 'lobby',
  sub: null,
  roundIndex: 0,
  roundName: '',
  roundType: null,
  table: { themes: [] },
  chooserId: null,
  players: [],
  scores: {},
  rules: null,
  times: null,
  paused: false,
  waitNext: false,
  question: null,
  prompts: {},
  timers: {},
  appeal: null,
  pending: null,
  winnerId: null,
  ended: false,
  gameEnd: null,
};

type Msg = { [T in keyof ServerMessages]: { t: T; p: ServerMessages[T]; tRecv: number } }[keyof ServerMessages];

function newQuestion(p: ServerMessages['QUESTION_START']): CurrentQuestion {
  return {
    questionId: p.questionId,
    themeIndex: p.themeIndex,
    questionIndex: p.questionIndex,
    theme: p.theme,
    price: p.price,
    type: p.type,
    answerType: p.answerType,
    isDefault: p.isDefault,
    content: [],
    contentState: null,
    mediaWait: null,
    options: null,
    excludedOptions: [],
    rightAnswer: null,
    answererId: null,
    answers: {},
    drafts: {},
    validations: [],
    history: [],
    stakes: null,
    stakeLog: [],
    hint: null,
    ended: false,
  };
}

function scoresFrom(entries: { personId: string; score: number }[]): Record<string, number> {
  const out: Record<string, number> = {};
  for (const e of entries) out[e.personId] = e.score;
  return out;
}

/** Drops timers that expired more than a moment ago (the engine does not always TIMER_STOP them). */
function pruneTimers(timers: Record<string, TimerState>, localNow: number): Record<string, TimerState> {
  let changed = false;
  const out: Record<string, TimerState> = {};
  for (const t of Object.values(timers)) {
    if (!t.paused && localNow - t.startedAtLocal > t.durationMs + 1500) {
      changed = true;
      continue;
    }
    out[t.id] = t;
  }
  return changed ? out : timers;
}

/** Builds the state from an authoritative snapshot (null = lobby). */
export function fromSnapshot(game: GameSnapshot | null, ctx: ReduceContext): GameState {
  if (!game) return { ...initialGameState };
  const timers: Record<string, TimerState> = {};
  for (const t of game.timers) {
    timers[t.id] = {
      id: t.id,
      kind: t.kind,
      durationMs: t.durationMs,
      startedAtLocal: ctx.toLocal(t.startedAtMs),
      paused: t.paused,
      remainingMs: t.remainingMs,
      personId: t.personId,
    };
  }
  let question: CurrentQuestion | null = null;
  const q = game.question;
  if (q) {
    question = {
      questionId: q.questionId,
      themeIndex: q.themeIndex,
      questionIndex: q.questionIndex,
      theme: q.theme,
      price: q.curPrice || q.price,
      type: q.type,
      answerType: q.answerType,
      isDefault: q.isDefault,
      content: q.content ?? [],
      contentState: null,
      mediaWait: null,
      options: q.options ?? null,
      excludedOptions: q.excludedOptions ?? [],
      rightAnswer: q.rightAnswer ?? null,
      answererId: q.answererId ?? null,
      answers: q.hiddenAnswers ?? {},
      drafts: {},
      validations: [],
      history: q.history ?? [],
      stakes: q.stakes ?? null,
      stakeLog: [],
      hint: q.rights ? { rights: q.rights, wrongs: q.wrongs } : null,
      ended: q.sub === 'end',
    };
  }
  const prompts: Prompts = {};
  if (game.askChoose) prompts.askChoose = game.askChoose;
  if (game.askAnswer) prompts.askAnswer = game.askAnswer;
  if (game.askStake) prompts.askStake = game.askStake;
  if (game.askValidate) prompts.askValidate = game.askValidate;
  if (game.askDeleteTheme) prompts.askDeleteTheme = game.askDeleteTheme;
  if (game.askAppealVote) prompts.askAppealVote = game.askAppealVote;
  if (game.pending && game.pending.deciderId === ctx.me) {
    prompts.askSelectPlayer = {
      reason: game.pending.kind,
      candidates: game.pending.candidates,
      deciderId: game.pending.deciderId,
      durationMs: 0,
    };
  }
  return {
    hasGame: true,
    stage: game.stage,
    sub: game.sub ?? null,
    roundIndex: game.roundIndex,
    roundName: game.roundName ?? '',
    roundType: game.roundType ?? null,
    table: game.table ?? { themes: [] },
    chooserId: game.chooserId ?? null,
    players: game.players ?? [],
    scores: scoresFrom(game.scores ?? []),
    rules: game.rules,
    times: game.times,
    paused: game.paused,
    waitNext: game.waitNext,
    question,
    prompts,
    timers,
    appeal: game.appeal ? { ...game.appeal, votes: {} } : null,
    pending: game.pending ?? null,
    winnerId: game.winner || null,
    ended: game.ended,
    gameEnd: null,
  };
}

/** Pure reducer: applies one engine event. Unknown/room messages return the same state. */
export function reduceGame(state: GameState, msg: Msg, ctx: ReduceContext): GameState {
  const s: GameState = state.hasGame ? state : { ...state, hasGame: isEngineEvent(msg.t) };
  const q = s.question;
  switch (msg.t) {
    case 'STAGE': {
      const p = msg.p;
      const next: GameState = { ...s, stage: p.stage, sub: p.sub ?? null, roundIndex: p.roundIndex };
      if (p.stage !== 'question') next.question = null;
      if (p.stage !== 'selecting') next.prompts = { ...next.prompts, askChoose: undefined };
      if (p.stage !== 'finalThemes') next.prompts = { ...next.prompts, askDeleteTheme: undefined };
      if (p.stage !== 'question' || (p.sub && p.sub !== 'answering' && p.sub !== 'hiddenAnswering'))
        next.prompts = { ...next.prompts, askAnswer: undefined };
      if (p.sub && p.sub !== 'stakes' && p.sub !== 'priceSelect') next.prompts = { ...next.prompts, askStake: undefined };
      return next;
    }
    case 'PLAYERS': {
      const scores = { ...s.scores };
      for (const pl of msg.p.players) scores[pl.id] = pl.score;
      return { ...s, players: msg.p.players, scores };
    }
    case 'OPTIONS':
      return { ...s, rules: msg.p.rules, times: msg.p.times };
    case 'ROUND_START':
      return {
        ...s,
        roundIndex: msg.p.index,
        roundName: msg.p.name,
        roundType: msg.p.type,
        question: null,
        prompts: {},
        pending: null,
      };
    case 'TABLE':
      return { ...s, table: msg.p };
    case 'SUMS':
      return { ...s, scores: scoresFrom(msg.p.scores) };
    case 'SET_CHOOSER':
      return { ...s, chooserId: msg.p.personId, pending: null, prompts: { ...s.prompts, askSelectPlayer: undefined } };
    case 'ASK_CHOOSE':
      return { ...s, prompts: { ...s.prompts, askChoose: msg.p } };
    case 'ASK_SELECT_PLAYER': {
      const pending: PendingView = { kind: msg.p.reason, deciderId: msg.p.deciderId, candidates: msg.p.candidates };
      const prompts = msg.p.deciderId === ctx.me || msg.p.deciderId === '' ? { ...s.prompts, askSelectPlayer: msg.p } : s.prompts;
      return { ...s, pending, prompts };
    }
    case 'QUESTION_START':
      return {
        ...s,
        stage: 'question',
        question: newQuestion(msg.p),
        prompts: { ...s.prompts, askChoose: undefined, askSelectPlayer: undefined },
        pending: null,
      };
    case 'QUESTION_CAPTION':
      return q ? { ...s, question: { ...q, theme: msg.p.theme, price: msg.p.price } } : s;
    case 'SHOWMAN_HINT':
      return q ? { ...s, question: { ...q, hint: msg.p } } : s;
    case 'CONTENT': {
      if (!q) return s;
      const content = q.content.filter((c) => !(c.phase === msg.p.phase && c.index === msg.p.index));
      content.push(msg.p);
      return { ...s, question: { ...q, content, contentState: null } };
    }
    case 'CONTENT_STATE':
      return q ? { ...s, question: { ...q, contentState: msg.p.state } } : s;
    case 'ANSWER_OPTIONS':
      return q ? { ...s, question: { ...q, options: msg.p } } : s;
    case 'MEDIA_WAIT':
      return q ? { ...s, question: { ...q, mediaWait: msg.p } } : s;
    case 'ASK_ANSWER': {
      const prompts = msg.p.personId === ctx.me || (msg.p.oral && ctx.role === 'showman') || msg.p.personId === ''
        ? { ...s.prompts, askAnswer: msg.p }
        : s.prompts;
      return { ...s, prompts, question: q && !msg.p.hidden ? { ...q, answererId: msg.p.personId } : q };
    }
    case 'ANSWER_DRAFT':
      return q ? { ...s, question: { ...q, drafts: { ...q.drafts, [msg.p.personId]: msg.p.text } } } : s;
    case 'PLAYER_ANSWER': {
      const prompts = msg.p.personId === ctx.me ? { ...s.prompts, askAnswer: undefined } : s.prompts;
      return q ? { ...s, prompts, question: { ...q, answers: { ...q.answers, [msg.p.personId]: msg.p.answer } } } : { ...s, prompts };
    }
    case 'ASK_VALIDATE':
      return { ...s, prompts: { ...s.prompts, askValidate: msg.p } };
    case 'VALIDATION': {
      const prompts =
        s.prompts.askValidate && s.prompts.askValidate.personId === msg.p.personId
          ? { ...s.prompts, askValidate: undefined }
          : s.prompts;
      const excluded = msg.p.excludedOption && q ? [...q.excludedOptions, msg.p.excludedOption] : q?.excludedOptions ?? [];
      return q
        ? { ...s, prompts, question: { ...q, validations: [...q.validations, msg.p], excludedOptions: excluded } }
        : { ...s, prompts };
    }
    case 'PERSON_SCORE':
      return {
        ...s,
        scores: { ...s.scores, [msg.p.personId]: msg.p.score },
        players: s.players.map((pl) => (pl.id === msg.p.personId ? { ...pl, score: msg.p.score } : pl)),
      };
    case 'PLAYER_STATE':
      return { ...s, players: s.players.map((pl) => (pl.id === msg.p.personId ? { ...pl, state: msg.p.state } : pl)) };
    case 'RIGHT_ANSWER':
      return q ? { ...s, question: { ...q, rightAnswer: msg.p } } : s;
    case 'QUESTION_END':
      return {
        ...s,
        question: q ? { ...q, ended: true } : q,
        prompts: { ...s.prompts, askAnswer: undefined, askStake: undefined, askValidate: undefined, askSelectPlayer: undefined },
        pending: null,
      };
    case 'ASK_STAKE':
      return { ...s, prompts: { ...s.prompts, askStake: msg.p, askSelectPlayer: undefined }, pending: null };
    case 'PERSON_STAKE': {
      const prompts = msg.p.personId === ctx.me ? { ...s.prompts, askStake: undefined } : s.prompts;
      return q ? { ...s, prompts, question: { ...q, stakeLog: [...q.stakeLog, msg.p] } } : { ...s, prompts };
    }
    case 'ASK_DELETE_THEME':
      return { ...s, prompts: { ...s.prompts, askDeleteTheme: msg.p, askSelectPlayer: undefined }, pending: null };
    case 'THEME_DELETED': {
      const themes = s.table.themes.map((t, i) => (i === msg.p.themeIndex ? { ...t, removed: true } : t));
      return { ...s, table: { themes }, prompts: { ...s.prompts, askDeleteTheme: undefined } };
    }
    case 'FINAL_THINK':
      return s;
    case 'TIMER_START': {
      const t: TimerState = {
        id: msg.p.id,
        kind: msg.p.kind,
        durationMs: msg.p.durationMs,
        startedAtLocal: msg.tRecv,
        paused: false,
        remainingMs: msg.p.durationMs,
        personId: msg.p.personId,
      };
      return { ...s, timers: { ...pruneTimers(s.timers, msg.tRecv), [t.id]: t } };
    }
    case 'TIMER_STOP': {
      if (!(msg.p.id in s.timers)) return s;
      const timers = { ...s.timers };
      delete timers[msg.p.id];
      return { ...s, timers };
    }
    case 'TIMER_PAUSE': {
      const t = s.timers[msg.p.id];
      const base: TimerState = t ?? {
        id: msg.p.id,
        kind: msg.p.kind,
        durationMs: msg.p.remainingMs,
        startedAtLocal: msg.tRecv,
        paused: true,
        remainingMs: msg.p.remainingMs,
      };
      return { ...s, timers: { ...s.timers, [msg.p.id]: { ...base, paused: true, remainingMs: msg.p.remainingMs } } };
    }
    case 'TIMER_RESUME': {
      const t = s.timers[msg.p.id];
      const durationMs = t?.durationMs ?? msg.p.remainingMs;
      const resumed: TimerState = {
        id: msg.p.id,
        kind: msg.p.kind,
        durationMs,
        startedAtLocal: msg.tRecv - (durationMs - msg.p.remainingMs),
        paused: false,
        remainingMs: msg.p.remainingMs,
        personId: t?.personId,
      };
      return { ...s, timers: { ...s.timers, [msg.p.id]: resumed } };
    }
    case 'PAUSE':
      return { ...s, paused: msg.p.on };
    case 'TOGGLE': {
      const themes = s.table.themes.map((t, ti) =>
        ti === msg.p.themeIndex
          ? { ...t, questions: t.questions.map((c, qi) => (qi === msg.p.questionIndex ? { ...c, removed: !msg.p.active } : c)) }
          : t,
      );
      return { ...s, table: { themes } };
    }
    case 'ROUND_END':
      return { ...s, prompts: {}, pending: null };
    case 'WINNER':
      return { ...s, winnerId: msg.p.personId || null };
    case 'GAME_END':
      return { ...s, ended: true, gameEnd: msg.p, scores: scoresFrom(msg.p.scores), winnerId: msg.p.winnerId || null, prompts: {} };
    case 'APPEAL_START':
      return {
        ...s,
        appeal: {
          kind: msg.p.kind,
          appellantId: msg.p.appellantId,
          answererId: msg.p.answererId,
          answer: msg.p.answer,
          for: 0,
          against: 0,
          pending: msg.p.voters,
          votes: {},
        },
      };
    case 'ASK_APPEAL_VOTE':
      return { ...s, prompts: { ...s.prompts, askAppealVote: msg.p } };
    case 'APPEAL_VOTE': {
      const prompts = msg.p.personId === ctx.me ? { ...s.prompts, askAppealVote: undefined } : s.prompts;
      if (!s.appeal) return { ...s, prompts };
      const votes = { ...s.appeal.votes, [msg.p.personId]: msg.p.right };
      const pending = s.appeal.pending.filter((id) => id !== msg.p.personId);
      const f = Object.values(votes).filter(Boolean).length;
      return { ...s, prompts, appeal: { ...s.appeal, votes, pending, for: f, against: Object.keys(votes).length - f } };
    }
    case 'APPEAL_RESULT':
      return { ...s, appeal: null, prompts: { ...s.prompts, askAppealVote: undefined } };
    default:
      return s;
  }
}

const ENGINE_EVENTS = new Set<string>([
  'STAGE', 'PLAYERS', 'OPTIONS', 'ROUND_START', 'ROUND_CONTENT', 'TABLE', 'SUMS', 'SET_CHOOSER', 'ASK_CHOOSE',
  'ASK_SELECT_PLAYER', 'QUESTION_START', 'QUESTION_CAPTION', 'SHOWMAN_HINT', 'CONTENT', 'CONTENT_STATE',
  'ANSWER_OPTIONS', 'MEDIA_WAIT', 'ASK_ANSWER', 'ANSWER_DRAFT', 'PLAYER_ANSWER', 'ASK_VALIDATE', 'VALIDATION',
  'AI_SUGGESTION', 'PERSON_SCORE', 'PLAYER_STATE', 'PASS', 'RIGHT_ANSWER', 'QUESTION_END', 'ASK_STAKE',
  'PERSON_STAKE', 'ASK_DELETE_THEME', 'THEME_DELETED', 'FINAL_THINK', 'TIMER_START', 'TIMER_STOP', 'TIMER_PAUSE',
  'TIMER_RESUME', 'PAUSE', 'TOGGLE', 'ROUND_END', 'WINNER', 'GAME_END', 'APPEAL_START', 'ASK_APPEAL_VOTE',
  'APPEAL_VOTE', 'APPEAL_RESULT',
]);

export function isEngineEvent(t: string): boolean {
  return ENGINE_EVENTS.has(t);
}

/** Prompt keys currently outstanding for me. */
export function activePrompts(p: Prompts): (keyof Prompts)[] {
  return (Object.keys(p) as (keyof Prompts)[]).filter((k) => p[k] !== undefined);
}

// ---- store -------------------------------------------------------------------

interface GameStore {
  game: GameState;
  apply: (msg: Msg, ctx: ReduceContext) => void;
  applySnapshot: (game: GameSnapshot | null, ctx: ReduceContext) => void;
  reset: () => void;
}

export const useGameStore = create<GameStore>()((set) => ({
  game: initialGameState,
  apply: (msg, ctx) =>
    set((s) => {
      if (!isEngineEvent(msg.t)) return s;
      const next = reduceGame(s.game, msg, ctx);
      return next === s.game ? s : { game: next };
    }),
  applySnapshot: (game, ctx) => set({ game: fromSnapshot(game, ctx) }),
  reset: () => set({ game: initialGameState }),
}));
