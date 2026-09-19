import { clsx } from 'clsx';
import { fmtScore } from '../lib/labels.ts';
import type { TablePayload } from '../ws/types.ts';
import { HexButton, HexPlate } from './HexPlate.tsx';

export interface GameTableProps {
  table: TablePayload;
  /** Currently played cell (gold). */
  active?: { themeIndex: number; questionIndex: number } | null;
  /** Cells are pressable (chooser's turn or host acting on behalf). */
  canPick?: boolean;
  onPick?: (theme: number, q: number) => void;
  /** Host edit mode: any cell (even played) is pressable → TOGGLE. */
  editMode?: boolean;
  onToggle?: (theme: number, q: number) => void;
  /** compact = phone (theme line + wrapping plates), full = host console, tv = 1920×1080. */
  size?: 'compact' | 'full' | 'tv';
  className?: string;
  /** Theme rows the current person may delete (final round). */
  deletableThemes?: number[];
  onDeleteTheme?: (theme: number) => void;
}

/**
 * The money ladder: one row per theme, a hex plate per price, all plates of
 * equal width (a grid with as many columns as the longest theme). Played and
 * removed cells are dimmed, the active one is gold.
 */
export function GameTable({
  table,
  active,
  canPick = false,
  onPick,
  editMode = false,
  onToggle,
  size = 'full',
  className,
  deletableThemes,
  onDeleteTheme,
}: GameTableProps) {
  if (table.themes.length === 0) return <p className="text-sm text-muted">Таблица ещё не показана</p>;
  const cols = Math.max(1, ...table.themes.map((t) => t.questions.length));
  const dense = cols > 8;
  const priceCls =
    size === 'tv'
      ? dense
        ? 'h-16 text-2xl'
        : 'h-16 text-3xl'
      : size === 'compact'
        ? 'h-9 text-sm'
        : dense
          ? 'h-9 text-xs'
          : 'h-11 text-lg';
  // Narrow plates (many columns on a console) drop the thousands separator.
  const price = (v: number) => (dense && size !== 'tv' ? String(v) : fmtScore(v));
  const themeCls = size === 'tv' ? 'text-2xl font-semibold' : size === 'compact' ? 'text-xs font-semibold' : 'text-sm font-semibold';
  const gap = size === 'tv' ? 'gap-3' : 'gap-1.5';

  const cell = (ti: number, qi: number, c: { price: number; played: boolean; removed: boolean }, themeRemoved: boolean) => {
    const theme = table.themes[ti]!;
    const dead = c.played || c.removed || themeRemoved;
    const isActive = active?.themeIndex === ti && active?.questionIndex === qi;
    const pressable = editMode ? !!onToggle : canPick && !dead && !!onPick;
    const tone = isActive ? 'gold' : dead ? 'dim' : canPick ? 'active' : 'default';
    return (
      <HexButton
        key={qi}
        tone={tone}
        glow={isActive ? 'gold' : null}
        padding="p-0"
        disabled={!pressable}
        aria-label={`${theme.name}, ${c.price}${dead ? ', сыграно' : ''}`}
        onClick={() => (editMode ? onToggle?.(ti, qi) : onPick?.(ti, qi))}
        className={clsx('w-full min-w-0', editMode && dead && 'opacity-80', size === 'compact' && 'flex-1 basis-12')}
      >
        <span className={clsx('font-display flex w-full items-center justify-center tabular-nums whitespace-nowrap', priceCls)}>
          {dead && !editMode && !isActive ? '' : price(c.price)}
        </span>
      </HexButton>
    );
  };

  const themePlate = (ti: number) => {
    const theme = table.themes[ti]!;
    const deletable = deletableThemes?.includes(ti);
    if (deletable && onDeleteTheme) {
      return (
        <HexButton tone="danger" padding="px-3 py-1" className="w-full min-w-0" onClick={() => onDeleteTheme(ti)}>
          <span className={clsx('truncate', themeCls)}>Убрать: {theme.name}</span>
        </HexButton>
      );
    }
    return (
      <HexPlate tone={theme.removed ? 'dim' : 'default'} padding="px-3 py-1" className="w-full min-w-0" title={theme.name}>
        <span className={clsx('line-clamp-2 w-full', themeCls, theme.removed && 'line-through')}>{theme.name}</span>
      </HexPlate>
    );
  };

  if (size === 'compact') {
    return (
      <div className={clsx('flex flex-col gap-2', className)} role="grid">
        {table.themes.map((theme, ti) => (
          <div key={ti} role="row" className="flex flex-col gap-1">
            <div className="px-1">{themePlate(ti)}</div>
            <div className="flex flex-wrap gap-1">{theme.questions.map((c, qi) => cell(ti, qi, c, theme.removed))}</div>
          </div>
        ))}
      </div>
    );
  }

  const themeCol = size === 'tv' ? 'minmax(220px, 24rem)' : dense ? 'minmax(112px, 9rem)' : 'minmax(140px, 13rem)';
  return (
    <div
      className={clsx('grid w-full', gap, className)}
      role="grid"
      style={{ gridTemplateColumns: `${themeCol} repeat(${cols}, minmax(0, 1fr))` }}
    >
      {table.themes.map((theme, ti) => (
        <div key={ti} role="row" className="contents">
          {themePlate(ti)}
          {theme.questions.map((c, qi) => cell(ti, qi, c, theme.removed))}
          {Array.from({ length: cols - theme.questions.length }, (_, i) => (
            <span key={`e${i}`} aria-hidden="true" />
          ))}
        </div>
      ))}
    </div>
  );
}
