/**
 * In-memory WebSocket double for connection tests. The test drives it: `open()`
 * fires onopen, `receive(obj)` delivers a server envelope, `serverClose()`
 * simulates the peer closing the socket.
 */
import type { WebSocketLike } from '../ws/connection.ts';

export const WS_CONNECTING = 0;
export const WS_OPEN = 1;
export const WS_CLOSING = 2;
export const WS_CLOSED = 3;

export class FakeWebSocket implements WebSocketLike {
  readonly url: string;
  readyState = WS_CONNECTING;
  sent: string[] = [];
  closedWith: { code?: number; reason?: string } | null = null;
  onopen: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: ((ev: { code: number; reason: string; wasClean?: boolean }) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;

  constructor(url: string) {
    this.url = url;
  }

  /** Parsed envelopes sent by the client. */
  get frames(): { t: string; seq?: number; p?: unknown }[] {
    return this.sent.map((s) => JSON.parse(s) as { t: string; seq?: number; p?: unknown });
  }

  open(): void {
    this.readyState = WS_OPEN;
    this.onopen?.({});
  }

  receive(obj: unknown): void {
    this.onmessage?.({ data: typeof obj === 'string' ? obj : JSON.stringify(obj) });
  }

  serverClose(code: number, reason = ''): void {
    this.readyState = WS_CLOSED;
    this.onclose?.({ code, reason, wasClean: true });
  }

  send(data: string): void {
    if (this.readyState !== WS_OPEN) throw new Error('socket not open');
    this.sent.push(data);
  }

  close(code?: number, reason?: string): void {
    this.closedWith = { code, reason };
    this.readyState = WS_CLOSED;
    // A real socket fires onclose asynchronously; tests that care call serverClose.
  }
}
