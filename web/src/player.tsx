import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { api, refOf, sized, inspectionID, type Item, type Stream } from './api';
import { navigate } from './router';
import { toast } from './toast';
import { useDesktopLyrics } from './desktop-lyrics';
import { localAudioID, localAudioStream, recordLocalAudioPlay } from './local-audio';
import { consumeExpiredSleepTimer, useSleepTimerPlayback } from './sleep-timer';
import { usePageVisible } from './view-activity';
import { nextIndex as nextInQueue, orderAfterAppend, orderAfterInsertNext, orderAfterRemove, shuffled } from './queue';

// One <audio> element for the whole app life: iOS keeps playing in the
// background and on the lock screen only if the same element is reused.

export type Repeat = 'all' | 'one';

type PlayerState = {
  queue: Item[];
  index: number;
  current: Item | null;
  stream: Stream | null;
  playing: boolean;
  loading: boolean;
  shuffle: boolean;
  repeat: Repeat;
  volume: number;
  muted: boolean;
};

type Player = PlayerState & {
  /** Display-only veil: real queue items and resolver inputs stay untouched. */
  presentation: Item | null;
  blindSession: { item: Item; seconds: number; revealed: boolean } | null;
  playBlind: (items: Item[], index: number) => void;
  revealBlind: () => void;
  audio: HTMLAudioElement;
  play: (items: Item[], index?: number) => void;
  /** Plays an endless list: refill is asked for more when the queue runs low. */
  playRadio: (items: Item[], refill: () => Promise<Item[]>) => void;
  radio: boolean;
  toggle: () => void;
  pause: () => void;
  next: () => void;
  previous: () => void;
  seek: (seconds: number) => void;
  jump: (index: number) => void;
  playNext: (item: Item) => void;
  addToQueue: (item: Item) => void;
  removeAt: (index: number) => void;
  moveUpcoming: (from: number, to: number) => void;
  playbackOrder: number[];
  setShuffle: (on: boolean) => void;
  cycleRepeat: () => void;
  setVolume: (v: number) => void;
  toggleMute: () => void;
};

const PlayerContext = createContext<Player | null>(null);
const keyOf = (item: Item) => (item.songId ? `song:${item.songId}` : item.copies.map(refOf).join('|'));

export function usePlayer() {
  const player = useContext(PlayerContext);
  if (!player) throw new Error('usePlayer outside PlayerProvider');
  return player;
}

/** Re-renders with the playback position, a few times a second. */
export function usePosition(enabled = true) {
  const { audio } = usePlayer();
  const visible = usePageVisible();
  const [position, setPosition] = useState({ time: audio.currentTime, duration: audio.duration || 0 });
  useEffect(() => {
    if (!visible || !enabled) return;
    const update = () => {
      const time = audio.currentTime, duration = audio.duration || 0;
      setPosition((before) => before.time === time && before.duration === duration ? before : { time, duration });
    };
    update();
    const events = ['timeupdate', 'durationchange', 'seeked', 'loadedmetadata', 'emptied'];
    events.forEach((e) => audio.addEventListener(e, update));
    return () => events.forEach((e) => audio.removeEventListener(e, update));
  }, [audio, visible, enabled]);
  return position;
}

const saved: Partial<PlayerState> & { playbackOrder?: number[] } = (() => {
  try {
    const value = JSON.parse(localStorage.getItem('player') ?? '{}') as Partial<PlayerState> & { playbackOrder?: number[] };
    // Older versions truncated the queue but kept an out-of-range index.
    const queue = Array.isArray(value.queue) ? value.queue : [];
    const index = Number.isInteger(value.index) && value.index! >= 0 && value.index! < queue.length ? value.index! : 0;
    const order=value.playbackOrder;
    const playbackOrder=Array.isArray(order)&&order.length===queue.length&&new Set(order).size===queue.length&&order.every(i=>Number.isInteger(i)&&i>=0&&i<queue.length)?order:[];
    return { ...value, queue, index, playbackOrder };
  } catch {
    return {};
  }
})();

export function PlayerProvider({ children }: { children: ReactNode }) {
  const pageVisible = usePageVisible();
  const audio = useMemo(() => {
    const element = new Audio();
    element.preload = 'auto';
    element.crossOrigin = null;
    element.setAttribute('playsinline', '');
    return element;
  }, []);
  const [state, setState] = useState<PlayerState>({
    queue: saved.queue ?? [],
    index: saved.index ?? 0,
    current: saved.queue?.[saved.index ?? 0] ?? null,
    stream: null,
    playing: false,
    loading: false,
    shuffle: saved.shuffle ?? false,
    repeat: saved.repeat === 'one' ? 'one' : 'all',
    volume: saved.volume ?? 1,
    muted: false,
  });
  const stateRef = useRef(state);
  stateRef.current = state;
  const order = useRef<number[]>(saved.playbackOrder ?? []); // shuffled play order of queue indexes
  const prefetched = useRef(new Map<string, Promise<Stream>>());
  const listened = useRef({ key: '', seconds: 0, last: 0 });
  const trackingSuspended = useRef(false);
  const attempt = useRef(0);
  const skipTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const failedSongs = useRef(new Set<string>());
  const wanted = useRef(false);
  const pauseForTimer = useRef<() => void>(() => {});
  const stopIfTimerExpired = useCallback(() => {
    if (!consumeExpiredSleepTimer()) return false;
    pauseForTimer.current();
    return true;
  }, []);
  const retryRound = useRef(0);
  const rejectedCopies = useRef<string[]>([]);
  const refill = useRef<(() => Promise<Item[]>) | null>(null);
  const [radio, setRadio] = useState(false);
  const [blindSession, setBlindSession] = useState<Player['blindSession']>(null);
  const blind = useRef<{ item: Item; seconds: number; last: number; revealed: boolean } | null>(null);
  const watchBlind = Boolean(blindSession && !blindSession.revealed);
  // Native media time continues while the screen is off. Reconcile at media
  // and seek boundaries rather than entering JS on every progress event.
  const settleListening = useCallback(() => {
    const record = listened.current, now = audio.currentTime, step = now - record.last;
    if (record.key && step > 0 && (trackingSuspended.current || step < 2)) record.seconds += step;
    record.last = now;
  }, [audio]);

  useEffect(() => {
    const { queue, index, shuffle, repeat, volume } = state;
    const start = Math.max(0, index - 150);
    const window = queue.slice(start, start + 300);
    try {
      const playbackOrder=order.current.length===queue.length?order.current.filter(i=>i>=start&&i<start+window.length).map(i=>i-start):undefined;
      localStorage.setItem('player', JSON.stringify({ queue: window, index: index - start, playbackOrder, shuffle, repeat, volume }));
    } catch { /* storage may be full or disabled; playback still works */ }
  }, [state.queue, state.index, state.shuffle, state.repeat, state.volume]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    audio.volume = state.volume;
    audio.muted = state.muted;
  }, [audio, state.volume, state.muted]);

  const resolve = useCallback((item: Item, rejected: string[] = []) => {
    const key = keyOf(item);
    if (rejected.length === 0) {
      const hit = prefetched.current.get(key);
      if (hit) return hit;
    }
    const localID=localAudioID(item);
    const pending = localID ? rejected.length?Promise.reject(new Error('这个音频文件暂时无法播放')):localAudioStream(item) : api.stream(item, rejected);
    prefetched.current.set(key, pending);
    // Signed addresses expire; never reuse one for long.
    setTimeout(() => prefetched.current.get(key) === pending && prefetched.current.delete(key), 8 * 60 * 1000);
    pending.catch(() => { if (prefetched.current.get(key) === pending) prefetched.current.delete(key); });
    return pending;
  }, []);

  /** Reports the finished (or skipped) listen so history and recommendations learn. */
  const report = useCallback(() => {
    settleListening();
    const { current, stream } = stateRef.current;
    const seconds = Math.round(listened.current.seconds);
    if (current && stream && seconds >= 20) {
      const localID=localAudioID(current);
      if(localID)void recordLocalAudioPlay(localID).catch(()=>{});
      else api.play(current, stream.source, stream.id, seconds).catch(() => {});
    }
    listened.current = { key: '', seconds: 0, last: 0 };
  }, [settleListening]);

  const load = useCallback(
    async (queue: Item[], index: number, autoplay = true, rejected: string[] = [], resumeAt = 0, automatic = false) => {
      if (stopIfTimerExpired() && automatic) return;
      if (automatic && !wanted.current) return;
      if (!automatic) wanted.current = autoplay;
      if (skipTimer.current !== null) { clearTimeout(skipTimer.current); skipTimer.current = null; }
      const item = queue[index];
      if (!item) return;
      if (blind.current && keyOf(blind.current.item) !== keyOf(item)) {
        blind.current = null;
        setBlindSession(null);
      }
      if (!automatic) failedSongs.current.clear();
      rejectedCopies.current = rejected;
      report();
      const token = ++attempt.current;
      audio.pause();
      setState((s) => ({ ...s, queue, index, current: item, playing: false, loading: true, stream: null }));
      try {
        const stream = await resolve(item, rejected);
        if (token !== attempt.current) return;
        if (stopIfTimerExpired()) return;
        audio.src = stream.url;
        if (resumeAt) audio.currentTime = resumeAt;
        setState((s) => ({ ...s, stream, loading: false }));
        if (autoplay && wanted.current) await audio.play();
        if (token !== attempt.current) return;
        failedSongs.current.clear();retryRound.current=0;
        listened.current = { key: keyOf(item), seconds: 0, last: resumeAt };
      } catch (err) {
        if (token !== attempt.current) return;
        setState((s) => ({ ...s, loading: false, playing: false }));
        if (!wanted.current) return;
        failedSongs.current.add(keyOf(item));
        toast(blind.current && !blind.current.revealed && keyOf(blind.current.item) === keyOf(item)
          ? '这首盲听暂时放不了，已跳过' : `《${item.title}》暂时放不了，已跳过`);
        // A pending stream may fail after the user reordered the queue. Read
        // the live queue both now and when the retry fires, never its old copy.
        const available = (s:PlayerState) => {
          let next=nextIndex(s.queue.length,s.index,s,order.current,true);
          for(let checked=0;next!==null&&failedSongs.current.has(keyOf(s.queue[next]))&&checked<s.queue.length;checked++)next=nextIndex(s.queue.length,next,s,order.current,true);
          return next!==null&&!failedSongs.current.has(keyOf(s.queue[next]))?next:null;
        };
        if (available(stateRef.current) !== null) {
          setState(s=>({...s,loading:true}));
          skipTimer.current = setTimeout(() => {
            if(token!==attempt.current||!wanted.current)return;
            const latest=stateRef.current,target=available(latest);
            if(target===null)failedSongs.current.clear();
            load(latest.queue,target??latest.index,autoplay,[],0,true);
          }, 400);
        } else if (autoplay && wanted.current) {
          setState(s=>({...s,loading:true}));
          const delay=Math.min(30000,5000*2**retryRound.current++);
          skipTimer.current=setTimeout(()=>{if(token!==attempt.current||!wanted.current)return;failedSongs.current.clear();const latest=stateRef.current;load(latest.queue,latest.index,true,[],0,true);},delay);
        }
      }
    },
    [audio, report, resolve],
  );

  // Warm up the next song's address so the change is instant (and works
  // when iOS runs us in the background).
  useEffect(() => {
    const { queue, index } = state;
    const next = nextIndex(queue.length, index, state, order.current, false);
    if (next !== null && queue[next]) resolve(queue[next]).catch(() => {});
  }, [state.index, state.queue, resolve]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    settleListening();
    trackingSuspended.current = !(pageVisible || watchBlind);
    const onPlay = () => setState((s) => ({ ...s, playing: true }));
    const onPause = () => { settleListening(); setState((s) => ({ ...s, playing: false })); };
    const onTime = () => {
      settleListening();
      const now = audio.currentTime;
      const session = blind.current;
      const current = stateRef.current.current;
      if (!session || session.revealed || !current || keyOf(session.item) !== keyOf(current)) return;
      const delta = now - session.last;
      session.last = now;
      if (!audio.paused && !audio.seeking && !stateRef.current.loading && delta > 0) {
        const before = Math.floor(session.seconds);
        session.seconds = Math.min(8, session.seconds + delta);
        if (session.seconds >= 8) session.revealed = true;
        if (session.revealed || Math.floor(session.seconds) !== before) {
          setBlindSession({ item: session.item, seconds: session.seconds, revealed: session.revealed });
        }
      }
    };
    const resetBlindPosition = () => { listened.current.last = audio.currentTime; if (blind.current) blind.current.last = audio.currentTime; };
    const onEnded = () => {
      if (stopIfTimerExpired()) return;
      const s = stateRef.current;
      if (!wanted.current) return;
      if (s.repeat === 'one' || s.queue.length === 1) {
        report();
        audio.currentTime = 0;
        listened.current = { key: s.current ? keyOf(s.current) : '', seconds: 0, last: 0 };
        const token=attempt.current;
        audio.play().catch(() => {if(token===attempt.current&&wanted.current)load(s.queue,s.index,true,[],0,true);});
        return;
      }
      const next = nextIndex(s.queue.length, s.index, s, order.current, true);
      if (next === null) {
        report();
        setState((st) => ({ ...st, playing: false }));
      } else load(s.queue, next, true, [], 0, true);
    };
    const onError = () => {
      const s = stateRef.current;
      if (!wanted.current || !s.current || !s.stream || !audio.src) return;
      // The platform CDN refused: ask the server for another copy and carry on.
      const rejected = [...new Set([...rejectedCopies.current, `${s.stream.source}:${s.stream.id}`])];
      load(s.queue, s.index, true, rejected, audio.currentTime, true);
    };
    audio.addEventListener('play', onPlay);
    audio.addEventListener('pause', onPause);
    if (!trackingSuspended.current) audio.addEventListener('timeupdate', onTime);
    audio.addEventListener('ended', onEnded);
    audio.addEventListener('error', onError);
    for (const event of ['play', 'pause', 'seeking', 'seeked', 'loadedmetadata']) audio.addEventListener(event, resetBlindPosition);
    return () => {
      audio.removeEventListener('play', onPlay);
      audio.removeEventListener('pause', onPause);
      audio.removeEventListener('timeupdate', onTime);
      audio.removeEventListener('ended', onEnded);
      audio.removeEventListener('error', onError);
      for (const event of ['play', 'pause', 'seeking', 'seeked', 'loadedmetadata']) audio.removeEventListener(event, resetBlindPosition);
    };
  }, [audio, load, report, settleListening, pageVisible, watchBlind]);

  const play = useCallback(
    (items: Item[], index = 0) => {
      const selected = items[index];
      if (!selected) return;
      const inspect = inspectionID(selected);
      if (inspect) { navigate(`/bilibili/${encodeURIComponent(inspect)}`); return; }
      const playable = items.filter((item) => !inspectionID(item));
      const start = playable.indexOf(selected);
      blind.current = null;
      setBlindSession(null);
      refill.current = null;
      setRadio(false);
      order.current = shuffled(playable.length, start);
      load(playable, start);
    },
    [load],
  );

  const playBlind = useCallback((items: Item[], index: number) => {
    const item = items[index];
    if (!item) return;
    play(items, index);
    blind.current = { item, seconds: 0, last: 0, revealed: false };
    setBlindSession({ item, seconds: 0, revealed: false });
  }, [play]);
  const revealBlind = useCallback(() => {
    if (!blind.current) return;
    blind.current.revealed = true;
    setBlindSession({ item: blind.current.item, seconds: blind.current.seconds, revealed: true });
  }, []);

  const playRadio = useCallback(
    (items: Item[], more: () => Promise<Item[]>) => {
      refill.current = more;
      setRadio(true);
      setState((s) => ({ ...s, shuffle: false, repeat: 'all' }));
      order.current = shuffled(items.length, 0);
      load(items, 0);
    },
    [load],
  );

  // Radio mode: top the queue up before it runs dry.
  useEffect(() => {
    const more = refill.current;
    if (!more || state.index < state.queue.length - 2) return;
    let alive = true;
    more()
      .then((items) => {
        if (!alive || refill.current !== more) return;
        setState((s) => {
          const known = new Set(s.queue.map((i) => i.copies.map(refOf).join('|')));
          const fresh = items.filter((i) => !known.has(i.copies.map(refOf).join('|')));
          return { ...s, queue: [...s.queue, ...fresh] };
        });
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [state.index, state.queue.length]);

  const pause = useCallback(()=>{
    wanted.current=false;++attempt.current;
    if(skipTimer.current!==null){clearTimeout(skipTimer.current);skipTimer.current=null;}
    audio.pause();setState(v=>({...v,loading:false,playing:false}));
  },[audio]);
  pauseForTimer.current = pause;
  useSleepTimerPlayback(audio, stopIfTimerExpired);
  const resume = useCallback(()=>{
    const s=stateRef.current;if(!s.current||wanted.current&&(s.loading||!audio.paused))return;
    wanted.current=true;const token=attempt.current;
    if(!audio.src||!s.stream)load(s.queue,s.index);
    else audio.play().catch(()=>{if(wanted.current&&token===attempt.current)load(s.queue,s.index,true,[],audio.currentTime);});
  },[audio,load]);
  const toggle = useCallback(()=>{
    const s=stateRef.current;if((s.loading&&wanted.current)||!audio.paused)pause();else resume();
  },[audio,pause,resume]);

  useEffect(()=>{
    const online=()=>{const s=stateRef.current;if(wanted.current&&s.loading&&!s.stream){failedSongs.current.clear();load(s.queue,s.index,true,[],0,true);}};
    window.addEventListener('online',online);return()=>window.removeEventListener('online',online);
  },[load]);

  const next = useCallback(() => {
    const s = stateRef.current;
    const n = nextIndex(s.queue.length, s.index, s, order.current, true);
    if (n !== null) load(s.queue, n);
  }, [load]);

  const previous = useCallback(() => {
    const s = stateRef.current;
    if (audio.currentTime > 4) {
      settleListening();
      audio.currentTime = 0;
      listened.current.last = 0;
      return;
    }
    const position = s.shuffle ? order.current.indexOf(s.index) : s.index;
    const prev = s.shuffle ? order.current[position - 1] : s.index - 1;
    if (prev !== undefined && prev >= 0) load(s.queue, prev);
    else { settleListening(); audio.currentTime = 0; listened.current.last = 0; }
  }, [audio, load, settleListening]);

  const seek = useCallback((seconds: number) => {
    settleListening();
    if (Number.isFinite(seconds)) audio.currentTime = Math.max(0, seconds);
    listened.current.last = audio.currentTime;
  }, [audio, settleListening]);

  const jump = useCallback((index: number) => load(stateRef.current.queue, index), [load]);

  const playNext = useCallback((item: Item) => {
    const inspect = inspectionID(item);
    if (inspect) { navigate(`/bilibili/${encodeURIComponent(inspect)}`); toast('先选择要听的分P'); return; }
    // Capture once: React StrictMode can evaluate the updater twice.
    const before = [...order.current];
    setState((s) => {
      if (!s.current) return s;
      const queue = [...s.queue];
      queue.splice(s.index + 1, 0, item);
      order.current = orderAfterInsertNext(before, s.queue.length, s.index) ?? shuffled(queue.length, s.index);
      return { ...s, queue };
    });
    toast('接下来播放');
  }, []);

  const addToQueue = useCallback((item: Item) => {
    const inspect = inspectionID(item);
    if (inspect) { navigate(`/bilibili/${encodeURIComponent(inspect)}`); toast('先选择要听的分P'); return; }
    const before = [...order.current];
    setState((s) => {
      order.current = orderAfterAppend(before, s.queue.length) ?? shuffled(s.queue.length + 1, s.index);
      return { ...s, queue: [...s.queue, item] };
    });
    toast('已加入待播');
  }, []);

  const removeAt = useCallback((index: number) => {
    const before = [...order.current];
    setState((s) => {
      if (index === s.index) return s;
      const queue = s.queue.filter((_, i) => i !== index);
      order.current = orderAfterRemove(before, s.queue.length, index) ?? shuffled(queue.length, index < s.index ? s.index - 1 : s.index);
      return { ...s, queue, index: index < s.index ? s.index - 1 : s.index };
    });
  }, []);

  const moveUpcoming = useCallback((from: number, to: number) => {
    // Capture once: React StrictMode can evaluate the updater twice.
    const before=[...order.current];
    setState(s=>{
      const visible=s.shuffle&&before.length===s.queue.length?before:s.queue.map((_,i)=>i);
      const current=visible.indexOf(s.index),a=visible.indexOf(from),b=visible.indexOf(to);
      if(a<=current||b<=current||a===b)return s;
      if(s.shuffle){
        const next=[...visible];next.splice(b,0,...next.splice(a,1));order.current=next;
        return {...s,queue:[...s.queue]};
      }
      const queue=[...s.queue];queue.splice(to,0,...queue.splice(from,1));
      return {...s,queue};
    });
  }, []);

  const setShuffle = useCallback((on: boolean) => {
    order.current = shuffled(stateRef.current.queue.length, stateRef.current.index);
    setState((s) => ({ ...s, shuffle: on }));
  }, []);

  const cycleRepeat = useCallback(
    () => setState((s) => ({ ...s, repeat: s.repeat === 'all' ? 'one' : 'all' })),
    [],
  );
  const setVolume = useCallback((v: number) => setState((s) => ({ ...s, volume: v, muted: false })), []);
  const toggleMute = useCallback(() => setState((s) => ({ ...s, muted: !s.muted })), []);

  const concealed = Boolean(blindSession && !blindSession.revealed && state.current && keyOf(blindSession.item) === keyOf(state.current));
  const presentation = useMemo(() => concealed && state.current
    ? { ...state.current, title: '先听，再看是谁', artists: ['盲听中 · 听满 8 秒揭晓'], album: '', cover: '' }
    : state.current, [concealed, state.current]);
  useMediaSession({ ...state, current: presentation }, audio, { toggle, pause, resume, next, previous, seek });
  useDesktopLyrics(state.current, audio, concealed);
  const playbackOrder = useMemo(() => state.shuffle && order.current.length === state.queue.length
    ? [...order.current] : state.queue.map((_, index) => index), [state.queue, state.index, state.shuffle]);

  const value = useMemo<Player>(
    () => ({ ...state, playbackOrder, presentation, blindSession, playBlind, revealBlind, audio, radio, play, playRadio, toggle, pause, next, previous, seek, jump, playNext, addToQueue, removeAt, moveUpcoming, setShuffle, cycleRepeat, setVolume, toggleMute }),
    [state, playbackOrder, presentation, blindSession, playBlind, revealBlind, audio, radio, play, playRadio, toggle, pause, next, previous, seek, jump, playNext, addToQueue, removeAt, moveUpcoming, setShuffle, cycleRepeat, setVolume, toggleMute],
  );
  return <PlayerContext.Provider value={value}>{children}</PlayerContext.Provider>;
}

/** The index after the current one, honouring shuffle. */
function nextIndex(length: number, index: number, s: Pick<PlayerState, 'shuffle'>, order: number[], wrap: boolean) {
  return nextInQueue(length, index, s.shuffle, order, wrap);
}

/** Lock screen, Control Center and headphone buttons. */
function useMediaSession(
  state: PlayerState,
  audio: HTMLAudioElement,
  actions: { toggle: () => void; pause:()=>void;resume:()=>void; next: () => void; previous: () => void; seek: (s: number) => void },
) {
  const { current, playing, loading } = state;
  useEffect(() => {
    if (!('mediaSession' in navigator) || !current) return;
    navigator.mediaSession.metadata = new MediaMetadata({
      title: current.title,
      artist: current.artists.join(' / '),
      album: current.album,
      artwork: current.cover
        ? [256, 512].map((px) => ({ src: sized(current.cover, px), sizes: `${px}x${px}`, type: 'image/jpeg' }))
        : [{ src: '/brand/husic-512.png', sizes: '512x512', type: 'image/png' }],
    });
  }, [current]);
  useEffect(() => {
    if (!('mediaSession' in navigator)) return;
    navigator.mediaSession.playbackState = playing || loading ? 'playing' : 'paused';
  }, [playing, loading]);
  useEffect(() => {
    if (!('mediaSession' in navigator)) return;
    const handlers: [MediaSessionAction, MediaSessionActionHandler][] = [
      ['play', () => actions.resume()],
      ['pause', () => actions.pause()],
      ['nexttrack', () => actions.next()],
      ['previoustrack', () => actions.previous()],
      ['seekto', (d) => d.seekTime !== undefined && actions.seek(d.seekTime)],
    ];
    for (const [action, handler] of handlers) {
      try {
        navigator.mediaSession.setActionHandler(action, handler);
      } catch {
        /* unsupported action */
      }
    }
  }, [actions.toggle, actions.pause, actions.resume, actions.next, actions.previous, actions.seek]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    const sync = () => {
      if (!('mediaSession' in navigator) || !audio.duration || !Number.isFinite(audio.duration)) return;
      try {
        navigator.mediaSession.setPositionState({ duration: audio.duration, position: Math.min(audio.currentTime, audio.duration), playbackRate: 1 });
      } catch {
        /* ignore */
      }
    };
    audio.addEventListener('durationchange', sync);
    audio.addEventListener('seeked', sync);
    audio.addEventListener('play', sync);
    return () => {
      audio.removeEventListener('durationchange', sync);
      audio.removeEventListener('seeked', sync);
      audio.removeEventListener('play', sync);
    };
  }, [audio]);
}
