// Intentionally independent of the app bundle. No React, Media Session API,
// artwork, polling, animation callbacks, queue resolution or persistent logs.
(() => {
  'use strict';
  const audio = document.querySelector('audio');
  const start = document.getElementById('start');
  const status = document.getElementById('status');
  const copy = document.getElementById('copy');
  const key = 'husic-native-audio-handoff-v1';
  const sessionStartedAt = Date.now();
  const number = value => typeof value === 'number' && Number.isFinite(value) ? Math.max(0, Math.round(value * 10) / 10) : 0;
  const oneOf = (value, allowed) => typeof value === 'string' && allowed.includes(value) ? value : '';
  let handoff;
  try {
    const raw = sessionStorage.getItem(key);
    // Consume before parsing, including expired/malformed handoffs. The signed
    // address lives only in this media element after the transition.
    sessionStorage.removeItem(key);
    const value = JSON.parse(raw || 'null');
    if (!value || typeof value.url !== 'string' || value.url.length > 16384 ||
        !Number.isFinite(value.expires) || value.expires < Date.now() || value.expires > Date.now() + 5 * 60_000) throw new Error('invalid handoff');
    const url = new URL(value.url, location.origin);
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password ||
        location.protocol === 'https:' && url.protocol !== 'https:') throw new Error('invalid source');
    handoff = { format: value.format, bitrate: value.bitrate, source: value.source };
    const position = number(value.position);
    audio.volume = typeof value.volume === 'number' && Number.isFinite(value.volume) ? Math.max(0, Math.min(1, value.volume)) : 1;
    audio.muted = value.muted === true;
    audio.addEventListener('loadedmetadata', () => {
      if (Number.isFinite(audio.duration) && audio.duration > 0) audio.currentTime = Math.min(position, Math.max(0, audio.duration - .05));
    }, { once: true });
    audio.src = url.href;
    start.disabled = false;
    copy.disabled = false;
    status.textContent = '原播放器已停止。点播放开始，返回后可继续使用原来的队列。';
  } catch {
    status.textContent = '请返回 Husic，播放一首在线歌曲后重新进入对照。';
  }
  audio.addEventListener('play', () => { start.hidden = true; }, { once: true });
  audio.addEventListener('error', () => { status.textContent = '这条音频暂时无法直接播放，请返回 Husic。'; });
  start.addEventListener('click', () => {
    audio.play().catch(() => { status.textContent = '暂时没能开始播放，请用上方音频控件重试。'; });
  });
  const release = () => { audio.pause(); audio.removeAttribute('src'); audio.load(); };
  document.getElementById('back').addEventListener('click', release);
  window.addEventListener('pagehide', release);
  copy.addEventListener('click', async () => {
    if (!handoff) return;
    const report = {
      schema: 1, build: '20261023-native-isolated', mode: 'native-control', sessionStartedAt,
      scriptPolicy: 'same-origin-only',
      encodingEvidence: 'declared-not-probed',
      current: { at: Date.now(), hidden: document.hidden, paused: audio.paused, position: number(audio.currentTime),
        duration: number(audio.duration), ready: audio.readyState, network: audio.networkState, error: audio.error?.code || 0,
        format: oneOf(handoff.format, ['mp3', 'm4a', 'mp4', 'aac', 'flac', 'wav', 'ogg', 'opus', 'webm']),
        bitrate: number(handoff.bitrate), source: oneOf(handoff.source, ['netease', 'qq', 'kuwo', 'kugou', 'migu', 'youtube', 'bilibili']) },
    };
    try {
      await navigator.clipboard.writeText(JSON.stringify(report));
      status.textContent = '对照诊断已复制，不包含歌曲、地址或账号信息。';
    } catch {
      status.textContent = '复制没有完成，请重试。';
    }
  });
})();
