/* i18n and date helpers (legacy tt / L / fDate / daysLeftTxt), bound to a language. */
import type { Lang, LText, Pair } from '@/lib/types';
import { locale, lt, tr } from '@/lib/i18n';

export const DAY = 86400000;
/** English month and weekday names come from Intl (en locale) */
const intl = (o: Intl.DateTimeFormatOptions, n: number, at: (i: number) => Date) => Array.from({ length: n }, (_, i) => new Intl.DateTimeFormat('en', o).format(at(i)));
const EN_MON_S = intl({ month: 'short' }, 12, i => new Date(2026, i, 1));
const EN_MON_L = intl({ month: 'long' }, 12, i => new Date(2026, i, 1));
const EN_WD = intl({ weekday: 'long' }, 7, i => new Date(2026, 10, 1 + i)); // 1 Nov 2026 is a Sunday
const MON_S: Record<Lang, string[]> = { en: EN_MON_S, kk: ['қаң', 'ақп', 'нау', 'сәу', 'мам', 'мау', 'шіл', 'там', 'қыр', 'қаз', 'қар', 'жел'], ru: ['янв', 'фев', 'мар', 'апр', 'мая', 'июн', 'июл', 'авг', 'сен', 'окт', 'ноя', 'дек'] };
const MON_L: Record<Lang, string[]> = { en: EN_MON_L, kk: ['қаңтар', 'ақпан', 'наурыз', 'сәуір', 'мамыр', 'маусым', 'шілде', 'тамыз', 'қыркүйек', 'қазан', 'қараша', 'желтоқсан'], ru: ['января', 'февраля', 'марта', 'апреля', 'мая', 'июня', 'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря'] };
const WD: Record<Lang, string[]> = { en: EN_WD, kk: ['жексенбі', 'дүйсенбі', 'сейсенбі', 'сәрсенбі', 'бейсенбі', 'жұма', 'сенбі'], ru: ['воскресенье', 'понедельник', 'вторник', 'среда', 'четверг', 'пятница', 'суббота'] };

/* "today" is fixed once per page load, as in the legacy app */
let t0: Date | null = null;
export function T0(): Date {
  if (!t0) { t0 = new Date(); t0.setHours(0, 0, 0, 0); }
  return t0;
}
export const dOff = (n: number) => new Date(T0().getTime() + n * DAY);
/** 'YYYY-MM-DD' -> local midnight */
export function parseYmd(s: string): Date {
  const [y, m, d] = s.slice(0, 10).split('-').map(Number);
  return new Date(y, (m || 1) - 1, d || 1);
}
export const diffDays = (d: Date | string) => Math.round((new Date(d).setHours(0, 0, 0, 0) - T0().getTime()) / DAY);
export const daysToYmd = (s: string) => Math.round((parseYmd(s).getTime() - T0().getTime()) / DAY);
export const isoLocal = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
export const money = (n: number, lang?: Lang) => (lang === 'en' ? new Intl.NumberFormat('en').format(Math.round(n)) : Math.round(n).toLocaleString('ru-RU').replace(/\s/g, ' ')) + ' ₸';
export function plural(n: number, f: [string, string, string]) {
  const a = Math.abs(n) % 100, b = a % 10;
  if (a > 10 && a < 20) return f[2];
  if (b > 1 && b < 5) return f[1];
  if (b === 1) return f[0];
  return f[2];
}

/** `dictVersion` only makes memoized copies refresh when the English dictionary arrives (src/lib/i18n) */
export function i18n(lang: Lang, dictVersion = 0) {
  void dictVersion;
  const tt = (kk: string, ru: string) => tr(lang, kk, ru);
  const L = (o?: LText | string | null) => lt(lang, o);
  const tp = (p: Pair) => tt(p[0], p[1]);
  const fDate = (d: Date) => (lang === 'en' ? new Intl.DateTimeFormat(locale(lang), { day: 'numeric', month: 'long' }).format(d) : `${d.getDate()} ${MON_L[lang][d.getMonth()]}`);
  const fDateY = (d: Date) => (lang === 'en' ? new Intl.DateTimeFormat(locale(lang), { day: 'numeric', month: 'long', year: 'numeric' }).format(d) : `${fDate(d)} ${d.getFullYear()}`);
  function daysLeftTxt(n: number) {
    if (n < 0) return tt('Мерзімі өтті', 'Срок прошёл');
    if (n === 0) return tt('Бүгін соңғы күн', 'Сегодня последний день');
    return tt(`${n} күн қалды`, `Осталось ${n} ${plural(n, ['день', 'дня', 'дней'])}`);
  }
  return { lang, tt, L, tp, fDate, fDateY, daysLeftTxt, monS: MON_S[lang], wd: WD[lang], money: (n: number) => money(n, lang) };
}
export type I18n = ReturnType<typeof i18n>;

/** the avatar letter: the first letter of the name, or of the fallback («Студент») while the name is empty;
    the sidebar, the profile and the edit page's preview all show the same letter */
export const initialOf = (name: string, fallback = 'Студент') => ((name || '').trim() || fallback).charAt(0).toUpperCase();
