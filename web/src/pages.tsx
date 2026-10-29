import { SearchField } from './search-ui';
import { searchMusic, rememberSearch } from './search-data';
import { useEffect, useState } from 'react';
import { api, type Item } from './api';
import { BackButton, BulkDownloadButton, Empty, PageHeader, PlayButtons, PlaylistTile, Section, SongList } from './components';
import { ActionDialog } from './dialogs';
import { Icon } from './icons';
import { useLibrary } from './library';
import { navigate, query, usePath } from './router';
import { toast } from './toast';
import { CollectionCover } from './collection-cover';
import { useLocalAudio } from './local-audio';
import { PlaylistActions } from './playlist-actions';

/** Loads data for a page. `what` names the content (say, a playlist id):
 * data loaded for something else is never shown for it. `version` only asks
 * for a fresh copy of the same content, which stays visible meanwhile. */
function useLoad<T>(load: () => Promise<T>, what: unknown[], version = 0): { data?: T; error?: string; loading: boolean } {
  const key = JSON.stringify(what);
  const [state, setState] = useState<{ key: string; data?: T; error?: string; loading: boolean }>({ key, loading: true });
  useEffect(() => {
    let alive = true;
    setState((s) => (s.key === key ? { ...s, loading: true } : { key, loading: true }));
    load()
      .then((data) => alive && setState({ key, data, loading: false }))
      .catch((e: Error) => alive && setState((current) => ({ ...current, key, error: e.message, loading: false })));
    return () => {
      alive = false;
    };
  }, [key, version]); // eslint-disable-line react-hooks/exhaustive-deps
  return state.key === key ? state : { loading: true };
}

function Loading() {
  return <div className="loading" aria-label="正在加载" />;
}

function Failed({ message }: { message?: string }) {
  return <Empty title="没能加载" hint={message} />;
}

export function Search() {
  const initial=query(usePath(),'q');
  const [results,setResults]=useState<Item[]|null>(null),[busy,setBusy]=useState(false);
  useEffect(()=>{let live=true;setResults(null);if(!initial){setBusy(false);return;}rememberSearch(initial);setBusy(true);searchMusic(initial).then(r=>{if(live)setResults(r.songs);}).catch(()=>{if(live)toast('搜索失败');}).finally(()=>{if(live)setBusy(false);});return()=>{live=false;};},[initial]);
  return <div className="page search-page"><PageHeader title="搜索" search={false}><SearchField initial={initial} onSearch={q=>navigate(`/search?q=${encodeURIComponent(q)}`,Boolean(initial))}/></PageHeader>{busy&&<Loading/>}{!busy&&results&&<SongList items={results} empty={<Empty title="没有找到"/>}/>}</div>;
}

export function Library() {
  const { playlists, createPlaylist } = useLibrary();
  const [creating, setCreating] = useState(false);
  const { records } = useLocalAudio();
  const downloads = records.filter((record) => record.origin === 'download').length;
  const imports = records.filter((record) => record.origin === 'import').length;
  return (
    <div className="page">
      <PageHeader title="音乐库" />
      <div className="library-shortcuts">
        <button className="shortcut" onClick={() => navigate('/downloads')}>
          <span className="shortcut-art">
            <Icon name="download" size={26} />
          </span>
          <strong>下载管理</strong>
          <small>{downloads} 首</small>
        </button>
        <button className="shortcut" onClick={() => navigate('/local')}>
          <span className="shortcut-art">
            <Icon name="folder" size={26} />
          </span>
          <strong>本地导入</strong>
          <small>{imports} 首</small>
        </button>
      </div>
      <Section
        title="歌单"
        action={
          <button
            className="text-button"
            onClick={() => setCreating(true)}
          >
            <Icon name="plus" size={13} />
            新建
          </button>
        }
      >
        {playlists.length === 0 ? (
          <Empty title="还没有歌单" />
        ) : (
          <div className="tiles">{playlists.map((p) => <PlaylistTile key={p.id} playlist={p} />)}</div>
        )}
      </Section>
      {creating && <ActionDialog title="新建歌单" inputLabel="歌单名称" confirmLabel="创建" onClose={() => setCreating(false)} onConfirm={async (name) => { await createPlaylist(name); }} />}
    </div>
  );
}

export function Liked() {
  const { version } = useLibrary();
  const { data, error, loading } = useLoad(() => api.library(), [], version);
  const { records, loading: localLoading, error: localError } = useLocalAudio();
  const songs = [...records.filter((record) => record.liked).map((record) => record.item), ...(data?.likes ?? [])];
  if ((loading && !data || localLoading) && songs.length === 0) return <Loading />;
  if (!data && (error || localError) && songs.length === 0) return <Failed message={error || localError} />;
  return (
    <div className="page collection-page">
      <nav className="collection-navigation" aria-label="页面导航"><BackButton /></nav>
      <CollectionHeader title="我喜欢" subtitle={`${songs.length} 首歌`} art={<CollectionCover coverKey="liked" title="我喜欢" src={songs[0]?.cover} liked/>} items={songs} />
      <SongList items={songs} empty={<Empty title="还没有喜欢的歌" />} />
    </div>
  );
}

export function Recent() {
  const { data, error, loading } = useLoad(() => api.home(), []);
  const { records, loading: localLoading, error: localError } = useLocalAudio();
  const localRecent = records.filter((record) => record.playedAt).map((record) => ({ ...record.item, playedAt: record.playedAt }));
  const songs = [...localRecent, ...(data?.recent ?? [])].sort((a, b) => (Date.parse(b.playedAt || '') || 0) - (Date.parse(a.playedAt || '') || 0));
  if ((loading && !data || localLoading) && songs.length === 0) return <Loading />;
  if (!data && (error || localError) && songs.length === 0) return <Failed message={error || localError} />;
  return (
    <div className="page">
      <PageHeader title="最近播放" />
      <SongList items={songs} empty={<Empty title="还没有播放记录" />} />
    </div>
  );
}

export function PlaylistPage({ id }: { id: number }) {
  const { version, refresh, renamePlaylist } = useLibrary();
  const { data, error, loading } = useLoad(() => api.playlist(id), [id], version);
  const [removed, setRemoved] = useState<Set<number>>(new Set());
  const [deleting, setDeleting] = useState(false);
  const [renaming, setRenaming] = useState(false);
  if (loading && !data) return <Loading />;
  if (error || !data) return <Failed message={error} />;
  const songs = data.songs.filter((s) => !removed.has(s.songId ?? 0));
  const p = data.playlist;
  const subtitle = `${songs.length} 首歌`;
  return (
    <div className="page playlist-page">
      <nav className="collection-navigation" aria-label="页面导航"><BackButton /></nav>
      <CollectionHeader
        title={p.name}
        subtitle={subtitle}
        note={p.description}
        art={<CollectionCover coverKey={`playlist:${p.id}`} title={p.name} src={p.cover||songs[0]?.cover}/>}
        items={songs}
        menu={<PlaylistActions onRename={() => setRenaming(true)} onDelete={() => setDeleting(true)} />}
        extra={<BulkDownloadButton items={songs} name={p.name} />}
      />
      <SongList
        items={songs}
        numbered
        onRemove={async (item) => {
          if (!item.songId) return;
          await api.removeFromPlaylist(p.id, item.songId);
          setRemoved((r) => new Set(r).add(item.songId!));
        }}
        empty={<Empty title="歌单是空的" />}
      />
      {renaming && <ActionDialog title="重命名歌单" inputLabel="歌单名称" initialValue={p.name} confirmLabel="保存" onClose={() => setRenaming(false)} onConfirm={async name => { await renamePlaylist(p, name); toast('歌单已重命名'); }} />}
      {deleting && <ActionDialog title="删除歌单" description={`从 Husic 中删除「${p.name}」？不会删除歌曲或音乐平台上的歌单。`} confirmLabel="删除歌单" onClose={() => setDeleting(false)} onConfirm={async () => { await api.deletePlaylist(p.id); refresh(); navigate('/library'); }} />}
    </div>
  );
}

export function RemotePlaylist({ id }: { id: string }) {
  const { data, error, loading } = useLoad(() => api.remotePlaylist(id), [id]);
  if (loading && !data) return <Loading />;
  if (error || !data) return <Failed message={error} />;
  return (
    <div className="page playlist-page">
      <nav className="collection-navigation" aria-label="页面导航"><BackButton /></nav>
      <CollectionHeader
        title={data.playlist.name}
        subtitle={`${data.songs.length} 首歌`}
        note={data.playlist.note}
        art={<CollectionCover coverKey={`netease:${id}`} title={data.playlist.name} src={data.playlist.cover||data.songs[0]?.cover}/>}
        items={data.songs}
        extra={<BulkDownloadButton items={data.songs} name={data.playlist.name} />}
      />
      <SongList items={data.songs} numbered />
    </div>
  );
}

export function CollectionHeader({ title, subtitle, note, art, items, extra, menu }: {
  title: string; subtitle: string; note?: string; art: React.ReactNode; items: Item[]; extra?: React.ReactNode; menu?: React.ReactNode;
}) {
  return (
    <header className={`collection-header${menu ? ' has-menu' : ''}`}>
      {menu && <div className="collection-menu">{menu}</div>}
      {art}
      <div className="collection-info">
        <h1>{title}</h1>
        <p className="collection-sub">{subtitle}</p>
        {note && <p className="collection-note">{note}</p>}
        <div className="collection-actions">
          <PlayButtons items={items} />
          {extra}
        </div>
      </div>
    </header>
  );
}
