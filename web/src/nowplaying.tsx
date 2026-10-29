import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type RefObject,
} from 'react';
import { artistLine, clock, sized, type Lyrics } from './api';
import { getLyrics } from './lyrics';
import { Cover, goSearch, Heart, SongMenu } from './components';
import { Icon, PlayGlyph } from './icons';
import { useArtworkFlight } from './motion';
import { usePlayer, usePosition } from './player';
import { Volume } from './shell';
import { reduceMotion } from './preferences';
import { usePageVisible } from './view-activity';

type Pane = 'lyrics' | 'queue' | 'cover';

/** The full-window player: blurred artwork behind, cover and controls, lyrics or queue beside. */
export function NowPlaying({ open, onClose }: { open: boolean; onClose: () => void }) {
  const player = usePlayer();
  const { presentation: current } = player;
  const concealed = player.current !== current;
  const pageVisible = usePageVisible();
  const [retained, setRetained] = useState(open);
  useEffect(() => {
    if (open) {
      setRetained(true);
      return;
    }
    const timer = setTimeout(() => setRetained(false), 520);
    return () => clearTimeout(timer);
  }, [open]);
  const [pane, setPane] = useState<Pane>(() => (window.matchMedia('(min-width: 761px)').matches ? 'lyrics' : 'cover'));
  const [phone, setPhone] = useState(() => matchMedia('(max-width: 760px)').matches);
  const openRef = useRef(open);
  openRef.current = open;
  const suppressArtworkTap = useRef(0);
  const drag = useRef<{
    id: number;
    x: number;
    y: number;
    axis: 'x' | 'y';
    dy: number;
    offset: number;
    active: boolean;
    lastY: number;
    lastTime: number;
    velocity: number;
    target: HTMLElement;
  } | null>(null);
  const sheet = useRef<HTMLDivElement>(null);
  const art = useRef<HTMLDivElement>(null);
  const settle = useRef<Animation | null>(null);
  const clearDrag = useRef<ReturnType<typeof setTimeout> | null>(null);
  const track = current ? `${current.title}|${current.artists.join('|')}|${current.cover}` : '';
  useEffect(() => {
    const mobile = matchMedia('(max-width: 760px)');
    const update = () => {
      setPhone(mobile.matches);
      if (!mobile.matches) setPane((value) => (value === 'cover' ? 'lyrics' : value));
    };
    mobile.addEventListener('change', update);
    return () => mobile.removeEventListener('change', update);
  }, []);
  useLayoutEffect(() => {
    if (!open || !sheet.current) return;
    if (clearDrag.current !== null) clearTimeout(clearDrag.current);
    settle.current?.cancel();
    settle.current = null;
    const pending = drag.current;
    drag.current = null;
    if (pending?.target.hasPointerCapture(pending.id)) pending.target.releasePointerCapture(pending.id);
    delete sheet.current.dataset.dismissed;
    delete sheet.current.dataset.dismissing;
    delete sheet.current.dataset.dragging;
    sheet.current.style.transform = sheet.current.style.opacity = sheet.current.style.borderRadius = '';
  }, [open]);
  useArtworkFlight(open, track, player.playing || player.loading, art);
  useEffect(() => {
    const cancel = () => {
      const pending = drag.current;
      drag.current = null;
      if (pending?.target.hasPointerCapture(pending.id)) pending.target.releasePointerCapture(pending.id);
      settle.current?.cancel();
      settle.current = null;
      if (sheet.current) {
        if (!openRef.current) sheet.current.dataset.dismissed = '';
        delete sheet.current.dataset.dismissing;
        delete sheet.current.dataset.dragging;
        sheet.current.style.transform = sheet.current.style.opacity = sheet.current.style.borderRadius = '';
      }
    };
    const hidden = () => {
      if (document.hidden) cancel();
    };
    window.addEventListener('blur', cancel);
    document.addEventListener('visibilitychange', hidden);
    return () => {
      cancel();
      if (clearDrag.current !== null) clearTimeout(clearDrag.current);
      window.removeEventListener('blur', cancel);
      document.removeEventListener('visibilitychange', hidden);
    };
  }, []);

  useEffect(() => {
    if (!open || !pageVisible) return;
    const previousFocus = document.activeElement;
    const frame = requestAnimationFrame(() =>
      sheet.current?.querySelector<HTMLButtonElement>('[aria-label="收起"]')?.focus({ preventScroll: true }),
    );
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
      if (e.key !== 'Tab' || !sheet.current) return;
      const targets = [...sheet.current.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),[tabindex="0"]')].filter(
        (n) => n.getClientRects().length > 0,
      );
      const first = targets[0],
        last = targets.at(-1);
      if (!first || !last) return;
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if ((!e.shiftKey && document.activeElement === last) || !sheet.current.contains(document.activeElement)) {
        e.preventDefault();
        first.focus();
      }
    };
    document.addEventListener('keydown', onKey);
    return () => {
      cancelAnimationFrame(frame);
      document.removeEventListener('keydown', onKey);
      const target =
        previousFocus instanceof HTMLElement && !sheet.current?.contains(previousFocus)
          ? previousFocus
          : document.querySelector<HTMLElement>('.mini-song');
      if (!document.hidden && !document.documentElement.hasAttribute('data-page-hidden')) target?.focus({ preventScroll: true });
    };
  }, [open, onClose, pageVisible]);

  if (!current || !pageVisible || !(open || retained)) return null;

  // The cover and free space are grab surfaces; controls and scrolling panes
  // keep their own gestures. Intent is decided before a vertical pull begins.
  const onPointerDown = (e: ReactPointerEvent) => {
    const target = e.target as HTMLElement;
    const edge = Boolean(target.closest('.np-edge-back'));
    if (
      !open ||
      e.pointerType === 'mouse' ||
      !e.isPrimary ||
      drag.current ||
      !matchMedia('(max-width: 760px)').matches ||
      (!edge &&
        (!target.closest('.np-top,.np-main') ||
          (target.closest('button,input,a,[role="button"],.np-side,.menu') && !target.closest('.np-art'))))
    )
      return;
    const offset = edge ? 0 : Math.max(0, sheet.current?.getBoundingClientRect().top ?? 0);
    drag.current = {
      id: e.pointerId,
      x: e.clientX,
      y: e.clientY,
      axis: edge ? 'x' : 'y',
      dy: offset,
      offset,
      active: false,
      lastY: edge ? e.clientX : e.clientY,
      lastTime: performance.now(),
      velocity: 0,
      target: e.currentTarget as HTMLElement,
    };
  };
  const onPointerMove = (e: ReactPointerEvent) => {
    if (!drag.current || e.pointerId !== drag.current.id || !sheet.current) return;
    const state = drag.current;
    const horizontal = state.axis === 'x';
    const dx = horizontal ? e.clientY - state.y : e.clientX - state.x;
    const dy = horizontal ? e.clientX - state.x : e.clientY - state.y;
    if (!state.active) {
      if (Math.abs(dx) > 10 && Math.abs(dx) > Math.abs(dy) * 1.15) {
        suppressArtworkTap.current = performance.now() + 400;
        drag.current = null;
        return;
      }
      if (Math.abs(dy) < 8 || Math.abs(dy) < Math.abs(dx) * 1.15) return;
      state.active = true;
      state.offset = horizontal ? 0 : Math.max(0, sheet.current.getBoundingClientRect().top);
      settle.current?.cancel();
      settle.current = null;
      sheet.current.dataset.dragging = '';
      sheet.current.style.opacity = '1';
      state.target.setPointerCapture(e.pointerId);
    }
    e.preventDefault();
    const now = performance.now();
    const point = horizontal ? e.clientX : e.clientY;
    state.velocity = state.velocity * 0.35 + ((point - state.lastY) / Math.max(1, now - state.lastTime)) * 0.65;
    state.lastY = point;
    state.lastTime = now;
    const raw = dy + state.offset;
    state.dy = raw < 0 ? (raw * 0.15) / (1 + Math.abs(raw) / Math.max(1, innerHeight)) : raw;
    sheet.current.style.transform = horizontal ? `translate3d(${Math.max(0, state.dy)}px,0,0)` : `translate3d(0,${state.dy}px,0)`;
    // An opaque moving surface avoids WebKit blending the blurred backdrop
    // into a large grey tile while the sheet is leaving.
    sheet.current.style.opacity = '1';
    sheet.current.style.borderRadius = `${Math.min(28, Math.max(0, state.dy) * 0.18)}px`;
  };
  const onPointerUp = (e: ReactPointerEvent) => {
    if (!drag.current || drag.current.id !== e.pointerId || !sheet.current) return;
    // Touch starts with implicit capture on the image/child. Transferring it
    // to the sheet emits a child lostpointercapture, not a cancelled gesture.
    if (e.type === 'lostpointercapture' && e.target !== drag.current.target) return;
    const { dy, lastTime, active, target, axis } = drag.current;
    const velocity = performance.now() - lastTime > 90 ? 0 : drag.current.velocity;
    const dismiss =
      active &&
      e.type === 'pointerup' &&
      (dy > (axis === 'x' ? Math.min(96, innerWidth * 0.25) : Math.min(120, innerHeight * 0.2)) ||
        (dy > (axis === 'x' ? 48 : 24) && velocity > 0.55));
    drag.current = null;
    if (target.hasPointerCapture(e.pointerId)) target.releasePointerCapture(e.pointerId);
    if (!active) return;
    suppressArtworkTap.current = performance.now() + 400;
    const node = sheet.current;
    const reset = () => {
      node.style.transform = node.style.opacity = node.style.borderRadius = '';
    };
    if (dismiss && axis === 'x' && pane !== 'cover') {
      // Lyrics/queue are a subview; the next edge swipe closes the player.
      setPane('cover');
      delete node.dataset.dragging;
      reset();
    } else if (dismiss) {
      delete node.dataset.dragging;
      node.dataset.dismissing = '';
      const height = Math.max(innerHeight, visualViewport?.height || 0);
      const animation = node.animate(
        [
          { transform: node.style.transform || 'none', opacity: 1 },
          { transform: axis === 'x' ? `translate3d(${innerWidth}px,0,0)` : 'translate3d(0,' + height + 'px,0)', opacity: 1 },
        ],
        { duration: reduceMotion() ? 0 : 300, easing: 'cubic-bezier(.22,.7,.3,1)', fill: 'forwards' },
      );
      settle.current = animation;
      const finish = () => {
        if (settle.current !== animation) return;
        // Hide before cancelling/resetting transforms. Never fade a reset,
        // full-screen blur layer back over the page in WebKit.
        node.dataset.dismissed = '';
        delete node.dataset.dismissing;
        reset();
        animation.cancel();
        settle.current = null;
      };
      animation.onfinish = finish;
      clearDrag.current = setTimeout(finish, 450);
      onClose();
    } else {
      const animation = node.animate(
        [
          { transform: node.style.transform || 'none', opacity: node.style.opacity || '1', borderRadius: node.style.borderRadius || '0px' },
          { transform: 'translate3d(0,-3px,0)', opacity: 1, borderRadius: '0px', offset: 0.72 },
          { transform: 'none', opacity: 1, borderRadius: '0px' },
        ],
        { duration: reduceMotion() ? 0 : 320, easing: 'cubic-bezier(.22,1,.36,1)', fill: 'forwards' },
      );
      settle.current = animation;
      animation.onfinish = () => {
        if (settle.current !== animation) return;
        delete node.dataset.dragging;
        reset();
        animation.cancel();
        settle.current = null;
      };
    }
  };

  return (
    <div
      className={`now-playing${open ? ' is-open' : ''}`}
      aria-hidden={!open}
      aria-modal={open || undefined}
      inert={!open}
      ref={sheet}
      role="dialog"
      aria-label="正在播放"
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerUp}
      onLostPointerCapture={onPointerUp}
    >
      <ArtworkBackdrop src={current.cover} />
      <div className="np-shade" />
      <div className="np-edge-back" aria-hidden="true" />
      <header className="np-top">
        <button className="icon-button" aria-label="收起" onClick={onClose}>
          <Icon name="down" size={24} />
        </button>
        <button className="np-grab-button" type="button" aria-label="返回音乐列表" onClick={onClose}>
          <span className="np-grabber" />
        </button>
        {concealed ? <span className="np-menu-spacer" aria-hidden="true" /> : player.current && <SongMenu item={player.current} />}
      </header>
      <div className={`np-body pane-${pane}`}>
        <section className="np-main">
          <PlayingArtwork
            src={current.cover}
            playing={player.playing || player.loading}
            artRef={art}
            onActivate={
              phone
                ? () => {
                    if (performance.now() >= suppressArtworkTap.current) setPane('lyrics');
                  }
                : undefined
            }
          />
          <div className="np-meta">
            <div className="song-text" key={track}>
              <strong>{current.title}</strong>
              <small className="np-artists">
                {concealed || !current.artists.length
                  ? artistLine(current)
                  : current.artists.map((artist, i) => (
                      <span key={`${artist}-${i}`}>
                        {i > 0 && <span aria-hidden="true">、</span>}
                        <button
                          onClick={() => {
                            onClose();
                            goSearch(artist);
                          }}
                        >
                          {artist}
                        </button>
                      </span>
                    ))}
              </small>
            </div>
            {concealed ? (
              <button className="text-button np-reveal" onClick={player.revealBlind}>
                揭晓
              </button>
            ) : (
              player.current && <Heart item={player.current} size={22} />
            )}
          </div>
          <Scrubber />
          <Transport />
          <div className="np-volume">
            <Volume />
          </div>
        </section>
        <section className="np-side">
          {(!phone || pane !== 'cover') &&
            (concealed ? (
              <div className="lyrics lyrics-state" onClick={() => phone && setPane('cover')}>
                先用耳朵认识它，听满 8 秒再揭晓。
              </div>
            ) : pane === 'queue' ? (
              <QueuePane />
            ) : (
              <LyricsPane visible={open} onBack={phone ? () => setPane('cover') : undefined} />
            ))}
        </section>
      </div>
      <footer className="np-tabs" aria-label="播放页视图">
        <button
          className={pane === 'lyrics' ? 'is-on' : ''}
          aria-label="歌词"
          aria-pressed={pane === 'lyrics'}
          onClick={() => setPane(pane === 'lyrics' && matchMedia('(max-width: 760px)').matches ? 'cover' : 'lyrics')}
        >
          <Icon name="lyrics" size={21} />
          <span>歌词</span>
        </button>
        <button
          className={pane === 'queue' ? 'is-on' : ''}
          aria-label="待播"
          aria-pressed={pane === 'queue'}
          onClick={() => setPane(pane === 'queue' && matchMedia('(max-width: 760px)').matches ? 'cover' : 'queue')}
        >
          <Icon name="queue" size={21} />
          <span>待播</span>
        </button>
      </footer>
    </div>
  );
}

function Transport() {
  const { playing, toggle, next, previous, shuffle, setShuffle, repeat, cycleRepeat, loading } = usePlayer();
  return (
    <div className="np-transport">
      <button
        className={`icon-button mode${shuffle ? ' is-on' : ''}`}
        aria-label="随机播放"
        aria-pressed={shuffle}
        onClick={() => setShuffle(!shuffle)}
      >
        <Icon name="shuffle" size={19} />
      </button>
      <button className="icon-button" aria-label="上一首" onClick={previous}>
        <Icon name="previous" size={30} />
      </button>
      <button
        className={`icon-button big${loading ? ' is-loading' : ''}`}
        aria-label={playing || loading ? '暂停' : '播放'}
        onClick={toggle}
      >
        <PlayGlyph playing={playing || loading} size={38} />
      </button>
      <button className="icon-button" aria-label="下一首" onClick={next}>
        <Icon name="next" size={30} />
      </button>
      <button className="icon-button mode is-on" aria-label={repeat === 'one' ? '单曲循环' : '列表循环'} onClick={cycleRepeat}>
        <Icon name="repeat" size={19} />
        {repeat === 'one' && <span className="mode-badge">1</span>}
      </button>
    </div>
  );
}

function ArtworkBackdrop({ src }: { src: string }) {
  const last = useRef(src);
  const [previous, setPrevious] = useState<string | null>(null);
  useLayoutEffect(() => {
    if (last.current === src) return;
    setPrevious(last.current);
    last.current = src;
    const timer = setTimeout(() => setPrevious(null), 850);
    return () => clearTimeout(timer);
  }, [src]);
  const background = (url: string) => (url ? `url("${sized(url, 400)}")` : undefined);
  return (
    <>
      {previous && <div className="np-backdrop" style={{ backgroundImage: background(previous) }} aria-hidden="true" />}
      <div key={src} className="np-backdrop np-backdrop-current" style={{ backgroundImage: background(src) }} aria-hidden="true" />
    </>
  );
}

function PlayingArtwork({
  src,
  playing,
  artRef,
  onActivate,
}: {
  src: string;
  playing: boolean;
  artRef: RefObject<HTMLDivElement | null>;
  onActivate?: () => void;
}) {
  // Keep the old image mounted until its actual on-screen scale can be sampled.
  // A leaving cover never starts a second pause/play zoom during its fade.
  const [shown, setShown] = useState(src);
  const [previous, setPrevious] = useState<{ src: string; transform: string } | null>(null);
  useLayoutEffect(() => {
    if (shown === src) return;
    const node = artRef.current?.querySelector('.np-art-current');
    setPrevious({ src: shown, transform: node ? getComputedStyle(node).transform : 'none' });
    setShown(src);
  }, [src, shown, artRef]);
  useEffect(() => {
    if (!previous) return;
    const timer = setTimeout(() => setPrevious(null), 650);
    return () => clearTimeout(timer);
  }, [previous]);
  return (
    <div
      className="np-art"
      ref={artRef}
      role={onActivate ? 'button' : undefined}
      tabIndex={onActivate ? 0 : undefined}
      aria-label={onActivate ? '查看歌词' : undefined}
      onClick={onActivate}
      onKeyDown={(event) => {
        if (onActivate && ['Enter', ' '].includes(event.key)) {
          event.preventDefault();
          onActivate();
        }
      }}
      style={{ ['--leaving-art-transform' as string]: previous?.transform || 'none' }}
    >
      {previous && <Cover key={previous.src} src={previous.src} size={600} eager className="np-art-previous np-art-frozen" />}
      <Cover key={shown} src={shown} size={600} eager className={`np-art-current${playing ? ' is-playing' : ''}`} />
    </div>
  );
}

function Scrubber() {
  const { seek, current } = usePlayer();
  const { time, duration } = usePosition();
  const [dragging, setDragging] = useState<number | null>(null);
  const interaction = useRef<'pointer' | 'keyboard' | null>(null);
  const draft = useRef<number | null>(null);
  const commitFrame = useRef(0);
  const input = useRef<HTMLInputElement>(null);
  const total = duration || current?.duration || 0;
  const shown = Math.max(0, Math.min(dragging ?? time, total || 1));
  const commit = (value: number) => {
    draft.current = value;
    setDragging(value);
    interaction.current = null;
    // Native ranges can dispatch their final change after pointerup. Coalesce
    // that last value, seeking once on release rather than once per move.
    if (commitFrame.current) return;
    commitFrame.current = requestAnimationFrame(() => {
      commitFrame.current = 0;
      if (draft.current !== null) seek(draft.current);
      draft.current = null;
      setDragging(null);
    });
  };
  useEffect(() => {
    interaction.current = null;
    draft.current = null;
    setDragging(null);
    cancelAnimationFrame(commitFrame.current);
    commitFrame.current = 0;
    return () => cancelAnimationFrame(commitFrame.current);
  }, [current]);
  useEffect(() => {
    const release = () => {
      if (interaction.current !== null && input.current) commit(Number(input.current.value));
    };
    const hidden = () => {
      if (document.hidden) release();
    };
    window.addEventListener('blur', release);
    document.addEventListener('visibilitychange', hidden);
    return () => {
      window.removeEventListener('blur', release);
      document.removeEventListener('visibilitychange', hidden);
    };
  }, [seek]);
  const seekKeys = ['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End', 'PageUp', 'PageDown'];
  return (
    <div className="scrubber">
      <input
        ref={input}
        type="range"
        min={0}
        max={total || 1}
        step={0.1}
        value={shown}
        disabled={!total}
        aria-label="播放进度"
        style={{ ['--fill' as string]: `${total ? (shown / total) * 100 : 0}%` }}
        onPointerDown={(e) => {
          cancelAnimationFrame(commitFrame.current);
          commitFrame.current = 0;
          interaction.current = 'pointer';
          draft.current = Number(e.currentTarget.value);
          e.currentTarget.setPointerCapture(e.pointerId);
        }}
        onChange={(e) => {
          const value = Number(e.currentTarget.value);
          draft.current = value;
          setDragging(value);
          if (interaction.current === null) commit(value);
        }}
        onPointerUp={(e) => commit(Number(e.currentTarget.value))}
        onPointerCancel={(e) => commit(Number(e.currentTarget.value))}
        onLostPointerCapture={(e) => {
          if (interaction.current === 'pointer') commit(Number(e.currentTarget.value));
        }}
        onBlur={(e) => {
          if (interaction.current !== null || draft.current !== null) commit(Number(e.currentTarget.value));
        }}
        onKeyDown={(e) => {
          if (seekKeys.includes(e.key)) interaction.current = 'keyboard';
        }}
        onKeyUp={(e) => {
          if (seekKeys.includes(e.key)) commit(Number(e.currentTarget.value));
        }}
      />
      <div className="scrubber-times">
        <span>{clock(shown)}</span>
        <QualityBadge />
        <span>-{clock(Math.max(0, total - shown))}</span>
      </div>
    </div>
  );
}

function QualityBadge() {
  const { stream } = usePlayer();
  const quality = stream && (stream.bitrate >= 900000 ? '无损' : stream.bitrate ? `${Math.round(stream.bitrate / 1000)}k` : '');
  return quality ? (
    <span className="np-quality" aria-label={`音质：${quality}`}>
      {quality}
    </span>
  ) : (
    <span aria-hidden="true" />
  );
}

function LyricsPane({ visible, onBack }: { visible: boolean; onBack?: () => void }) {
  const { current, audio, seek, playing } = usePlayer();
  const [lyrics, setLyrics] = useState<Lyrics | null>(null);
  const [active, setActive] = useState(-1);
  const [interlude, setInterlude] = useState(false);
  const box = useRef<HTMLDivElement>(null);
  const userScroll = useRef(0);
  const scrollFrame = useRef(0);
  const scrollVelocity = useRef(0);
  const suppressTap = useRef(0);

  useEffect(() => {
    if (!current) return;
    let cancelled = false;
    setLyrics(null);
    setActive(-1);
    setInterlude(false);
    getLyrics(current)
      .then((l) => !cancelled && setLyrics(l))
      .catch(() => !cancelled && setLyrics({ lines: [], synced: false }));
    return () => {
      cancelled = true;
    };
  }, [current]);

  const lines = useMemo(() => lyrics?.lines ?? [], [lyrics]);

  // Native media events drive line-only lyrics. Frames are only needed for
  // the currently visible, playing word fill; paused text has no idle loop.
  useEffect(() => {
    if (!visible || !lyrics?.synced || lines.length === 0) return;
    let frame = 0,
      last = -1,
      lastTick = 0,
      gap = false;
    let lineTimer: ReturnType<typeof setTimeout> | undefined;
    const update = () => {
      const ms = audio.currentTime * 1000 + 120;
      let index = -1;
      for (let i = 0; i < lines.length && lines[i].t <= ms; i++) index = i;
      if (index !== last) {
        last = index;
        setActive(index);
      }
      const line = index >= 0 ? lines[index] : null,
        next = lines[index + 1];
      const wordEnd = line?.words?.length ? Math.max(...line.words.map((w) => w.t + w.d)) : null;
      const isGap = Boolean(
        next &&
        (index < 0
          ? next.t > 6000 && next.t - ms > 900
          : wordEnd !== null && next.t - wordEnd > 6000 && ms > wordEnd + 650 && next.t - ms > 900),
      );
      if (isGap !== gap) {
        gap = isGap;
        setInterlude(isGap);
      }
      if (line?.words && box.current) {
        const spans = box.current.querySelectorAll<HTMLSpanElement>('[data-index="' + index + '"] .word');
        line.words.forEach((word, i) => {
          const progress = Math.max(0, Math.min(1, (ms - word.t) / Math.max(1, word.d)));
          const fill = Math.round(progress * 10000) / 100 + '%';
          if (spans[i] && spans[i].style.getPropertyValue('--p') !== fill) spans[i].style.setProperty('--p', fill);
        });
      }
      return Boolean(line?.words?.length);
    };
    const stop = () => {
      cancelAnimationFrame(frame);
      frame = 0;
      clearTimeout(lineTimer);
    };
    const tick = (now: number) => {
      frame = 0;
      if (document.hidden || audio.paused || !playing) return;
      if (now - lastTick >= (reduceMotion() ? 120 : 32)) {
        lastTick = now;
        if (!update()) return;
      }
      frame = requestAnimationFrame(tick);
    };
    const pulse = () => {
      stop();
      if (document.hidden) return;
      const wordTimed = update();
      if (audio.paused || !playing) return;
      if (wordTimed) frame = requestAnimationFrame(tick);
      else {
        const next = lines[last + 1];
        if (next)
          lineTimer = setTimeout(pulse, Math.max(16, (next.t - audio.currentTime * 1000 - 120) / Math.max(0.1, audio.playbackRate || 1)));
      }
    };
    const events = ['timeupdate', 'seeked', 'seeking', 'play', 'pause', 'ratechange'];
    events.forEach((event) => audio.addEventListener(event, pulse));
    document.addEventListener('visibilitychange', pulse);
    setInterlude(false);
    pulse();
    return () => {
      stop();
      events.forEach((event) => audio.removeEventListener(event, pulse));
      document.removeEventListener('visibilitychange', pulse);
    };
  }, [visible, lyrics, lines, audio, playing]);

  useEffect(() => {
    if (!visible || active < 0 || !box.current || Date.now() - userScroll.current < 2500) return;
    // Scroll only the lyrics box; scrollIntoView would also shift the page.
    const el = box.current.querySelector<HTMLElement>(`[data-index="${active}"]`);
    if (!el) return;
    const node = box.current;
    const target = Math.max(0, Math.min(node.scrollHeight - node.clientHeight, el.offsetTop - node.clientHeight * 0.4));
    cancelAnimationFrame(scrollFrame.current);
    if (reduceMotion()) {
      node.scrollTop = target;
      scrollVelocity.current = 0;
      return;
    }
    let last = performance.now();
    const tick = (now: number) => {
      const dt = Math.min(0.032, (now - last) / 1000);
      last = now;
      scrollVelocity.current += ((target - node.scrollTop) * 180 - scrollVelocity.current * 28) * dt;
      const movement = scrollVelocity.current * dt;
      // scrollTop can be integer-quantised; snap before tiny steps stall.
      if (Math.abs(target - node.scrollTop) < 1.5 || (Math.abs(movement) < 0.25 && Math.abs(target - node.scrollTop) < 8)) {
        node.scrollTop = target;
        scrollVelocity.current = 0;
        scrollFrame.current = 0;
        return;
      }
      node.scrollTop += movement;
      scrollFrame.current = requestAnimationFrame(tick);
    };
    scrollFrame.current = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(scrollFrame.current);
  }, [active, visible]);

  const manualScroll = () => {
    suppressTap.current = performance.now() + 400;
    userScroll.current = Date.now();
    cancelAnimationFrame(scrollFrame.current);
    scrollVelocity.current = 0;
  };
  const tapBack = () => {
    if (performance.now() >= suppressTap.current) onBack?.();
  };
  const gapIndicator = (
    <span className={`lyric-interlude${playing ? '' : ' is-paused'}`} role="status" aria-label="间奏">
      <i />
      <i />
      <i />
    </span>
  );

  if (!lyrics)
    return (
      <div className="lyrics lyrics-state" onClick={tapBack}>
        正在找歌词…
      </div>
    );
  if (lines.length === 0)
    return (
      <div className="lyrics lyrics-state" onClick={tapBack}>
        这首歌暂时没有歌词
      </div>
    );
  return (
    <div
      className={`lyrics${lyrics.synced ? '' : ' is-plain'}`}
      ref={box}
      onWheel={manualScroll}
      onTouchMove={manualScroll}
      onClick={tapBack}
    >
      <div className="lyrics-pad" />
      {interlude && active < 0 && gapIndicator}
      {lines.map((line, i) => (
        <p
          key={i}
          data-index={i}
          data-depth={lyrics.synced && active >= 0 ? Math.min(4, Math.abs(i - active)) : undefined}
          className={`lyric${i === active ? ' is-active' : ''}${i < active ? ' is-past' : ''}`}
          onClick={() => !onBack && lyrics.synced && seek(line.t / 1000)}
        >
          {i === active && line.words ? (
            line.words.map((w, j) => (
              <span key={j} className="word">
                {w.w}
              </span>
            ))
          ) : (
            <span>{line.text}</span>
          )}
          {line.tr && <small>{line.tr}</small>}
          {interlude && i === active && gapIndicator}
        </p>
      ))}
      {lyrics.credits && lyrics.credits.length > 0 && <div className="lyrics-credits">{lyrics.credits.join('\n')}</div>}
      <div className="lyrics-pad" />
    </div>
  );
}

function QueuePane() {
  const { queue, index, playbackOrder, shuffle, jump, removeAt, moveUpcoming, playing, toggle } = usePlayer();
  const box = useRef<HTMLDivElement>(null);
  const list = useRef<HTMLOListElement>(null);
  const currentRow = useRef<HTMLLIElement>(null);
  const [dragged, setDragged] = useState<{ from: number; over: number; offset: number } | null>(null);
  const [announcement, setAnnouncement] = useState('');
  const gesture = useRef<{
    id: number;
    from: number;
    over: number;
    startY: number;
    y: number;
    scroll: number;
    active: boolean;
    target: HTMLButtonElement;
    frame: number;
    last: number;
  } | null>(null);
  const position = playbackOrder.indexOf(index);
  const ordered = playbackOrder.map((i) => ({ item: queue[i], i }));
  const recent = ordered.slice(Math.max(0, position - 3), Math.max(0, position));
  const current = queue[index];
  const upcoming = ordered.slice(position + 1);
  const cancelDrag = () => {
    const g = gesture.current;
    gesture.current = null;
    if (g) {
      cancelAnimationFrame(g.frame);
      if (g.target.hasPointerCapture(g.id)) g.target.releasePointerCapture(g.id);
    }
    setDragged(null);
  };
  useEffect(() => {
    cancelDrag();
  }, [queue, index, shuffle, playbackOrder]);
  useEffect(() => {
    const cancel = () => cancelDrag();
    const hidden = () => {
      if (document.hidden) cancel();
    };
    const escape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') cancel();
    };
    window.addEventListener('blur', cancel);
    document.addEventListener('visibilitychange', hidden);
    document.addEventListener('keydown', escape, true);
    return () => {
      cancel();
      window.removeEventListener('blur', cancel);
      document.removeEventListener('visibilitychange', hidden);
      document.removeEventListener('keydown', escape, true);
    };
  }, []);
  useLayoutEffect(() => {
    if (box.current && currentRow.current)
      box.current.scrollTop += currentRow.current.getBoundingClientRect().top - box.current.getBoundingClientRect().top - 36;
  }, [index, shuffle, current]);
  const updateDrag = () => {
    const g = gesture.current;
    if (!g || !g.active || !box.current || !list.current) return;
    const height = 58;
    g.over = Math.max(0, Math.min(upcoming.length - 1, Math.floor((g.y - list.current.getBoundingClientRect().top) / height)));
    setDragged({ from: g.from, over: g.over, offset: g.y - g.startY + box.current.scrollTop - g.scroll });
  };
  const startDrag = (e: ReactPointerEvent<HTMLButtonElement>, from: number) => {
    if (e.button !== 0 || !e.isPrimary || gesture.current) return;
    e.preventDefault();
    e.currentTarget.focus({ preventScroll: true });
    e.currentTarget.setPointerCapture(e.pointerId);
    gesture.current = {
      id: e.pointerId,
      from,
      over: from,
      startY: e.clientY,
      y: e.clientY,
      scroll: box.current?.scrollTop ?? 0,
      active: false,
      target: e.currentTarget,
      frame: 0,
      last: performance.now(),
    };
  };
  const moveDrag = (e: ReactPointerEvent<HTMLButtonElement>) => {
    const g = gesture.current;
    if (!g || g.id !== e.pointerId) return;
    g.y = e.clientY;
    if (!g.active && Math.abs(g.y - g.startY) < 4) return;
    if (!g.active) {
      g.active = true;
      const scroll = (now: number) => {
        const drag = gesture.current,
          container = box.current;
        if (!drag || !container) return;
        const r = container.getBoundingClientRect(),
          dt = Math.min(32, now - drag.last);
        drag.last = now;
        const speed =
          drag.y < r.top + 42
            ? -Math.min(1, (r.top + 42 - drag.y) / 42)
            : drag.y > r.bottom - 42
              ? Math.min(1, (drag.y - r.bottom + 42) / 42)
              : 0;
        if (speed) {
          container.scrollTop += speed * dt * 0.65;
          updateDrag();
        }
        drag.frame = requestAnimationFrame(scroll);
      };
      g.frame = requestAnimationFrame(scroll);
    }
    e.preventDefault();
    updateDrag();
  };
  const finishDrag = (e: ReactPointerEvent<HTMLButtonElement>) => {
    const g = gesture.current;
    if (!g || g.id !== e.pointerId) return;
    const from = upcoming[g.from],
      to = upcoming[g.over],
      changed = g.active && g.from !== g.over;
    cancelDrag();
    if (changed && from && to) {
      moveUpcoming(from.i, to.i);
      setAnnouncement(`已将${from.item.title}移到待播第${g.over + 1}首`);
    }
  };
  const keyboardMove = (e: ReactKeyboardEvent<HTMLButtonElement>, at: number) => {
    if (!['ArrowUp', 'ArrowDown', 'Home', 'End'].includes(e.key)) return;
    e.preventDefault();
    e.stopPropagation();
    const to = e.key === 'Home' ? 0 : e.key === 'End' ? upcoming.length - 1 : at + (e.key === 'ArrowUp' ? -1 : 1),
      from = upcoming[at],
      target = upcoming[to];
    if (!from || !target || to === at) return;
    moveUpcoming(from.i, target.i);
    setAnnouncement(`已将${from.item.title}移到待播第${to + 1}首`);
    requestAnimationFrame(() =>
      list.current
        ?.querySelector<HTMLButtonElement>(`[data-queue-index="${shuffle ? from.i : target.i}"] .queue-drag-handle`)
        ?.focus({ preventScroll: true }),
    );
  };
  const row = ({ item, i }: { item: (typeof queue)[number]; i: number }, at = -1) => {
    const offset =
      dragged && at >= 0
        ? at === dragged.from
          ? dragged.offset
          : dragged.over > dragged.from && at > dragged.from && at <= dragged.over
            ? -58
            : dragged.over < dragged.from && at >= dragged.over && at < dragged.from
              ? 58
              : 0
        : 0;
    return (
      <li
        key={`${i}-${item.copies[0]?.id}`}
        data-queue-index={i}
        className={`${i === index ? 'is-current' : ''}${dragged && at === dragged.from ? ' is-dragging' : ''}`}
        ref={i === index ? currentRow : undefined}
        style={at >= 0 ? { transform: `translateY(${offset}px)` } : undefined}
      >
        {at >= 0 && (
          <button
            className="queue-drag-handle"
            aria-label={`调整待播顺序：${item.title}`}
            aria-keyshortcuts="ArrowUp ArrowDown Home End"
            onPointerDown={(e) => startDrag(e, at)}
            onPointerMove={moveDrag}
            onPointerUp={finishDrag}
            onPointerCancel={cancelDrag}
            onLostPointerCapture={cancelDrag}
            onKeyDown={(e) => keyboardMove(e, at)}
          >
            <Icon name="grip" size={17} />
          </button>
        )}
        <button
          className="queue-song"
          aria-current={i === index ? 'true' : undefined}
          onClick={() => (i === index ? toggle() : jump(i))}
          aria-label={i === index ? `${playing ? '暂停' : '播放'} ${item.title}` : `播放 ${item.title}`}
        >
          <span className="queue-cover">
            <Cover src={item.cover} size={48} />
            {i === index && (
              <span className="queue-current-mark">
                <Icon name={playing ? 'volume' : 'play'} size={16} />
              </span>
            )}
          </span>
          <span className="song-text">
            <strong>{item.title}</strong>
            <small>{artistLine(item)}</small>
          </span>
        </button>
        {i !== index && (
          <button className="icon-button danger" aria-label={`从待播移除 ${item.title}`} onClick={() => removeAt(i)}>
            <Icon name="close" size={16} />
          </button>
        )}
      </li>
    );
  };
  return (
    <div className={`queue${dragged ? ' is-reordering' : ''}`} ref={box}>
      {recent.length > 0 && (
        <>
          <h3 className="queue-section-title">前面的歌曲</h3>
          <ol className="queue-history">{recent.map((entry) => row(entry))}</ol>
        </>
      )}
      {current && (
        <>
          <h3 className="queue-section-title">正在播放</h3>
          <ol>{row({ item: current, i: index })}</ol>
        </>
      )}
      <h3 className="queue-section-title">
        接下来播放 <small>{upcoming.length} 首</small>
      </h3>
      {upcoming.length === 0 && <p className="queue-empty">待播是空的</p>}
      <ol className="queue-upcoming" ref={list}>
        {upcoming.map((entry, at) => row(entry, at))}
      </ol>
      <span className="sr-only" role="status" aria-live="polite">
        {announcement}
      </span>
    </div>
  );
}
