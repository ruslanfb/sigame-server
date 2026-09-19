/** Clock helpers. Always performance.now(), never Date.now(), for anything the buzzer touches. */

export const now = (): number => performance.now();

/** Progress of a timer in [0,1] on the local clock. */
export function timerProgress(
  t: { durationMs: number; startedAtLocal: number; paused: boolean; remainingMs: number },
  localNow: number,
): { remainingMs: number; fraction: number } {
  if (t.durationMs <= 0) return { remainingMs: 0, fraction: 0 };
  const remaining = t.paused ? t.remainingMs : Math.max(0, t.durationMs - (localNow - t.startedAtLocal));
  return { remainingMs: remaining, fraction: Math.min(1, Math.max(0, remaining / t.durationMs)) };
}

export function msToClock(ms: number): string {
  const s = Math.max(0, Math.ceil(ms / 1000));
  const m = Math.floor(s / 60);
  const r = s % 60;
  return m > 0 ? `${m}:${String(r).padStart(2, '0')}` : `${r}`;
}
