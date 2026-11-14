// Remote control (GET /api/remote/listen): a program on the server — the
// smart-speaker bridge — asks this player to play the liked songs or a
// playlist, skip, pause, resume or stop. The server sends each command to
// the player the owner used last; every player says when it was used
// (POST /api/remote/active) so the right one answers.

export type RemoteAction = 'play' | 'next' | 'pause' | 'resume' | 'stop';
export type RemoteCommand = { action: RemoteAction; what: string };

const ACTIONS = new Set<RemoteAction>(['play', 'next', 'pause', 'resume', 'stop']);

export function parseCommand(data: string): RemoteCommand | null {
  try {
    const value = JSON.parse(data) as { action?: unknown; what?: unknown };
    if (typeof value.action !== 'string' || !ACTIONS.has(value.action as RemoteAction)) return null;
    return { action: value.action as RemoteAction, what: typeof value.what === 'string' ? value.what : '' };
  } catch {
    return null;
  }
}

/** The playlist whose name matches best: exact, then containing, else null. */
export function pickPlaylist<T extends { name: string }>(lists: T[], name: string): T | null {
  const want = name.trim().toLowerCase();
  if (!want) return null;
  return (
    lists.find((p) => p.name.toLowerCase() === want) ??
    lists.find((p) => p.name.toLowerCase().includes(want) || want.includes(p.name.toLowerCase())) ??
    null
  );
}

/** A stable id for this player (desktop app or browser tab). */
export function playerID(): string {
  const desktop = Boolean((window as unknown as { chrome?: { webview?: unknown } }).chrome?.webview);
  const key = 'husic-remote-player';
  let id = sessionStorage.getItem(key);
  if (!id) {
    id = `${desktop ? 'exe' : 'web'}-${Math.random().toString(36).slice(2, 10)}`;
    sessionStorage.setItem(key, id);
  }
  return id;
}

let lastSent = 0;

/** Tells the server this player is the one in use (at most every 20 s). */
export function markActive(id: string, force = false) {
  const now = Date.now();
  if (!force && now - lastSent < 20_000) return;
  lastSent = now;
  fetch('/api/remote/active', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ player: id }),
    credentials: 'same-origin',
    keepalive: true,
  }).catch(() => {});
}

/** Listens for commands; the browser reconnects by itself. Returns a stop. */
export function listen(id: string, on: (c: RemoteCommand) => void): () => void {
  if (typeof EventSource === 'undefined') return () => {};
  const source = new EventSource(`/api/remote/listen?player=${encodeURIComponent(id)}`, { withCredentials: true });
  source.onmessage = (event) => {
    const command = parseCommand(String(event.data));
    if (command) on(command);
  };
  return () => source.close();
}
