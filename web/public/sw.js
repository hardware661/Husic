// Retire the old G1 lab's offline shell after this origin was reused by Music.
// Do not remove Cache Storage or IndexedDB: existing downloads stay untouched.
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (event) => {
  event.waitUntil((async () => {
    await self.clients.claim();
    await self.registration.unregister();
    const pages = await self.clients.matchAll({ type: 'window' });
    await Promise.allSettled(pages.map((page) => page.navigate(page.url)));
  })());
});
