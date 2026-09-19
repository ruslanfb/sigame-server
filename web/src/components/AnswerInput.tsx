import { useEffect, useRef, useState, type FormEvent } from 'react';
import type { AnswerIn, AnswerOptionsPayload, AskAnswerPayload } from '../ws/types.ts';
import { Button } from './Button.tsx';
import { AnswerOptionsView } from './ContentView.tsx';
import { Input } from './Input.tsx';

const DRAFT_THROTTLE_MS = 250;

/**
 * The answer form for ASK_ANSWER: text (with throttled ANSWER_DRAFT
 * streaming), select (hex plates), number. Hidden answers are marked as such.
 */
export function AnswerInput({
  ask,
  options,
  excluded = [],
  onAnswer,
  onDraft,
}: {
  ask: AskAnswerPayload;
  options: AnswerOptionsPayload | null;
  excluded?: string[];
  onAnswer: (a: AnswerIn) => void;
  onDraft?: (text: string) => void;
}) {
  const [text, setText] = useState('');
  const [number, setNumber] = useState('');
  const lastDraft = useRef({ at: 0, text: '' });
  const timer = useRef<number | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const [prevAsk, setPrevAsk] = useState(ask);
  if (prevAsk !== ask) {
    // A new ASK_ANSWER: clear the form (state adjustment during render, per React docs).
    setPrevAsk(ask);
    setText('');
    setNumber('');
  }
  useEffect(() => {
    inputRef.current?.focus();
    return () => {
      if (timer.current !== null) window.clearTimeout(timer.current);
    };
  }, [ask]);

  const draft = (t: string) => {
    if (!onDraft) return;
    const now = performance.now();
    const flush = () => {
      timer.current = null;
      if (lastDraft.current.text === t) return;
      lastDraft.current = { at: performance.now(), text: t };
      onDraft(t);
    };
    if (now - lastDraft.current.at >= DRAFT_THROTTLE_MS) flush();
    else {
      if (timer.current !== null) window.clearTimeout(timer.current);
      timer.current = window.setTimeout(flush, DRAFT_THROTTLE_MS);
    }
  };

  const submitText = (e: FormEvent) => {
    e.preventDefault();
    const t = text.trim();
    if (!t) return;
    onAnswer({ text: t });
  };
  const submitNumber = (e: FormEvent) => {
    e.preventDefault();
    const n = Number(number);
    if (!Number.isFinite(n)) return;
    onAnswer({ number: n });
  };

  const hiddenNote = ask.hidden ? <p className="mb-2 text-xs text-muted">Ответ увидит только ведущий, пока не откроют все ответы.</p> : null;

  if (ask.answerType === 'select' && options) {
    return (
      <div>
        {hiddenNote}
        <AnswerOptionsView options={options} excluded={excluded} onPick={(label) => onAnswer({ optionLabel: label })} />
      </div>
    );
  }
  if (ask.answerType === 'number') {
    return (
      <form className="flex gap-2" onSubmit={submitNumber}>
        {hiddenNote}
        <Input
          ref={inputRef}
          type="number"
          inputMode="numeric"
          value={number}
          onChange={(e) => setNumber(e.target.value)}
          placeholder="Число"
          className="font-display text-2xl"
          autoFocus
        />
        <Button type="submit" variant="gold" size="lg" disabled={number === ''}>
          Ответить
        </Button>
      </form>
    );
  }
  if (ask.answerType === 'client') {
    return (
      <div className="flex gap-2">
        <Button variant="success" size="lg" className="flex-1" onClick={() => onAnswer({ right: true })}>
          Я ответил верно
        </Button>
        <Button variant="danger" size="lg" className="flex-1" onClick={() => onAnswer({ right: false })}>
          Не знаю
        </Button>
      </div>
    );
  }
  return (
    <form className="flex flex-col gap-2" onSubmit={submitText}>
      {hiddenNote}
      <div className="flex gap-2">
        <Input
          ref={inputRef}
          value={text}
          onChange={(e) => {
            setText(e.target.value);
            draft(e.target.value);
          }}
          placeholder="Ваш ответ"
          autoComplete="off"
          autoCapitalize="sentences"
          enterKeyHint="send"
          maxLength={200}
          className="text-lg"
          autoFocus
        />
        <Button type="submit" variant="gold" size="lg" disabled={!text.trim()}>
          Ответить
        </Button>
      </div>
    </form>
  );
}
