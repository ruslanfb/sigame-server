import { useState, type FormEvent } from 'react';
import { Link, useLocation, useNavigate } from 'react-router';
import { ApiError, joinRoom } from '../api/client.ts';
import { Button } from '../components/Button.tsx';
import { Card } from '../components/Card.tsx';
import { Field, Input, Select } from '../components/Input.tsx';
import { normalizeRoomCode, roomCodeFromLocation } from '../lib/format.ts';
import { roomRouteFor, useSessionStore } from '../state/session.ts';
import type { Role } from '../ws/types.ts';

export function JoinPage() {
  const location = useLocation();
  const navigate = useNavigate();
  const session = useSessionStore((s) => s.session);
  const lastName = useSessionStore((s) => s.lastName);
  const setSession = useSessionStore((s) => s.setSession);
  const [code, setCode] = useState(() => roomCodeFromLocation(location.search) ?? session?.roomCode ?? '');
  const [name, setName] = useState(lastName);
  const [role, setRole] = useState<Role>('player');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      const c = normalizeRoomCode(code);
      const res = await joinRoom(c, { name: name.trim(), role, password: password || undefined });
      setSession({
        roomCode: res.roomCode,
        sessionToken: res.sessionToken,
        personId: res.personId,
        role: res.role,
        isHost: res.isHost,
        hostToken: session?.roomCode === res.roomCode ? session.hostToken : null,
        name: name.trim(),
      });
      navigate(roomRouteFor(res.roomCode, res.role, res.isHost));
    } catch (err) {
      setError(err instanceof ApiError ? `${err.message}${err.code ? ` (${err.code})` : ''}` : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto flex max-w-md flex-col gap-4 p-4">
      <h1 className="text-2xl font-semibold">SIGame</h1>
      <Card title="Join a room">
        <form className="flex flex-col gap-3" onSubmit={submit}>
          <Field label="Room code">
            <Input
              value={code}
              onChange={(e) => setCode(normalizeRoomCode(e.target.value))}
              placeholder="K7Q2M"
              autoCapitalize="characters"
              required
              className="font-mono uppercase"
            />
          </Field>
          <Field label="Your name">
            <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={32} required />
          </Field>
          <Field label="Role">
            <Select value={role} onChange={(e) => setRole(e.target.value as Role)}>
              <option value="player">player</option>
              <option value="viewer">viewer</option>
              <option value="showman">showman</option>
            </Select>
          </Field>
          <Field label="Password (if any)">
            <Input value={password} onChange={(e) => setPassword(e.target.value)} type="password" />
          </Field>
          {error && <p className="text-sm text-danger">{error}</p>}
          <Button type="submit" disabled={busy || !code || !name.trim()}>
            {busy ? 'joining…' : 'Join'}
          </Button>
        </form>
      </Card>
      {session && (
        <Card title="Current session">
          <p className="text-sm">
            {session.name} ({session.role}) in <span className="font-mono">{session.roomCode}</span>
          </p>
          <Link className="text-sm text-accent underline" to={roomRouteFor(session.roomCode, session.role, session.isHost)}>
            return to the room
          </Link>
        </Card>
      )}
      <p className="text-sm text-muted">
        Hosting?{' '}
        <Link className="text-accent underline" to="/host">
          Create a room
        </Link>
      </p>
    </div>
  );
}
