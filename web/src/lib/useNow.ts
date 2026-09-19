import { useEffect, useState } from 'react';
import { now } from './clock.ts';

/**
 * A clock value for rendering time-dependent UI (countdowns, progress bars).
 * Re-renders every `intervalMs` (or every animation frame with 'raf'); 0 disables ticking.
 * Reading the clock in render directly is impure; this hook keeps it in state.
 */
export function useNow(interval: number | 'raf' = 250): number {
  const [value, setValue] = useState<number>(() => now());
  useEffect(() => {
    if (interval === 0) return;
    if (interval === 'raf') {
      let raf = 0;
      const loop = () => {
        setValue(now());
        raf = requestAnimationFrame(loop);
      };
      raf = requestAnimationFrame(loop);
      return () => cancelAnimationFrame(raf);
    }
    const id = setInterval(() => setValue(now()), interval);
    return () => clearInterval(id);
  }, [interval]);
  return value;
}
