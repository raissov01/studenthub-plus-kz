// Opportunity data from the site: /api/content first, /data/opportunities.json as fallback.
import { config } from './util.js';

async function getJson(url) {
  const res = await fetch(url, { headers: { Accept: 'application/json' }, signal: AbortSignal.timeout(8000) });
  if (!res.ok) throw new Error(`${url}: HTTP ${res.status}`);
  return res.json();
}

export async function loadOpportunities() {
  const { siteUrl } = config();
  try {
    const data = await getJson(`${siteUrl}/api/content`);
    if (Array.isArray(data?.opportunities)) return data.opportunities;
  } catch { /* not deployed yet or failing: use the static file */ }
  const data = await getJson(`${siteUrl}/data/opportunities.json`);
  return Array.isArray(data) ? data : Array.isArray(data?.opportunities) ? data.opportunities : [];
}

export const oppUrl = id => `${config().siteUrl}/#/opportunity/${encodeURIComponent(id)}`;
