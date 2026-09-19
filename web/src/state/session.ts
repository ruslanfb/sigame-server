/**
 * Session store — persisted in localStorage so a reload (or a reconnect after
 * the tab was killed) can re-open the same room with the same person.
 *
 * Sessions are keyed by `${roomCode}/${page}` (page = host | player | table),
 * so the host's laptop can keep the console and the TV table of the same room
 * open side by side, each with its own person.
 */
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { Role } from '../ws/types.ts';

export type RoomPage = 'host' | 'player' | 'table';

export interface Session {
  roomCode: string;
  sessionToken: string;
  personId: string;
  role: Role;
  isHost: boolean;
  /** Only the room creator has it; authorises host REST calls. */
  hostToken: string | null;
  name: string;
  /** Share link from POST /rooms (creator only). */
  joinUrl?: string;
}

interface SessionState {
  sessions: Record<string, Session>;
  /** Last name used, kept for the join form. */
  lastName: string;
  setSession: (s: Session) => void;
  setHostFlag: (key: string, isHost: boolean) => void;
  setLastName: (name: string) => void;
  clear: (key: string) => void;
}

/** Page a person lands on for their role. */
export function pageFor(role: Role, isHost: boolean): RoomPage {
  if (role === 'showman' || (isHost && role !== 'player')) return 'host';
  if (role === 'viewer') return 'table';
  return 'player';
}

export function sessionKey(code: string, page: RoomPage): string {
  return `${code}/${page}`;
}

/** Route for a role inside a room. */
export function roomRouteFor(code: string, role: Role, isHost: boolean): string {
  return `/room/${code}/${pageFor(role, isHost)}`;
}

export const useSessionStore = create<SessionState>()(
  persist(
    (set) => ({
      sessions: {},
      lastName: '',
      setSession: (session) =>
        set((s) => ({
          sessions: { ...s.sessions, [sessionKey(session.roomCode, pageFor(session.role, session.isHost))]: session },
          lastName: session.name,
        })),
      setHostFlag: (key, isHost) =>
        set((s) => {
          const cur = s.sessions[key];
          return cur ? { sessions: { ...s.sessions, [key]: { ...cur, isHost } } } : {};
        }),
      setLastName: (lastName) => set({ lastName }),
      clear: (key) =>
        set((s) => {
          const sessions = { ...s.sessions };
          delete sessions[key];
          return { sessions };
        }),
    }),
    { name: 'sigame.session', version: 2, migrate: () => ({ sessions: {}, lastName: '' }) },
  ),
);

/** Sessions ordered newest-first is not tracked; return them in insertion order. */
export function listSessions(sessions: Record<string, Session>): Session[] {
  return Object.values(sessions);
}
