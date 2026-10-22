import { useEffect, useSyncExternalStore } from 'react';
import { type Item, type Stream } from './api';

export type LocalAudioRecord = {
  id: string;
  origin: 'import' | 'download';
  item: Item;
  fileName: string;
  format: string;
  size: number;
  importedAt: string;
  playedAt?: string;
  liked: boolean;
};

type StoredAudio = LocalAudioRecord & { downloadKey?: string };
type AudioSnapshot = { records: LocalAudioRecord[]; loading: boolean; error: string };
export type AudioDownload = {
  id: string;
  item: Item;
  state: 'downloading' | 'saving' | 'failed' | 'cancelled';
  received: number;
  total: number;
  error: string;
};

const MAX_AUDIO_BYTES = 512 * 1024 * 1024;
const AUDIO_TYPES: Record<string, string> = {
  mp3: 'audio/mpeg', wav: 'audio/wav', flac: 'audio/flac', aac: 'audio/aac',
  m4a: 'audio/mp4', ogg: 'audio/ogg', opus: 'audio/ogg', webm: 'audio/webm',
};
export const localAudioAccept = '.mp3,.wav,.flac,.aac,.m4a,.ogg,.opus,.webm,audio/mpeg,audio/wav,audio/flac,audio/aac,audio/mp4,audio/ogg,audio/webm';
const AUDIO_CHANGE = 'local-audio-change';
const recordListeners = new Set<() => void>();
const downloadListeners = new Set<() => void>();
const blobURLs = new Map<string, string>();
const downloadControllers = new Map<string, AbortController>();
const downloadKeys = new Set<string>();
let snapshot: AudioSnapshot = { records: [], loading: true, error: '' };
let downloads: AudioDownload[] = [];
let database: Promise<IDBDatabase> | undefined;
let refreshVersion = 0;
let started = false;
let channel: BroadcastChannel | undefined;

function message(error: unknown, fallback = '没有保存成功，请稍后再试') {
  if (error instanceof DOMException) {
    if (error.name === 'QuotaExceededError') return '本地空间不足，请先移除一些本地副本';
    if (error.name === 'ConstraintError') return '这份音频已经保存在本地音乐库中';
    if (error.name === 'AbortError') return '操作已取消';
    if (error.name === 'SecurityError' || error.name === 'InvalidStateError') return '本地存储不可用，请检查浏览器的存储权限';
  }
  return error instanceof Error && error.message ? error.message : fallback;
}

function openDatabase() {
  if (database) return database;
  database = new Promise<IDBDatabase>((resolve, reject) => {
    if (!globalThis.indexedDB) { reject(new Error('当前环境不支持本地音乐存储')); return; }
    const request = indexedDB.open('husic-local-audio', 1);
    request.onupgradeneeded = () => {
      request.result.createObjectStore('records', { keyPath: 'id' });
      request.result.createObjectStore('blobs', { keyPath: 'id' });
    };
    request.onsuccess = () => {
      const db = request.result;
      db.onversionchange = () => { db.close(); database = undefined; };
      resolve(db);
    };
    request.onerror = () => { database = undefined; reject(request.error ?? new Error('无法打开本地音乐库')); };
    request.onblocked = () => { database = undefined; reject(new Error('本地音乐库正在更新，请关闭其它 Husic 窗口后重试')); };
  });
  return database;
}

function transactionComplete(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onerror = event => reject((event.target as IDBRequest | null)?.error ?? transaction.error ?? new Error('本地音乐保存失败'));
    transaction.onabort = () => reject(transaction.error ?? new Error('本地音乐保存失败'));
  });
}

function readRequest<T>(request: IDBRequest<T>) {
  return new Promise<T>((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error('无法读取本地音乐'));
  });
}

export async function localAudioRecords(): Promise<LocalAudioRecord[]> {
  const db = await openDatabase();
  const rows = await readRequest(db.transaction('records').objectStore('records').getAll()) as StoredAudio[];
  return rows.sort((a, b) => b.importedAt.localeCompare(a.importedAt));
}

async function refreshRecords() {
  const version = ++refreshVersion;
  try {
    const records = await localAudioRecords();
    if (version !== refreshVersion) return;
    snapshot = { records, loading: false, error: '' };
  } catch (error) {
    if (version !== refreshVersion) return;
    snapshot = { ...snapshot, loading: false, error: message(error, '无法读取本地音乐') };
  }
  recordListeners.forEach(listener => listener());
}

function initialize() {
  if (started) return;
  started = true;
  window.addEventListener(AUDIO_CHANGE, () => { void refreshRecords(); });
  if ('BroadcastChannel' in globalThis) {
    try {
      channel = new BroadcastChannel('husic-local-audio');
      channel.onmessage = () => window.dispatchEvent(new Event(AUDIO_CHANGE));
    } catch { /* A single-window library still works when channels are unavailable. */ }
  }
  void refreshRecords();
}

async function changed() {
  // Initialize before dispatching: changes can originate in a song menu before
  // either of the local-library pages has ever mounted.
  initialize();
  window.dispatchEvent(new Event(AUDIO_CHANGE));
  try { channel?.postMessage('changed'); } catch { /* Local subscribers already received the update. */ }
}

const subscribeRecords = (listener: () => void) => {
  recordListeners.add(listener);
  return () => { recordListeners.delete(listener); };
};
const getSnapshot = () => snapshot;

export function useLocalAudio(origin?: 'import' | 'download') {
  useEffect(initialize, []);
  const state = useSyncExternalStore(subscribeRecords, getSnapshot, getSnapshot);
  return { ...state, records: origin ? state.records.filter(record => record.origin === origin) : state.records };
}

export function localAudioID(item: Item): string | undefined {
  return item.copies.find(copy => copy.source === 'local')?.id;
}

async function audioBlob(id: string) {
  const db = await openDatabase();
  const row = await readRequest(db.transaction('blobs').objectStore('blobs').get(id)) as { id: string; blob: Blob } | undefined;
  if (!row?.blob) throw new Error('这首歌的本地副本已移除，请重新导入或下载');
  return row.blob;
}

export async function localAudioStream(item: Item): Promise<Stream> {
  const id = localAudioID(item);
  if (!id) throw new Error('没有找到这首歌的本地副本');
  // Read before reusing a URL so another window removing the record cannot
  // leave the player believing a deleted copy is still in the library.
  const blob = await audioBlob(id);
  let url = blobURLs.get(id);
  if (!url) { url = URL.createObjectURL(blob); blobURLs.set(id, url); }
  return { url, source: 'local', id, bitrate: item.duration > 0 ? Math.round(blob.size * 8 / item.duration) : 0, format: item.copies.find(copy => copy.source === 'local')?.extra?.format ?? 'audio' };
}

async function updateRecord(id: string, update: (record: StoredAudio) => StoredAudio) {
  const db = await openDatabase();
  const transaction = db.transaction('records', 'readwrite');
  const complete = transactionComplete(transaction);
  const store = transaction.objectStore('records');
  const request = store.get(id);
  request.onsuccess = () => {
    const record = request.result as StoredAudio | undefined;
    if (record) store.put(update(record));
  };
  await complete;
  await changed();
}

export async function setLocalAudioLiked(id: string, liked: boolean): Promise<void> {
  await updateRecord(id, record => ({ ...record, liked, item: { ...record.item, liked } }));
}

export async function recordLocalAudioPlay(id: string): Promise<void> {
  const playedAt = new Date().toISOString();
  await updateRecord(id, record => ({ ...record, playedAt, item: { ...record.item, playedAt } }));
}

export async function removeLocalAudio(id: string): Promise<void> {
  const db = await openDatabase();
  const transaction = db.transaction(['records', 'blobs'], 'readwrite');
  const complete = transactionComplete(transaction);
  transaction.objectStore('records').delete(id);
  transaction.objectStore('blobs').delete(id);
  await complete;
  const url = blobURLs.get(id);
  if (url) URL.revokeObjectURL(url);
  blobURLs.delete(id);
  await changed();
}

function safeFileName(name: string) {
  const clean = name.replace(/[<>:"/\\|?*\u0000-\u001f]/g, '_').replace(/[. ]+$/g, '');
  const dot = clean.lastIndexOf('.');
  const extension = dot > 0 && clean.length - dot <= 10 ? clean.slice(dot) : '';
  let base = (extension ? clean.slice(0, dot) : clean).slice(0, 180 - extension.length).replace(/[. ]+$/g, '') || 'Husic-audio';
  if (/^(con|prn|aux|nul|com[1-9]|lpt[1-9])$/i.test(base)) base = `_${base}`;
  return `${base}${extension}`;
}

export async function exportLocalAudio(id: string): Promise<void> {
  const db = await openDatabase();
  const record = await readRequest(db.transaction('records').objectStore('records').get(id)) as StoredAudio | undefined;
  if (!record) throw new Error('这首歌的本地副本已移除');
  const blob = await audioBlob(id);
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = safeFileName(record.fileName);
  link.hidden = true;
  document.body.append(link);
  link.click();
  link.remove();
  // WebView2 starts the native Save As flow asynchronously.
  window.setTimeout(() => URL.revokeObjectURL(url), 60_000);
}

async function fingerprint(blob: Blob) {
  if (!crypto.subtle) throw new Error('请通过 HTTPS 或本机地址打开 Husic 后导入音乐');
  const digest = await crypto.subtle.digest('SHA-256', await blob.arrayBuffer());
  return [...new Uint8Array(digest)].map(byte => byte.toString(16).padStart(2, '0')).join('');
}

type AudioTags = { title?: string; artists?: string[]; album?: string };
function cleanTag(text: string) { return text.replace(/\u0000/g, '').trim().slice(0, 500); }
function tagText(bytes: Uint8Array) {
  if (!bytes.length) return '';
  const encoding = bytes[0];
  const text = bytes.subarray(1);
  if (encoding === 0) return cleanTag(new TextDecoder('windows-1252').decode(text));
  if (encoding === 1) return cleanTag(new TextDecoder(text[0] === 0xfe && text[1] === 0xff ? 'utf-16be' : 'utf-16le').decode(text));
  return cleanTag(new TextDecoder(encoding === 2 ? 'utf-16be' : 'utf-8').decode(text));
}

/** Read only bounded tag headers; the browser is responsible for decoding. */
async function readTags(blob: Blob): Promise<AudioTags> {
  const bytes = new Uint8Array(await blob.slice(0, 512 * 1024).arrayBuffer());
  const view = new DataView(bytes.buffer);
  const ascii = (start: number, length: number) => String.fromCharCode(...bytes.subarray(start, start + length));
  const tags: AudioTags = {};
  const apply = (name: string, value: string) => {
    if (!value) return;
    if (['TIT2', 'TT2', 'TITLE', 'INAM'].includes(name)) tags.title = value;
    if (['TPE1', 'TP1', 'ARTIST', 'IART'].includes(name)) tags.artists = value.split(/\u0000|\s+\/\s+/).filter(Boolean);
    if (['TALB', 'TAL', 'ALBUM', 'IPRD'].includes(name)) tags.album = value;
  };
  if (ascii(0, 3) === 'ID3' && bytes.length >= 10) {
    const version = bytes[3];
    if (version >= 2 && version <= 4 && !(bytes[5] & 0x80)) {
      let offset = 10;
      if (bytes[5] & 0x40 && version >= 3 && bytes.length >= 14) {
        const extra = version === 4 ? (bytes[10] << 21) | (bytes[11] << 14) | (bytes[12] << 7) | bytes[13] : view.getUint32(10);
        offset += extra + (version === 3 ? 4 : 0);
      }
      while (offset + (version === 2 ? 6 : 10) <= bytes.length) {
        const name = ascii(offset, version === 2 ? 3 : 4);
        if (!/^[A-Z0-9]+$/.test(name)) break;
        const header = version === 2 ? 6 : 10;
        const length = version === 2 ? (bytes[offset + 3] << 16) | (bytes[offset + 4] << 8) | bytes[offset + 5]
          : version === 4 ? (bytes[offset + 4] << 21) | (bytes[offset + 5] << 14) | (bytes[offset + 6] << 7) | bytes[offset + 7]
            : view.getUint32(offset + 4);
        if (length <= 0 || offset + header + length > bytes.length) break;
        if (['TIT2', 'TT2', 'TPE1', 'TP1', 'TALB', 'TAL'].includes(name)) apply(name, tagText(bytes.subarray(offset + header, offset + header + length)));
        offset += header + length;
      }
    }
  } else if (ascii(0, 4) === 'fLaC') {
    let offset = 4;
    while (offset + 4 <= bytes.length) {
      const kind = bytes[offset] & 0x7f;
      const last = Boolean(bytes[offset] & 0x80);
      const length = (bytes[offset + 1] << 16) | (bytes[offset + 2] << 8) | bytes[offset + 3];
      const end = offset + 4 + length;
      if (end > bytes.length) break;
      if (kind === 4 && length >= 8) {
        let p = offset + 4;
        const vendorLength = view.getUint32(p, true); p += 4 + vendorLength;
        if (p + 4 > end) break;
        const count = Math.min(view.getUint32(p, true), 1000); p += 4;
        for (let i = 0; i < count && p + 4 <= end; i++) {
          const size = view.getUint32(p, true); p += 4;
          if (p + size > end) break;
          const text = new TextDecoder().decode(bytes.subarray(p, p + size));
          const equal = text.indexOf('=');
          if (equal > 0) apply(text.slice(0, equal).toUpperCase(), cleanTag(text.slice(equal + 1)));
          p += size;
        }
      }
      offset = end;
      if (last) break;
    }
  } else if (ascii(0, 4) === 'RIFF' && ascii(8, 4) === 'WAVE') {
    let offset = 12;
    while (offset + 8 <= bytes.length) {
      const length = view.getUint32(offset + 4, true);
      const end = offset + 8 + length;
      if (end > bytes.length) break;
      if (ascii(offset, 4) === 'LIST' && ascii(offset + 8, 4) === 'INFO') {
        let p = offset + 12;
        while (p + 8 <= end) {
          const size = view.getUint32(p + 4, true);
          if (p + 8 + size > end) break;
          apply(ascii(p, 4), cleanTag(new TextDecoder().decode(bytes.subarray(p + 8, p + 8 + size))));
          p += 8 + size + (size & 1);
        }
      }
      offset = end + (length & 1);
    }
  }
  return tags;
}

async function probeAudio(blob: Blob, signal?: AbortSignal): Promise<number> {
  if (signal?.aborted) throw new Error('已取消下载');
  const url = URL.createObjectURL(blob);
  const audio = new Audio();
  audio.preload = 'auto';
  audio.muted = true;
  audio.volume = 0;
  try {
    return await new Promise<number>((resolve, reject) => {
      const timeout = window.setTimeout(() => finish(new Error('读取音频超时，请检查文件后重试')), 15_000);
      const cancel = () => finish(new Error('已取消下载'));
      const finish = (error?: Error) => {
        clearTimeout(timeout);
        signal?.removeEventListener('abort', cancel);
        audio.onloadeddata = null;
        audio.onerror = null;
        if (error) reject(error);
        else if (!Number.isFinite(audio.duration) || audio.duration <= 0) reject(new Error('无法读取这份音频的时长'));
        else resolve(audio.duration);
      };
      audio.onloadeddata = () => finish();
      audio.onerror = () => finish(new Error(audio.error?.code === 3 ? '音频文件损坏或编码不受支持' : '这份文件无法播放，请检查音频格式'));
      signal?.addEventListener('abort', cancel, { once: true });
      audio.src = url;
      audio.load();
    });
  } finally {
    audio.pause();
    audio.removeAttribute('src');
    audio.load();
    URL.revokeObjectURL(url);
  }
}

async function saveAudio(record: StoredAudio, blob: Blob, signal?: AbortSignal) {
  if (signal?.aborted) throw new Error('已取消下载');
  const db = await openDatabase();
  if (signal?.aborted) throw new Error('已取消下载');
  const transaction = db.transaction(['records', 'blobs'], 'readwrite');
  const complete = transactionComplete(transaction);
  const cancel = () => { try { transaction.abort(); } catch { /* The atomic commit already finished. */ } };
  signal?.addEventListener('abort', cancel, { once: true });
  try {
    transaction.objectStore('records').add(record);
    transaction.objectStore('blobs').add({ id: record.id, blob });
    await complete;
  } finally { signal?.removeEventListener('abort', cancel); }
  // This is best effort: denial is not a reason to claim the import failed.
  void navigator.storage?.persist?.().catch(() => false);
  await changed();
}

function localItem(id: string, format: string, details: Omit<Item, 'copies' | 'songId'>): Item {
  return {
    ...details,
    copies: [{ source: 'local', id, title: details.title, artists: details.artists, album: details.album,
      duration: details.duration, cover: details.cover, extra: { format } }],
  };
}

/** Never uploads imported audio or keeps references to the original file path. */
export async function importLocalAudio(files: File[]): Promise<{ added: number; duplicates: number; failed: string[] }> {
  const result = { added: 0, duplicates: 0, failed: [] as string[] };
  const existing = new Set((await localAudioRecords()).map(record => record.id));
  for (const file of files) {
    try {
      const format = file.name.split('.').pop()?.toLowerCase() ?? '';
      if (!AUDIO_TYPES[format]) throw new Error('不支持这种格式');
      if (!file.size) throw new Error('文件是空的');
      if (file.size > MAX_AUDIO_BYTES) throw new Error('文件超过 512 MB');
      const id = `import:${await fingerprint(file)}`;
      if (existing.has(id)) { result.duplicates++; continue; }
      const blob = file.type.startsWith('audio/') ? file : new Blob([file], { type: AUDIO_TYPES[format] });
      const duration = await probeAudio(blob);
      const tags = await readTags(blob).catch(() => ({} as AudioTags));
      const item = localItem(id, format, { title: tags.title || file.name.replace(/\.[^.]+$/, ''), artists: tags.artists || [],
        album: tags.album || '', duration, cover: '', liked: false });
      const record: StoredAudio = { id, origin: 'import', item, format, fileName: safeFileName(file.name), size: file.size,
        importedAt: new Date().toISOString(), liked: false };
      try { await saveAudio(record, blob); }
      catch (error) {
        if (error instanceof DOMException && error.name === 'ConstraintError') { result.duplicates++; existing.add(id); continue; }
        throw error;
      }
      existing.add(id);
      result.added++;
    } catch (error) { result.failed.push(`${file.name}：${message(error)}`); }
  }
  return result;
}

const subscribeDownloads = (listener: () => void) => {
  downloadListeners.add(listener);
  return () => { downloadListeners.delete(listener); };
};
const getDownloads = () => downloads;

export function useAudioDownloads() { return useSyncExternalStore(subscribeDownloads, getDownloads, getDownloads); }

function updateDownload(id: string, changes: Partial<AudioDownload>) {
  downloads = downloads.map(task => task.id === id ? { ...task, ...changes } : task);
  downloadListeners.forEach(listener => listener());
}

export function dismissAudioDownload(id: string) {
  if (downloadControllers.has(id)) return;
  downloads = downloads.filter(task => task.id !== id);
  downloadListeners.forEach(listener => listener());
}

export function cancelAudioDownload(id: string) { downloadControllers.get(id)?.abort(); }

function downloadKey(item: Item) {
  if (item.songId) return `song:${item.songId}`;
  return item.copies.map(copy => `${copy.source}:${copy.id}`).sort().join('|') || `${item.title}:${item.artists.join('/')}:${item.duration}`;
}

// Recommendations extend Item with display-only fields. The download API
// deliberately rejects unknown JSON properties, so send its public contract.
function downloadableItem(item: Item): Item {
  return {
    songId: item.songId,
    title: item.title,
    artists: item.artists,
    album: item.album,
    duration: item.duration,
    cover: item.cover,
    liked: item.liked,
    playedAt: item.playedAt,
    copies: item.copies.map(copy => ({
      source: copy.source,
      id: copy.id,
      title: copy.title,
      artists: copy.artists,
      album: copy.album,
      duration: copy.duration,
      cover: copy.cover,
      extra: copy.extra,
    })),
  };
}

function responseFormat(response: Response) {
  const header = response.headers.get('X-Husic-Audio-Format')?.toLowerCase().replace(/^\./, '') ?? '';
  if (AUDIO_TYPES[header]) return header;
  const mime = response.headers.get('Content-Type')?.toLowerCase().split(';')[0].trim() ?? '';
  const byMime: Record<string, string> = { 'audio/mpeg': 'mp3', 'audio/mp3': 'mp3', 'audio/wav': 'wav', 'audio/x-wav': 'wav',
    'audio/flac': 'flac', 'audio/x-flac': 'flac', 'audio/aac': 'aac', 'audio/aacp': 'aac', 'audio/mp4': 'm4a',
    'audio/x-m4a': 'm4a', 'audio/ogg': 'ogg', 'application/ogg': 'ogg', 'audio/webm': 'webm' };
  const format = byMime[mime];
  if (!format) throw new Error('服务器没有返回可保存的音频');
  return format;
}

export async function downloadAudio(item: Item): Promise<void> {
  if (localAudioID(item)) throw new Error('这首歌已经保存在本地，可以直接另存为');
  const key = downloadKey(item);
  if (downloadKeys.has(key)) throw new Error('这首歌正在下载');
  downloadKeys.add(key);
  const id = crypto.randomUUID();
  const controller = new AbortController();
  downloadControllers.set(id, controller);
  downloads = [{ id, item, state: 'downloading', received: 0, total: 0, error: '' }, ...downloads];
  downloadListeners.forEach(listener => listener());
  try {
    const db = await openDatabase();
    const records = await readRequest(db.transaction('records').objectStore('records').getAll()) as StoredAudio[];
    if (records.some(record => record.origin === 'download' && record.downloadKey === key)) throw new Error('这首歌已经在下载管理中');
    if (controller.signal.aborted) throw new Error('已取消下载');
    const response = await fetch('/api/download', { method: 'POST', credentials: 'same-origin', signal: controller.signal,
      headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ item: downloadableItem(item) }) });
    if (!response.ok) {
      const body = await response.json().catch(() => ({})) as { error?: string };
      throw new Error(body.error || (response.status === 401 ? '登录已过期，请重新连接后下载' : '这首歌暂时无法下载'));
    }
    const format = responseFormat(response);
    const rawLength = Number(response.headers.get('Content-Length'));
    const encoded = response.headers.get('Content-Encoding');
    const total = (!encoded || encoded === 'identity') && Number.isFinite(rawLength) && rawLength > 0 ? rawLength : 0;
    if (total > MAX_AUDIO_BYTES) throw new Error('这份音频超过 512 MB，暂时无法下载');
    if (!response.body) throw new Error('没有收到音频内容');
    updateDownload(id, { total });
    const reader = response.body.getReader();
    const chunks: Uint8Array<ArrayBuffer>[] = [];
    let received = 0;
    let lastUpdate = 0;
    try {
      while (true) {
        const part = await reader.read();
        if (part.done) break;
        received += part.value.byteLength;
        if (received > MAX_AUDIO_BYTES) { await reader.cancel(); throw new Error('这份音频超过 512 MB，下载已停止'); }
        chunks.push(new Uint8Array(part.value));
        if (performance.now() - lastUpdate > 120) { updateDownload(id, { received }); lastUpdate = performance.now(); }
      }
    } finally { reader.releaseLock(); }
    if (!received) throw new Error('没有收到音频内容');
    if (total && received !== total) throw new Error('音频没有下载完整，请重试');
    if (controller.signal.aborted) throw new Error('已取消下载');
    updateDownload(id, { state: 'saving', received, total: total || received });
    const blob = new Blob(chunks, { type: AUDIO_TYPES[format] });
    const duration = await probeAudio(blob, controller.signal);
    const recordID = `download:${await fingerprint(blob)}`;
    if (controller.signal.aborted) throw new Error('已取消下载');
    if (records.some(record => record.id === recordID)) throw new Error('这份音频已经在下载管理中');
    const recordItem = localItem(recordID, format, { title: item.title, artists: item.artists, album: item.album,
      cover: item.cover, duration, liked: item.liked });
    const fileName = safeFileName(`${item.artists.length ? `${item.artists.join(', ')} - ` : ''}${item.title}.${format}`);
    await saveAudio({ id: recordID, origin: 'download', item: recordItem, fileName, format, size: blob.size,
      importedAt: new Date().toISOString(), liked: item.liked, downloadKey: key }, blob, controller.signal);
    downloads = downloads.filter(task => task.id !== id);
    downloadListeners.forEach(listener => listener());
  } catch (error) {
    const cancelled = controller.signal.aborted;
    // Also release a response body when its headers, size or decoding failed.
    controller.abort();
    const text = cancelled ? '已取消下载' : message(error, '下载失败，请重试');
    updateDownload(id, { state: cancelled ? 'cancelled' : 'failed', error: text });
    throw new Error(text);
  } finally {
    downloadKeys.delete(key);
    downloadControllers.delete(id);
  }
}
