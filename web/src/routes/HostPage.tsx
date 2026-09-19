import { useQuery, useQueryClient } from '@tanstack/react-query';
import { clsx } from 'clsx';
import { useState, type DragEvent, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router';
import {
  ApiError,
  createRoom,
  getSystemInfo,
  importSiq,
  joinRoom,
  listBuzzerPresets,
  listPacks,
  type PackSummary,
  type Schemas,
} from '../api/client.ts';
import { Brand } from '../components/Brand.tsx';
import { Button } from '../components/Button.tsx';
import { Card } from '../components/Card.tsx';
import { Field, Input, Segmented } from '../components/Input.tsx';
import { apiErrorText, plural } from '../lib/labels.ts';
import { useSessionStore } from '../state/session.ts';
import type { ShowmanMode } from '../ws/types.ts';

type Preset = 'lanWired' | 'wifiParty' | 'internetFair' | 'tournament' | 'noRace';
type Report = Schemas['Report'];

export function HostPage() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const setSession = useSessionStore((s) => s.setSession);
  const lastName = useSessionStore((s) => s.lastName);
  const [search, setSearch] = useState('');
  const packs = useQuery({ queryKey: ['packs', search], queryFn: () => listPacks({ limit: 100, q: search || undefined }) });
  const presets = useQuery({ queryKey: ['buzzer-presets'], queryFn: listBuzzerPresets });
  const sysinfo = useQuery({ queryKey: ['system-info'], queryFn: getSystemInfo });
  const [packId, setPackId] = useState('');
  const [roomName, setRoomName] = useState('');
  const [hostName, setHostName] = useState(lastName || 'Ведущий');
  const [showman, setShowman] = useState<ShowmanMode>('human');
  const [preset, setPreset] = useState<Preset>('wifiParty');
  const [maxPlayers, setMaxPlayers] = useState(6);
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [importing, setImporting] = useState(false);
  const [dragOver, setDragOver] = useState(false);
  const [report, setReport] = useState<{ name: string; report: Report } | null>(null);
  const [error, setError] = useState<string | null>(null);

  const items = packs.data?.items ?? [];
  const effectivePack = packId || items[0]?.id || '';
  const aiOk = sysinfo.data?.aiConfigured ?? true;
  const presetList = presets.data?.presets ?? [];
  const errText = (err: unknown) => (err instanceof ApiError ? apiErrorText(err.status, err.code, err.message) : String(err));

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
        maxPlayers,
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
        joinUrl: created.joinUrl,
        name: hostName.trim(),
      });
      navigate(`/room/${code}/host`);
    } catch (err) {
      setError(errText(err));
    } finally {
      setBusy(false);
    }
  };

  const onImport = async (file: File | undefined) => {
    if (!file) return;
    setError(null);
    setReport(null);
    setImporting(true);
    try {
      const res = await importSiq(file);
      await qc.invalidateQueries({ queryKey: ['packs'] });
      setPackId(res.pack.id ?? '');
      setReport({ name: res.pack.name, report: res.report });
    } catch (err) {
      setError(errText(err));
    } finally {
      setImporting(false);
    }
  };
  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    setDragOver(false);
    onImport(e.dataTransfer.files?.[0]);
  };

  return (
    <div className="mx-auto flex min-h-dvh w-full max-w-3xl flex-col gap-5 px-4 py-8">
      <div className="flex items-end justify-between gap-4">
        <Brand size="md" />
        <Link className="text-sm text-muted underline hover:text-text" to="/">
          Я игрок
        </Link>
      </div>
      <h1 className="font-display text-3xl">Новая комната</h1>
      <form className="grid gap-4 md:grid-cols-[1fr_320px]" onSubmit={submit}>
        <div className="flex flex-col gap-4">
          <Card title="Пак вопросов" action={<span className="text-xs text-muted">{items.length} шт.</span>}>
            <Input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Поиск по названию, авторам, тегам" className="mb-2" />
            <ul className="flex max-h-72 flex-col gap-1 overflow-auto" role="listbox" aria-label="Паки">
              {packs.isLoading && <li className="text-sm text-muted">Загрузка…</li>}
              {packs.isError && <li className="text-sm text-danger">Не удалось загрузить список паков</li>}
              {items.map((p) => (
                <PackRow key={p.id} pack={p} selected={p.id === effectivePack} onSelect={() => setPackId(p.id)} />
              ))}
              {packs.data && items.length === 0 && <li className="text-sm text-muted">Паков нет — загрузите .siq ниже</li>}
            </ul>
            <label
              onDragOver={(e) => {
                e.preventDefault();
                setDragOver(true);
              }}
              onDragLeave={() => setDragOver(false)}
              onDrop={onDrop}
              className={clsx(
                'mt-3 flex cursor-pointer flex-col items-center justify-center gap-1 rounded-md border border-dashed px-4 py-4 text-center text-sm transition',
                dragOver ? 'border-accent bg-accent/10' : 'border-border-strong hover:border-accent',
              )}
            >
              <span className="font-semibold">{importing ? 'Импортируем…' : 'Загрузить .siq'}</span>
              <span className="text-xs text-muted">перетащите файл сюда или нажмите</span>
              <input type="file" accept=".siq,.zip" className="sr-only" disabled={importing} onChange={(e) => onImport(e.target.files?.[0])} />
            </label>
            {report && <ImportReport name={report.name} report={report.report} />}
          </Card>
        </div>
        <div className="flex flex-col gap-4">
          <Card title="Настройки">
            <div className="flex flex-col gap-3">
              <Field label="Название комнаты">
                <Input value={roomName} onChange={(e) => setRoomName(e.target.value)} maxLength={80} placeholder="Пятничная игра" />
              </Field>
              <Field label="Ваше имя">
                <Input value={hostName} onChange={(e) => setHostName(e.target.value)} maxLength={32} required />
              </Field>
              <Field label="Ведущий" group hint={!aiOk ? 'ИИ-ведущий не настроен на сервере' : undefined}>
                <Segmented<ShowmanMode>
                  size="sm"
                  value={showman}
                  onChange={setShowman}
                  options={[
                    { value: 'human', label: 'Человек' },
                    { value: 'hybrid', label: 'Гибрид', disabled: !aiOk, title: 'ИИ подсказывает, решает человек' },
                    { value: 'ai', label: 'ИИ', disabled: !aiOk },
                  ]}
                />
              </Field>
              <Field label="Кнопка" group hint={presetList.find((p) => p.name === preset)?.description}>
                <div className="flex flex-col gap-1">
                  {(presetList.length ? presetList : [{ name: 'wifiParty' as const, title: 'Wi-Fi', supported: true, description: '' }]).map((p) => (
                    <label
                      key={p.name}
                      className={clsx(
                        'flex cursor-pointer items-center gap-2 rounded-md border px-3 py-1.5 text-sm',
                        preset === p.name ? 'border-accent bg-accent/10' : 'border-border',
                        !p.supported && 'cursor-not-allowed opacity-40',
                      )}
                    >
                      <input
                        type="radio"
                        name="preset"
                        value={p.name}
                        checked={preset === p.name}
                        disabled={!p.supported}
                        onChange={() => setPreset(p.name)}
                        className="accent-accent"
                      />
                      <span className="font-semibold">{p.title}</span>
                    </label>
                  ))}
                </div>
              </Field>
              <Field label="Игроков максимум">
                <Input
                  type="number"
                  min={1}
                  max={sysinfo.data?.maxPlayers ?? 12}
                  value={maxPlayers}
                  onChange={(e) => setMaxPlayers(Math.max(1, Math.min(sysinfo.data?.maxPlayers ?? 12, Number(e.target.value) || 1)))}
                />
              </Field>
              <Field label="Пароль (необязательно)">
                <Input value={password} onChange={(e) => setPassword(e.target.value)} maxLength={64} autoComplete="off" />
              </Field>
            </div>
          </Card>
          {error && (
            <p className="rounded-md border border-danger/60 bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
              {error}
            </p>
          )}
          <Button type="submit" variant="gold" size="xl" disabled={busy || importing || !effectivePack || !hostName.trim()}>
            {busy ? 'Создаём…' : 'Создать и войти как ведущий'}
          </Button>
        </div>
      </form>
    </div>
  );
}

function PackRow({ pack, selected, onSelect }: { pack: PackSummary; selected: boolean; onSelect: () => void }) {
  return (
    <li>
      <button
        type="button"
        role="option"
        aria-selected={selected}
        onClick={onSelect}
        className={clsx(
          'flex w-full items-center gap-3 rounded-md border px-3 py-2 text-left transition',
          selected ? 'border-accent bg-accent/10 shadow-glow-gold' : 'border-border bg-bg-2/60 hover:border-border-strong',
        )}
      >
        <span className="flex-1 truncate">
          <span className="block truncate text-sm font-semibold">{pack.name}</span>
          <span className="block truncate text-xs text-muted">
            {pack.roundCount} {plural(pack.roundCount, 'раунд', 'раунда', 'раундов')} · {pack.questionCount}{' '}
            {plural(pack.questionCount, 'вопрос', 'вопроса', 'вопросов')}
            {pack.authors?.length ? ` · ${pack.authors.join(', ')}` : ''}
            {pack.hasMedia ? ' · медиа' : ''}
          </span>
        </span>
        {selected && <span className="font-display text-xs text-accent">выбран</span>}
      </button>
    </li>
  );
}

function ImportReport({ name, report }: { name: string; report: Report }) {
  const entries = report.entries ?? [];
  const errors = entries.filter((e) => e.level === 'error').length;
  const warnings = entries.filter((e) => e.level === 'warning').length;
  const stats = report.stats as unknown as Record<string, number | undefined>;
  return (
    <div className="mt-3 rounded-md border border-border bg-bg-2/60 p-3 text-xs">
      <p className="text-sm font-semibold text-success">Импортирован «{name}»</p>
      <p className="mt-1 text-muted">
        SIQ v{report.version} · {stats.rounds ?? '?'} раунд. · {stats.themes ?? '?'} тем · {stats.questions ?? '?'} вопр.
        {errors ? ` · ${errors} ошиб.` : ''}
        {warnings ? ` · ${warnings} предупр.` : ''}
        {!entries.length ? ' · без замечаний' : ''}
      </p>
      {entries.length > 0 && (
        <ul className="mt-2 flex max-h-32 flex-col gap-0.5 overflow-auto">
          {entries.slice(0, 30).map((e, i) => (
            <li key={i} className={clsx(e.level === 'error' ? 'text-danger' : e.level === 'warning' ? 'text-warning' : 'text-muted')}>
              [{e.code}] {e.message}
              {e.path ? <span className="opacity-60"> {e.path}</span> : null}
            </li>
          ))}
          {entries.length > 30 && <li className="text-muted">… ещё {entries.length - 30}</li>}
        </ul>
      )}
    </div>
  );
}
