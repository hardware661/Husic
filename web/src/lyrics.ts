import { api, refOf, type Item, type Lyrics } from './api';
import { localAudioID } from './local-audio';

// The full player and native lyric window share requests, including in-flight
// ones. Keep this small: lyrics aren't another persistent music library.
const cache = new Map<string, Promise<Lyrics>>();
export function getLyrics(item: Item): Promise<Lyrics> {
  if (localAudioID(item)) return Promise.resolve({ lines: [], synced: false });
  const key = JSON.stringify([item.songId, item.copies.map(refOf)]);
  const existing = cache.get(key);
  if (existing) return existing;
  const request = api.lyrics(item).catch((error) => { cache.delete(key); throw error; });
  cache.set(key, request);
  if (cache.size > 24) cache.delete(cache.keys().next().value!);
  return request;
}
