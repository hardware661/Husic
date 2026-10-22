import { useEffect, useRef, useState, type FormEvent } from 'react';
import { api, inspectionID, type AccountCollection, type AccountCollectionPage, type AccountPlatform } from './api';
import { CardTile, Cover, Empty, PageHeader, PlayButtons, SongList } from './components';
import { Icon } from './icons';
import { Link, navigate } from './router';
import { CollectionCover } from './collection-cover';
import { CollectionHeader } from './pages';

export const accountKind = (value: string): value is AccountPlatform => value === 'bilibili';

function VideoLink() {
  const [input, setInput] = useState('');
  const [error, setError] = useState('');
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const id = input.trim().match(/\bBV[0-9A-Za-z]{10}\b/)?.[0];
    if (!id) { setError('粘贴完整视频链接，或输入 BV 号。'); return; }
    navigate(`/bilibili/${id}`);
  };
  return <form className="video-link-form" onSubmit={submit}>
    <label htmlFor="bilibili-video-link">有想听的视频？</label>
    <div><input id="bilibili-video-link" value={input} onChange={(event) => { setInput(event.target.value); setError(''); }} placeholder="粘贴视频链接或 BV 号" autoComplete="off" maxLength={1000} aria-describedby={error ? 'video-link-error' : undefined} />
      <button type="submit" className="pill"><Icon name="chevron" size={14} />打开</button></div>
    {error && <p id="video-link-error" role="alert">{error}</p>}
  </form>;
}

export function AccountCollections({ kind }: { kind: AccountPlatform }) {
  const [collections, setCollections] = useState<AccountCollection[] | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    let alive = true; setCollections(null); setError('');
    void api.accountCollections(kind).then((data) => { if (alive) setCollections(data.collections); }).catch((e: Error) => { if (alive) setError(e.message); });
    return () => { alive = false; };
  }, [kind]);
  return <div className="page account-page">
    <PageHeader title={kind !== 'bilibili' ? '我的歌单' : '我的收藏夹'}><Link className="pill" to="/settings">连接管理</Link></PageHeader>
    {kind === 'bilibili' && <VideoLink />}
    {error ? <Empty title="暂时没有读到账号内容" hint={error} /> : collections === null ? <p className="account-note" role="status">正在翻开你的收藏…</p> : collections.length === 0 ? <Empty title={kind !== 'bilibili' ? '还没有歌单' : '还没有收藏夹'} /> : <div className="tiles account-collections">{collections.map((collection) => <CardTile key={collection.id} card={{ ...collection, source: kind }} />)}</div>}
  </div>;
}

export function AccountPlaylist({ kind, id }: { kind: AccountPlatform; id: string }) {
  const [data, setData] = useState<AccountCollectionPage | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const generation = useRef(0);
  const busy = useRef(false);
  useEffect(() => {
    const sequence = ++generation.current; busy.current = true; setData(null); setError(''); setLoading(true);
    void api.accountCollection(kind, id).then((result) => { if (generation.current === sequence) setData(result); }).catch((e: Error) => { if (generation.current === sequence) setError(e.message); }).finally(() => { if (generation.current === sequence) { busy.current = false; setLoading(false); } });
    return () => { generation.current++; };
  }, [kind, id]);
  const more = async () => {
    if (!data?.next || busy.current) return;
    const sequence = generation.current; busy.current = true; setLoading(true); setError('');
    try {
      const result = await api.accountCollection(kind, id, data.next);
      if (generation.current === sequence) setData((previous) => previous ? { ...result, songs: [...previous.songs, ...result.songs.filter((item) => !previous.songs.some((old) => old.copies[0]?.id === item.copies[0]?.id))] } : result);
    } catch (e) { if (generation.current === sequence) setError(e instanceof Error ? e.message : '暂时没有加载完成'); }
    finally { if (generation.current === sequence) { busy.current = false; setLoading(false); } }
  };
  const playable = data?.songs.filter((item) => !inspectionID(item)) || [];
  return <div className="page account-page playlist-page">
    <PageHeader title={kind !== 'bilibili' ? '歌单' : '收藏夹'} search={false}><Link className="pill" to={`/account/${kind}`}>返回收藏</Link></PageHeader>
    {data && <><CollectionHeader title={data.collection.name} subtitle={`${data.total} ${kind !== 'bilibili' ? '首歌' : '个视频'}`} art={<CollectionCover coverKey={`${kind}:${id}`} title={data.collection.name} src={data.collection.cover||data.songs[0]?.cover}/>} items={playable}/>
      <SongList items={data.songs} numbered empty={<Empty title="这里暂时没有内容" />} /></>}
    {loading && !data && <p className="account-note" role="status">正在加载收藏内容…</p>}
    {error && <p className="account-note" role="alert">{error}</p>}
    {data?.next !== 0 && data && <button className="pill account-load-more" onClick={more} disabled={loading}>{loading ? '正在加载…' : '继续加载'}</button>}
  </div>;
}

export function BilibiliVideo({ id }: { id: string }) {
  const [data, setData] = useState<Awaited<ReturnType<typeof api.bilibiliVideo>> | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    let alive = true; setData(null); setError('');
    void api.bilibiliVideo(id).then((result) => { if (alive) setData(result); }).catch((e: Error) => { if (alive) setError(e.message); });
    return () => { alive = false; };
  }, [id]);
  return <div className="page account-page">
    <PageHeader title="选一段来听" search={false}><Link className="pill" to="/account/bilibili">我的收藏</Link></PageHeader>
    {data ? <><header className="account-collection-heading"><Cover src={data.video.cover} size={320} /><div><h2>{data.video.title}</h2><p>{data.songs.length} 个分P</p><PlayButtons items={data.songs} /></div></header><SongList items={data.songs} numbered /></> : <Empty title={error ? '暂时没有打开视频' : '正在读取分P…'} hint={error || undefined} />}
  </div>;
}
