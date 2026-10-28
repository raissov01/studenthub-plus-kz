/* Content loading for server components: GET /api/content from the Go API (revalidated every 10 minutes),
   falling back to the bundled data/*.json when the API is unreachable (build time, local dev without Go). */
import { cache } from 'react';
import type { Content, Discount, Job, Opportunity } from '@/lib/types';
import opportunitiesJson from '@/data/opportunities.json';
import jobsJson from '@/data/jobs.json';
import discountsJson from '@/data/discounts.json';

export const REVALIDATE_SECONDS = 600;
const TIMEOUT_MS = 4000;

/** Same origin as the /api rewrite in next.config.ts: API_ORIGIN, else the Go dev server locally, else our API host. */
export function apiOrigin(): string {
  const o = process.env.API_ORIGIN ?? (process.env.NODE_ENV === 'development' ? 'http://localhost:8080' : 'https://studenthub.85-137-91-47.sslip.io');
  return o.replace(/\/+$/, '');
}

/** today's date in Astana (UTC+5) as YYYY-MM-DD, so the cut-off does not depend on the server's zone */
export function astanaToday(now = Date.now()): string {
  return new Date(now + 5 * 3600_000).toISOString().slice(0, 10);
}

/** an opportunity whose deadline has passed never reaches a list (the API filters too; this is the guard) */
export const isOpen = (o: { deadline: string | null }, today = astanaToday()) => !o.deadline || o.deadline >= today;

function fallback(): Content {
  return {
    opportunities: opportunitiesJson as Opportunity[],
    jobs: jobsJson as Job[],
    discounts: discountsJson as Discount[],
    source: 'bundled',
    updated_at: '',
  };
}

function valid(x: unknown): x is Content {
  if (!x || typeof x !== 'object') return false;
  const c = x as Record<string, unknown>;
  return Array.isArray(c.opportunities) && Array.isArray(c.jobs) && Array.isArray(c.discounts);
}

async function load(): Promise<Content> {
  let data: Content;
  try {
    const res = await fetch(`${apiOrigin()}/api/content`, {
      next: { revalidate: REVALIDATE_SECONDS },
      signal: AbortSignal.timeout(TIMEOUT_MS),
      headers: { accept: 'application/json' },
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const json: unknown = await res.json();
    if (!valid(json)) throw new Error('unexpected shape');
    data = { ...json, source: json.source || 'api', updated_at: json.updated_at || '' };
  } catch {
    data = fallback();
  }
  const today = astanaToday();
  return { ...data, opportunities: data.opportunities.filter(o => isOpen(o, today)) };
}

/** deduplicated per request; the fetch itself is cached for REVALIDATE_SECONDS */
export const getContent = cache(load);

export async function getJob(id: string) {
  return (await getContent()).jobs.find(j => j.id === id) ?? null;
}
export async function getOpportunity(id: string) {
  return (await getContent()).opportunities.find(o => o.id === id) ?? null;
}
