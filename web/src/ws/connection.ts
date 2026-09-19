/**
 * RoomConnection — the WebSocket transport of one room session.
 *
 * - Envelope codec `{t, seq, p}` (docs/protocol.md §2); client seq counter.
 * - Stamps `tRecv = performance.now()` in `onmessage` BEFORE JSON.parse so the
 *   buzzer/sync code sees the true arrival time.
 * - Tracks `lastSeq` from server envelopes; on reconnect sends `HELLO{lastSeq}`.
 * - Exponential reconnect 0.5 s → 10 s unless the close was terminal
 *   (4000 bad token, 4001 replaced, 4002 kicked, 1000 closed by us, ROOM_CLOSED).
 *
 * The class is transport-only: it does not know about stores. Consumers
 * subscribe with `on(type, handler)` / `onAny(handler)` / `onState(handler)`.
 */
import type {
  ClientMessages,
  ClientMessageType,
  Envelope,
  ServerMessage,
  ServerMessages,
  ServerMessageType,
  UnknownServerMessage,
} from './types.ts';
import { CloseCode } from './types.ts';

export type ConnectionState = 'idle' | 'connecting' | 'open' | 'reconnecting' | 'closed';

export interface ConnectParams {
  roomCode: string;
  sessionToken: string;
  /** Last seq processed on a previous connection (e.g. restored from a snapshot). */
  lastSeq?: number;
}

/** The subset of the WebSocket API the connection uses (lets tests inject a fake). */
export interface WebSocketLike {
  readonly readyState: number;
  onopen: ((ev: unknown) => void) | null;
  onmessage: ((ev: { data: unknown }) => void) | null;
  onclose: ((ev: { code: number; reason: string; wasClean?: boolean }) => void) | null;
  onerror: ((ev: unknown) => void) | null;
  send(data: string): void;
  close(code?: number, reason?: string): void;
}

export interface ConnectionOptions {
  /** Creates the socket; defaults to `new WebSocket(url)`. */
  wsFactory?: (url: string) => WebSocketLike;
  /** Base origin for the socket URL, e.g. `ws://localhost:8080`; defaults to `location`. */
  baseUrl?: string;
  now?: () => number;
  setTimeout?: (cb: () => void, ms: number) => unknown;
  clearTimeout?: (id: unknown) => void;
  reconnectMinMs?: number;
  reconnectMaxMs?: number;
  /** Deterministic jitter for tests (0..1); defaults to Math.random. */
  random?: () => number;
}

export interface CloseInfo {
  code: number;
  reason: string;
  byUs: boolean;
  willReconnect: boolean;
}

type Handler<T extends ServerMessageType> = (msg: {
  t: T;
  seq: number;
  p: ServerMessages[T];
  tRecv: number;
}) => void;
type AnyHandler = (msg: ServerMessage | UnknownServerMessage) => void;
type StateHandler = (state: ConnectionState, info?: CloseInfo) => void;

const WS_OPEN = 1;

export function buildWsUrl(baseUrl: string | undefined, roomCode: string, token: string): string {
  let origin: string;
  if (baseUrl) {
    origin = baseUrl.replace(/^http/, 'ws').replace(/\/$/, '');
  } else if (typeof location !== 'undefined') {
    origin = `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}`;
  } else {
    origin = 'ws://localhost:8080';
  }
  const q = new URLSearchParams({ room: roomCode, token });
  return `${origin}/ws?${q.toString()}`;
}

export class RoomConnection {
  private ws: WebSocketLike | null = null;
  private params: ConnectParams | null = null;
  private handlers = new Map<string, Set<Handler<ServerMessageType>>>();
  private anyHandlers = new Set<AnyHandler>();
  private stateHandlers = new Set<StateHandler>();
  private reconnectTimer: unknown = null;
  private attempt = 0;
  private closedByUs = false;
  private terminal = false;
  private hadConnection = false;
  private clientSeq = 0;
  private readonly opts: Required<
    Pick<ConnectionOptions, 'now' | 'setTimeout' | 'clearTimeout' | 'reconnectMinMs' | 'reconnectMaxMs' | 'random'>
  > &
    ConnectionOptions;

  state: ConnectionState = 'idle';
  /** Last server seq processed on this or the previous connection. */
  lastSeq = 0;
  /** Timestamp (performance.now) of the last inbound frame. */
  lastRecv = 0;

  constructor(options: ConnectionOptions = {}) {
    this.opts = {
      now: () => performance.now(),
      setTimeout: (cb, ms) => setTimeout(cb, ms),
      clearTimeout: (id) => clearTimeout(id as ReturnType<typeof setTimeout>),
      reconnectMinMs: 500,
      reconnectMaxMs: 10_000,
      random: Math.random,
      ...options,
    };
  }

  // ---- lifecycle ------------------------------------------------------------

  connect(params: ConnectParams): void {
    this.params = params;
    if (params.lastSeq !== undefined) this.lastSeq = params.lastSeq;
    this.closedByUs = false;
    this.terminal = false;
    this.attempt = 0;
    this.open();
  }

  /** Closes the socket for good (no reconnect). */
  close(code: number = CloseCode.Normal, reason = 'client left'): void {
    this.closedByUs = true;
    this.terminal = true;
    this.cancelReconnect();
    const ws = this.ws;
    this.ws = null;
    if (ws) {
      try {
        ws.close(code, reason);
      } catch {
        /* ignore */
      }
    }
    this.setState('closed', { code, reason, byUs: true, willReconnect: false });
  }

  /** Marks the session as finished server-side (ROOM_CLOSED etc.): no reconnect. */
  markTerminal(): void {
    this.terminal = true;
  }

  get isOpen(): boolean {
    return this.ws !== null && this.ws.readyState === WS_OPEN;
  }

  /** Reconnects now (e.g. after visibilitychange when the socket died silently). */
  reconnectNow(): void {
    if (!this.params || this.terminal) return;
    this.cancelReconnect();
    if (this.ws) {
      try {
        this.ws.close(CloseCode.Normal, 'reconnect');
      } catch {
        /* ignore */
      }
      this.ws = null;
    }
    this.open();
  }

  private open(): void {
    if (!this.params) return;
    const url = buildWsUrl(this.opts.baseUrl, this.params.roomCode, this.params.sessionToken);
    const factory = this.opts.wsFactory ?? ((u: string) => new WebSocket(u) as unknown as WebSocketLike);
    this.setState(this.hadConnection ? 'reconnecting' : 'connecting');
    let ws: WebSocketLike;
    try {
      ws = factory(url);
    } catch {
      this.scheduleReconnect();
      return;
    }
    this.ws = ws;
    ws.onopen = () => {
      if (this.ws !== ws) return;
      this.attempt = 0;
      const resumed = this.hadConnection;
      this.hadConnection = true;
      this.setState('open');
      // Resume: ask for a replay of what we missed (docs/protocol.md §1 step 4).
      if (resumed && this.lastSeq > 0) this.send('HELLO', { lastSeq: this.lastSeq });
    };
    ws.onmessage = (ev) => {
      // Stamp BEFORE parsing — this is the arrival time the protocol wants.
      const tRecv = this.opts.now();
      if (this.ws !== ws) return;
      this.lastRecv = tRecv;
      let env: Envelope;
      try {
        env = JSON.parse(String(ev.data)) as Envelope;
      } catch {
        return;
      }
      if (!env || typeof env.t !== 'string') return;
      const seq = typeof env.seq === 'number' ? env.seq : 0;
      if (seq > this.lastSeq) this.lastSeq = seq;
      if (env.t === 'ROOM_CLOSED' || env.t === 'KICKED' || env.t === 'SESSION_REPLACED') {
        this.terminal = true;
      }
      this.dispatch({ t: env.t, seq, p: env.p, tRecv } as ServerMessage);
    };
    ws.onclose = (ev) => {
      if (this.ws !== ws) return;
      this.ws = null;
      const byUs = this.closedByUs;
      const code = ev.code;
      const terminalCode =
        code === CloseCode.BadToken || code === CloseCode.SessionReplaced || code === CloseCode.Kicked;
      const willReconnect = !byUs && !terminalCode && !this.terminal;
      if (willReconnect) {
        this.setState('reconnecting', { code, reason: ev.reason, byUs, willReconnect });
        this.scheduleReconnect();
      } else {
        this.setState('closed', { code, reason: ev.reason, byUs, willReconnect });
      }
    };
    ws.onerror = () => {
      /* onclose follows; nothing to do */
    };
  }

  private scheduleReconnect(): void {
    if (this.terminal || this.closedByUs) return;
    this.cancelReconnect();
    const base = Math.min(this.opts.reconnectMaxMs, this.opts.reconnectMinMs * 2 ** this.attempt);
    const delay = Math.round(base * (0.8 + 0.4 * this.opts.random()));
    this.attempt += 1;
    this.setState('reconnecting');
    this.reconnectTimer = this.opts.setTimeout(() => {
      this.reconnectTimer = null;
      this.open();
    }, delay);
  }

  private cancelReconnect(): void {
    if (this.reconnectTimer !== null) {
      this.opts.clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }

  // ---- sending ---------------------------------------------------------------

  /** Sends an envelope; returns the client seq (0 when the socket is not open). */
  send<T extends ClientMessageType>(
    t: T,
    ...args: ClientMessages[T] extends undefined ? [payload?: undefined] : [payload: ClientMessages[T]]
  ): number {
    const ws = this.ws;
    if (!ws || ws.readyState !== WS_OPEN) return 0;
    const seq = ++this.clientSeq;
    const env: Envelope = { t, seq };
    const payload = args[0];
    if (payload !== undefined) env.p = payload;
    try {
      ws.send(JSON.stringify(env));
    } catch {
      return 0;
    }
    return seq;
  }

  // ---- subscriptions ---------------------------------------------------------

  on<T extends ServerMessageType>(type: T, handler: Handler<T>): () => void {
    let set = this.handlers.get(type);
    if (!set) {
      set = new Set();
      this.handlers.set(type, set);
    }
    set.add(handler as Handler<ServerMessageType>);
    return () => {
      set.delete(handler as Handler<ServerMessageType>);
    };
  }

  onAny(handler: AnyHandler): () => void {
    this.anyHandlers.add(handler);
    return () => {
      this.anyHandlers.delete(handler);
    };
  }

  onState(handler: StateHandler): () => void {
    this.stateHandlers.add(handler);
    return () => {
      this.stateHandlers.delete(handler);
    };
  }

  private dispatch(msg: ServerMessage): void {
    const set = this.handlers.get(msg.t);
    if (set) {
      for (const h of set) {
        try {
          h(msg);
        } catch (err) {
          console.error(`[ws] handler for ${msg.t} failed`, err);
        }
      }
    }
    for (const h of this.anyHandlers) {
      try {
        h(msg);
      } catch (err) {
        console.error('[ws] onAny handler failed', err);
      }
    }
  }

  private setState(state: ConnectionState, info?: CloseInfo): void {
    this.state = state;
    for (const h of this.stateHandlers) h(state, info);
  }
}
