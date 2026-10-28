/* Offline copy of the student's e-tickets (DB events, prompt 05). Whatever the API already returned to this device
   — the event page (GET /api/events/{id}) and «Менің тізімім» (GET /api/events/my) — is kept in localStorage so the
   ticket and its QR open without internet. The API calls themselves are unchanged; this only remembers answers.
   Cleared on sign-out, account deletion, «delete all data», or when another account signs in on the device. */
import type { DbEvent, MyReg } from '@/lib/events';

export const TICKETS_KEY = 'studenthub.tickets';
/** a finished event's ticket is not useful for long */
const KEEP_AFTER_END_MS = 2 * 24 * 3600_000;

export interface SavedTicket { event: DbEvent; savedAt: string }
interface Stored { v: 1; owner: number | null; events: Record<string, SavedTicket> }

const empty = (owner: number | null = null): Stored => ({ v: 1, owner, events: {} });

function read(): Stored {
  try {
    const s = JSON.parse(window.localStorage.getItem(TICKETS_KEY) || 'null') as Stored | null;
    if (s && s.v === 1 && s.events && typeof s.events === 'object') return s;
  } catch { /* corrupted or no storage */ }
  return empty();
}
function write(s: Stored) {
  try { window.localStorage.setItem(TICKETS_KEY, JSON.stringify(s)); } catch { /* storage full or unavailable */ }
}

const valid = (e: DbEvent | undefined): e is DbEvent & { my: MyReg } =>
  !!e && typeof e.id === 'number' && !!e.my && typeof e.my.ticket === 'string' && (e.my.status === 'registered' || e.my.status === 'waitlist');
const fresh = (e: DbEvent, now = Date.now()) => new Date(e.ends_at).getTime() + KEEP_AFTER_END_MS > now;

/** the stored set for this account (another account's tickets never mix in) */
function forOwner(owner: number | null | undefined): Stored {
  const s = read();
  if (owner != null && s.owner !== owner) return empty(owner);
  return s;
}

/** one event as the API returned it: kept when the student is registered or waitlisted, dropped otherwise */
export function rememberEvent(e: DbEvent, owner: number | null | undefined) {
  const s = forOwner(owner);
  const k = String(e.id);
  if (valid(e) && fresh(e)) s.events[k] = { event: e, savedAt: new Date().toISOString() };
  else if (owner != null) delete s.events[k]; // signed in and not registered (any more)
  else return;
  write(s);
}

/** GET /api/events/my answered: it is the complete list, so it replaces what was kept */
export function rememberMine(events: DbEvent[], owner: number) {
  const s = empty(owner);
  const at = new Date().toISOString();
  for (const e of events) if (valid(e) && fresh(e)) s.events[String(e.id)] = { event: e, savedAt: at };
  write(s);
}

export function savedTicket(id: string | number): SavedTicket | null {
  const t = read().events[String(id)];
  return t && valid(t.event) ? t : null;
}

export function savedTickets(): SavedTicket[] {
  const now = Date.now();
  return Object.values(read().events)
    .filter(t => valid(t.event) && fresh(t.event, now))
    .sort((a, b) => a.event.starts_at.localeCompare(b.event.starts_at));
}

export function clearTickets() {
  try { window.localStorage.removeItem(TICKETS_KEY); } catch { /* ignore */ }
}

/** a row of GET /api/events/my as an event with its `my` part filled in (the row carries it next to the event) */
export function withMy(r: { event: DbEvent; status?: MyReg['status']; ticket?: string; checked_in_at?: string | null; team_no?: number | null; feedback_score?: number | null }): DbEvent {
  if (r.event.my || !r.status || !r.ticket) return r.event;
  return { ...r.event, my: { status: r.status, ticket: r.ticket, checked_in_at: r.checked_in_at ?? null, team_no: r.team_no ?? null, feedback_score: r.feedback_score ?? null } };
}
