import { useState } from 'react';
import { useRoom } from '../app/roomContext.ts';
import { BuzzerButton } from '../components/BuzzerButton.tsx';
import { Button } from '../components/Button.tsx';
import { Card } from '../components/Card.tsx';
import { Chat } from '../components/Chat.tsx';
import { Input } from '../components/Input.tsx';
import { ScoreList } from '../components/ScoreList.tsx';
import { TableGrid } from '../components/TableGrid.tsx';
import { useGameStore } from '../state/game.ts';
import { useSessionStore } from '../state/session.ts';
import { RoomShell } from './RoomShell.tsx';

export function PlayerRoomPage() {
  return (
    <RoomShell title="Player">
      <PlayerBody />
    </RoomShell>
  );
}

function PlayerBody() {
  const { conn } = useRoom();
  const game = useGameStore((s) => s.game);
  const me = useSessionStore((s) => s.session?.personId ?? null);
  const [answer, setAnswer] = useState('');
  const meInfo = game.players.find((p) => p.id === me);
  const q = game.question;

  return (
    <div className="grid gap-4 md:grid-cols-[1fr_280px]">
      <div className="flex flex-col gap-4">
        <Card title={game.stage === 'lobby' ? 'lobby' : `round ${game.roundIndex + 1} · ${game.roundName}`}>
          {game.stage === 'lobby' && (
            <Button variant="secondary" onClick={() => conn.send('READY')}>
              {meInfo?.ready ? 'not ready' : 'ready'}
            </Button>
          )}
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
              {q.rightAnswer && (
                <div className="mt-2 text-success">
                  answer: <b>{q.rightAnswer.text}</b>
                </div>
              )}
            </div>
          )}
          <TableGrid
            table={game.table}
            canPick={game.prompts.askChoose !== undefined}
            onPick={(theme, qq) => conn.send('CHOOSE_QUESTION', { theme, q: qq })}
          />
        </Card>
        <Card title="button">
          <BuzzerButton />
        </Card>
        {game.prompts.askAnswer && (
          <Card title="your answer">
            <form
              className="flex gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                conn.send('ANSWER', { text: answer });
                setAnswer('');
              }}
            >
              <Input
                autoFocus
                value={answer}
                onChange={(e) => {
                  setAnswer(e.target.value);
                  conn.send('ANSWER_DRAFT', { text: e.target.value });
                }}
              />
              <Button type="submit">answer</Button>
            </form>
          </Card>
        )}
        {game.prompts.askStake && (
          <Card title="stake">
            <div className="flex flex-wrap gap-2">
              {game.prompts.askStake.modes.map((m) => (
                <Button
                  key={m}
                  variant="secondary"
                  onClick={() =>
                    conn.send('SET_STAKE', {
                      stakeMode: m,
                      amount: m === 'stake' ? game.prompts.askStake!.min : undefined,
                    })
                  }
                >
                  {m}
                  {m === 'stake' ? ` ${game.prompts.askStake!.min}` : ''}
                </Button>
              ))}
            </div>
          </Card>
        )}
        {game.prompts.askDeleteTheme && (
          <Card title="delete a theme">
            <div className="flex flex-wrap gap-2">
              {game.prompts.askDeleteTheme.themes.map((ti) => (
                <Button key={ti} variant="secondary" onClick={() => conn.send('DELETE_THEME', { theme: ti })}>
                  {game.table.themes[ti]?.name ?? ti}
                </Button>
              ))}
            </div>
          </Card>
        )}
        {game.prompts.askSelectPlayer && (
          <Card title={`select a player (${game.prompts.askSelectPlayer.reason})`}>
            <div className="flex flex-wrap gap-2">
              {game.prompts.askSelectPlayer.candidates.map((id) => (
                <Button key={id} variant="secondary" onClick={() => conn.send('SELECT_PLAYER', { personId: id })}>
                  {game.players.find((p) => p.id === id)?.name ?? id}
                </Button>
              ))}
            </div>
          </Card>
        )}
        {game.prompts.askAppealVote && (
          <Card title={`appeal: "${game.prompts.askAppealVote.answer}"`}>
            <div className="flex gap-2">
              <Button onClick={() => conn.send('VOTE_APPEAL', { right: true })}>right</Button>
              <Button variant="danger" onClick={() => conn.send('VOTE_APPEAL', { right: false })}>
                wrong
              </Button>
            </div>
          </Card>
        )}
      </div>
      <div className="flex flex-col gap-4">
        <Card title="scores">
          <ScoreList players={game.players} scores={game.scores} chooserId={game.chooserId} meId={me} />
        </Card>
        <Card title="chat">
          <Chat />
        </Card>
      </div>
    </div>
  );
}
