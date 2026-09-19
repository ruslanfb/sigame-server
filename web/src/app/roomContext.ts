import { createContext, useContext } from 'react';
import type { ArmController } from '../buzzer/armController.ts';
import type { RoomConnection } from '../ws/connection.ts';
import type { SyncController } from '../ws/sync.ts';

export interface RoomHandles {
  conn: RoomConnection;
  sync: SyncController;
  arm: ArmController;
}

export const RoomContext = createContext<RoomHandles | null>(null);

export function useRoom(): RoomHandles {
  const ctx = useContext(RoomContext);
  if (!ctx) throw new Error('useRoom must be used inside <RoomShell>');
  return ctx;
}
