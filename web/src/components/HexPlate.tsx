import { clsx } from 'clsx';
import type { ComponentProps, CSSProperties, ReactNode } from 'react';

export type HexTone = 'default' | 'active' | 'gold' | 'dim' | 'danger' | 'success' | 'outline';

const inner: Record<HexTone, string> = {
  default: 'hex text-text',
  active: 'hex hex-active text-text',
  gold: 'hex hex-gold',
  dim: 'hex hex-dim',
  danger: 'hex bg-danger text-primary-contrast',
  success: 'hex bg-success text-text-inverse',
  outline: 'hex bg-bg-2 text-text',
};

const frame: Record<HexTone, string> = {
  default: 'hex-frame',
  active: 'hex-frame hex-frame-primary',
  gold: 'hex-frame hex-frame-gold',
  dim: 'hex-frame opacity-60',
  danger: 'hex-frame bg-danger',
  success: 'hex-frame bg-success',
  outline: 'hex-frame hex-frame-gold',
};

interface BaseProps {
  tone?: HexTone;
  className?: string;
  /** Inner padding classes; defaults to a compact plate. */
  padding?: string;
  glow?: 'gold' | 'blue' | 'danger' | 'success' | null;
  style?: CSSProperties;
  children?: ReactNode;
  title?: string;
}

/**
 * Millionaire-style hexagonal plate: an outer clipped frame gives the 2px
 * border, the inner clipped surface carries the gradient and the content.
 * `clip-path` drops box-shadows, so the glow lives on a wrapping element.
 */
export function HexPlate({ tone = 'default', className, padding = 'px-4 py-2', glow = null, style, children, title }: BaseProps) {
  return (
    <div className={clsx('inline-block', glow && `rounded-full glow-${glow}`, className)} style={style} title={title}>
      <div className={clsx(frame[tone])}>
        <div className={clsx(inner[tone], 'flex h-full w-full items-center justify-center text-center', padding)}>{children}</div>
      </div>
    </div>
  );
}

/** A pressable plate (prices, answer options, theme choices). */
export function HexButton({
  tone = 'default',
  className,
  padding = 'px-4 py-2',
  glow = null,
  children,
  disabled,
  ...rest
}: BaseProps & Omit<ComponentProps<'button'>, 'style' | 'title' | 'children'>) {
  return (
    <button
      type="button"
      disabled={disabled}
      className={clsx(
        'group inline-block bg-transparent p-0 transition',
        !disabled && 'hover:scale-[1.03] active:scale-[0.98]',
        disabled && 'cursor-default',
        glow && `rounded-full glow-${glow}`,
        className,
      )}
      {...rest}
    >
      <div className={clsx(frame[tone], !disabled && tone === 'default' && 'group-hover:hex-frame-gold')}>
        <div className={clsx(inner[tone], 'flex h-full w-full items-center justify-center text-center', padding)}>{children}</div>
      </div>
    </button>
  );
}
