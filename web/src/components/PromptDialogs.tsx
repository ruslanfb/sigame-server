import { SELECT_REASON_LABEL } from '../lib/labels.ts';
import type { AskAppealVotePayload, AskDeleteThemePayload, AskSelectPlayerPayload, PlayerInfo, TablePayload } from '../ws/types.ts';
import { Button } from './Button.tsx';
import { HexButton } from './HexPlate.tsx';

export function SelectPlayerPrompt({
  ask,
  players,
  onSelect,
}: {
  ask: AskSelectPlayerPayload;
  players: PlayerInfo[];
  onSelect: (personId: string) => void;
}) {
  return (
    <div className="flex flex-col gap-2">
      <p className="text-sm text-muted">{SELECT_REASON_LABEL[ask.reason] ?? 'Выберите игрока'}</p>
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
        {ask.candidates.map((id) => (
          <HexButton key={id} tone="active" padding="px-5 py-3" className="w-full" onClick={() => onSelect(id)}>
            <span className="text-base font-semibold">{players.find((p) => p.id === id)?.name ?? id.slice(0, 8)}</span>
          </HexButton>
        ))}
      </div>
    </div>
  );
}

export function DeleteThemePrompt({ ask, table, onDelete }: { ask: AskDeleteThemePayload; table: TablePayload; onDelete: (theme: number) => void }) {
  return (
    <div className="flex flex-col gap-2">
      <p className="text-sm text-muted">Уберите одну тему из финала</p>
      {ask.themes.map((ti) => (
        <HexButton key={ti} tone="danger" padding="px-5 py-3" className="w-full" onClick={() => onDelete(ti)}>
          <span className="text-base font-semibold">{table.themes[ti]?.name ?? `Тема ${ti + 1}`}</span>
        </HexButton>
      ))}
    </div>
  );
}

export function AppealVotePrompt({
  ask,
  answererName,
  onVote,
}: {
  ask: AskAppealVotePayload;
  answererName: string;
  onVote: (right: boolean) => void;
}) {
  return (
    <div className="flex flex-col gap-3">
      <p className="text-sm text-muted">
        {ask.kind === 'for' ? `${answererName} считает свой ответ верным.` : `Игрок не согласен с решением по ответу ${answererName}.`}
      </p>
      <p className="font-display text-2xl text-accent">«{ask.answer}»</p>
      {ask.rights.length > 0 && (
        <p className="text-xs text-muted">
          Правильные ответы: <b className="text-text">{ask.rights.join(' / ')}</b>
        </p>
      )}
      <div className="flex gap-2">
        <Button variant="success" size="lg" className="flex-1" onClick={() => onVote(true)}>
          Ответ верный
        </Button>
        <Button variant="danger" size="lg" className="flex-1" onClick={() => onVote(false)}>
          Неверный
        </Button>
      </div>
    </div>
  );
}
