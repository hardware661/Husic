import { useSyncExternalStore } from 'react';

let snapshot = { url: '', name: '' };
const listeners = new Set<() => void>();
let generation = 0;
function database(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open('husic-appearance', 1);
    request.onupgradeneeded = () => request.result.createObjectStore('background');
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(new Error('这台设备暂时不能保存背景图片'));
  });
}
async function store(record: { blob: Blob; name: string } | null) {
  const db = await database();
  try {
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction('background', 'readwrite');
      if (record) tx.objectStore('background').put(record, 'image');
      else tx.objectStore('background').delete('image');
      tx.oncomplete = () => resolve();
      tx.onabort = tx.onerror = () => reject(new Error('背景没有保存，可能是设备存储空间不足'));
    });
  } finally {
    db.close();
  }
}
function display(record: { blob: Blob; name: string } | null) {
  if (snapshot.url) URL.revokeObjectURL(snapshot.url);
  snapshot = record ? { url: URL.createObjectURL(record.blob), name: record.name } : { url: '', name: '' };
  document.documentElement.dataset.wallpaper = String(Boolean(record));
  document.documentElement.style.setProperty('--wallpaper', record ? `url("${snapshot.url}")` : 'none');
  listeners.forEach((listener) => listener());
}
export async function setBackground(file: File | null) {
  const sequence = ++generation;
  if (!file) {
    await store(null);
    display(null);
    return;
  }
  if (!['image/jpeg', 'image/png', 'image/webp', 'image/avif'].includes(file.type) || file.size > 10 * 1024 * 1024)
    throw new Error('请选择 10 MB 以内的 JPG、PNG、WebP 或 AVIF 图片');
  const url = URL.createObjectURL(file);
  try {
    const image = new Image();
    image.src = url;
    await image.decode();
    if (!image.naturalWidth || image.naturalWidth * image.naturalHeight > 40000000) throw new Error('这张图片尺寸太大，请换一张');
    const ratio = Math.min(1, 2560 / Math.max(image.naturalWidth, image.naturalHeight));
    const canvas = document.createElement('canvas');
    canvas.width = Math.max(1, Math.round(image.naturalWidth * ratio));
    canvas.height = Math.max(1, Math.round(image.naturalHeight * ratio));
    const context = canvas.getContext('2d');
    if (!context) throw new Error('暂时无法读取这张图片');
    context.fillStyle = '#ffffff';
    context.fillRect(0, 0, canvas.width, canvas.height);
    context.drawImage(image, 0, 0, canvas.width, canvas.height);
    const blob = await new Promise<Blob>((resolve, reject) =>
      canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('暂时无法读取这张图片'))), 'image/jpeg', 0.87),
    );
    if (sequence !== generation) return;
    const record = { blob, name: file.name.slice(0, 120) };
    await store(record);
    if (sequence === generation) display(record);
  } catch (error) {
    throw error instanceof Error ? error : new Error('这张图片无法打开，请换一张');
  } finally {
    URL.revokeObjectURL(url);
  }
}
export function useBackground() {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    () => snapshot,
  );
}
const initialGeneration = generation;
void database()
  .then(async (db) => {
    try {
      const record = await new Promise<{ blob: Blob; name: string } | undefined>((resolve, reject) => {
        const r = db.transaction('background').objectStore('background').get('image');
        r.onsuccess = () => resolve(r.result);
        r.onerror = () => reject(r.error);
      });
      if (
        initialGeneration === generation &&
        record?.blob instanceof Blob &&
        record.blob.size <= 10 * 1024 * 1024 &&
        record.blob.type === 'image/jpeg'
      )
        display(record);
    } finally {
      db.close();
    }
  })
  .catch(() => {});
