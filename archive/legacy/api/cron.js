// Daily job, called by Vercel Cron (vercel.json "crons") with Authorization: Bearer <CRON_SECRET>.
import { runCron } from '../lib/bot/cron.js';
import { config, safeEqual } from '../lib/bot/util.js';

export default async function handler(req, res) {
  const auth = String(req.headers.authorization || '');
  const token = auth.startsWith('Bearer ') ? auth.slice(7) : '';
  if (!safeEqual(token, config().cronSecret)) return res.status(401).json({ ok: false });
  try {
    const result = await runCron();
    console.log('cron', JSON.stringify(result));
    return res.status(200).json({ ok: true, ...result });
  } catch (err) {
    console.error('cron failed', err);
    return res.status(500).json({ ok: false });
  }
}
