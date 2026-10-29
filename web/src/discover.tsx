import { useCallback, useEffect, useRef, useState, type CSSProperties, type PointerEvent } from 'react';
import { api, artistLine, clock, recall, remember, sized, type Card, type Discover as DiscoverData, type Item, type Pick } from './api';
import { Cover, Empty, Heart, PageHeader, PageSkeleton, sameItem } from './components';
import { Icon } from './icons';
import { usePlayer } from './player';
import { Link } from './router';

// Discover is for meeting new songs. Like the record crate on Home, every
// block borrows one physical thing — a blind listen, a hand of cards, a radar
// scope, a mixtape, a newspaper, an arrivals board, a wall of sleeves, a radio
// dial — so no two blocks look alike.

export function Discover() {
  const [data, setData] = useState<DiscoverData | null>(() => recall<DiscoverData>('discover:multi')?.value ?? null);
  const [busy, setBusy] = useState(false);
  const load = useCallback(() => {
    setBusy(true);
    api
      .discover()
      .then((d) => {
        remember('discover:multi', d);
        setData(d);
      })
      .catch(() => {})
      .finally(() => setBusy(false));
  }, []);
  useEffect(() => {
    // A batch from the last few minutes is still new to the listener; keep it.
    const last = recall<DiscoverData>('discover:multi');
    if (!last || Date.now() - last.at > 10 * 60 * 1000) load();
  }, [load]);
  if (!data) return <PageSkeleton title="发现" />;
  const more = data.more ?? [];
  const blind = [data.hero, ...more.slice(0, 5)].filter(Boolean) as Pick[];
  const empty = blind.length === 0 && data.radar.length === 0 && data.fresh.length === 0;
  return (
    <div className="page discover">
      <PageHeader title="发现" />
      {empty && <Empty title="暂时没有新歌" hint="稍后再来看看。" />}
      <div className="discover-opening">
        <div className="discover-refresh">
          <span>发现有些不喜欢？</span>
          <button className="text-button" onClick={load} disabled={busy} aria-label="换一批">
            <Icon name="sync" size={15} className={busy ? 'spin' : ''} />
            <strong>{busy ? '正在换…' : '换一批'}</strong>
          </button>
        </div>
        {blind.length > 0 && <BlindListen songs={blind} />}
      </div>
      {data.picks.length > 0 && <Hand cards={data.picks} />}
      {data.radar.length > 2 && <Radar items={data.radar} />}
      {data.similar && <Mixtape seed={data.similar.seed} songs={data.similar.songs} />}
      {data.fresh.length > 3 && <Gazette items={data.fresh} />}
      {data.world.length > 0 && <Arrivals items={data.world} />}
      {more.length > 7 && <Wall items={more.slice(5)} />}
      {data.random.length > 0 && <Tuner items={data.random} />}
    </div>
  );
}

function Head({ title, note }: { title: string; note?: string }) {
  return (
    <header className="section-head">
      <h2>{title}</h2>
      {note && <span className="section-note">{note}</span>}
    </header>
  );
}

function usePhone() {
  const query = '(max-width: 760px)';
  const [phone, setPhone] = useState(() => matchMedia(query).matches);
  useEffect(() => {
    const list = matchMedia(query);
    const change = () => setPhone(list.matches);
    list.addEventListener('change', change);
    return () => list.removeEventListener('change', change);
  }, []);
  return phone;
}

/** One song with its cover veiled: listen first, see who it is after. */
function BlindListen({ songs }: { songs: Pick[] }) {
  const { play, playBlind, current, playing, loading, toggle, blindSession, revealBlind } = usePlayer();
  const [index, setIndex] = useState(0);
  const [peek, setPeek] = useState<Item | null>(null);
  const at = index % songs.length;
  const song = songs[at];
  const live = current !== null && sameItem(current, song);
  const session = blindSession && sameItem(blindSession.item, song) ? blindSession : null;
  const open = Boolean((peek && sameItem(peek, song)) || session?.revealed);
  const progress = Math.max(0, Math.min(1, (session?.seconds ?? 0) / 8));
  const next = () => {
    setIndex((i) => i + 1);
    setPeek(null);
  };
  return (
    <section className={`blind${open ? ' is-open' : ''}`} style={{ '--blind-blur': `${22 - progress * 10}px` } as CSSProperties}>
      <span className="blind-bg" style={{ backgroundImage: song.cover ? `url("${sized(song.cover, 200)}")` : undefined }} />
      <div className="blind-lead">
        <span className="blind-tag">
          <span className="blind-label">盲听</span>
          <span className="blind-count">
            {at + 1} / {songs.length}
          </span>
        </span>
      </div>
      <button
        className="blind-art"
        onClick={() => (live && (session || open) ? toggle() : open ? play(songs, at) : playBlind(songs, at))}
        aria-label={open ? `${live && playing ? '暂停' : '播放'} ${song.title}` : live && session && playing ? '暂停盲听' : '盲听这一首'}
      >
        <Cover src={song.cover} size={320} />
        <span className="blind-veil" aria-hidden="true">
          {!open && (
            <svg className="blind-progress" viewBox="0 0 84 84">
              <circle className="blind-progress-track" cx="42" cy="42" r="38" />
              <circle
                className="blind-progress-value"
                cx="42"
                cy="42"
                r="38"
                pathLength="1"
                strokeDasharray="1"
                strokeDashoffset={1 - progress}
              />
            </svg>
          )}
          <Icon className="blind-play" name={live && playing ? 'pause' : 'play'} size={30} />
        </span>
      </button>
      <div className="blind-info">
        {open ? (
          <>
            <strong>{song.title}</strong>
            <small>{artistLine(song)}</small>
          </>
        ) : (
          <>
            <strong className="blind-mask">？？？？</strong>
            {live && session && (
              <small aria-live="polite">
                {loading ? '准备中' : playing ? `${Math.max(1, Math.ceil(8 - session.seconds))} 秒` : '已暂停'}
              </small>
            )}
            <button
              className="text-button blind-detail"
              onClick={() => {
                setPeek(song);
                if (session) revealBlind();
              }}
            >
              揭晓
            </button>
          </>
        )}
        <div className="blind-actions">
          <button className="pill light" onClick={next}>
            换一首
            <Icon name="chevron" size={14} />
          </button>
          <span className="blind-heart">{open && <Heart item={song} size={20} />}</span>
        </div>
      </div>
    </section>
  );
}

/** Curated playlists held like a hand of cards, with their table of contents
 * beside them; pointing at either lifts the card. */
function Hand({ cards }: { cards: Card[] }) {
  const n = cards.length;
  const [lifted, setLifted] = useState<number | null>(null);
  return (
    <section className="section">
      <Head title="精选歌单" />
      <div
        className={`hand-wrap${lifted !== null ? ' is-pointing' : ''}`}
        onMouseLeave={() => setLifted(null)}
        onBlur={(e) => {
          if (!e.currentTarget.contains(e.relatedTarget)) setLifted(null);
        }}
        style={
          {
            '--mobile-step': `${Math.min(24, 76 / Math.max(1, n - 1))}px`,
            '--mobile-turn': `${Math.min(6, 20 / Math.max(1, n - 1))}deg`,
          } as CSSProperties
        }
      >
        <ol className="hand-index">
          {cards.map((c, i) => (
            <li key={c.id} onMouseEnter={() => setLifted(i)} className={lifted === i ? 'is-on' : ''}>
              <Link to={`/remote/${c.source}/${c.id}`} onIntent={() => setLifted(i)}>
                <span className="hand-no">{String(i + 1).padStart(2, '0')}</span>
                <span className="hand-name">
                  <strong>{c.name}</strong>
                  <small>{c.count ? `${c.count} 首` : ''}</small>
                </span>
              </Link>
            </li>
          ))}
        </ol>
        <div className="hand">
          {cards.map((c, i) => (
            <Link
              key={c.id}
              to={`/remote/${c.source}/${c.id}`}
              className={`hand-card${lifted === i ? ' is-lifted' : ''}`}
              style={{ '--o': i - (n - 1) / 2 } as CSSProperties}
              onIntent={() => setLifted(i)}
            >
              <Cover src={c.cover} size={320} />
              <span className="hand-text">
                <strong>{c.name}</strong>
                <small>{c.count ? `${c.count} 首` : ''}</small>
              </span>
            </Link>
          ))}
        </div>
      </div>
    </section>
  );
}

// Fixed spots on the scope (distance 0–1, angle in degrees), so blips stay put.
const spots: [number, number][] = [
  [0.36, 28],
  [0.7, 64],
  [0.52, 118],
  [0.83, 156],
  [0.3, 205],
  [0.66, 236],
  [0.86, 292],
  [0.48, 322],
];

/** New songs picked for the listener as blips on a sweeping radar scope. */
function Radar({ items }: { items: Pick[] }) {
  const { play, current } = usePlayer();
  const [hovered, setHovered] = useState<number | null>(null);
  const [focused, setFocused] = useState<number | null>(null);
  const active = hovered ?? focused;
  const blips = items.slice(0, spots.length);
  return (
    <section className="section">
      <Head title="新歌雷达" />
      <div className="radar">
        <div className="radar-visual">
          <div className="radar-scope" aria-label="新歌雷达，编号对应曲目列表">
            <span className="radar-sweep" aria-hidden="true" />
            {blips.map((item, i) => {
              const [r, a] = spots[i];
              const rad = (a * Math.PI) / 180;
              // The sweep starts at 12 o'clock and turns once every 4 s; light up as it passes.
              const style = {
                left: `${50 + r * 44 * Math.cos(rad)}%`,
                top: `${50 + r * 44 * Math.sin(rad)}%`,
                animationDelay: `${(((a + 90) % 360) / 360) * 4 - 4}s`,
              };
              return (
                <button
                  key={i}
                  className={`radar-blip${active === i ? ' is-pointed' : ''}${current && sameItem(current, item) ? ' is-current' : ''}`}
                  style={style}
                  onPointerEnter={() => setHovered(i)}
                  onPointerLeave={() => setHovered(null)}
                  onFocus={() => setFocused(i)}
                  onBlur={() => setFocused(null)}
                  onClick={() => play(items, i)}
                  aria-label={`${String(i + 1).padStart(2, '0')} ${item.title}，${artistLine(item)}`}
                >
                  <Cover src={item.cover} size={64} />
                  <span className="radar-blip-no" aria-hidden="true">
                    {String(i + 1).padStart(2, '0')}
                  </span>
                </button>
              );
            })}
          </div>
        </div>
        <ol className="radar-list">
          {items.slice(0, 12).map((item, i) => (
            <li
              key={`${i}-${item.copies[0]?.id}`}
              className={`${current && sameItem(current, item) ? 'is-current' : ''}${active === i ? ' is-pointed' : ''}`}
              role="button"
              aria-label={`${String(i + 1).padStart(2, '0')} ${item.title}，${artistLine(item)}`}
              onPointerEnter={() => setHovered(i)}
              onPointerLeave={() => setHovered(null)}
              onFocus={() => setFocused(i)}
              onBlur={() => setFocused(null)}
              onClick={() => play(items, i)}
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  e.stopPropagation();
                  play(items, i);
                }
              }}
            >
              <span className="radar-no">{String(i + 1).padStart(2, '0')}</span>
              <span className="song-text">
                <strong>{item.title}</strong>
                <small>{artistLine(item)}</small>
              </span>
              <span className="radar-time">{clock(item.duration)}</span>
            </li>
          ))}
        </ol>
      </div>
    </section>
  );
}

/** Songs like one the listener loves, as a home-made mixtape with its J-card. */
function Mixtape({ seed, songs }: { seed: Item; songs: Pick[] }) {
  const { play, current, playing } = usePlayer();
  const live = playing && current !== null && songs.some((s) => sameItem(current, s));
  return (
    <section className="section">
      <Head title="因为你喜欢" />
      <div className="mixtape">
        <button className={`cassette${live ? ' is-live' : ''}`} onClick={() => play(songs, 0)} aria-label="播放这盘磁带">
          <span className="cassette-label">
            <small>MIXTAPE · SIDE A</small>
            <strong>{seed.title}</strong>
            <em>{artistLine(seed)}</em>
          </span>
          <span className="cassette-window" aria-hidden="true">
            <i className="reel" />
            <span className="tape" />
            <i className="reel" />
          </span>
          <span className="cassette-foot">{songs.length} SONGS · TYPE II</span>
        </button>
        <ol className="jcard">
          {songs.map((s, i) => (
            <li
              key={`${i}-${s.copies[0]?.id}`}
              className={current && sameItem(current, s) ? 'is-current' : ''}
              onClick={() => play(songs, i)}
              tabIndex={0}
              onKeyDown={(e) => e.key === 'Enter' && play(songs, i)}
            >
              <span className="jcard-no">A{i + 1}</span>
              <span className="jcard-title">{s.title}</span>
              <span className="jcard-dots" aria-hidden="true" />
              <span className="jcard-artist">{artistLine(s)}</span>
              <span className="jcard-time">{clock(s.duration)}</span>
            </li>
          ))}
        </ol>
      </div>
    </section>
  );
}

/** Today's Chinese releases laid out as a newspaper front page. */
function Gazette({ items }: { items: Pick[] }) {
  const { play, current } = usePlayer();
  const [lead, ...rest] = items;
  const now = new Date();
  const issue = Math.floor((now.getTime() - new Date(now.getFullYear(), 0, 0).getTime()) / 86400000);
  return (
    <section className="gazette">
      <header className="gazette-mast">
        <span>第 {issue} 期</span>
        <h2>听歌日报</h2>
        <span>
          {now.getMonth() + 1} 月 {now.getDate()} 日 · 星期{'日一二三四五六'[now.getDay()]}
        </span>
      </header>
      <div className="gazette-body">
        <button className="gazette-lead" onClick={() => play(items, 0)}>
          <Cover src={lead.cover} size={300} />
          <span className="gazette-kicker">今天值得一听</span>
          <strong>{lead.title}</strong>
          <small>
            {artistLine(lead)}
            {lead.album && lead.album !== lead.title ? ` ·《${lead.album}》` : ''}
          </small>
        </button>
        <ol className="gazette-cols">
          {rest.map((s, i) => (
            <li
              key={`${i}-${s.copies[0]?.id}`}
              className={current && sameItem(current, s) ? 'is-current' : ''}
              onClick={() => play(items, i + 1)}
              tabIndex={0}
              onKeyDown={(e) => e.key === 'Enter' && play(items, i + 1)}
            >
              <strong>{s.title}</strong>
              <small>{artistLine(s)}</small>
            </li>
          ))}
        </ol>
      </div>
    </section>
  );
}

const regionCode = (reason: string) => (reason.startsWith('日本') ? 'JP' : reason.startsWith('韩国') ? 'KR' : 'US');

/** Songs from abroad on an airport arrivals board. */
function Arrivals({ items }: { items: Pick[] }) {
  const { play, current, playing } = usePlayer();
  const now = new Date();
  return (
    <section className="arrivals">
      <header className="arrivals-head">
        <div>
          <h2>世界新歌</h2>
        </div>
        <span className="arrivals-clock">
          {String(now.getHours()).padStart(2, '0')}:{String(now.getMinutes()).padStart(2, '0')}
        </span>
      </header>
      <div className="arrivals-boards">
        {[items.slice(0, Math.ceil(items.length / 2)), items.slice(Math.ceil(items.length / 2))].map((half, h) => (
          <div key={h} className="arrivals-board">
            <div className="arrivals-row is-head" aria-hidden="true">
              <span>来自</span>
              <span />
              <span>歌名</span>
              <span>歌手</span>
              <span>时长</span>
              <span>状态</span>
            </div>
            {half.map((s, k) => {
              const i = h * Math.ceil(items.length / 2) + k;
              const on = current !== null && sameItem(current, s);
              return (
                <button
                  key={`${i}-${s.copies[0]?.id}`}
                  className={`arrivals-row${on ? ' is-current' : ''}`}
                  onClick={() => play(items, i)}
                  aria-label={`${s.title}，${artistLine(s)}${on && playing ? '，正在播放' : ''}`}
                  aria-pressed={on}
                >
                  <span className="flap">{regionCode(s.reason)}</span>
                  <Cover src={s.cover} size={40} />
                  <span className="arrivals-title">{s.title}</span>
                  <span className="arrivals-artist">{artistLine(s)}</span>
                  <span className="flap">{clock(s.duration)}</span>
                  <span className="arrivals-status">{on && playing ? '正在播放' : '已到达'}</span>
                </button>
              );
            })}
          </div>
        ))}
      </div>
    </section>
  );
}

/** A slowly drifting wall of new sleeves; pointing stops it. */
function Wall({ items }: { items: Pick[] }) {
  const { play } = usePlayer();
  const rows = [items.filter((_, i) => i % 2 === 0), items.filter((_, i) => i % 2 === 1)];
  return (
    <section className="section wall">
      <Head title="新发现一览" />
      {rows.map((row, r) => (
        <div key={r} className={`wall-row${r ? ' is-reverse' : ''}`}>
          <div className="wall-track">
            {[...row, ...row].map((s, i) => (
              <button
                key={i}
                className="wall-item"
                onClick={() => play(items, items.indexOf(s))}
                tabIndex={i < row.length ? 0 : -1}
                aria-hidden={i >= row.length}
              >
                <Cover src={s.cover} size={150} />
                <span className="wall-caption">
                  <strong>{s.title}</strong>
                  <small>{artistLine(s)}</small>
                </span>
              </button>
            ))}
          </div>
        </div>
      ))}
    </section>
  );
}

/** The private radio as a tuning dial: each song is a station. */
function Tuner({ items }: { items: Pick[] }) {
  const { playRadio, current } = usePlayer();
  const phone = usePhone();
  const stations = items.slice(0, phone ? 6 : 12);
  const [selected, setSelected] = useState(0);
  const [preview, setPreview] = useState<number | null>(null);
  const [tuning, setTuning] = useState(false);
  const drag = useRef<{ id: number; x: number; y: number; horizontal: boolean } | null>(null);
  const suppressClickUntil = useRef(0);
  const at = Math.max(0, Math.min(preview ?? selected, stations.length - 1));
  const song = stations[at];
  const freq = (i: number) => (87.5 + ((i + 0.5) * 20.5) / stations.length).toFixed(1);
  const left = (i: number) => {
    const ratio = (i + 0.5) / stations.length;
    return `calc(32px + ${ratio * 100}% - ${ratio * 64}px)`;
  };
  const start = (i: number) => {
    setSelected(i);
    setPreview(null);
    playRadio([...stations.slice(i), ...stations.slice(0, i)], () => api.radio().then((r) => r.songs));
  };
  const stationAt = (event: PointerEvent<HTMLDivElement>) => {
    const box = event.currentTarget.getBoundingClientRect();
    return Math.max(
      0,
      Math.min(stations.length - 1, Math.floor(((event.clientX - box.left - 32) / Math.max(1, box.width - 64)) * stations.length)),
    );
  };
  const cancel = () => {
    drag.current = null;
    setPreview(null);
    setTuning(false);
  };
  const pointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (!event.isPrimary || event.button !== 0 || drag.current) return;
    drag.current = { id: event.pointerId, x: event.clientX, y: event.clientY, horizontal: false };
  };
  const pointerMove = (event: PointerEvent<HTMLDivElement>) => {
    const active = drag.current;
    if (!active || active.id !== event.pointerId) return;
    const dx = Math.abs(event.clientX - active.x);
    const dy = Math.abs(event.clientY - active.y);
    if (!active.horizontal) {
      // Leave vertical motion to the page; capture only after horizontal intent.
      if (dy > 7 && dy > dx) {
        cancel();
        return;
      }
      if (dx < 7 || dx <= dy) return;
      active.horizontal = true;
      event.currentTarget.setPointerCapture(event.pointerId);
      setTuning(true);
    }
    event.preventDefault();
    setPreview(stationAt(event));
  };
  const pointerUp = (event: PointerEvent<HTMLDivElement>) => {
    const active = drag.current;
    if (!active || active.id !== event.pointerId) return;
    drag.current = null;
    setTuning(false);
    if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
    if (active.horizontal) {
      suppressClickUntil.current = Date.now() + 500;
      start(stationAt(event));
    } else if (!(event.target as Element).closest('.tuner-station')) {
      start(stationAt(event));
    }
  };
  return (
    <section className="section">
      <Head title="随便听听" />
      <div className="tuner">
        <button className="tuner-now" onClick={() => start(at)}>
          <Cover src={song.cover} size={180} />
          <span className="tuner-text">
            <span className="tuner-freq">FM {freq(at)}</span>
            <strong>{song.title}</strong>
            <small>{artistLine(song)}</small>
          </span>
        </button>
        <div className="tuner-controls">
          <div
            className={`tuner-dial${tuning ? ' is-tuning' : ''}`}
            role="group"
            aria-label="收音机刻度盘，可左右拖动或选择电台"
            onPointerDown={pointerDown}
            onPointerMove={pointerMove}
            onPointerUp={pointerUp}
            onPointerCancel={(event) => {
              if (drag.current?.id === event.pointerId) cancel();
            }}
            onLostPointerCapture={(event) => {
              if (event.target === event.currentTarget && drag.current?.id === event.pointerId) cancel();
            }}
            onClickCapture={(event) => {
              if (event.detail > 0 && Date.now() < suppressClickUntil.current) {
                event.preventDefault();
                event.stopPropagation();
                suppressClickUntil.current = 0;
              }
            }}
          >
            <div className="tuner-scale" aria-hidden="true">
              {[90, 94, 98, 102, 106].map((f) => (
                <span key={f} style={{ left: `${((f - 87.5) / 20.5) * 100}%` }}>
                  {f}
                </span>
              ))}
            </div>
            {stations.map((s, i) => (
              <button
                key={`${i}-${s.copies[0]?.id}`}
                className={`tuner-station${i === at ? ' is-on' : ''}${current && sameItem(current, s) ? ' is-current' : ''}`}
                style={{ left: left(i) }}
                onPointerEnter={(event) => {
                  if (event.pointerType === 'mouse' && !drag.current) setPreview(i);
                }}
                onPointerLeave={() => {
                  if (!drag.current) setPreview(null);
                }}
                onFocus={() => setPreview(i)}
                onBlur={() => {
                  if (!drag.current) setPreview(null);
                }}
                onClick={() => start(i)}
                aria-label={`FM ${freq(i)} ${s.title}${current && sameItem(current, s) ? '，当前歌曲' : ''}`}
                aria-pressed={Boolean(current && sameItem(current, s))}
              >
                <Cover src={s.cover} size={48} />
              </button>
            ))}
            <span className="tuner-needle" style={{ left: left(at) }} aria-hidden="true" />
          </div>
        </div>
      </div>
    </section>
  );
}
