import { clsx } from 'clsx';
import type { HTMLAttributes, ReactNode } from 'react';

/** Studio panel: a dark surface with a soft border; `title` is a small caps label. */
export function Card({
  title,
  action,
  children,
  className,
  bodyClassName,
  ...rest
}: Omit<HTMLAttributes<HTMLDivElement>, 'title'> & { title?: ReactNode; action?: ReactNode; bodyClassName?: string }) {
  return (
    <section className={clsx('rounded-lg border border-border bg-surface/90 p-4 shadow-md backdrop-blur-sm', className)} {...rest}>
      {(title !== undefined || action) && (
        <header className="mb-3 flex items-center justify-between gap-2">
          {title !== undefined && <h2 className="text-xs font-bold tracking-widest text-muted uppercase">{title}</h2>}
          {action}
        </header>
      )}
      <div className={bodyClassName}>{children}</div>
    </section>
  );
}
