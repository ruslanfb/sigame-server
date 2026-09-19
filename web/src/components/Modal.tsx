import type { ReactNode } from 'react';
import { Button } from './Button.tsx';

export function Modal({
  open,
  title,
  children,
  onClose,
}: {
  open: boolean;
  title: string;
  children: ReactNode;
  onClose?: () => void;
}) {
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" role="dialog" aria-modal="true">
      <div className="w-full max-w-md rounded-lg border border-border bg-surface p-5 shadow-lg">
        <h2 className="mb-3 text-lg font-semibold">{title}</h2>
        <div className="text-sm">{children}</div>
        {onClose && (
          <div className="mt-4 flex justify-end">
            <Button variant="secondary" onClick={onClose}>
              OK
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}
