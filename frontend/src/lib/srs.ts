/* Spaced repetition for the flashcard decks: pure scheduler, no DOM, no storage.

   Algorithm: SM-2 (SuperMemo 2, 1987) in the four-button form Anki uses, chosen over Leitner because it adapts the
   gap per card (an «ease» factor) instead of moving cards between a few fixed boxes, and the four answers map to the
   «Қайта / Қиын / Жақсы / Оңай» buttons directly.
   - Every card starts «new» (no state): it is due at once. New cards per session are capped (NEW_PER_DAY).
   - again: the card is forgotten. reps = 0, ease − 0.20, interval 0 → due again today (and later in the same session).
   - hard:  ease − 0.15, interval × 1.2 (1 day for a new card).
   - good:  2 days for a new card, then interval × ease.
   - easy:  ease + 0.15, a longer step than good (4 days for a new card, then interval × ease × 1.3).
   Ease never drops below 1.3; hard < good < easy always holds; no interval exceeds MAX_DAYS.
   Days are Almaty calendar days 'YYYY-MM-DD' (lib/calendar), so «today» does not depend on the device zone. */
import { addDays, type Ymd } from './calendar';

export type Grade = 'again' | 'hard' | 'good' | 'easy';
export const GRADES: Grade[] = ['again', 'hard', 'good', 'easy'];

export interface SrState {
  /** successful reviews in a row */
  reps: number;
  /** days until the next review (0 = again today) */
  interval: number;
  /** SM-2 easiness factor, 1.3 … */
  ease: number;
  /** next review day */
  due: Ymd;
  /** times the card was forgotten */
  lapses: number;
  /** day of the last review */
  last: Ymd;
}

export const START_EASE = 2.5;
export const MIN_EASE = 1.3;
export const MAX_DAYS = 365;
export const NEW_PER_DAY = 20;

const round2 = (n: number) => Math.round(n * 100) / 100;
const cap = (n: number) => Math.min(MAX_DAYS, Math.max(1, Math.round(n)));

/** the intervals (days) each answer would give, for the button hints; again is always 0 */
export function previewIntervals(s: SrState | undefined): Record<Grade, number> {
  const reps = s?.reps ?? 0, interval = s?.interval ?? 0, ease = s?.ease ?? START_EASE;
  const base = Math.max(interval, 1);
  const hard = reps === 0 ? 1 : cap(base * 1.2);
  let good = reps === 0 ? 2 : cap(base * ease);
  let easy = reps === 0 ? 4 : cap(base * (ease + 0.15) * 1.3);
  good = Math.min(MAX_DAYS, Math.max(good, hard + (hard < MAX_DAYS ? 1 : 0)));
  easy = Math.min(MAX_DAYS, Math.max(easy, good + (good < MAX_DAYS ? 1 : 0)));
  return { again: 0, hard, good, easy };
}

/** the card's state after one answer on `today` */
export function review(s: SrState | undefined, grade: Grade, today: Ymd): SrState {
  const prev: SrState = s ?? { reps: 0, interval: 0, ease: START_EASE, due: today, lapses: 0, last: today };
  const days = previewIntervals(s)[grade];
  let ease = prev.ease;
  if (grade === 'again') ease -= 0.2;
  else if (grade === 'hard') ease -= 0.15;
  else if (grade === 'easy') ease += 0.15;
  ease = round2(Math.max(MIN_EASE, ease));
  if (grade === 'again') return { reps: 0, interval: 0, ease, due: today, lapses: prev.lapses + 1, last: today };
  return { reps: prev.reps + 1, interval: days, ease, due: addDays(today, days), lapses: prev.lapses, last: today };
}

/** a never-reviewed card is due; otherwise when its day has come */
export const isDue = (s: SrState | undefined, today: Ymd) => !s || s.due <= today;

/** stable key of a card inside a deck (cards have no ids): the deck id and a hash of question and answer.
    Editing a card's text starts it over, which is what the student expects after rewriting it. */
export function cardKey(deckId: string, q: string, a: string): string {
  let h = 0x811c9dc5;
  const s = q.trim() + '\u0001' + a.trim();
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return `${deckId}:${h.toString(36)}`;
}

export interface QueueCard { key: string; deckId: string; q: string; a: string; isNew: boolean }

/** today's queue: cards already learnt that are due (the most overdue first), then new cards up to `newLimit`.
    Duplicate cards (same text in one deck) appear once. */
export function dueQueue(
  decks: { id: string; cards: { q: string; a: string }[] }[],
  states: Record<string, SrState | undefined>,
  today: Ymd,
  newLimit = NEW_PER_DAY,
): QueueCard[] {
  const old: (QueueCard & { due: Ymd })[] = [], fresh: QueueCard[] = [];
  const seen = new Set<string>();
  for (const d of decks) {
    for (const c of d.cards) {
      if (!c.q.trim() || !c.a.trim()) continue;
      const key = cardKey(d.id, c.q, c.a);
      if (seen.has(key)) continue;
      seen.add(key);
      const st = states[key];
      if (!st) fresh.push({ key, deckId: d.id, q: c.q, a: c.a, isNew: true });
      else if (st.due <= today) old.push({ key, deckId: d.id, q: c.q, a: c.a, isNew: false, due: st.due });
    }
  }
  old.sort((a, b) => a.due.localeCompare(b.due));
  return [...old.map(c => ({ key: c.key, deckId: c.deckId, q: c.q, a: c.a, isNew: false })), ...fresh.slice(0, Math.max(0, newLimit))];
}

/** a stored value -> a valid state, or undefined */
export function cleanState(raw: unknown): SrState | undefined {
  if (!raw || typeof raw !== 'object') return undefined;
  const x = raw as Partial<SrState>;
  const ymd = (v: unknown): v is Ymd => typeof v === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(v);
  if (!ymd(x.due)) return undefined;
  const n = (v: unknown, d: number) => (typeof v === 'number' && Number.isFinite(v) && v >= 0 ? v : d);
  return {
    reps: Math.floor(n(x.reps, 0)), interval: Math.min(MAX_DAYS, Math.floor(n(x.interval, 0))),
    ease: Math.max(MIN_EASE, n(x.ease, START_EASE)), due: x.due, lapses: Math.floor(n(x.lapses, 0)), last: ymd(x.last) ? x.last : x.due,
  };
}
