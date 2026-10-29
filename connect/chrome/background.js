const origin = 'http://127.0.0.1:18788';
const key = 'husic-youtube-attempt';
const validID = (id) => typeof id === 'string' && /^[A-Za-z0-9_-]{32}$/.test(id);
const isYouTube = (domain) => domain === 'youtube.com' || domain.endsWith('.youtube.com');

export function toNetscape(cookies) {
  const rows = [];
  let authenticated = false;
  for (const cookie of cookies) {
    if (!isYouTube(cookie.domain.replace(/^\./, '')) || cookie.partitionKey) continue;
    if ([cookie.domain, cookie.path, cookie.name, cookie.value].some((value) => typeof value !== 'string' || /[\r\n\t]/.test(value))) continue;
    const expires = Math.trunc(cookie.expirationDate || 0);
    if (expires && expires <= Date.now() / 1000) continue;
    if (cookie.name === 'SAPISID' && cookie.value) authenticated = true;
    rows.push([(cookie.httpOnly ? '#HttpOnly_' : '') + cookie.domain, cookie.domain.startsWith('.') ? 'TRUE' : 'FALSE', cookie.path, cookie.secure ? 'TRUE' : 'FALSE', expires, cookie.name, cookie.value].join('\t'));
  }
  if (!authenticated) throw new Error('login_incomplete');
  return '# Netscape HTTP Cookie File\n' + rows.join('\n') + '\n';
}

async function active() { return (await chrome.storage.session.get(key))[key]; }
async function closeOwn(attempt) {
  if (!attempt) return;
  // Only IDs created by this extension are ever removed.
  try { await chrome.windows.remove(attempt.windowId); } catch { /* The user may have closed it. */ }
  await chrome.storage.session.remove(key);
}

async function staticPage(tabId) {
  await new Promise((resolve, reject) => {
    const finish = (error) => { clearTimeout(timer); chrome.tabs.onUpdated.removeListener(listener); error ? reject(error) : resolve(); };
    const listener = (id, info) => { if (id === tabId && info.status === 'complete') finish(); };
    const timer = setTimeout(() => finish(new Error('transfer_failed')), 10000);
    chrome.tabs.onUpdated.addListener(listener);
    chrome.tabs.update(tabId, { url: 'https://www.youtube.com/robots.txt' }).catch(() => finish(new Error('transfer_failed')));
  });
}

export async function handleRequest(message, sender) {
  if (!sender.tab || sender.frameId !== 0 || new URL(sender.url).origin !== origin) return { ok: false, error: 'transfer_failed' };
  if (message.action === 'ping') return { ok: true };
  if (!validID(message.id)) return { ok: false, error: 'transfer_failed' };
  const previous = await active();
  if (message.action === 'cancel') {
    if (previous?.id === message.id && previous.sourceTabId === sender.tab.id) await closeOwn(previous);
    return { ok: true };
  }
  if (message.action === 'start') {
    if (!await chrome.extension.isAllowedIncognitoAccess()) return { ok: false, error: 'incognito_permission' };
    if (previous) await closeOwn(previous);
    if ((await chrome.windows.getAll()).some((window) => window.incognito)) return { ok: false, error: 'private_window_open' };
    try {
      const window = await chrome.windows.create({
        incognito: true, type: 'popup', width: 920, height: 760,
        url: 'https://accounts.google.com/ServiceLogin?service=youtube&continue=https%3A%2F%2Fmusic.youtube.com%2F',
      });
      if (!window.id || !window.tabs?.[0]?.id) return { ok: false, error: 'window_failed' };
      await chrome.storage.session.set({ [key]: { id: message.id, windowId: window.id, tabId: window.tabs[0].id, sourceTabId: sender.tab.id, expires: Date.now() + 5 * 60 * 1000 } });
      return { ok: true };
    } catch { return { ok: false, error: 'window_failed' }; }
  }
  if (message.action !== 'complete' || !previous || previous.id !== message.id || previous.sourceTabId !== sender.tab.id || previous.expires < Date.now()) return { ok: false, error: 'login_window_closed' };
  let tab;
  try { tab = await chrome.tabs.get(previous.tabId); } catch { return { ok: false, error: 'login_window_closed' }; }
  if (!tab.url?.startsWith('https://music.youtube.com/') && !tab.url?.startsWith('https://www.youtube.com/')) return { ok: false, error: 'login_incomplete' };
  await staticPage(previous.tabId);
  const store = (await chrome.cookies.getAllCookieStores()).find((item) => item.tabIds.includes(previous.tabId));
  if (!store) return { ok: false, error: 'login_incomplete' };
  const cookies = toNetscape(await chrome.cookies.getAll({ domain: 'youtube.com', storeId: store.id }));
  await closeOwn(previous);
  // This reply goes only to our isolated content script, never to page JS.
  return { ok: true, cookies };
}

if (globalThis.chrome?.runtime?.onMessage) {
  chrome.runtime.onMessage.addListener((message, sender, respond) => {
    if (message?.type !== 'husic-connect') return;
    handleRequest(message, sender).then(respond).catch((error) => respond({ ok: false, error: ['login_incomplete', 'transfer_failed'].includes(error?.message) ? error.message : 'transfer_failed' }));
    return true;
  });
  chrome.action.onClicked.addListener(() => chrome.tabs.create({ url: origin + '/settings' }));
}
