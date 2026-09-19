import { clsx } from 'clsx';
import { useEffect, useRef, useState } from 'react';
import { useRoom } from '../app/roomContext.ts';
import { fmtTime } from '../lib/format.ts';
import { useRoomStore } from '../state/room.ts';
import { Button } from './Button.tsx';
import { Input } from './Input.tsx';

export function Chat({ className, maxHeight = 'max-h-56' }: { className?: string; maxHeight?: string }) {
  const { conn } = useRoom();
  const chat = useRoomStore((s) => s.chat);
  const me = useRoomStore((s) => s.welcome?.personId);
  const [text, setText] = useState('');
  const listRef = useRef<HTMLUListElement>(null);
  useEffect(() => {
    const el = listRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [chat.length]);
  const send = () => {
    const t = text.trim();
    if (!t) return;
    conn.send('CHAT', { text: t.slice(0, 500) });
    setText('');
  };
  return (
    <div className={clsx('flex flex-col gap-2 text-sm', className)}>
      <ul ref={listRef} className={clsx('flex flex-col gap-1 overflow-auto', maxHeight)}>
        {chat.length === 0 && <li className="text-xs text-muted">Пока тихо</li>}
        {chat.map((c, i) => (
          <li key={`${c.atMs}-${i}`} className={clsx('rounded-sm px-1', c.personId === me && 'bg-surface-2')}>
            <span className="text-[10px] text-muted">{fmtTime(c.atMs)}</span> <b className="text-accent-2">{c.name}</b>: {c.text}
          </li>
        ))}
      </ul>
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          send();
        }}
      >
        <Input value={text} onChange={(e) => setText(e.target.value)} placeholder="Сообщение" maxLength={500} enterKeyHint="send" />
        <Button type="submit" variant="secondary" disabled={!text.trim()}>
          Отправить
        </Button>
      </form>
    </div>
  );
}
