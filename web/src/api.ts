// Typed calls to the Husic server. Every song the app shows is an Item:
// library songs carry songId; search and recommendation results only carry
// the platform copies they were found on.

export type Track = {
  source: string;
  id: string;
  title: string;
  artists: string[];
  album?: string;
  duration: number;
  cover?: string;
  extra?: Record<string, string>;
  pinned?: boolean;
};

export type Item = {
  songId?: number;
  title: string;
  artists: string[];
  album: string;
  duration: number;
  cover: string;
  copies: Track[];
  liked: boolean;
  playedAt?: string;
};

export type Playlist = {
  id: number;
  name: string;
  description: string;
  cover: string;
  origin: string;
  count: number;
  covers: string[];
  updatedAt: string;
};

/** A playlist on a platform (recommendations, charts) that can be opened. */
export type Card = { source: string; id: string; name: string; cover: string; count: number; note?: string };

/** A recommended song and why it was picked. */
export type Pick = Item & { reason: string };

export type Chart = { source: string; id: string; name: string; short: string };

export type Discover = {
  hero: Pick | null;
  picks: Card[];
  radar: Pick[];
  fresh: Pick[];
  more: Pick[];
  world: Pick[];
  random: Pick[];
  similar: { seed: Item; songs: Pick[] } | null;
};

export type Home = {
  daily: Item[];
  recommendations?: Item[];
  picks: Card[];
  recent: Item[];
  playlists: Playlist[];
  fm: Item[];
  liked: Item[];
};

/** Last answers of slow pages, kept in memory and in localStorage, so a page
 * can draw at once and refresh behind the scenes. */
const memo = new Map<string, { at: number; value: unknown }>();

export function remember<T>(key: string, value: T) {
  const entry = { at: Date.now(), value };
  memo.set(key, entry);
  try {
    localStorage.setItem(`husic:${key}`, JSON.stringify(entry));
  } catch {
    // storage full or blocked: memory is enough
  }
}

export function recall<T>(key: string): { at: number; value: T } | null {
  let entry = memo.get(key);
  if (!entry) {
    try {
      const raw = localStorage.getItem(`husic:${key}`);
      if (raw) memo.set(key, (entry = JSON.parse(raw)));
    } catch {
      return null;
    }
  }
  return (entry as { at: number; value: T }) ?? null;
}

export type Stream = { url: string; source: string; id: string; bitrate: number; format: string };
export type ConnectionAttempt = { id: string; kind: string; state: string; expires: string; message?: string };
export type AccountPlatform = 'bilibili';
export type QRKind = 'netease' | AccountPlatform;
export type AccountState = { state: string; name?: string };
export type AccountStatus = { netease: AccountState; youtube: { state: string; lastSuccess?: string }; bilibili?: AccountState };
export type AccountCollection = { id: string; name: string; cover: string; count: number };
export type AccountCollectionPage = { collection: AccountCollection; songs: Item[]; total: number; next: number };

export type Word = { t: number; d: number; w: string };
export type LyricLine = { t: number; text: string; tr?: string; words?: Word[] };
export type Lyrics = { lines: LyricLine[]; credits?: string[]; synced: boolean };

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const response = await fetch(path, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: 'same-origin',
  });
  if (response.status === 401 || (response.redirected && new URL(response.url).origin !== location.origin)) {
    // A login gateway in front of Husic wants the page again. Reload once a
    // minute at most, so a gateway that keeps refusing cannot loop forever.
    const last = Number(sessionStorage.getItem('husic-login-reload') || 0);
    if (Date.now() - last > 60_000) {
      sessionStorage.setItem('husic-login-reload', String(Date.now()));
      location.reload();
    }
    throw new ApiError(401, '需要重新登录');
  }
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new ApiError(response.status, data.error ?? response.statusText);
  return data as T;
}

export const api = {
  accounts: () => call<AccountStatus>('GET', '/api/accounts'),
  startNetEase: () => call<ConnectionAttempt>('POST', '/api/accounts/netease/login', {}),
  checkNetEase: (id: string) => call<ConnectionAttempt>('POST', `/api/accounts/netease/login/${encodeURIComponent(id)}/check`, {}),
  startConnection: (kind: QRKind) => call<ConnectionAttempt>('POST', `/api/accounts/${kind}/login`, {}),
  checkConnection: (kind: QRKind, id: string) =>
    call<ConnectionAttempt>('POST', `/api/accounts/${kind}/login/${encodeURIComponent(id)}/check`, {}),
  accountCollections: (kind: AccountPlatform) => call<{ collections: AccountCollection[] }>('GET', `/api/accounts/${kind}/collections`),
  accountCollection: (kind: AccountPlatform, id: string, page = 1) =>
    call<AccountCollectionPage>('GET', `/api/accounts/${kind}/collections/${encodeURIComponent(id)}?page=${page}`),
  bilibiliVideo: (id: string) =>
    call<{ video: { id: string; title: string; cover: string }; songs: Item[] }>('GET', `/api/bilibili/videos/${encodeURIComponent(id)}`),
  startYouTube: () => call<ConnectionAttempt>('POST', '/api/accounts/youtube/login', {}),
  checkYouTube: () => call<{ ok: boolean; youtube: AccountStatus['youtube'] }>('POST', '/api/accounts/youtube/check', {}),
  cancelConnection: (id: string) => call<{ ok: boolean }>('DELETE', `/api/accounts/attempts/${encodeURIComponent(id)}`, {}),
  search: (q: string) => call<{ songs: Item[] }>('GET', `/api/search?q=${encodeURIComponent(q)}`),
  stream: (item: Item, rejected: string[] = []) => call<Stream>('POST', '/api/stream', { item, rejected }),
  lyrics: (item: Item) => call<Lyrics>('POST', '/api/lyrics', { item }),
  like: (item: Item, liked: boolean) => call<{ songId: number; liked: boolean }>('POST', '/api/likes', { item, liked }),
  play: (item: Item, source: string, id: string, seconds: number) =>
    call<{ songId: number }>('POST', '/api/plays', { item, source, id, seconds }),
  library: () => call<{ likes: Item[]; playlists: Playlist[] }>('GET', '/api/library'),
  home: () => call<Home>('GET', '/api/home'),
  discover: () => call<Discover>('GET', '/api/discover'),
  fm: () => call<{ songs: Item[] }>('GET', '/api/fm'),
  radio: () => call<{ songs: Item[] }>('GET', '/api/radio'),
  charts: () => call<{ charts: Chart[] }>('GET', '/api/charts'),
  chart: (source: string, id: string) => call<{ chart: Chart; updated: string; songs: Item[] }>('GET', `/api/charts/${source}/${id}`),
  playlist: (id: number) => call<{ playlist: Playlist; songs: Item[] }>('GET', `/api/playlists/${id}`),
  remotePlaylist: (id: string) => call<{ playlist: Card; songs: Item[] }>('GET', `/api/netease/playlists/${id}`),
  createPlaylist: (name: string) => call<{ id: number }>('POST', '/api/playlists', { name }),
  renamePlaylist: (id: number, name: string, description = '', cover = '') =>
    call<{ ok: boolean }>('PATCH', `/api/playlists/${id}`, { name, description, cover }),
  deletePlaylist: (id: number) => call<{ ok: boolean }>('DELETE', `/api/playlists/${id}`),
  addToPlaylist: (id: number, items: Item[]) => call<{ songIds: number[] }>('POST', `/api/playlists/${id}/songs`, { items }),
  removeFromPlaylist: (id: number, songId: number) => call<{ ok: boolean }>('DELETE', `/api/playlists/${id}/songs/${songId}`),
  pin: (songId: number, source: string, id: string) => call<{ ok: boolean }>('POST', `/api/songs/${songId}/pin`, { source, id }),
  syncNetEase: () => call<{ liked: number; playlists: number }>('POST', '/api/sync/netease'),
};

export const sourceNames: Record<string, string> = {
  netease: '网易云',
  qq: 'QQ 音乐',
  kuwo: '酷我',
  kugou: '酷狗',
  migu: '咪咕',
  youtube: 'YouTube',
  bilibili: 'B站',
};

export const refOf = (t: Track) => `${t.source}:${t.id}`;

export function inspectionID(item: Item) {
  return item.copies.length > 0 && item.copies.every((copy) => copy.source === 'bilibili' && copy.extra?.inspect === 'parts')
    ? item.copies[0].id
    : null;
}

export function artistLine(item: { artists: string[] }) {
  return item.artists.join(' / ');
}

export function clock(seconds: number) {
  if (!Number.isFinite(seconds) || seconds < 0) return '0:00';
  const s = Math.floor(seconds);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}

/** Platform artwork URLs accept a size hint; ask for what we draw. */
export function sized(url: string, px: number) {
  if (!url) return '';
  if (url.includes('music.126.net')) return `${url.replace(/\?.*$/, '')}?param=${px}y${px}`;
  if (url.includes('y.gtimg.cn')) return url.replace(/R\d+x\d+/, px > 300 ? 'R800x800' : 'R300x300');
  return url.replace(/^http:\/\//, 'https://');
}
