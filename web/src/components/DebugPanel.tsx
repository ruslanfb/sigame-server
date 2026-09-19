import { useEffect, useState } from 'react';
import { fmtMs } from '../lib/format.ts';
import { useBuzzerStore } from '../state/buzzer.ts';
import { useDebugStore } from '../state/debug.ts';
import { activePrompts, useGameStore } from '../state/game.ts';
import { useRoomStore } from '../state/room.ts';
import { Card } from './Card.tsx';
import { TimerList } from './TimerBar.tsx';

/**
 * Debug panel for validating the foundation against the real server:
 * connection state, clock model, last 20 message types, stage, prompts, buzzer.
 */
export function DebugPanel() {
  const connection = useDebugStore((s) => s.connection);
  const lastClose = useDebugStore((s) => s.lastClose);
  const recent = useDebugStore((s) => s.recent);
  const model = useBuzzerStore((s) => s.model);
  const syncAcks = useBuzzerStore((s) => s.syncAcks);
  const arm = useBuzzerStore((s) => s.arm);
  const game = useGameStore((s) => s.game);
  const welcome = useRoomStore((s) => s.welcome);
  const [open, setOpen] = useState(true);
  const [, tick] = useState(0);
  useEffect(() => {
    const id = setInterval(() => tick((n) => n + 1), 500);
    return () => clearInterval(id);
  }, []);

  return (
    <Card
      title={
        <button className="flex w-full items-center justify-between" onClick={() => setOpen((o) => !o)}>
          <span>debug</span>
          <span className="font-mono text-xs">{open ? '−' : '+'}</span>
        </button>
      }
      className="font-mono text-xs"
    >
      {open && (
        <div className="grid gap-3 md:grid-cols-2">
          <div>
            <div>
              conn: <b>{connection}</b>
              {lastClose ? ` (last close ${lastClose.code} ${lastClose.reason})` : ''}
            </div>
            <div>
              me: {welcome?.name ?? '—'} / {welcome?.role ?? '—'}
              {welcome?.isHost ? ' / host' : ''}
            </div>
            <div className="mt-2">
              model:{' '}
              {model ? (
                <>
                  <b>{model.quality}</b> off {fmtMs(model.offsetMs, 2)} rtt {fmtMs(model.rttRefMs)} σ{' '}
                  {fmtMs(model.sigmaMs)} ±u {fmtMs(model.uMs)} lead {fmtMs(model.leadMs, 0)} samples{' '}
                  {model.samples} acks {syncAcks}
                  {model.flags?.length ? ` flags ${model.flags.join(',')}` : ''}
                </>
              ) : (
                '—'
              )}
            </div>
            <div className="mt-2">
              stage: <b>{game.stage}</b>
              {game.sub ? ` / ${game.sub}` : ''} round {game.roundIndex}
              {game.paused ? ' PAUSED' : ''}
            </div>
            <div>prompts: {activePrompts(game.prompts).join(', ') || '—'}</div>
            <div>
              buzzer: <b>{arm.phase}</b>
              {arm.armId ? ` arm ${arm.armId.slice(0, 8)}` : ''}
              {arm.mode ? ` ${arm.mode}` : ''}
              {arm.litLocal ? ` lit@${arm.litLocal.toFixed(1)}` : ''}
              {arm.pressSeq ? ` press@${arm.pressLocal.toFixed(1)} (${(arm.pressLocal - arm.litLocal).toFixed(0)} ms)` : ''}
              {arm.ack ? ` ack ${arm.ack.status}${arm.ack.reactionMs ? ` ${arm.ack.reactionMs.toFixed(0)} ms` : ''}` : ''}
              {arm.result ? ` result ${arm.result.kind}${arm.result.winnerId ? ` ${arm.result.winnerId.slice(0, 8)}` : ''}` : ''}
              {arm.lockedUntilLocal > performance.now() ? ` locked ${Math.ceil((arm.lockedUntilLocal - performance.now()) / 1000)} s` : ''}
            </div>
            <div className="mt-2">
              <TimerList timers={game.timers} />
            </div>
          </div>
          <div>
            <div className="text-muted">last {recent.length} messages</div>
            <ol className="mt-1 max-h-64 overflow-auto">
              {[...recent].reverse().map((m) => (
                <li key={`${m.seq}-${m.tRecv}`}>
                  #{m.seq} {m.t}
                </li>
              ))}
            </ol>
          </div>
        </div>
      )}
    </Card>
  );
}
