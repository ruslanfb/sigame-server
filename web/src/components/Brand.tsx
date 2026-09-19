import { clsx } from 'clsx';

/** The show mark: "СВОЯ ИГРА" in the display face with a gold underline. */
export function Brand({ size = 'md', className }: { size?: 'sm' | 'md' | 'lg'; className?: string }) {
  return (
    <div className={clsx('inline-flex flex-col items-center', className)}>
      <span
        className={clsx(
          'font-display text-gold-gradient leading-none font-bold',
          size === 'sm' && 'text-xl',
          size === 'md' && 'text-3xl',
          size === 'lg' && 'text-6xl',
        )}
      >
        Своя игра
      </span>
      <span className={clsx('hr-glow mt-1 w-full', size === 'lg' && 'mt-2')} />
    </div>
  );
}
