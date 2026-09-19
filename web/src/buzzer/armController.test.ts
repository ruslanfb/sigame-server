import { beforeEach, describe, expect, it, vi } from 'vitest';
import { FakeClock } from '../test/fakeClock.ts';
import type { ButtonArmPayload, ButtonResultPayload } from '../ws/types.ts';
import { ArmController, pressInstant } from './armController.ts';

/** Server monotonic = local + OFFSET (the sync model's offsetMs). */
const OFFSET = 100_000;
const toLocal = (ms: number) => ms - OFFSET;
const toServer = (ms: number) => ms + OFFSET;

interface Sent {
  t: string;
  p: unknown;
}

function setup(myId = 'me') {
  const clock = new FakeClock(1000);
  const sent: Sent[] = [];
  const onLight = vi.fn();
  const onChange = vi.fn();
  const arm = new ArmController({
    send: (t, p) => {
      sent.push({ t, p });
    },
    now: clock.now,
    setTimeout: clock.setTimeout,
    clearTimeout: clock.clearTimeout,
    raf: clock.raf,
    toLocal,
    toServer,
    myId: () => myId,
    onChange,
    onLight,
  });
  return { clock, sent, arm, onLight, onChange };
}

function armPayload(clock: FakeClock, over: Partial<ButtonArmPayload> = {}): ButtonArmPayload {
  const armAtLocal = clock.peek() + 500;
  return {
    armId: 'arm-1',
    armAt: toServer(armAtLocal),
    armAtLocal,
    mode: 'scheduled',
    deadlineAt: toServer(armAtLocal + 5000),
    lockoutMs: 1500,
    ...over,
  };
}

const trusted = (src: 'pointer' | 'key' = 'pointer', timeStamp?: number) => ({ isTrusted: true, src, timeStamp });

describe('ArmController', () => {
  let s: ReturnType<typeof setup>;
  beforeEach(() => {
    s = setup();
  });

  it('acknowledges BUTTON_ARM immediately with recvLocal = the receive instant', () => {
    const before = s.clock.peek();
    s.arm.onButtonArm(armPayload(s.clock));
    expect(s.sent).toHaveLength(1);
    const ack = s.sent[0]!;
    expect(ack.t).toBe('ARM_ACK');
    const p = ack.p as { armId: string; recvLocal: number };
    expect(p.armId).toBe('arm-1');
    expect(p.recvLocal).toBeCloseTo(before, 0);
    expect(p.recvLocal).toBeGreaterThanOrEqual(before);
    expect(s.arm.current.phase).toBe('armed');
    expect(s.arm.current.armId).toBe('arm-1');
    expect(s.arm.current.litLocal).toBe(0);
  });

  it('scheduled: lights within ±2 ms of armAtLocal, once', () => {
    const p = armPayload(s.clock);
    s.arm.onButtonArm(p);
    // Well before the light nothing happens.
    s.clock.advance(400);
    expect(s.arm.current.phase).toBe('armed');
    expect(s.onLight).not.toHaveBeenCalled();
    s.clock.advance(200);
    expect(s.arm.current.phase).toBe('lit');
    expect(Math.abs(s.arm.current.litLocal - p.armAtLocal)).toBeLessThanOrEqual(2);
    expect(s.arm.current.litLocal).toBeGreaterThanOrEqual(p.armAtLocal);
    expect(s.onLight).toHaveBeenCalledTimes(1);
    s.clock.advance(1000);
    expect(s.onLight).toHaveBeenCalledTimes(1);
  });

  it('scheduled with armAtLocal already in the past lights at once', () => {
    s.arm.onButtonArm(armPayload(s.clock, { armAtLocal: s.clock.peek() - 10 }));
    expect(s.arm.current.phase).toBe('lit');
    expect(s.onLight).toHaveBeenCalledTimes(1);
  });

  it('onReceipt: lights immediately', () => {
    const before = s.clock.peek();
    s.arm.onButtonArm(armPayload(s.clock, { mode: 'onReceipt' }));
    expect(s.arm.current.phase).toBe('lit');
    expect(s.arm.current.litLocal).toBeCloseTo(before, 0);
    expect(s.onLight).toHaveBeenCalledTimes(1);
    expect(s.sent[0]!.t).toBe('ARM_ACK');
  });

  it('press before the light → MISFIRE and a local lockout', () => {
    const p = armPayload(s.clock);
    s.arm.onButtonArm(p);
    s.clock.advance(100);
    const before = s.clock.peek();
    expect(s.arm.press(trusted())).toBe('misfire');
    expect(s.sent.map((m) => m.t)).toEqual(['ARM_ACK', 'MISFIRE']);
    expect(s.sent[1]!.p).toEqual({ armId: 'arm-1' });
    const st = s.arm.current;
    expect(st.phase).toBe('locked');
    expect(st.lockoutReason).toBe('misfire');
    expect(st.lockedUntilLocal).toBeCloseTo(before + p.lockoutMs, 0);
    expect(st.pressSeq).toBe(0);
    // Still locked while the lockout runs: no PRESS goes out.
    expect(s.arm.press(trusted())).toBe('ignored');
    expect(s.sent).toHaveLength(2);
  });

  it('press after the light → one PRESS with {armId, seq, pressLocal, litLocal, src}', () => {
    const p = armPayload(s.clock);
    s.arm.onButtonArm(p);
    s.clock.advance(600);
    expect(s.arm.current.phase).toBe('lit');
    const litLocal = s.arm.current.litLocal;
    s.clock.advance(200);
    const ts = s.clock.peek() - 30; // a recent, trusted event timeStamp
    expect(s.arm.press(trusted('pointer', ts))).toBe('press');
    expect(s.sent).toHaveLength(2);
    expect(s.sent[1]!.t).toBe('PRESS');
    expect(s.sent[1]!.p).toEqual({ armId: 'arm-1', seq: 1, pressLocal: ts, litLocal, src: 'pointer' });
    const st = s.arm.current;
    expect(st.phase).toBe('pressed');
    expect(st.pressSeq).toBe(1);
    expect(st.pressLocal).toBe(ts);
    expect(st.pressSrc).toBe('pointer');
    // Only one press per arm.
    expect(s.arm.press(trusted('key', s.clock.peek()))).toBe('ignored');
    expect(s.sent).toHaveLength(2);
  });

  it('press uses now() when the event timeStamp is stale or missing', () => {
    s.arm.onButtonArm(armPayload(s.clock, { mode: 'onReceipt' }));
    s.clock.advance(300);
    const stale = s.clock.peek() - 500;
    const before = s.clock.peek();
    expect(s.arm.press(trusted('key', stale))).toBe('press');
    const p = s.sent[1]!.p as { pressLocal: number; src: string };
    expect(p.pressLocal).not.toBe(stale);
    expect(p.pressLocal).toBeCloseTo(before, 0);
    expect(p.src).toBe('key');
  });

  it('pressInstant mirrors the controller rule', () => {
    expect(pressInstant(1000, undefined)).toBe(1000);
    expect(pressInstant(1000, 990)).toBe(990);
    expect(pressInstant(1000, 940)).toBe(940);
    expect(pressInstant(1000, 900)).toBe(1000);
    expect(pressInstant(1000, 1010)).toBe(1000);
  });

  it('ignores untrusted events and presses without an arm', () => {
    expect(s.arm.press(trusted())).toBe('ignored');
    s.arm.onButtonArm(armPayload(s.clock, { mode: 'onReceipt' }));
    expect(s.arm.press({ isTrusted: false, src: 'pointer' })).toBe('ignored');
    expect(s.sent.map((m) => m.t)).toEqual(['ARM_ACK']);
  });

  it('PRESS_ACK pending keeps the pressed phase and stores the ack', () => {
    s.arm.onButtonArm(armPayload(s.clock, { mode: 'onReceipt' }));
    s.arm.press(trusted());
    s.arm.onPressAck({ armId: 'arm-1', status: 'pending', reactionMs: 231 });
    expect(s.arm.current.phase).toBe('pressed');
    expect(s.arm.current.ack).toEqual({ armId: 'arm-1', status: 'pending', reactionMs: 231 });
    // An ack for another arm is ignored.
    s.arm.onPressAck({ armId: 'other', status: 'late' });
    expect(s.arm.current.ack?.status).toBe('pending');
  });

  it('PRESS_ACK falseStart locks until toLocal(lockoutUntil)', () => {
    s.arm.onButtonArm(armPayload(s.clock, { mode: 'onReceipt' }));
    s.arm.press(trusted());
    const lockoutUntil = toServer(s.clock.peek() + 2000);
    s.arm.onPressAck({ armId: 'arm-1', status: 'falseStart', lockoutUntil });
    expect(s.arm.current.phase).toBe('locked');
    expect(s.arm.current.lockoutReason).toBe('falseStart');
    expect(s.arm.current.lockedUntilLocal).toBe(toLocal(lockoutUntil));
  });

  it('BUTTON_RESULT resolves the arm; a result for another arm is ignored', () => {
    s.arm.onButtonArm(armPayload(s.clock, { mode: 'onReceipt' }));
    s.arm.press(trusted());
    const other: ButtonResultPayload = { armId: 'arm-9', kind: 'nobody', contested: false, marginMs: 0, rule: 'x' };
    s.arm.onButtonResult(other);
    expect(s.arm.current.phase).toBe('pressed');
    expect(s.arm.current.result).toBeNull();
    const res: ButtonResultPayload = {
      armId: 'arm-1',
      winnerId: 'me',
      kind: 'winner',
      contested: false,
      marginMs: 41,
      rule: 'single',
      reactionMs: 231,
      source: 'client',
    };
    s.arm.onButtonResult(res);
    expect(s.arm.current.phase).toBe('resolved');
    expect(s.arm.current.result).toEqual(res);
    expect(s.arm.press(trusted())).toBe('ignored');
  });

  it('PRESS_PENDING collects players once', () => {
    s.arm.onButtonArm(armPayload(s.clock, { mode: 'onReceipt' }));
    s.arm.onPressPending({ armId: 'arm-1', playerId: 'a' });
    s.arm.onPressPending({ armId: 'arm-1', playerId: 'a' });
    s.arm.onPressPending({ armId: 'arm-1', playerId: 'b' });
    s.arm.onPressPending({ armId: 'other', playerId: 'c' });
    expect(s.arm.current.pendingPlayers).toEqual(['a', 'b']);
  });

  it('LOCKOUT for me locks; for someone else only records lastLockout', () => {
    s.arm.onButtonArm(armPayload(s.clock, { mode: 'onReceipt' }));
    expect(s.arm.current.phase).toBe('lit');
    const theirs = { playerId: 'them', untilAt: toServer(s.clock.peek() + 1000), durationMs: 1000, reason: 'tooEarly' as const };
    s.arm.onLockout(theirs);
    expect(s.arm.current.phase).toBe('lit');
    expect(s.arm.current.lastLockout).toEqual(theirs);
    expect(s.arm.current.lockedUntilLocal).toBe(0);

    const untilAt = toServer(s.clock.peek() + 1000);
    s.arm.onLockout({ playerId: 'me', untilAt, durationMs: 1000, reason: 'falseStart' });
    expect(s.arm.current.phase).toBe('locked');
    expect(s.arm.current.lockedUntilLocal).toBe(toLocal(untilAt));
    expect(s.arm.current.lockoutReason).toBe('falseStart');
    expect(s.arm.press(trusted())).toBe('ignored');
  });

  it('lockout expiry returns to the underlying phase (lit)', () => {
    s.arm.onButtonArm(armPayload(s.clock, { mode: 'onReceipt' }));
    const untilAt = toServer(s.clock.peek() + 1000);
    s.arm.onLockout({ playerId: 'me', untilAt, durationMs: 1000, reason: 'tooEarly' });
    expect(s.arm.current.phase).toBe('locked');
    s.clock.advance(1100);
    expect(s.arm.current.phase).toBe('lit');
    expect(s.arm.current.lockedUntilLocal).toBe(0);
    expect(s.arm.press(trusted())).toBe('press');
  });

  it('disarm() → idle, or locked while a lockout is still running', () => {
    s.arm.onButtonArm(armPayload(s.clock));
    s.arm.disarm();
    expect(s.arm.current.phase).toBe('idle');
    expect(s.arm.current.armId).toBeNull();
    // The scheduled light must not fire after a disarm.
    s.clock.advance(1000);
    expect(s.onLight).not.toHaveBeenCalled();

    s.arm.onButtonArm(armPayload(s.clock, { armId: 'arm-2' }));
    s.arm.press(trusted()); // misfire → lockout
    expect(s.arm.current.phase).toBe('locked');
    s.arm.disarm();
    expect(s.arm.current.phase).toBe('locked');
    expect(s.arm.current.armId).toBeNull();
    s.clock.advance(2000);
    expect(s.arm.current.phase).toBe('idle');
  });

  it('a passed deadline disarms an unpressed arm', () => {
    const p = armPayload(s.clock);
    s.arm.onButtonArm(p);
    s.clock.advance(600);
    expect(s.arm.current.phase).toBe('lit');
    s.clock.advance(4000);
    expect(s.arm.current.phase).toBe('lit');
    s.clock.advance(1000);
    expect(s.arm.current.phase).toBe('disarmed');
    expect(s.arm.press(trusted())).toBe('ignored');
  });

  it('a new BUTTON_ARM replaces the previous arm and keeps the lockout', () => {
    s.arm.onButtonArm(armPayload(s.clock, { mode: 'onReceipt' }));
    const untilAt = toServer(s.clock.peek() + 5000);
    s.arm.onLockout({ playerId: 'me', untilAt, durationMs: 5000, reason: 'falseStart' });
    s.arm.onButtonArm(armPayload(s.clock, { armId: 'arm-2', mode: 'onReceipt' }));
    expect(s.arm.current.armId).toBe('arm-2');
    expect(s.arm.current.phase).toBe('locked');
    expect(s.arm.current.lockedUntilLocal).toBe(toLocal(untilAt));
    expect(s.sent.filter((m) => m.t === 'ARM_ACK')).toHaveLength(2);
  });

  it('onSnapshot with an armed state and a past armAt lights immediately', () => {
    const before = s.clock.peek();
    s.arm.onSnapshot({
      state: 'armed',
      armId: 'arm-s',
      armAt: toServer(before - 100),
      deadlineAt: toServer(before + 4000),
      lockedUntil: 0,
    });
    expect(s.arm.current.phase).toBe('lit');
    expect(s.arm.current.armId).toBe('arm-s');
    expect(s.arm.current.mode).toBe('onReceipt');
    expect(s.arm.current.litLocal).toBeCloseTo(before, 0);
    expect(s.onLight).toHaveBeenCalledTimes(1);
    // No ARM_ACK for a snapshot (nothing to acknowledge).
    expect(s.sent).toHaveLength(0);
    expect(s.arm.press(trusted())).toBe('press');
  });

  it('onSnapshot with a future armAt schedules the light; idle snapshot keeps a running lockout', () => {
    const before = s.clock.peek();
    s.arm.onSnapshot({ state: 'armed', armId: 'arm-s', armAt: toServer(before + 300), deadlineAt: toServer(before + 4000) });
    expect(s.arm.current.phase).toBe('armed');
    s.clock.advance(400);
    expect(s.arm.current.phase).toBe('lit');
    expect(Math.abs(s.arm.current.litLocal - (before + 300))).toBeLessThanOrEqual(2);

    const lockedUntil = toServer(s.clock.peek() + 1000);
    s.arm.onSnapshot({ state: 'idle', lockedUntil });
    expect(s.arm.current.phase).toBe('idle');
    expect(s.arm.current.armId).toBeNull();
    expect(s.arm.current.lockedUntilLocal).toBe(toLocal(lockedUntil));
    expect(s.arm.press(trusted())).toBe('ignored');
  });

  it('dispose() cancels every pending timer', () => {
    s.arm.onButtonArm(armPayload(s.clock));
    expect(s.clock.pending()).toBeGreaterThan(0);
    s.arm.dispose();
    expect(s.clock.pending()).toBe(0);
    s.clock.advance(1000);
    expect(s.onLight).not.toHaveBeenCalled();
  });

  it('emits onChange for every transition', () => {
    s.arm.onButtonArm(armPayload(s.clock, { mode: 'onReceipt' }));
    const calls = s.onChange.mock.calls.length;
    expect(calls).toBeGreaterThanOrEqual(1);
    s.arm.press(trusted());
    expect(s.onChange.mock.calls.length).toBe(calls + 1);
    expect(s.onChange.mock.lastCall?.[0]).toMatchObject({ phase: 'pressed', pressSeq: 1 });
  });

  it('misfire before the light: the light is still stamped, and a press after the lockout is a normal PRESS', () => {
    const p = armPayload(s.clock, { lockoutMs: 300 });
    s.arm.onButtonArm(p);
    s.clock.advance(300); // misfire at +300, lockout until +600 — after the light at +500
    expect(s.arm.press(trusted())).toBe('misfire');
    expect(s.arm.current.phase).toBe('locked');
    // The light fires while locked: litLocal is stamped, the phase stays locked.
    s.clock.advance(250);
    expect(s.arm.current.litLocal).toBeGreaterThan(0);
    expect(s.arm.current.litLocal).toBeCloseTo(p.armAtLocal, 0);
    expect(s.arm.current.phase).toBe('locked');
    expect(s.arm.press(trusted())).toBe('ignored');
    // After the lockout the phase is lit and the player can press normally.
    s.clock.advance(100);
    expect(s.arm.current.phase).toBe('lit');
    expect(s.sent.filter((m) => m.t === 'PRESS')).toHaveLength(0);
    expect(s.arm.press(trusted())).toBe('press');
    expect(s.sent.filter((m) => m.t === 'PRESS')).toHaveLength(1);
    expect(s.sent.filter((m) => m.t === 'MISFIRE')).toHaveLength(1);
  });
});
