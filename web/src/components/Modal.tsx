import { clsx } from 'clsx';
import { useEffect, type ReactNode } from 'react';
import { Button } from './Button.tsx';

/**
 * Dialog: centred on desktop, a bottom sheet on phones. `sheet` forces the
 * sheet look everywhere (player prompts).
 */
export function Modal({
  open,
  title,
  children,
  onClose,
  closeLabel = 'OK',
  sheet = false,
  tone = 'default',
  className,
}: {
  open: boolean;
  title?: ReactNode;
  children: ReactNode;
  onClose?: () => void;
  closeLabel?: string;
  sheet?: boolean;
  tone?: 'default' | 'gold';
  className?: string;
}) {
  useEffect(() => {
    if (!open || !onClose) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);
  if (!open) return null;
  return (
    <div
      className={clsx(
        'fixed inset-0 z-50 flex bg-bg/70 p-0 backdrop-blur-sm sm:p-4',
        sheet ? 'items-end justify-center' : 'items-end justify-center sm:items-center',
      )}
      role="dialog"
      aria-modal="true"
    >
      <div
        className={clsx(
          'animate-pop w-full max-w-md rounded-t-lg border bg-surface p-5 shadow-lg sm:rounded-lg',
          tone === 'gold' ? 'border-accent shadow-glow-gold' : 'border-border-strong',
          className,
        )}
      >
        {title !== undefined && <h2 className="font-display mb-3 text-xl text-accent">{title}</h2>}
        <div className="text-sm">{children}</div>
        {onClose && (
          <div className="mt-4 flex justify-end">
            <Button variant="secondary" onClick={onClose}>
              {closeLabel}
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}
