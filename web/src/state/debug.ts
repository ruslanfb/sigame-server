/** Debug store: connection state + the last N inbound message types, for the debug panel. */
import { create } from 'zustand';
import type { ConnectionState } from '../ws/connection.ts';

export interface DebugEntry {
  t: string;
  seq: number;
  tRecv: number;
}

interface DebugState {
  connection: ConnectionState;
  lastClose: { code: number; reason: string } | null;
  recent: DebugEntry[];
  setConnection: (c: ConnectionState, close?: { code: number; reason: string } | null) => void;
  pushMessage: (e: DebugEntry) => void;
  reset: () => void;
}

const MAX_RECENT = 20;

export const useDebugStore = create<DebugState>()((set) => ({
  connection: 'idle',
  lastClose: null,
  recent: [],
  setConnection: (connection, close) =>
    set((s) => ({ connection, lastClose: close === undefined ? s.lastClose : close })),
  pushMessage: (e) => set((s) => ({ recent: [...s.recent.slice(-(MAX_RECENT - 1)), e] })),
  reset: () => set({ connection: 'idle', lastClose: null, recent: [] }),
}));
