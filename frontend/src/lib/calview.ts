/* «Күнтізбе» views: pure logic for the year view, the titles packed into a month cell, the day agenda order and the
   own-event colour palette. No DOM, no React. The colours live in app/globals.css as tokens --ev-N (solid: stripe and
   swatch), --ev-N-soft (chip background) and --ev-N-ink (text on the chip, WCAG AA on --ev-N-soft) in both themes. */
import { monthGrid, type GridDay, type MonthRef, type Ymd } from './calendar';

/* ---------- own-event colours ---------- */
export interface EvColor { n: number; kk: string; ru: string }
/** the palette of own events, in the order of the swatches in the add sheet */
export const EV_COLORS: EvColor[] = [
  { n: 1, kk: 'Көк', ru: 'Синий' },
  { n: 2, kk: 'Күлгін', ru: 'Фиолетовый' },
  { n: 3, kk: 'Қызғылт', ru: 'Розовый' },
  { n: 4, kk: 'Қызыл', ru: 'Красный' },
  { n: 5, kk: 'Қызғылт сары', ru: 'Оранжевый' },
  { n: 6, kk: 'Сары', ru: 'Жёлтый' },
  { n: 7, kk: 'Жасыл', ru: 'Зелёный' },
  { n: 8, kk: 'Көгілдір', ru: 'Бирюзовый' },
];
export const DEFAULT_EV_COLOR = 1;
export const isEvColor = (n: unknown): n is number => Number.isInteger(n) && (n as number) >= 1 && (n as number) <= EV_COLORS.length;

/** the CSS values of an own-event colour; an unknown number falls back to the first colour */
export function evColorVars(n: number): { solid: string; soft: string; ink: string } {
  const k = isEvColor(n) ? n : DEFAULT_EV_COLOR;
  return { solid: `var(--ev-${k})`, soft: `var(--ev-${k}-soft)`, ink: `var(--ev-${k}-ink)` };
}
/** the colour's name in the UI language (the swatch's accessible name) */
export const evColorName = (n: number, lang: 'kk' | 'ru') => (EV_COLORS.find(c => c.n === n) ?? EV_COLORS[0])[lang];

/* ---------- agenda order ---------- */
export interface Timed { time?: string; end?: string; title: string }

/** all-day items first (by title), then timed ones by start, end and title */
export function compareAgenda(a: Timed, b: Timed): number {
  const ta = a.time || '', tb = b.time || '';
  if (!ta !== !tb) return ta ? 1 : -1;
  return ta.localeCompare(tb) || (a.end || '').localeCompare(b.end || '') || a.title.localeCompare(b.title);
}
export const sortAgenda = <T extends Timed>(items: T[]): T[] => [...items].sort(compareAgenda);

/** '09:00–10:20', '09:00' or '' for an all-day item */
export const timeRange = (time?: string, end?: string) => (time ? (end && end > time ? `${time}–${end}` : time) : '');

/* ---------- month cell ---------- */
/** the titles a month cell shows: up to `max` items; when there are more, `max − 1` of them and «+N» for the rest
    (so the cell never grows past `max` lines) */
export function packCell<T>(items: T[], max = 3): { shown: T[]; more: number } {
  const cap = Math.max(1, max);
  if (items.length <= cap) return { shown: items, more: 0 };
  return { shown: items.slice(0, cap - 1), more: items.length - (cap - 1) };
}

/* ---------- year view ---------- */
export interface MiniMonth { ref: MonthRef; weeks: (GridDay | null)[][] }
/** the 12 months of a year as Monday-first mini grids; days of the neighbouring months are null (blank) */
export function yearGrid(y: number): MiniMonth[] {
  return Array.from({ length: 12 }, (_, m) => ({
    ref: { y, m },
    weeks: monthGrid({ y, m }).map(w => w.map(d => (d.inMonth ? d : null))),
  }));
}

/** how many items fall in each month of `y` (index 0–11) */
export function monthCounts(dates: Iterable<Ymd>, y: number): number[] {
  const out = Array<number>(12).fill(0);
  const pre = String(y) + '-';
  for (const d of dates) if (d.startsWith(pre)) out[+d.slice(5, 7) - 1]++;
  return out;
}

/** the years the month/year picker offers: two back and three ahead of this year, always including `shown` */
export function yearOptions(thisYear: number, shown: number): number[] {
  const a = Math.min(thisYear - 2, shown), b = Math.max(thisYear + 3, shown);
  return Array.from({ length: b - a + 1 }, (_, i) => a + i);
}
