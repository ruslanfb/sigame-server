export function fmtMs(v: number | undefined | null, digits = 1): string {
  if (v === undefined || v === null || Number.isNaN(v)) return '—';
  return `${v.toFixed(digits)} ms`;
}

export function fmtSigned(v: number): string {
  return v > 0 ? `+${v}` : String(v);
}

export function fmtTime(unixMs: number): string {
  const d = new Date(unixMs);
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

export function normalizeRoomCode(s: string): string {
  return s.trim().toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 8);
}

/** Reads `?room=CODE` from a join URL or a query string. */
export function roomCodeFromLocation(search: string): string | null {
  const q = new URLSearchParams(search);
  const c = q.get('room');
  return c ? normalizeRoomCode(c) : null;
}
