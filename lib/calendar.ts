/* «Күнтізбе» (deadline calendar): pure date logic. Every date is an Almaty calendar day 'YYYY-MM-DD' (UTC+5 all
   year since 2024-03-01), so the grid does not depend on the device's zone. Weeks start on Monday. No DOM, no imports. */

export type Ymd = string;
export type CalLang = 'kk' | 'ru';

/** a deadline the student added on this device (AppState.deadlines) */
export interface OwnDeadline {
  id: string;
  title: string;
  /** 'YYYY-MM-DD' Almaty day */
  date: Ymd;
  /** 'HH:MM' Almaty wall clock, empty for an all-day deadline */
  time: string;
  note: string;
  created: number;
}

export const ALMATY_TZ = 'Asia/Almaty';
const OFFSET_MS = 5 * 3600_000;
const DAY_MS = 86400_000;
const pad = (n: number) => String(n).padStart(2, '0');

export const MONTHS: Record<CalLang, string[]> = {
  kk: ['Қаңтар', 'Ақпан', 'Наурыз', 'Сәуір', 'Мамыр', 'Маусым', 'Шілде', 'Тамыз', 'Қыркүйек', 'Қазан', 'Қараша', 'Желтоқсан'],
  ru: ['Январь', 'Февраль', 'Март', 'Апрель', 'Май', 'Июнь', 'Июль', 'Август', 'Сентябрь', 'Октябрь', 'Ноябрь', 'Декабрь'],
};
/** column heads, Monday first */
export const WEEK_SHORT: Record<CalLang, string[]> = {
  kk: ['Дс', 'Сс', 'Ср', 'Бс', 'Жм', 'Сб', 'Жс'],
  ru: ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс'],
};

export const isYmd = (s: unknown): s is Ymd => typeof s === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(s) && !Number.isNaN(Date.parse(s + 'T00:00:00Z'));
export const isHm = (s: unknown): s is string => typeof s === 'string' && /^([01]\d|2[0-3]):[0-5]\d$/.test(s);

/** the Almaty calendar day of an instant */
export function almatyYmd(at: Date | number | string): Ymd {
  const t = typeof at === 'number' ? at : new Date(at).getTime();
  return new Date(t + OFFSET_MS).toISOString().slice(0, 10);
}
/** the Almaty wall-clock time 'HH:MM' of an instant */
export function almatyHm(at: Date | number | string): string {
  const t = typeof at === 'number' ? at : new Date(at).getTime();
  return new Date(t + OFFSET_MS).toISOString().slice(11, 16);
}

const utc = (d: Ymd) => new Date(d + 'T00:00:00Z');
export function addDays(d: Ymd, n: number): Ymd {
  return new Date(utc(d).getTime() + n * DAY_MS).toISOString().slice(0, 10);
}
/** whole days from a to b (b − a) */
export const daysBetween = (a: Ymd, b: Ymd) => Math.round((utc(b).getTime() - utc(a).getTime()) / DAY_MS);
/** 0 = Monday … 6 = Sunday */
export const weekdayMon = (d: Ymd) => (utc(d).getUTCDay() + 6) % 7;
/** 0 = Sunday … 6 = Saturday (the i18n `wd` order) */
export const weekdaySun = (d: Ymd) => utc(d).getUTCDay();

export interface MonthRef { y: number; m: number } // m: 0–11
export const monthOf = (d: Ymd): MonthRef => ({ y: +d.slice(0, 4), m: +d.slice(5, 7) - 1 });
export function shiftMonth(r: MonthRef, n: number): MonthRef {
  const k = r.y * 12 + r.m + n;
  return { y: Math.floor(k / 12), m: ((k % 12) + 12) % 12 };
}
export const monthKey = (r: MonthRef) => `${r.y}-${pad(r.m + 1)}`;

export interface GridDay { ymd: Ymd; day: number; inMonth: boolean }
/** whole Monday-first weeks covering the month (4–6 rows of 7) */
export function monthGrid(r: MonthRef): GridDay[][] {
  const first = `${r.y}-${pad(r.m + 1)}-01`;
  const start = addDays(first, -weekdayMon(first));
  const next = shiftMonth(r, 1);
  const last = addDays(`${next.y}-${pad(next.m + 1)}-01`, -1);
  const end = addDays(last, 6 - weekdayMon(last));
  const weeks: GridDay[][] = [];
  for (let d = start; d <= end; d = addDays(d, 7)) {
    const w: GridDay[] = [];
    for (let i = 0; i < 7; i++) {
      const x = addDays(d, i);
      w.push({ ymd: x, day: +x.slice(8, 10), inMonth: x.slice(0, 7) === first.slice(0, 7) });
    }
    weeks.push(w);
  }
  return weeks;
}

/** items keyed by day, each day's items sorted by time (all-day first) */
export function byDay<T extends { date: Ymd; time?: string }>(items: T[]): Map<Ymd, T[]> {
  const m = new Map<Ymd, T[]>();
  for (const it of items) {
    const a = m.get(it.date);
    if (a) a.push(it); else m.set(it.date, [it]);
  }
  for (const a of m.values()) a.sort((x, y) => (x.time || '').localeCompare(y.time || ''));
  return m;
}

/** the next `days` days from `from` (inclusive) that have items, in order */
export function agenda<T extends { date: Ymd; time?: string }>(items: T[], from: Ymd, days: number): [Ymd, T[]][] {
  const until = addDays(from, days - 1);
  return [...byDay(items.filter(i => i.date >= from && i.date <= until)).entries()].sort((a, b) => a[0].localeCompare(b[0]));
}

/** stored deadlines (absent before the feature, or edited by hand) -> valid OwnDeadline[] */
export function cleanDeadlines(raw: unknown): OwnDeadline[] {
  if (!Array.isArray(raw)) return [];
  const out: OwnDeadline[] = [];
  for (const r of raw) {
    if (!r || typeof r !== 'object') continue;
    const x = r as Partial<OwnDeadline>;
    if (typeof x.id !== 'string' || !x.id || typeof x.title !== 'string' || !x.title.trim() || !isYmd(x.date)) continue;
    out.push({
      id: x.id, title: x.title.slice(0, 200), date: x.date, time: isHm(x.time) ? x.time : '',
      note: typeof x.note === 'string' ? x.note.slice(0, 2000) : '', created: typeof x.created === 'number' ? x.created : 0,
    });
  }
  return out;
}

/* ---------- export to other calendars ---------- */
export interface CalExport { title: string; date: Ymd; time?: string; details?: string }

const compact = (d: Ymd) => d.replace(/-/g, '');
/** Google Calendar «add event» link: an all-day event, or one hour from `time` in Almaty */
export function googleCalendarUrl(e: CalExport): string {
  let dates: string, tz = '';
  if (e.time && isHm(e.time)) {
    const [h, mi] = e.time.split(':').map(Number);
    const endMin = h * 60 + mi + 60;
    const endDay = endMin >= 1440 ? addDays(e.date, 1) : e.date;
    const em = endMin % 1440;
    dates = `${compact(e.date)}T${pad(h)}${pad(mi)}00/${compact(endDay)}T${pad(Math.floor(em / 60))}${pad(em % 60)}00`;
    tz = '&ctz=' + encodeURIComponent(ALMATY_TZ);
  } else {
    dates = `${compact(e.date)}/${compact(addDays(e.date, 1))}`;
  }
  return 'https://calendar.google.com/calendar/render?action=TEMPLATE'
    + '&text=' + encodeURIComponent(e.title)
    + '&dates=' + dates
    + (e.details ? '&details=' + encodeURIComponent(e.details) : '')
    + tz;
}
