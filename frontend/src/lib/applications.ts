/* Spec 06 «Менің өтінімдерім»: one list of everything the student applied to — external vacancies marked on the device
   and in-app applications from GET /api/applications/my — with a stage, a follow-up date and a private note.
   Device-only: the note and follow-up never leave the device. Pure; tested in lib/applications.test.ts. */
import type { AppStatus, MyApplication as ServerApplication } from '@/lib/b2b';

type Tr = { kk: string; ru: string };
type Ymd = string;

/** the student's own stages, in the order an application moves */
export type Stage = 'sent' | 'interview' | 'offer' | 'rejected' | 'withdrawn';
export const STAGES: Stage[] = ['sent', 'interview', 'offer', 'rejected', 'withdrawn'];
export const STAGE_LABEL: Record<Stage, Tr> = {
  sent: { kk: 'Жіберілді', ru: 'Отправлено' },
  interview: { kk: 'Сұхбат', ru: 'Собеседование' },
  offer: { kk: 'Ұсыныс', ru: 'Оффер' },
  rejected: { kk: 'Бас тарту', ru: 'Отказ' },
  withdrawn: { kk: 'Кері алынды', ru: 'Отозвано' },
};
export const NOTE_MAX = 500;

export interface TrackedApp {
  /** 'job:<id>' | 'opp:<id>' */
  ref: string;
  /** copied when marked, so the row survives the vacancy leaving the feed */
  title: Tr;
  company: string;
  stage: Stage;
  appliedAt: number;
  followUp: Ymd | '';
  note: string;
  /** the in-app application's id: its stage comes from the server */
  server?: number;
}

const isYmd = (s: unknown): s is Ymd => typeof s === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(s) && !Number.isNaN(Date.parse(s + 'T00:00:00Z'));
const str = (v: unknown, max: number) => (typeof v === 'string' ? v.slice(0, max) : '');

/** stored list (absent before the feature) + the old S.applied {jobId: ts} marks → a valid list, one row per ref.
    Old marks become stage «sent» with their timestamp; the title is filled from the feed when shown. */
export function cleanApplications(raw: unknown, applied: unknown): TrackedApp[] {
  const out: TrackedApp[] = [];
  const seen = new Set<string>();
  for (const x of Array.isArray(raw) ? raw : []) {
    if (!x || typeof x !== 'object') continue;
    const r = x as Partial<TrackedApp>;
    if (typeof r.ref !== 'string' || !/^(job|opp):\S{1,80}$/.test(r.ref) || seen.has(r.ref)) continue;
    seen.add(r.ref);
    const t = r.title && typeof r.title === 'object' ? r.title : { kk: '', ru: '' };
    out.push({
      ref: r.ref, title: { kk: str(t.kk, 200), ru: str(t.ru, 200) }, company: str(r.company, 200),
      stage: STAGES.includes(r.stage as Stage) ? r.stage as Stage : 'sent',
      appliedAt: typeof r.appliedAt === 'number' && r.appliedAt > 0 ? r.appliedAt : 0,
      followUp: isYmd(r.followUp) ? r.followUp : '', note: str(r.note, NOTE_MAX),
      ...(typeof r.server === 'number' && r.server > 0 ? { server: r.server } : {}),
    });
  }
  if (applied && typeof applied === 'object') {
    for (const [id, ts] of Object.entries(applied as Record<string, unknown>)) {
      const ref = 'job:' + id;
      if (seen.has(ref) || !/^\S{1,80}$/.test(id)) continue;
      seen.add(ref);
      out.push({ ref, title: { kk: '', ru: '' }, company: '', stage: 'sent', appliedAt: typeof ts === 'number' ? ts : 0, followUp: '', note: '' });
    }
  }
  return out;
}

/** «Өтінім бердім» on an external vacancy: add it (or keep the existing row) */
export function markApplied(list: TrackedApp[], item: { ref: string; title: Tr; company: string }, now: number, extra: { followUp?: Ymd | ''; note?: string } = {}): TrackedApp[] {
  if (list.some(a => a.ref === item.ref)) return list;
  return [...list, { ref: item.ref, title: item.title, company: item.company, stage: 'sent', appliedAt: now, followUp: extra.followUp ?? '', note: (extra.note ?? '').slice(0, NOTE_MAX) }];
}

/** the server's statuses, as the student reads them; the stage they imply in our list */
export const SERVER_STAGE: Record<AppStatus, Stage> = {
  sent: 'sent', viewed: 'sent', invited: 'interview', rejected: 'rejected', no_response: 'rejected', withdrawn: 'withdrawn',
};

/** one row of the list: a local mark, an in-app application, or both joined by vacancy */
export interface AppRow {
  ref: string;
  title: Tr;
  company: string;
  stage: Stage;
  /** set for in-app applications: shown as their own label, and the stage cannot be moved here */
  serverStatus?: AppStatus;
  serverId?: number;
  appliedAt: number;
  followUp: Ymd | '';
  note: string;
}

const tsOf = (iso: string) => { const t = Date.parse(iso); return Number.isNaN(t) ? 0 : t; };

/** local marks + server applications; the server wins on the stage of an in-app application */
export function mergeApps(local: TrackedApp[], server: ServerApplication[] | null): AppRow[] {
  const byRef = new Map(local.map(a => [a.ref, a]));
  const rows: AppRow[] = [];
  for (const s of server ?? []) {
    const ref = 'job:' + s.jobId;
    const mine = byRef.get(ref);
    byRef.delete(ref);
    rows.push({
      ref, title: { kk: s.title, ru: s.titleRu || s.title }, company: s.company, stage: SERVER_STAGE[s.status] ?? 'sent',
      serverStatus: s.status, serverId: s.id, appliedAt: tsOf(s.createdAt), followUp: mine?.followUp ?? '', note: mine?.note ?? '',
    });
  }
  for (const a of byRef.values()) rows.push({ ref: a.ref, title: a.title, company: a.company, stage: a.stage, appliedAt: a.appliedAt, followUp: a.followUp, note: a.note });
  return rows;
}

/** the student's own stages can be moved only on rows the server does not own */
export const canMove = (r: Pick<AppRow, 'serverStatus'>) => !r.serverStatus;

export type Section = 'active' | 'offer' | 'closed';
export const sectionOf = (s: Stage): Section => (s === 'offer' ? 'offer' : s === 'rejected' || s === 'withdrawn' ? 'closed' : 'active');

/** sorted by the next action: follow-up dates first (soonest first), then the newest applications */
export function byNextAction(rows: AppRow[]): AppRow[] {
  return [...rows].sort((a, b) => {
    if (a.followUp && b.followUp) return a.followUp.localeCompare(b.followUp) || b.appliedAt - a.appliedAt;
    if (a.followUp || b.followUp) return a.followUp ? -1 : 1;
    return b.appliedAt - a.appliedAt;
  });
}

/** change one row's stage, follow-up or note; an in-app row gets a local record for its note and follow-up */
export function editApp(list: TrackedApp[], row: AppRow, patch: Partial<Pick<TrackedApp, 'stage' | 'followUp' | 'note'>>): TrackedApp[] {
  const clean = {
    ...(patch.stage && canMove(row) ? { stage: patch.stage } : {}),
    ...(patch.followUp !== undefined ? { followUp: isYmd(patch.followUp) ? patch.followUp : '' } : {}),
    ...(patch.note !== undefined ? { note: patch.note.slice(0, NOTE_MAX) } : {}),
  };
  if (list.some(a => a.ref === row.ref)) return list.map(a => (a.ref === row.ref ? { ...a, ...clean } : a));
  return [...list, { ref: row.ref, title: row.title, company: row.company, stage: row.stage, appliedAt: row.appliedAt, followUp: '', note: '', ...(row.serverId ? { server: row.serverId } : {}), ...clean }];
}
