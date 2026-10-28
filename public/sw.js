/* Student Hub service worker. Hand-written, no libraries, no build step: read it top to bottom.
   What it does and the rules it follows are described in docs/PWA.md. In short:
   - pages (the app shell; no personal data is in the HTML, screens fill in from localStorage): network first,
     the saved copy when there is no network, the server is slow (> NETWORK_TIMEOUT_MS) or answers 5xx;
     a page never saved -> /offline.html instead of the browser's error page
   - /_next/static/* (fingerprinted build files): cache first
   - fonts, icons, the campus map, the manifest: the saved copy at once, refreshed in the background
   - NEVER cached: /api/* (not even touched), /p/* (public portfolio, no-store), router data requests (RSC),
     anything sent with an Authorization header, non-GET requests, other sites except Google Fonts
   Versions: VERSION (below) for changes to this file's logic, BUILD (from ?v=, set per deploy by the page) for
   everything else. Old caches are deleted when a new worker activates. A new worker waits until the student
   taps «Жаңарту» (message SKIP_WAITING); it never takes over a page on its own. */
'use strict';

const VERSION = 'v2';
const BUILD = new URL(self.location.href).searchParams.get('v') || 'dev';
const PREFIX = 'studenthub-';
const PAGES = `${PREFIX}pages-${VERSION}-${BUILD}`;
const STATIC = `${PREFIX}static-${VERSION}-${BUILD}`;
const CURRENT = [PAGES, STATIC];

const OFFLINE_URL = '/offline.html';
/** saved on install: the screens that must open without internet (schedule is on «/», the map on «/map») */
const SHELL_PAGES = ['/', '/map', '/settings', '/roadmap', '/profile', '/mine', '/events', '/search', '/notifications', '/saved', '/services'];
/** saved on install (all must exist: a missing one fails the install, and the old worker stays) */
const STATIC_FILES = [
  OFFLINE_URL, '/manifest.webmanifest', '/campus-map.jpg', '/favicon.ico',
  '/icons/icon.svg', '/icons/icon-32.png', '/icons/icon-180.png', '/icons/icon-192.png', '/icons/icon-512.png', '/icons/icon-maskable-512.png',
];
const NETWORK_TIMEOUT_MS = 4000;
const MAX_WARM_PAGES = 20;

/** never cached, never answered from a cache */
const NEVER = /^\/(?:api|p)(?:\/|$)/;
/** pages rendered per request (Cache-Control: no-store) that are still only the app shell — the screen loads its
    data on the client — so keeping them is safe. Any other no-store page is respected and not kept. */
const SHELL_DYNAMIC = /^\/(?:event|job|opportunity|market|organize|plan|deck|match|certificate|employer)\//;
/** public files that are not fingerprinted */
const PUBLIC_FILE = /^\/(?:fonts|icons)\/|^\/[^/]+\.(?:woff2?|png|jpe?g|svg|webp|ico|webmanifest)$/;
const FONT_HOSTS = ['fonts.googleapis.com', 'fonts.gstatic.com'];

const ORIGIN = self.location.origin;
const pageKey = url => ORIGIN + url.pathname; // the query (?k=jobs, ?lang=ru) does not change the shell

/* ---------- lifecycle ---------- */
self.addEventListener('install', event => {
  event.waitUntil((async () => {
    const st = await caches.open(STATIC);
    await st.addAll(STATIC_FILES.map(u => new Request(u, { cache: 'no-cache' })));
    const assets = new Set();
    await Promise.all(SHELL_PAGES.map(p => storePage(p, assets)));
    await storeAssets(assets);
  })());
  // no skipWaiting(): an update waits for the student's «Жаңарту»
});

self.addEventListener('activate', event => {
  event.waitUntil((async () => {
    const keys = await caches.keys();
    await Promise.all(keys.filter(k => k.startsWith(PREFIX) && !CURRENT.includes(k)).map(k => caches.delete(k)));
    // the first install takes over open tabs so the very first visit already works offline afterwards
    await self.clients.claim();
  })());
});

self.addEventListener('message', event => {
  const d = event.data || {};
  if (d.type === 'SKIP_WAITING') { self.skipWaiting(); return; }
  if (d.type === 'CACHE_PAGES' && Array.isArray(d.urls)) {
    event.waitUntil((async () => {
      const assets = new Set();
      const paths = d.urls.filter(u => typeof u === 'string').slice(0, MAX_WARM_PAGES);
      await Promise.all(paths.map(p => storePage(p, assets)));
      await storeAssets(assets);
    })());
  }
});

/* ---------- routing ---------- */
self.addEventListener('fetch', event => {
  const req = event.request;
  if (req.method !== 'GET' || req.headers.has('authorization')) return;
  const url = new URL(req.url);

  if (url.origin !== ORIGIN) {
    if (FONT_HOSTS.includes(url.hostname)) event.respondWith(staleWhileRevalidate(req, true));
    return;
  }
  if (NEVER.test(url.pathname)) {
    // /p/<slug> opened without internet: the offline page instead of the browser's error; nothing is stored
    if (req.mode === 'navigate' && url.pathname.startsWith('/p/')) {
      event.respondWith(fetch(req).catch(() => offlinePage()));
    }
    return;
  }
  // the router's data requests (soft navigation, prefetch) stay network-only: when they fail, Next falls back to a
  // full page load, which this worker then answers from the saved pages
  if (req.headers.get('RSC') === '1' || url.searchParams.has('_rsc')) return;

  if (req.mode === 'navigate') { event.respondWith(page(event)); return; }
  if (url.pathname.startsWith('/_next/static/')) { event.respondWith(cacheFirst(req)); return; }
  if (PUBLIC_FILE.test(url.pathname)) { event.respondWith(staleWhileRevalidate(req, false)); return; }
  // everything else (/_next/image, robots.txt, sitemap.xml, ...): the browser's usual way
});

/* ---------- strategies ---------- */
async function page(event) {
  const req = event.request;
  const url = new URL(req.url);
  const cache = await caches.open(PAGES);
  const network = fetch(req).then(res => {
    if (cacheablePage(url, res)) {
      const copy = res.clone();
      event.waitUntil(cache.put(pageKey(url), copy).catch(() => {}));
    }
    return res;
  });
  const cached = await cache.match(pageKey(url));
  if (!cached) {
    try { return await network; } catch { return offlinePage(); }
  }
  event.waitUntil(network.then(() => {}, () => {})); // the saved copy keeps being refreshed
  try {
    const res = await withTimeout(network, NETWORK_TIMEOUT_MS);
    return res.status >= 500 ? cached : res; // the server is down: the saved copy is better than an error page
  } catch {
    return cached; // offline or slow
  }
}

async function cacheFirst(req) {
  const cache = await caches.open(STATIC);
  const hit = await cache.match(req);
  if (hit) return hit;
  const res = await fetch(req);
  if (res.ok && res.type === 'basic') cache.put(req, res.clone()).catch(() => {});
  return res;
}

async function staleWhileRevalidate(req, crossOrigin) {
  const cache = await caches.open(STATIC);
  const hit = await cache.match(req);
  const network = fetch(req).then(res => {
    if ((res.ok && (res.type === 'basic' || res.type === 'cors')) || (crossOrigin && res.type === 'opaque')) {
      cache.put(req, res.clone()).catch(() => {});
    }
    return res;
  });
  if (hit) { network.catch(() => {}); return hit; }
  try { return await network; } catch { return Response.error(); }
}

/* ---------- helpers ---------- */
function cacheablePage(url, res) {
  if (!res || res.status !== 200 || res.type !== 'basic' || res.redirected) return false;
  if (NEVER.test(url.pathname)) return false;
  if (!(res.headers.get('content-type') || '').includes('text/html')) return false;
  const cc = res.headers.get('cache-control') || '';
  if (/no-store/i.test(cc) && !SHELL_PAGES.includes(url.pathname) && !SHELL_DYNAMIC.test(url.pathname)) return false;
  return true;
}

async function offlinePage() {
  return (await caches.match(OFFLINE_URL)) || new Response('Offline', { status: 503, headers: { 'content-type': 'text/plain; charset=utf-8' } });
}

function withTimeout(promise, ms) {
  return new Promise((resolve, reject) => {
    const t = setTimeout(() => reject(new Error('timeout')), ms);
    promise.then(v => { clearTimeout(t); resolve(v); }, e => { clearTimeout(t); reject(e); });
  });
}

/** fetch a same-origin page, keep it, and note the build files it references */
async function storePage(path, assets) {
  try {
    const url = new URL(path, ORIGIN);
    if (url.origin !== ORIGIN || NEVER.test(url.pathname)) return;
    const res = await fetch(url.pathname, { cache: 'no-cache', credentials: 'same-origin', signal: AbortSignal.timeout(NETWORK_TIMEOUT_MS) });
    if (!cacheablePage(url, res)) { await res.body?.cancel(); return; }
    const html = await res.clone().text();
    await (await caches.open(PAGES)).put(pageKey(url), res);
    for (const m of html.matchAll(/\/_next\/static\/[^"'\s\\)<>]+/g)) assets.add(m[0]);
  } catch { /* a page that cannot be fetched now is saved on its first visit */ }
}

/** keep the build files (and the fonts their CSS points to) that are not saved yet */
async function storeAssets(assets) {
  const cache = await caches.open(STATIC);
  const fonts = new Set();
  await Promise.all([...assets].map(async a => {
    try {
      if (await cache.match(a)) return;
      const res = await fetch(a);
      if (!res.ok || res.type !== 'basic') return;
      if (a.endsWith('.css')) {
        const css = await res.clone().text();
        for (const m of css.matchAll(/url\(\s*["']?(\/[^"')\s]+)/g)) fonts.add(m[1]);
      }
      await cache.put(a, res);
    } catch { /* fetched again on first use */ }
  }));
  await Promise.all([...fonts].map(async f => {
    try {
      if (await cache.match(f)) return;
      const res = await fetch(f);
      if (res.ok && res.type === 'basic') await cache.put(f, res);
    } catch { /* ignore */ }
  }));
}
