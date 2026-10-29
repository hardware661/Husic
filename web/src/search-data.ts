import { api, type Item } from './api';
const cache = new Map<string, { at: number; pending: Promise<{ songs: Item[] }> }>();
const suggestionCache=new Map<string,{at:number;songs:Item[]}>();
export async function searchSuggestions(query:string,signal:AbortSignal){
  const key=query.trim().toLocaleLowerCase(),hit=suggestionCache.get(key);
  if(hit&&Date.now()-hit.at<60000)return hit.songs;
  const response=await fetch(`/api/suggestions?q=${encodeURIComponent(query.trim())}`,{signal});
  if(!response.ok)throw new Error('搜索建议暂不可用');
  const result=await response.json() as {songs:Item[]};
  if(!signal.aborted){suggestionCache.set(key,{at:Date.now(),songs:result.songs});if(suggestionCache.size>32)suggestionCache.delete(suggestionCache.keys().next().value!);}
  return result.songs;
}
export function searchMusic(query: string) {
  const key = query.trim().toLocaleLowerCase();
  const hit = cache.get(key);
  if (hit && Date.now() - hit.at < 180000) return hit.pending;
  const pending = api.search(query.trim()).catch(error => { cache.delete(key); throw error; });
  cache.set(key, { at: Date.now(), pending });
  if (cache.size > 16) cache.delete(cache.keys().next().value!);
  return pending;
}
const historyKey = 'husic-search-history';
export function searchHistory(): string[] {
  try { const value=JSON.parse(localStorage.getItem(historyKey)||localStorage.getItem('music-search-history')||'[]');return Array.isArray(value)?value.filter((s):s is string=>typeof s==='string'&&s.length<=96).slice(0,8):[]; } catch { return []; }
}
export function rememberSearch(q: string) {
  try { localStorage.setItem(historyKey,JSON.stringify([q.slice(0,96),...searchHistory().filter(s=>s!==q)].slice(0,8))); } catch {}
}
export function removeSearch(q?: string) {
  try {
    if(q===undefined){localStorage.removeItem(historyKey);localStorage.removeItem('music-search-history');}
    else localStorage.setItem(historyKey,JSON.stringify(searchHistory().filter(s=>s!==q)));
  } catch {}
}
export function openSearch() { window.dispatchEvent(new Event('husic-open-search')); }
