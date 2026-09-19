import { clsx } from 'clsx';
import { useId, useRef, useState, type ClipboardEvent } from 'react';
import { normalizeRoomCode } from '../lib/format.ts';
import { HexPlate } from './HexPlate.tsx';

export const ROOM_CODE_LENGTH = 5;

/** Read-only room code as gold hex plates (host console, TV title). */
export function RoomCodePlates({ code, size = 'md', className }: { code: string; size?: 'md' | 'lg' | 'tv'; className?: string }) {
  const chars = code.padEnd(ROOM_CODE_LENGTH, ' ').slice(0, ROOM_CODE_LENGTH).split('');
  const dims = size === 'tv' ? 'h-28 w-24 text-6xl' : size === 'lg' ? 'h-16 w-14 text-3xl' : 'h-12 w-10 text-xl';
  return (
    <div className={clsx('flex gap-1.5', className)} aria-label={`Код комнаты ${code}`}>
      {chars.map((c, i) => (
        <HexPlate key={i} tone="gold" padding="p-0" className={clsx(dims, 'font-display')}>
          <span className={clsx('flex h-full w-full items-center justify-center', dims)}>{c.trim()}</span>
        </HexPlate>
      ))}
    </div>
  );
}

/**
 * Room code entry: a real (invisible) input on top of five hex plates, so
 * paste, autofill, IME and screen readers all work.
 */
export function RoomCodeInput({
  value,
  onChange,
  autoFocus,
  disabled,
}: {
  value: string;
  onChange: (v: string) => void;
  autoFocus?: boolean;
  disabled?: boolean;
}) {
  const id = useId();
  const ref = useRef<HTMLInputElement>(null);
  const [focused, setFocused] = useState(false);
  const chars = value.padEnd(ROOM_CODE_LENGTH, ' ').slice(0, ROOM_CODE_LENGTH).split('');
  const caret = Math.min(value.length, ROOM_CODE_LENGTH - 1);
  const onPaste = (e: ClipboardEvent<HTMLInputElement>) => {
    const text = e.clipboardData.getData('text');
    const fromUrl = /[?&]room=([A-Za-z0-9]+)/.exec(text)?.[1];
    if (fromUrl) {
      e.preventDefault();
      onChange(normalizeRoomCode(fromUrl).slice(0, ROOM_CODE_LENGTH));
    }
  };
  return (
    <div className="relative" onClick={() => ref.current?.focus()}>
      <label htmlFor={id} className="sr-only">
        Код комнаты
      </label>
      <div className="flex justify-center gap-2" aria-hidden="true">
        {chars.map((c, i) => {
          const filled = c.trim() !== '';
          const active = focused && i === caret && value.length < ROOM_CODE_LENGTH;
          return (
            <HexPlate
              key={i}
              tone={filled ? 'gold' : active ? 'active' : 'default'}
              padding="p-0"
              glow={active ? 'blue' : null}
              className={clsx('h-16 w-14 font-display text-3xl transition', active && 'animate-pulse-gold')}
            >
              <span className="flex h-16 w-14 items-center justify-center">{filled ? c : active ? '|' : ''}</span>
            </HexPlate>
          );
        })}
      </div>
      <input
        ref={ref}
        id={id}
        value={value}
        onChange={(e) => onChange(normalizeRoomCode(e.target.value).slice(0, ROOM_CODE_LENGTH))}
        onPaste={onPaste}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        autoFocus={autoFocus}
        disabled={disabled}
        autoCapitalize="characters"
        autoComplete="off"
        autoCorrect="off"
        spellCheck={false}
        inputMode="text"
        maxLength={ROOM_CODE_LENGTH}
        placeholder="KOD"
        className="absolute inset-0 h-full w-full cursor-text opacity-0"
        style={{ caretColor: 'transparent' }}
      />
    </div>
  );
}
