import { Card } from '../components/Card.tsx';
import { ScoreList } from '../components/ScoreList.tsx';
import { TableGrid } from '../components/TableGrid.tsx';
import { useGameStore } from '../state/game.ts';
import { RoomShell } from './RoomShell.tsx';

/** Big-screen view (viewer / TV). */
export function TablePage() {
  return (
    <RoomShell title="Table">
      <TableBody />
    </RoomShell>
  );
}

function TableBody() {
  const game = useGameStore((s) => s.game);
  const q = game.question;
  return (
    <div className="grid gap-4 md:grid-cols-[1fr_280px]">
      <Card title={game.stage === 'lobby' ? 'lobby' : `round ${game.roundIndex + 1} · ${game.roundName}`}>
        {q ? (
          <div className="text-lg">
            <div className="text-sm text-muted">
              {q.theme} · {q.price}
            </div>
            {q.content.map((c) => (
              <div key={`${c.phase}-${c.index}`} className="mt-2">
                {c.items.map((it, i) => (
                  <span key={i}>{it.type === 'text' ? it.text : `[${it.type}]`} </span>
                ))}
              </div>
            ))}
            {q.rightAnswer && (
              <div className="mt-2 text-success">
                answer: <b>{q.rightAnswer.text}</b>
              </div>
            )}
          </div>
        ) : (
          <TableGrid table={game.table} />
        )}
      </Card>
      <Card title="scores">
        <ScoreList players={game.players} scores={game.scores} chooserId={game.chooserId} />
      </Card>
    </div>
  );
}
