import { beforeEach, describe, expect, it } from 'vitest';
import type { GameSnapshot, PlayerInfo, Rules, ServerMessages, TimeSettings } from '../ws/types.ts';
import {
  activePrompts,
  fromSnapshot,
  initialGameState,
  isEngineEvent,
  reduceGame,
  useGameStore,
  type GameState,
  type ReduceContext,
} from './game.ts';

type Msg = Parameters<typeof reduceGame>[1];

const ctx: ReduceContext = { me: 'p1', role: 'player', tRecv: 5000, toLocal: (ms) => ms - 100 };

function ev<T extends Msg['t']>(t: T, p: ServerMessages[T], tRecv = ctx.tRecv): Msg {
  return { t, p, tRecv } as Msg;
}

function player(id: string, over: Partial<PlayerInfo> = {}): PlayerInfo {
  return { id, name: id.toUpperCase(), score: 0, connected: true, ready: false, state: 'none', canPress: true, inGame: true, ...over };
}

const rules = { mode: 'classic', falseStart: true, oral: false } as Rules;
const times = { questionSelectionMs: 30_000 } as TimeSettings;

function snapshot(over: Partial<GameSnapshot> = {}): GameSnapshot {
  return {
    role: 'player',
    personId: 'p1',
    stage: 'selecting',
    roundIndex: 0,
    roundName: 'Раунд 1',
    roundType: 'standard',
    table: { themes: [{ name: 'T1', removed: false, questions: [{ price: 100, played: false, removed: false }] }] },
    chooserId: 'p1',
    players: [player('p1', { score: 100 }), player('p2', { score: 200 })],
    scores: [
      { personId: 'p1', score: 100 },
      { personId: 'p2', score: 200 },
    ],
    rules,
    times,
    paused: false,
    waitNext: false,
    timers: [],
    ended: false,
    ...over,
  };
}

/** A state in the question stage with a current question, reduced from events. */
function questionState(): GameState {
  let s = fromSnapshot(snapshot(), ctx);
  s = reduceGame(
    s,
    ev('QUESTION_START', {
      questionId: 'q1',
      themeIndex: 0,
      questionIndex: 0,
      theme: 'T1',
      price: 100,
      type: 'simple',
      isDefault: true,
      answerType: 'text',
    }),
    ctx,
  );
  return s;
}

describe('fromSnapshot', () => {
  it('null → the lobby state', () => {
    const s = fromSnapshot(null, ctx);
    expect(s.hasGame).toBe(false);
    expect(s.stage).toBe('lobby');
    expect(s.question).toBeNull();
    expect(s.table.themes).toEqual([]);
    expect(activePrompts(s.prompts)).toEqual([]);
  });

  it('projects stage, timers (local clock), prompts and pending decisions', () => {
    const s = fromSnapshot(
      snapshot({
        stage: 'question',
        sub: 'buttonWait',
        timers: [{ id: 't1', kind: 'buttonPressing', durationMs: 5000, startedAtMs: 4100, paused: false, remainingMs: 5000 }],
        askChoose: { personId: 'p1', durationMs: 30_000 },
        pending: { kind: 'secretTransfer', deciderId: 'p1', candidates: ['p2'] },
        question: {
          questionId: 'q1',
          themeIndex: 0,
          questionIndex: 0,
          theme: 'T1',
          price: 100,
          curPrice: 300,
          type: 'secret',
          isDefault: false,
          answerType: 'text',
          sub: 'buttonWait',
          content: [{ phase: 'question', index: 0, items: [{ type: 'text', text: 'Q?', placement: 'screen' }], waitMs: 0 }],
          history: [],
          armed: true,
          thinkingRemainingMs: 0,
          rights: ['A'],
        },
      }),
      ctx,
    );
    expect(s.hasGame).toBe(true);
    expect(s.stage).toBe('question');
    expect(s.sub).toBe('buttonWait');
    expect(s.roundName).toBe('Раунд 1');
    expect(s.chooserId).toBe('p1');
    expect(s.scores).toEqual({ p1: 100, p2: 200 });
    expect(s.rules).toBe(rules);
    expect(s.timers.t1).toMatchObject({ id: 't1', kind: 'buttonPressing', durationMs: 5000, startedAtLocal: 4000, paused: false });
    expect(s.prompts.askChoose).toEqual({ personId: 'p1', durationMs: 30_000 });
    expect(s.prompts.askSelectPlayer).toMatchObject({ reason: 'secretTransfer', deciderId: 'p1', candidates: ['p2'] });
    expect(s.pending).toEqual({ kind: 'secretTransfer', deciderId: 'p1', candidates: ['p2'] });
    expect(s.question).toMatchObject({ questionId: 'q1', price: 300, type: 'secret', ended: false });
    expect(s.question?.content).toHaveLength(1);
    expect(s.question?.hint).toEqual({ rights: ['A'], wrongs: undefined });
  });

  it('a pending decision for someone else is not my prompt', () => {
    const s = fromSnapshot(snapshot({ pending: { kind: 'chooser', deciderId: 'sm', candidates: ['p1', 'p2'] } }), ctx);
    expect(s.pending?.deciderId).toBe('sm');
    expect(s.prompts.askSelectPlayer).toBeUndefined();
  });
});

describe('reduceGame', () => {
  let base: GameState;
  beforeEach(() => {
    base = fromSnapshot(snapshot({ askChoose: { personId: 'p1', durationMs: 30_000 } }), ctx);
  });

  it('STAGE sets stage/sub/roundIndex and clears question/askChoose when leaving', () => {
    let s = reduceGame(base, ev('STAGE', { stage: 'selecting', sub: undefined, roundIndex: 0 }), ctx);
    expect(s.prompts.askChoose).toBeDefined();
    s = reduceGame(questionState(), ev('STAGE', { stage: 'question', sub: 'content', roundIndex: 1 }), ctx);
    expect(s.stage).toBe('question');
    expect(s.sub).toBe('content');
    expect(s.roundIndex).toBe(1);
    expect(s.question).not.toBeNull();
    s = reduceGame(s, ev('STAGE', { stage: 'roundEnd', roundIndex: 1 }), ctx);
    expect(s.stage).toBe('roundEnd');
    expect(s.sub).toBeNull();
    expect(s.question).toBeNull();
    expect(s.prompts.askChoose).toBeUndefined();
  });

  it('TABLE / SUMS / PLAYERS update their slices', () => {
    const table = { themes: [{ name: 'X', removed: false, questions: [{ price: 500, played: true, removed: false }] }] };
    let s = reduceGame(base, ev('TABLE', table), ctx);
    expect(s.table).toBe(table);
    s = reduceGame(s, ev('SUMS', { scores: [{ personId: 'p1', score: 700 }] }), ctx);
    expect(s.scores).toEqual({ p1: 700 });
    const players = [player('p1', { score: 700, ready: true }), player('p3')];
    s = reduceGame(s, ev('PLAYERS', { players }), ctx);
    expect(s.players).toBe(players);
    expect(s.scores).toEqual({ p1: 700, p3: 0 });
  });

  it('SET_CHOOSER / ASK_CHOOSE', () => {
    let s = reduceGame(base, ev('SET_CHOOSER', { personId: 'p2', reason: 'rightAnswer' }), ctx);
    expect(s.chooserId).toBe('p2');
    expect(s.prompts.askSelectPlayer).toBeUndefined();
    s = reduceGame(s, ev('ASK_CHOOSE', { personId: 'p2', durationMs: 10 }), ctx);
    expect(s.prompts.askChoose).toEqual({ personId: 'p2', durationMs: 10 });
  });

  it('QUESTION_START creates the question and clears askChoose', () => {
    const s = questionState();
    expect(s.stage).toBe('question');
    expect(s.question).toMatchObject({ questionId: 'q1', theme: 'T1', price: 100, type: 'simple', answerType: 'text', content: [] });
    expect(s.prompts.askChoose).toBeUndefined();
    expect(s.pending).toBeNull();
  });

  it('QUESTION_CAPTION / SHOWMAN_HINT update the question', () => {
    let s = reduceGame(questionState(), ev('QUESTION_CAPTION', { theme: 'Secret', price: 900 }), ctx);
    expect(s.question).toMatchObject({ theme: 'Secret', price: 900 });
    s = reduceGame(s, ev('SHOWMAN_HINT', { rights: ['A', 'B'], comments: 'c' }), ctx);
    expect(s.question?.hint).toEqual({ rights: ['A', 'B'], comments: 'c' });
    // Without a question these events are no-ops.
    expect(reduceGame(base, ev('SHOWMAN_HINT', { rights: ['A'] }), ctx)).toBe(base);
  });

  it('CONTENT appends groups and replaces the same phase/index', () => {
    const g0 = { phase: 'question' as const, index: 0, items: [{ type: 'text' as const, text: 'a', placement: 'screen' as const }], waitMs: 0 };
    const g1 = { ...g0, index: 1, items: [{ type: 'image' as const, mediaId: 'm1', placement: 'screen' as const }] };
    let s = reduceGame(questionState(), ev('CONTENT', g0), ctx);
    s = reduceGame(s, ev('CONTENT', g1), ctx);
    expect(s.question?.content).toEqual([g0, g1]);
    const g0b = { ...g0, items: [{ type: 'text' as const, text: 'b', placement: 'screen' as const }] };
    s = reduceGame(s, ev('CONTENT', g0b), ctx);
    expect(s.question?.content).toEqual([g1, g0b]);
    s = reduceGame(s, ev('CONTENT_STATE', { state: 'paused' }), ctx);
    expect(s.question?.contentState).toBe('paused');
    s = reduceGame(s, ev('ANSWER_OPTIONS', { options: [{ label: 'A', content: [] }], showLabels: true, oneByOne: false }), ctx);
    expect(s.question?.options?.options).toHaveLength(1);
    s = reduceGame(s, ev('MEDIA_WAIT', { mediaId: 'm1' }), ctx);
    expect(s.question?.mediaWait).toEqual({ mediaId: 'm1' });
  });

  it('ASK_ANSWER is a prompt for me (or the showman in oral mode), not for others', () => {
    const q = questionState();
    const mine = { personId: 'p1', answerType: 'text' as const, durationMs: 25_000, hidden: false, oral: false };
    let s = reduceGame(q, ev('ASK_ANSWER', mine), ctx);
    expect(s.prompts.askAnswer).toEqual(mine);
    expect(s.question?.answererId).toBe('p1');
    s = reduceGame(q, ev('ASK_ANSWER', { ...mine, personId: 'p2' }), ctx);
    expect(s.prompts.askAnswer).toBeUndefined();
    expect(s.question?.answererId).toBe('p2');
    s = reduceGame(q, ev('ASK_ANSWER', { ...mine, personId: 'p2', oral: true }), { ...ctx, me: 'sm', role: 'showman' });
    expect(s.prompts.askAnswer?.personId).toBe('p2');
    // Hidden answering does not expose an answerer.
    s = reduceGame(q, ev('ASK_ANSWER', { ...mine, hidden: true }), ctx);
    expect(s.prompts.askAnswer).toBeDefined();
    expect(s.question?.answererId).toBeNull();
  });

  it('ANSWER_DRAFT / PLAYER_ANSWER; my answer clears askAnswer', () => {
    let s = reduceGame(questionState(), ev('ASK_ANSWER', { personId: 'p1', answerType: 'text', durationMs: 1, hidden: false, oral: false }), ctx);
    s = reduceGame(s, ev('ANSWER_DRAFT', { personId: 'p1', text: 'Par' }), ctx);
    expect(s.question?.drafts).toEqual({ p1: 'Par' });
    s = reduceGame(s, ev('PLAYER_ANSWER', { personId: 'p2', answer: { text: 'Rome' } }), ctx);
    expect(s.prompts.askAnswer).toBeDefined();
    s = reduceGame(s, ev('PLAYER_ANSWER', { personId: 'p1', answer: { text: 'Paris' } }), ctx);
    expect(s.prompts.askAnswer).toBeUndefined();
    expect(s.question?.answers).toEqual({ p2: { text: 'Rome' }, p1: { text: 'Paris' } });
  });

  it('ASK_VALIDATE then VALIDATION for that person clears the prompt and logs the verdict', () => {
    const ask = { personId: 'p2', answer: 'Rome', rights: ['Paris'], allowFactor: true, oral: false, autoAfterMs: 0, preview: false };
    let s = reduceGame(questionState(), ev('ASK_VALIDATE', ask), ctx);
    expect(s.prompts.askValidate).toEqual(ask);
    s = reduceGame(s, ev('VALIDATION', { personId: 'p9', answer: 'x', right: true, factor: 1, source: 'showman' }), ctx);
    expect(s.prompts.askValidate).toEqual(ask);
    const v = { personId: 'p2', answer: 'Rome', right: false, factor: 1, source: 'showman', excludedOption: 'B' };
    s = reduceGame(s, ev('VALIDATION', v), ctx);
    expect(s.prompts.askValidate).toBeUndefined();
    expect(s.question?.validations).toHaveLength(2);
    expect(s.question?.validations[1]).toEqual(v);
    expect(s.question?.excludedOptions).toEqual(['B']);
  });

  it('PERSON_SCORE / PLAYER_STATE / RIGHT_ANSWER', () => {
    let s = reduceGame(base, ev('PERSON_SCORE', { personId: 'p2', delta: -100, score: 100, reason: 'penalty' }), ctx);
    expect(s.scores.p2).toBe(100);
    expect(s.players.find((p) => p.id === 'p2')?.score).toBe(100);
    s = reduceGame(s, ev('PLAYER_STATE', { personId: 'p2', state: 'wrong' }), ctx);
    expect(s.players.find((p) => p.id === 'p2')?.state).toBe('wrong');
    expect(s.players.find((p) => p.id === 'p1')?.state).toBe('none');
    s = reduceGame(questionState(), ev('RIGHT_ANSWER', { text: 'Paris', comments: 'capital' }), ctx);
    expect(s.question?.rightAnswer).toEqual({ text: 'Paris', comments: 'capital' });
  });

  it('QUESTION_END marks the question ended and clears the question prompts', () => {
    let s = questionState();
    s = reduceGame(s, ev('ASK_ANSWER', { personId: 'p1', answerType: 'text', durationMs: 1, hidden: false, oral: false }), ctx);
    s = reduceGame(s, ev('ASK_STAKE', { personId: 'p1', modes: ['nominal'], min: 1, max: 2, step: 1, reason: 'stake', durationMs: 1 }), ctx);
    s = reduceGame(s, ev('ASK_VALIDATE', { personId: 'p2', answer: 'x', rights: [], allowFactor: false, oral: false, autoAfterMs: 0, preview: false }), ctx);
    expect(activePrompts(s.prompts).sort()).toEqual(['askAnswer', 'askStake', 'askValidate']);
    s = reduceGame(s, ev('QUESTION_END', { themeIndex: 0, questionIndex: 0 }), ctx);
    expect(s.question?.ended).toBe(true);
    expect(activePrompts(s.prompts)).toEqual([]);
    expect(s.pending).toBeNull();
  });

  it('ASK_STAKE / PERSON_STAKE', () => {
    const ask = { personId: 'p1', modes: ['nominal', 'stake', 'allIn', 'pass'] as const, min: 100, max: 500, step: 100, reason: 'stake' as const, durationMs: 30_000 };
    let s = reduceGame(questionState(), ev('ASK_STAKE', { ...ask, modes: [...ask.modes] }), ctx);
    expect(s.prompts.askStake?.max).toBe(500);
    s = reduceGame(s, ev('PERSON_STAKE', { personId: 'p2', mode: 'pass', amount: 0, hidden: false }), ctx);
    expect(s.prompts.askStake).toBeDefined();
    expect(s.question?.stakeLog).toHaveLength(1);
    s = reduceGame(s, ev('PERSON_STAKE', { personId: 'p1', mode: 'stake', amount: 300, hidden: false }), ctx);
    expect(s.prompts.askStake).toBeUndefined();
    expect(s.question?.stakeLog).toHaveLength(2);
  });

  it('ASK_SELECT_PLAYER is a prompt for the decider only', () => {
    const p = { reason: 'chooser' as const, candidates: ['p1', 'p2'], deciderId: 'p1', durationMs: 30_000 };
    let s = reduceGame(base, ev('ASK_SELECT_PLAYER', p), ctx);
    expect(s.prompts.askSelectPlayer).toEqual(p);
    expect(s.pending).toEqual({ kind: 'chooser', deciderId: 'p1', candidates: ['p1', 'p2'] });
    s = reduceGame(base, ev('ASK_SELECT_PLAYER', { ...p, deciderId: 'sm' }), ctx);
    expect(s.prompts.askSelectPlayer).toBeUndefined();
    expect(s.pending?.deciderId).toBe('sm');
  });

  it('ASK_DELETE_THEME / THEME_DELETED', () => {
    const table = {
      themes: [
        { name: 'A', removed: false, questions: [] },
        { name: 'B', removed: false, questions: [] },
      ],
    };
    let s = reduceGame(base, ev('TABLE', table), ctx);
    s = reduceGame(s, ev('ASK_DELETE_THEME', { personId: 'p1', themes: [0, 1], durationMs: 30_000 }), ctx);
    expect(s.prompts.askDeleteTheme?.themes).toEqual([0, 1]);
    s = reduceGame(s, ev('THEME_DELETED', { themeIndex: 1, personId: 'p1' }), ctx);
    expect(s.table.themes[1]?.removed).toBe(true);
    expect(s.table.themes[0]?.removed).toBe(false);
    expect(s.prompts.askDeleteTheme).toBeUndefined();
  });

  it('timers: start / pause / resume / stop with local offset math', () => {
    let s = reduceGame(base, ev('TIMER_START', { id: 't1', kind: 'answering', durationMs: 25_000, personId: 'p1' }, 5000), ctx);
    expect(s.timers.t1).toEqual({ id: 't1', kind: 'answering', durationMs: 25_000, startedAtLocal: 5000, paused: false, remainingMs: 25_000, personId: 'p1' });
    s = reduceGame(s, ev('TIMER_PAUSE', { id: 't1', kind: 'answering', remainingMs: 20_000 }, 10_000), ctx);
    expect(s.timers.t1).toMatchObject({ paused: true, remainingMs: 20_000, durationMs: 25_000, personId: 'p1' });
    s = reduceGame(s, ev('TIMER_RESUME', { id: 't1', kind: 'answering', remainingMs: 20_000 }, 12_000), ctx);
    expect(s.timers.t1).toMatchObject({ paused: false, remainingMs: 20_000, durationMs: 25_000, startedAtLocal: 12_000 - 5000, personId: 'p1' });
    s = reduceGame(s, ev('TIMER_STOP', { id: 't1' }), ctx);
    expect(s.timers.t1).toBeUndefined();
    // Stopping an unknown timer is a no-op (same object).
    expect(reduceGame(s, ev('TIMER_STOP', { id: 'nope' }), ctx)).toBe(s);
    // A pause for an unknown timer creates a paused entry.
    s = reduceGame(s, ev('TIMER_PAUSE', { id: 't2', kind: 'round', remainingMs: 1000 }, 100), ctx);
    expect(s.timers.t2).toMatchObject({ paused: true, remainingMs: 1000, durationMs: 1000 });
    // A resume for an unknown timer uses remainingMs as the duration.
    s = reduceGame(s, ev('TIMER_RESUME', { id: 't3', kind: 'round', remainingMs: 400 }, 700), ctx);
    expect(s.timers.t3).toMatchObject({ paused: false, durationMs: 400, startedAtLocal: 700 });
    // Expired timers are pruned when a new one starts.
    s = reduceGame(s, ev('TIMER_START', { id: 't4', kind: 'content', durationMs: 10 }, 700 + 400 + 5000), ctx);
    expect(s.timers.t3).toBeUndefined();
    expect(s.timers.t2).toBeDefined(); // paused timers are never pruned
    expect(s.timers.t4).toBeDefined();
  });

  it('PAUSE / TOGGLE / ROUND_START / ROUND_END', () => {
    let s = reduceGame(base, ev('PAUSE', { on: true }), ctx);
    expect(s.paused).toBe(true);
    s = reduceGame(s, ev('TOGGLE', { themeIndex: 0, questionIndex: 0, active: false }), ctx);
    expect(s.table.themes[0]?.questions[0]?.removed).toBe(true);
    s = reduceGame(s, ev('TOGGLE', { themeIndex: 0, questionIndex: 0, active: true }), ctx);
    expect(s.table.themes[0]?.questions[0]?.removed).toBe(false);
    s = reduceGame(questionState(), ev('ROUND_START', { index: 1, name: 'Раунд 2', type: 'final', themes: ['A'] }), ctx);
    expect(s).toMatchObject({ roundIndex: 1, roundName: 'Раунд 2', roundType: 'final', question: null, pending: null });
    expect(activePrompts(s.prompts)).toEqual([]);
    s = reduceGame(base, ev('ROUND_END', { index: 0, reason: 'empty' }), ctx);
    expect(activePrompts(s.prompts)).toEqual([]);
  });

  it('WINNER / GAME_END', () => {
    let s = reduceGame(base, ev('WINNER', { personId: 'p2' }), ctx);
    expect(s.winnerId).toBe('p2');
    s = reduceGame(s, ev('WINNER', { personId: '' }), ctx);
    expect(s.winnerId).toBeNull();
    const end = { scores: [{ personId: 'p1', score: 900 }], winnerId: 'p1', statistics: { questionsPlayed: 3, players: [] } };
    s = reduceGame(s, ev('GAME_END', end), ctx);
    expect(s.ended).toBe(true);
    expect(s.gameEnd).toBe(end);
    expect(s.winnerId).toBe('p1');
    expect(s.scores).toEqual({ p1: 900 });
  });

  it('appeals: start, votes, result', () => {
    let s = reduceGame(base, ev('APPEAL_START', { kind: 'for', appellantId: 'p1', answererId: 'p1', answer: 'Rome', voters: ['p2', 'p3'], durationMs: 30_000 }), ctx);
    expect(s.appeal).toMatchObject({ kind: 'for', answererId: 'p1', answer: 'Rome', pending: ['p2', 'p3'], for: 0, against: 0 });
    s = reduceGame(s, ev('ASK_APPEAL_VOTE', { kind: 'for', answererId: 'p1', answer: 'Rome', rights: ['Paris'] }), ctx);
    expect(s.prompts.askAppealVote?.answer).toBe('Rome');
    s = reduceGame(s, ev('APPEAL_VOTE', { personId: 'p2', right: true }), ctx);
    expect(s.appeal).toMatchObject({ for: 1, against: 0, pending: ['p3'], votes: { p2: true } });
    expect(s.prompts.askAppealVote).toBeDefined();
    s = reduceGame(s, ev('APPEAL_VOTE', { personId: 'p1', right: false }), ctx);
    expect(s.prompts.askAppealVote).toBeUndefined();
    expect(s.appeal).toMatchObject({ for: 1, against: 1 });
    s = reduceGame(s, ev('APPEAL_RESULT', { kind: 'for', answererId: 'p1', accepted: true, for: 2, against: 1 }), ctx);
    expect(s.appeal).toBeNull();
    expect(s.prompts.askAppealVote).toBeUndefined();
  });

  it('OPTIONS stores rules and times; an unknown event returns the same state', () => {
    const s = reduceGame(base, ev('OPTIONS', { rules, times }), ctx);
    expect(s.rules).toBe(rules);
    expect(s.times).toBe(times);
    expect(reduceGame(base, ev('FINAL_THINK', { durationMs: 45_000 }), ctx)).toBe(base);
  });

  it('a first engine event on the lobby state flips hasGame', () => {
    expect(initialGameState.hasGame).toBe(false);
    const s = reduceGame(initialGameState, ev('STAGE', { stage: 'roundIntro', roundIndex: 0 }), ctx);
    expect(s.hasGame).toBe(true);
    expect(isEngineEvent('STAGE')).toBe(true);
    expect(isEngineEvent('CHAT')).toBe(false);
  });
});

describe('useGameStore', () => {
  beforeEach(() => {
    useGameStore.getState().reset();
  });

  it('apply ignores non-engine messages and reduces engine ones', () => {
    const before = useGameStore.getState().game;
    useGameStore.getState().apply({ t: 'CHAT', p: { personId: 'a', name: 'A', text: 'x', atMs: 1 }, tRecv: 1 } as never, ctx);
    expect(useGameStore.getState().game).toBe(before);
    useGameStore.getState().apply(ev('STAGE', { stage: 'selecting', roundIndex: 2 }), ctx);
    expect(useGameStore.getState().game.stage).toBe('selecting');
    expect(useGameStore.getState().game.roundIndex).toBe(2);
    expect(useGameStore.getState().game.hasGame).toBe(true);
  });

  it('applySnapshot resets to the authoritative state; reset returns to the lobby', () => {
    useGameStore.getState().apply(ev('STAGE', { stage: 'selecting', roundIndex: 2 }), ctx);
    useGameStore.getState().applySnapshot(snapshot({ stage: 'roundIntro', roundIndex: 0 }), ctx);
    expect(useGameStore.getState().game.stage).toBe('roundIntro');
    expect(useGameStore.getState().game.roundIndex).toBe(0);
    expect(useGameStore.getState().game.scores).toEqual({ p1: 100, p2: 200 });
    useGameStore.getState().applySnapshot(null, ctx);
    expect(useGameStore.getState().game.hasGame).toBe(false);
    useGameStore.getState().apply(ev('STAGE', { stage: 'selecting', roundIndex: 2 }), ctx);
    useGameStore.getState().reset();
    expect(useGameStore.getState().game).toBe(initialGameState);
  });
});
