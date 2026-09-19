import { clsx } from 'clsx';
import { useEffect, useRef } from 'react';
import { useRoom } from '../app/roomContext.ts';
import { LOCKOUT_REASON_LABEL, PRESS_STATUS_LABEL, QUALITY_LABEL } from '../lib/labels.ts';
import { useNow } from '../lib/useNow.ts';
import { useBuzzerStore } from '../state/buzzer.ts';

/**
 * The player's big hexagonal button. Press handling follows docs/protocol.md
 * §6.3: the press is stamped inside the native pointerdown/keydown listener
 * (passive, trusted events only) and sent before React re-renders anything.
 */
export function BuzzerButton({ className }: { className?: string }) {
  const { arm } = useRoom();
  const state = useBuzzerStore((s) => s.arm);
  const model = useBuzzerStore((s) => s.model);
  const ref = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const onPointer = (e: PointerEvent) => {
      arm.press({ isTrusted: e.isTrusted, timeStamp: e.timeStamp, src: 'pointer' });
    };
    const onKey = (e: KeyboardEvent) => {
      if ((e.code !== 'Space' && e.code !== 'Enter') || e.repeat) return;
      const target = e.target as HTMLElement | null;
      if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT' || target.isContentEditable))
        return;
      arm.press({ isTrusted: e.isTrusted, timeStamp: e.timeStamp, src: 'key' });
    };
    el.addEventListener('pointerdown', onPointer, { passive: true });
    window.addEventListener('keydown', onKey, { passive: true });
    return () => {
      el.removeEventListener('pointerdown', onPointer);
      window.removeEventListener('keydown', onKey);
    };
  }, [arm]);

  const localNow = useNow(state.lockedUntilLocal ? 100 : 0);
  const locked = state.lockedUntilLocal > localNow;
  const phase = locked && (state.phase === 'armed' || state.phase === 'lit' || state.phase === 'idle') ? 'locked' : state.phase;
  const lockLeft = Math.max(0, (state.lockedUntilLocal - localNow) / 1000);
  const won = phase === 'resolved' && state.result?.winnerId && state.result.kind === 'winner';

  let label: string;
  let sub: string | null = null;
  switch (phase) {
    case 'lit':
      label = 'ЖМИ';
      break;
    case 'armed':
      label = 'Ждите';
      sub = 'сигнал скоро';
      break;
    case 'pressed':
      label = state.ack ? PRESS_STATUS_LABEL[state.ack.status] : 'Отправлено';
      sub = state.ack?.reactionMs ? `${state.ack.reactionMs.toFixed(0)} мс` : null;
      break;
    case 'locked':
      label = lockLeft.toFixed(1);
      sub = LOCKOUT_REASON_LABEL[state.lockoutReason ?? ''] ?? 'Блокировка';
      break;
    case 'resolved':
      label = state.result?.kind === 'nobody' ? 'Никто' : won ? 'Вы!' : 'Есть';
      sub = state.result?.reactionMs ? `ваша реакция ${state.result.reactionMs.toFixed(0)} мс` : null;
      break;
    case 'disarmed':
      label = 'Закрыто';
      break;
    default:
      label = 'Кнопка';
      sub = 'ждём вопрос';
  }

  const tone =
    phase === 'lit'
      ? 'bg-button-lit text-accent-contrast animate-pulse-gold'
      : phase === 'armed'
        ? 'bg-button-armed/80 text-accent-contrast'
        : phase === 'pressed'
          ? 'bg-button-pressed text-primary-contrast'
          : phase === 'locked'
            ? 'bg-button-locked text-primary-contrast'
            : won
              ? 'bg-gradient-gold text-accent-contrast'
              : 'bg-button-idle text-muted';
  const glow = phase === 'lit' || won ? 'glow-gold' : phase === 'pressed' ? 'glow-blue' : phase === 'locked' ? 'glow-danger' : '';

  return (
    <div className={clsx('flex flex-col items-center gap-2', className)}>
      <div className={clsx('rounded-full transition-shadow', glow)}>
        <button
          ref={ref}
          type="button"
          aria-label={`Кнопка: ${label}`}
          aria-pressed={phase === 'pressed'}
          className={clsx(
            'flex h-56 w-64 max-w-[80vw] select-none flex-col items-center justify-center transition-colors duration-75',
            'font-display text-5xl font-bold',
            tone,
          )}
          style={{ clipPath: 'var(--sg-hex-clip)', touchAction: 'none', WebkitUserSelect: 'none', WebkitTapHighlightColor: 'transparent' }}
        >
          <span>{label}</span>
          {sub && <span className="mt-1 font-sans text-xs font-medium normal-case tracking-normal opacity-80">{sub}</span>}
        </button>
      </div>
      <SyncBadge quality={model?.quality} uMs={model?.uMs} />
    </div>
  );
}

export function SyncBadge({ quality, uMs, className }: { quality?: string; uMs?: number; className?: string }) {
  const q = (quality ?? 'unsynced') as keyof typeof QUALITY_LABEL;
  const color = q === 'good' ? 'text-success' : q === 'fair' ? 'text-warning' : q === 'poor' ? 'text-danger' : 'text-muted';
  return (
    <span className={clsx('text-[11px] text-muted', className)} title="Точность синхронизации часов с сервером">
      Синхронизация: <b className={color}>{QUALITY_LABEL[q] ?? q}</b>
      {uMs !== undefined && q !== 'unsynced' ? ` ±${uMs.toFixed(0)} мс` : ''}
    </span>
  );
}
