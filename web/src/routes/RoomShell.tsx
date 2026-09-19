import { useCallback, useState, type ReactNode } from 'react';
import { Navigate, useLocation, useNavigate, useParams } from 'react-router';
import { leaveRoom } from '../api/client.ts';
import { RoomContext } from '../app/roomContext.ts';
import { useRoomSession } from '../app/useRoomSession.ts';
import { Button } from '../components/Button.tsx';
import { DebugPanel } from '../components/DebugPanel.tsx';
import { Modal } from '../components/Modal.tsx';
import { readDebugFlag, writeDebugFlag } from '../lib/debugFlag.ts';
import { normalizeRoomCode } from '../lib/format.ts';
import { ROOM_CLOSED_LABEL } from '../lib/labels.ts';
import { useDebugStore } from '../state/debug.ts';
import { useRoomStore } from '../state/room.ts';
import { roomRouteFor, sessionKey, useSessionStore, type RoomPage, type Session } from '../state/session.ts';

/**
 * Shared frame of the three room pages: mounts the connection (useRoomSession),
 * redirects to the join page when there is no session for this room, sends a
 * person with the wrong role to their own page, shows the terminal dialog
 * (room closed / kicked / replaced) and the optional debug panel.
 */
export function RoomShell({ page, children }: { page: RoomPage; children: (ctx: { session: Session; leave: () => void }) => ReactNode }) {
  const params = useParams<{ code: string }>();
  const code = normalizeRoomCode(params.code ?? '');
  const navigate = useNavigate();
  const location = useLocation();
  const { status, handles, session } = useRoomSession(code, page);
  const closedReason = useRoomStore((s) => s.closedReason);
  const kicked = useRoomStore((s) => s.kicked);
  const replaced = useRoomStore((s) => s.replaced);
  const lastClose = useDebugStore((s) => s.lastClose);
  const connection = useDebugStore((s) => s.connection);
  const clear = useSessionStore((s) => s.clear);
  const [debug, setDebug] = useState(() => readDebugFlag(location.search));

  const leave = useCallback(() => {
    const key = sessionKey(code, page);
    const s = useSessionStore.getState().sessions[key];
    if (s) {
      leaveRoom(s.roomCode, s.sessionToken).catch(() => undefined);
    }
    clear(key);
    navigate('/');
  }, [clear, code, navigate, page]);

  if (status !== 'ready' || !session) return <Navigate to={`/?room=${encodeURIComponent(code)}`} replace />;
  const expected = roomRouteFor(code, session.role, session.isHost);
  if (location.pathname !== expected) return <Navigate to={expected} replace />;

  const terminal = closedReason
    ? (ROOM_CLOSED_LABEL[closedReason] ?? 'Комната закрыта')
    : kicked
      ? kicked.banned
        ? 'Ведущий заблокировал вас в этой комнате'
        : 'Ведущий удалил вас из комнаты'
      : replaced
        ? 'Эта сессия открыта в другой вкладке'
        : connection === 'closed' && lastClose?.code === 4000
          ? 'Сессия устарела — войдите заново'
          : null;

  return (
    <>
      {handles ? <RoomContext.Provider value={handles}>{children({ session, leave })}</RoomContext.Provider> : null}
      {debug && (
        <div className="mx-auto w-full max-w-5xl p-3">
          <DebugPanel />
        </div>
      )}
      <button
        type="button"
        onClick={() => {
          const next = !debug;
          setDebug(next);
          writeDebugFlag(next);
        }}
        className="fixed right-1 bottom-1 z-30 rounded-sm px-1.5 py-0.5 font-mono text-[10px] text-muted/40 hover:text-muted"
        aria-label="Панель отладки"
      >
        dbg
      </button>
      <Modal open={terminal !== null} title={terminal ?? ''}>
        <p className="text-muted">Соединение закрыто.</p>
        <div className="mt-4 flex justify-end">
          <Button variant="gold" onClick={leave}>
            На главную
          </Button>
        </div>
      </Modal>
    </>
  );
}
