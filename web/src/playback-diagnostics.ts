import { useEffect } from 'react';
import type { Stream } from './api';

// Local, bounded and event-driven. No polling, network requests, track names,
// addresses, account data, user agent or device identifiers enter this report.
const key = 'husic-playback-diagnostics-v1';
const events = ['observer', 'stream', 'loadstart', 'loadedmetadata', 'playing', 'pause', 'waiting', 'stalled', 'error', 'ended', 'seeking', 'seeked', 'suspend', 'resume', 'export'];
const formats = ['mp3', 'm4a', 'mp4', 'aac', 'flac', 'wav', 'ogg', 'opus', 'webm'];
const sources = ['netease', 'qq', 'kuwo', 'kugou', 'migu', 'youtube', 'bilibili', 'local'];
type Entry = { at: number; event: string; hidden: boolean; surface: string; paused: boolean; position: number; duration: number; ready: number; network: number; error: number; format: string; bitrate: number; source: string };
const number = (value: unknown) => typeof value === 'number' && Number.isFinite(value) ? Math.max(0, Math.round(value * 10) / 10) : 0;
const oneOf = (value: unknown, allowed: string[]) => typeof value === 'string' && allowed.includes(value) ? value : '';
const clean = (value: Partial<Entry>): Entry => ({ at: number(value.at), event: oneOf(value.event, events), hidden: value.hidden === true,
  surface: oneOf(value.surface, ['none', 'visible', 'not-mounted']), paused: value.paused === true, position: number(value.position), duration: number(value.duration),
  ready: number(value.ready), network: number(value.network), error: number(value.error), format: oneOf(value.format, formats), bitrate: number(value.bitrate), source: oneOf(value.source, sources) });
let entries: Entry[] = [];
try { const saved: unknown = JSON.parse(localStorage.getItem(key) || '[]'); if (Array.isArray(saved)) entries = saved.filter(v => v && typeof v === 'object').slice(-32).map(clean); } catch { /* Diagnostics never block playback. */ }
let audio: HTMLAudioElement | null = null;
let stream = { format: '', bitrate: 0, source: '' };
let surface = 'not-mounted', lastWrite = 0;
const counts: Record<string, number> = { backgroundTimeUpdate: 0 };
const sessionStartedAt = Date.now();
let sleepTimerActive = false;
export function noteSleepTimerActive(active: boolean) { sleepTimerActive = active; }
export function noteBackgroundMediaCallback() {
  if (document.hidden || document.documentElement.hasAttribute('data-page-hidden')) counts.backgroundTimeUpdate++;
}
function snapshot(event: string): Entry {
  return clean({ at: Date.now(), event, hidden: document.hidden || document.documentElement.hasAttribute('data-page-hidden'), surface,
    paused: audio?.paused ?? true, position: audio?.currentTime, duration: audio?.duration, ready: audio?.readyState, network: audio?.networkState, error: audio?.error?.code, ...stream });
}
function record(event: string, flush = false) {
  counts[event] = (counts[event] || 0) + 1;
  entries.push(snapshot(event)); if (entries.length > 32) entries.shift();
  if (!flush && Date.now() - lastWrite < 2000) return;
  lastWrite = Date.now();
  try { localStorage.setItem(key, JSON.stringify(entries)); } catch { /* A full device can still play. */ }
}
export function notePresentation(visible: boolean) {
  const frame = document.querySelector('.frame');
  surface = frame ? getComputedStyle(frame).display === 'none' ? 'none' : 'visible' : 'not-mounted';
  record(visible ? 'resume' : 'suspend', true);
}
export function usePlaybackDiagnostics(element: HTMLAudioElement, value: Stream | null) {
  useEffect(() => {
    audio = element;
    const recordEvent = (event: Event) => record(event.type);
    const media = events.filter(event => !['observer', 'stream', 'suspend', 'resume', 'export'].includes(event));
    media.forEach(event => element.addEventListener(event, recordEvent)); record('observer');
    return () => { media.forEach(event => element.removeEventListener(event, recordEvent)); if (audio === element) audio = null; };
  }, [element]);
  useEffect(() => {
    if (!value) return;
    const first = !stream.format;
    stream = { format: oneOf(typeof value.format === 'string' ? value.format.toLowerCase() : '', formats), bitrate: number(value.bitrate), source: oneOf(value.source, sources) };
    record('stream', first);
  }, [value]);
}
export function playbackDiagnostics() {
  return JSON.stringify({ schema: 1, build: '20261023-native-audio-control', sessionStartedAt, encodingEvidence: 'declared-not-probed', sleepTimerActive, current: snapshot('export'), counts: { ...counts }, recent: entries.slice(-32) });
}
