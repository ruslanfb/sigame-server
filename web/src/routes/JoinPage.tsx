import { useQuery } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';
import { Link, useLocation, useNavigate } from 'react-router';
import { ApiError, getRoom, joinRoom } from '../api/client.ts';
import { Brand } from '../components/Brand.tsx';
import { Button } from '../components/Button.tsx';
import { Card } from '../components/Card.tsx';
import { Field, Input, Segmented } from '../components/Input.tsx';
import { ROOM_CODE_LENGTH, RoomCodeInput } from '../components/RoomCode.tsx';
import { roomCodeFromLocation } from '../lib/format.ts';
import { apiErrorText, plural, ROLE_LABEL } from '../lib/labels.ts';
import { listSessions, pageFor, roomRouteFor, sessionKey, useSessionStore } from '../state/session.ts';
import type { Role } from '../ws/types.ts';

export function JoinPage() {
  const location = useLocation();
  const navigate = useNavigate();
  const sessions = useSessionStore((s) => s.sessions);
  const lastName = useSessionStore((s) => s.lastName);
  const setSession = useSessionStore((s) => s.setSession);
  const setLastName = useSessionStore((s) => s.setLastName);
  const [code, setCode] = useState(() => roomCodeFromLocation(location.search) ?? '');
  const [name, setName] = useState(lastName);
  const [role, setRole] = useState<Role>('player');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const complete = code.length === ROOM_CODE_LENGTH;
  const room = useQuery({
    queryKey: ['room', code],
    queryFn: () => getRoom(code),
    enabled: complete,
    retry: false,
    staleTime: 5000,
  });

  const roomError = room.error instanceof ApiError ? apiErrorText(room.error.status, room.error.code, room.error.message) : null;
  const info = room.data;
  const playerCount = info?.players?.filter((p) => p.role === 'player').length ?? 0;
  const showmanTaken = info?.players?.some((p) => p.role === 'showman' && p.connected) || info?.showman === 'ai';

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      const res = await joinRoom(code, { name: name.trim(), role, password: password || undefined });
      setLastName(name.trim());
      const prev = sessions[sessionKey(res.roomCode, pageFor(res.role, res.isHost))];
      setSession({
        roomCode: res.roomCode,
        sessionToken: res.sessionToken,
        personId: res.personId,
        role: res.role,
        isHost: res.isHost,
        hostToken: prev?.hostToken ?? null,
        joinUrl: prev?.joinUrl,
        name: name.trim(),
      });
      navigate(roomRouteFor(res.roomCode, res.role, res.isHost));
    } catch (err) {
      setError(err instanceof ApiError ? apiErrorText(err.status, err.code, err.message) : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto flex min-h-dvh w-full max-w-md flex-col gap-5 px-4 py-8">
      <div className="flex flex-col items-center gap-1 text-center">
        <Brand size="lg" />
        <p className="text-sm text-muted">Введите код комнаты с экрана ведущего</p>
      </div>
      <form className="flex flex-col gap-4" onSubmit={submit}>
        <RoomCodeInput
          value={code}
          onChange={(v) => {
            setCode(v);
            setError(null);
          }}
          autoFocus={!code}
        />
        <div className="min-h-6 text-center text-sm" aria-live="polite">
          {complete && room.isLoading && <span className="text-muted">Ищем комнату…</span>}
          {complete && roomError && <span className="text-danger">{roomError}</span>}
          {info && (
            <span className="text-muted">
              <b className="text-text">{info.name}</b> · {info.packName} · {playerCount}{' '}
              {plural(playerCount, 'игрок', 'игрока', 'игроков')}
              {info.status === 'playing' ? ' · идёт игра' : info.status === 'finished' ? ' · игра окончена' : ''}
            </span>
          )}
        </div>
        <Card bodyClassName="flex flex-col gap-4">
          <Field label="Ваше имя">
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={32}
              required
              autoComplete="nickname"
              placeholder="Как вас объявлять?"
              className="text-base"
            />
          </Field>
          <Field label="Роль" group>
            <Segmented<Role>
              value={role}
              onChange={setRole}
              name="Роль"
              options={[
                { value: 'player', label: ROLE_LABEL.player },
                { value: 'viewer', label: ROLE_LABEL.viewer, disabled: info ? !info.allowViewers : false },
                { value: 'showman', label: ROLE_LABEL.showman, disabled: !!showmanTaken, title: showmanTaken ? 'Ведущий уже есть' : undefined },
              ]}
            />
          </Field>
          {(info?.hasPassword || (!info && password)) && (
            <Field label="Пароль комнаты">
              <Input value={password} onChange={(e) => setPassword(e.target.value)} type="password" autoComplete="off" />
            </Field>
          )}
          {error && (
            <p className="rounded-md border border-danger/60 bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
              {error}
            </p>
          )}
          <Button type="submit" variant="gold" size="xl" className="w-full" disabled={busy || !complete || !name.trim()}>
            {busy ? 'Входим…' : 'Войти'}
          </Button>
        </Card>
      </form>
      {listSessions(sessions).length > 0 && (
        <Card title="Ваши комнаты">
          <ul className="flex flex-col gap-1">
            {listSessions(sessions).map((s) => (
              <li key={sessionKey(s.roomCode, pageFor(s.role, s.isHost))} className="flex items-center gap-2 text-sm">
                <b className="font-display text-accent">{s.roomCode}</b>
                <span className="flex-1 truncate text-muted">
                  {s.name} · {ROLE_LABEL[s.role]}
                </span>
                <Link className="text-primary-2 underline" to={roomRouteFor(s.roomCode, s.role, s.isHost)}>
                  Вернуться
                </Link>
              </li>
            ))}
          </ul>
        </Card>
      )}
      <p className="text-center text-sm text-muted">
        Ведёте игру?{' '}
        <Link className="text-accent underline" to="/host">
          Создать комнату
        </Link>
      </p>
    </div>
  );
}
