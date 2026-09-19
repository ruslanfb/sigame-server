import { useState } from 'react';
import { useRoom } from '../app/roomContext.ts';
import { fmtTime } from '../lib/format.ts';
import { useRoomStore } from '../state/room.ts';
import { Button } from './Button.tsx';
import { Input } from './Input.tsx';

export function Chat() {
  const { conn } = useRoom();
  const chat = useRoomStore((s) => s.chat);
  const [text, setText] = useState('');
  const send = () => {
    const t = text.trim();
    if (!t) return;
    conn.send('CHAT', { text: t.slice(0, 500) });
    setText('');
  };
  return (
    <div className="flex flex-col gap-2 text-sm">
      <ul className="max-h-40 overflow-auto">
        {chat.map((c, i) => (
          <li key={`${c.atMs}-${i}`}>
            <span className="text-muted">{fmtTime(c.atMs)}</span> <b>{c.name}</b>: {c.text}
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
        <Input value={text} onChange={(e) => setText(e.target.value)} placeholder="message" maxLength={500} />
        <Button type="submit" variant="secondary">
          send
        </Button>
      </form>
    </div>
  );
}
