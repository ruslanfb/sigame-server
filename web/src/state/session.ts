/**
 * Session store — persisted in localStorage so a reload (or a reconnect after
 * the tab was killed) can re-open the same room with the same person.
 */
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { Role } from '../ws/types.ts';

export interface Session {
  roomCode: string;
  sessionToken: string;
  personId: string;
  role: Role;
  isHost: boolean;
  /** Only the room creator has it; authorises host REST calls. */
  hostToken: string | null;
  name: string;
}

interface SessionState {
  session: Session | null;
  /** Last name used, kept for the join form. */
  lastName: string;
  setSession: (s: Session) => void;
  setHostFlag: (isHost: boolean) => void;
  setLastName: (name: string) => void;
  clear: () => void;
}

export const useSessionStore = create<SessionState>()(
  persist(
    (set) => ({
      session: null,
      lastName: '',
      setSession: (session) => set({ session, lastName: session.name }),
      setHostFlag: (isHost) => set((s) => (s.session ? { session: { ...s.session, isHost } } : {})),
      setLastName: (lastName) => set({ lastName }),
      clear: () => set({ session: null }),
    }),
    { name: 'sigame.session', version: 1 },
  ),
);

/** Route for a role inside a room. */
export function roomRouteFor(code: string, role: Role, isHost: boolean): string {
  if (role === 'showman' || (isHost && role !== 'player')) return `/room/${code}/host`;
  if (role === 'viewer') return `/room/${code}/table`;
  return `/room/${code}/player`;
}
