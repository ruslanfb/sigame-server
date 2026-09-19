/** Room store: identity (WELCOME), room info/settings, persons and chat. */
import { create } from 'zustand';
import type { ChatPayload, PersonView, RoomInfo, WelcomePayload } from '../ws/types.ts';

interface RoomState {
  welcome: WelcomePayload | null;
  info: RoomInfo | null;
  persons: PersonView[];
  chat: ChatPayload[];
  hostId: string | null;
  closedReason: string | null;
  kicked: { banned: boolean } | null;
  replaced: boolean;
  setWelcome: (w: WelcomePayload) => void;
  setInfo: (i: RoomInfo) => void;
  setPersons: (p: PersonView[]) => void;
  setHost: (id: string) => void;
  pushChat: (c: ChatPayload) => void;
  setClosed: (reason: string) => void;
  setKicked: (banned: boolean) => void;
  setReplaced: () => void;
  reset: () => void;
}

const MAX_CHAT = 200;

const empty = {
  welcome: null,
  info: null,
  persons: [],
  chat: [],
  hostId: null,
  closedReason: null,
  kicked: null,
  replaced: false,
};

export const useRoomStore = create<RoomState>()((set) => ({
  ...empty,
  setWelcome: (welcome) => set({ welcome, closedReason: null, kicked: null, replaced: false }),
  setInfo: (info) => set({ info, persons: info.players, hostId: info.players.find((p) => p.isHost)?.id ?? null }),
  setPersons: (persons) => set({ persons, hostId: persons.find((p) => p.isHost)?.id ?? null }),
  setHost: (hostId) =>
    set((s) => ({ hostId, persons: s.persons.map((p) => ({ ...p, isHost: p.id === hostId })) })),
  pushChat: (c) => set((s) => ({ chat: [...s.chat.slice(-(MAX_CHAT - 1)), c] })),
  setClosed: (closedReason) => set({ closedReason }),
  setKicked: (banned) => set({ kicked: { banned } }),
  setReplaced: () => set({ replaced: true }),
  reset: () => set({ ...empty }),
}));

export function personName(persons: PersonView[], id: string | undefined | null): string {
  if (!id) return '—';
  if (id === 'ai-showman') return 'AI showman';
  return persons.find((p) => p.id === id)?.name ?? id.slice(0, 8);
}
