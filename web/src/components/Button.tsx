import { clsx } from 'clsx';
import type { ComponentProps } from 'react';

type Variant = 'primary' | 'gold' | 'secondary' | 'danger' | 'success' | 'ghost';
type Size = 'sm' | 'md' | 'lg' | 'xl';

const sizes: Record<Size, string> = {
  sm: 'h-8 px-3 text-xs',
  md: 'h-10 px-4 text-sm',
  lg: 'h-12 px-5 text-base',
  xl: 'h-14 px-6 text-lg',
};

/**
 * Studio button: electric blue for the primary action, gold for the "money"
 * action (start, confirm, right), flat surfaces for the rest.
 */
export function Button({
  variant = 'primary',
  size = 'md',
  className,
  type = 'button',
  ...rest
}: ComponentProps<'button'> & { variant?: Variant; size?: Size }) {
  return (
    <button
      type={type}
      className={clsx(
        'inline-flex select-none items-center justify-center gap-2 rounded-md font-semibold whitespace-nowrap transition',
        'active:translate-y-px disabled:cursor-not-allowed disabled:opacity-40 disabled:active:translate-y-0',
        sizes[size],
        variant === 'primary' && 'bg-gradient-primary text-primary-contrast shadow-md hover:shadow-glow-blue',
        variant === 'gold' && 'bg-gradient-gold text-accent-contrast shadow-md hover:shadow-glow-gold',
        variant === 'secondary' && 'border border-border-strong bg-surface-2 text-text hover:bg-surface-3',
        variant === 'danger' && 'bg-danger text-primary-contrast hover:shadow-glow-danger',
        variant === 'success' && 'bg-success text-text-inverse hover:shadow-glow-success',
        variant === 'ghost' && 'text-muted hover:bg-surface-2 hover:text-text',
        className,
      )}
      {...rest}
    />
  );
}
