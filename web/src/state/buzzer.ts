/** Buzzer store: mirror of the ArmController state + clock model + link quality + the last race. */
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { initialArmState, type ArmState } from '../buzzer/armController.ts';
import type { ButtonResultPayload, ClockModel, ConnQualityEntry, LockoutPayload } from '../ws/types.ts';

export interface LockoutEntry extends LockoutPayload {
  /** Local receive instant. */
  at: number;
}

interface BuzzerState {
  arm: ArmState;
  model: ClockModel | null;
  syncAcks: number;
  connQuality: ConnQualityEntry[];
  soundEnabled: boolean;
  vibrateEnabled: boolean;
  /** Last BUTTON_RESULT of the room (kept across disarm, for the host panel and the table). */
  lastResult: ButtonResultPayload | null;
  /** Players whose press entered the current fairness window (PRESS_PENDING), cleared on a new arm/result. */
  pendingPresses: string[];
  /** Recent LOCKOUT broadcasts (who jumped the gun). */
  lockouts: LockoutEntry[];
  setArm: (a: ArmState) => void;
  setModel: (m: ClockModel, acks: number) => void;
  setConnQuality: (q: ConnQualityEntry[]) => void;
  setSound: (on: boolean) => void;
  setVibrate: (on: boolean) => void;
  setLastResult: (r: ButtonResultPayload | null) => void;
  pushPending: (playerId: string) => void;
  clearPending: () => void;
  pushLockout: (l: LockoutEntry) => void;
  reset: () => void;
}

const MAX_LOCKOUTS = 20;

export const useBuzzerStore = create<BuzzerState>()(
  persist(
    (set) => ({
      arm: initialArmState,
      model: null,
      syncAcks: 0,
      connQuality: [],
      soundEnabled: true,
      vibrateEnabled: true,
      lastResult: null,
      pendingPresses: [],
      lockouts: [],
      setArm: (arm) => set({ arm }),
      setModel: (model, syncAcks) => set({ model, syncAcks }),
      setConnQuality: (connQuality) => set({ connQuality }),
      setSound: (soundEnabled) => set({ soundEnabled }),
      setVibrate: (vibrateEnabled) => set({ vibrateEnabled }),
      setLastResult: (lastResult) => set({ lastResult, pendingPresses: [] }),
      pushPending: (id) => set((s) => (s.pendingPresses.includes(id) ? s : { pendingPresses: [...s.pendingPresses, id] })),
      clearPending: () => set({ pendingPresses: [] }),
      pushLockout: (l) => set((s) => ({ lockouts: [...s.lockouts.slice(-(MAX_LOCKOUTS - 1)), l] })),
      reset: () =>
        set({ arm: initialArmState, model: null, syncAcks: 0, connQuality: [], lastResult: null, pendingPresses: [], lockouts: [] }),
    }),
    {
      name: 'sigame.buzzer',
      version: 2,
      partialize: (s) => ({ soundEnabled: s.soundEnabled, vibrateEnabled: s.vibrateEnabled }),
    },
  ),
);
