/* «Пәндер»: the subject list and what belongs to a subject. Pure (no DOM, no storage).
   Subjects come from the student's own class timetable («Сабақ кестесі», lib/timetable.ts); «Пән қосу» adds others.
   Everything else links to a subject by its id, or — for data that predates subjects (decks, deadlines, exam plans,
   the GPA list) — by the subject's name appearing in its title. */

import type { TimetableEntry } from '@/lib/types';

export interface SubjectMeta {
  id: string;
  /** own subject's name (timetable subjects take theirs from the timetable) */
  name: string;
  custom: boolean;
  /** exam day 'YYYY-MM-DD' or '' */
  exam: string;
  /** a timetable subject the student hid */
  hidden?: boolean;
}

/** wd: weekday, Monday = 0 … Sunday = 6 */
export interface ClassSlot { t: string; end: string; room: string; kind: string; wd: number }
export interface Subject {
  id: string;
  name: string;
  /** the names links are matched by (a timetable subject: its one name) */
  names: string[];
  custom: boolean;
  exam: string;
  /** the weekly classes of this subject */
  slots: ClassSlot[];
}

/** a stable ASCII id from a timetable subject's name */
export function subjectId(nameKk: string): string {
  let h = 5381;
  for (const ch of nameKk.trim().toLowerCase()) h = (Math.imul(h, 33) ^ ch.codePointAt(0)!) >>> 0;
  return 's' + h.toString(36);
}

export function subjectsFrom(
  schedule: Pick<TimetableEntry, 't' | 'end' | 'name' | 'room' | 'kind' | 'wd'>[],
  meta: SubjectMeta[],
): Subject[] {
  const byId = new Map(meta.map(m => [m.id, m]));
  const out: Subject[] = [];
  const seen = new Map<string, Subject>();
  for (const c of schedule) {
    const name = c.name.trim();
    if (!name) continue;
    const id = subjectId(name);
    let s = seen.get(id);
    if (!s) {
      const m = byId.get(id);
      if (m?.hidden) continue;
      s = { id, name, names: [name], custom: false, exam: m?.exam ?? '', slots: [] };
      seen.set(id, s);
      out.push(s);
    }
    s.slots.push({ t: c.t, end: c.end, room: c.room, kind: c.kind, wd: c.wd });
  }
  for (const m of meta) {
    if (!m.custom || m.hidden || !m.name.trim() || seen.has(m.id)) continue;
    out.push({ id: m.id, name: m.name.trim(), names: [m.name.trim()], custom: true, exam: m.exam, slots: [] });
  }
  return out;
}

const fold = (s: string) => s.toLocaleLowerCase('ru').replace(/ё/g, 'е').trim();
/** does a free text (a deadline title, a deck name, a plan subject) mention the subject? */
export function mentions(s: Pick<Subject, 'names'>, text: string): boolean {
  const t = fold(text);
  return !!t && s.names.some(n => { const f = fold(n); return f.length >= 2 && t.includes(f); });
}

/** an item linked by id (`courseId`/`subject`, if the data has it) or by naming the subject */
export function belongs(s: Pick<Subject, 'id' | 'names'>, item: { courseId?: unknown; subject?: unknown }, text: string): boolean {
  if (typeof item.courseId === 'string' && item.courseId) return item.courseId === s.id;
  if (typeof item.subject === 'string' && item.subject) return item.subject === s.id;
  return mentions(s, text);
}

const toMin = (t: string) => { const [h, m] = t.split(':').map(Number); return h * 60 + m; };

/** the subject's next class from `now` (local clock), each slot on its own weekday */
export function nextSlot(s: Pick<Subject, 'slots'>, now: Date): { slot: ClassSlot; at: Date; today: boolean } | null {
  if (!s.slots.length) return null;
  const sorted = s.slots.slice().sort((a, b) => toMin(a.t) - toMin(b.t));
  for (let d = 0; d < 8; d++) {
    const day = new Date(now.getFullYear(), now.getMonth(), now.getDate() + d);
    const wd = (day.getDay() + 6) % 7;
    const mins = d === 0 ? now.getHours() * 60 + now.getMinutes() : -1;
    const slot = sorted.find(x => x.wd === wd && toMin(x.end) > mins);
    if (!slot) continue;
    const [h, m] = slot.t.split(':').map(Number);
    return { slot, at: new Date(day.getFullYear(), day.getMonth(), day.getDate(), h, m), today: d === 0 };
  }
  return null;
}

/** stored metadata -> valid entries, one per id */
export function cleanMeta(raw: unknown): SubjectMeta[] {
  if (!Array.isArray(raw)) return [];
  const out: SubjectMeta[] = [];
  const seen = new Set<string>();
  for (const r of raw) {
    if (!r || typeof r !== 'object') continue;
    const x = r as Partial<SubjectMeta>;
    if (typeof x.id !== 'string' || !x.id || seen.has(x.id)) continue;
    seen.add(x.id);
    out.push({
      id: x.id, name: typeof x.name === 'string' ? x.name.slice(0, 120) : '', custom: !!x.custom,
      exam: typeof x.exam === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(x.exam) ? x.exam : '', ...(x.hidden ? { hidden: true } : {}),
    });
  }
  return out;
}
