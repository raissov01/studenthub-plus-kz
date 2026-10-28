// Telegram webhook for the deadline reminder bot. Logic lives in lib/bot/.
import { handleUpdate } from '../lib/bot/bot.js';
import { config, safeEqual } from '../lib/bot/util.js';

export default async function handler(req, res) {
  if (req.method !== 'POST') {
    res.setHeader('Allow', 'POST');
    return res.status(405).json({ ok: false });
  }
  const secret = req.headers['x-telegram-bot-api-secret-token'];
  if (!safeEqual(Array.isArray(secret) ? secret[0] : secret, config().webhookSecret)) {
    return res.status(401).json({ ok: false });
  }
  let update = req.body;
  if (typeof update === 'string') { try { update = JSON.parse(update); } catch { update = null; } }
  if (!update || typeof update !== 'object') return res.status(400).json({ ok: false });
  try {
    await handleUpdate(update);
  } catch (err) {
    // Answer 200 anyway: a non-2xx makes Telegram redeliver the same update again and again.
    console.error('telegram update failed', update.update_id, err);
  }
  return res.status(200).json({ ok: true });
}
