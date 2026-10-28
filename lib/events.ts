/* Events (prompt 05) on the client: API shapes (docs/api/events.md, the Go code wins) and the pure list helpers
   that merge DB events with the legacy demo EVENTS. Kept free of React so a search screen can reuse them. */
import { EVENT_CATS } from '@/lib/data/static';
import type { EventItem, LText } from '@/lib/types';

export interface MyReg { status: 'registered' | 'waitlist'; ticket: string; checked_in_at: string | null; team_no: number | null; feedback_score: number | null }
export interface DbEvent {
  id: number; title_kk: string; title_ru: string | null; desc_kk: string | null; desc_ru: string | null; cat: string;
  starts_at: string; ends_at: string; university: string; building_id: string | null; place_text: string | null;
  capacity: number | null; walk_in_allowed: boolean; gives_cert: boolean; teams_enabled: boolean;
  status: 'draft' | 'published' | 'finished' | 'cancelled'; created_by: number | null; checkin_started_at: string | null;
  finished_at: string | null; created_at: string; registered: number; waitlist: number; checked_in: number;
  spots_left: number | null; my?: MyReg; can_manage: boolean;
}
export interface LiveView {
  token: string; deep_link: string; web_link: string; rotates_in_sec: number; open: boolean;
  registered: number; checked_in: number; waitlist: number; notice_kk: string; notice_ru: string;
}
export interface RegRow {
  user_id: number; name: string | null; username: string | null; faculty: string | null; course: number | null;
  status: 'registered' | 'waitlist'; ticket_short: string; registered_at: string; checked_in_at: string | null;
  checkin_method: string | null; checkin_revoked: boolean; team_no: number | null; feedback_score: number | null;
}
export interface RegsResp { registered: number; checked_in: number; waitlist: number; regs: RegRow[] }
export interface CheckinResult { status: 'checked_in' | 'already'; team_no: number | null; method: 'qr' | 'walk_in' | 'manual'; message: string }
export interface FeedbackSummary { count: number; average: number | null; distribution: number[]; comments: { score: number; text: string; at: string }[] }
export interface Attendance { event_id: number; title_kk: string; title_ru: string | null; cat: string; university: string; starts_at: string; checked_in_at: string; has_certificate: boolean; verified: boolean }

/** every category the API accepts; the first seven are the legacy demo ones */
export const ALL_EVENT_CATS: Record<string, LText> = {
  ...EVENT_CATS,
  mentoring: { kk: 'Топтық менторлық', ru: 'Групповое менторство' },
  other: { kk: 'Басқа', ru: 'Другое' },
};

export const evTitle = (e: Pick<DbEvent, 'title_kk' | 'title_ru'>, lang: 'kk' | 'ru') => (lang === 'ru' && e.title_ru ? e.title_ru : e.title_kk);
export const evDesc = (e: Pick<DbEvent, 'desc_kk' | 'desc_ru'>, lang: 'kk' | 'ru') => (lang === 'ru' && e.desc_ru ? e.desc_ru : e.desc_kk || '');

export type EventListItem = { kind: 'db'; e: DbEvent } | { kind: 'demo'; e: EventItem };

/** one event list for the «Іс-шара» tab: DB events (upcoming, by start) and — when the database has none, or the
    student is not signed in — the demo EVENTS marked «үлгі». `cat` 'all' = every category; `q` = free text. */
export function eventListItems(db: DbEvent[] | null, demo: EventItem[], o: { signedIn: boolean; cat: string; q: string; lang: 'kk' | 'ru' }): EventListItem[] {
  const q = o.q.trim().toLowerCase();
  const L = (t: LText) => t[o.lang] || t.kk;
  const out: EventListItem[] = [];
  for (const e of db || []) {
    if (o.cat !== 'all' && e.cat !== o.cat) continue;
    if (q && ![evTitle(e, o.lang), e.place_text || '', evDesc(e, o.lang)].join(' ').toLowerCase().includes(q)) continue;
    out.push({ kind: 'db', e });
  }
  if (!db || !db.length || !o.signedIn) {
    demo.filter(e => !e.past && (o.cat === 'all' || e.cat === o.cat) && (!q || [L(e.title), L(e.place), L(e.desc)].join(' ').toLowerCase().includes(q)))
      .sort((a, b) => a.day - b.day)
      .forEach(e => out.push({ kind: 'demo', e }));
  }
  return out;
}

/** roles that may create events (prompt 05; an approved mentor only manages mentoring meetings it already has) */
export const canCreateEvents = (role: string | undefined) => role === 'admin' || role === 'ambassador' || role === 'university_staff';

/** the check-in token from a scanned link: ?startapp=ci_…, #/checkin?t=ci_… or the bare token */
export function checkinToken(s: string): string {
  const m = /(?:startapp=|[?&]t=|^)(ci_[A-Za-z0-9_-]{8,64})/.exec(s.trim());
  return m ? m[1] : '';
}
export const CHECKIN_KEY = 'studenthub.checkin';
export const CLAIM_KEY = 'studenthub.claim';

/** has the moment passed (read at render time; a list re-renders on every load, so a few seconds' drift is fine) */
export const isPast = (iso: string) => new Date(iso).getTime() <= Date.now();
