// Sheet <-> record mapping for the three content lists.
// One rule for every list cell: items are separated by "|" (written as " | ").
// Bilingual fields are two columns: <name>_kk and <name>_ru.

export const LIST_SEP = ' | ';
export const KINDS = ['opportunities', 'jobs', 'discounts'];
export const STATUSES = ['draft', 'published', 'hidden'];

// Keep in sync with index.html (OPP_TYPES, CITIES, DEAL_CATS, per handling).
export const OPP_TYPES = ['mobility', 'intl', 'company', 'grant'];
export const JOB_PER = ['shift', 'month', 'hour'];
export const EN_LEVELS = ['A1', 'A2', 'B1', 'B2', 'C1', 'C2'];
export const DEAL_CATS = ['food', 'cinema', 'books', 'sport', 'clothes', 'lang', 'edu', 'cowork', 'print'];
export const CITIES = ['astana', 'almaty', 'shymkent', 'karaganda', 'aktobe', 'taraz', 'turkistan', 'pavlodar',
  'oskemen', 'semey', 'kostanay', 'atyrau', 'aktau', 'oral', 'kyzylorda', 'petropavl', 'taldykorgan',
  'kokshetau', 'other', 'remote'];

const COMMON_TAIL = ['source_url', 'posted_at', 'verified_at', 'verified_by',
  'contact_phone', 'contact_email', 'contact_site', 'is_demo', 'deadline', 'no_deadline'];

export const COLUMNS = {
  opportunities: ['id', 'status', 'type', 'funded', 'title_kk', 'title_ru', 'org_kk', 'org_ru',
    'req_course', 'req_gpa', 'req_en', 'req_skills', 'spec', 'desc_kk', 'desc_ru',
    'docs_kk', 'docs_ru', 'steps_kk', 'steps_ru', 'city', ...COMMON_TAIL],
  jobs: ['id', 'status', 'today', 'title_kk', 'title_ru', 'org_kk', 'org_ru', 'city', 'pay', 'per', 'hours',
    'sched_kk', 'sched_ru', 'tags', 'skills', 'spec', 'desc_kk', 'desc_ru', 'req_kk', 'req_ru', ...COMMON_TAIL],
  discounts: ['id', 'status', 'cat', 'pct', 'name_kk', 'name_ru', 'what_kk', 'what_ru', 'cond_kk', 'cond_ru',
    'city', ...COMMON_TAIL]
};

// ---------- record -> row (used by scripts/json-to-csv.mjs) ----------

const B = v => (v ? 'TRUE' : '');
const joinList = arr => (arr || []).map(s => {
  if (String(s).includes('|')) throw new Error(`"|" is the list separator and cannot appear inside an item: ${s}`);
  return s;
}).join(LIST_SEP);
const num = v => (v == null ? '' : String(v));

export function recordToRow(kind, r) {
  const c = r.contact || {};
  const base = {
    id: r.id, status: 'published',
    source_url: r.source_url, posted_at: r.posted_at || '', verified_at: r.verified_at || '',
    verified_by: r.verified_by || '', contact_phone: c.phone || '', contact_email: c.email || '',
    contact_site: c.site || '', is_demo: B(r.is_demo), deadline: r.deadline || '', no_deadline: B(!r.deadline),
    city: r.city || ''
  };
  const bi = (name, o) => { base[name + '_kk'] = o?.kk || ''; base[name + '_ru'] = o?.ru || ''; };
  const biList = (name, arr) => {
    base[name + '_kk'] = joinList((arr || []).map(x => x.kk));
    base[name + '_ru'] = joinList((arr || []).map(x => x.ru));
  };
  if (kind === 'opportunities') {
    Object.assign(base, {
      type: r.type, funded: B(r.funded), req_course: num(r.req?.course), req_gpa: num(r.req?.gpa),
      req_en: r.req?.en || '', req_skills: joinList(r.req?.skills), spec: joinList(r.spec)
    });
    bi('title', r.title); bi('org', r.org); bi('desc', r.desc); biList('docs', r.docs); biList('steps', r.steps);
  } else if (kind === 'jobs') {
    Object.assign(base, {
      today: B(r.today), pay: num(r.pay), per: r.per, hours: num(r.hours),
      tags: joinList(r.tags), skills: joinList(r.skills), spec: joinList(r.spec)
    });
    bi('title', r.title); bi('org', r.org); bi('sched', r.sched); bi('desc', r.desc); biList('req', r.req);
  } else if (kind === 'discounts') {
    Object.assign(base, { cat: r.cat, pct: r.pct });
    bi('name', r.name); bi('what', r.what); bi('cond', r.cond);
  } else throw new Error('unknown kind ' + kind);
  return COLUMNS[kind].map(k => base[k] ?? '');
}

// ---------- row -> record (validation) ----------

export class RowError extends Error {}
const fail = msg => { throw new RowError(msg); };

const TRUE_WORDS = ['true', '1', 'yes', 'y', 'x', 'иә', 'ия', 'да', 'истина', 'ақиқат', '✓', '✔'];
const FALSE_WORDS = ['', 'false', '0', 'no', 'n', 'жоқ', 'нет', 'ложь', 'жалған'];

function str(row, key) { return String(row[key] ?? '').trim(); }

function bool(row, key) {
  const v = str(row, key).toLowerCase();
  if (TRUE_WORDS.includes(v)) return true;
  if (FALSE_WORDS.includes(v)) return false;
  fail(`"${key}" бағанында TRUE немесе бос болуы керек, ал "${row[key]}" жазылған`);
}

export function isValidDate(s) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(s)) return false;
  const [y, m, d] = s.split('-').map(Number);
  const dt = new Date(Date.UTC(y, m - 1, d));
  return dt.getUTCFullYear() === y && dt.getUTCMonth() === m - 1 && dt.getUTCDate() === d;
}

// Dates must be YYYY-MM-DD. DD.MM.YYYY is also accepted because Google Sheets
// with a ru/kk locale silently reformats typed dates that way.
function date(row, key) {
  let v = str(row, key);
  if (!v) return null;
  const m = /^(\d{1,2})\.(\d{1,2})\.(\d{4})$/.exec(v);
  if (m) v = `${m[3]}-${m[2].padStart(2, '0')}-${m[1].padStart(2, '0')}`;
  if (!isValidDate(v)) fail(`"${key}" күні қате: "${row[key]}" (керек формат: ЖЖЖЖ-АА-КК, мысалы 2026-12-31)`);
  return v;
}

function number(row, key, { required = false, int = false, min = -Infinity, max = Infinity } = {}) {
  const raw = str(row, key).replace(/[\s  ]/g, '').replace(',', '.');
  if (!raw) { if (required) fail(`"${key}" толтырылмаған`); return undefined; }
  const n = Number(raw);
  if (!Number.isFinite(n) || (int && !Number.isInteger(n)) || n < min || n > max) {
    fail(`"${key}" бағанындағы сан қате: "${row[key]}"`);
  }
  return n;
}

function httpsUrl(row, key, required) {
  const v = str(row, key);
  if (!v) { if (required) fail(`"${key}" толтырылмаған`); return ''; }
  let ok = /^https:\/\//i.test(v);
  if (ok) { try { new URL(v); } catch { ok = false; } }
  if (!ok) fail(`"${key}" сілтемесі https:// деп басталуы керек: "${v}"`);
  return v;
}

function oneOf(row, key, allowed, required = true) {
  const v = str(row, key);
  if (!v) { if (required) fail(`"${key}" толтырылмаған`); return undefined; }
  if (!allowed.includes(v)) fail(`"${key}" мәні белгісіз: "${v}" (рұқсат етілгені: ${allowed.join(', ')})`);
  return v;
}

function list(row, key) {
  return str(row, key).split('|').map(s => s.trim()).filter(Boolean);
}

// Bilingual text. A missing side copies the other one; `requireKk` makes kk mandatory.
function bi(row, name, requireKk = false) {
  const kk = str(row, name + '_kk');
  const ru = str(row, name + '_ru');
  if (requireKk && !kk) fail(`"${name}_kk" толтырылмаған`);
  return { kk: kk || ru, ru: ru || kk };
}

function biList(row, name) {
  const kk = list(row, name + '_kk');
  const ru = list(row, name + '_ru');
  if (ru.length > kk.length) fail(`"${name}_ru" тізімінде "${name}_kk" тізімінен көп элемент бар (бөлгіш: |)`);
  return kk.map((k, i) => ({ kk: k, ru: ru[i] || k }));
}

function common(row) {
  const deadline = date(row, 'deadline');
  const noDeadline = bool(row, 'no_deadline');
  if (deadline && noDeadline) fail('"deadline" мен "no_deadline" екеуі бірдей толтырылған — біреуін ғана қалдырыңыз');
  if (!deadline && !noDeadline) fail('"deadline" толтырылмаған (мерзімсіз болса no_deadline = TRUE)');
  const email = str(row, 'contact_email');
  if (email && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) fail(`"contact_email" қате: "${email}"`);
  return {
    source_url: httpsUrl(row, 'source_url', true),
    posted_at: date(row, 'posted_at'),
    verified_at: date(row, 'verified_at'),
    verified_by: str(row, 'verified_by'),
    contact: { phone: str(row, 'contact_phone'), email, site: httpsUrl(row, 'contact_site', false) },
    is_demo: bool(row, 'is_demo'),
    deadline
  };
}

function idOf(row) {
  const id = str(row, 'id');
  if (!id) fail('"id" толтырылмаған');
  if (!/^[A-Za-z0-9_-]{1,64}$/.test(id)) fail(`"id" тек латын әріптері, сандар, - және _ болуы керек: "${id}"`);
  return id;
}

const specOf = row => { const s = list(row, 'spec'); return s.length ? s : ['all']; };

const BUILDERS = {
  opportunities(row) {
    const id = idOf(row);
    const title = bi(row, 'title', true);
    const type = oneOf(row, 'type', OPP_TYPES);
    const req = {};
    const course = number(row, 'req_course', { int: true, min: 1, max: 7 });
    const gpa = number(row, 'req_gpa', { min: 0, max: 4 });
    const en = oneOf(row, 'req_en', EN_LEVELS, false);
    const skills = list(row, 'req_skills');
    if (course !== undefined) req.course = course;
    if (gpa !== undefined) req.gpa = gpa;
    if (en) req.en = en;
    if (skills.length) req.skills = skills;
    const city = oneOf(row, 'city', CITIES);
    const c = common(row);
    return {
      id, type, funded: bool(row, 'funded'), title, org: bi(row, 'org'), req, spec: specOf(row),
      desc: bi(row, 'desc'), docs: biList(row, 'docs'), steps: biList(row, 'steps'),
      source_url: c.source_url, posted_at: c.posted_at, verified_at: c.verified_at, verified_by: c.verified_by,
      contact: c.contact, is_demo: c.is_demo, deadline: c.deadline, city
    };
  },
  jobs(row) {
    const id = idOf(row);
    const today = bool(row, 'today');
    const title = bi(row, 'title', true);
    const city = oneOf(row, 'city', CITIES);
    const pay = number(row, 'pay', { required: true, min: 0 });
    const per = oneOf(row, 'per', JOB_PER);
    const hours = number(row, 'hours', { required: per === 'shift', min: 0, max: 24 * 7 });
    if (per === 'shift' && !(hours > 0)) fail('per = shift болса "hours" 0-ден үлкен болуы керек');
    const c = common(row);
    const rec = { id };
    if (today) rec.today = true;
    Object.assign(rec, {
      title, org: bi(row, 'org'), city, pay, per, hours: hours ?? 0, sched: bi(row, 'sched'),
      tags: list(row, 'tags'), skills: list(row, 'skills'), spec: specOf(row), desc: bi(row, 'desc'),
      req: biList(row, 'req'),
      source_url: c.source_url, posted_at: c.posted_at, verified_at: c.verified_at, verified_by: c.verified_by,
      contact: c.contact, is_demo: c.is_demo, deadline: c.deadline
    });
    return rec;
  },
  discounts(row) {
    const id = idOf(row);
    const name = bi(row, 'name', true);
    const cat = oneOf(row, 'cat', DEAL_CATS);
    const pct = str(row, 'pct');
    if (!pct) fail('"pct" толтырылмаған');
    const city = oneOf(row, 'city', CITIES);
    const c = common(row);
    return {
      id, cat, pct, name, what: bi(row, 'what'), cond: bi(row, 'cond'),
      source_url: c.source_url, posted_at: c.posted_at, verified_at: c.verified_at, verified_by: c.verified_by,
      contact: c.contact, is_demo: c.is_demo, deadline: c.deadline, city
    };
  }
};

// Validates sheet rows (objects from rowsToObjects). Returns only published,
// valid, unique rows; no deadline filtering here (see filterActive).
export function buildRecords(kind, items, { warn = console.warn } = {}) {
  const build = BUILDERS[kind];
  const records = [];
  const seen = new Set();
  let rejected = 0;
  for (const row of items) {
    const label = `[content] ${kind}: ${row._row}-жол${row.id ? ` (id=${String(row.id).trim()})` : ''}`;
    const status = str(row, 'status').toLowerCase() || 'draft'; // empty status = draft
    if (!STATUSES.includes(status)) {
      rejected++;
      warn(`${label}: "status" мәні белгісіз: "${row.status ?? ''}" (draft / published / hidden) — жол алынып тасталды`);
      continue;
    }
    if (status !== 'published') continue;
    try {
      const rec = build(row);
      if (seen.has(rec.id)) fail(`id "${rec.id}" қайталанады — бірінші жол ғана алынды`);
      seen.add(rec.id);
      records.push(rec);
    } catch (e) {
      rejected++;
      warn(`${label}: ${e instanceof RowError ? e.message : 'күтпеген қате: ' + e.message} — жол алынып тасталды`);
    }
  }
  return { records, rejected };
}

// Today's date in Asia/Almaty (UTC+5, no DST) as YYYY-MM-DD.
export function almatyToday(now = Date.now()) {
  return new Date(now + 5 * 3600 * 1000).toISOString().slice(0, 10);
}

// Drops records whose deadline is before `today` (a deadline equal to today stays).
export function filterActive(records, today) {
  return records.filter(r => !r.deadline || r.deadline >= today);
}
