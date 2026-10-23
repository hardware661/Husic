import { useEffect, useState, type ReactNode } from 'react';
import { artistLine } from './api';
import { prepareChartEntry } from './chart-cache';
import { Cover, DownloadButton, Heart } from './components';
import { Icon, PlayGlyph } from './icons';
import { usePageMotion } from './motion';
import { useLibrary } from './library';
import { usePlayer, usePosition } from './player';
import { Link, usePath, useScrollMemory } from './router';

import { openSearch } from './search-ui';
import { reduceMotion } from './preferences';

const sections = [
  { to: '/', label: '首页', icon: 'home' },
  { to: '/discover', label: '发现', icon: 'sparkle' },
  { to: '/charts', label: '排行榜', icon: 'chart' },
  { to: '/library', label: '音乐库', icon: 'library' },
];

// Search is the sidebar box on computers and a header button on phones.
const tabs = sections;

function isActive(path: string, to: string) {
  const [p] = path.split('?');
  return to === '/' ? p === '/' : p.startsWith(to);
}

export function Sidebar({ collapsed, onToggle }: { collapsed: boolean; onToggle: () => void }) {
  const path = usePath();
  const { playlists } = useLibrary();
  return (
    <aside className="sidebar" id="desktop-sidebar" onDragStart={event => event.preventDefault()} onContextMenu={event => event.preventDefault()}>
      <button className="brand" type="button" onClick={onToggle} aria-expanded={!collapsed} aria-controls="sidebar-navigation" aria-label={collapsed ? '展开侧边栏' : '收起侧边栏'} title={collapsed ? '展开侧边栏' : '收起侧边栏'}>
        <img className="brand-mark" src="/brand/music-icon.svg?v=20261022-hardware-a" width={28} height={28} alt="" />
        <strong>Husic</strong>
        <Icon name="chevron" size={12} className="brand-disclosure" />
      </button>
      <nav className="side-nav" id="sidebar-navigation" aria-label="主要导航">
        <button className={`side-search-button${path.startsWith('/search')?' is-selected':''}`} type="button" aria-label="搜索音乐" data-label="搜索" onClick={openSearch}><Icon name="search" size={18}/><span className="side-nav-label">搜索</span></button>
        {sections.map((s) => (
          <Link key={s.to} to={s.to} label={s.label} className={isActive(path, s.to) ? 'is-selected' : ''} onIntent={s.to === '/charts' ? prepareChartEntry : undefined}>
            <Icon name={s.icon} size={18} />
            <span className="side-nav-label">{s.label}</span>
          </Link>
        ))}
      </nav>
      <div className="side-label">资料库</div>
      <nav className="side-nav side-library" aria-label="资料库">
        <Link to="/liked" label="我喜欢" className={isActive(path, '/liked') ? 'is-selected' : ''}>
          <Icon name="heart" size={18} />
          <span className="side-nav-label">我喜欢</span>
        </Link>
        <Link to="/recent" label="最近播放" className={isActive(path, '/recent') ? 'is-selected' : ''}>
          <Icon name="clock" size={18} />
          <span className="side-nav-label">最近播放</span>
        </Link>
      </nav>
      <div className="side-label">歌单</div>
      <nav className="side-nav side-playlists" hidden={collapsed} aria-label="歌单">
        {playlists.map((p) => (
          <Link key={p.id} to={`/playlist/${p.id}`} className={path === `/playlist/${p.id}` ? 'is-selected' : ''}>
            <Cover src={p.cover || p.covers[0]} size={20} />
            <span>{p.name}</span>
          </Link>
        ))}
      </nav>
      <nav className="side-nav side-settings" aria-label="应用设置">
        <Link to="/settings" label="设置" className={isActive(path, '/settings') ? 'is-selected' : ''}>
          <Icon name="settings" size={18} /><span className="side-nav-label">设置</span>
        </Link>
      </nav>
    </aside>
  );
}

export function TabBar() {
  const path = usePath();
  return (
    <nav className="tab-bar" aria-label="主要导航" onDragStart={event => event.preventDefault()} onContextMenu={event => event.preventDefault()}>
      {tabs.map((s) => (
        <Link key={s.to} to={s.to} className={isActive(path, s.to) ? 'is-selected' : ''} onIntent={s.to === '/charts' ? prepareChartEntry : undefined}>
          <Icon name={s.icon} size={23} />
          <span>{s.label}</span>
        </Link>
      ))}
    </nav>
  );
}

/** The floating glass bar: tap the song to open the full player. */
export function MiniPlayer({ onOpen, covered = false }: { onOpen: () => void; covered?: boolean }) {
  const { current: actual, presentation: current, playing, loading, toggle, next, previous } = usePlayer();
  if (!current) return null;
  const active=playing||loading;
  return (
    <div className={`mini-player${loading ? ' is-loading' : ''}`} role="region" aria-label="正在播放">
      <div className="mini-transport">
        <button className="icon-button" aria-label="上一首" onClick={previous}>
          <Icon name="previous" size={18} />
        </button>
        <button className="icon-button play" aria-label={active ? '暂停' : '播放'} onClick={toggle}>
          <PlayGlyph playing={active} size={20} />
        </button>
        <button className="icon-button" aria-label="下一首" onClick={next}>
          <Icon name="next" size={18} />
        </button>
      </div>
      <button className="mini-song" onClick={onOpen} aria-label="打开正在播放">
        <Cover src={current.cover} size={40} eager />
        <span className="song-text">
          <strong>{current.title}</strong>
          <small>{loading ? '正在找可以播放的版本…' : artistLine(current)}</small>
        </span>
        <MiniProgress active={!covered} />
      </button>
      <div className="mini-extra">
        {actual && <Heart item={actual} />}
        {actual && <DownloadButton key={actual.songId ?? actual.copies.map(copy => `${copy.source}:${copy.id}`).join('|')} item={actual} />}
        <Volume />
      </div>
      <button className="icon-button mini-play-mobile" aria-label={active ? '暂停' : '播放'} onClick={toggle}>
        <PlayGlyph playing={active} size={24} />
      </button>
      <button className="icon-button mini-next-mobile" aria-label="下一首" onClick={next}>
        <Icon name="next" size={22} />
      </button>
    </div>
  );
}

function MiniProgress({ active }: { active: boolean }) {
  const { time, duration } = usePosition(active);
  const ratio = duration > 0 ? Math.min(1, time / duration) : 0;
  return (
    <span className="mini-progress" aria-hidden="true">
      <i style={{ transform: `scaleX(${ratio})` }} />
    </span>
  );
}

export function Volume() {
  const { volume, muted, setVolume, toggleMute } = usePlayer();
  return (
    <label className="volume">
      <button className="icon-button" aria-label={muted ? '取消静音' : '静音'} onClick={toggleMute}>
        <Icon name={muted || volume === 0 ? 'mute' : 'volume'} size={17} />
      </button>
      <input
        type="range"
        min={0}
        max={1}
        step={0.01}
        value={muted ? 0 : volume}
        onChange={(e) => setVolume(Number(e.target.value))}
        aria-label="音量"
        style={{ ['--fill' as string]: `${(muted ? 0 : volume) * 100}%` }}
      />
    </label>
  );
}

/** Lays out sidebar, content, tab bar and player; tracks scroll for the header edge. */
export function Frame({ children, overlay }: { children: ReactNode; overlay: ReactNode }) {
  const [main, setMain] = useState<HTMLElement | null>(null);
  const [scrolled, setScrolled] = useState(false);
  const [canReturn,setCanReturn]=useState(false);
  const [collapsed, setCollapsed] = useState(false);
  const path = usePath();
  usePageMotion(main, path.split('?')[0]);
  useScrollMemory(main, path);
  useEffect(() => {
    if (!main) return;
    const onScroll = () => {setScrolled(main.scrollTop > 8);setCanReturn(main.scrollTop>480);};
    onScroll();
    main.addEventListener('scroll', onScroll, { passive: true });
    return () => main.removeEventListener('scroll', onScroll);
  }, [main]);
  return (
    <div className={`frame${collapsed ? ' is-sidebar-collapsed' : ''}`}>
      <Sidebar collapsed={collapsed} onToggle={() => setCollapsed((value) => !value)} />
      <main ref={setMain} className={`content${scrolled ? ' is-scrolled' : ''}`}>
        {children}
      </main>
      {overlay}
      <button className={`back-to-top${canReturn?' is-visible':''}`} type="button" aria-label="回到顶部" title="回到顶部" aria-hidden={!canReturn} tabIndex={canReturn?0:-1} onClick={()=>main?.scrollTo({top:0,behavior:reduceMotion()?'instant':'smooth'})}><span/></button>
      <TabBar />
    </div>
  );
}
