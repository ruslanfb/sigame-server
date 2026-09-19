import { clsx } from 'clsx';
import { useToastStore } from '../state/toast.ts';

export function Toasts() {
  const toasts = useToastStore((s) => s.toasts);
  const dismiss = useToastStore((s) => s.dismiss);
  if (toasts.length === 0) return null;
  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-4 z-40 flex flex-col items-center gap-2 px-4">
      {toasts.map((t) => (
        <button
          key={t.id}
          onClick={() => dismiss(t.id)}
          className={clsx(
            'pointer-events-auto max-w-lg rounded-md px-4 py-2 text-sm shadow-md',
            t.kind === 'error' && 'bg-danger text-white',
            t.kind === 'info' && 'bg-surface-2 text-text',
            t.kind === 'success' && 'bg-success text-white',
          )}
        >
          {t.text}
          {t.ref ? <span className="ml-2 opacity-70">#{t.ref}</span> : null}
        </button>
      ))}
    </div>
  );
}
