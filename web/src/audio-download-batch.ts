import { useSyncExternalStore } from 'react';
import { type Item } from './api';
import { downloadAudio, downloadKey, isAudioDownloading, localAudioID, localAudioRecords } from './local-audio';

export type AudioDownloadBatch = {
  id: string; name: string; total: number; finished: number; downloaded: number; skipped: number; failed: number;
  state: 'running' | 'cancelling' | 'cancelled' | 'completed' | 'failed'; error?: string;
};

let batch: AudioDownloadBatch | null = null;
let controller: AbortController | null = null;
const listeners = new Set<() => void>();
const notify = () => listeners.forEach(listener => listener());
const subscribe = (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; };
const snapshot = () => batch;
export const batchIsRunning = (value: AudioDownloadBatch | null) => value?.state === 'running' || value?.state === 'cancelling';
export const useAudioDownloadBatch = () => useSyncExternalStore(subscribe, snapshot, snapshot);

function update(id: string, changes: Partial<AudioDownloadBatch>) {
  if (batch?.id !== id) return;
  batch = { ...batch, ...changes }; notify();
}

/** One bounded transfer at a time, independent of the current page/player. */
async function run(id: string, items: Item[], signal: AbortSignal) {
  let finished = 0, downloaded = 0, skipped = 0, failed = 0;
  try {
    const saved = new Set((await localAudioRecords()).map(record => record.downloadKey).filter(Boolean));
    const seen = new Set<string>();
    for (const item of items) {
      if (signal.aborted) break;
      const key = downloadKey(item);
      if (localAudioID(item) || seen.has(key) || saved.has(key) || isAudioDownloading(item)) skipped++;
      else {
        try { await downloadAudio(item, signal); downloaded++; saved.add(key); }
        catch (error) {
          if (signal.aborted) break;
          failed++;
          // Do not keep spending bandwidth once this device cannot save files.
          if (error instanceof Error && error.cause instanceof DOMException && error.cause.name === 'QuotaExceededError') {
            update(id, { finished: finished + 1, downloaded, skipped, failed, state: 'failed', error: '本机存储空间不足，请清理空间后重新下载' });
            return;
          }
        }
      }
      seen.add(key); finished++;
      update(id, { finished, downloaded, skipped, failed });
    }
    update(id, { state: signal.aborted ? 'cancelled' : 'completed' });
  } catch (error) {
    update(id, { state: signal.aborted ? 'cancelled' : 'failed', error: error instanceof Error ? error.message : '无法读取本机音乐库' });
  } finally { if (batch?.id === id) controller = null; }
}

export function startAudioDownloadBatch(items: Item[], name: string): boolean {
  if (!items.length || batchIsRunning(batch)) return false;
  controller = new AbortController();
  batch = { id: crypto.randomUUID(), name, total: items.length, finished: 0, downloaded: 0, skipped: 0, failed: 0, state: 'running' };
  notify();
  void run(batch.id, [...items], controller.signal);
  return true;
}

export function cancelAudioDownloadBatch() {
  if (!batch || !batchIsRunning(batch)) return;
  update(batch.id, { state: 'cancelling' }); controller?.abort();
}

export function dismissAudioDownloadBatch() {
  if (batchIsRunning(batch)) return;
  batch = null; notify();
}
