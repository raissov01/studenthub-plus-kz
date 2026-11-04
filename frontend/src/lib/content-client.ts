/* The feed (jobs, opportunities, discounts, events) in the browser. The app shell and every tab are prerendered at
   build time, so nothing waits for the server on a tap: the last feed is kept in memory and in localStorage, shown
   at once, and refreshed from GET /api/content in the background (on start, when the network comes back, and when a
   tab returns after a while). The prerendered HTML carries no feed; the server snapshot is EMPTY_CONTENT. */
import { useSyncExternalStore } from 'react';
import { EMPTY_CONTENT, validContent } from '@/lib/content';
import type { Content } from '@/lib/types';

const KEY = 'studenthub.content.v1';
/** a tab coming back to the foreground refreshes the feed when it is older than this */
const STALE_MS = 5 * 60_000;
const TIMEOUT_MS = 12_000;

let mem: Content | null = null;
let fetchedAt = 0;
let inflight: Promise<Content> | null = null;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach(l => l());

function readSaved(): Content | null {
  try {
    const raw = window.localStorage.getItem(KEY);
    if (!raw) return null;
    const saved = JSON.parse(raw) as { at?: number; data?: unknown };
    if (!validContent(saved.data)) return null;
    fetchedAt = typeof saved.at === 'number' ? saved.at : 0;
    return saved.data;
  } catch { return null; }
}

function save(data: Content) {
  try { window.localStorage.setItem(KEY, JSON.stringify({ at: fetchedAt, data })); } catch { /* quota or private mode */ }
}

/** the current feed: memory, else the saved copy, else EMPTY_CONTENT (source 'loading') */
export function currentContent(): Content {
  if (mem) return mem;
  if (typeof window === 'undefined') return EMPTY_CONTENT;
  mem = readSaved() ?? EMPTY_CONTENT;
  return mem;
}

/** GET /api/content now (one request at a time). Throws when the API is unreachable or answers nonsense; the shown
    feed is then left as it was. */
export function refreshContent(): Promise<Content> {
  inflight ??= (async () => {
    try {
      const r = await fetch('/api/content', { cache: 'no-store', signal: AbortSignal.timeout(TIMEOUT_MS), headers: { accept: 'application/json' } });
      if (!r.ok) throw new Error('Content unavailable');
      const json: unknown = await r.json();
      if (!validContent(json)) throw new Error('Content unavailable');
      const data: Content = { ...json, source: json.source || 'api', updated_at: json.updated_at || '' };
      mem = data;
      fetchedAt = Date.now();
      save(data);
      emit();
      return data;
    } finally {
      inflight = null;
    }
  })();
  return inflight;
}

/** background refresh: never throws; a failed refresh without any saved feed marks the feed «unavailable» so the
    screens stop showing the loading state */
export function refreshQuietly(): void {
  refreshContent().catch(() => {
    if (currentContent().source === 'loading') { mem = { ...EMPTY_CONTENT, source: 'unavailable' }; emit(); }
  });
}

export const contentIsStale = () => Date.now() - fetchedAt > STALE_MS;

function subscribe(l: () => void) {
  listeners.add(l);
  return () => { listeners.delete(l); };
}

/** the feed for rendering; during hydration it is EMPTY_CONTENT (like the prerendered HTML), right after that the
    saved copy */
export function useContentFeed(): Content {
  return useSyncExternalStore(subscribe, currentContent, () => EMPTY_CONTENT);
}
