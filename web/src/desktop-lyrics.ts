import { useEffect, useState } from 'react';
import { type Item, type Lyrics } from './api';
import { getLyrics } from './lyrics';

type NativeBridge = {
  postMessage: (message: unknown) => void;
  addEventListener: (name: 'message', listener: (event: MessageEvent) => void) => void;
  removeEventListener: (name: 'message', listener: (event: MessageEvent) => void) => void;
};
const native = () => (window as Window & { chrome?: { webview?: NativeBridge } }).chrome?.webview;

/** Text-only companion to the existing Audio, never a second playback path. */
export function useDesktopLyrics(item: Item | null, audio: HTMLAudioElement, concealed: boolean) {
  const [enabled, setEnabled] = useState(false);
  useEffect(() => {
    const bridge = native();
    if (!bridge) return;
    const receive = (event: MessageEvent) => {
      if (event.data?.type === 'husic-desktop-state') setEnabled(event.data.lyricsEnabled === true);
    };
    bridge.addEventListener('message', receive);
    bridge.postMessage({ type: 'desktop-shell-ready' });
    return () => bridge.removeEventListener('message', receive);
  }, []);

  useEffect(() => {
    const bridge = native();
    if (!bridge || !enabled) return;
    let cancelled = false, lyrics: Lyrics | null = null, failed = false, previous = '';
    const publish = () => {
      if (cancelled) return;
      let line = '选一首喜欢的歌', secondary = '', state = 'idle';
      if (concealed) { line = '先听，再看是谁'; secondary = ''; state = 'concealed'; }
      else if (item) {
        line = item.title;
        if (!lyrics && !failed) { secondary = '正在找歌词…'; state = 'loading'; }
        else if (!lyrics?.lines.length) { secondary = failed ? '歌词暂时没连上，音乐照常播放' : '这首歌暂时没有歌词'; state = 'empty'; }
        else if (!lyrics.synced) { secondary = '这首歌的歌词暂不支持滚动'; state = 'plain'; }
        else {
          const ms = audio.currentTime * 1000 + 120;
          let index = -1;
          for (let i = 0; i < lyrics.lines.length && lyrics.lines[i].t <= ms; i++) index = i;
          const active = lyrics.lines[index], next = lyrics.lines[index + 1];
          line = active?.text.trim() || '♪';
          secondary = active?.tr?.trim() || next?.text.trim() || '';
          state = 'synced';
        }
      }
      const message = { type: 'husic-lyrics', line: line.slice(0, 500), secondary: secondary.slice(0, 500), state };
      const value = JSON.stringify(message);
      if (value !== previous) { previous = value; bridge.postMessage(message); }
    };
    // Concealed sessions must not even request identifying lyric content.
    publish();
    if (item && !concealed) getLyrics(item).then((value) => { lyrics = value; publish(); }).catch(() => { failed = true; publish(); });
    const events = ['timeupdate', 'seeked', 'seeking', 'play', 'pause', 'emptied'];
    events.forEach((event) => audio.addEventListener(event, publish));
    // Works while the main window is hidden (rAF would stop there).
    const timer = window.setInterval(publish, 200);
    return () => { cancelled = true; clearInterval(timer); events.forEach((event) => audio.removeEventListener(event, publish)); };
  }, [item, audio, concealed, enabled]);
}
