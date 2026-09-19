import { create } from 'zustand';

export interface Toast {
  id: number;
  kind: 'error' | 'info' | 'success';
  text: string;
  /** Client seq of the request that caused an ERROR (protocol `ref`). */
  ref?: number;
  at: number;
}

interface ToastState {
  toasts: Toast[];
  push: (t: Omit<Toast, 'id' | 'at'>) => void;
  dismiss: (id: number) => void;
}

let nextId = 1;

export const useToastStore = create<ToastState>()((set) => ({
  toasts: [],
  push: (t) =>
    set((s) => ({ toasts: [...s.toasts.slice(-4), { ...t, id: nextId++, at: Date.now() }] })),
  dismiss: (id) => set((s) => ({ toasts: s.toasts.filter((x) => x.id !== id) })),
}));

export function toastError(text: string, ref?: number): void {
  useToastStore.getState().push({ kind: 'error', text, ref });
}
