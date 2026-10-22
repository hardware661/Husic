import { useEffect, useRef, useState, type CSSProperties, type PointerEvent, type ReactNode } from 'react';
import { api, artistLine, recall, remember, sized, type Card, type Home as HomeData, type Item, type Playlist } from './api';
import { CardTile, Cover, Empty, Mosaic, PageHeader, PageSkeleton, Section, SongMenu, sameItem } from './components';
import { Icon } from './icons';
import { useLibrary } from './library';
import { usePlayer } from './player';
import { Link, navigate } from './router';

// Home: every block has its own form — a crate of records to start from, a
// feature list, a cover quilt, playlists in Cover Flow, a listening diary.

export function Home() {
  const { version } = useLibrary();
  const [data, setData] = useState<HomeData | null>(() => recall<HomeData>('home:multi')?.value ?? null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let alive = true;
    api
      .home()
      .then((d) => {
        remember('home:multi', d);
        if (alive) setData(d);
      })
      .catch(() => alive && setFailed(true));
    return () => {
      alive = false;
    };
  }, [version]);
  if (failed && !data) return <Empty title="没能加载" hint="稍后再试试" />;
  if (!data) return <PageSkeleton title="首页" />;
  const { daily, picks, recent, playlists, liked } = data;
  const recommended = data.recommendations ?? daily;
  return (
    <div className="page">
      <PageHeader title="首页" />
      <Crate data={data} />
      {recommended.length > 1 && (
        <Section title="今天为你挑的">
          <FeatureList items={recommended.slice(0, 16)} eyebrow="今日推荐第一首" />
        </Section>
      )}
      {liked.length >= 5 && (
        <Section title="你喜欢的歌" to="/liked" action={<ShuffleLikes />}>
          <Quilt items={liked} />
        </Section>
      )}
      {picks.length > 0 && (
        <Section title="推荐歌单">
          <CoverFlow cards={picks.slice(0, 9)} />
        </Section>
      )}
      {playlists.length > 0 && (
        <Section title="我的歌单" to="/library">
          <div className="shelf">{playlists.slice(0, 6).map((p) => <ShelfItem key={p.id} playlist={p} />)}</div>
        </Section>
      )}
      {recent.length > 0 && (
        <Section title="听歌日记" to="/recent">
          <Diary items={recent} />
        </Section>
      )}
    </div>
  );
}

type Sleeve = { key: string; title: string; note: string; art: ReactNode; disc?: string; live?: boolean; onPlay: () => void };

/** The top row is a crate of records: each sleeve is something to play right
 * now, and its disc slides out when you point at it (and spins while it plays). */
function Crate({ data }: { data: HomeData }) {
  const { daily, fm, liked, recent, playlists } = data;
  const { play, setShuffle, playRadio, radio, current, playing } = usePlayer();
  const now = new Date();
  const sleeves: Sleeve[] = [];
  if (daily.length > 0) {
    sleeves.push({
      key: 'daily',
      title: '每日推荐',
      note: `${daily.length} 首`,
      art: (
        <span className="sleeve-daily">
          <Mosaic covers={daily.map((s) => s.cover).filter(Boolean) as string[]} size={180} />
          <b>{now.getDate()}</b>
        </span>
      ),
      disc: daily[0].cover,
      onPlay: () => (setShuffle(false), play(daily, 0)),
    });
  }
  if (fm.length > 0) {
    const shown = radio && current ? current : fm[0];
    sleeves.push({
      key: 'fm',
      title: '私人 FM',
      note: radio && current ? `正在放 · ${shown.title}` : shown.title,
      art: <Cover src={shown.cover} size={180} />,
      disc: shown.cover,
      live: radio && playing,
      onPlay: () => playRadio(fm, () => api.fm().then((r) => r.songs)),
    });
  }
  if (liked.length > 0) {
    sleeves.push({
      key: 'liked',
      title: '我喜欢',
      note: '',
      art: <Mosaic covers={liked.map((s) => s.cover).filter(Boolean) as string[]} size={180} />,
      disc: liked[0].cover,
      onPlay: async () => {
        const { likes } = await api.library();
        setShuffle(true);
        play(likes, Math.floor(Math.random() * likes.length));
      },
    });
  }
  if (recent[0]) {
    sleeves.push({
      key: 'recent',
      title: '接着听',
      note: recent[0].title,
      art: <Cover src={recent[0].cover} size={180} />,
      disc: recent[0].cover,
      onPlay: () => play(recent, 0),
    });
  }
  for (const p of playlists.slice(0, 6 - sleeves.length)) {
    sleeves.push({
      key: `p${p.id}`,
      title: p.name,
      note: `${p.count} 首`,
      art: <Mosaic covers={p.covers} cover={p.cover} size={180} />,
      disc: p.cover || p.covers[0],
      onPlay: () => navigate(`/playlist/${p.id}`),
    });
  }
  return (
    <section className="crate">
      <div className="crate-row">
        {sleeves.map((v) => (
          <button key={v.key} className={`sleeve${v.live ? ' is-live' : ''}`} onClick={v.onPlay}>
            <span className="sleeve-stage">
              <span className="sleeve-disc" aria-hidden="true">
                {v.disc && <img src={sized(v.disc, 120)} alt="" loading="lazy" decoding="async" />}
              </span>
              <span className="sleeve-art">{v.art}</span>
            </span>
            <strong>{v.title}</strong>
            <small>{v.note}</small>
          </button>
        ))}
      </div>
    </section>
  );
}

const pad = (n: number) => String(n).padStart(2, '0');

function dayLabel(at: Date, now: Date) {
  const start = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const days = Math.round((start(now) - start(at)) / 86400000);
  if (days === 0) return '今天';
  if (days === 1) return '昨天';
  return `${at.getMonth() + 1} 月 ${at.getDate()} 日 · 星期${'日一二三四五六'[at.getDay()]}`;
}

/** Recently played songs kept like a diary: grouped by day, each with its time. */
function Diary({ items }: { items: Item[] }) {
  const { play, current } = usePlayer();
  const now = new Date();
  const days: { label: string; entries: { item: Item; index: number; time: string }[] }[] = [];
  items.slice(0, 12).forEach((item, index) => {
    const at = item.playedAt ? new Date(item.playedAt) : null;
    const label = at ? dayLabel(at, now) : '更早';
    if (days.at(-1)?.label !== label) days.push({ label, entries: [] });
    days.at(-1)!.entries.push({ item, index, time: at ? `${pad(at.getHours())}:${pad(at.getMinutes())}` : '' });
  });
  return (
    <div className="diary">
      {days.map((d) => (
        <section key={d.label} className="diary-day">
          <h3>{d.label}</h3>
          <ol>
            {d.entries.map(({ item, index, time }) => (
              <li
                key={index}
                className={`${current && sameItem(current, item) ? 'is-current' : ''}${index >= 6 ? ' is-extra' : ''}`}
                onClick={() => play(items, index)}
                tabIndex={0}
                onKeyDown={(e) => e.key === 'Enter' && play(items, index)}
              >
                <time>{time}</time>
                <Cover src={item.cover} size={40} />
                <span className="song-text">
                  <strong>{item.title}</strong>
                  <small>{artistLine(item)}</small>
                </span>
              </li>
            ))}
          </ol>
        </section>
      ))}
    </div>
  );
}

/** A large lead song with the rest listed beside it. */
export function FeatureList({ items, eyebrow, numbered }: { items: Item[]; eyebrow: string; numbered?: boolean }) {
  const { play, current } = usePlayer();
  const [lead, ...rest] = items;
  return (
    <div className="feature-list">
      <button className="feature-lead" onClick={() => play(items, 0)}>
        <Cover src={lead.cover} size={360} />
        <span className="eyebrow">{eyebrow}</span>
        <strong>{lead.title}</strong>
        <small>{artistLine(lead)}</small>
      </button>
      <ol className={`feature-rows${numbered ? ' is-numbered' : ''}`}>
        {rest.map((item, i) => (
          <li
            key={`${i}-${item.copies[0]?.id}`}
            className={current && sameItem(current, item) ? 'is-current' : ''}
            onClick={() => play(items, i + 1)}
            tabIndex={0}
            onKeyDown={(e) => e.key === 'Enter' && play(items, i + 1)}
          >
            {numbered && <span className="feature-index">{String(i + 2).padStart(2, '0')}</span>}
            <Cover src={item.cover} size={44} />
            <span className="song-text">
              <strong>{item.title}</strong>
              <small>{artistLine(item)}</small>
            </span>
            <SongMenu item={item} />
          </li>
        ))}
      </ol>
    </div>
  );
}

/** Covers set like a quilt: one large, the rest small. */
export function Quilt({ items, onPlay }: { items: Item[]; onPlay?: (index: number) => void }) {
  const { play } = usePlayer();
  // One large tile plus six (or four) small ones; never leave holes.
  const count = items.length >= 7 ? 7 : items.length >= 5 ? 5 : items.length;
  return (
    <div className={`quilt quilt-${count >= 5 ? count : 'few'}`}>
      {items.slice(0, count).map((item, i) => (
        <button key={`${i}-${item.copies[0]?.id}`} className="quilt-tile" onClick={() => (onPlay ? onPlay(i) : play(items, i))}>
          <Cover src={item.cover} size={i === 0 ? 420 : 220} />
          <span className="quilt-caption">
            <strong>{item.title}</strong>
            <small>{artistLine(item)}</small>
          </span>
        </button>
      ))}
    </div>
  );
}

function ShuffleLikes() {
  const { play, setShuffle } = usePlayer();
  return (
    <button
      className="text-button"
      onClick={async () => {
        const { likes } = await api.library();
        setShuffle(true);
        play(likes, Math.floor(Math.random() * likes.length));
      }}
    >
      <Icon name="shuffle" size={13} />
      随机播放全部
    </button>
  );
}

/** Recommended playlists in Cover Flow, the way iTunes once showed albums:
 * the middle one faces you, the rest lean away; click one to bring it forward. */
export function CoverFlow({ cards }: { cards: Card[] }) {
  const [active, setActive] = useState(Math.max(0, Math.floor((cards.length - 1) / 2)));
  const swipe = useRef<{ id: number; x: number; y: number; horizontal: boolean } | null>(null);
  const suppressClickUntil = useRef(0);
  const selected = Math.min(active, cards.length - 1);
  const card = cards[selected];
  const move = (by: number) => setActive((a) => Math.max(0, Math.min(cards.length - 1, Math.min(a, cards.length - 1) + by)));
  const finishSwipe = (e: PointerEvent<HTMLDivElement>, cancelled = false) => {
    const gesture = swipe.current;
    if (!gesture || gesture.id !== e.pointerId) return;
    swipe.current = null;
    if (gesture.horizontal) {
      suppressClickUntil.current = Date.now() + 700;
      const dx = e.clientX - gesture.x;
      if (!cancelled && Math.abs(dx) >= 35) move(dx < 0 ? 1 : -1);
    }
    if (e.currentTarget.hasPointerCapture(e.pointerId)) e.currentTarget.releasePointerCapture(e.pointerId);
  };
  if (!card) return null;
  return (
    <div className="flow">
      <div
        className="flow-stage"
        tabIndex={0}
        aria-label="推荐歌单，左右滑动或方向键切换"
        onKeyDown={(e) => {
          if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
            e.preventDefault();
            move(e.key === 'ArrowLeft' ? -1 : 1);
          }
        }}
        onPointerDown={(e) => {
          if (e.pointerType === 'mouse' || !e.isPrimary || swipe.current) return;
          suppressClickUntil.current = 0;
          swipe.current = { id: e.pointerId, x: e.clientX, y: e.clientY, horizontal: false };
        }}
        onPointerMove={(e) => {
          const gesture = swipe.current;
          if (!gesture || gesture.id !== e.pointerId) return;
          const dx = Math.abs(e.clientX - gesture.x);
          const dy = Math.abs(e.clientY - gesture.y);
          if (!gesture.horizontal) {
            if (dy > 10 && dy >= dx) { swipe.current = null; return; }
            if (dx < 12 || dx <= dy * 1.2) return;
            gesture.horizontal = true;
            e.currentTarget.setPointerCapture(e.pointerId);
          }
          e.preventDefault();
        }}
        onPointerUp={(e) => finishSwipe(e)}
        onPointerCancel={(e) => finishSwipe(e, true)}
        onLostPointerCapture={(e) => { if (e.target === e.currentTarget) finishSwipe(e, true); }}
        onClickCapture={(e) => {
          if (e.detail !== 0 && Date.now() < suppressClickUntil.current) {
            e.preventDefault();
            e.stopPropagation();
          }
        }}
      >
        {cards.map((c, i) => {
          const d = i - selected;
          const style = { '--d': d, '--side': Math.sign(d), '--far': Math.abs(d), zIndex: 50 - Math.abs(d) } as CSSProperties;
          return d === 0 ? (
            <Link key={c.id} to={`/remote/${c.source}/${c.id}`} className="flow-item is-active" style={style} label={`打开 ${c.name}`}>
              <Cover src={c.cover} size={320} />
            </Link>
          ) : (
            <button key={c.id} className="flow-item" style={style} onClick={() => setActive(i)} aria-label={c.name}>
              <Cover src={c.cover} size={240} />
            </button>
          );
        })}
      </div>
      <div className="flow-caption">
        <button className="icon-button" onClick={() => move(-1)} disabled={selected === 0} aria-label="上一个">
          <Icon name="chevron" size={16} className="flip" />
        </button>
        <Link to={`/remote/${card.source}/${card.id}`} className="flow-title">
          <strong>{card.name}</strong>
          <small>{card.count ? `${card.count} 首` : ''}</small>
        </Link>
        <button className="icon-button" onClick={() => move(1)} disabled={selected === cards.length - 1} aria-label="下一个">
          <Icon name="chevron" size={16} />
        </button>
      </div>
    </div>
  );
}

function ShelfItem({ playlist }: { playlist: Playlist }) {
  return (
    <Link to={`/playlist/${playlist.id}`} className="shelf-item">
      <Mosaic covers={playlist.covers} cover={playlist.cover} size={240} />
      <strong>{playlist.name}</strong>
      <small>{playlist.count} 首</small>
    </Link>
  );
}

export { CardTile };
