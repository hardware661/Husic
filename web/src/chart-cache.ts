import { api, type Chart, type Item } from './api';

export type RankingData = { chart: Chart; updated: string; songs: Item[] };
const ttl = 10 * 60 * 1000;
const answers = new Map<string, { at: number; value: RankingData }>();
const pending = new Map<string, Promise<RankingData>>();
let catalog: Chart[] = [];
let catalogRequest: Promise<Chart[]> | null = null;
let warming: Promise<void> | null = null;
export const chartKey = (c: Chart) => `${c.source}:${c.id}`;
export const cachedCharts = () => catalog;
export const cachedRanking = (c: Chart) => answers.get(chartKey(c))?.value;

export function loadCharts() {
  if (catalog.length) return Promise.resolve(catalog);
  if (!catalogRequest) {
    catalogRequest = api.charts().then((r) => (catalog = r.charts)).finally(() => { catalogRequest = null; });
  }
  return catalogRequest;
}

export function prepareChartEntry() {
  void loadCharts().then((charts) => charts[0] ? loadRanking(charts[0]) : undefined).catch(() => {});
}

/** Main ranking, side preview and prefetch share the same in-flight answer.
 * Keep the last answer visible during a refresh; failures aren't cached. */
export function loadRanking(c: Chart): Promise<RankingData> {
  const key = chartKey(c);
  const hit = answers.get(key);
  if (hit && Date.now() - hit.at < ttl) return Promise.resolve(hit.value);
  const existing = pending.get(key);
  if (existing) return existing;
  const request = api.chart(c.source, c.id).then((value) => {
    answers.set(key, { at: Date.now(), value });
    return value;
  }).finally(() => { pending.delete(key); });
  pending.set(key, request);
  return request;
}

/** The four platform landing charts first, then the other editions. Only two
 * background requests at a time; selected charts never wait in this queue. */
export function warmRankings(charts: Chart[]) {
  if (warming) return warming;
  const first = charts.filter((c, i) => charts.findIndex((other) => other.source === c.source) === i);
  const queue = [...first, ...charts.filter((c) => !first.includes(c))];
  const worker = async () => {
    while (queue.length) {
      const c = queue.shift()!;
      try { await loadRanking(c); } catch { /* a click can retry this chart */ }
    }
  };
  warming = Promise.all([worker(), worker()]).then(() => {}).finally(() => { warming = null; });
  return warming;
}
