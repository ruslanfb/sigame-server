const KEY = 'sigame.debug';

/** Debug panel visibility: `?debug=1` in the URL or a persisted toggle. */
export function readDebugFlag(search: string): boolean {
  try {
    if (new URLSearchParams(search).get('debug') === '1') return true;
    return localStorage.getItem(KEY) === '1';
  } catch {
    return false;
  }
}

export function writeDebugFlag(on: boolean): void {
  try {
    if (on) localStorage.setItem(KEY, '1');
    else localStorage.removeItem(KEY);
  } catch {
    /* ignore */
  }
}
