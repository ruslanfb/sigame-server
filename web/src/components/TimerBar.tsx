import { useEffect, useState } from 'react';
import { msToClock, timerProgress } from '../lib/clock.ts';
import type { TimerState } from '../state/game.ts';

/** Progress bar for one engine timer; re-renders on animation frames while running. */
export function TimerBar({ timer }: { timer: TimerState }) {
  const [, tick] = useState(0);
  useEffect(() => {
    if (timer.paused) return;
    let raf = 0;
    const loop = () => {
      tick((n) => n + 1);
      raf = requestAnimationFrame(loop);
    };
    raf = requestAnimationFrame(loop);
    return () => cancelAnimationFrame(raf);
  }, [timer.paused, timer.id, timer.startedAtLocal]);
  const { remainingMs, fraction } = timerProgress(timer, performance.now());
  return (
    <div className="text-xs">
      <div className="flex justify-between text-muted">
        <span>
          {timer.kind}
          {timer.paused ? ' (paused)' : ''}
        </span>
        <span className="font-mono">{msToClock(remainingMs)}</span>
      </div>
      <div className="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-surface-2">
        <div className="h-full bg-accent" style={{ width: `${fraction * 100}%` }} />
      </div>
    </div>
  );
}

export function TimerList({ timers }: { timers: Record<string, TimerState> }) {
  const list = Object.values(timers);
  if (list.length === 0) return <p className="text-xs text-muted">no timers</p>;
  return (
    <div className="flex flex-col gap-2">
      {list.map((t) => (
        <TimerBar key={t.id} timer={t} />
      ))}
    </div>
  );
}
