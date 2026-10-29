/* In-site inbox (docs/api/inbox.md, pkg/notify/inbox.go): the shapes of GET /api/me/inbox and POST /api/me/inbox/read
   and the small pure helpers the screen and the header badge share. No imports with side effects: lib/inbox.test.ts
   runs this file under plain Node. */
import type { IconName } from './icons';

export type InboxCategory = 'deadlines' | 'digest' | 'events' | 'mentors' | 'market' | 'jobs' | 'moderation' | 'community';
export interface InboxItem {
  id: number;
  category: InboxCategory | string;
  /** plain text (bot HTML stripped by the server); React escapes it */
  text: string;
  /** a site path ("/event/12") or null */
  link: string | null;
  createdAt: string;
  readAt: string | null;
  read: boolean;
}
export interface InboxPage { items: InboxItem[]; unread: number; nextBefore: number | null }
export interface InboxReadResp { ok: true; marked: number; unread: number }

/** server page size (default 20, at most 50) */
export const INBOX_PAGE = 20;
/** GET /api/me re-check while the page is visible */
export const INBOX_POLL_MS = 2 * 60 * 1000;

export function inboxPath(opts: { before?: number | null; unread?: boolean } = {}): string {
  const q = new URLSearchParams({ limit: String(INBOX_PAGE) });
  if (opts.before) q.set('before', String(opts.before));
  if (opts.unread) q.set('unread', '1');
  return '/api/me/inbox?' + q.toString();
}

type TT = (kk: string, ru: string) => string;
const CATS: Record<InboxCategory, { kk: string; ru: string; icon: IconName; tone: number }> = {
  deadlines: { kk: 'Мерзім', ru: 'Дедлайн', icon: 'clock', tone: 3 },
  digest: { kk: 'Топтама', ru: 'Подборка', icon: 'spark', tone: 4 },
  events: { kk: 'Іс-шара', ru: 'Мероприятие', icon: 'calendar', tone: 1 },
  mentors: { kk: 'Менторлар', ru: 'Менторы', icon: 'user', tone: 2 },
  market: { kk: 'Маркет', ru: 'Маркет', icon: 'tag', tone: 2 },
  jobs: { kk: 'Жұмыс', ru: 'Работа', icon: 'briefcase', tone: 0 },
  community: { kk: 'Қауымдастық', ru: 'Сообщество', icon: 'bell', tone: 4 },
  moderation: { kk: 'Модерация', ru: 'Модерация', icon: 'shield', tone: 3 },
};
/** label, icon and colour of a category; an unknown one (a newer server) reads as a plain notice */
export function inboxCategory(c: string, tt: TT): { label: string; icon: IconName; tone: number } {
  const x = (CATS as Record<string, (typeof CATS)[InboxCategory] | undefined>)[c];
  return x ? { label: tt(x.kk, x.ru), icon: x.icon, tone: x.tone } : { label: tt('Хабарлама', 'Уведомление'), icon: 'bell', tone: 4 };
}

/** only an in-site path ("/x", not "//host", "/\\host" or a full URL) is followed */
export function inboxLink(link: string | null | undefined): string | null {
  if (!link || link.length > 300) return null;
  return /^\/[^/\\][^\s<>"']*$/.test(link) || link === '/' ? link : null;
}

/** header / tab badge: nothing for 0, «9+» above nine */
export function badgeText(n: number): string {
  return n > 9 ? '9+' : n > 0 ? String(n) : '';
}

/** «5 мин бұрын», «2 сағ бұрын», «3 күн бұрын», else the date */
export function inboxAgo(iso: string, now: number, tt: TT, lang: 'kk' | 'ru'): string {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return '';
  const min = Math.max(0, Math.floor((now - t) / 60000));
  if (min < 1) return tt('Жаңа ғана', 'Только что');
  if (min < 60) return tt(`${min} мин бұрын`, `${min} мин назад`);
  const h = Math.floor(min / 60);
  if (h < 24) return tt(`${h} сағ бұрын`, `${h} ч назад`);
  const d = Math.floor(h / 24);
  if (d < 7) return tt(`${d} күн бұрын`, `${d} дн. назад`);
  return new Date(t).toLocaleDateString(lang === 'ru' ? 'ru-RU' : 'kk-KZ', { day: 'numeric', month: 'short', year: d > 300 ? 'numeric' : undefined });
}
