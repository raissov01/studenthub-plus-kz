/* i18n and date helpers (legacy tt / L / fDate / daysLeftTxt), bound to a language. */
import type { Lang, LText, Pair } from '@/lib/types';

export const DAY = 86400000;
const MON_S: Record<Lang, string[]> = { kk: ['қаң', 'ақп', 'нау', 'сәу', 'мам', 'мау', 'шіл', 'там', 'қыр', 'қаз', 'қар', 'жел'], ru: ['янв', 'фев', 'мар', 'апр', 'мая', 'июн', 'июл', 'авг', 'сен', 'окт', 'ноя', 'дек'] };
const MON_L: Record<Lang, string[]> = { kk: ['қаңтар', 'ақпан', 'наурыз', 'сәуір', 'мамыр', 'маусым', 'шілде', 'тамыз', 'қыркүйек', 'қазан', 'қараша', 'желтоқсан'], ru: ['января', 'февраля', 'марта', 'апреля', 'мая', 'июня', 'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря'] };
const WD: Record<Lang, string[]> = { kk: ['жексенбі', 'дүйсенбі', 'сейсенбі', 'сәрсенбі', 'бейсенбі', 'жұма', 'сенбі'], ru: ['воскресенье', 'понедельник', 'вторник', 'среда', 'четверг', 'пятница', 'суббота'] };

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
export const money = (n: number) => Math.round(n).toLocaleString('ru-RU').replace(/\s/g, ' ') + ' ₸';
export function plural(n: number, f: [string, string, string]) {
  const a = Math.abs(n) % 100, b = a % 10;
  if (a > 10 && a < 20) return f[2];
  if (b > 1 && b < 5) return f[1];
  if (b === 1) return f[0];
  return f[2];
}

export function i18n(lang: Lang) {
  const tt = (kk: string, ru: string) => (lang === 'ru' ? ru : kk);
  const L = (o?: LText | string | null) => (o ? (typeof o === 'string' ? o : (o[lang] || o.kk || o.ru || '')) : '');
  const tp = (p: Pair) => tt(p[0], p[1]);
  const fDate = (d: Date) => `${d.getDate()} ${MON_L[lang][d.getMonth()]}`;
  const fDateY = (d: Date) => `${fDate(d)} ${d.getFullYear()}`;
  function daysLeftTxt(n: number) {
    if (n < 0) return tt('Мерзімі өтті', 'Срок прошёл');
    if (n === 0) return tt('Бүгін соңғы күн', 'Сегодня последний день');
    return tt(`${n} күн қалды`, `Осталось ${n} ${plural(n, ['день', 'дня', 'дней'])}`);
  }
  return { lang, tt, L, tp, fDate, fDateY, daysLeftTxt, monS: MON_S[lang], wd: WD[lang] };
}
export type I18n = ReturnType<typeof i18n>;
