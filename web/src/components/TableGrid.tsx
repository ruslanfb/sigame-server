import { clsx } from 'clsx';
import type { TablePayload } from '../ws/types.ts';

export function TableGrid({
  table,
  onPick,
  canPick,
}: {
  table: TablePayload;
  onPick?: (theme: number, q: number) => void;
  canPick?: boolean;
}) {
  if (table.themes.length === 0) return <p className="text-xs text-muted">no table</p>;
  return (
    <div className="flex flex-col gap-1 text-sm">
      {table.themes.map((theme, ti) => (
        <div key={ti} className={clsx('flex items-center gap-1', theme.removed && 'opacity-40 line-through')}>
          <div className="w-40 truncate pr-2 text-xs" title={theme.name}>
            {theme.name}
          </div>
          {theme.questions.map((c, qi) => {
            const dead = c.played || c.removed || theme.removed;
            return (
              <button
                key={qi}
                type="button"
                disabled={dead || !canPick}
                onClick={() => onPick?.(ti, qi)}
                className={clsx(
                  'h-9 min-w-14 rounded-sm px-2 font-mono',
                  dead ? 'bg-surface-2 text-muted' : 'bg-accent text-accent-contrast',
                  canPick && !dead && 'hover:opacity-90',
                  'disabled:cursor-default',
                )}
              >
                {dead ? '' : c.price}
              </button>
            );
          })}
        </div>
      ))}
    </div>
  );
}
