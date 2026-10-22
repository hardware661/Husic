import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';

const source = await readFile(new URL('./chrome/background.js', import.meta.url), 'utf8');
const { toNetscape, handleRequest } = await import('data:text/javascript;base64,' + Buffer.from(source).toString('base64'));
const fixture = { domain: '.youtube.com', path: '/', name: 'SAPISID', value: 'private-fixture', secure: true, httpOnly: true, expirationDate: 9999999999 };
assert.match(toNetscape([fixture]), /#HttpOnly_\.youtube\.com/);
assert.doesNotMatch(toNetscape([fixture, { ...fixture, domain: 'accounts.google.com', value: 'foreign-fixture' }]), /foreign-fixture/);
assert.throws(() => toNetscape([{ ...fixture, domain: 'youtube.com.evil.test' }]));
assert.throws(() => toNetscape([{ ...fixture, value: 'bad\nvalue' }]));
const id = 'abcdefghijklmnopqrstuvwxyzABCDEF';
const sender = { url: 'http://127.0.0.1:18788/settings', frameId: 0, tab: { id: 9 } };
let storage = {}, allowed = false, windows = [], closed = [], fetched = 0;
const listeners = new Set();
globalThis.chrome = {
  storage: { session: { async get(key) { return { [key]: storage[key] }; }, async set(value) { Object.assign(storage, value); }, async remove(key) { delete storage[key]; } } },
  extension: { async isAllowedIncognitoAccess() { return allowed; } },
  windows: { async getAll() { return windows; }, async create() { return { id: 50, tabs: [{ id: 60 }] }; }, async remove(id) { closed.push(id); } },
  tabs: { async get() { return { url: 'https://music.youtube.com/' }; }, onUpdated: { addListener(fn) { listeners.add(fn); }, removeListener(fn) { listeners.delete(fn); } }, async update(tabId) { queueMicrotask(() => { for (const fn of listeners) fn(tabId, { status: 'complete' }); }); } },
  cookies: { async getAllCookieStores() { return [{ id: 'private', tabIds: [60] }]; }, async getAll(filter) { assert.equal(filter.storeId, 'private'); assert.equal(filter.domain, 'youtube.com'); fetched++; return [fixture]; } },
};
assert.equal((await handleRequest({ action: 'start', id }, sender)).error, 'incognito_permission');
allowed = true; windows = [{ id: 12, incognito: true }];
assert.equal((await handleRequest({ action: 'start', id }, sender)).error, 'private_window_open');
assert.equal(closed.length, 0);
windows = [];
assert.equal((await handleRequest({ action: 'start', id }, sender)).ok, true);
assert.equal((await handleRequest({ action: 'complete', id }, { ...sender, tab: { id: 99 } })).ok, false);
assert.equal(fetched, 0);
const complete = await handleRequest({ action: 'complete', id }, sender);
assert.equal(complete.ok, true); assert.match(complete.cookies, /private-fixture/); assert.deepEqual(closed, [50]);
assert.equal((await handleRequest({ action: 'complete', id }, sender)).ok, false);
assert.equal((await handleRequest({ action: 'start', id }, { ...sender, url: 'https://unrelated.test/' })).ok, false);

let receive, posted, sent = 0;
const window = { addEventListener(_name, handler) { receive = handler; }, postMessage(data) { posted = data; } };
window.top = window;
vm.runInNewContext(await readFile(new URL('./chrome/content.js', import.meta.url), 'utf8'), {
  window, location: { origin: 'http://127.0.0.1:18788' }, encodeURIComponent,
  chrome: { runtime: { async sendMessage() { sent++; return { ok: true, cookies: 'private-fixture' }; } } },
  async fetch(path, options) { assert.equal(path, `/api/accounts/youtube/login/${id}/complete`); assert.equal(options.credentials, 'same-origin'); assert.match(options.body, /private-fixture/); return { ok: true, async json() { return { id, kind: 'youtube', state: 'connected' }; } }; },
});
await receive({ source: window, origin: 'https://unrelated.test', data: { type: 'MUSIC_CONNECT_REQUEST', requestId: 'request', action: 'complete', id } });
assert.equal(sent, 0);
await receive({ source: window, origin: 'http://127.0.0.1:18788', data: { type: 'MUSIC_CONNECT_REQUEST', requestId: 'request', action: 'complete', id } });
assert.equal(posted.result.attempt.state, 'connected'); assert.doesNotMatch(JSON.stringify(posted), /private-fixture|cookies/);
const manifest = JSON.parse(await readFile(new URL('./chrome/manifest.json', import.meta.url), 'utf8'));
assert.deepEqual(manifest.permissions, ['cookies', 'storage']); assert.equal(manifest.host_permissions.includes('<all_urls>'), false);
console.log('PASS: domain scope, independent private window, owned-window-only cleanup, one-use export, same-origin transfer, no page credential exposure');
