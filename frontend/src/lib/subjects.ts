/* «Пәндер»: the subject list and what belongs to a subject. Pure (no DOM, no storage).
   Subjects come from the class timetable automatically (the student builds nothing); «Пән қосу» adds own ones.
   Everything else links to a subject by its id, or — for data that predates subjects (decks, deadlines, exam plans,
   the GPA list) — by the subject's name appearing in its title. */

type Tr = { kk: string; ru: string };
type L2 = 'kk' | 'ru';

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

export interface ClassSlot { t: string; end: string; room: string; kind: Tr }
export interface Subject {
  id: string;
  name: string;
  /** both names, so links match whichever language the data was written in */
  names: string[];
  custom: boolean;
  exam: string;
  /** weekday classes of this subject (the sample timetable repeats Monday–Friday) */
  slots: ClassSlot[];
}

/** a stable ASCII id from a timetable subject's Kazakh name */
export function subjectId(nameKk: string): string {
  let h = 5381;
  for (const ch of nameKk.trim().toLowerCase()) h = (Math.imul(h, 33) ^ ch.codePointAt(0)!) >>> 0;
  return 's' + h.toString(36);
}

export function subjectsFrom(
  schedule: { t: string; end: string; name: Tr; room: string; kind: Tr }[],
  meta: SubjectMeta[],
  lang: L2,
): Subject[] {
  const byId = new Map(meta.map(m => [m.id, m]));
  const out: Subject[] = [];
  const seen = new Map<string, Subject>();
  for (const c of schedule) {
    const id = subjectId(c.name.kk);
    let s = seen.get(id);
    if (!s) {
      const m = byId.get(id);
      if (m?.hidden) continue;
      s = { id, name: c.name[lang], names: [c.name.kk, c.name.ru], custom: false, exam: m?.exam ?? '', slots: [] };
      seen.set(id, s);
      out.push(s);
    }
    s.slots.push({ t: c.t, end: c.end, room: c.room, kind: c.kind });
  }
  for (const m of meta) {
    if (!m.custom || m.hidden || !m.name.trim()) continue;
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

/** the subject's next class from `now` (local clock, the sample timetable runs Monday–Friday) */
export function nextSlot(s: Pick<Subject, 'slots'>, now: Date): { slot: ClassSlot; at: Date; today: boolean } | null {
  if (!s.slots.length) return null;
  const sorted = s.slots.slice().sort((a, b) => toMin(a.t) - toMin(b.t));
  for (let d = 0; d < 8; d++) {
    const day = new Date(now.getFullYear(), now.getMonth(), now.getDate() + d);
    const wd = day.getDay();
    if (wd === 0 || wd === 6) continue;
    const mins = d === 0 ? now.getHours() * 60 + now.getMinutes() : -1;
    const slot = sorted.find(x => toMin(x.end) > mins);
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
