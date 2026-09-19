import { clsx } from 'clsx';
import type { ComponentProps, ReactNode } from 'react';

const base =
  'w-full rounded-md border border-border bg-bg-2 px-3 py-2 text-sm text-text placeholder:text-muted/70 outline-none transition focus:border-accent disabled:opacity-50';

export function Input({ className, ...rest }: ComponentProps<'input'>) {
  return <input className={clsx(base, className)} {...rest} />;
}

export function Textarea({ className, ...rest }: ComponentProps<'textarea'>) {
  return <textarea className={clsx(base, 'min-h-20 resize-y', className)} {...rest} />;
}

export function Select({ className, ...rest }: ComponentProps<'select'>) {
  return <select className={clsx(base, 'appearance-none', className)} {...rest} />;
}

/**
 * Labelled control. `group` renders a `div[role=group]` instead of a `label`
 * for composite controls (segmented radios, radio lists), which must not sit
 * inside a label.
 */
export function Field({
  label,
  hint,
  children,
  className,
  group = false,
}: {
  label: string;
  hint?: ReactNode;
  children: ReactNode;
  className?: string;
  group?: boolean;
}) {
  const Tag = group ? 'div' : 'label';
  return (
    <Tag className={clsx('flex flex-col gap-1 text-sm', className)} role={group ? 'group' : undefined} aria-label={group ? label : undefined}>
      <span className="text-xs font-semibold tracking-wide text-muted uppercase">{label}</span>
      {children}
      {hint && <span className="text-xs text-muted">{hint}</span>}
    </Tag>
  );
}

/** Segmented control (roles, showman modes, stake modes). */
export function Segmented<T extends string>({
  value,
  options,
  onChange,
  className,
  size = 'md',
  name,
}: {
  value: T;
  options: { value: T; label: ReactNode; disabled?: boolean; title?: string }[];
  onChange: (v: T) => void;
  className?: string;
  size?: 'sm' | 'md' | 'lg';
  name?: string;
}) {
  return (
    <div role="radiogroup" aria-label={name} className={clsx('flex rounded-md border border-border bg-bg-2 p-1', className)}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          disabled={o.disabled}
          title={o.title}
          onClick={() => onChange(o.value)}
          className={clsx(
            'flex-1 rounded-sm font-semibold transition disabled:cursor-not-allowed disabled:opacity-40',
            size === 'sm' && 'px-2 py-1 text-xs',
            size === 'md' && 'px-3 py-1.5 text-sm',
            size === 'lg' && 'px-4 py-2.5 text-base',
            value === o.value ? 'bg-gradient-primary text-primary-contrast shadow-sm' : 'text-muted hover:text-text',
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

export function Toggle({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label: ReactNode }) {
  return (
    <label className="flex cursor-pointer items-center gap-2 text-sm select-none">
      <span
        role="switch"
        aria-checked={checked}
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === ' ' || e.key === 'Enter') {
            e.preventDefault();
            onChange(!checked);
          }
        }}
        onClick={() => onChange(!checked)}
        className={clsx(
          'relative h-5 w-9 shrink-0 rounded-full border transition',
          checked ? 'border-accent bg-accent' : 'border-border-strong bg-surface-2',
        )}
      >
        <span
          className={clsx(
            'absolute top-0.5 h-3.5 w-3.5 rounded-full bg-text transition-all',
            checked ? 'left-4.5 bg-accent-contrast' : 'left-0.5',
          )}
        />
      </span>
      <span>{label}</span>
    </label>
  );
}
