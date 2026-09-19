import { clsx } from 'clsx';
import { ROLE_LABEL } from '../lib/labels.ts';
import type { PersonView, PlayerInfo } from '../ws/types.ts';
import { Button } from './Button.tsx';

/** Everyone in the room (lobby): role, connection, ready flag; host actions optional. */
export function PersonsList({
  persons,
  players,
  meId,
  onKick,
  className,
}: {
  persons: PersonView[];
  players?: PlayerInfo[];
  meId?: string | null;
  onKick?: (id: string, ban: boolean) => void;
  className?: string;
}) {
  const order: Record<string, number> = { showman: 0, player: 1, viewer: 2 };
  const sorted = [...persons].sort((a, b) => (order[a.role] ?? 3) - (order[b.role] ?? 3) || a.name.localeCompare(b.name, 'ru'));
  if (sorted.length === 0) return <p className="text-sm text-muted">Пока никого</p>;
  return (
    <ul className={clsx('flex flex-col gap-1', className)}>
      {sorted.map((p) => {
        const ready = players?.find((pl) => pl.id === p.id)?.ready;
        return (
          <li
            key={p.id}
            className={clsx(
              'flex items-center gap-2 rounded-md border border-border bg-bg-2/60 px-3 py-2 text-sm',
              !p.connected && 'opacity-50',
              p.id === meId && 'border-accent/60',
            )}
          >
            <span className={clsx('h-2 w-2 shrink-0 rounded-full', p.connected ? 'bg-success' : 'bg-muted')} />
            <span className="flex-1 truncate font-semibold">
              {p.name}
              {p.id === meId && <span className="ml-1 text-xs text-muted">(вы)</span>}
            </span>
            <span className="text-xs text-muted">{ROLE_LABEL[p.role]}</span>
            {p.isHost && <span className="rounded-sm bg-gradient-gold px-1.5 text-[10px] font-bold text-accent-contrast uppercase">хост</span>}
            {p.role === 'player' && ready !== undefined && (
              <span className={clsx('text-xs', ready ? 'text-success' : 'text-muted')}>{ready ? 'готов' : 'не готов'}</span>
            )}
            {onKick && !p.isHost && p.id !== meId && (
              <span className="flex gap-1">
                <Button size="sm" variant="ghost" onClick={() => onKick(p.id, false)} title="Выгнать">
                  Выгнать
                </Button>
                <Button size="sm" variant="ghost" className="text-danger" onClick={() => onKick(p.id, true)} title="Выгнать и заблокировать">
                  Бан
                </Button>
              </span>
            )}
          </li>
        );
      })}
    </ul>
  );
}
