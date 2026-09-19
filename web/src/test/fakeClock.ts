/**
 * Deterministic clock + timer scheduler for unit tests. `now()` advances by a
 * tiny drift on every call so busy-wait loops terminate and "time passes"
 * inside handlers, like a real high-resolution clock.
 */
export class FakeClock {
  private t: number;
  private timers: { id: number; at: number; cb: () => void; order: number }[] = [];
  private nextId = 1;
  private order = 0;
  readonly drift: number;

  constructor(start = 1000, driftPerCall = 0.01) {
    this.t = start;
    this.drift = driftPerCall;
  }

  now = (): number => {
    this.t += this.drift;
    return this.t;
  };

  /** Current time without advancing. */
  peek(): number {
    return this.t;
  }

  setTimeout = (cb: () => void, ms: number): number => {
    const id = this.nextId++;
    this.timers.push({ id, at: this.t + Math.max(0, ms), cb, order: this.order++ });
    return id;
  };

  clearTimeout = (id: unknown): void => {
    this.timers = this.timers.filter((x) => x.id !== id);
  };

  raf = (cb: () => void): void => {
    // A frame is ~16 ms; good enough for the coarse phase.
    this.setTimeout(cb, 16);
  };

  /** Runs every timer due within `ms`, in order, moving the clock to each. */
  advance(ms: number): void {
    const end = this.t + ms;
    for (;;) {
      const due = this.timers.filter((x) => x.at <= end).sort((a, b) => a.at - b.at || a.order - b.order);
      const next = due[0];
      if (!next) break;
      this.timers = this.timers.filter((x) => x.id !== next.id);
      if (next.at > this.t) this.t = next.at;
      next.cb();
    }
    if (end > this.t) this.t = end;
  }

  pending(): number {
    return this.timers.length;
  }
}
