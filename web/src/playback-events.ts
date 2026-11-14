// Records where each play/pause came from so a speaker or OS sending
// play/pause behind the owner's back can be told apart from a click. Events
// are batched to the server log (POST /api/playback-events); nothing is kept.

export type PlaybackSource = 'button' | 'mediasession' | 'desktop' | 'auto' | 'audio' | 'remote';

export type PlaybackEvent = { at: number; action: string; from: PlaybackSource | string; state: string };

export type Recorder = {
  /** A caller is about to ask the player to play/pause. */
  intent: (action: 'play' | 'pause', from: PlaybackSource, state: string) => void;
  /** The audio element actually changed; unmatched changes are logged as from "audio". */
  observed: (action: 'play' | 'pause', state: string) => void;
  flush: () => PlaybackEvent[];
};

const INTENT_WINDOW = 1500; // an audio event this soon after an intent is that intent's result

export function createRecorder(now: () => number = Date.now, limit = 50): Recorder {
  let queue: PlaybackEvent[] = [];
  const lastIntent: Record<string, number> = {};
  const push = (e: PlaybackEvent) => {
    queue.push(e);
    if (queue.length > limit) queue = queue.slice(-limit);
  };
  return {
    intent(action, from, state) {
      lastIntent[action] = now();
      push({ at: now(), action, from, state });
    },
    observed(action, state) {
      const t = lastIntent[action];
      if (t !== undefined && now() - t <= INTENT_WINDOW) return;
      push({ at: now(), action, from: 'audio', state });
    },
    flush() {
      const out = queue;
      queue = [];
      return out;
    },
  };
}

export const playbackRecorder = createRecorder();

let timer: ReturnType<typeof setTimeout> | null = null;

/** Sends what was recorded a moment later, in one request. */
export function scheduleSend() {
  if (timer !== null) return;
  timer = setTimeout(() => {
    timer = null;
    const events = playbackRecorder.flush();
    if (!events.length) return;
    fetch('/api/playback-events', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ events }),
      keepalive: true,
    }).catch(() => {});
  }, 2000);
}
