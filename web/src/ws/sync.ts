/**
 * SyncController — clock synchronisation with the room (docs/protocol.md §6.1).
 *
 * Schedule: a burst (`syncPlan.burst` × `syncPlan.intervalMs`) right after
 * WELCOME and after every reconnect, then one SYNC every `syncPlan.steadyMs`;
 * a new burst when the tab becomes visible (after sending VISIBLE) and a short
 * burst of 5 when QUESTION_START arrives (the pre-arm burst).
 *
 * Samples are interleaved 4-tuples: the arrival time (c4) of SYNC_ACK n rides
 * inside SYNC n+1 as `prevSeq`/`prevC4`. `c4` is the `tRecv` stamped in the
 * socket's onmessage handler, before any parsing.
 *
 * `serverNow() = performance.now() + model.offsetMs`.
 */
import type { RoomConnection } from './connection.ts';
import type { ClockModel, SyncAckPayload, SyncPlan } from './types.ts';

export interface SyncOptions {
  now?: () => number;
  setTimeout?: (cb: () => void, ms: number) => unknown;
  clearTimeout?: (id: unknown) => void;
  onModel?: (model: ClockModel) => void;
}

export const DEFAULT_SYNC_PLAN: SyncPlan = { burst: 10, intervalMs: 100, steadyMs: 1000 };
export const PRE_ARM_BURST = 5;

export class SyncController {
  private seq = 0;
  private prev: { seq: number; c4: number } | null = null;
  private plan: SyncPlan = DEFAULT_SYNC_PLAN;
  private timer: unknown = null;
  private burstLeft = 0;
  private running = false;
  private readonly now: () => number;
  private readonly setTimeoutFn: (cb: () => void, ms: number) => unknown;
  private readonly clearTimeoutFn: (id: unknown) => void;
  private readonly onModel?: (model: ClockModel) => void;

  /** Latest public clock model from SYNC_ACK (null until the first ack). */
  model: ClockModel | null = null;
  /** Coarse offset taken from WELCOME.serverTimeMs, used until the model arrives. */
  private coarseOffsetMs: number | null = null;
  /** Number of SYNC_ACKs received on the current connection. */
  acks = 0;

  constructor(
    private readonly conn: RoomConnection,
    opts: SyncOptions = {},
  ) {
    this.now = opts.now ?? (() => performance.now());
    this.setTimeoutFn = opts.setTimeout ?? ((cb, ms) => setTimeout(cb, ms));
    this.clearTimeoutFn = opts.clearTimeout ?? ((id) => clearTimeout(id as ReturnType<typeof setTimeout>));
    this.onModel = opts.onModel;
  }

  /** Starts (or restarts) the schedule: called on WELCOME of every connection. */
  start(plan?: SyncPlan, welcome?: { serverTimeMs: number; tRecv: number }): void {
    if (plan) this.plan = plan;
    if (welcome) this.coarseOffsetMs = welcome.serverTimeMs - welcome.tRecv;
    // A new connection resets the server-side model and our interleaving state.
    this.prev = null;
    this.acks = 0;
    this.running = true;
    this.burst(this.plan.burst);
  }

  stop(): void {
    this.running = false;
    this.clearTimer();
  }

  /** Sends `n` SYNCs `intervalMs` apart starting now, then resumes the steady cadence. */
  burst(n: number = this.plan.burst): void {
    if (!this.running) return;
    this.burstLeft = Math.max(this.burstLeft, n);
    this.clearTimer();
    this.tick();
  }

  /** Pre-arm burst on QUESTION_START (docs/buzzer.md §10). */
  preArmBurst(): void {
    this.burst(PRE_ARM_BURST);
  }

  /** visibilitychange → visible: tell the server (model goes cold) and re-sync. */
  onVisible(): void {
    this.conn.send('VISIBLE', {});
    this.burst(this.plan.burst);
  }

  /** Feed SYNC_ACK here with the receive stamp from the envelope. */
  onAck(p: SyncAckPayload, tRecv: number): void {
    // Stamp the arrival first (this is c4 of the tuple), before any other work.
    this.prev = { seq: p.seq, c4: tRecv };
    this.model = p.model;
    this.acks += 1;
    this.onModel?.(p.model);
  }

  /** Server monotonic time estimated on the local clock. */
  serverNow(): number {
    return this.now() + this.offsetMs();
  }

  /** Best known offset (server − client), model first, WELCOME coarse offset as fallback. */
  offsetMs(): number {
    if (this.model && this.model.samples > 0) return this.model.offsetMs;
    return this.coarseOffsetMs ?? 0;
  }

  /** Converts a server monotonic timestamp to the local performance.now() scale. */
  toLocal(serverMs: number): number {
    return serverMs - this.offsetMs();
  }

  toServer(localMs: number): number {
    return localMs + this.offsetMs();
  }

  private tick(): void {
    if (!this.running) return;
    this.sendSync();
    const inBurst = this.burstLeft > 0;
    if (inBurst) this.burstLeft -= 1;
    const delay = inBurst && this.burstLeft > 0 ? this.plan.intervalMs : this.plan.steadyMs;
    this.timer = this.setTimeoutFn(() => {
      this.timer = null;
      this.tick();
    }, delay);
  }

  private sendSync(): void {
    if (!this.conn.isOpen) return;
    const msg: { seq: number; c1: number; prevSeq?: number; prevC4?: number } = {
      seq: ++this.seq,
      c1: this.now(),
    };
    if (this.prev) {
      msg.prevSeq = this.prev.seq;
      msg.prevC4 = this.prev.c4;
      // Each ack is reported exactly once.
      this.prev = null;
    }
    this.conn.send('SYNC', msg);
  }

  private clearTimer(): void {
    if (this.timer !== null) {
      this.clearTimeoutFn(this.timer);
      this.timer = null;
    }
  }
}
