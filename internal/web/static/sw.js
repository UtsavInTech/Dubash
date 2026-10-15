// Keeps the OBS overlay loadable while Dubash is down. OBS often starts
// before the server, and a page that failed to load never runs the retry in
// viewer.js. Overlay pages and their assets come from the network when it
// answers and from the copy viewer.js saved otherwise; live endpoints
// (/api, /events, …) always go to the network so the page keeps retrying.
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (e) => e.waitUntil(self.clients.claim()));

self.addEventListener('fetch', (e) => {
  const url = new URL(e.request.url);
  if (e.request.method !== 'GET' || url.origin !== location.origin) return;
  if (!url.pathname.startsWith('/r/') && !url.pathname.startsWith('/static/')) return;
  e.respondWith(fetch(e.request).catch(async () =>
    (await caches.match(e.request, { ignoreSearch: true })) ?? Response.error()));
});
