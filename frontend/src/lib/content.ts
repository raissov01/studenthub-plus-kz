/* Content loading for SERVER components only (the job / opportunity detail pages need it for their metadata and
   their existence check): GET /api/content from the Go API, uncached so new records show at once. The app shell
   and every tab are static and load the same feed in the browser instead (lib/content-client.ts).
   When the API is unreachable the lists are empty: the Go API itself already falls back to backend/data/*.json. */
import { cache } from 'react';
import type { Content } from '@/lib/types';

const TIMEOUT_MS = 4000;

/** Where server-side code reaches the Go API: API_ORIGIN (http://api:8110 inside docker compose), else the Go dev
    server locally, else the public site (nginx sends /api/* straight to the API). */
export function apiOrigin(): string {
  const o = process.env.API_ORIGIN ?? (process.env.NODE_ENV === 'development' ? 'http://localhost:8080' : 'https://studenthub.studywithraissov.com');
  return o.replace(/\/+$/, '');
}

/** today's date in Astana (UTC+5) as YYYY-MM-DD, so the cut-off does not depend on the server's zone */
export function astanaToday(now = Date.now()): string {
  return new Date(now + 5 * 3600_000).toISOString().slice(0, 10);
}

/** an opportunity whose deadline has passed never reaches a list (the API filters too; this is the guard) */
export const isOpen = (o: { deadline: string | null }, today = astanaToday()) => !o.deadline || o.deadline >= today;

export const EMPTY_CONTENT: Content = { opportunities: [], jobs: [], discounts: [], source: 'loading', updated_at: '' };

/** the shape every reader relies on; anything else is treated as «no content» */
export function validContent(x: unknown): x is Content {
  if (!x || typeof x !== 'object') return false;
  const c = x as Record<string, unknown>;
  return Array.isArray(c.opportunities) && Array.isArray(c.jobs) && Array.isArray(c.discounts);
}

async function load(): Promise<Content> {
  let data: Content;
  try {
    const res = await fetch(`${apiOrigin()}/api/content`, {
      cache: 'no-store',
      signal: AbortSignal.timeout(TIMEOUT_MS),
      headers: { accept: 'application/json' },
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const json: unknown = await res.json();
    if (!validContent(json)) throw new Error('unexpected shape');
    data = { ...json, source: json.source || 'api', updated_at: json.updated_at || '' };
  } catch {
    data = { ...EMPTY_CONTENT, source: 'unavailable' };
  }
  const today = astanaToday();
  return { ...data, opportunities: data.opportunities.filter(o => isOpen(o, today)) };
}

/** deduplicated per request; no persistent cache */
export const getContent = cache(load);

export async function getJob(id: string) {
  return (await getContent()).jobs.find(j => j.id === id) ?? null;
}
export async function getOpportunity(id: string) {
  return (await getContent()).opportunities.find(o => o.id === id) ?? null;
}
