import { clsx } from 'clsx';
import { fmtDelta, fmtScore, PLAYER_STATE_LABEL } from '../lib/labels.ts';
import { useNow } from '../lib/useNow.ts';
import type { ScoreDelta } from '../state/game.ts';
import type { PlayerInfo, PlayerState } from '../ws/types.ts';

const DELTA_TTL_MS = 1800;

const stateTone: Record<PlayerState, string> = {
  none: '',
  answering: 'border-accent shadow-glow-gold',
  right: 'border-success shadow-glow-success',
  wrong: 'border-danger shadow-glow-danger',
  lost: 'border-danger/60',
  hasAnswered: 'border-info',
  pass: 'border-border-strong opacity-70',
};

export interface ScoreStripProps {
  players: PlayerInfo[];
  scores: Record<string, number>;
  deltas?: Record<string, ScoreDelta>;
  chooserId?: string | null;
  meId?: string | null;
  /** Players whose press is pending / who won the button (lit gold). */
  litIds?: string[];
  size?: 'sm' | 'md' | 'tv';
  className?: string;
  onSelect?: (id: string) => void;
}

/**
 * Horizontal strip of player plates: name, score, state and an animated delta
 * that floats up after PERSON_SCORE. Scrolls on phones, spreads on TV.
 */
export function ScoreStrip({ players, scores, deltas = {}, chooserId, meId, litIds = [], size = 'md', className, onSelect }: ScoreStripProps) {
  const now = useNow(Object.keys(deltas).length ? 300 : 0);
  // `inGame` marks participation in the current hidden phase (final/forAll), not a seat: show everyone not kicked.
  const seated = players.filter((p) => !p.kicked);
  if (seated.length === 0) return null;
  return (
    <div
      className={clsx(
        'flex gap-2',
        size === 'tv' ? 'justify-center gap-4' : 'overflow-x-auto pb-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden',
        className,
      )}
    >
      {seated.map((p) => {
        const score = scores[p.id] ?? p.score;
        const delta = deltas[p.id];
        const showDelta = delta && now - delta.at < DELTA_TTL_MS;
        const lit = litIds.includes(p.id);
        const me = p.id === meId;
        const chooser = p.id === chooserId;
        const body = (
          <>
            {showDelta && (
              <span
                key={delta.at}
                className={clsx(
                  'animate-float-up pointer-events-none absolute -top-3 right-2 font-display font-bold',
                  size === 'tv' ? 'text-3xl' : 'text-sm',
                  delta.delta >= 0 ? 'text-success' : 'text-danger',
                )}
              >
                {fmtDelta(delta.delta)}
              </span>
            )}
            <span
              className={clsx(
                'flex items-center gap-1 truncate font-semibold',
                size === 'tv' ? 'text-2xl' : size === 'sm' ? 'text-xs' : 'text-sm',
                lit && 'text-accent-contrast',
                !p.connected && 'text-muted line-through',
              )}
            >
              {chooser && <span className={lit ? 'text-accent-contrast' : 'text-accent'} title="Выбирает вопрос">▸</span>}
              {p.name}
              {me && <span className={lit ? 'text-accent-contrast/70' : 'text-muted'}>(вы)</span>}
            </span>
            <span
              className={clsx(
                'font-display tabular-nums',
                size === 'tv' ? 'text-4xl' : size === 'sm' ? 'text-base' : 'text-xl',
                lit ? 'text-accent-contrast' : score < 0 ? 'text-danger' : p.state === 'answering' ? 'text-accent' : 'text-text',
              )}
            >
              {fmtScore(score)}
            </span>
            {p.state !== 'none' && (
              <span className={clsx('uppercase tracking-wide', lit ? 'text-accent-contrast/80' : 'text-muted', size === 'tv' ? 'text-base' : 'text-[10px]')}>
                {PLAYER_STATE_LABEL[p.state]}
              </span>
            )}
          </>
        );
        const cls = clsx(
          'relative flex shrink-0 flex-col items-center rounded-md border bg-surface/80 text-center transition',
          size === 'tv' ? 'min-w-44 px-5 py-3' : size === 'sm' ? 'min-w-20 px-2 py-1' : 'min-w-24 px-3 py-1.5',
          lit ? 'border-accent bg-gradient-gold text-accent-contrast shadow-glow-gold' : stateTone[p.state] || 'border-border',
          me && !lit && 'ring-1 ring-accent/60',
          !p.connected && 'opacity-60',
          onSelect && 'cursor-pointer hover:border-accent',
        );
        return onSelect ? (
          <button key={p.id} type="button" className={cls} onClick={() => onSelect(p.id)}>
            {body}
          </button>
        ) : (
          <div key={p.id} className={cls}>
            {body}
          </div>
        );
      })}
    </div>
  );
}
