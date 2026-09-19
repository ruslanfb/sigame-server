import { useQuery } from '@tanstack/react-query';
import { clsx } from 'clsx';
import { useState, type ReactNode } from 'react';
import { ApiError, getSystemInfo, listBuzzerPresets, updateRoomSettings } from '../api/client.ts';
import { useRoom } from '../app/roomContext.ts';
import { AnswerInput } from '../components/AnswerInput.tsx';
import { Button } from '../components/Button.tsx';
import { Card } from '../components/Card.tsx';
import { Chat } from '../components/Chat.tsx';
import { ConnDot, ConnectionBanner } from '../components/ConnectionBanner.tsx';
import { AnswerOptionsView, ContentView } from '../components/ContentView.tsx';
import { GameTable } from '../components/GameTable.tsx';
import { HexPlate } from '../components/HexPlate.tsx';
import { Input, Select, Toggle } from '../components/Input.tsx';
import { PersonsList } from '../components/PersonsList.tsx';
import { SelectPlayerPrompt } from '../components/PromptDialogs.tsx';
import { QRCode } from '../components/QRCode.tsx';
import { RoomCodePlates } from '../components/RoomCode.tsx';
import { ScoreStrip } from '../components/ScoreStrip.tsx';
import { StakeDialog } from '../components/StakeDialog.tsx';
import { TimerBar } from '../components/TimerBar.tsx';
import { joinUrlFor, shortUrl } from '../lib/joinUrl.ts';
import {
  apiErrorText,
  fmtScore,
  QUALITY_LABEL,
  QUESTION_TYPE_LABEL,
  SOURCE_LABEL,
  STAGE_LABEL,
  STAKE_MODE_LABEL,
  SUB_LABEL,
  TRUST_LABEL,
  VALIDATION_SOURCE_LABEL,
} from '../lib/labels.ts';
import { useNow } from '../lib/useNow.ts';
import { useBuzzerStore } from '../state/buzzer.ts';
import { useGameStore, type GameState } from '../state/game.ts';
import { personName, useRoomStore } from '../state/room.ts';
import type { Session } from '../state/session.ts';
import { toastError, useToastStore } from '../state/toast.ts';
import type { AskValidatePayload, TrustLevel } from '../ws/types.ts';
import { RoomShell } from './RoomShell.tsx';

interface RankingEntry {
  playerId: string;
  reactionMs: number;
  errMs?: number;
  rttMs?: number;
  uMs?: number;
  source: string;
  flags?: string[];
}

export function HostRoomPage() {
  return <RoomShell page="host">{({ session, leave }) => <HostBody session={session} leave={leave} />}</RoomShell>;
}

function HostBody({ session, leave }: { session: Session; leave: () => void }) {
  const { conn } = useRoom();
  const game = useGameStore((s) => s.game);
  const persons = useRoomStore((s) => s.persons);
  const info = useRoomStore((s) => s.info);
  const [editTable, setEditTable] = useState(false);
  const me = session.personId;
  const q = game.question;
  const names = (id: string | null | undefined) => game.players.find((pl) => pl.id === id)?.name ?? personName(persons, id);
  const lobby = game.stage === 'lobby';

  return (
    <div className="flex min-h-dvh flex-col">
      <ConnectionBanner />
      <header className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-2">
        <RoomCodePlates code={session.roomCode} />
        <div className="min-w-0">
          <p className="truncate font-semibold">{info?.name ?? '…'}</p>
          <p className="truncate text-xs text-muted">
            {info?.packName} · {session.name} · {session.isHost ? 'хост' : 'ведущий'}
          </p>
        </div>
        <span className="rounded-full border border-border px-2 py-0.5 text-xs text-muted">
          {STAGE_LABEL[game.stage]}
          {game.sub ? ` · ${SUB_LABEL[game.sub]}` : ''}
          {game.stage !== 'lobby' ? ` · раунд ${game.roundIndex + 1}` : ''}
        </span>
        <span className="flex-1" />
        <Controls game={game} lobby={lobby} />
        <ConnDot />
        <Button variant="ghost" size="sm" onClick={leave}>
          Выйти
        </Button>
      </header>

      {lobby ? (
        <main className="grid flex-1 gap-4 p-4 lg:grid-cols-[1fr_360px_360px]">
          <JoinInfo session={session} />
          <div className="flex flex-col gap-4">
            <Card title="Участники">
              <PersonsList persons={persons} players={game.players} meId={me} onKick={(id, ban) => conn.send('KICK', { personId: id, ban })} />
            </Card>
            <Settings session={session} />
          </div>
          <div className="flex flex-col gap-4">
            <Card title="Чат">
              <Chat maxHeight="max-h-80" />
            </Card>
          </div>
        </main>
      ) : (
        <main className="grid flex-1 gap-4 p-4 lg:grid-cols-[minmax(0,1fr)_360px_300px]">
          <div className="flex min-w-0 flex-col gap-4">
            <TimersPanel game={game} />
            {q ? (
              <QuestionPanel game={game} names={names} />
            ) : (
              <Card title={STAGE_LABEL[game.stage]}>
                {game.stage === 'selecting' && (
                  <p className="text-sm text-muted">
                    Выбирает <b className="text-text">{names(game.chooserId)}</b>
                    {game.rules?.oral ? ' — в устном режиме вы можете выбрать за игрока' : ''}
                  </p>
                )}
                {game.stage === 'finalThemes' && (
                  <p className="text-sm text-muted">
                    Тему убирает <b className="text-text">{names(game.pending?.deciderId)}</b>
                  </p>
                )}
                {game.stage === 'roundIntro' && <p className="font-display text-2xl">{game.roundName}</p>}
                {game.stage === 'roundEnd' && <p className="text-sm text-muted">Раунд окончен — нажмите «Дальше»</p>}
                {(game.stage === 'gameEnd' || game.ended) && (
                  <p className="font-display text-2xl text-accent">{game.winnerId ? `Победитель: ${names(game.winnerId)}` : 'Игра окончена'}</p>
                )}
              </Card>
            )}
            <Card
              title="Таблица"
              action={<Toggle checked={editTable} onChange={setEditTable} label={<span className="text-xs text-muted">Правка ячеек</span>} />}
            >
              <GameTable
                table={game.table}
                size="full"
                active={q && !q.ended ? { themeIndex: q.themeIndex, questionIndex: q.questionIndex } : null}
                canPick={game.stage === 'selecting'}
                onPick={(theme, qq) => conn.send('CHOOSE_QUESTION', { theme, q: qq })}
                editMode={editTable}
                onToggle={(theme, qq) => conn.send('TOGGLE', { theme, q: qq })}
                deletableThemes={game.prompts.askDeleteTheme?.themes}
                onDeleteTheme={(theme) => conn.send('DELETE_THEME', { theme })}
              />
            </Card>
          </div>
          <div className="flex min-w-0 flex-col gap-4">
            <ValidationPanel game={game} names={names} />
            <AnswersPanel game={game} names={names} />
            <BuzzerPanel game={game} names={names} />
            <AppealPanel game={game} names={names} />
          </div>
          <div className="flex min-w-0 flex-col gap-4">
            <PlayersPanel game={game} names={names} />
            <Card title="Участники">
              <PersonsList persons={persons} meId={me} onKick={(id, ban) => conn.send('KICK', { personId: id, ban })} />
            </Card>
            <Settings session={session} />
            <Card title="Чат">
              <Chat />
            </Card>
          </div>
        </main>
      )}
    </div>
  );
}

// ---- header controls ----------------------------------------------------------

function Controls({ game, lobby }: { game: GameState; lobby: boolean }) {
  const { conn } = useRoom();
  const persons = useRoomStore((s) => s.persons);
  const [round, setRound] = useState(1);
  if (lobby) {
    // Before START there is no engine: seats come from ROOM_PERSONS, not PLAYERS.
    const playerCount = persons.filter((p) => p.role === 'player').length;
    return (
      <Button variant="gold" size="lg" onClick={() => conn.send('START')} disabled={playerCount === 0} title={playerCount === 0 ? 'Нет игроков' : undefined}>
        Начать игру
      </Button>
    );
  }
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <Button variant={game.paused ? 'gold' : 'secondary'} size="sm" onClick={() => conn.send('PAUSE', { on: !game.paused })}>
        {game.paused ? 'Продолжить' : 'Пауза'}
      </Button>
      <Button variant="primary" size="sm" onClick={() => conn.send('NEXT')} title="Продолжить с точки ожидания">
        Дальше
      </Button>
      <Button variant="secondary" size="sm" onClick={() => conn.send('MOVE', { dir: 1 })} title="Пропустить шаг">
        Пропустить
      </Button>
      <Button variant="secondary" size="sm" onClick={() => conn.send('MOVE', { dir: -1 })} title="Вернуть вопрос в таблицу">
        Вернуть вопрос
      </Button>
      <Button variant="secondary" size="sm" onClick={() => conn.send('MOVE', { dir: 2 })}>
        След. раунд
      </Button>
      <Button variant="secondary" size="sm" onClick={() => conn.send('MOVE', { dir: -2 })}>
        Пред. раунд
      </Button>
      <span className="flex items-center gap-1">
        <Input type="number" min={1} value={round} onChange={(e) => setRound(Math.max(1, Number(e.target.value) || 1))} className="w-14 py-1 text-xs" aria-label="Номер раунда" />
        <Button variant="secondary" size="sm" onClick={() => conn.send('MOVE', { dir: 3, round: round - 1 })}>
          К раунду
        </Button>
      </span>
    </div>
  );
}

// ---- lobby: join info + QR --------------------------------------------------------

function JoinInfo({ session }: { session: Session }) {
  const sys = useQuery({ queryKey: ['system-info'], queryFn: getSystemInfo, enabled: !session.joinUrl });
  const url = session.joinUrl ?? joinUrlFor(session.roomCode, sys.data?.joinUrls);
  const [copied, setCopied] = useState(false);
  return (
    <Card className="flex flex-col items-center gap-4 text-center">
      <p className="text-xs font-bold tracking-widest text-muted uppercase">Код комнаты</p>
      <RoomCodePlates code={session.roomCode} size="lg" />
      <QRCode value={url} size={220} />
      <p className="font-display text-xl break-all text-accent-2">{shortUrl(url)}</p>
      <Button
        variant="secondary"
        size="sm"
        onClick={() => {
          navigator.clipboard?.writeText(url).then(() => setCopied(true)).catch(() => undefined);
          setTimeout(() => setCopied(false), 1500);
        }}
      >
        {copied ? 'Скопировано' : 'Скопировать ссылку'}
      </Button>
      <p className="max-w-sm text-sm text-muted">
        Игроки открывают ссылку или вводят код на странице входа. Табло для телевизора — та же ссылка, роль «Зритель».
      </p>
    </Card>
  );
}

// ---- settings (buzzer preset, join mode) ----------------------------------------

function Settings({ session }: { session: Session }) {
  const info = useRoomStore((s) => s.info);
  const presets = useQuery({ queryKey: ['buzzer-presets'], queryFn: listBuzzerPresets });
  const [busy, setBusy] = useState(false);
  if (!session.hostToken || !info) return null;
  const patch = async (body: Parameters<typeof updateRoomSettings>[2]) => {
    setBusy(true);
    try {
      await updateRoomSettings(session.roomCode, session.hostToken!, body);
      useToastStore.getState().push({ kind: 'success', text: 'Настройки обновлены' });
    } catch (err) {
      toastError(err instanceof ApiError ? apiErrorText(err.status, err.code, err.message) : String(err));
    } finally {
      setBusy(false);
    }
  };
  const presetList = presets.data?.presets ?? [];
  const currentPreset = presetList.find((p) => p.settings.netProfile === info.buzzer.netProfile && p.settings.mode === info.buzzer.mode)?.name ?? '';
  return (
    <Card title="Настройки комнаты">
      <div className="flex flex-col gap-3 text-sm">
        <label className="flex flex-col gap-1">
          <span className="text-xs text-muted">Кнопка (пресет)</span>
          <Select value={currentPreset} disabled={busy} onChange={(e) => e.target.value && patch({ buzzerPreset: e.target.value as never })}>
            <option value="">— {info.buzzer.mode} / {info.buzzer.netProfile} —</option>
            {presetList.map((p) => (
              <option key={p.name} value={p.name} disabled={!p.supported}>
                {p.title}
              </option>
            ))}
          </Select>
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-xs text-muted">Вход в комнату</span>
          <Select value={info.joinMode} disabled={busy} onChange={(e) => patch({ joinMode: e.target.value as never })}>
            <option value="any">Открыт для всех</option>
            <option value="viewersOnly">Только зрители</option>
            <option value="closed">Закрыт</option>
          </Select>
        </label>
        <p className="text-xs text-muted">
          {info.hasPassword ? 'Комната с паролем' : 'Без пароля'} · до {info.maxPlayers} игроков · пинг игрокам {info.buzzer.showPing ? 'виден' : 'скрыт'}
        </p>
      </div>
    </Card>
  );
}

// ---- timers ----------------------------------------------------------------------

function TimersPanel({ game }: { game: GameState }) {
  const list = Object.values(game.timers).filter((t) => t.kind !== 'mediaFallback');
  if (list.length === 0) return null;
  return (
    <div className="grid gap-2 sm:grid-cols-2">
      {list.map((t) => (
        <TimerBar key={t.id} timer={t} tone={t.kind === 'round' ? 'blue' : 'gold'} />
      ))}
    </div>
  );
}

// ---- question ----------------------------------------------------------------------

function QuestionPanel({ game, names }: { game: GameState; names: (id: string | null | undefined) => string }) {
  const { conn } = useRoom();
  const q = game.question!;
  const hint = q.hint;
  return (
    <Card
      title={
        <span className="flex items-center gap-2">
          <HexPlate tone="gold" padding="px-3 py-0.5" className="font-display text-lg tabular-nums">
            {fmtScore(q.price)}
          </HexPlate>
          <span className="normal-case tracking-normal text-text">{q.theme}</span>
          <span className="font-normal normal-case tracking-normal">· {QUESTION_TYPE_LABEL[q.type] ?? q.type}</span>
          {q.answererId && <span className="font-normal normal-case tracking-normal text-accent">· отвечает {names(q.answererId)}</span>}
        </span>
      }
      action={
        <span className="flex gap-1">
          {game.sub === 'content' && (
            <Button size="sm" variant="secondary" onClick={() => conn.send('NEXT')}>
              Дальше
            </Button>
          )}
        </span>
      }
    >
      <div className="grid gap-4 md:grid-cols-[1fr_280px]">
        <div className="flex min-w-0 flex-col gap-3">
          <ContentView content={q.content} size="md" muted autoplay={false} />
          {q.options && <AnswerOptionsView options={q.options} excluded={q.excludedOptions} size="sm" />}
          {q.rightAnswer && (
            <div className="rounded-md border border-success/50 bg-success/10 p-2 text-sm">
              Ответ показан: <b>{q.rightAnswer.text}</b>
            </div>
          )}
          {q.stakes && (
            <p className="text-xs text-muted">
              Торги: ставка {fmtScore(q.stakes.stake)} · лидер {names(q.stakes.leader)} · ход {names(q.stakes.current)}
            </p>
          )}
          {q.stakeLog.length > 0 && (
            <p className="text-xs text-muted">
              {q.stakeLog.map((s, i) => (
                <span key={i} className="mr-2">
                  {names(s.personId)}: {STAKE_MODE_LABEL[s.mode]}
                  {s.mode === 'stake' || s.mode === 'nominal' ? ` ${fmtScore(s.amount)}` : ''}
                </span>
              ))}
            </p>
          )}
        </div>
        <div className="flex flex-col gap-2 rounded-md border border-accent/40 bg-bg-2/60 p-3 text-sm">
          <p className="text-xs font-bold tracking-widest text-accent uppercase">Для ведущего</p>
          {hint?.rights?.length ? (
            <div>
              <p className="text-xs text-muted">Верные ответы</p>
              <ul className="font-semibold">
                {hint.rights.map((r, i) => (
                  <li key={i}>{r}</li>
                ))}
              </ul>
            </div>
          ) : (
            <p className="text-xs text-muted">Подсказки скрыты настройками</p>
          )}
          {hint?.wrongs?.length ? (
            <div>
              <p className="text-xs text-muted">Неверные</p>
              <ul className="text-danger">
                {hint.wrongs.map((r, i) => (
                  <li key={i}>{r}</li>
                ))}
              </ul>
            </div>
          ) : null}
          {hint?.showmanComments && <p className="text-xs text-muted italic">{hint.showmanComments}</p>}
          {hint?.comments && <p className="text-xs text-muted">{hint.comments}</p>}
        </div>
      </div>
    </Card>
  );
}

// ---- validation ----------------------------------------------------------------------

function ValidationPanel({ game, names }: { game: GameState; names: (id: string | null | undefined) => string }) {
  const { conn } = useRoom();
  const v = game.prompts.askValidate;
  const oral = game.prompts.askAnswer?.oral ? game.prompts.askAnswer : null;
  const q = game.question;
  const ai = v ? q?.aiSuggestions[v.personId] : undefined;
  // Keyed on the prompt so the auto-verdict countdown restarts with every ASK_VALIDATE.
  if (v) return <Validate key={`${v.personId}:${v.answer}:${v.preview}`} v={v} names={names} ai={ai} />;
  if (oral && oral.personId && oral.personId !== '') {
    return (
      <Card title="Устный ответ" className="border-accent shadow-glow-gold">
        <p className="font-display text-2xl">{names(oral.personId)} отвечает вслух</p>
        <div className="mt-3 flex gap-2">
          <Button variant="success" size="lg" className="flex-1" onClick={() => conn.send('VALIDATE', { personId: oral.personId, right: true })}>
            Верно
          </Button>
          <Button variant="danger" size="lg" className="flex-1" onClick={() => conn.send('VALIDATE', { personId: oral.personId, right: false })}>
            Неверно
          </Button>
        </div>
        {q && (
          <div className="mt-3">
            <AnswerInput ask={oral} options={q.options} excluded={q.excludedOptions} onAnswer={(a) => conn.send('ANSWER', { ...a, personId: oral.personId })} />
          </div>
        )}
      </Card>
    );
  }
  if (game.prompts.askSelectPlayer) {
    return (
      <Card title="Выбор игрока" className="border-accent shadow-glow-gold">
        <SelectPlayerPrompt ask={game.prompts.askSelectPlayer} players={game.players} onSelect={(id) => conn.send('SELECT_PLAYER', { personId: id })} />
      </Card>
    );
  }
  if (game.prompts.askStake) {
    return (
      <Card title={`Ставка за ${names(game.prompts.askStake.personId)}`} className="border-accent shadow-glow-gold">
        <StakeDialog ask={game.prompts.askStake} onStake={(mode, amount) => conn.send('SET_STAKE', { stakeMode: mode, amount })} />
      </Card>
    );
  }
  if (game.prompts.askChoose && game.rules?.oral) {
    return (
      <Card title="Выбор вопроса" className="border-accent/60">
        <p className="text-sm text-muted">Устный режим: выберите вопрос в таблице за {names(game.chooserId)}</p>
      </Card>
    );
  }
  return (
    <Card title="Проверка ответа">
      <p className="text-sm text-muted">Нет ответов на проверке</p>
    </Card>
  );
}

function Validate({
  v,
  names,
  ai,
}: {
  v: AskValidatePayload;
  names: (id: string | null | undefined) => string;
  ai?: { right: boolean; factor: number; reason?: string };
}) {
  const { conn } = useRoom();
  const now = useNow(v.autoAfterMs ? 250 : 0);
  const [askedAt] = useState(() => performance.now());
  const remainingMs = v.autoAfterMs ? Math.max(0, v.autoAfterMs - (now - askedAt)) : null;
  const send = (right: boolean, factor?: number) => conn.send('VALIDATE', { personId: v.personId, right, factor });
  return (
    <Card
      title={v.preview ? 'Предварительная проверка' : 'Проверка ответа'}
      className="animate-pop border-accent shadow-glow-gold"
      action={remainingMs !== null ? <span className="font-mono text-xs text-muted">авто через {Math.ceil(remainingMs / 1000)} с</span> : null}
    >
      <p className="text-sm text-muted">{names(v.personId)} отвечает:</p>
      <p className="font-display my-2 text-3xl break-words text-accent">«{v.answer || '—'}»</p>
      {ai && (
        <div className={clsx('mb-2 rounded-md border px-2 py-1 text-xs', ai.right ? 'border-success/60 bg-success/10' : 'border-danger/60 bg-danger/10')}>
          <b>ИИ предлагает: {ai.right ? (ai.factor && ai.factor !== 1 ? `частично (×${ai.factor})` : 'верно') : 'неверно'}</b>
          {ai.reason ? ` — ${ai.reason}` : ''}
        </div>
      )}
      <div className="grid grid-cols-2 gap-2 text-xs">
        <div>
          <p className="text-muted">Верные</p>
          <ul className="font-semibold">
            {v.rights.map((r, i) => (
              <li key={i}>{r}</li>
            ))}
            {v.rights.length === 0 && <li className="text-muted">—</li>}
          </ul>
        </div>
        <div>
          <p className="text-muted">Неверные</p>
          <ul className="text-danger">
            {(v.wrongs ?? []).map((r, i) => (
              <li key={i}>{r}</li>
            ))}
            {!v.wrongs?.length && <li className="text-muted">—</li>}
          </ul>
        </div>
      </div>
      <div className="mt-3 grid grid-cols-2 gap-2">
        <Button variant="success" size="lg" onClick={() => send(true)} autoFocus>
          Верно
        </Button>
        <Button variant="danger" size="lg" onClick={() => send(false)}>
          Неверно
        </Button>
        {v.allowFactor && (
          <Button variant="secondary" size="md" className="col-span-2" onClick={() => send(true, 0.5)} title="Засчитать половину">
            Частично ½
          </Button>
        )}
      </div>
    </Card>
  );
}

// ---- answers stream ---------------------------------------------------------------------

function AnswersPanel({ game, names }: { game: GameState; names: (id: string | null | undefined) => string }) {
  const q = game.question;
  if (!q) return null;
  const drafts = Object.entries(q.drafts).filter(([id]) => !(id in q.answers));
  const answers = Object.entries(q.answers);
  if (drafts.length === 0 && answers.length === 0 && q.validations.length === 0) return null;
  return (
    <Card title="Ответы">
      <ul className="flex flex-col gap-1 text-sm">
        {drafts.map(([id, text]) => (
          <li key={`d${id}`} className="text-muted">
            <b className="text-text">{names(id)}</b> печатает: <span className="italic">{text}</span>
            <span className="animate-pulse">▍</span>
          </li>
        ))}
        {answers.map(([id, a]) => {
          const val = q.validations.find((x) => x.personId === id);
          return (
            <li key={`a${id}`} className="flex items-center gap-2">
              <b>{names(id)}</b>
              <span className="flex-1 truncate">{a.text ?? a.optionLabel ?? (a.number !== undefined ? String(a.number) : a.clientRight !== undefined ? (a.clientRight ? 'верно (сам)' : 'неверно (сам)') : '—')}</span>
              {val && (
                <span className={clsx('text-xs font-semibold', val.right ? 'text-success' : 'text-danger')}>
                  {val.right ? (val.factor && val.factor !== 1 ? `×${val.factor}` : 'верно') : 'неверно'} · {VALIDATION_SOURCE_LABEL[val.source] ?? val.source}
                </span>
              )}
            </li>
          );
        })}
      </ul>
    </Card>
  );
}

// ---- buzzer panel -----------------------------------------------------------------

function BuzzerPanel({ game, names }: { game: GameState; names: (id: string | null | undefined) => string }) {
  const { conn } = useRoom();
  const lastResult = useBuzzerStore((s) => s.lastResult);
  const pending = useBuzzerStore((s) => s.pendingPresses);
  const conns = useBuzzerStore((s) => s.connQuality);
  const lockouts = useBuzzerStore((s) => s.lockouts);
  const arm = useBuzzerStore((s) => s.arm);
  const ranking = (lastResult?.ranking ?? []) as RankingEntry[];
  const players = game.players.filter((p) => !p.kicked);
  return (
    <Card
      title="Кнопка"
      action={
        <Button size="sm" variant="secondary" onClick={() => conn.send('BUTTON_REOPEN', { armId: arm.armId ?? lastResult?.armId })} title="Переоткрыть кнопку для того же вопроса">
          Переоткрыть
        </Button>
      }
    >
      {lastResult && (
        <div className="mb-2 rounded-md border border-border bg-bg-2/60 p-2 text-xs">
          <p className="font-semibold">
            {lastResult.kind === 'nobody' ? 'Никто не нажал' : lastResult.kind === 'allPlay' ? 'Отвечают все' : `Первый: ${names(lastResult.winnerId)}`}
            {lastResult.contested ? ' · спорно' : ''} · правило {lastResult.rule}
            {lastResult.marginMs ? ` · отрыв ${lastResult.marginMs.toFixed(0)} мс` : ''}
          </p>
          {ranking.length > 0 && (
            <ol className="mt-1 flex flex-col gap-0.5">
              {ranking.map((r, i) => (
                <li key={r.playerId} className={clsx('flex gap-2', i === 0 && 'text-accent')}>
                  <span className="w-4 text-right">{i + 1}.</span>
                  <span className="flex-1 truncate">{names(r.playerId)}</span>
                  <span className="font-mono tabular-nums">{r.reactionMs.toFixed(0)} мс</span>
                  <span className="text-muted">{SOURCE_LABEL[r.source] ?? r.source}</span>
                  {r.flags?.length ? <span className="text-warning">{r.flags.join(',')}</span> : null}
                </li>
              ))}
            </ol>
          )}
        </div>
      )}
      {pending.length > 0 && <p className="mb-2 text-xs text-accent">Нажали: {pending.map(names).join(', ')}</p>}
      <ul className="flex flex-col gap-1 text-xs">
        {players.map((p) => {
          const c = conns.find((x) => x.playerId === p.id);
          const trust = c?.trust as { level?: TrustLevel; score?: number; flags?: Record<string, number> } | undefined;
          const qColor = c?.quality === 'good' ? 'text-success' : c?.quality === 'fair' ? 'text-warning' : c?.quality === 'poor' ? 'text-danger' : 'text-muted';
          return (
            <li key={p.id} className="flex items-center gap-2 rounded-sm border border-border px-2 py-1">
              <span className="w-24 truncate font-semibold">{p.name}</span>
              <span className={qColor}>{QUALITY_LABEL[c?.quality ?? 'unsynced']}</span>
              {c?.rttMs !== undefined && <span className="font-mono text-muted">rtt {c.rttMs.toFixed(0)}</span>}
              {c?.jitterMs !== undefined && <span className="font-mono text-muted">j {c.jitterMs.toFixed(0)}</span>}
              {c?.uMs !== undefined && <span className="font-mono text-muted">±{c.uMs.toFixed(0)}</span>}
              {c?.flags?.length ? <span className="text-warning">{c.flags.join(',')}</span> : null}
              <span className="flex-1" />
              <Select
                value={trust?.level ?? ''}
                onChange={(e) => conn.send('SET_TRUST', { personId: p.id, level: e.target.value as TrustLevel | '' })}
                className="w-auto py-0.5 text-[11px]"
                aria-label={`Доверие: ${p.name}`}
                title={trust ? `Доверие ${TRUST_LABEL[trust.level ?? 'full']} (${trust.score ?? 0})` : 'Уровень доверия'}
              >
                <option value="">авто{trust?.level ? ` (${TRUST_LABEL[trust.level]})` : ''}</option>
                {(Object.keys(TRUST_LABEL) as TrustLevel[]).map((l) => (
                  <option key={l} value={l}>
                    {TRUST_LABEL[l]}
                  </option>
                ))}
              </Select>
            </li>
          );
        })}
      </ul>
      {lockouts.length > 0 && (
        <p className="mt-2 text-xs text-muted">
          Фальстарты:{' '}
          {lockouts
            .slice(-5)
            .map((l) => `${names(l.playerId)} (${(l.durationMs / 1000).toFixed(1)} с)`)
            .join(', ')}
        </p>
      )}
    </Card>
  );
}

// ---- appeals ---------------------------------------------------------------------

function AppealPanel({ game, names }: { game: GameState; names: (id: string | null | undefined) => string }) {
  const a = game.appeal;
  if (!a) return null;
  return (
    <Card title="Апелляция" className="border-warning/60">
      <p className="text-sm">
        {a.kind === 'for' ? `${names(a.appellantId)} считает свой ответ верным` : `${names(a.appellantId)} не согласен с ответом ${names(a.answererId)}`}: «{a.answer}»
      </p>
      <p className="mt-1 text-xs text-muted">
        За {a.for} · против {a.against} · ждём: {a.pending.map(names).join(', ') || '—'}
      </p>
    </Card>
  );
}

// ---- players: score editor + chooser ---------------------------------------------

function PlayersPanel({ game, names }: { game: GameState; names: (id: string | null | undefined) => string }) {
  const { conn } = useRoom();
  const [editing, setEditing] = useState<string | null>(null);
  const [value, setValue] = useState('');
  return (
    <Card title="Игроки">
      <ScoreStrip players={game.players} scores={game.scores} deltas={game.deltas} chooserId={game.chooserId} size="sm" className="mb-3 flex-wrap" />
      <ul className="flex flex-col gap-1 text-sm">
        {game.players.map((p) => (
          <li key={p.id} className={clsx('flex items-center gap-2 rounded-sm px-1 py-0.5', p.id === game.chooserId && 'bg-surface-2')}>
            <span className={clsx('flex-1 truncate', !p.connected && 'text-muted line-through')}>{p.name}</span>
            {editing === p.id ? (
              <form
                className="flex items-center gap-1"
                onSubmit={(e) => {
                  e.preventDefault();
                  const n = Number(value);
                  if (Number.isFinite(n)) conn.send('CHANGE_SCORE', { personId: p.id, newSum: n });
                  setEditing(null);
                }}
              >
                <Input type="number" value={value} onChange={(e) => setValue(e.target.value)} className="w-24 py-0.5 text-xs" autoFocus aria-label={`Счёт: ${p.name}`} />
                <Button size="sm" variant="gold" type="submit">
                  OK
                </Button>
                <Button size="sm" variant="ghost" onClick={() => setEditing(null)}>
                  ✕
                </Button>
              </form>
            ) : (
              <>
                <button
                  type="button"
                  className="font-display text-base tabular-nums hover:text-accent"
                  title="Изменить счёт"
                  onClick={() => {
                    setEditing(p.id);
                    setValue(String(game.scores[p.id] ?? p.score));
                  }}
                >
                  {fmtScore(game.scores[p.id] ?? p.score)}
                </button>
                <Button size="sm" variant="ghost" onClick={() => conn.send('SET_CHOOSER', { personId: p.id })} title="Назначить выбирающим" disabled={p.id === game.chooserId}>
                  ▸
                </Button>
              </>
            )}
          </li>
        ))}
        {game.players.length === 0 && <li className="text-xs text-muted">Нет игроков</li>}
      </ul>
      {game.chooserId && <p className="mt-2 text-xs text-muted">Выбирает: {names(game.chooserId)}</p>}
    </Card>
  );
}

export function SectionLabel({ children }: { children: ReactNode }) {
  return <p className="text-xs font-bold tracking-widest text-muted uppercase">{children}</p>;
}
