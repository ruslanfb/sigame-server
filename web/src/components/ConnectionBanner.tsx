import { clsx } from 'clsx';
import { useDebugStore } from '../state/debug.ts';

/** Sticky banner while the socket is not open (reconnecting / first connect). */
export function ConnectionBanner({ className }: { className?: string }) {
  const state = useDebugStore((s) => s.connection);
  if (state === 'open' || state === 'idle') return null;
  const text =
    state === 'connecting' ? 'Подключение…' : state === 'reconnecting' ? 'Связь потеряна, переподключаемся…' : 'Соединение закрыто';
  return (
    <div
      role="status"
      className={clsx(
        'flex items-center justify-center gap-2 px-3 py-1.5 text-center text-xs font-semibold',
        state === 'closed' ? 'bg-danger text-primary-contrast' : 'bg-warning text-text-inverse',
        className,
      )}
    >
      {state !== 'closed' && <span className="inline-block h-2 w-2 animate-pulse rounded-full bg-text-inverse" />}
      {text}
    </div>
  );
}

export function ConnDot({ className }: { className?: string }) {
  const state = useDebugStore((s) => s.connection);
  const color = state === 'open' ? 'bg-success' : state === 'closed' ? 'bg-danger' : 'bg-warning animate-pulse';
  const title = state === 'open' ? 'Подключено' : state === 'closed' ? 'Отключено' : 'Подключение…';
  return <span className={clsx('inline-block h-2.5 w-2.5 rounded-full', color, className)} title={title} aria-label={title} />;
}
