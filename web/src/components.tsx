import { openSearch } from './search-data';
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { artistLine, clock, sized, inspectionID, type Card, type Item, type Playlist } from './api';
import { Icon } from './icons';
import { useLibrary } from './library';
import { usePlayer } from './player';
import { Link, navigate, goBack, usePath } from './router';
import { ActionDialog } from './dialogs';
import { downloadAudio, downloadKey, exportLocalAudio, localAudioID, useLocalAudio } from './local-audio';
import { toast } from './toast';
import { batchIsRunning, startAudioDownloadBatch, useAudioDownloadBatch } from './audio-download-batch';

const loadedCovers = new Set<string>();
function rememberCover(url: string) {
  loadedCovers.add(url);
  if (loadedCovers.size > 512) loadedCovers.delete(loadedCovers.values().next().value!);
}

export function Cover({
  src,
  size,
  className = '',
  alt = '',
  eager = false,
}: {
  src?: string;
  size: number;
  className?: string;
  alt?: string;
  eager?: boolean;
}) {
  const url = sized(src || '', size * 2);
  const latest = useRef(src);
  latest.current = src;
  const [image, setImage] = useState({ src, loaded: false, failed: false });
  if (!src || (image.src === src && image.failed)) {
    return (
      <span className={`cover cover-empty ${className}`} aria-hidden={!alt}>
        <Icon name="note" size={Math.max(14, Math.round(size / 3))} />
      </span>
    );
  }
  return (
    <span className={`cover cover-art${loadedCovers.has(url) || (image.src === src && image.loaded) ? ' is-loaded' : ''} ${className}`}>
      <img
        key={src}
        className="cover-image"
        src={url}
        alt={alt}
        loading={eager ? 'eager' : 'lazy'}
        decoding="async"
        onLoad={() => {
          rememberCover(url);
          if (latest.current === src) setImage({ src, loaded: true, failed: false });
        }}
        onError={() => latest.current === src && setImage({ src, loaded: false, failed: true })}
      />
    </span>
  );
}

/** Up to four song covers in a 2×2 grid, the way Apple Music draws playlists. */
export function Mosaic({ covers, cover, size }: { covers: string[]; cover?: string; size: number }) {
  if (cover) return <Cover src={cover} size={size} className="mosaic" />;
  if (covers.length < 4) return <Cover src={covers[0]} size={size} className="mosaic" />;
  return (
    <span className="mosaic mosaic-grid">
      {covers.slice(0, 4).map((c, i) => (
        <Cover key={i} src={c} size={size / 2} />
      ))}
    </span>
  );
}

export function Heart({ item, size = 17 }: { item: Item; size?: number }) {
  const { isLiked, toggleLike } = useLibrary();
  const liked = isLiked(item);
  const [pulse, setPulse] = useState(false);
  return (
    <button
      className={`icon-button heart${liked ? ' is-on' : ''}${pulse ? ' heart-pulse' : ''}`}
      onAnimationEnd={() => setPulse(false)}
      aria-label={liked ? '取消喜欢' : '喜欢'}
      aria-pressed={liked}
      onClick={(e) => {
        e.stopPropagation();
        setPulse(!liked);
        toggleLike(item);
      }}
    >
      <Icon name={liked ? 'heartFill' : 'heart'} size={size} />
    </button>
  );
}

export function DownloadButton({ item }: { item: Item }) {
  const [saving, setSaving] = useState(false);
  const { records } = useLocalAudio('download');
  const saved = records.some((record) => record.downloadKey === downloadKey(item));
  const localID = localAudioID(item);
  const label = saving ? (localID ? '正在另存为' : '正在下载') : localID ? '另存为' : saved ? '已下载' : '下载';
  return (
    <button
      type="button"
      className="icon-button"
      aria-label={label}
      title={label}
      disabled={saving || (saved && !localID)}
      aria-busy={saving}
      onClick={async (event) => {
        event.stopPropagation();
        if (saving || (saved && !localID)) return;
        setSaving(true);
        try {
          if (localID) await exportLocalAudio(localID);
          else {
            await downloadAudio(item);
            toast('已下载到本地');
          }
        } catch (error) {
          toast(error instanceof Error ? error.message : '下载失败，请重试');
        } finally {
          setSaving(false);
        }
      }}
    >
      <Icon name={saving ? 'sync' : saved && !localID ? 'check' : 'download'} size={18} className={saving ? 'spin' : undefined} />
    </button>
  );
}

export function BulkDownloadButton({ items, name }: { items: Item[]; name: string }) {
  const batch = useAudioDownloadBatch();
  const busy = batchIsRunning(batch);
  return (
    <button
      type="button"
      className="pill bulk-download-button"
      aria-label="全部下载"
      aria-busy={busy}
      disabled={!items.length || busy}
      title={busy ? '已有歌单正在批量下载' : '下载当前歌单的全部歌曲'}
      onClick={() => {
        if (startAudioDownloadBatch(items, name)) toast('已开始逐首下载，可在下载管理查看');
      }}
    >
      <Icon name={busy ? 'sync' : 'download'} size={15} className={busy ? 'spin' : undefined} />
      {busy ? '下载中' : '全部下载'}
    </button>
  );
}

/** The "…" menu every song carries. */
export function SongMenu({ item, onRemove, removeLabel = '从歌单移除' }: { item: Item; onRemove?: () => void; removeLabel?: string }) {
  const [open, setOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [saving, setSaving] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const { playNext, addToQueue } = usePlayer();
  const { playlists, addTo, createPlaylist } = useLibrary();
  const localID = localAudioID(item);
  useEffect(() => {
    if (!open) return;
    const close = (e: Event) => !ref.current?.contains(e.target as Node) && !panel.current?.contains(e.target as Node) && setOpen(false);
    const escape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        e.stopImmediatePropagation();
        setOpen(false);
        trigger.current?.focus();
      }
    };
    const scroll = (e: Event) => {
      const target = e.target;
      if (panel.current?.contains(target as Node)) return;
      if (target === document || (target instanceof Element && target.contains(trigger.current))) setOpen(false);
    };
    const resize = () => setOpen(false);
    document.addEventListener('pointerdown', close);
    document.addEventListener('keydown', escape, true);
    document.addEventListener('scroll', scroll, true);
    window.addEventListener('resize', resize);
    return () => {
      document.removeEventListener('pointerdown', close);
      document.removeEventListener('keydown', escape, true);
      document.removeEventListener('scroll', scroll, true);
      window.removeEventListener('resize', resize);
    };
  }, [open, localID]);
  useLayoutEffect(() => {
    if (!open || !panel.current || !trigger.current) return;
    const menu = panel.current;
    const anchor = trigger.current.getBoundingClientRect();
    const boundary = window.visualViewport
      ? Math.min(innerHeight, window.visualViewport.height + window.visualViewport.offsetTop)
      : innerHeight;
    const bottomBar =
      !ref.current?.closest('.now-playing') && document.querySelector<HTMLElement>('.mini-player, .tab-bar')?.getBoundingClientRect();
    const bottom = bottomBar ? Math.min(boundary - 8, bottomBar.top - 8) : boundary - 8;
    menu.style.maxHeight = `${Math.max(100, boundary - 24)}px`;
    const height = menu.getBoundingClientRect().height;
    const upward = anchor.bottom + height + 6 > bottom;
    menu.dataset.upward = String(upward);
    menu.style.top = `${Math.max(8, Math.min(upward ? anchor.top - height - 6 : anchor.bottom + 6, boundary - height - 8))}px`;
    menu.style.left = `${Math.max(8, Math.min(anchor.right - menu.offsetWidth, innerWidth - menu.offsetWidth - 8))}px`;
  }, [open, localID]);
  const run = (fn: () => void) => () => {
    setOpen(false);
    fn();
  };
  const own = playlists.filter((p) => !p.origin || p.origin.startsWith('netease:'));
  const save = async () => {
    if (saving) return;
    setSaving(true);
    try {
      if (localID) await exportLocalAudio(localID);
      else await downloadAudio(item);
      if (!localID) toast('已下载');
      setOpen(false);
    } catch (error) {
      toast(error instanceof Error ? error.message : localID ? '没能另存为' : '下载失败');
    } finally {
      setSaving(false);
    }
  };
  return (
    <div className="menu-anchor" ref={ref} onClick={(e) => e.stopPropagation()}>
      <button
        ref={trigger}
        className="icon-button more"
        aria-label="更多"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        onKeyDown={(event) => {
          if (event.key === 'ArrowDown') {
            event.preventDefault();
            setOpen(true);
            requestAnimationFrame(() => panel.current?.querySelector<HTMLElement>('[role="menuitem"]')?.focus());
          }
        }}
      >
        <Icon name="more" size={18} />
      </button>
      {open &&
        createPortal(
          <div
            className="menu"
            role="menu"
            ref={panel}
            onClick={(event) => event.stopPropagation()}
            onKeyDown={(event) => {
              const items = [...(panel.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not(:disabled)') ?? [])];
              const index = items.indexOf(document.activeElement as HTMLButtonElement);
              if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
                event.preventDefault();
                items[(index + (event.key === 'ArrowDown' ? 1 : items.length - 1)) % items.length]?.focus();
              }
              if (event.key === 'Home' || event.key === 'End') {
                event.preventDefault();
                items[event.key === 'Home' ? 0 : items.length - 1]?.focus();
              }
              if (event.key === 'Tab') {
                event.preventDefault();
                setOpen(false);
                trigger.current?.focus();
              }
            }}
          >
            <button role="menuitem" onClick={run(() => playNext(item))}>
              <Icon name="queue" size={16} />
              接下来播放
            </button>
            <button role="menuitem" onClick={run(() => addToQueue(item))}>
              <Icon name="plus" size={16} />
              加入待播
            </button>
            <button
              role="menuitem"
              disabled={saving}
              aria-busy={saving}
              onClick={() => {
                void save();
              }}
            >
              <Icon name="download" size={16} />
              {saving ? (localID ? '正在另存为…' : '正在下载…') : localID ? '另存为…' : '下载'}
            </button>
            {!localID && (
              <>
                <div className="menu-label">加入歌单</div>
                <div className="menu-scroll">
                  {own.map((p) => (
                    <button role="menuitem" key={p.id} onClick={run(() => addTo(p.id, [item]))}>
                      <Icon name="note" size={16} />
                      {p.name}
                    </button>
                  ))}
                  <button role="menuitem" onClick={run(() => setCreating(true))}>
                    <Icon name="plus" size={16} />
                    新建歌单…
                  </button>
                </div>
              </>
            )}
            {onRemove && (
              <button role="menuitem" className="danger" onClick={run(onRemove)}>
                <Icon name="close" size={16} />
                {removeLabel}
              </button>
            )}
          </div>,
          ref.current?.closest('dialog') || document.body,
        )}
      {creating && !localID && (
        <ActionDialog
          title="新建歌单"
          inputLabel="歌单名称"
          confirmLabel="创建"
          onClose={() => setCreating(false)}
          onConfirm={async (name) => {
            await addTo(await createPlaylist(name), [item]);
          }}
        />
      )}
    </div>
  );
}

type ListProps = {
  items: Item[];
  numbered?: boolean;
  showAlbum?: boolean;
  onRemove?: (item: Item) => void;
  removeLabel?: string;
  empty?: ReactNode;
};

/** A full-width song list: tap a row to play the list from there. */
export function SongList({ items, numbered, showAlbum = true, onRemove, removeLabel, empty }: ListProps) {
  const { play, current, playing } = usePlayer();
  if (items.length === 0) return <>{empty}</>;
  return (
    <ol className={`song-list${items.length > 120 ? ' is-long' : ''}`}>
      {items.map((item, i) => {
        const active = current !== null && sameItem(current, item);
        return (
          <li
            key={`${i}-${item.songId ?? item.copies[0]?.id}`}
            className={`song-row${numbered ? ' is-numbered' : ''}${!showAlbum ? ' without-album' : ''}${active ? ' is-current' : ''}`}
            onClick={() => play(items, i)}
            onKeyDown={(e) => e.key === 'Enter' && e.target === e.currentTarget && play(items, i)}
            tabIndex={0}
          >
            {numbered && <span className="song-number">{active && playing ? <Bars /> : i + 1}</span>}
            <span className="song-lead">
              <Cover src={item.cover} size={40} />
            </span>
            <span className="song-text">
              <strong>{item.title}</strong>
              <small>{inspectionID(item) ? '多分P' : artistLine(item)}</small>
            </span>
            {showAlbum && (
              <span className="song-album" title={item.album}>
                {item.album || '—'}
              </span>
            )}
            <span className="song-actions">
              <Heart item={item} />
              <span className="song-time">{clock(item.duration)}</span>
              <SongMenu item={item} onRemove={onRemove && (() => onRemove(item))} removeLabel={removeLabel} />
            </span>
          </li>
        );
      })}
    </ol>
  );
}

/** Three compact columns of songs, as on the home page. */
export function SongGrid({ items, rows = 3 }: { items: Item[]; rows?: number }) {
  const { play, current } = usePlayer();
  return (
    <div className="song-grid" style={{ ['--rows' as string]: rows }}>
      {items.map((item, i) => (
        <div
          key={`${i}-${item.copies[0]?.id}`}
          className={`grid-song${current && sameItem(current, item) ? ' is-current' : ''}`}
          onClick={() => play(items, i)}
          onKeyDown={(e) => e.key === 'Enter' && e.target === e.currentTarget && play(items, i)}
          tabIndex={0}
        >
          <Cover src={item.cover} size={44} />
          <span className="song-text">
            <strong>{item.title}</strong>
            <small>{artistLine(item)}</small>
          </span>
          <SongMenu item={item} />
        </div>
      ))}
    </div>
  );
}

export function PlaylistTile({ playlist }: { playlist: Playlist }) {
  return (
    <Link to={`/playlist/${playlist.id}`} className="tile">
      <Mosaic covers={playlist.covers} cover={playlist.cover} size={200} />
      <strong>{playlist.name}</strong>
      <small>{playlist.count} 首</small>
    </Link>
  );
}

export function CardTile({ card }: { card: Card }) {
  return (
    <Link to={`/remote/${card.source}/${card.id}`} className="tile">
      <Cover src={card.cover} size={200} className="mosaic" />
      <strong>{card.name}</strong>
      <small>{card.note || (card.count ? `${card.count} 首` : '')}</small>
    </Link>
  );
}

export function Section({ title, to, action, children }: { title: string; to?: string; action?: ReactNode; children: ReactNode }) {
  return (
    <section className="section">
      <header className="section-head">
        <h2>
          {to ? (
            <Link to={to} className="section-link">
              {title}
              <Icon name="chevron" size={14} />
            </Link>
          ) : (
            title
          )}
        </h2>
        {action}
      </header>
      {children}
    </section>
  );
}

export function PageHeader({ title, children, search = true }: { title: string; children?: ReactNode; search?: boolean }) {
  const path = usePath().split('?')[0];
  const compact = ['/', '/discover', '/charts', '/library', '/recent', '/search'].includes(path);
  const secondary = !['/', '/discover', '/charts', '/library'].includes(path);
  return (
    <header className={`page-header${compact ? ' is-compact' : ''}`}>
      <h1 className={compact ? 'sr-only' : undefined}>{title}</h1>
      {secondary ? (
        <BackButton />
      ) : (
        <Link to="/settings" className="mobile-settings-logo" label="设置">
          <img src="/brand/husic-icon.svg?v=0.2.0" alt="" width={28} height={28} />
        </Link>
      )}
      <div className="page-actions">
        {children}
        {search && (
          <button type="button" className="icon-button header-search" aria-label="搜索" onClick={openSearch}>
            <Icon name="search" size={21} />
          </button>
        )}
      </div>
    </header>
  );
}

export function BackButton() {
  return (
    <button className="mobile-back icon-button" aria-label="返回上一页" onClick={goBack}>
      <Icon name="chevron" size={21} />
    </button>
  );
}

/** Grey shapes in the page's layout while its first answer is on the way. */
export function PageSkeleton({ title }: { title: string }) {
  return (
    <div className="page" aria-busy="true" aria-label="正在加载">
      <PageHeader title={title} />
      <div className="skeleton">
        <i className="sk-hero" />
        <div className="sk-row">
          {Array.from({ length: 6 }, (_, i) => (
            <i key={i} />
          ))}
        </div>
        <div className="sk-list">
          {Array.from({ length: 8 }, (_, i) => (
            <i key={i} />
          ))}
        </div>
      </div>
    </div>
  );
}

export function PlayButtons({ items }: { items: Item[] }) {
  const { play, setShuffle } = usePlayer();
  return (
    <div className="play-buttons">
      <button className="pill primary" disabled={items.length === 0} onClick={() => (setShuffle(false), play(items, 0))}>
        <Icon name="play" size={15} />
        播放
      </button>
      <button
        className="pill"
        disabled={items.length === 0}
        onClick={() => {
          setShuffle(true);
          play(items, Math.floor(Math.random() * items.length));
        }}
      >
        <Icon name="shuffle" size={16} />
        随机播放
      </button>
    </div>
  );
}

export function Bars() {
  return (
    <span className="bars" aria-label="正在播放">
      <i />
      <i />
      <i />
    </span>
  );
}

export function Empty({ title, hint, children }: { title: string; hint?: string; children?: ReactNode }) {
  return (
    <div className="empty">
      <strong>{title}</strong>
      {hint && <p>{hint}</p>}
      {children}
    </div>
  );
}

export function sameItem(a: Item, b: Item) {
  if (a.songId && b.songId) return a.songId === b.songId;
  return a.copies.some((x) => b.copies.some((y) => x.source === y.source && x.id === y.id));
}

export function goSearch(q: string) {
  navigate(`/search?q=${encodeURIComponent(q)}`);
}
