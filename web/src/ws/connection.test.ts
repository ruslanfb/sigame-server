import { beforeEach, describe, expect, it, vi } from 'vitest';
import { toastError, useToastStore } from '../state/toast.ts';
import { FakeClock } from '../test/fakeClock.ts';
import { FakeWebSocket } from '../test/fakeWebSocket.ts';
import { buildWsUrl, RoomConnection, type CloseInfo, type ConnectionState } from './connection.ts';

function setup() {
  const clock = new FakeClock(1000);
  const sockets: FakeWebSocket[] = [];
  const conn = new RoomConnection({
    wsFactory: (url) => {
      const ws = new FakeWebSocket(url);
      sockets.push(ws);
      return ws;
    },
    baseUrl: 'http://h:1',
    now: clock.now,
    setTimeout: clock.setTimeout,
    clearTimeout: clock.clearTimeout,
    random: () => 0.5, // reconnect delay = base exactly
  });
  const states: { state: ConnectionState; info?: CloseInfo }[] = [];
  conn.onState((state, info) => states.push({ state, info }));
  return { clock, sockets, conn, states };
}

const params = { roomCode: 'K7Q2M', sessionToken: 'tok' };

describe('buildWsUrl', () => {
  it('builds ws:// from an http base and encodes the query', () => {
    expect(buildWsUrl('http://h:1', 'K7Q2M', 'tok')).toBe('ws://h:1/ws?room=K7Q2M&token=tok');
    expect(buildWsUrl('https://h/', 'K7Q2M', 'a b')).toBe('wss://h/ws?room=K7Q2M&token=a+b');
  });
});

describe('RoomConnection', () => {
  let s: ReturnType<typeof setup>;
  beforeEach(() => {
    s = setup();
  });

  it('connects to the room URL and reports state transitions', () => {
    s.conn.connect(params);
    expect(s.sockets).toHaveLength(1);
    expect(s.sockets[0]!.url).toBe('ws://h:1/ws?room=K7Q2M&token=tok');
    expect(s.conn.state).toBe('connecting');
    expect(s.conn.isOpen).toBe(false);
    s.sockets[0]!.open();
    expect(s.conn.state).toBe('open');
    expect(s.conn.isOpen).toBe(true);
    expect(s.states.map((x) => x.state)).toEqual(['connecting', 'open']);
    // First connection: no HELLO.
    expect(s.sockets[0]!.sent).toHaveLength(0);
  });

  it('encodes envelopes {t, seq, p} with a monotonic client seq', () => {
    s.conn.connect(params);
    expect(s.conn.send('CHAT', { text: 'early' })).toBe(0); // not open yet
    s.sockets[0]!.open();
    expect(s.conn.send('CHAT', { text: 'hi' })).toBe(1);
    expect(s.conn.send('READY')).toBe(2);
    expect(s.conn.send('SYNC', { seq: 1, c1: 12.5 })).toBe(3);
    const frames = s.sockets[0]!.frames;
    expect(frames[0]).toEqual({ t: 'CHAT', seq: 1, p: { text: 'hi' } });
    expect(frames[1]).toEqual({ t: 'READY', seq: 2 });
    expect(frames[1]).not.toHaveProperty('p');
    expect(frames[2]).toEqual({ t: 'SYNC', seq: 3, p: { seq: 1, c1: 12.5 } });
    expect(JSON.parse(s.sockets[0]!.sent[0]!)).toEqual(frames[0]);
  });

  it('decodes inbound envelopes, stamps tRecv and tracks lastSeq', () => {
    s.conn.connect(params);
    s.sockets[0]!.open();
    const got: unknown[] = [];
    s.conn.on('CHAT', (m) => got.push(m));
    const before = s.clock.peek();
    s.sockets[0]!.receive({ t: 'CHAT', seq: 5, p: { personId: 'a', name: 'A', text: 'x', atMs: 1 } });
    expect(got).toHaveLength(1);
    const m = got[0] as { t: string; seq: number; p: unknown; tRecv: number };
    expect(m.t).toBe('CHAT');
    expect(m.seq).toBe(5);
    expect(m.p).toEqual({ personId: 'a', name: 'A', text: 'x', atMs: 1 });
    expect(m.tRecv).toBeCloseTo(before, 0);
    expect(s.conn.lastSeq).toBe(5);
    expect(s.conn.lastRecv).toBe(m.tRecv);
    // An older seq does not move lastSeq back; a missing seq counts as 0.
    s.sockets[0]!.receive({ t: 'CHAT', seq: 3, p: {} });
    expect(s.conn.lastSeq).toBe(5);
    s.sockets[0]!.receive({ t: 'CHAT', p: {} });
    expect(s.conn.lastSeq).toBe(5);
    s.sockets[0]!.receive({ t: 'CHAT', seq: 9, p: {} });
    expect(s.conn.lastSeq).toBe(9);
  });

  it('onAny sees every message, including unknown types; bad frames are ignored', () => {
    s.conn.connect(params);
    s.sockets[0]!.open();
    const all: string[] = [];
    s.conn.onAny((m) => all.push(m.t));
    s.sockets[0]!.receive({ t: 'FOO_BAR', seq: 1, p: { x: 1 } });
    s.sockets[0]!.receive('{not json');
    s.sockets[0]!.receive({ seq: 2 }); // no type
    s.sockets[0]!.receive({ t: 'STAGE', seq: 3, p: { stage: 'lobby', roundIndex: 0 } });
    expect(all).toEqual(['FOO_BAR', 'STAGE']);
    expect(s.conn.lastSeq).toBe(3);
  });

  it('a throwing handler does not break dispatch to the others', () => {
    s.conn.connect(params);
    s.sockets[0]!.open();
    const err = vi.spyOn(console, 'error').mockImplementation(() => {});
    const seen: string[] = [];
    s.conn.on('CHAT', () => {
      throw new Error('boom');
    });
    s.conn.on('CHAT', () => seen.push('second'));
    s.conn.onAny(() => seen.push('any'));
    s.sockets[0]!.receive({ t: 'CHAT', seq: 1, p: {} });
    expect(seen).toEqual(['second', 'any']);
    expect(err).toHaveBeenCalled();
    err.mockRestore();
  });

  it('reconnects after a non-terminal close and resumes with HELLO{lastSeq}', () => {
    s.conn.connect(params);
    s.sockets[0]!.open();
    s.sockets[0]!.receive({ t: 'CHAT', seq: 57, p: {} });
    s.sockets[0]!.serverClose(1006, 'abnormal');
    expect(s.conn.state).toBe('reconnecting');
    expect(s.conn.isOpen).toBe(false);
    const closeInfo = s.states.find((x) => x.info)?.info;
    expect(closeInfo).toMatchObject({ code: 1006, reason: 'abnormal', byUs: false, willReconnect: true });
    expect(s.sockets).toHaveLength(1);
    // Backoff: 500 ms × (0.8 + 0.4 × 0.5) = 500 ms.
    s.clock.advance(400);
    expect(s.sockets).toHaveLength(1);
    s.clock.advance(200);
    expect(s.sockets).toHaveLength(2);
    expect(s.sockets[1]!.url).toBe(s.sockets[0]!.url);
    s.sockets[1]!.open();
    expect(s.conn.state).toBe('open');
    expect(s.sockets[1]!.frames).toEqual([{ t: 'HELLO', seq: 1, p: { lastSeq: 57 } }]);
    expect(s.conn.lastSeq).toBe(57);
    // The dead socket's late events are ignored.
    s.sockets[0]!.receive({ t: 'CHAT', seq: 99, p: {} });
    expect(s.conn.lastSeq).toBe(57);
  });

  it('backs off exponentially while the socket keeps failing', () => {
    s.conn.connect(params);
    s.sockets[0]!.open();
    s.sockets[0]!.serverClose(1006);
    s.clock.advance(500); // attempt 1 → socket 2
    expect(s.sockets).toHaveLength(2);
    s.sockets[1]!.serverClose(1006);
    s.clock.advance(900);
    expect(s.sockets).toHaveLength(2);
    s.clock.advance(100); // 1000 ms
    expect(s.sockets).toHaveLength(3);
    s.sockets[2]!.serverClose(1006);
    s.clock.advance(1999);
    expect(s.sockets).toHaveLength(3);
    s.clock.advance(1); // 2000 ms
    expect(s.sockets).toHaveLength(4);
    // A successful open resets the backoff.
    s.sockets[3]!.open();
    s.sockets[3]!.serverClose(1006);
    s.clock.advance(500);
    expect(s.sockets).toHaveLength(5);
  });

  it('reconnects with a lastSeq restored from a previous session', () => {
    s.conn.connect({ ...params, lastSeq: 12 });
    s.sockets[0]!.open();
    // A fresh connection (not a resume) never sends HELLO, even with a stored lastSeq.
    expect(s.sockets[0]!.sent).toHaveLength(0);
    s.sockets[0]!.serverClose(1006);
    s.clock.advance(500);
    s.sockets[1]!.open();
    expect(s.sockets[1]!.frames).toEqual([{ t: 'HELLO', seq: 1, p: { lastSeq: 12 } }]);
  });

  it.each([
    [4000, 'bad token'],
    [4001, 'session replaced'],
    [4002, 'kicked'],
  ])('does not reconnect after close %i (%s)', (code) => {
    s.conn.connect(params);
    s.sockets[0]!.open();
    s.sockets[0]!.serverClose(code, 'bye');
    expect(s.conn.state).toBe('closed');
    const info = s.states.at(-1)?.info;
    expect(info).toMatchObject({ code, byUs: false, willReconnect: false });
    s.clock.advance(60_000);
    expect(s.sockets).toHaveLength(1);
    expect(s.clock.pending()).toBe(0);
  });

  it.each(['ROOM_CLOSED', 'KICKED', 'SESSION_REPLACED'])('%s marks the session terminal', (t) => {
    s.conn.connect(params);
    s.sockets[0]!.open();
    s.sockets[0]!.receive({ t, seq: 1, p: {} });
    s.sockets[0]!.serverClose(1000, 'closed');
    expect(s.conn.state).toBe('closed');
    s.clock.advance(60_000);
    expect(s.sockets).toHaveLength(1);
  });

  it('close() by us closes the socket for good', () => {
    s.conn.connect(params);
    s.sockets[0]!.open();
    s.conn.close();
    expect(s.sockets[0]!.closedWith).toEqual({ code: 1000, reason: 'client left' });
    expect(s.conn.state).toBe('closed');
    expect(s.states.at(-1)?.info).toMatchObject({ code: 1000, byUs: true, willReconnect: false });
    // Whatever the old socket does afterwards is ignored.
    s.sockets[0]!.serverClose(1006);
    s.clock.advance(60_000);
    expect(s.sockets).toHaveLength(1);
    expect(s.conn.send('READY')).toBe(0);
  });

  it('close() while a reconnect is pending cancels it', () => {
    s.conn.connect(params);
    s.sockets[0]!.open();
    s.sockets[0]!.serverClose(1006);
    expect(s.clock.pending()).toBe(1);
    s.conn.close();
    expect(s.clock.pending()).toBe(0);
    s.clock.advance(60_000);
    expect(s.sockets).toHaveLength(1);
  });

  it('reconnectNow() replaces a live socket immediately', () => {
    s.conn.connect(params);
    s.sockets[0]!.open();
    s.conn.reconnectNow();
    expect(s.sockets).toHaveLength(2);
    expect(s.sockets[0]!.closedWith?.code).toBe(1000);
    expect(s.conn.state).toBe('reconnecting');
    s.sockets[1]!.open();
    expect(s.conn.state).toBe('open');
  });

  it('ERROR reaches its handler, which can surface it as a toast', () => {
    s.conn.connect(params);
    s.sockets[0]!.open();
    s.conn.on('ERROR', (m) => toastError(`${m.p.code}: ${m.p.message}`, m.p.ref));
    s.sockets[0]!.receive({ t: 'ERROR', seq: 41, p: { code: 'badState', message: 'no question', ref: 17 } });
    const toast = useToastStore.getState().toasts.find((t) => t.text === 'badState: no question');
    expect(toast).toBeDefined();
    expect(toast).toMatchObject({ kind: 'error', ref: 17 });
    useToastStore.getState().dismiss(toast!.id);
    expect(useToastStore.getState().toasts.find((t) => t.id === toast!.id)).toBeUndefined();
  });

  it('subscriptions can be removed', () => {
    s.conn.connect(params);
    s.sockets[0]!.open();
    const h = vi.fn();
    const off = s.conn.on('CHAT', h);
    s.sockets[0]!.receive({ t: 'CHAT', seq: 1, p: {} });
    off();
    s.sockets[0]!.receive({ t: 'CHAT', seq: 2, p: {} });
    expect(h).toHaveBeenCalledTimes(1);
  });

  it('a factory failure schedules a reconnect instead of throwing', () => {
    const clock = new FakeClock(1000);
    let calls = 0;
    const made: FakeWebSocket[] = [];
    const conn = new RoomConnection({
      wsFactory: (url) => {
        calls += 1;
        if (calls === 1) throw new Error('SecurityError');
        const ws = new FakeWebSocket(url);
        made.push(ws);
        return ws;
      },
      baseUrl: 'http://h:1',
      now: clock.now,
      setTimeout: clock.setTimeout,
      clearTimeout: clock.clearTimeout,
      random: () => 0.5,
    });
    expect(() => conn.connect(params)).not.toThrow();
    expect(conn.state).toBe('reconnecting');
    clock.advance(500);
    expect(made).toHaveLength(1);
  });
});
