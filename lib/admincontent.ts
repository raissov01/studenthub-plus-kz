/* Admin content editor (docs/api/admin-content.md): the form state of one opportunity / job / discount, its conversion to
   and from the data/*.json record the API stores, the same rules the server runs (pkg/content, Sheets rules) so the form
   can mark a field before the round trip, and the record the card/detail preview renders. Pure: no React, no fetch. */
import type { Discount, Job, LText, Opportunity } from './types';

export const KINDS = ['opportunities', 'jobs', 'discounts'] as const;
export type Kind = (typeof KINDS)[number];
export const isKind = (k: unknown): k is Kind => typeof k === 'string' && (KINDS as readonly string[]).includes(k);

/* the server's lists (pkg/content/schema.go) */
export const OPP_TYPE_CODES = ['mobility', 'intl', 'company', 'grant'];
export const JOB_PER = ['shift', 'month', 'hour'];
export const EN_LEVELS = ['A1', 'A2', 'B1', 'B2', 'C1', 'C2'];
export const DEAL_CAT_CODES = ['food', 'cinema', 'books', 'sport', 'clothes', 'lang', 'edu', 'cowork', 'print'];
export const CITY_CODES = ['astana', 'almaty', 'shymkent', 'karaganda', 'aktobe', 'taraz', 'turkistan', 'pavlodar', 'oskemen', 'semey',
  'kostanay', 'atyrau', 'aktau', 'oral', 'kyzylorda', 'petropavl', 'taldykorgan', 'kokshetau', 'other', 'remote'];
export const SPEC_CODES = ['all', 'econ', 'acc', 'it', 'agro', 'vet', 'eng', 'energy'];
export const JOB_TAG_CODES = ['noexp', 'daily', 'remote', 'near'];

export interface Bi { kk: string; ru: string }
const bi = (v: unknown): Bi => {
  const o = (v && typeof v === 'object' ? v : {}) as Record<string, unknown>;
  return { kk: typeof o.kk === 'string' ? o.kk : '', ru: typeof o.ru === 'string' ? o.ru : '' };
};
const EMPTY: Bi = { kk: '', ru: '' };

export interface Form {
  kind: Kind;
  status: 'published' | 'hidden';
  /** title (name for a discount) */
  title: Bi;
  org: Bi; desc: Bi; sched: Bi; what: Bi; cond: Bi;
  city: string;
  source_url: string; posted_at: string; verified_at: string; verified_by: string;
  phone: string; email: string; site: string;
  deadline: string; noDeadline: boolean;
  // opportunities
  type: string; funded: boolean; course: string; gpa: string; en: string; reqSkills: string[]; spec: string[];
  docs: Bi[]; steps: Bi[];
  // jobs
  today: boolean; pay: string; per: string; hours: string; tags: string[]; skills: string[]; req: Bi[];
  // discounts
  cat: string; pct: string;
}

export function emptyForm(kind: Kind): Form {
  return {
    kind, status: 'published', title: { ...EMPTY }, org: { ...EMPTY }, desc: { ...EMPTY }, sched: { ...EMPTY }, what: { ...EMPTY }, cond: { ...EMPTY },
    city: '', source_url: '', posted_at: '', verified_at: '', verified_by: '', phone: '', email: '', site: '',
    deadline: '', noDeadline: false,
    type: '', funded: false, course: '', gpa: '', en: '', reqSkills: [], spec: [], docs: [], steps: [],
    today: false, pay: '', per: '', hours: '', tags: [], skills: [], req: [],
    cat: '', pct: '',
  };
}

const s = (v: unknown) => (typeof v === 'string' ? v : typeof v === 'number' && Number.isFinite(v) ? String(v) : '');
const strs = (v: unknown) => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []);
const bis = (v: unknown) => (Array.isArray(v) ? v.map(bi) : []);

/** the editor form of an Item from GET /api/admin/content/{id} (record as /api/content serves it) */
export function formOfItem(kind: Kind, status: string, noDeadline: boolean, record: Record<string, unknown>): Form {
  const f = emptyForm(kind);
  const c = (record.contact && typeof record.contact === 'object' ? record.contact : {}) as Record<string, unknown>;
  Object.assign(f, {
    status: status === 'hidden' ? 'hidden' : 'published',
    title: bi(kind === 'discounts' ? record.name : record.title),
    city: s(record.city), source_url: s(record.source_url), posted_at: s(record.posted_at), verified_at: s(record.verified_at),
    verified_by: s(record.verified_by), phone: s(c.phone), email: s(c.email), site: s(c.site),
    deadline: s(record.deadline), noDeadline: noDeadline || !record.deadline,
  });
  if (kind === 'opportunities') {
    const r = (record.req && typeof record.req === 'object' ? record.req : {}) as Record<string, unknown>;
    const spec = strs(record.spec);
    Object.assign(f, {
      type: s(record.type), funded: record.funded === true, org: bi(record.org), desc: bi(record.desc),
      course: s(r.course), gpa: s(r.gpa), en: s(r.en), reqSkills: strs(r.skills),
      spec: spec.length === 1 && spec[0] === 'all' ? [] : spec, docs: bis(record.docs), steps: bis(record.steps),
    });
  } else if (kind === 'jobs') {
    const spec = strs(record.spec);
    Object.assign(f, {
      today: record.today === true, org: bi(record.org), desc: bi(record.desc), sched: bi(record.sched),
      pay: s(record.pay), per: s(record.per), hours: record.hours ? s(record.hours) : '', tags: strs(record.tags), skills: strs(record.skills),
      spec: spec.length === 1 && spec[0] === 'all' ? [] : spec, req: bis(record.req),
    });
  } else {
    Object.assign(f, { cat: s(record.cat), pct: s(record.pct), what: bi(record.what), cond: bi(record.cond) });
  }
  return f;
}

/* ---------- form -> record (the WriteBody the API takes) ---------- */
const t = (x: string) => x.trim();
const tb = (b: Bi): Bi => ({ kk: t(b.kk), ru: t(b.ru) });
/** list rows: empty rows dropped; an empty ru takes the kk text (the server pairs the kk and ru lists by position) */
const tbList = (l: Bi[]): Bi[] => l.map(tb).filter(x => x.kk || x.ru).map(x => ({ kk: x.kk, ru: x.ru || x.kk }));
/** numbers as numbers when they parse (comma decimals too); anything else as typed, so the server names the field */
function num(v: string): number | string | undefined {
  const x = v.replace(/\s/g, '').replace(',', '.');
  if (!x) return undefined;
  const n = Number(x);
  return Number.isFinite(n) ? n : v.trim();
}

export interface WriteBody { kind: Kind; status: 'published' | 'hidden'; no_deadline: boolean; record: Record<string, unknown> }

export function bodyOf(f: Form, status = f.status): WriteBody {
  const common: Record<string, unknown> = {
    city: f.city, source_url: t(f.source_url), posted_at: f.posted_at || null, verified_at: f.verified_at || null, verified_by: t(f.verified_by),
    contact: { phone: t(f.phone), email: t(f.email), site: t(f.site) },
    deadline: f.noDeadline ? null : (f.deadline || null),
  };
  let record: Record<string, unknown>;
  if (f.kind === 'opportunities') {
    const req: Record<string, unknown> = {};
    const course = num(f.course), gpa = num(f.gpa);
    if (course !== undefined) req.course = course;
    if (gpa !== undefined) req.gpa = gpa;
    if (f.en) req.en = f.en;
    if (f.reqSkills.length) req.skills = f.reqSkills;
    record = {
      type: f.type, funded: f.funded, title: tb(f.title), org: tb(f.org), req, spec: f.spec, desc: tb(f.desc),
      docs: tbList(f.docs), steps: tbList(f.steps), ...common,
    };
  } else if (f.kind === 'jobs') {
    record = {
      title: tb(f.title), org: tb(f.org), pay: num(f.pay) ?? '', per: f.per, hours: num(f.hours) ?? '', sched: tb(f.sched),
      tags: f.tags, skills: f.skills, spec: f.spec, desc: tb(f.desc), req: tbList(f.req), ...common,
    };
    if (f.today) record.today = true;
  } else {
    record = { cat: f.cat, pct: t(f.pct), name: tb(f.title), what: tb(f.what), cond: tb(f.cond), ...common };
  }
  return { kind: f.kind, status, no_deadline: f.noDeadline, record };
}

/* ---------- client rules (the docs table; the server stays the judge) ---------- */
export type Msg = [string, string];
export type Errors = Record<string, Msg>;

const DATE = /^\d{4}-\d{2}-\d{2}$/;
export function validDate(v: string): boolean {
  if (!DATE.test(v)) return false;
  const [y, m, d] = v.split('-').map(Number);
  const dt = new Date(Date.UTC(y, m - 1, d));
  return dt.getUTCFullYear() === y && dt.getUTCMonth() === m - 1 && dt.getUTCDate() === d;
}
export function httpsOk(v: string): boolean {
  if (!/^https:\/\//i.test(v) || /[\s<>"]/.test(v)) return false;
  try { return !!new URL(v).hostname; } catch { return false; }
}
const EMAIL = /^[^@\s]+@[^@\s]+\.[^@\s]+$/;

/** the kk/ru text the form shows for a field path (also used for the server's `path`) */
export const MESSAGES: Record<string, Msg> = {
  'title.kk': ['Қазақша атауын жазыңыз', 'Укажите название на казахском'],
  'name.kk': ['Қазақша атауын жазыңыз', 'Укажите название на казахском'],
  city: ['Қаланы таңдаңыз', 'Выберите город'],
  source_url: ['Сілтеме https:// деп басталуы керек', 'Ссылка должна начинаться с https://'],
  deadline: ['Соңғы күнді таңдаңыз не «Мерзімсіз» белгілеңіз', 'Укажите дедлайн или отметьте «Без срока»'],
  posted_at: ['Күн қате', 'Неверная дата'],
  verified_at: ['Күн қате', 'Неверная дата'],
  'contact.email': ['Email қате', 'Неверный email'],
  'contact.site': ['Сайт https:// деп басталуы керек', 'Сайт должен начинаться с https://'],
  list: ['«|» таңбасын қолданбаңыз', 'Не используйте символ «|»'],
  type: ['Түрін таңдаңыз', 'Выберите тип'],
  'req.course': ['Курс 1–7 аралығында', 'Курс от 1 до 7'],
  'req.gpa': ['GPA 0–4 аралығында', 'GPA от 0 до 4'],
  'req.en': ['Деңгейді таңдаңыз', 'Выберите уровень'],
  'docs[].kk': ['Әр элементтің қазақшасын жазыңыз', 'Заполните казахский текст каждого пункта'],
  'steps[].kk': ['Әр элементтің қазақшасын жазыңыз', 'Заполните казахский текст каждого пункта'],
  pay: ['Төлемді жазыңыз', 'Укажите оплату'],
  per: ['Төлем кезеңін таңдаңыз', 'Выберите период оплаты'],
  hours: ['Ауысым сағатын жазыңыз', 'Укажите часы смены'],
  'req[].kk': ['Әр талаптың қазақшасын жазыңыз', 'Заполните казахский текст требования'],
  cat: ['Санатын таңдаңыз', 'Выберите категорию'],
  pct: ['Жеңілдік мөлшерін жазыңыз', 'Укажите размер скидки'],
};
const PIPE: Msg = MESSAGES.list;

/** where a server `path` is shown in the form: list items share their list's slot, «мерзімсіз» sits with the deadline */
export function slotOf(path: string): string {
  if (path === 'no_deadline') return 'deadline';
  if (path === 'name.kk' || path === 'name.ru') return 'title.' + path.slice(5);
  const m = /^(docs|steps|req)\[\]\.(kk|ru)$/.exec(path);
  if (m) return `${m[1]}[]`;
  return path;
}

/** every broken field of the form, keyed by slot (see slotOf) */
export function check(f: Form): Errors {
  const e: Errors = {};
  if (!t(f.title.kk)) e['title.kk'] = MESSAGES['title.kk'];
  if (f.kind === 'opportunities' && !OPP_TYPE_CODES.includes(f.type)) e.type = MESSAGES.type;
  if (!CITY_CODES.includes(f.city)) e.city = MESSAGES.city;
  if (!httpsOk(t(f.source_url))) e.source_url = MESSAGES.source_url;
  if (f.noDeadline ? !!f.deadline : !validDate(f.deadline)) e.deadline = MESSAGES.deadline;
  if (f.posted_at && !validDate(f.posted_at)) e.posted_at = MESSAGES.posted_at;
  if (f.verified_at && !validDate(f.verified_at)) e.verified_at = MESSAGES.verified_at;
  const email = t(f.email);
  if (email && !EMAIL.test(email)) e['contact.email'] = MESSAGES['contact.email'];
  if (t(f.site) && !httpsOk(t(f.site))) e['contact.site'] = MESSAGES['contact.site'];
  const lists = (name: 'docs' | 'steps' | 'req', l: Bi[]) => {
    if (l.some(x => !t(x.kk) && t(x.ru))) e[`${name}[]`] = MESSAGES[`${name}[].kk`];
    else if (l.some(x => x.kk.includes('|') || x.ru.includes('|'))) e[`${name}[]`] = PIPE;
  };
  if (f.kind === 'opportunities') {
    const course = num(f.course), gpa = num(f.gpa);
    if (course !== undefined && !(typeof course === 'number' && Number.isInteger(course) && course >= 1 && course <= 7)) e['req.course'] = MESSAGES['req.course'];
    if (gpa !== undefined && !(typeof gpa === 'number' && gpa >= 0 && gpa <= 4)) e['req.gpa'] = MESSAGES['req.gpa'];
    if (f.en && !EN_LEVELS.includes(f.en)) e['req.en'] = MESSAGES['req.en'];
    lists('docs', f.docs);
    lists('steps', f.steps);
  } else if (f.kind === 'jobs') {
    const pay = num(f.pay), hours = num(f.hours);
    if (!(typeof pay === 'number' && pay >= 0)) e.pay = MESSAGES.pay;
    if (!JOB_PER.includes(f.per)) e.per = MESSAGES.per;
    const hOk = typeof hours === 'number' && hours > 0 && hours <= 168;
    if (f.per === 'shift' ? !hOk : hours !== undefined && !(typeof hours === 'number' && hours >= 0 && hours <= 168)) e.hours = MESSAGES.hours;
    lists('req', f.req);
  } else {
    if (!DEAL_CAT_CODES.includes(f.cat)) e.cat = MESSAGES.cat;
    if (!t(f.pct)) e.pct = MESSAGES.pct;
  }
  return e;
}

/* ---------- preview: the record the card and detail components render ---------- */
const lt = (b: Bi): LText => {
  const x = tb(b);
  return { kk: x.kk || x.ru, ru: x.ru || x.kk };
};
const n0 = (v: string) => { const x = num(v); return typeof x === 'number' ? x : 0; };

export function previewOf(f: Form): Opportunity | Job | Discount {
  const prov = {
    source_url: t(f.source_url), posted_at: f.posted_at, verified_at: f.verified_at, verified_by: t(f.verified_by),
    contact: { phone: t(f.phone), email: t(f.email), site: t(f.site) }, is_demo: false,
    deadline: f.noDeadline || !validDate(f.deadline) ? null : f.deadline, city: f.city || 'other',
  };
  const spec = f.spec.length ? f.spec : ['all'];
  if (f.kind === 'opportunities') {
    const gpa = num(f.gpa);
    return {
      ...prov, id: 'preview', type: (OPP_TYPE_CODES.includes(f.type) ? f.type : 'grant') as Opportunity['type'], funded: f.funded,
      title: lt(f.title), org: lt(f.org),
      req: { course: n0(f.course), ...(typeof gpa === 'number' && gpa ? { gpa } : {}), ...(f.en ? { en: f.en } : {}), ...(f.reqSkills.length ? { skills: f.reqSkills } : {}) },
      spec, desc: lt(f.desc), docs: tbList(f.docs).map(lt), steps: tbList(f.steps).map(lt),
    };
  }
  if (f.kind === 'jobs') {
    return {
      ...prov, id: 'preview', today: f.today || undefined, title: lt(f.title), org: lt(f.org), pay: n0(f.pay),
      per: (JOB_PER.includes(f.per) ? f.per : 'month') as Job['per'], hours: n0(f.hours), sched: lt(f.sched), tags: f.tags, skills: f.skills,
      spec, desc: lt(f.desc), req: tbList(f.req).map(lt),
    };
  }
  return { ...prov, id: 'preview', cat: f.cat || 'food', pct: t(f.pct), name: lt(f.title), what: lt(f.what), cond: lt(f.cond) };
}
