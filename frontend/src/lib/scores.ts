/* Spec 04 «Бағалар»: the student's own score and absence sheet in the Kazakhstan credit-system format (RK1, RK2,
   admission rating, exam, final, letter) and «what do I need on the exam». Device-only: nothing here is sent anywhere.
   Pure (no DOM, no storage); tested in lib/scores.test.ts.

   The formula below is the one widely used by KZ credit-system universities. It is NOT confirmed for KazATU yet:
   every weight, threshold and the letter table live in GRADING, so a different policy is a data change, and the
   screen says «бұл өз есебіңіз, ресми журнал емес». */

export interface ScoreEntry {
  /** «Пәндер» subject id (lib/subjects.ts), so the sheet follows the subject page */
  subject: string;
  /** the subject's name when the entry was last edited: the row survives a timetable change */
  name: string;
  rk1: number | null;
  rk2: number | null;
  exam: number | null;
  /** FX / пересдача: the latest attempt counts */
  retake?: number | null;
  absences: number;
  /** null = unknown: count only, no warning */
  absenceLimit: number | null;
  updated: number;
  /** 'import' rows (a future university journal) are read-only */
  source: 'self' | 'import';
}

export interface Grading {
  /** false until the university's academic policy is checked: the screens label the numbers as an estimate */
  confirmed: boolean;
  note: { kk: string; ru: string };
  /** admission rating R = rk1·w1 + rk2·w2 */
  rk1: number; rk2: number;
  /** final F = R·admission + E·exam */
  admission: number; exam: number;
  minAdmission: number; minExam: number; minFinal: number;
  /** letter and its lower bound (inclusive), best first; F is compared unrounded */
  letters: [string, number][];
}

export const GRADING: Grading = {
  confirmed: false,
  note: {
    kk: 'Формула ҚР кредиттік жүйесінде кең тараған: R = (АБ1 + АБ2) / 2, қорытынды = 0,6·R + 0,4·емтихан. Сейфуллин университетінің ережесімен әлі расталмаған.',
    ru: 'Формула, распространённая в кредитной системе РК: R = (РК1 + РК2) / 2, итог = 0,6·R + 0,4·экзамен. С правилами КАТУ им. Сейфуллина ещё не сверена.',
  },
  rk1: 0.5, rk2: 0.5,
  admission: 0.6, exam: 0.4,
  minAdmission: 50, minExam: 50, minFinal: 50,
  letters: [['A', 95], ['A-', 90], ['B+', 85], ['B', 80], ['B-', 75], ['C+', 70], ['C', 65], ['C-', 60], ['D+', 55], ['D', 50], ['F', 0]],
};

const EPS = 1e-9;
const round1 = (x: number) => Math.round(x * 10 + EPS) / 10;

/** parse a typed score: '' → null, 0–100 with at most one decimal (comma or dot), otherwise 'invalid' */
export function parseScore(raw: string): number | null | 'invalid' {
  const s = raw.trim().replace(',', '.');
  if (!s) return null;
  if (!/^\d{1,3}(\.\d)?$/.test(s)) return 'invalid';
  const n = Number(s);
  return n >= 0 && n <= 100 ? n : 'invalid';
}

/** the exam that counts: a retake replaces the first attempt */
export const examOf = (e: Pick<ScoreEntry, 'exam' | 'retake'>) => (e.retake ?? null) ?? e.exam;

/** admission rating; with one RK only, that RK is the partial value; null with neither */
export function admission(e: Pick<ScoreEntry, 'rk1' | 'rk2'>, g = GRADING): number | null {
  if (e.rk1 != null && e.rk2 != null) return e.rk1 * g.rk1 + e.rk2 * g.rk2;
  return e.rk1 ?? e.rk2 ?? null;
}
export const admissionPartial = (e: Pick<ScoreEntry, 'rk1' | 'rk2'>) => (e.rk1 == null) !== (e.rk2 == null);

/** final score, once both RKs and an exam are in */
export function final(e: Pick<ScoreEntry, 'rk1' | 'rk2' | 'exam' | 'retake'>, g = GRADING): number | null {
  const ex = examOf(e);
  if (e.rk1 == null || e.rk2 == null || ex == null) return null;
  return admission(e, g)! * g.admission + ex * g.exam;
}

export function letter(f: number, g = GRADING): string {
  return (g.letters.find(([, min]) => f >= min - EPS) ?? g.letters[g.letters.length - 1])[0];
}

/** the exam score needed for a letter (one decimal, rounded up); null when it is out of reach or the student is not admitted */
export function needFor(e: Pick<ScoreEntry, 'rk1' | 'rk2'>, l: string, g = GRADING): number | null {
  if (e.rk1 == null || e.rk2 == null) return null;
  const r = admission(e, g)!;
  if (r < g.minAdmission - EPS) return null;
  const min = g.letters.find(([x]) => x === l)?.[1];
  if (min == null) return null;
  const need = Math.max(g.minExam, Math.ceil(((Math.max(min, g.minFinal) - r * g.admission) / g.exam) * 10 - EPS) / 10);
  return need > 100 + EPS ? null : need;
}

/** «what do I need» for every passing letter, best first */
export function needTable(e: Pick<ScoreEntry, 'rk1' | 'rk2'>, g = GRADING): { letter: string; need: number | null }[] {
  return g.letters.filter(([, min]) => min >= g.minFinal).map(([l]) => ({ letter: l, need: needFor(e, l, g) }));
}

/** the best letter still within reach (never say «impossible» without it) */
export function nextReachable(e: Pick<ScoreEntry, 'rk1' | 'rk2'>, g = GRADING): { letter: string; need: number } | null {
  for (const row of needTable(e, g)) if (row.need != null) return { letter: row.letter, need: row.need };
  return null;
}

export type ScoreState = 'empty' | 'partial' | 'not_admitted' | 'awaiting_exam' | 'exam_failed' | 'failed' | 'passed';

export function scoreState(e: ScoreEntry, g = GRADING): ScoreState {
  if (e.rk1 == null && e.rk2 == null) return 'empty';
  if (admissionPartial(e)) return 'partial';
  if (admission(e, g)! < g.minAdmission - EPS) return 'not_admitted';
  const ex = examOf(e);
  if (ex == null) return 'awaiting_exam';
  if (ex < g.minExam - EPS) return 'exam_failed';
  return final(e, g)! < g.minFinal - EPS ? 'failed' : 'passed';
}

/** RK points missing for admission, one decimal */
export function admissionShortfall(e: Pick<ScoreEntry, 'rk1' | 'rk2'>, g = GRADING): number {
  const r = admission(e, g);
  return r == null ? g.minAdmission : Math.max(0, round1(g.minAdmission - r));
}

/** 'none' when the limit is unknown: count only, no warning */
export function absenceState(e: Pick<ScoreEntry, 'absences' | 'absenceLimit'>): 'none' | 'ok' | 'near' | 'limit' {
  if (e.absenceLimit == null || e.absenceLimit <= 0) return 'none';
  if (e.absences >= e.absenceLimit) return 'limit';
  return e.absences >= e.absenceLimit - 1 ? 'near' : 'ok';
}

export const fmtScore = (n: number | null) => (n == null ? '—' : String(round1(n)).replace(/\.0$/, ''));

export function emptyEntry(subject: string, name: string, now = Date.now()): ScoreEntry {
  return { subject, name, rk1: null, rk2: null, exam: null, retake: null, absences: 0, absenceLimit: null, updated: now, source: 'self' };
}

const score = (v: unknown) => (typeof v === 'number' && Number.isFinite(v) && v >= 0 && v <= 100 ? round1(v) : null);

/** stored scores (absent in data saved before the feature, or damaged) → a valid list */
export function cleanScores(raw: unknown): ScoreEntry[] {
  if (!Array.isArray(raw)) return [];
  const out: ScoreEntry[] = [];
  const seen = new Set<string>();
  for (const x of raw) {
    if (!x || typeof x !== 'object') continue;
    const r = x as Partial<ScoreEntry>;
    if (typeof r.subject !== 'string' || !r.subject || seen.has(r.subject)) continue;
    seen.add(r.subject);
    const abs = typeof r.absences === 'number' && Number.isInteger(r.absences) && r.absences >= 0 ? Math.min(r.absences, 999) : 0;
    const lim = typeof r.absenceLimit === 'number' && Number.isInteger(r.absenceLimit) && r.absenceLimit > 0 ? Math.min(r.absenceLimit, 999) : null;
    out.push({
      subject: r.subject, name: typeof r.name === 'string' ? r.name.slice(0, 120) : '',
      rk1: score(r.rk1), rk2: score(r.rk2), exam: score(r.exam), retake: score(r.retake),
      absences: abs, absenceLimit: lim, updated: typeof r.updated === 'number' ? r.updated : 0,
      source: r.source === 'import' ? 'import' : 'self',
    });
  }
  return out;
}

const fold = (s: string) => s.toLocaleLowerCase('ru').replace(/ё/g, 'е').trim();

/** GPA «Бағалардан толтыру»: every subject with a final sets the expected letter of its GPA row (matched by name),
    a subject without a row gets one; rows without a final are left as they are */
export function fillGpa(plan: { n: string; cr: number; g: string }[], scores: ScoreEntry[], names: (e: ScoreEntry) => string[], g = GRADING): { plan: { n: string; cr: number; g: string }[]; changed: number } {
  const next = plan.map(r => ({ ...r }));
  let changed = 0;
  for (const e of scores) {
    const f = final(e, g);
    if (f == null) continue;
    const l = letter(f, g);
    const ns = names(e).map(fold).filter(n => n.length >= 2);
    const row = next.find(r => ns.some(n => fold(r.n).includes(n)));
    if (row) { if (row.g !== l) { row.g = l; changed++; } } else { next.push({ n: e.name || names(e)[0] || e.subject, cr: 5, g: l }); changed++; }
  }
  return { plan: next, changed };
}
