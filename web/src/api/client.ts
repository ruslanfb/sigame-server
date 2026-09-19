/**
 * REST client generated from ../docs/openapi.json (`npm run gen:api`).
 * baseUrl '' → same origin: Vite proxies /api to the Go server in dev, and the
 * Go binary serves both in production.
 */
import createClient from 'openapi-fetch';
import type { components, paths } from './schema.d.ts';

export type Schemas = components['schemas'];
export type PackSummary = Schemas['Summary'];
export type RoomView = Schemas['RoomView'];
export type CreateRoomRequest = Omit<Schemas['CreateRoomRequest'], '$schema'>;
export type JoinRequest = Omit<Schemas['JoinRequest'], '$schema'>;
export type JoinResponse = Schemas['JoinResponse'];
export type BuzzerPreset = Schemas['BuzzerPreset'];
export type SystemInfo = Schemas['SystemInfo'];
export type Problem = Schemas['ErrorModel'];

export const api = createClient<paths>({ baseUrl: '' });

/** RFC 9457 problem turned into an Error with the machine-readable code. */
export class ApiError extends Error {
  status: number;
  code: string | null;
  problem: Problem | null;
  constructor(status: number, problem: Problem | null, fallback: string) {
    super(problem?.detail || problem?.title || fallback);
    this.name = 'ApiError';
    this.status = status;
    this.problem = problem;
    const first = problem?.errors?.[0];
    this.code = typeof first?.value === 'string' ? first.value : null;
  }
}

function unwrap<T>(res: { data?: T; error?: unknown; response: Response }): T {
  if (res.error !== undefined || !res.response.ok) {
    const problem = (res.error ?? null) as Problem | null;
    throw new ApiError(res.response.status, problem, `HTTP ${res.response.status}`);
  }
  return res.data as T;
}

// ---- packs -------------------------------------------------------------------

export async function listPacks(params: { q?: string; limit?: number; offset?: number } = {}) {
  const res = await api.GET('/api/v1/packs', { params: { query: { limit: 50, ...params } } });
  return unwrap(res);
}

export async function importSiq(file: File, dryRun = false) {
  const body = new FormData();
  body.append('file', file);
  const res = await fetch(`/api/v1/packs/import${dryRun ? '?dryRun=true' : ''}`, { method: 'POST', body });
  if (!res.ok) {
    let problem: Problem | null = null;
    try {
      problem = (await res.json()) as Problem;
    } catch {
      /* ignore */
    }
    throw new ApiError(res.status, problem, `HTTP ${res.status}`);
  }
  return (await res.json()) as Schemas['ImportResult'];
}

// ---- rooms -------------------------------------------------------------------

export async function listRooms() {
  return unwrap(await api.GET('/api/v1/rooms'));
}

export async function getRoom(code: string) {
  return unwrap(await api.GET('/api/v1/rooms/{code}', { params: { path: { code } } }));
}

export async function createRoom(body: CreateRoomRequest) {
  return unwrap(await api.POST('/api/v1/rooms', { body }));
}

export async function joinRoom(code: string, body: JoinRequest, hostToken?: string) {
  const headers: Record<string, string> = {};
  if (hostToken) headers['X-Host-Token'] = hostToken;
  return unwrap(await api.POST('/api/v1/rooms/{code}/join', { params: { path: { code } }, body, headers }));
}

export async function leaveRoom(code: string, sessionToken: string) {
  return unwrap(
    await api.POST('/api/v1/rooms/{code}/leave', { params: { path: { code } }, body: { sessionToken } }),
  );
}

export async function listBuzzerPresets() {
  return unwrap(await api.GET('/api/v1/buzzer-presets'));
}

export type RoomSettingsPatch = Omit<Schemas['UpdateRoomSettingsRequest'], '$schema'>;

/** Host-only: PATCH /rooms/{code}/settings with the creator's X-Host-Token. */
export async function updateRoomSettings(code: string, hostToken: string, body: RoomSettingsPatch) {
  return unwrap(
    await api.PATCH('/api/v1/rooms/{code}/settings', {
      params: { path: { code } },
      body,
      headers: { 'X-Host-Token': hostToken },
    }),
  );
}

/** Host-only: close the room for everyone. */
export async function closeRoom(code: string, hostToken: string) {
  const res = await api.DELETE('/api/v1/rooms/{code}', { params: { path: { code } }, headers: { 'X-Host-Token': hostToken } });
  if (!res.response.ok) throw new ApiError(res.response.status, (res.error ?? null) as Problem | null, `HTTP ${res.response.status}`);
}

// ---- media / system ------------------------------------------------------------

export function mediaUrl(mediaId: string): string {
  return `/media/${encodeURIComponent(mediaId)}`;
}

/** Resolves a content item's media reference: stored id or external URL. */
export function contentUrl(item: { mediaId?: string; url?: string }): string | null {
  if (item.mediaId) return mediaUrl(item.mediaId);
  return item.url ?? null;
}

export async function getSystemInfo() {
  return unwrap(await api.GET('/api/v1/system/info'));
}

export async function getHealth() {
  return unwrap(await api.GET('/api/v1/system/health'));
}
