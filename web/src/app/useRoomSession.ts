/**
 * useRoomSession — mounts the connection layer for a room page and wires every
 * inbound message into the stores. One instance per room page.
 */
import { useEffect, useMemo, useRef, useState } from 'react';
import { ArmController } from '../buzzer/armController.ts';
import { beep, primeAudio } from '../buzzer/beep.ts';
import { fmtDelta, WS_ERROR_LABEL } from '../lib/labels.ts';
import { useBuzzerStore } from '../state/buzzer.ts';
import { useDebugStore } from '../state/debug.ts';
import { useGameStore, type ReduceContext } from '../state/game.ts';
import { useRoomStore } from '../state/room.ts';
import { sessionKey, useSessionStore, type RoomPage, type Session } from '../state/session.ts';
import { toastError, useToastStore } from '../state/toast.ts';
import { RoomConnection } from '../ws/connection.ts';
import { SyncController } from '../ws/sync.ts';
import type { RoomHandles } from './roomContext.ts';

export type RoomSessionStatus = 'noSession' | 'wrongRoom' | 'ready';

export function useRoomSession(code: string, page: RoomPage): { status: RoomSessionStatus; handles: RoomHandles | null; session: Session | null } {
  const key = sessionKey(code, page);
  const session = useSessionStore((s) => s.sessions[key] ?? null);
  const status: RoomSessionStatus = !session ? 'noSession' : session.roomCode !== code ? 'wrongRoom' : 'ready';
  const [handles, setHandles] = useState<RoomHandles | null>(null);
  const sessionRef = useRef(session);
  useEffect(() => {
    sessionRef.current = session;
  }, [session]);

  const token = status === 'ready' ? session!.sessionToken : null;

  useEffect(() => {
    if (!token) return;
    const conn = new RoomConnection();
    const sync = new SyncController(conn, {
      onModel: (m) => useBuzzerStore.getState().setModel(m, sync.acks),
    });
    const arm = new ArmController({
      send: (t, p) => {
        conn.send(t, p as never);
      },
      now: () => performance.now(),
      setTimeout: (cb, ms) => window.setTimeout(cb, ms),
      clearTimeout: (id) => window.clearTimeout(id as number),
      raf: (cb) => {
        requestAnimationFrame(() => cb());
      },
      toLocal: (ms) => sync.toLocal(ms),
      toServer: (ms) => sync.toServer(ms),
      myId: () => sessionRef.current?.personId ?? null,
      onChange: (st) => useBuzzerStore.getState().setArm(st),
      onLight: () => {
        const b = useBuzzerStore.getState();
        if (b.soundEnabled) beep();
        if (b.vibrateEnabled && typeof navigator !== 'undefined' && typeof navigator.vibrate === 'function') {
          try {
            navigator.vibrate(30);
          } catch {
            /* best effort */
          }
        }
      },
    });

    const ctx = (tRecv: number): ReduceContext => ({
      me: sessionRef.current?.personId ?? '',
      role: sessionRef.current?.role ?? 'viewer',
      tRecv,
      toLocal: (ms) => sync.toLocal(ms),
    });

    const debug = useDebugStore.getState();
    const room = useRoomStore.getState();
    const game = useGameStore.getState();
    const buzzer = useBuzzerStore.getState();
    debug.reset();
    room.reset();
    game.reset();
    buzzer.reset();

    const offs: (() => void)[] = [];
    offs.push(
      conn.onState((state, info) => {
        useDebugStore.getState().setConnection(state, info ? { code: info.code, reason: info.reason } : undefined);
        if (state !== 'open') sync.stop();
      }),
      conn.onAny((msg) => {
        useDebugStore.getState().pushMessage({ t: msg.t, seq: msg.seq, tRecv: msg.tRecv });
        useGameStore.getState().apply(msg as never, ctx(msg.tRecv));
      }),
      conn.on('WELCOME', (m) => {
        useRoomStore.getState().setWelcome(m.p);
        useSessionStore.getState().setHostFlag(key, m.p.isHost);
        sync.start(m.p.syncPlan, { serverTimeMs: m.p.serverTimeMs, tRecv: m.tRecv });
      }),
      conn.on('SNAPSHOT', (m) => {
        useRoomStore.getState().setInfo(m.p.room);
        useGameStore.getState().applySnapshot(m.p.game, ctx(m.tRecv));
        arm.onSnapshot(m.p.buzzer);
      }),
      conn.on('SYNC_ACK', (m) => sync.onAck(m.p, m.tRecv)),
      conn.on('BUTTON_ARM', (m) => {
        arm.onButtonArm(m.p);
        useBuzzerStore.getState().clearPending();
      }),
      conn.on('PRESS_ACK', (m) => arm.onPressAck(m.p)),
      conn.on('PRESS_PENDING', (m) => {
        arm.onPressPending(m.p);
        useBuzzerStore.getState().pushPending(m.p.playerId);
      }),
      conn.on('BUTTON_RESULT', (m) => {
        arm.onButtonResult(m.p);
        useBuzzerStore.getState().setLastResult(m.p);
      }),
      conn.on('LOCKOUT', (m) => {
        arm.onLockout(m.p);
        useBuzzerStore.getState().pushLockout({ ...m.p, at: m.tRecv });
      }),
      conn.on('QUESTION_START', () => {
        sync.preArmBurst();
        useBuzzerStore.getState().setLastResult(null);
      }),
      conn.on('QUESTION_END', () => arm.disarm()),
      conn.on('ROUND_END', () => arm.disarm()),
      conn.on('ROOM_PERSONS', (m) => useRoomStore.getState().setPersons(m.p.persons)),
      conn.on('ROOM_SETTINGS', (m) => useRoomStore.getState().setInfo(m.p)),
      conn.on('HOST_CHANGED', (m) => {
        useRoomStore.getState().setHost(m.p.personId);
        useSessionStore.getState().setHostFlag(key, m.p.personId === sessionRef.current?.personId);
      }),
      conn.on('CHAT', (m) => useRoomStore.getState().pushChat(m.p)),
      conn.on('KICKED', (m) => useRoomStore.getState().setKicked(m.p.banned)),
      conn.on('ROOM_CLOSED', (m) => useRoomStore.getState().setClosed(m.p.reason)),
      conn.on('SESSION_REPLACED', () => useRoomStore.getState().setReplaced()),
      conn.on('CONN_QUALITY', (m) => useBuzzerStore.getState().setConnQuality(m.p.players)),
      conn.on('ERROR', (m) => {
        if (m.p.code === 'badToken') return; // the close dialog covers it
        toastError(`${WS_ERROR_LABEL[m.p.code] ?? m.p.code}: ${m.p.message}`, m.p.ref);
      }),
      conn.on('USER_ERROR', (m) => toastError(`${WS_ERROR_LABEL[m.p.code] ?? m.p.code}: ${m.p.message}`)),
      conn.on('VALIDATION', (m) => {
        const me = sessionRef.current;
        if (!me || me.role !== 'player' || m.p.personId !== me.personId) return;
        const right = m.p.right;
        useToastStore.getState().push({
          kind: right ? 'success' : 'error',
          text: right ? (m.p.factor && m.p.factor !== 1 ? `Засчитано частично (×${m.p.factor})` : 'Верно!') : 'Неверно',
        });
      }),
      conn.on('PERSON_SCORE', (m) => {
        const me = sessionRef.current;
        if (!me || me.role !== 'player' || m.p.personId !== me.personId || m.p.reason === 'answer') return;
        useToastStore.getState().push({ kind: 'info', text: `Счёт: ${fmtDelta(m.p.delta)}` });
      }),
      conn.on('RESUME', (m) => {
        if (!m.p.covered) useToastStore.getState().push({ kind: 'info', text: 'Соединение восстановлено' });
      }),
    );

    const onVisibility = () => {
      if (document.visibilityState !== 'visible') return;
      if (conn.isOpen) sync.onVisible();
      else if (conn.state === 'reconnecting') conn.reconnectNow();
    };
    document.addEventListener('visibilitychange', onVisibility);
    const onGesture = () => primeAudio();
    window.addEventListener('pointerdown', onGesture, { passive: true, once: true });

    conn.connect({ roomCode: sessionRef.current!.roomCode, sessionToken: token });
    setHandles({ conn, sync, arm });

    return () => {
      document.removeEventListener('visibilitychange', onVisibility);
      window.removeEventListener('pointerdown', onGesture);
      for (const off of offs) off();
      sync.stop();
      arm.dispose();
      conn.close();
      setHandles(null);
    };
  }, [code, key, token]);

  return useMemo(() => ({ status, handles, session }), [status, handles, session]);
}
