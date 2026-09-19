import type { ReactNode } from 'react';
import { Navigate, useNavigate, useParams } from 'react-router';
import { RoomContext } from '../app/roomContext.ts';
import { useRoomSession } from '../app/useRoomSession.ts';
import { Button } from '../components/Button.tsx';
import { DebugPanel } from '../components/DebugPanel.tsx';
import { Modal } from '../components/Modal.tsx';
import { leaveRoom } from '../api/client.ts';
import { normalizeRoomCode } from '../lib/format.ts';
import { useDebugStore } from '../state/debug.ts';
import { useRoomStore } from '../state/room.ts';
import { useSessionStore } from '../state/session.ts';

/**
 * Shared frame of the three room pages: mounts the connection (useRoomSession),
 * redirects to the join page when there is no session for this room, renders
 * the header, the page body and the debug panel.
 */
export function RoomShell({ title, children }: { title: string; children: ReactNode }) {
  const params = useParams<{ code: string }>();
  const code = normalizeRoomCode(params.code ?? '');
  const navigate = useNavigate();
  const { status, handles, session } = useRoomSession(code);
  const connection = useDebugStore((s) => s.connection);
  const info = useRoomStore((s) => s.info);
  const closedReason = useRoomStore((s) => s.closedReason);
  const kicked = useRoomStore((s) => s.kicked);
  const replaced = useRoomStore((s) => s.replaced);
  const clear = useSessionStore((s) => s.clear);

  if (status !== 'ready') return <Navigate to={`/?room=${encodeURIComponent(code)}`} replace />;

  const leave = async () => {
    if (session) {
      try {
        await leaveRoom(session.roomCode, session.sessionToken);
      } catch {
        /* the socket close is enough */
      }
    }
    clear();
    navigate('/');
  };

  const terminal = closedReason ? `Room closed (${closedReason})` : kicked ? (kicked.banned ? 'You were banned' : 'You were kicked') : replaced ? 'Session opened elsewhere' : null;

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-4 p-4">
      <header className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h1 className="text-lg font-semibold">
            {title} · <span className="font-mono">{code}</span>
          </h1>
          <p className="text-xs text-muted">
            {info?.name ?? '…'} · {info?.packName ?? ''} · {session?.name} ({session?.role}
            {session?.isHost ? ', host' : ''}) · <ConnBadge state={connection} />
          </p>
        </div>
        <Button variant="secondary" onClick={leave}>
          leave
        </Button>
      </header>
      {handles ? <RoomContext.Provider value={handles}>{children}</RoomContext.Provider> : null}
      <DebugPanel />
      <Modal open={terminal !== null} title={terminal ?? ''} onClose={leave}>
        The connection is closed.
      </Modal>
    </div>
  );
}

function ConnBadge({ state }: { state: string }) {
  const color = state === 'open' ? 'text-success' : state === 'closed' ? 'text-danger' : 'text-warning';
  return <span className={color}>{state}</span>;
}
