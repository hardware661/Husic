(() => {
  'use strict';
  if (window !== window.top || location.origin !== __MUSIC_ORIGIN__ || !window.chrome?.webview) return;
  const bridge = window.chrome.webview;
  const handlers = new Map();
  const session = navigator.mediaSession;
  let previous = '';
  let previousArtwork = '', artworkGeneration = 0;
  const publishArtwork = (metadata) => {
    const src = metadata?.artwork?.[0]?.src || '';
    const title = String(metadata?.title || '').slice(0, 160);
    const identity = `${title}\n${src}`;
    if (identity === previousArtwork) return;
    previousArtwork = identity;
    const generation = ++artworkGeneration;
    bridge.postMessage({ type: 'music-artwork', title, dataUrl: null });
    if (!src || !/^https:\/\//.test(src)) return;
    const image = new Image();
    image.crossOrigin = 'anonymous';
    image.onload = () => {
      if (generation !== artworkGeneration) return;
      try {
        const canvas = document.createElement('canvas'); canvas.width = canvas.height = 64;
        const context = canvas.getContext('2d');
        const edge = Math.min(image.naturalWidth, image.naturalHeight);
        context.drawImage(image, (image.naturalWidth - edge) / 2, (image.naturalHeight - edge) / 2, edge, edge, 0, 0, 64, 64);
        const dataUrl = canvas.toDataURL('image/png');
        if (dataUrl.length < 50000) bridge.postMessage({ type: 'music-artwork', title, dataUrl });
      } catch { /* A CDN without CORS keeps the brand fallback; no native URL fetch. */ }
    };
    image.src = src;
  };
  const publish = () => {
    const metadata = session?.metadata;
    const state = {
      type: 'music-status',
      title: String(metadata?.title || '').slice(0, 160),
      artist: String(metadata?.artist || '').slice(0, 160),
      playing: session?.playbackState === 'playing',
      ready: Boolean(metadata && handlers.has('play') && handlers.has('pause')),
      dark: document.documentElement.dataset.theme === 'dark',
      reduced: document.documentElement.dataset.motion === 'reduced' || matchMedia('(prefers-reduced-motion: reduce)').matches,
    };
    const value = JSON.stringify(state);
    if (value !== previous) { previous = value; bridge.postMessage(state); }
    publishArtwork(metadata);
  };
  if (session) {
    const setActionHandler = session.setActionHandler.bind(session);
    session.setActionHandler = (action, handler) => {
      const result = setActionHandler(action, handler);
      if (handler) handlers.set(action, handler); else handlers.delete(action);
      queueMicrotask(publish);
      return result;
    };
    for (const name of ['metadata', 'playbackState']) {
      const descriptor = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(session), name);
      if (!descriptor?.get || !descriptor.set) continue;
      Object.defineProperty(session, name, {
        configurable: true,
        get() { return descriptor.get.call(session); },
        set(value) { descriptor.set.call(session, value); queueMicrotask(publish); },
      });
    }
  }
  bridge.addEventListener('message', event => {
    const value = event.data;
    if (value?.type !== 'music-desktop-control') return;
    let action = value.action;
    if (action === 'toggle') action = session?.playbackState === 'playing' ? 'pause' : 'play';
    if (!['play', 'pause', 'previoustrack', 'nexttrack'].includes(action)) return;
    // The existing Music play/pause handlers both toggle; duplicate requests
    // must not turn a requested pause back into play.
    if (action === 'pause' && session?.playbackState !== 'playing') return;
    if (action === 'play' && session?.playbackState === 'playing') return;
    const handler = handlers.get(action);
    if (handler) { handler({ action }); queueMicrotask(publish); }
  });
  // Renew YouTube through the already-working Chrome helper. No browser
  // cookies, arbitrary URLs or native functions are exposed to page code.
  document.addEventListener('click', event => {
    const button = event.target instanceof Element ? event.target.closest('button') : null;
    const row = button?.closest('.setting-row');
    if (event.isTrusted && row?.querySelector('strong')?.textContent === 'YouTube Music' && button.textContent === '重新连接') {
      event.preventDefault(); event.stopImmediatePropagation();
      bridge.postMessage({ type: 'open-youtube-settings' });
    }
  }, true);
  const observeTheme = () => {
    new MutationObserver(publish).observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme', 'data-motion'] });
    publish();
  };
  if (document.documentElement) observeTheme(); else document.addEventListener('DOMContentLoaded', observeTheme, { once: true });
  // Only changed, bounded playback metadata is sent. No cookie/storage/API
  // data is read, and blind-listening uses Music's concealed MediaMetadata.
  window.setInterval(publish, 1000);
})();
