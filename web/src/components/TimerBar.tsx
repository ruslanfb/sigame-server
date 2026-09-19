import { clsx } from 'clsx';
import { msToClock, timerProgress } from '../lib/clock.ts';
import { TIMER_LABEL } from '../lib/labels.ts';
import { useNow } from '../lib/useNow.ts';
import { useReducedMotion } from '../lib/useReducedMotion.ts';
import type { TimerState } from '../state/game.ts';

/** Progress bar for one engine timer; re-renders on animation frames while running. */
export function TimerBar({
  timer,
  label,
  thin = false,
  tone = 'gold',
}: {
  timer: TimerState;
  label?: string;
  thin?: boolean;
  tone?: 'gold' | 'blue' | 'danger';
}) {
  const reduced = useReducedMotion();
  const localNow = useNow(timer.paused ? 0 : reduced ? 250 : 'raf');
  const { remainingMs, fraction } = timerProgress(timer, localNow);
  const urgent = remainingMs < 5000 && remainingMs > 0 && timer.durationMs > 8000;
  // Thin bars are 4 px: a gradient reads as its darkest stop, so use the flat accent there.
  const color = urgent ? 'bg-danger' : tone === 'gold' ? (thin ? 'bg-accent' : 'bg-gradient-gold') : tone === 'blue' ? 'bg-primary' : 'bg-danger';
  return (
    <div className={clsx('w-full', thin ? '' : 'text-xs')}>
      {!thin && (
        <div className="mb-1 flex justify-between text-muted">
          <span>
            {label ?? TIMER_LABEL[timer.kind] ?? timer.kind}
            {timer.paused ? ' · пауза' : ''}
          </span>
          <span className="font-mono tabular-nums">{msToClock(remainingMs)}</span>
        </div>
      )}
      <div className={clsx('w-full overflow-hidden rounded-full bg-surface-2', thin ? 'h-1' : 'h-1.5')}>
        <div className={clsx('h-full transition-[width] duration-100 ease-linear', color)} style={{ width: `${fraction * 100}%` }} />
      </div>
    </div>
  );
}

/** The timers that matter for a person: the personal one first, then the shared ones. */
export function TimerStack({
  timers,
  thin = false,
  personId,
  max = 2,
}: {
  timers: Record<string, TimerState>;
  thin?: boolean;
  personId?: string | null;
  max?: number;
}) {
  const list = Object.values(timers)
    .filter((t) => t.kind !== 'round' && t.kind !== 'mediaFallback')
    .sort((a, b) => Number(b.personId === personId) - Number(a.personId === personId) || a.durationMs - b.durationMs)
    .slice(0, max);
  if (list.length === 0) return null;
  return (
    <div className="flex flex-col gap-1.5">
      {list.map((t) => (
        <TimerBar key={t.id} timer={t} thin={thin} />
      ))}
    </div>
  );
}
