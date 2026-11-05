'use client';
/* The laptop layout (≥1024px wide): a left sidebar instead of the bottom tabs, a wider content area, sheets as centred
   dialogs. Most of it is CSS (app/desktop.css); the few screens that arrange their parts differently on a laptop
   (search filters in a side panel, a detail page with a right rail) ask this hook. Screens render only after
   hydration (components/Shell.tsx), so the first client render already has the right answer: no layout shift.
   The sidebar's «collapsed to icons» choice lives on <html data-side="mini"> (set before paint by the boot script). */
import { useSyncExternalStore } from 'react';

export const DESKTOP_QUERY = '(min-width: 1024px)';
export const SIDE_KEY = 'studenthub.side';

function subscribe(cb: () => void) {
  const mq = window.matchMedia(DESKTOP_QUERY);
  mq.addEventListener('change', cb);
  return () => mq.removeEventListener('change', cb);
}
const get = () => window.matchMedia(DESKTOP_QUERY).matches;

/** true on a laptop/desktop-wide window (false on the server and on phones/tablets) */
export function useDesktop(): boolean {
  return useSyncExternalStore(subscribe, get, () => false);
}

const sideListeners = new Set<() => void>();
const readMini = () => document.documentElement.dataset.side === 'mini';
/** the sidebar collapsed to icons, remembered on this device */
export function useSideMini(): [boolean, (v: boolean) => void] {
  const mini = useSyncExternalStore(cb => { sideListeners.add(cb); return () => { sideListeners.delete(cb); }; }, readMini, () => false);
  const set = (v: boolean) => {
    if (v) document.documentElement.dataset.side = 'mini'; else delete document.documentElement.dataset.side;
    try { window.localStorage.setItem(SIDE_KEY, v ? 'mini' : ''); } catch { /* ignore */ }
    sideListeners.forEach(f => f());
  };
  return [mini, set];
}
