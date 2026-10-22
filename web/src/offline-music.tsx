import { useRef, useState, type DragEvent, type ReactNode } from 'react';
import { artistLine, clock } from './api';
import { BackButton, Bars, Cover, Empty, Heart, PlayButtons } from './components';
import { Icon } from './icons';
import { usePlayer } from './player';
import { toast } from './toast';
import {
  cancelAudioDownload, dismissAudioDownload, downloadAudio, exportLocalAudio, importLocalAudio,
  localAudioAccept, localAudioID, removeLocalAudio, useAudioDownloads, useLocalAudio,
  type AudioDownload, type LocalAudioRecord,
} from './local-audio';
import './local-music.css';

function sizeLabel(bytes: number) {
  if (bytes < 1024 * 1024) return `${Math.max(1, Math.round(bytes / 1024))} KB`;
  return `${(bytes / 1024 / 1024).toFixed(bytes < 10 * 1024 * 1024 ? 1 : 0)} MB`;
}

const fail = (error: unknown) => toast(error instanceof Error ? error.message : '操作没有完成，请重试');

function AudioRows({ records }: { records: LocalAudioRecord[] }) {
  const { play, current, playing, addToQueue } = usePlayer();
  const [removing, setRemoving] = useState('');
  const items = records.map(record => record.item);
  return <ol className="offline-song-list" aria-label="本地音频">
    {records.map((record, index) => {
      const active = current !== null && localAudioID(current) === record.id;
      return <li key={record.id} data-local-audio-id={record.id} className={`offline-song-row${active ? ' is-current' : ''}`}
        tabIndex={0} onClick={() => play(items, index)}
        onKeyDown={event => { if (event.key === 'Enter' && event.target === event.currentTarget) play(items, index); }}>
        <span className="offline-song-art"><Cover src={record.item.cover} size={44} />
          {active && playing && <span className="offline-playing"><Bars /></span>}
        </span>
        <span className="song-text offline-song-title"><strong title={record.item.title}>{record.item.title}</strong>
          <small title={record.fileName}>{artistLine(record.item) || record.fileName}</small>
        </span>
        <span className="offline-file-info"><span className="offline-format">{record.format.toUpperCase()}</span><span>{sizeLabel(record.size)}</span></span>
        <span className="offline-song-duration">{clock(record.item.duration)}</span>
        <span className="offline-song-actions" onClick={event => event.stopPropagation()}>
          <Heart item={record.item} />
          <button className="icon-button offline-secondary-action" aria-label={`将${record.item.title}加入待播`} title="加入待播" onClick={() => addToQueue(record.item)}><Icon name="plus" size={17}/></button>
          <button className="icon-button" aria-label={`另存为${record.item.title}`} title="另存为" onClick={() => void exportLocalAudio(record.id).catch(fail)}><Icon name="download" size={17}/></button>
          <button className="icon-button danger" disabled={removing === record.id} aria-label={`移除${record.item.title}的本地副本`} title="移除本地副本" onClick={async () => {
            setRemoving(record.id);
            try { await removeLocalAudio(record.id); toast('已移除本地副本'); } catch (error) { fail(error); }
            finally { setRemoving(''); }
          }}><Icon name="close" size={16}/></button>
        </span>
      </li>;
    })}
  </ol>;
}

function AudioHeading({ title, records, children }: { title: string; records: LocalAudioRecord[]; children?: ReactNode }) {
  const size = records.reduce((sum, record) => sum + record.size, 0);
  return <>
    <nav className="collection-navigation" aria-label="页面导航"><BackButton /></nav>
    <header className="offline-heading">
      <span className="offline-heading-art"><Icon name={title === '本地导入' ? 'folder' : 'download'} size={34}/></span>
      <div className="offline-heading-title"><h1>{title}</h1><span>{records.length} 首{size ? ` · ${sizeLabel(size)}` : ''}</span></div>
      {children}
    </header>
  </>;
}

export function LocalImport() {
  const { records, loading, error } = useLocalAudio('import');
  const input = useRef<HTMLInputElement>(null);
  const depth = useRef(0);
  const importing = useRef(false);
  const [dragging, setDragging] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problems, setProblems] = useState<string[]>([]);
  const [status, setStatus] = useState('');
  const importFiles = async (files: File[]) => {
    if (!files.length || importing.current) return;
    importing.current = true;
    setBusy(true); setProblems([]); setStatus('');
    try {
      const result = await importLocalAudio(files);
      const text = [result.added ? `已导入 ${result.added} 首` : '', result.duplicates ? `${result.duplicates} 首已存在` : '', result.failed.length ? `${result.failed.length} 首未能导入` : ''].filter(Boolean).join(' · ');
      setStatus(text); setProblems(result.failed); if (text) toast(text);
    } catch (error) { setProblems([error instanceof Error ? error.message : '导入失败，请重试']); }
    finally { importing.current = false; setBusy(false); if (input.current) input.current.value = ''; }
  };
  const hasFiles = (event: DragEvent) => [...event.dataTransfer.types].includes('Files');
  return <div className={`page offline-page local-import-page${dragging ? ' is-dragging' : ''}`} aria-busy={busy}
    onDragEnter={event => { if (!hasFiles(event)) return; event.preventDefault(); depth.current++; setDragging(true); }}
    onDragOver={event => { if (!hasFiles(event)) return; event.preventDefault(); event.dataTransfer.dropEffect = busy ? 'none' : 'copy'; }}
    onDragLeave={event => { if (!hasFiles(event)) return; event.preventDefault(); if (--depth.current <= 0) { depth.current = 0; setDragging(false); } }}
    onDrop={event => { event.preventDefault(); depth.current = 0; setDragging(false); void importFiles([...event.dataTransfer.files]); }}>
    <input ref={input} className="sr-only" tabIndex={-1} type="file" accept={localAudioAccept} multiple aria-label="选择本地音频" onChange={event => void importFiles([...(event.currentTarget.files ?? [])])}/>
    <AudioHeading title="本地导入" records={records}>
      <button className="pill primary offline-import-button" disabled={busy} onClick={() => input.current?.click()}><Icon name="plus" size={16}/>{busy ? '正在导入…' : '导入音频'}</button>
    </AudioHeading>
    {records.length > 0 && <div className="offline-toolbar"><PlayButtons items={records.map(record => record.item)}/></div>}
    {error && <p className="offline-error" role="alert">{error}</p>}
    {problems.length > 0 && <div className="offline-import-errors" role="alert"><button className="icon-button" aria-label="关闭导入结果" onClick={() => setProblems([])}><Icon name="close" size={16}/></button>{problems.map((problem, i) => <p key={i}>{problem}</p>)}</div>}
    <span className="sr-only" role="status">{status}</span>
    {loading ? <div className="loading" aria-label="正在读取本地音乐"/> : records.length ? <AudioRows records={records}/> : <div className="offline-drop-empty">
      <Icon name="folder" size={42}/><h2>{busy ? '正在读取音频…' : '把音乐拖到这里'}</h2>
      <span className="offline-supported-formats">WAV · MP3 · FLAC · AAC · M4A · OGG · OPUS · WEBM</span>
    </div>}
    {dragging && <div className="offline-drop-target" aria-hidden="true"><Icon name="plus" size={36}/><strong>{busy ? '正在导入…' : '松开，导入音乐'}</strong></div>}
  </div>;
}

function DownloadTask({ task }: { task: AudioDownload }) {
  const active = task.state === 'downloading' || task.state === 'saving';
  const percent = task.total ? Math.min(100, Math.round(task.received / task.total * 100)) : null;
  return <li className={`offline-download-task${active ? '' : ' is-finished'}`}>
    <Cover src={task.item.cover} size={44}/>
    <div className="offline-task-content"><strong>{task.item.title}</strong>
      <span className="offline-task-state">{task.state === 'saving' ? '正在保存' : task.state === 'downloading'
        ? task.received===0?'准备下载':`${sizeLabel(task.received)}${task.total ? ` / ${sizeLabel(task.total)}` : ''}` : task.error}</span>
      {active && <div className={`offline-progress${percent === null ? ' is-indeterminate' : ''}`} role="progressbar" aria-label={`下载${task.item.title}`} aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent ?? undefined}>
        <span style={percent === null ? undefined : { width: `${percent}%` }}/>
      </div>}
    </div>
    {!active && task.state === 'failed' && <button className="icon-button" aria-label={`重新下载${task.item.title}`} title="重新下载" onClick={() => { dismissAudioDownload(task.id); void downloadAudio(task.item).then(() => toast('已下载到本地')).catch(fail); }}><Icon name="sync" size={17}/></button>}
    <button className="icon-button" aria-label={active ? `取消下载${task.item.title}` : '移除下载记录'} title={active ? '取消下载' : '移除记录'} onClick={() => active ? cancelAudioDownload(task.id) : dismissAudioDownload(task.id)}><Icon name="close" size={17}/></button>
  </li>;
}

export function DownloadManager() {
  const { records, loading, error } = useLocalAudio('download');
  const tasks = useAudioDownloads();
  const active = tasks.filter(task => task.state === 'downloading' || task.state === 'saving').length;
  return <div className="page offline-page download-manager-page">
    <AudioHeading title="下载管理" records={records}/>
    {tasks.length > 0 && <section className="offline-download-section" aria-label="下载任务">
      <header className="section-head"><h2>{active ? `下载中 · ${active}` : '下载记录'}</h2></header>
      <ol className="offline-download-tasks">{tasks.map(task => <DownloadTask key={task.id} task={task}/>)}</ol>
    </section>}
    {error && <p className="offline-error" role="alert">{error}</p>}
    {records.length > 0 && <div className="offline-toolbar"><h2>已完成 <span>{records.length}</span></h2><PlayButtons items={records.map(record => record.item)}/></div>}
    {loading ? <div className="loading" aria-label="正在读取下载"/> : records.length ? <AudioRows records={records}/> : tasks.length === 0 ? <Empty title="还没有下载"/> : null}
  </div>;
}
