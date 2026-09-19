/** Join link for a room: the server's advertised join URL when known, else this origin. */
export function joinUrlFor(code: string, serverJoinUrls?: (string | null | undefined)[] | null): string {
  const base = serverJoinUrls?.find((u): u is string => typeof u === 'string' && u.length > 0);
  if (base) {
    try {
      const u = new URL(base);
      u.pathname = '/';
      u.search = '';
      u.searchParams.set('room', code);
      return u.toString();
    } catch {
      /* fall through */
    }
  }
  const origin = typeof location !== 'undefined' ? location.origin : '';
  return `${origin}/?room=${encodeURIComponent(code)}`;
}

/** The link without the scheme, for reading aloud / typing. */
export function shortUrl(url: string): string {
  return url.replace(/^https?:\/\//, '');
}
