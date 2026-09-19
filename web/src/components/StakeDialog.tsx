import { clsx } from 'clsx';
import { useState } from 'react';
import { fmtScore, STAKE_MODE_LABEL, STAKE_REASON_LABEL } from '../lib/labels.ts';
import type { AskStakePayload, StakeMode } from '../ws/types.ts';
import { Button } from './Button.tsx';
import { HexPlate } from './HexPlate.tsx';

/**
 * ASK_STAKE: the allowed modes as buttons; `stake` opens a stepper/slider
 * bounded by min/max/step. Sends SET_STAKE{stakeMode, amount}.
 */
export function StakeDialog({ ask, onStake, myScore }: { ask: AskStakePayload; onStake: (mode: StakeMode, amount?: number) => void; myScore?: number }) {
  const [amount, setAmount] = useState(ask.min);
  const [prevAsk, setPrevAsk] = useState(ask);
  if (prevAsk !== ask) {
    // A new ASK_STAKE: reset the stepper (state adjustment during render, per React docs).
    setPrevAsk(ask);
    setAmount(ask.min);
  }
  const step = Math.max(1, ask.step || 1);
  const clamp = (v: number) => Math.min(ask.max, Math.max(ask.min, Math.round(v / step) * step));
  const canStake = ask.modes.includes('stake') && ask.max >= ask.min;
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between text-sm text-muted">
        <span>{STAKE_REASON_LABEL[ask.reason] ?? 'Ставка'}</span>
        {myScore !== undefined && <span>Ваш счёт: {fmtScore(myScore)}</span>}
      </div>
      {canStake && (
        <div className="flex flex-col items-center gap-2">
          <HexPlate tone="gold" padding="px-8 py-2" className="font-display text-4xl tabular-nums">
            {fmtScore(amount)}
          </HexPlate>
          <div className="flex w-full items-center gap-2">
            <Button variant="secondary" onClick={() => setAmount((a) => clamp(a - step))} disabled={amount <= ask.min} aria-label="Меньше">
              −
            </Button>
            <input
              type="range"
              min={ask.min}
              max={ask.max}
              step={step}
              value={amount}
              onChange={(e) => setAmount(clamp(Number(e.target.value)))}
              className="flex-1 accent-accent"
              aria-label="Сумма ставки"
            />
            <Button variant="secondary" onClick={() => setAmount((a) => clamp(a + step))} disabled={amount >= ask.max} aria-label="Больше">
              +
            </Button>
          </div>
          <div className="flex w-full justify-between text-xs text-muted">
            <span>{fmtScore(ask.min)}</span>
            <span>{fmtScore(ask.max)}</span>
          </div>
        </div>
      )}
      <div className={clsx('grid gap-2', ask.modes.length > 2 ? 'grid-cols-2' : 'grid-cols-1')}>
        {ask.modes.map((m) => (
          <Button
            key={m}
            size="lg"
            variant={m === 'stake' ? 'gold' : m === 'allIn' ? 'danger' : m === 'pass' ? 'secondary' : 'primary'}
            onClick={() => onStake(m, m === 'stake' ? amount : undefined)}
          >
            {STAKE_MODE_LABEL[m]}
            {m === 'stake' ? ` ${fmtScore(amount)}` : m === 'nominal' ? ` ${fmtScore(ask.min)}` : ''}
          </Button>
        ))}
      </div>
    </div>
  );
}
