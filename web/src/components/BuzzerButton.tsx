import { clsx } from 'clsx';
import { useEffect, useRef } from 'react';
import { useRoom } from '../app/roomContext.ts';
import { useBuzzerStore } from '../state/buzzer.ts';
import { useNow } from '../lib/useNow.ts';

/**
 * The player's button. Press handling follows docs/protocol.md §6.3: the press
 * is stamped inside the native pointerdown/keydown listener (passive, trusted
 * events only) and sent before React re-renders anything.
 */
export function BuzzerButton() {
  const { arm } = useRoom();
  const state = useBuzzerStore((s) => s.arm);
  const model = useBuzzerStore((s) => s.model);
  const soundEnabled = useBuzzerStore((s) => s.soundEnabled);
  const setSound = useBuzzerStore((s) => s.setSound);
  const ref = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const onPointer = (e: PointerEvent) => {
      arm.press({ isTrusted: e.isTrusted, timeStamp: e.timeStamp, src: 'pointer' });
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.code !== 'Space' || e.repeat) return;
      const target = e.target as HTMLElement | null;
      if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)) return;
      arm.press({ isTrusted: e.isTrusted, timeStamp: e.timeStamp, src: 'key' });
    };
    el.addEventListener('pointerdown', onPointer, { passive: true });
    window.addEventListener('keydown', onKey, { passive: true });
    return () => {
      el.removeEventListener('pointerdown', onPointer);
      window.removeEventListener('keydown', onKey);
    };
  }, [arm]);

  // countdown for the lockout badge
  const localNow = useNow(250);
  const locked = state.lockedUntilLocal > localNow;

  const phase = locked && (state.phase === 'armed' || state.phase === 'lit' || state.phase === 'idle') ? 'locked' : state.phase;
  const label =
    phase === 'lit'
      ? 'PRESS'
      : phase === 'armed'
        ? 'wait…'
        : phase === 'pressed'
          ? state.ack
            ? state.ack.status
            : 'sent'
          : phase === 'locked'
            ? `locked ${Math.ceil((state.lockedUntilLocal - localNow) / 1000)} s`
            : phase === 'resolved'
              ? state.result?.kind === 'nobody'
                ? 'nobody'
                : 'resolved'
              : phase === 'disarmed'
                ? 'closed'
                : 'idle';

  return (
    <div className="flex flex-col items-center gap-2">
      <button
        ref={ref}
        type="button"
        aria-label="buzzer"
        className={clsx(
          'h-40 w-40 select-none rounded-full text-lg font-bold text-white shadow-md transition-colors',
          'focus:outline-none focus:ring-4 focus:ring-accent/40',
          phase === 'idle' && 'bg-button-idle',
          phase === 'armed' && 'bg-button-armed',
          phase === 'lit' && 'bg-button-lit',
          phase === 'pressed' && 'bg-button-pressed',
          phase === 'locked' && 'bg-button-locked',
          (phase === 'resolved' || phase === 'disarmed') && 'bg-button-idle',
        )}
        style={{ touchAction: 'none', WebkitUserSelect: 'none' }}
      >
        {label}
      </button>
      <div className="flex items-center gap-3 text-xs text-muted">
        <span>
          sync: <b>{model?.quality ?? 'unsynced'}</b>
          {model ? ` ±${model.uMs.toFixed(0)} ms` : ''}
        </span>
        <label className="flex items-center gap-1">
          <input type="checkbox" checked={soundEnabled} onChange={(e) => setSound(e.target.checked)} /> beep
        </label>
      </div>
      {state.result && (
        <div className="text-xs text-muted">
          result: {state.result.kind}
          {state.result.contested ? ' (contested)' : ''}
          {state.result.reactionMs ? ` · your reaction ${state.result.reactionMs.toFixed(0)} ms (${state.result.source})` : ''}
        </div>
      )}
    </div>
  );
}
