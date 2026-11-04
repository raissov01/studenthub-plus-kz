/* «Сабақ кестесі»: the student's own weekly timetable, stored on the device (AppState.timetable). Pure: no DOM,
   no storage. It feeds the home «Келесі сабақ» card, the calendar «Сабақтар» layer and the «Пәндер» subjects. */
import type { ClassItem, TimetableEntry } from '@/lib/types';

const HM = /^([01]\d|2[0-3]):[0-5]\d$/;
export const isHm = (s: unknown): s is string => typeof s === 'string' && HM.test(s);
export const toMin = (t: string) => { const [h, m] = t.split(':').map(Number); return h * 60 + m; };
/** Monday = 0 … Sunday = 6 for a local date */
export const weekdayOf = (d: Date) => (d.getDay() + 6) % 7;

export const MAX_ENTRIES = 80;
export const NAME_MAX = 120;
export const ROOM_MAX = 40;
export const KIND_MAX = 40;

/** stored timetable (absent in data saved before the feature, or edited by hand) -> valid entries, sorted by day and time */
export function cleanTimetable(raw: unknown): TimetableEntry[] {
  if (!Array.isArray(raw)) return [];
  const out: TimetableEntry[] = [];
  const seen = new Set<string>();
  for (const r of raw) {
    if (!r || typeof r !== 'object') continue;
    const x = r as Partial<TimetableEntry>;
    const name = typeof x.name === 'string' ? x.name.trim().slice(0, NAME_MAX) : '';
    if (typeof x.id !== 'string' || !x.id || seen.has(x.id) || !name) continue;
    if (!Number.isInteger(x.wd) || x.wd! < 0 || x.wd! > 6 || !isHm(x.t)) continue;
    const end = isHm(x.end) && toMin(x.end) > toMin(x.t) ? x.end : addMinutes(x.t, 50);
    seen.add(x.id);
    out.push({
      id: x.id, name, wd: x.wd!, t: x.t, end,
      room: typeof x.room === 'string' ? x.room.trim().slice(0, ROOM_MAX) : '',
      kind: typeof x.kind === 'string' ? x.kind.trim().slice(0, KIND_MAX) : '',
    });
    if (out.length >= MAX_ENTRIES) break;
  }
  return sortTimetable(out);
}

export const sortTimetable = (list: TimetableEntry[]) => list.slice().sort((a, b) => a.wd - b.wd || toMin(a.t) - toMin(b.t));

export function addMinutes(t: string, n: number): string {
  const m = Math.min(23 * 60 + 59, toMin(t) + n);
  return `${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`;
}

/** the classes of one weekday (Monday = 0), by start time */
export function classesOn(list: TimetableEntry[], wd: number): ClassItem[] {
  return list.filter(e => e.wd === wd).sort((a, b) => toMin(a.t) - toMin(b.t))
    .map(e => ({ t: e.t, end: e.end, name: e.name, room: e.room, kind: e.kind }));
}

/** today's class that has not ended yet (the one in progress, or the next one); null when the day is over or empty */
export function nextClass(list: TimetableEntry[], now = new Date()): ClassItem | null {
  const mins = now.getHours() * 60 + now.getMinutes();
  return classesOn(list, weekdayOf(now)).find(c => toMin(c.end) > mins) || null;
}

/** an entry for a form's values, or the reason it is not valid ('name' | 'time') */
export function makeEntry(f: { name: string; wd: number; t: string; end: string; room: string; kind: string }, id: string): TimetableEntry | 'name' | 'time' {
  const name = f.name.trim().slice(0, NAME_MAX);
  if (!name) return 'name';
  if (!isHm(f.t) || (f.end && (!isHm(f.end) || toMin(f.end) <= toMin(f.t)))) return 'time';
  if (!Number.isInteger(f.wd) || f.wd < 0 || f.wd > 6) return 'time';
  return {
    id, name, wd: f.wd, t: f.t, end: f.end || addMinutes(f.t, 50),
    room: f.room.trim().slice(0, ROOM_MAX), kind: f.kind.trim().slice(0, KIND_MAX),
  };
}
