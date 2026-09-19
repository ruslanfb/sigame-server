import { useQuery } from '@tanstack/react-query';
import { clsx } from 'clsx';
import { useEffect, useState, type ReactNode } from 'react';
import { getSystemInfo } from '../api/client.ts';
import { Brand } from '../components/Brand.tsx';
import { ConnectionBanner } from '../components/ConnectionBanner.tsx';
import { AnswerOptionsView, ContentView } from '../components/ContentView.tsx';
import { GameTable } from '../components/GameTable.tsx';
import { HexPlate } from '../components/HexPlate.tsx';
import { QRCode } from '../components/QRCode.tsx';
import { RoomCodePlates } from '../components/RoomCode.tsx';
import { ScoreStrip } from '../components/ScoreStrip.tsx';
import { TimerBar } from '../components/TimerBar.tsx';
import { joinUrlFor, shortUrl } from '../lib/joinUrl.ts';
import { fmtScore, QUESTION_TYPE_LABEL, ROLE_LABEL, roundTitle, STAKE_MODE_LABEL } from '../lib/labels.ts';
import { useBuzzerStore } from '../state/buzzer.ts';
import { useGameStore, type GameState } from '../state/game.ts';
import { personName, useRoomStore } from '../state/room.ts';
import type { Session } from '../state/session.ts';
import { RoomShell } from './RoomShell.tsx';

/** Big-screen view (viewer / TV): full screen, no inputs, cursor auto-hides. */
export function TablePage() {
  return <RoomShell page="table">{({ session }) => <TableBody session={session} />}</RoomShell>;
}

const CURSOR_HIDE_MS = 3000;

function TableBody({ session }: { session: Session }) {
  const game = useGameStore((s) => s.game);
  const persons = useRoomStore((s) => s.persons);
  const info = useRoomStore((s) => s.info);
  const pendingPresses = useBuzzerStore((s) => s.pendingPresses);
  const lastResult = useBuzzerStore((s) => s.lastResult);
  const [soundOn, setSoundOn] = useState(false);
  const names = (id: string | null | undefined) => game.players.find((pl) => pl.id === id)?.name ?? personName(persons, id);
  useCursorAutoHide();

  const q = game.question;
  const litIds = [...pendingPresses, ...(lastResult?.winnerId ? [lastResult.winnerId] : []), ...game.players.filter((pl) => pl.state === 'answering').map((pl) => pl.id)];
  const timers = Object.values(game.timers).filter((t) => t.kind !== 'round' && t.kind !== 'mediaFallback').slice(0, 2);

  return (
    <div className="fixed inset-0 flex flex-col overflow-hidden select-none">
      <ConnectionBanner className="text-base" />
      {!soundOn && (
        <button
          type="button"
          onClick={() => setSoundOn(true)}
          className="absolute right-4 bottom-4 z-20 rounded-md border border-border bg-surface/80 px-3 py-1.5 text-sm text-muted opacity-70 hover:opacity-100 hover:text-text"
        >
          Включить звук
        </button>
      )}
      {/* top bar */}
      <header className="flex items-center gap-6 px-10 pt-6 pb-2">
        <Brand size="sm" />
        <span className="text-xl text-muted">
          {game.stage === 'lobby' ? info?.name : roundTitle(game.roundIndex, game.roundName)}
        </span>
        <span className="flex-1" />
        {game.paused && <span className="font-display text-2xl text-warning">Пауза</span>}
        <div className="flex w-96 flex-col gap-1">
          {timers.map((t) => (
            <TimerBar key={t.id} timer={t} thin />
          ))}
        </div>
      </header>

      <main className="flex min-h-0 flex-1 flex-col items-center justify-center px-10">
        {game.stage === 'lobby' && <LobbyScreen session={session} />}
        {game.stage === 'roundIntro' && <Headline title={`Раунд ${game.roundIndex + 1}`} text={roundTitle(game.roundIndex, game.roundName).replace(/^Раунд \d+( · )?/, '')} />}
        {(game.stage === 'selecting' || game.stage === 'finalThemes') && (
          <div className="flex w-full max-w-7xl flex-col items-center gap-6">
            <GameTable table={game.table} size="tv" />
            <p className="text-2xl text-muted">
              {game.stage === 'finalThemes'
                ? `Тему убирает ${names(game.pending?.deciderId)}`
                : game.chooserId
                  ? `Выбирает ${names(game.chooserId)}`
                  : ''}
            </p>
          </div>
        )}
        {game.stage === 'question' && q && <QuestionScreen game={game} names={names} soundOn={soundOn} />}
        {game.stage === 'roundEnd' && <Headline title="Раунд окончен" text={game.roundName} />}
        {(game.stage === 'gameEnd' || game.ended) && <WinnerScreen game={game} names={names} />}
      </main>

      <footer className="px-10 pt-2 pb-6">
        {game.hasGame ? (
          <ScoreStrip players={game.players} scores={game.scores} deltas={game.deltas} chooserId={game.chooserId} litIds={litIds} size="tv" />
        ) : (
          <div className="flex flex-wrap justify-center gap-3">
            {persons
              .filter((p) => p.role !== 'viewer')
              .map((p) => (
                <span key={p.id} className={clsx('rounded-md border border-border bg-surface/80 px-4 py-2 text-xl', !p.connected && 'opacity-50')}>
                  {p.name} <span className="text-sm text-muted">{ROLE_LABEL[p.role]}</span>
                </span>
              ))}
          </div>
        )}
      </footer>
    </div>
  );
}

function LobbyScreen({ session }: { session: Session }) {
  const sys = useQuery({ queryKey: ['system-info'], queryFn: getSystemInfo });
  const info = useRoomStore((s) => s.info);
  const url = session.joinUrl ?? joinUrlFor(session.roomCode, sys.data?.joinUrls);
  return (
    <div className="flex items-center gap-16">
      <div className="flex flex-col items-center gap-6 text-center">
        <Brand size="lg" />
        <p className="text-2xl text-muted">{info?.name}</p>
        <RoomCodePlates code={session.roomCode} size="tv" />
        <p className="font-display text-3xl text-accent-2">{shortUrl(url)}</p>
      </div>
      <QRCode value={url} size={320} />
    </div>
  );
}

function QuestionScreen({ game, names, soundOn }: { game: GameState; names: (id: string | null | undefined) => string; soundOn: boolean }) {
  const q = game.question!;
  const paused = game.paused || q.contentState === 'paused';
  const answerer = q.answererId && !q.rightAnswer ? names(q.answererId) : null;
  return (
    <div className="flex h-full w-full max-w-7xl flex-col items-center gap-6">
      <div className="flex items-center gap-6">
        <HexPlate tone="gold" padding="px-8 py-2" className="font-display text-5xl tabular-nums" glow="gold">
          {fmtScore(q.price)}
        </HexPlate>
        <div>
          <p className="font-display text-4xl">{q.theme}</p>
          <p className="text-xl text-muted">{QUESTION_TYPE_LABEL[q.type] ?? q.type}</p>
        </div>
      </div>
      <div className="flex min-h-0 w-full flex-1 flex-col items-center justify-center">
        {q.rightAnswer ? (
          <div className="animate-pop flex flex-col items-center gap-4 text-center">
            <p className="text-2xl tracking-widest text-muted uppercase">Правильный ответ</p>
            <HexPlate tone="gold" padding="px-12 py-4" glow="gold" className="font-display max-w-6xl text-6xl">
              {q.rightAnswer.text}
            </HexPlate>
            {q.rightAnswer.comments && <p className="max-w-4xl text-2xl text-muted">{q.rightAnswer.comments}</p>}
            <ContentView content={q.content} phase="answer" size="tv" muted={!soundOn} />
          </div>
        ) : (
          <>
            <ContentView content={q.content} size="tv" muted={!soundOn} paused={paused} />
            {q.options && <AnswerOptionsView options={q.options} excluded={q.excludedOptions} size="tv" className="mt-6 max-w-6xl" />}
          </>
        )}
      </div>
      <div className="h-12 text-center text-3xl">
        {answerer && <span className="font-display text-accent">Отвечает {answerer}</span>}
        {!answerer && q.finalThinkMs !== null && !q.rightAnswer && <span className="text-muted">Игроки пишут ответы</span>}
        {!answerer && q.stakes && !q.rightAnswer && (
          <span className="text-muted">
            Торги · ставка {fmtScore(q.stakes.stake)} · {q.stakes.current ? `ход: ${names(q.stakes.current)}` : ''}
          </span>
        )}
        {!answerer && q.stakeLog.length > 0 && !q.stakes && !q.rightAnswer && (
          <span className="text-muted">
            {q.stakeLog.slice(-1).map((s) => `${names(s.personId)}: ${STAKE_MODE_LABEL[s.mode]}${s.mode === 'stake' ? ` ${fmtScore(s.amount)}` : ''}`)}
          </span>
        )}
      </div>
    </div>
  );
}

function WinnerScreen({ game, names }: { game: GameState; names: (id: string | null | undefined) => string }) {
  const sorted = [...game.players].sort((a, b) => (game.scores[b.id] ?? b.score) - (game.scores[a.id] ?? a.score));
  return (
    <div className="animate-pop flex flex-col items-center gap-8 text-center">
      <p className="text-3xl tracking-widest text-muted uppercase">{game.winnerId ? 'Победитель' : 'Игра окончена'}</p>
      {game.winnerId && (
        <HexPlate tone="gold" padding="px-16 py-6" glow="gold" className="font-display text-7xl">
          {names(game.winnerId)}
        </HexPlate>
      )}
      <ol className="flex flex-col gap-2 text-3xl">
        {sorted.map((p, i) => (
          <li key={p.id} className={clsx('flex gap-6', i === 0 && 'text-accent')}>
            <span className="w-10 text-right text-muted">{i + 1}.</span>
            <span className="w-96 text-left">{p.name}</span>
            <span className="font-display tabular-nums">{fmtScore(game.scores[p.id] ?? p.score)}</span>
          </li>
        ))}
      </ol>
    </div>
  );
}

function Headline({ title, text }: { title: string; text: ReactNode }) {
  return (
    <div className="animate-pop flex flex-col items-center gap-4 text-center">
      <p className="font-display text-gold-gradient text-8xl">{title}</p>
      <p className="text-4xl text-muted">{text}</p>
    </div>
  );
}

function useCursorAutoHide() {
  useEffect(() => {
    let timer = 0;
    const show = () => {
      document.documentElement.classList.remove('cursor-hidden');
      window.clearTimeout(timer);
      timer = window.setTimeout(() => document.documentElement.classList.add('cursor-hidden'), CURSOR_HIDE_MS);
    };
    show();
    window.addEventListener('mousemove', show, { passive: true });
    return () => {
      window.removeEventListener('mousemove', show);
      window.clearTimeout(timer);
      document.documentElement.classList.remove('cursor-hidden');
    };
  }, []);
}
