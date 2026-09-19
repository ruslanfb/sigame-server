import { describe, expect, it, vi } from 'vitest';
import { FakeClock } from '../test/fakeClock.ts';
import type { RoomConnection } from './connection.ts';
import { DEFAULT_SYNC_PLAN, PRE_ARM_BURST, SyncController } from './sync.ts';
import type { ClockModel, SyncIn } from './types.ts';

const PLAN = { burst: 10, intervalMs: 100, steadyMs: 1000 };

function model(over: Partial<ClockModel> = {}): ClockModel {
  return {
    offsetMs: 49_010,
    rttRefMs: 12,
    rttWsMs: 11,
    rttKernelMs: 10,
    sigmaMs: 1,
    tolMs: 40,
    uMs: 3,
    leadMs: 60,
    quality: 'good',
    samples: 5,
    ...over,
  };
}

function setup() {
  const clock = new FakeClock(1000);
  const send = vi.fn(() => 1);
  const conn = { isOpen: true, send } as unknown as RoomConnection;
  const onModel = vi.fn();
  const sync = new SyncController(conn, {
    now: clock.now,
    setTimeout: clock.setTimeout,
    clearTimeout: clock.clearTimeout,
    onModel,
  });
  /** SYNC payloads sent so far, in order. */
  const syncs = (): SyncIn[] =>
    send.mock.calls.filter((c: unknown[]) => c[0] === 'SYNC').map((c: unknown[]) => c[1] as SyncIn);
  const types = (): string[] => send.mock.calls.map((c: unknown[]) => c[0] as string);
  return { clock, send, sync, onModel, syncs, types };
}

describe('SyncController', () => {
  it('exports the documented defaults', () => {
    expect(DEFAULT_SYNC_PLAN).toEqual({ burst: 10, intervalMs: 100, steadyMs: 1000 });
    expect(PRE_ARM_BURST).toBe(5);
  });

  it('start: burst of 10 × 100 ms, then one SYNC every second', () => {
    const s = setup();
    const t0 = s.clock.peek();
    s.sync.start(PLAN);
    expect(s.syncs()).toHaveLength(1);
    expect(s.syncs()[0]!.seq).toBe(1);
    expect(s.syncs()[0]!.c1).toBeCloseTo(t0, 0);
    s.clock.advance(950);
    const burst = s.syncs();
    expect(burst).toHaveLength(10);
    expect(burst.map((m) => m.seq)).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9, 10]);
    for (let i = 1; i < burst.length; i++) {
      expect(burst[i]!.c1 - burst[i - 1]!.c1).toBeCloseTo(100, 0);
    }
    // Steady cadence: the 11th goes 1000 ms after the 10th.
    s.clock.advance(1000);
    expect(s.syncs()).toHaveLength(11);
    expect(s.syncs()[10]!.c1 - burst[9]!.c1).toBeCloseTo(1000, 0);
    s.clock.advance(1000);
    expect(s.syncs()).toHaveLength(12);
    // c1 is always the clock at send time.
    for (const m of s.syncs()) expect(m.c1).toBeGreaterThanOrEqual(t0);
  });

  it('preArmBurst: 5 × 100 ms then back to steady', () => {
    const s = setup();
    s.sync.start(PLAN);
    s.clock.advance(3000); // burst done, steady running
    const n = s.syncs().length;
    s.sync.preArmBurst();
    expect(s.syncs()).toHaveLength(n + 1);
    s.clock.advance(450);
    expect(s.syncs()).toHaveLength(n + 5);
    const last5 = s.syncs().slice(-5);
    for (let i = 1; i < 5; i++) expect(last5[i]!.c1 - last5[i - 1]!.c1).toBeCloseTo(100, 0);
    s.clock.advance(500);
    expect(s.syncs()).toHaveLength(n + 5);
    s.clock.advance(600);
    expect(s.syncs()).toHaveLength(n + 6);
  });

  it('onAck: the next SYNC carries prevSeq/prevC4 = receive time of the ack, exactly once', () => {
    const s = setup();
    s.sync.start(PLAN);
    expect(s.syncs()[0]).not.toHaveProperty('prevSeq');
    const tRecv = 1042.5;
    s.sync.onAck({ seq: 1, c1: 1000, s2: 50_000, s3: 50_001, model: model() }, tRecv);
    s.clock.advance(100);
    const second = s.syncs()[1]!;
    expect(second.seq).toBe(2);
    expect(second.prevSeq).toBe(1);
    expect(second.prevC4).toBe(tRecv);
    s.clock.advance(100);
    const third = s.syncs()[2]!;
    expect(third.seq).toBe(3);
    expect(third).not.toHaveProperty('prevSeq');
    expect(third).not.toHaveProperty('prevC4');
    // Only the latest unreported ack rides along.
    s.sync.onAck({ seq: 2, c1: 1100, s2: 0, s3: 0, model: model() }, 1150);
    s.sync.onAck({ seq: 3, c1: 1200, s2: 0, s3: 0, model: model() }, 1250);
    s.clock.advance(100);
    const fourth = s.syncs()[3]!;
    expect(fourth.prevSeq).toBe(3);
    expect(fourth.prevC4).toBe(1250);
  });

  it('stores the model, counts acks and notifies onModel', () => {
    const s = setup();
    s.sync.start(PLAN);
    expect(s.sync.model).toBeNull();
    expect(s.sync.acks).toBe(0);
    const m = model({ samples: 7 });
    s.sync.onAck({ seq: 1, c1: 1000, s2: 0, s3: 0, model: m }, 1050);
    expect(s.sync.model).toEqual(m);
    expect(s.sync.acks).toBe(1);
    expect(s.onModel).toHaveBeenCalledTimes(1);
    expect(s.onModel).toHaveBeenCalledWith(m);
    // A restart (new connection) resets the counter and the interleaving.
    s.sync.start(PLAN);
    expect(s.sync.acks).toBe(0);
    expect(s.syncs().at(-1)).not.toHaveProperty('prevSeq');
  });

  it('offsetMs: model when samples > 0, else the WELCOME coarse offset', () => {
    const s = setup();
    expect(s.sync.offsetMs()).toBe(0);
    s.sync.start(PLAN, { serverTimeMs: 50_000, tRecv: 1000 });
    expect(s.sync.offsetMs()).toBe(49_000);
    s.sync.onAck({ seq: 1, c1: 0, s2: 0, s3: 0, model: model({ offsetMs: 49_010, samples: 0 }) }, 1050);
    expect(s.sync.offsetMs()).toBe(49_000);
    s.sync.onAck({ seq: 2, c1: 0, s2: 0, s3: 0, model: model({ offsetMs: 49_010, samples: 3 }) }, 1150);
    expect(s.sync.offsetMs()).toBe(49_010);
    // toLocal / toServer round-trip on the model offset.
    expect(s.sync.toLocal(s.sync.toServer(1234.5))).toBeCloseTo(1234.5, 6);
    expect(s.sync.toServer(2000)).toBe(51_010);
    expect(s.sync.toLocal(51_010)).toBe(2000);
    // serverNow = now + offset.
    const before = s.clock.peek();
    expect(s.sync.serverNow()).toBeCloseTo(before + 49_010, 0);
  });

  it('onVisible: sends VISIBLE then a fresh burst', () => {
    const s = setup();
    s.sync.start(PLAN);
    s.clock.advance(3000);
    const before = s.types().length;
    s.sync.onVisible();
    const after = s.types().slice(before);
    expect(after[0]).toBe('VISIBLE');
    expect(after[1]).toBe('SYNC');
    s.clock.advance(950);
    expect(s.types().slice(before).filter((t) => t === 'SYNC')).toHaveLength(10);
  });

  it('stop() cancels the schedule; sends nothing while the socket is closed', () => {
    const s = setup();
    s.sync.start(PLAN);
    expect(s.clock.pending()).toBe(1);
    s.sync.stop();
    expect(s.clock.pending()).toBe(0);
    const n = s.syncs().length;
    s.clock.advance(5000);
    expect(s.syncs()).toHaveLength(n);
    // burst() after stop is a no-op.
    s.sync.burst();
    expect(s.syncs()).toHaveLength(n);

    // A closed socket: the tick keeps the cadence but sends nothing.
    (s.send as ReturnType<typeof vi.fn>).mockClear();
    (s.sync as unknown as { conn: { isOpen: boolean } }).conn.isOpen = false;
    s.sync.start(PLAN);
    s.clock.advance(500);
    expect(s.syncs()).toHaveLength(0);
  });
});
