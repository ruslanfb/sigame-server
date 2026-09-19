import { clsx } from 'clsx';
import { useState, type ReactNode } from 'react';
import { useRoom } from '../app/roomContext.ts';
import { AnswerInput } from '../components/AnswerInput.tsx';
import { Button } from '../components/Button.tsx';
import { BuzzerButton } from '../components/BuzzerButton.tsx';
import { Card } from '../components/Card.tsx';
import { Chat } from '../components/Chat.tsx';
import { ConnDot, ConnectionBanner } from '../components/ConnectionBanner.tsx';
import { AnswerOptionsView, ContentView } from '../components/ContentView.tsx';
import { GameTable } from '../components/GameTable.tsx';
import { HexPlate } from '../components/HexPlate.tsx';
import { Modal } from '../components/Modal.tsx';
import { PersonsList } from '../components/PersonsList.tsx';
import { AppealVotePrompt, DeleteThemePrompt, SelectPlayerPrompt } from '../components/PromptDialogs.tsx';
import { ScoreStrip } from '../components/ScoreStrip.tsx';
import { StakeDialog } from '../components/StakeDialog.tsx';
import { TimerStack } from '../components/TimerBar.tsx';
import { fmtScore, QUESTION_TYPE_LABEL, roundTitle } from '../lib/labels.ts';
import { useBuzzerStore } from '../state/buzzer.ts';
import { useGameStore, type GameState } from '../state/game.ts';
import { personName, useRoomStore } from '../state/room.ts';
import type { Session } from '../state/session.ts';
import { RoomShell } from './RoomShell.tsx';

export function PlayerRoomPage() {
  return <RoomShell page="player">{({ session, leave }) => <PlayerBody session={session} leave={leave} />}</RoomShell>;
}

const BUTTON_TYPES = new Set(['simple', 'custom']);

function PlayerBody({ session, leave }: { session: Session; leave: () => void }) {
  const { conn } = useRoom();
  const game = useGameStore((s) => s.game);
  const persons = useRoomStore((s) => s.persons);
  const info = useRoomStore((s) => s.info);
  const chatCount = useRoomStore((s) => s.chat.length);
  const arm = useBuzzerStore((s) => s.arm);
  const pendingPresses = useBuzzerStore((s) => s.pendingPresses);
  const lastResult = useBuzzerStore((s) => s.lastResult);
  const [chatOpen, setChatOpen] = useState(false);
  const [seenChat, setSeenChat] = useState(0);
  const me = session.personId;
  const meInfo = game.players.find((p) => p.id === me);
  const q = game.question;
  const p = game.prompts;
  const names = (id: string | null | undefined) => (game.players.find((pl) => pl.id === id)?.name ?? personName(persons, id));

  const litIds = [...pendingPresses, ...(lastResult?.winnerId ? [lastResult.winnerId] : []), ...game.players.filter((pl) => pl.state === 'answering').map((pl) => pl.id)];

  return (
    <div className="flex min-h-dvh flex-col">
      <ConnectionBanner />
      <header className="flex items-center gap-2 px-3 pt-2 pb-1">
        <span className="font-display text-lg text-accent">{session.roomCode}</span>
        <span className="truncate text-sm text-muted">{info?.name}</span>
        <span className="flex-1" />
        <span className="truncate text-sm font-semibold">{session.name}</span>
        <ConnDot />
        <Button
          size="sm"
          variant="ghost"
          onClick={() => {
            setChatOpen(true);
            setSeenChat(chatCount);
          }}
          aria-label="Чат"
          className="relative"
        >
          Чат
          {chatCount > seenChat && <span className="absolute -top-0.5 -right-0.5 h-2 w-2 rounded-full bg-accent" />}
        </Button>
        <Button size="sm" variant="ghost" onClick={leave} aria-label="Выйти из комнаты">
          Выйти
        </Button>
      </header>
      <div className="px-3">
        <TimerStack timers={game.timers} thin personId={me} />
      </div>
      {game.hasGame && (
        <div className="px-3 pt-2">
          <ScoreStrip players={game.players} scores={game.scores} deltas={game.deltas} chooserId={game.chooserId} meId={me} litIds={litIds} size="sm" />
        </div>
      )}
      <main className="flex flex-1 flex-col gap-3 p-3">
        {game.stage === 'lobby' && (
          <>
            <Card title="Ждём начала игры" action={<span className="text-xs text-muted">{info?.packName}</span>}>
              <PersonsList persons={persons} players={game.players} meId={me} />
            </Card>
            <StatusLine>Ведущий начнёт игру, когда все соберутся</StatusLine>
          </>
        )}

        {game.stage === 'roundIntro' && <BigNotice title={roundTitle(game.roundIndex, game.roundName)} text="" />}

        {game.stage === 'selecting' && (
          <>
            <StatusLine>
              {p.askChoose ? <span className="text-accent">Ваш ход — выбирайте вопрос</span> : `Выбирает ${names(game.chooserId)}`}
            </StatusLine>
            <GameTable
              table={game.table}
              size="compact"
              canPick={p.askChoose !== undefined}
              onPick={(theme, qq) => conn.send('CHOOSE_QUESTION', { theme, q: qq })}
            />
          </>
        )}

        {game.stage === 'finalThemes' && (
          <>
            <StatusLine>{p.askDeleteTheme ? <span className="text-accent">Уберите тему</span> : `Тему убирает ${names(game.pending?.deciderId)}`}</StatusLine>
            <GameTable table={game.table} size="compact" />
          </>
        )}

        {game.stage === 'question' && q && (
          <QuestionPhase
            game={game}
            me={me}
            names={names}
            onMediaEnded={() => conn.send('MEDIA_COMPLETED')}
            showButton={BUTTON_TYPES.has(q.type) || arm.phase !== 'idle'}
            paused={game.paused || q.contentState === 'paused'}
          />
        )}

        {game.stage === 'roundEnd' && <BigNotice title="Раунд окончен" text={roundTitle(game.roundIndex, game.roundName)} />}

        {(game.stage === 'gameEnd' || game.ended) && (
          <BigNotice
            title={game.winnerId ? (game.winnerId === me ? 'Вы победили!' : `Победитель: ${names(game.winnerId)}`) : 'Игра окончена'}
            text={meInfo ? `Ваш счёт: ${fmtScore(game.scores[me] ?? meInfo.score)}` : ''}
          />
        )}

        {/* ---- prompts for me ---- */}
        {p.askAnswer && q && (
          <PromptCard title={p.askAnswer.hidden ? 'Письменный ответ' : 'Ваш ответ'}>
            <AnswerInput
              ask={p.askAnswer}
              options={q.options}
              excluded={q.excludedOptions}
              onAnswer={(a) => conn.send('ANSWER', a)}
              onDraft={(text) => conn.send('ANSWER_DRAFT', { text })}
            />
          </PromptCard>
        )}
        {p.askStake && (
          <PromptCard title="Ставка">
            <StakeDialog ask={p.askStake} myScore={game.scores[me]} onStake={(mode, amount) => conn.send('SET_STAKE', { stakeMode: mode, amount })} />
          </PromptCard>
        )}
        {p.askDeleteTheme && (
          <PromptCard title="Финал">
            <DeleteThemePrompt ask={p.askDeleteTheme} table={game.table} onDelete={(theme) => conn.send('DELETE_THEME', { theme })} />
          </PromptCard>
        )}
        <Modal open={p.askSelectPlayer !== undefined} title="Выберите игрока" sheet tone="gold">
          {p.askSelectPlayer && <SelectPlayerPrompt ask={p.askSelectPlayer} players={game.players} onSelect={(id) => conn.send('SELECT_PLAYER', { personId: id })} />}
        </Modal>
        <Modal open={p.askAppealVote !== undefined} title="Апелляция" sheet tone="gold">
          {p.askAppealVote && (
            <AppealVotePrompt ask={p.askAppealVote} answererName={names(p.askAppealVote.answererId)} onVote={(right) => conn.send('VOTE_APPEAL', { right })} />
          )}
        </Modal>
        <AppealButtons game={game} me={me} names={names} onAppeal={(f) => conn.send('APPELLATE', { for: f })} />
        {game.appeal && (
          <StatusLine>
            Апелляция по ответу {names(game.appeal.answererId)}: за {game.appeal.for} · против {game.appeal.against} · ждём {game.appeal.pending.length}
          </StatusLine>
        )}
        {game.paused && <StatusLine className="text-warning">Пауза</StatusLine>}
      </main>
      <Modal open={chatOpen} title="Чат" sheet onClose={() => setChatOpen(false)} closeLabel="Закрыть">
        <Chat maxHeight="max-h-[50vh]" />
      </Modal>
    </div>
  );
}

function QuestionPhase({
  game,
  me,
  names,
  onMediaEnded,
  showButton,
  paused,
}: {
  game: GameState;
  me: string;
  names: (id: string | null | undefined) => string;
  onMediaEnded: () => void;
  showButton: boolean;
  paused: boolean;
}) {
  const q = game.question!;
  const p = game.prompts;
  const answering = q.answererId && q.answererId !== me && !q.rightAnswer;
  const myTurn = p.askAnswer !== undefined;
  return (
    <>
      <div className="flex items-center gap-3">
        <HexPlate tone="gold" padding="px-4 py-1" className="font-display shrink-0 text-2xl tabular-nums">
          {fmtScore(q.price)}
        </HexPlate>
        <div className="min-w-0 flex-1">
          <p className="truncate font-semibold">{q.theme}</p>
          <p className="text-xs text-muted">{QUESTION_TYPE_LABEL[q.type] ?? q.type}</p>
        </div>
      </div>
      {q.rightAnswer ? (
        <Card className="border-accent/60">
          <p className="text-xs font-bold tracking-widest text-muted uppercase">Правильный ответ</p>
          <p className="font-display mt-1 text-2xl text-accent">{q.rightAnswer.text}</p>
          {q.rightAnswer.comments && <p className="mt-1 text-sm text-muted">{q.rightAnswer.comments}</p>}
          <ContentView content={q.content} phase="answer" size="sm" muted className="mt-2" />
        </Card>
      ) : (
        <Card className="min-h-24">
          <ContentView content={q.content} size="md" onMediaEnded={onMediaEnded} paused={paused} />
          {q.content.length === 0 && <p className="text-center text-sm text-muted">Слушайте ведущего…</p>}
          {q.options && !myTurn && <AnswerOptionsView options={q.options} excluded={q.excludedOptions} size="sm" className="mt-3" />}
        </Card>
      )}
      {q.finalThinkMs !== null && !p.askAnswer && !q.rightAnswer && <StatusLine>Все пишут ответы…</StatusLine>}
      {answering && <StatusLine className="text-accent">Отвечает {names(q.answererId)}</StatusLine>}
      {showButton && !myTurn && !q.rightAnswer && q.finalThinkMs === null && <BuzzerButton className="my-2" />}
    </>
  );
}

function AppealButtons({
  game,
  me,
  names,
  onAppeal,
}: {
  game: GameState;
  me: string;
  names: (id: string | null | undefined) => string;
  onAppeal: (forMe: boolean) => void;
}) {
  const v = game.lastValidation;
  if (!v || !game.rules?.useAppellations || game.appeal) return null;
  if (v.personId === me && !v.right) {
    return (
      <Button variant="secondary" size="lg" onClick={() => onAppeal(true)}>
        Я прав! Апелляция
      </Button>
    );
  }
  if (v.personId !== me && v.right) {
    return (
      <Button variant="ghost" size="md" onClick={() => onAppeal(false)}>
        Не согласен с ответом {names(v.personId)}
      </Button>
    );
  }
  return null;
}

function PromptCard({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Card title={title} className="animate-pop border-accent shadow-glow-gold">
      {children}
    </Card>
  );
}

function StatusLine({ children, className }: { children: ReactNode; className?: string }) {
  return <p className={clsx('text-center text-sm font-semibold text-muted', className)}>{children}</p>;
}

function BigNotice({ title, text }: { title: string; text: string }) {
  return (
    <div className="animate-pop flex flex-1 flex-col items-center justify-center gap-2 py-10 text-center">
      <p className="font-display text-gold-gradient text-4xl">{title}</p>
      {text && <p className="text-lg text-muted">{text}</p>}
    </div>
  );
}
