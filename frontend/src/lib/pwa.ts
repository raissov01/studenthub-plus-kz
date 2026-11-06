/* Offline support on the client (docs/PWA.md): the connectivity flag, the service worker registration with the
   «new version ready» signal, and a way to ask the worker to keep a page for offline use. The worker itself is the
   hand-written public/sw.js. */
import { useSyncExternalStore } from 'react';

/* ---------- connectivity ---------- */
const subscribeOnline = (l: () => void) => {
  window.addEventListener('online', l);
  window.addEventListener('offline', l);
  return () => { window.removeEventListener('online', l); window.removeEventListener('offline', l); };
};
/** false only when the browser knows it has no network (navigator.onLine); the server always renders «online» */
export const isOnline = () => typeof navigator === 'undefined' || navigator.onLine !== false;
export function useOnline(): boolean {
  return useSyncExternalStore(subscribeOnline, isOnline, () => true);
}

/* ---------- service worker ---------- */
/** a new worker installed and waiting for the student to accept it */
let waiting: ServiceWorker | null = null;
const updListeners = new Set<() => void>();
const subscribeUpdate = (l: () => void) => { updListeners.add(l); return () => { updListeners.delete(l); }; };
function setWaiting(w: ServiceWorker | null) {
  waiting = w;
  updListeners.forEach(l => l());
}
export function useUpdateReady(): boolean {
  return useSyncExternalStore(subscribeUpdate, () => !!waiting, () => false);
}

/** where a worker may run: production builds over https, or on this machine (http://localhost for testing) */
function swAllowed(): boolean {
  if (process.env.NODE_ENV !== 'production') return false;
  if (typeof window === 'undefined' || !('serviceWorker' in navigator)) return false;
  const { protocol, hostname } = window.location;
  return protocol === 'https:' || hostname === 'localhost' || hostname === '127.0.0.1' || hostname === '[::1]';
}

let registered = false;
/** Register /sw.js once. The build id rides in the URL, so every deploy installs a new worker (and new caches);
    it then waits until the student taps «Жаңарту» — the page is never reloaded on its own. */
export function registerServiceWorker(): void {
  if (registered || !swAllowed()) return;
  registered = true;
  const build = process.env.NEXT_PUBLIC_BUILD_ID || 'dev';
  const time = process.env.NEXT_PUBLIC_BUILD_TIME || '0';
  // a page that is older than the running worker (its saved copy was served because the network was slow) must not
  // register its own, older build: that installed the old worker as an "update" and showed «Жаңа нұсқа дайын» again
  if (isOlderThanController(time)) return;
  let reloading = false;
  let accepted = false;
  navigator.serviceWorker.addEventListener('controllerchange', () => {
    // the very first install (clients.claim): the build files this page loaded before the worker existed are kept
    // too, in case the install's own fetch of the pages timed out (a cold server) and missed them
    if (!accepted) { keepLoadedAssets(); return; }
    // only after the student accepted the update; the very first install (clients.claim) must not reload
    if (reloading) return;
    reloading = true;
    window.location.reload();
  });
  applyUpdate = () => {
    if (!waiting) return;
    accepted = true;
    waiting.postMessage({ type: 'SKIP_WAITING' });
  };
  navigator.serviceWorker.register(`/sw.js?v=${encodeURIComponent(build)}&t=${encodeURIComponent(time)}`, { scope: '/', updateViaCache: 'none' }).then(reg => {
    const track = (w: ServiceWorker | null) => {
      if (!w) return;
      const check = () => { if (w.state === 'installed' && navigator.serviceWorker.controller) setWaiting(w); };
      check();
      w.addEventListener('statechange', check);
    };
    track(reg.waiting);
    reg.addEventListener('updatefound', () => track(reg.installing));
    // a long-open tab (Telegram keeps the Mini App alive) still learns about a deploy
    const poll = () => { if (document.visibilityState === 'visible' && isOnline()) reg.update().catch(() => {}); };
    document.addEventListener('visibilitychange', poll);
    setInterval(poll, 30 * 60_000);
  }).catch(() => { /* no worker: the site works as before, only without offline pages */ });
}

/** the running worker's build time (the t= in its script URL) is newer than this page's build */
export function isOlderThanController(time: string, scriptURL = typeof navigator !== 'undefined' ? navigator.serviceWorker?.controller?.scriptURL : undefined): boolean {
  if (!scriptURL) return false;
  try {
    const t = Number(new URL(scriptURL).searchParams.get('t') || 0);
    return t > 0 && Number(time) > 0 && t > Number(time);
  } catch { return false; }
}

function keepLoadedAssets(): void {
  try {
    const c = navigator.serviceWorker.controller;
    const urls = performance.getEntriesByType('resource').map(e => e.name)
      .filter(u => u.startsWith(window.location.origin + '/_next/static/')).map(u => new URL(u).pathname);
    if (c && urls.length) c.postMessage({ type: 'CACHE_ASSETS', urls });
  } catch { /* ignore */ }
}

let applyUpdate = () => {};
/** «Жаңарту»: activate the waiting worker; the page reloads once it takes control */
export const acceptUpdate = () => applyUpdate();

/** Ask the worker to keep these same-origin pages for offline use (e.g. /event/12 once its ticket is saved).
    No-op without a controlling worker or without network. */
export function keepPagesOffline(paths: string[]): void {
  try {
    const c = typeof navigator !== 'undefined' ? navigator.serviceWorker?.controller : null;
    if (c && isOnline() && paths.length) c.postMessage({ type: 'CACHE_PAGES', urls: paths });
  } catch { /* ignore */ }
}
