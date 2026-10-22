(() => {
  const origin = 'http://127.0.0.1:18788';
  if (location.origin !== origin || window.top !== window) return;
  window.addEventListener('message', async (event) => {
    const message = event.data;
    if (event.source !== window || event.origin !== origin || message?.type !== 'MUSIC_CONNECT_REQUEST' || typeof message.requestId !== 'string') return;
    if (!['ping', 'start', 'complete', 'cancel'].includes(message.action)) return;
    let result;
    try {
      const reply = await chrome.runtime.sendMessage({ type: 'music-connect', action: message.action, id: message.id });
      if (reply?.ok && message.action === 'complete' && typeof reply.cookies === 'string') {
        // Same-origin fetch retains Music's existing login gateway. Cookies
        // stay in the isolated extension world and are never posted to page JS.
        const response = await fetch(`/api/accounts/youtube/login/${encodeURIComponent(message.id)}/complete`, {
          method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ cookies: reply.cookies }),
        });
        const attempt = await response.json();
        result = response.ok ? { ok: true, attempt: { id: attempt.id, kind: attempt.kind, state: attempt.state, expires: attempt.expires, message: attempt.message } } : { ok: false, error: 'transfer_failed' };
      } else { result = { ok: !!reply?.ok, error: reply?.error }; }
    } catch { result = { ok: false, error: 'transfer_failed' }; }
    window.postMessage({ type: 'MUSIC_CONNECT_RESPONSE', requestId: message.requestId, result }, origin);
  });
})();
