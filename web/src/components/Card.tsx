import { clsx } from 'clsx';
import type { HTMLAttributes, ReactNode } from 'react';

export function Card({ title, children, className, ...rest }: Omit<HTMLAttributes<HTMLDivElement>, 'title'> & { title?: ReactNode }) {
  return (
    <section className={clsx('rounded-lg border border-border bg-surface p-4 shadow-sm', className)} {...rest}>
      {title !== undefined && <h2 className="mb-3 text-sm font-semibold text-muted">{title}</h2>}
      {children}
    </section>
  );
}
