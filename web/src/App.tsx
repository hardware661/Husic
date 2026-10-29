import { Activity, useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { LibraryProvider, useLibrary } from './library';
import { NowPlaying } from './nowplaying';
import { Charts } from './charts';
import { Discover } from './discover';
import { Home } from './home';
import { Library, Liked, PlaylistPage, Recent, RemotePlaylist, Search } from './pages';
import { PlayerProvider, usePlayer } from './player';
import { match, usePath, useMobileEdgeGuard } from './router';
import { Frame, MiniPlayer } from './shell';
import { Toaster } from './toast';
import { SearchOverlay, openSearch } from './search-ui';
import { Settings } from './settings';
import { AccountCollections, AccountPlaylist, BilibiliVideo, accountKind } from './account-pages';
import { DownloadManager, LocalImport } from './offline-music';
import { usePageVisible } from './view-activity';

export function App() {
  return (
    <PlayerProvider>
      <LibraryProvider>
        <Presentation />
      </LibraryProvider>
    </PlayerProvider>
  );
}

/** An inactive window retains its state, but releases all presentation effects
 * and rendering surfaces. Audio and the sleep engine live outside this boundary. */
function Presentation() {
  const visible = usePageVisible();
  const scroll = useRef<{ node: HTMLElement; top: number; left: number } | null>(null);
  useEffect(() => {
    const capture = () => {
      const node = document.querySelector<HTMLElement>('.content');
      if (node) scroll.current = { node, top: node.scrollTop, left: node.scrollLeft };
    };
    window.addEventListener('music-before-presentation-suspend', capture);
    return () => window.removeEventListener('music-before-presentation-suspend', capture);
  }, []);
  useLayoutEffect(() => {
    if (!visible || !scroll.current) return;
    const saved = scroll.current; scroll.current = null;
    if (saved.node.isConnected) { saved.node.scrollTop = saved.top; saved.node.scrollLeft = saved.left; }
  }, [visible]);
  return <Activity mode={visible ? 'visible' : 'hidden'}><Layout /></Activity>;
}

function Layout() {
  useMobileEdgeGuard();
  const [open, setOpen] = useState(false);
  const close = useCallback(() => setOpen(false), []);
  const { current, presentation, toggle, audio, seek, next, previous, volume, setVolume } = usePlayer();
  const { toggleLike } = useLibrary();
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.repeat) return;
      const target = e.target instanceof Element ? e.target : null;
      const typing = !!target?.closest('input,textarea,select,[contenteditable]:not([contenteditable="false"])');
      if (document.querySelector('dialog[open], [role="menu"]')) return;
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k' || e.key === '/' && !typing && !e.altKey && !e.ctrlKey && !e.metaKey) {
        e.preventDefault();
        setOpen(false);
        openSearch();
        return;
      }
      if (typing || !current || e.altKey) return;
      if ((e.ctrlKey || e.metaKey) && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) { e.preventDefault(); (e.key === 'ArrowLeft' ? previous : next)(); return; }
      if (e.ctrlKey || e.metaKey) return;
      // Preserve Space for controls; song-launch cards use global transport.
      const launch = target?.closest('.sleeve,.quilt-tile,.feature-lead,.feature-row,.wall-item,.shelf-item,.flow-item,.song-row,.grid-song,.radar-blip,.arrival-row,.side-nav a,.tab-bar a');
      const control = target?.closest('button,a,[role="button"]');
      if (e.code === 'Space' && control && (!launch || target?.closest('.icon-button,.menu'))) return;
      if (e.code === 'Space') {
        e.preventDefault();
        toggle();
      }
      if (e.key.toLowerCase() === 'l' && presentation === current) { e.preventDefault(); void toggleLike(current); }
      if (open && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) { e.preventDefault(); seek(Math.max(0, Math.min(audio.duration || current.duration, audio.currentTime + (e.key === 'ArrowRight' ? 5 : -5)))); }
      if (open && (e.key === 'ArrowUp' || e.key === 'ArrowDown')) { e.preventDefault(); setVolume(Math.max(0, Math.min(1, volume + (e.key === 'ArrowUp' ? .05 : -.05)))); }
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [current, presentation, toggle, audio, seek, next, previous, volume, setVolume, toggleLike, open]);
  return (
    <Frame
      overlay={
        <>
          <MiniPlayer onOpen={() => setOpen(true)} covered={open} />
          <NowPlaying open={open} onClose={close} />
          <SearchOverlay onCommit={close} />
          <Toaster />
        </>
      }
    >
      <Routes />
    </Frame>
  );
}

// Only the three heavy editorial destinations are retained. Activity removes
// hidden effects/subscriptions and display layers; details and forms still
// unmount normally. Stable children avoid rerendering every cached page.
const retainedPages = { '/': <Home />, '/discover': <Discover />, '/charts': <Charts /> };
type RetainedPath = keyof typeof retainedPages;
function Routes() {
  const path = usePath();
  const pathname = path.split('?')[0];
  const retained = Object.hasOwn(retainedPages, pathname) ? pathname as RetainedPath : null;
  const [visited, setVisited] = useState<RetainedPath[]>(() => retained ? [retained] : []);
  if (retained && !visited.includes(retained)) setVisited([...visited, retained]);
  return <>
    {visited.map((page) => <div key={page} className="route-panel" data-route-active={page === retained ? 'true' : undefined} hidden={page !== retained}>
      <Activity mode={page === retained ? 'visible' : 'hidden'}>{retainedPages[page]}</Activity>
    </div>)}
    {!retained && <div className="route-panel" data-route-active="true"><OtherRoutes path={path} /></div>}
  </>;
}

function OtherRoutes({ path }: { path: string }) {
  let params: Record<string, string> | null;
  if ((params = match('/playlist/:id', path))) return <PlaylistPage key={params.id} id={Number(params.id)} />;
  if ((params = match('/remote/:source/:id', path))) return accountKind(params.source) ? <AccountPlaylist kind={params.source} id={params.id} /> : <RemotePlaylist id={params.id} />;
  if ((params = match('/account/:source', path)) && accountKind(params.source)) return <AccountCollections key={params.source} kind={params.source} />;
  if ((params = match('/bilibili/:id', path))) return <BilibiliVideo id={params.id} />;
  const [pathname] = path.split('?');
  switch (pathname) {
    case '/settings':
      return <Settings />;
    case '/discover':
      return <Discover />;
    case '/charts':
      return <Charts />;
    case '/search':
      return <Search />;
    case '/library':
      return <Library />;
    case '/liked':
      return <Liked />;
    case '/recent':
      return <Recent />;
    case '/downloads':
      return <DownloadManager />;
    case '/local':
      return <LocalImport />;
    default:
      return <Home />;
  }
}
