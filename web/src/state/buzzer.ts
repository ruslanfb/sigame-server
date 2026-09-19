/** Buzzer store: mirror of the ArmController state + clock model + link quality. */
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { initialArmState, type ArmState } from '../buzzer/armController.ts';
import type { ClockModel, ConnQualityEntry } from '../ws/types.ts';

interface BuzzerState {
  arm: ArmState;
  model: ClockModel | null;
  syncAcks: number;
  connQuality: ConnQualityEntry[];
  soundEnabled: boolean;
  setArm: (a: ArmState) => void;
  setModel: (m: ClockModel, acks: number) => void;
  setConnQuality: (q: ConnQualityEntry[]) => void;
  setSound: (on: boolean) => void;
  reset: () => void;
}

export const useBuzzerStore = create<BuzzerState>()(
  persist(
    (set) => ({
      arm: initialArmState,
      model: null,
      syncAcks: 0,
      connQuality: [],
      soundEnabled: true,
      setArm: (arm) => set({ arm }),
      setModel: (model, syncAcks) => set({ model, syncAcks }),
      setConnQuality: (connQuality) => set({ connQuality }),
      setSound: (soundEnabled) => set({ soundEnabled }),
      reset: () => set({ arm: initialArmState, model: null, syncAcks: 0, connQuality: [] }),
    }),
    { name: 'sigame.buzzer', version: 1, partialize: (s) => ({ soundEnabled: s.soundEnabled }) },
  ),
);
