import { clsx } from 'clsx';
import type { PlayerInfo } from '../ws/types.ts';

export function ScoreList({
  players,
  scores,
  chooserId,
  meId,
}: {
  players: PlayerInfo[];
  scores: Record<string, number>;
  chooserId?: string | null;
  meId?: string | null;
}) {
  if (players.length === 0) return <p className="text-xs text-muted">no players yet</p>;
  return (
    <ul className="flex flex-col gap-1 text-sm">
      {players.map((p) => (
        <li
          key={p.id}
          className={clsx(
            'flex items-center justify-between rounded-md px-2 py-1',
            p.id === chooserId && 'bg-surface-2',
            !p.connected && 'opacity-50',
          )}
        >
          <span className="flex items-center gap-2">
            <span className={clsx('h-2 w-2 rounded-full', p.connected ? 'bg-success' : 'bg-muted')} />
            <span className={clsx(p.id === meId && 'font-semibold')}>{p.name}</span>
            {p.state !== 'none' && <span className="text-xs text-muted">{p.state}</span>}
            {p.ready && <span className="text-xs text-success">ready</span>}
          </span>
          <span className="font-mono">{scores[p.id] ?? p.score}</span>
        </li>
      ))}
    </ul>
  );
}
