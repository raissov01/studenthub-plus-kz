// GET /api/content — opportunities, jobs and discounts from Google Sheets
// (published CSV links in env), with in-memory and data/*.json fallbacks.
// See docs/CONTENT.md. Helpers live in lib/content/.
import { loadContent } from '../lib/content/load.js';

export default async function handler(req, res) {
  if (req.method !== 'GET' && req.method !== 'HEAD') {
    res.setHeader('Allow', 'GET, HEAD');
    return res.status(405).json({ error: { code: 'method', message: 'Тек GET сұранысы қабылданады.' } });
  }
  let body;
  try {
    body = await loadContent();
  } catch (e) {
    // loadContent already isolates failures per list; this is a last resort.
    console.warn(`[content] күтпеген қате: ${e.message}`);
    body = { opportunities: [], jobs: [], discounts: [], source: 'fallback', updated_at: null, rejected: 0 };
  }
  res.setHeader('Cache-Control', 'public, s-maxage=600, stale-while-revalidate=86400');
  return res.status(200).json(body);
}
