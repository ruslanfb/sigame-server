import { useRoom } from '../app/roomContext.ts';
import { Button } from '../components/Button.tsx';
import { Card } from '../components/Card.tsx';
import { Chat } from '../components/Chat.tsx';
import { ScoreList } from '../components/ScoreList.tsx';
import { TableGrid } from '../components/TableGrid.tsx';
import { useGameStore } from '../state/game.ts';
import { personName, useRoomStore } from '../state/room.ts';
import { useSessionStore } from '../state/session.ts';
import { RoomShell } from './RoomShell.tsx';

export function HostRoomPage() {
  return (
    <RoomShell title="Host">
      <HostBody />
    </RoomShell>
  );
}

function HostBody() {
  const { conn } = useRoom();
  const game = useGameStore((s) => s.game);
  const persons = useRoomStore((s) => s.persons);
  const me = useSessionStore((s) => s.session?.personId ?? null);
  const q = game.question;
  const v = game.prompts.askValidate;

  return (
    <div className="grid gap-4 md:grid-cols-[1fr_280px]">
      <div className="flex flex-col gap-4">
        <Card title="controls">
          <div className="flex flex-wrap gap-2">
            {game.stage === 'lobby' && <Button onClick={() => conn.send('START')}>start game</Button>}
            {game.stage !== 'lobby' && (
              <>
                <Button variant="secondary" onClick={() => conn.send('NEXT')}>
                  next
                </Button>
                <Button variant="secondary" onClick={() => conn.send('PAUSE', { on: !game.paused })}>
                  {game.paused ? 'resume' : 'pause'}
                </Button>
                <Button variant="secondary" onClick={() => conn.send('MOVE', { dir: 1 })}>
                  skip
                </Button>
                <Button variant="secondary" onClick={() => conn.send('MOVE', { dir: 2 })}>
                  next round
                </Button>
              </>
            )}
          </div>
        </Card>
        <Card title={game.stage === 'lobby' ? 'lobby' : `round ${game.roundIndex + 1} · ${game.roundName} · ${game.sub ?? game.stage}`}>
          {q && (
            <div className="mb-3 text-sm">
              <div className="text-muted">
                {q.theme} · {q.price} · {q.type}
              </div>
              {q.content.map((c) => (
                <div key={`${c.phase}-${c.index}`} className="mt-1">
                  {c.items.map((it, i) => (
                    <span key={i}>{it.type === 'text' ? it.text : `[${it.type}]`} </span>
                  ))}
                </div>
              ))}
              {q.hint?.rights && (
                <div className="mt-2 text-xs text-muted">rights: {q.hint.rights.join(' / ')}</div>
              )}
              {Object.entries(q.drafts).map(([pid, text]) => (
                <div key={pid} className="text-xs text-muted">
                  {personName(persons, pid)} typing: {text}
                </div>
              ))}
              {Object.entries(q.answers).map(([pid, a]) => (
                <div key={pid} className="text-xs">
                  {personName(persons, pid)} answered: <b>{a.text ?? a.optionLabel ?? String(a.number ?? '')}</b>
                </div>
              ))}
            </div>
          )}
          <TableGrid
            table={game.table}
            canPick={game.prompts.askChoose !== undefined}
            onPick={(theme, qq) => conn.send('CHOOSE_QUESTION', { theme, q: qq })}
          />
        </Card>
        {v && (
          <Card title={`validate: ${personName(persons, v.personId)} — "${v.answer}"`}>
            <p className="mb-2 text-xs text-muted">rights: {v.rights.join(' / ')}</p>
            <div className="flex gap-2">
              <Button onClick={() => conn.send('VALIDATE', { personId: v.personId, right: true })}>right</Button>
              <Button variant="danger" onClick={() => conn.send('VALIDATE', { personId: v.personId, right: false })}>
                wrong
              </Button>
            </div>
          </Card>
        )}
        {game.prompts.askSelectPlayer && (
          <Card title={`select a player (${game.prompts.askSelectPlayer.reason})`}>
            <div className="flex flex-wrap gap-2">
              {game.prompts.askSelectPlayer.candidates.map((id) => (
                <Button key={id} variant="secondary" onClick={() => conn.send('SELECT_PLAYER', { personId: id })}>
                  {personName(persons, id)}
                </Button>
              ))}
            </div>
          </Card>
        )}
      </div>
      <div className="flex flex-col gap-4">
        <Card title="scores">
          <ScoreList players={game.players} scores={game.scores} chooserId={game.chooserId} meId={me} />
        </Card>
        <Card title="people">
          <ul className="text-sm">
            {persons.map((p) => (
              <li key={p.id} className={p.connected ? '' : 'opacity-50'}>
                {p.name} · {p.role}
                {p.isHost ? ' · host' : ''}
              </li>
            ))}
          </ul>
        </Card>
        <Card title="chat">
          <Chat />
        </Card>
      </div>
    </div>
  );
}
