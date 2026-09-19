import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router';
import { ApiError, createRoom, getSystemInfo, importSiq, joinRoom, listBuzzerPresets, listPacks } from '../api/client.ts';
import { Button } from '../components/Button.tsx';
import { Card } from '../components/Card.tsx';
import { Field, Input, Select } from '../components/Input.tsx';
import { useSessionStore } from '../state/session.ts';
import type { ShowmanMode } from '../ws/types.ts';

type Preset = 'lanWired' | 'wifiParty' | 'internetFair' | 'tournament' | 'noRace';

export function HostPage() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const setSession = useSessionStore((s) => s.setSession);
  const lastName = useSessionStore((s) => s.lastName);
  const packs = useQuery({ queryKey: ['packs'], queryFn: () => listPacks({ limit: 100 }) });
  const presets = useQuery({ queryKey: ['buzzer-presets'], queryFn: listBuzzerPresets });
  const sysinfo = useQuery({ queryKey: ['system-info'], queryFn: getSystemInfo });
  const [packId, setPackId] = useState('');
  const [roomName, setRoomName] = useState('');
  const [hostName, setHostName] = useState(lastName || 'Host');
  const [showman, setShowman] = useState<ShowmanMode>('human');
  const [preset, setPreset] = useState<Preset>('wifiParty');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const effectivePack = packId || packs.data?.items?.[0]?.id || '';

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      const created = await createRoom({
        packId: effectivePack,
        name: roomName || undefined,
        showman,
        buzzerPreset: preset,
        password: password || undefined,
      });
      const code = created.room.code;
      const joined = await joinRoom(code, { name: hostName.trim(), role: 'showman' }, created.hostToken);
      setSession({
        roomCode: code,
        sessionToken: joined.sessionToken,
        personId: joined.personId,
        role: joined.role,
        isHost: joined.isHost,
        hostToken: created.hostToken,
        name: hostName.trim(),
      });
      navigate(`/room/${code}/host`);
    } catch (err) {
      setError(err instanceof ApiError ? `${err.message}${err.code ? ` (${err.code})` : ''}` : String(err));
    } finally {
      setBusy(false);
    }
  };

  const onImport = async (file: File | undefined) => {
    if (!file) return;
    setError(null);
    setBusy(true);
    try {
      const res = await importSiq(file);
      await qc.invalidateQueries({ queryKey: ['packs'] });
      setPackId(res.pack.id ?? '');
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto flex max-w-md flex-col gap-4 p-4">
      <h1 className="text-2xl font-semibold">Create a room</h1>
      <Card>
        <form className="flex flex-col gap-3" onSubmit={submit}>
          <Field label="Pack">
            <Select value={effectivePack} onChange={(e) => setPackId(e.target.value)} required>
              {packs.isLoading && <option value="">loading…</option>}
              {packs.data?.items?.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} ({p.questionCount} q)
                </option>
              ))}
              {packs.data && (packs.data.items?.length ?? 0) === 0 && <option value="">no packs — import a .siq</option>}
            </Select>
          </Field>
          <Field label="Import .siq">
            <Input type="file" accept=".siq,.zip" onChange={(e) => onImport(e.target.files?.[0])} />
          </Field>
          <Field label="Room name (optional)">
            <Input value={roomName} onChange={(e) => setRoomName(e.target.value)} maxLength={80} />
          </Field>
          <Field label="Your name (showman)">
            <Input value={hostName} onChange={(e) => setHostName(e.target.value)} maxLength={32} required />
          </Field>
          <Field label="Showman mode">
            <Select value={showman} onChange={(e) => setShowman(e.target.value as ShowmanMode)}>
              <option value="human">human</option>
              <option value="hybrid" disabled={sysinfo.data ? !sysinfo.data.aiConfigured : false}>
                hybrid (AI suggests)
              </option>
              <option value="ai" disabled={sysinfo.data ? !sysinfo.data.aiConfigured : false}>
                ai
              </option>
            </Select>
          </Field>
          <Field label="Buzzer preset">
            <Select value={preset} onChange={(e) => setPreset(e.target.value as Preset)}>
              {(presets.data?.presets ?? []).map((p) => (
                <option key={p.name} value={p.name} disabled={!p.supported}>
                  {p.name} — {p.title}
                </option>
              ))}
              {!presets.data && <option value="wifiParty">wifiParty</option>}
            </Select>
          </Field>
          <Field label="Password (optional)">
            <Input value={password} onChange={(e) => setPassword(e.target.value)} maxLength={64} />
          </Field>
          {error && <p className="text-sm text-danger">{error}</p>}
          <Button type="submit" disabled={busy || !effectivePack || !hostName.trim()}>
            {busy ? 'creating…' : 'Create and join as showman'}
          </Button>
        </form>
      </Card>
      <p className="text-sm text-muted">
        <Link className="text-accent underline" to="/">
          Back to join
        </Link>
      </p>
    </div>
  );
}
