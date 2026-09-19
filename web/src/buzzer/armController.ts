/**
 * ArmController — the fairness-critical client side of the buzzer
 * (docs/protocol.md §6.2–6.4, docs/buzzer.md §10).
 *
 * State machine:
 *   idle → armed(waiting) → lit → pressed(pending) → resolved
 *                └────────────┴──────── locked / disarmed
 *
 * Rules implemented here:
 * - BUTTON_ARM: immediately ARM_ACK{armId, recvLocal}; `scheduled` → light at
 *   armAtLocal via setTimeout(delay − 4) then a rAF/spin wait until
 *   `now() >= armAtLocal`; `onReceipt` (or armAtLocal already past) → light now.
 *   `litLocal` is stamped at the instant the light is shown.
 * - Press: `pressLocal` is the trusted event timeStamp when it is recent
 *   (0..60 ms old), else `now()`. Not lit → MISFIRE + local lockout. Lit → send
 *   PRESS BEFORE any state/render work. One PRESS per arm.
 * - PRESS_ACK / PRESS_PENDING / BUTTON_RESULT / LOCKOUT / disarm / deadline
 *   update the state; server `untilAt`/`lockoutUntil` are converted to the
 *   local clock with the sync offset.
 */
import type {
  ButtonArmPayload,
  ButtonResultPayload,
  BuzzerSnapshot,
  ClientMessages,
  ClientMessageType,
  LockoutPayload,
  PressAckPayload,
  PressPendingPayload,
} from '../ws/types.ts';

export type ArmPhase = 'idle' | 'armed' | 'lit' | 'pressed' | 'resolved' | 'locked' | 'disarmed';

export interface ArmState {
  phase: ArmPhase;
  armId: string | null;
  mode: ButtonArmPayload['mode'] | null;
  /** Planned light instant on the local clock (scheduled mode). */
  armAtLocal: number | null;
  /** Server time when the arm closes. */
  deadlineAt: number | null;
  lockoutMs: number;
  /** Instant the light was actually shown (local clock), 0 = not lit. */
  litLocal: number;
  /** Client press seq for this arm (0 = not pressed). */
  pressSeq: number;
  pressLocal: number;
  pressSrc: 'pointer' | 'key' | null;
  ack: PressAckPayload | null;
  /** Players whose press entered the fairness window (from PRESS_PENDING). */
  pendingPlayers: string[];
  result: ButtonResultPayload | null;
  /** Local-clock instant until which this player is locked out (0 = none). */
  lockedUntilLocal: number;
  lockoutReason: string | null;
  /** How the current lockout came about, for UI copy. */
  lastLockout: LockoutPayload | null;
}

export interface PressEventLike {
  isTrusted: boolean;
  timeStamp?: number;
  src: 'pointer' | 'key';
}

export interface ArmControllerDeps {
  send: <T extends ClientMessageType>(t: T, payload: ClientMessages[T]) => void;
  now: () => number;
  setTimeout: (cb: () => void, ms: number) => unknown;
  clearTimeout: (id: unknown) => void;
  /** requestAnimationFrame-like scheduler; falls back to setTimeout(0). */
  raf?: (cb: () => void) => void;
  /** Server monotonic → local clock. */
  toLocal: (serverMs: number) => number;
  /** Local → server monotonic. */
  toServer: (localMs: number) => number;
  onChange?: (state: ArmState) => void;
  /** Called at the instant the light is shown (beep). */
  onLight?: () => void;
  /** My personId, to recognise LOCKOUT broadcasts addressed to me. */
  myId: () => string | null;
}

export const initialArmState: ArmState = {
  phase: 'idle',
  armId: null,
  mode: null,
  armAtLocal: null,
  deadlineAt: null,
  lockoutMs: 0,
  litLocal: 0,
  pressSeq: 0,
  pressLocal: 0,
  pressSrc: null,
  ack: null,
  pendingPlayers: [],
  result: null,
  lockedUntilLocal: 0,
  lockoutReason: null,
  lastLockout: null,
};

/** Coarse setTimeout lead before the precise wait (ms). */
const TIMER_LEAD_MS = 4;
/** Below this remaining time we busy-wait instead of yielding to rAF. */
const SPIN_THRESHOLD_MS = 6;
/** Safety cap for the busy-wait (a stalled clock must not freeze the tab). */
const SPIN_CAP_MS = 25;
/** Max age of an event timeStamp to trust it as the press instant. */
const MAX_EVENT_AGE_MS = 60;

export class ArmController {
  private state: ArmState = { ...initialArmState };
  private lightTimer: unknown = null;
  private deadlineTimer: unknown = null;
  private lockoutTimer: unknown = null;
  private rafActive = false;
  private disposed = false;
  private readonly deps: ArmControllerDeps;

  constructor(deps: ArmControllerDeps) {
    this.deps = deps;
  }

  get current(): ArmState {
    return this.state;
  }

  // ---- inbound ---------------------------------------------------------------

  onButtonArm(p: ButtonArmPayload): void {
    const recvLocal = this.deps.now();
    // Acknowledge at once: the server measures ARM delivery from this.
    this.deps.send('ARM_ACK', { armId: p.armId, recvLocal });
    this.clearArmTimers();
    const lockedUntilLocal = this.state.lockedUntilLocal;
    this.state = {
      ...initialArmState,
      phase: 'armed',
      armId: p.armId,
      mode: p.mode,
      armAtLocal: p.mode === 'scheduled' ? p.armAtLocal : recvLocal,
      deadlineAt: p.deadlineAt,
      lockoutMs: p.lockoutMs,
      lockedUntilLocal,
      lastLockout: this.state.lastLockout,
    };
    this.scheduleDeadline(p.deadlineAt);
    if (p.mode === 'onReceipt') {
      this.light();
      return;
    }
    const delay = p.armAtLocal - recvLocal;
    if (delay <= 0) {
      this.light();
      return;
    }
    const armAtLocal = p.armAtLocal;
    this.lightTimer = this.deps.setTimeout(
      () => {
        this.lightTimer = null;
        this.preciseWait(armAtLocal);
      },
      Math.max(0, delay - TIMER_LEAD_MS),
    );
    this.emit();
  }

  /** Reconnect: the snapshot may carry an active arm without armAtLocal. */
  onSnapshot(b: BuzzerSnapshot): void {
    this.clearArmTimers();
    let lockedUntilLocal = 0;
    if (b.lockedUntil && b.lockedUntil > 0) {
      const local = this.deps.toLocal(b.lockedUntil);
      if (local > this.deps.now()) lockedUntilLocal = local;
    }
    if (b.state === 'armed' && b.armId) {
      const armAtLocal = b.armAtLocal && b.armAtLocal > 0 ? b.armAtLocal : this.deps.toLocal(b.armAt ?? 0);
      this.state = {
        ...initialArmState,
        phase: 'armed',
        armId: b.armId,
        mode: 'onReceipt',
        armAtLocal,
        deadlineAt: b.deadlineAt ?? null,
        lockoutMs: this.state.lockoutMs,
        lockedUntilLocal,
      };
      if (b.deadlineAt) this.scheduleDeadline(b.deadlineAt);
      // The clock model was reset: a past armAt means "light now, scored on receipt".
      if (armAtLocal <= this.deps.now()) this.light();
      else {
        this.lightTimer = this.deps.setTimeout(
          () => {
            this.lightTimer = null;
            this.preciseWait(armAtLocal);
          },
          Math.max(0, armAtLocal - this.deps.now() - TIMER_LEAD_MS),
        );
        this.emit();
      }
      return;
    }
    this.state = { ...initialArmState, lockedUntilLocal, lastLockout: this.state.lastLockout };
    if (lockedUntilLocal) this.scheduleLockoutEnd();
    this.emit();
  }

  onPressAck(p: PressAckPayload): void {
    if (p.armId !== this.state.armId) return;
    let lockedUntilLocal = this.state.lockedUntilLocal;
    let phase = this.state.phase;
    if (p.lockoutUntil && p.lockoutUntil > 0) {
      lockedUntilLocal = Math.max(lockedUntilLocal, this.deps.toLocal(p.lockoutUntil));
    }
    if (p.status === 'falseStart' || p.status === 'tooEarly' || p.status === 'lockedOut') {
      phase = 'locked';
    }
    this.state = { ...this.state, ack: p, phase, lockedUntilLocal, lockoutReason: p.status };
    if (phase === 'locked') this.scheduleLockoutEnd();
    this.emit();
  }

  onPressPending(p: PressPendingPayload): void {
    if (p.armId !== this.state.armId) return;
    if (this.state.pendingPlayers.includes(p.playerId)) return;
    this.state = { ...this.state, pendingPlayers: [...this.state.pendingPlayers, p.playerId] };
    this.emit();
  }

  onButtonResult(p: ButtonResultPayload): void {
    if (this.state.armId && p.armId !== this.state.armId) return;
    this.clearArmTimers();
    this.state = { ...this.state, phase: 'resolved', result: p };
    this.emit();
  }

  /** LOCKOUT broadcast: only mine changes the local state. */
  onLockout(p: LockoutPayload): void {
    this.state = { ...this.state, lastLockout: p };
    if (p.playerId !== this.deps.myId()) {
      this.emit();
      return;
    }
    const untilLocal = this.deps.toLocal(p.untilAt);
    this.state = {
      ...this.state,
      lockedUntilLocal: Math.max(this.state.lockedUntilLocal, untilLocal),
      lockoutReason: p.reason,
      phase: this.state.phase === 'armed' || this.state.phase === 'lit' || this.state.phase === 'idle' ? 'locked' : this.state.phase,
    };
    this.scheduleLockoutEnd();
    this.emit();
  }

  /** BUTTON_DISARM / QUESTION_END / a new question: the arm is over. */
  disarm(): void {
    this.clearArmTimers();
    if (this.state.phase === 'idle') return;
    this.state = {
      ...initialArmState,
      phase: 'idle',
      lockedUntilLocal: this.state.lockedUntilLocal,
      lastLockout: this.state.lastLockout,
    };
    if (this.state.lockedUntilLocal > this.deps.now()) {
      this.state = { ...this.state, phase: 'locked' };
      this.scheduleLockoutEnd();
    }
    this.emit();
  }

  // ---- press -----------------------------------------------------------------

  /**
   * Handle a pointerdown/keydown on the button. Send first, render later.
   * Returns what was sent (for tests/diagnostics).
   */
  press(ev: PressEventLike): 'press' | 'misfire' | 'ignored' {
    const now = this.deps.now();
    if (!ev.isTrusted) return 'ignored';
    const st = this.state;
    if (!st.armId) return 'ignored';
    if (st.phase === 'resolved' || st.phase === 'disarmed') return 'ignored';
    if (st.lockedUntilLocal > now) return 'ignored';
    const age = ev.timeStamp !== undefined ? now - ev.timeStamp : NaN;
    const pressLocal = ev.timeStamp && age >= 0 && age <= MAX_EVENT_AGE_MS ? ev.timeStamp : now;
    if (!st.litLocal) {
      // Pressed before the light: report the misfire, lock locally.
      this.deps.send('MISFIRE', { armId: st.armId });
      const lockedUntilLocal = now + st.lockoutMs;
      this.state = { ...st, phase: 'locked', lockedUntilLocal, lockoutReason: 'misfire' };
      this.scheduleLockoutEnd();
      this.emit();
      return 'misfire';
    }
    if (st.pressSeq) return 'ignored'; // one press per arm
    const seq = 1;
    this.deps.send('PRESS', {
      armId: st.armId,
      seq,
      pressLocal,
      litLocal: st.litLocal,
      src: ev.src,
    });
    this.state = { ...st, phase: 'pressed', pressSeq: seq, pressLocal, pressSrc: ev.src };
    this.emit();
    return 'press';
  }

  dispose(): void {
    this.disposed = true;
    this.clearArmTimers();
    if (this.lockoutTimer !== null) {
      this.deps.clearTimeout(this.lockoutTimer);
      this.lockoutTimer = null;
    }
  }

  // ---- internals -------------------------------------------------------------

  private preciseWait(armAtLocal: number): void {
    const remaining = armAtLocal - this.deps.now();
    if (remaining <= 0) {
      this.light();
      return;
    }
    if (remaining <= SPIN_THRESHOLD_MS) {
      // Busy-wait the last few ms for sub-ms accuracy (bounded).
      const cap = this.deps.now() + SPIN_CAP_MS;
      while (this.deps.now() < armAtLocal && this.deps.now() < cap) {
        /* spin */
      }
      this.light();
      return;
    }
    // Still far: yield to the next frame and re-check.
    if (this.rafActive) return;
    this.rafActive = true;
    const raf = this.deps.raf ?? ((cb) => this.deps.setTimeout(cb, 0));
    raf(() => {
      this.rafActive = false;
      const ph = this.state.phase;
      if (this.disposed || this.state.armAtLocal !== armAtLocal || (ph !== 'armed' && ph !== 'locked')) return;
      this.preciseWait(armAtLocal);
    });
  }

  private light(): void {
    const st = this.state;
    // A locally locked player (misfire / LOCKOUT while waiting) still gets the
    // light stamped, so a press after the lockout is a normal PRESS.
    const lockedBeforeLight = st.phase === 'locked' && st.armId !== null && !st.litLocal;
    if (st.phase !== 'armed' && !lockedBeforeLight) return;
    const litLocal = this.deps.now(); // the instant the DOM changes (emit follows synchronously)
    this.state = { ...st, phase: 'lit', litLocal };
    if (this.state.lockedUntilLocal > litLocal) this.state = { ...this.state, phase: 'locked' };
    this.emit();
    this.deps.onLight?.();
  }

  private scheduleDeadline(deadlineAt: number): void {
    if (this.deadlineTimer !== null) this.deps.clearTimeout(this.deadlineTimer);
    const delay = this.deps.toLocal(deadlineAt) - this.deps.now();
    this.deadlineTimer = this.deps.setTimeout(
      () => {
        this.deadlineTimer = null;
        const ph = this.state.phase;
        if (ph === 'armed' || ph === 'lit') {
          this.state = { ...this.state, phase: 'disarmed' };
          this.emit();
        }
      },
      Math.max(0, delay),
    );
  }

  private scheduleLockoutEnd(): void {
    if (this.lockoutTimer !== null) this.deps.clearTimeout(this.lockoutTimer);
    const delay = this.state.lockedUntilLocal - this.deps.now();
    if (delay <= 0) return;
    this.lockoutTimer = this.deps.setTimeout(() => {
      this.lockoutTimer = null;
      if (this.state.phase !== 'locked') return;
      // Back to the underlying arm state (still lit / still waiting) or idle.
      let phase: ArmPhase = 'idle';
      if (this.state.armId && !this.state.result) {
        phase = this.state.pressSeq ? 'pressed' : this.state.litLocal ? 'lit' : 'armed';
      }
      this.state = { ...this.state, phase, lockedUntilLocal: 0 };
      this.emit();
    }, delay);
  }

  private clearArmTimers(): void {
    if (this.lightTimer !== null) {
      this.deps.clearTimeout(this.lightTimer);
      this.lightTimer = null;
    }
    if (this.deadlineTimer !== null) {
      this.deps.clearTimeout(this.deadlineTimer);
      this.deadlineTimer = null;
    }
    this.rafActive = false;
  }

  private emit(): void {
    this.deps.onChange?.(this.state);
  }
}

/** Derives `pressLocal` exactly as the controller does (exported for tests). */
export function pressInstant(now: number, timeStamp: number | undefined): number {
  if (!timeStamp) return now;
  const age = now - timeStamp;
  return age >= 0 && age <= MAX_EVENT_AGE_MS ? timeStamp : now;
}
