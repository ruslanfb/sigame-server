import { clsx } from 'clsx';
import { useEffect } from 'react';
import { useToastStore } from '../state/toast.ts';

const TOAST_TTL_MS = 4000;

export function Toasts() {
  const toasts = useToastStore((s) => s.toasts);
  const dismiss = useToastStore((s) => s.dismiss);
  useEffect(() => {
    if (toasts.length === 0) return;
    const id = setInterval(() => {
      const cutoff = Date.now() - TOAST_TTL_MS;
      for (const t of useToastStore.getState().toasts) if (t.at < cutoff) dismiss(t.id);
    }, 500);
    return () => clearInterval(id);
  }, [toasts.length, dismiss]);
  if (toasts.length === 0) return null;
  return (
    <div className="pointer-events-none fixed inset-x-0 top-3 z-[60] flex flex-col items-center gap-2 px-4">
      {toasts.map((t) => (
        <button
          key={t.id}
          onClick={() => dismiss(t.id)}
          className={clsx(
            'animate-fade-in pointer-events-auto max-w-lg rounded-md border px-4 py-2 text-sm font-semibold shadow-lg',
            t.kind === 'error' && 'border-danger bg-danger/90 text-primary-contrast',
            t.kind === 'info' && 'border-border-strong bg-surface-2 text-text',
            t.kind === 'success' && 'border-accent bg-gradient-gold text-accent-contrast shadow-glow-gold',
          )}
        >
          {t.text}
          {t.ref ? <span className="ml-2 opacity-60">#{t.ref}</span> : null}
        </button>
      ))}
    </div>
  );
}
