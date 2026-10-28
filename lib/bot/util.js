import { createHash, timingSafeEqual } from 'node:crypto';

export const TZ = 'Asia/Almaty';
export const REMIND_DAYS = [7, 3, 1];
export const MAX_REMINDERS = 30;
export const ID_RE = /^[A-Za-z0-9_-]{1,40}$/;

export function config(env = process.env) {
  return {
    token: env.BOT_TOKEN || '',
    webhookSecret: env.WEBHOOK_SECRET || '',
    cronSecret: env.CRON_SECRET || '',
    siteUrl: (env.SITE_URL || 'https://studenthub-plus-kz.vercel.app').replace(/\/+$/, ''),
    admins: new Set(String(env.ADMIN_IDS || '').split(',').map(s => s.trim()).filter(Boolean)),
    digest: /^(1|true|yes|on)$/i.test(String(env.DIGEST_ENABLED || ''))
  };
}

// Constant-time comparison; hashing first makes lengths equal so nothing leaks via length.
// An empty expected secret never matches (fail closed when the env var is missing).
export function safeEqual(given, expected) {
  if (!expected || typeof given !== 'string') return false;
  const a = createHash('sha256').update(given).digest();
  const b = createHash('sha256').update(expected).digest();
  return timingSafeEqual(a, b);
}

// 'YYYY-MM-DD' of the calendar day in Almaty.
export function localDate(now = new Date()) {
  return new Intl.DateTimeFormat('en-CA', { timeZone: TZ, year: 'numeric', month: '2-digit', day: '2-digit' }).format(now);
}

const dayNum = iso => Date.UTC(+iso.slice(0, 4), +iso.slice(5, 7) - 1, +iso.slice(8, 10)) / 864e5;
export const isIsoDate = s => typeof s === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(s) && !Number.isNaN(dayNum(s));
export const daysBetween = (fromIso, toIso) => Math.round(dayNum(toIso) - dayNum(fromIso));
export function addDays(iso, n) { return new Date((dayNum(iso) + n) * 864e5).toISOString().slice(0, 10); }
// 1 = Monday ... 7 = Sunday
export function weekday(iso) { return ((new Date(dayNum(iso) * 864e5).getUTCDay() + 6) % 7) + 1; }
// Monday of the week, used as the digest "week id".
export const weekStart = iso => addDays(iso, 1 - weekday(iso));

export const esc = s => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
export const pick = (obj, lang) => (obj && typeof obj === 'object' ? obj[lang] || obj.kk || obj.ru || '' : String(obj || ''));
