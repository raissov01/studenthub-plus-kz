/* Domain logic ported from the legacy app: matching, qualification, plans, résumé, notifications, search. */
import { COURSES, DEMO_DECK, EVENTS, CITIES, DEAL_CATS, LEVELS, OPP_TYPES, SKILLS, SPECS } from '@/lib/data/static';
import { DAY, T0, dOff, daysToYmd, diffDays, money, type I18n } from '@/lib/format';
import type { AppState, Content, Deck, IconName, Job, LText, Opportunity, Plan, PlanItem, Profile } from '@/lib/types';

export const lvl = (l: string) => LEVELS.indexOf(l);

export function studentCode(p: Profile) {
  const s = (p.name || 'SG') + p.spec + p.course;
  let h = 7;
  for (const c of s) h = (h * 31 + c.charCodeAt(0)) % 100000;
  return 'SG-' + String(h).padStart(5, '0');
}
export function gradYear(p: Profile) {
  const n = new Date();
  const start = n.getMonth() >= 8 ? n.getFullYear() : n.getFullYear() - 1;
  return start + (4 - p.course) + 1;
}
// employer jobs may leave hours empty for a monthly salary: fall back to a full-time month (22 days × 8 h)
export const hourly = (j: Job) => (j.per === 'hour' ? j.pay : j.per === 'shift' ? (j.hours ? j.pay / j.hours : 0) : j.pay / ((j.hours || 8) * 22));
export function payText(j: Job, I: I18n) {
  const { tt } = I;
  if (j.per === 'month') return { main: money(j.pay), sub: tt('айына', 'в месяц') };
  if (j.per === 'hour') return { main: money(j.pay), sub: tt('сағатына', 'в час') };
  return { main: money(j.pay), sub: tt(`ауысымға · ${j.hours} сағ`, `за смену · ${j.hours} ч`) };
}
/** Prefer the publisher's instructions, then an actual contact method; never invent one. */
export function jobApplyText(j: Job, I: I18n): string {
  if (j.apply && I.L(j.apply).trim()) return I.L(j.apply);
  if (j.contact.email) return I.tt('Email арқылы: ', 'По email: ') + j.contact.email;
  if (j.contact.phone) return I.tt('Телефон арқылы: ', 'По телефону: ') + j.contact.phone;
  if (j.contact.site) return I.tt('Сайт арқылы: ', 'На сайте: ') + j.contact.site;
  return I.tt('Өтінім әдісі көрсетілмеген', 'Способ отклика не указан');
}
export const typeLabel = (t: string, I: I18n) => I.tp(OPP_TYPES[t]);
/** search category: grants/contests and academic mobility are their own groups, the rest are internships */
export const oppKind = (o: Opportunity) => (o.type === 'grant' ? 'grant' : o.type === 'mobility' ? 'mobility' : 'intern');
export const OPP_KINDS = ['intern', 'grant', 'mobility'];

/** days until the opportunity's deadline; Infinity when it has none */
export const oppLeft = (o: Opportunity) => (o.deadline ? daysToYmd(o.deadline) : Infinity);
/** client-side guard: past deadlines never show (the API already filters them) */
export const openOpps = (list: Opportunity[]) => list.filter(o => oppLeft(o) >= 0);
export function deadlineText(o: Opportunity, I: I18n) {
  const left = oppLeft(o);
  if (!Number.isFinite(left)) return I.tt('Мерзімі шектелмеген', 'Без срока');
  return `${I.fDate(dOff(left))}, ${I.daysLeftTxt(left).toLowerCase()}`;
}

export interface QualRow { ok: boolean; mid?: boolean; t: string; you: string }
export function qualify(o: Opportunity, p: Profile, I: I18n) {
  const { tt, L } = I;
  const r = o.req, res: QualRow[] = [];
  // a record without a course requirement (admin editor, student suggestions) has no course row
  if (r.course) res.push({ ok: p.course >= r.course, t: tt(`Курс: кемінде ${r.course}`, `Курс: не ниже ${r.course}`), you: tt(`Сізде: ${p.course}-курс`, `У вас: ${p.course} курс`) });
  if (r.gpa) res.push({ ok: p.gpa != null && p.gpa >= r.gpa, mid: p.gpa == null, t: `GPA ≥ ${r.gpa.toFixed(2)}`, you: p.gpa == null ? tt('GPA көрсетілмеген (міндетті емес)', 'GPA не указан (необязательно)') : tt(`Сізде: ${p.gpa.toFixed(2)}`, `У вас: ${p.gpa.toFixed(2)}`) });
  if (r.en) res.push({ ok: lvl(p.en) >= lvl(r.en), t: tt(`Ағылшын тілі: ${r.en} және жоғары`, `Английский: ${r.en} и выше`), you: tt(`Сізде: ${p.en}`, `У вас: ${p.en}`) });
  (r.skills || []).forEach(k => res.push({ ok: p.skills.includes(k), t: tt('Дағды: ', 'Навык: ') + L(SKILLS[k]), you: p.skills.includes(k) ? tt('Профильде бар', 'Есть в профиле') : tt('Профильде жоқ', 'Нет в профиле') }));
  if (!o.spec.includes('all')) res.push({ ok: o.spec.includes(p.spec), t: tt('Мамандық: ', 'Специальность: ') + o.spec.map(s => L(SPECS[s])).join(', '), you: tt('Сізде: ', 'У вас: ') + L(SPECS[p.spec]) });
  const left = oppLeft(o);
  if (Number.isFinite(left)) res.push({ ok: left >= 0, mid: left >= 0 && left <= 14, t: tt('Өтінім мерзімі: ', 'Приём заявок до: ') + I.fDate(dOff(left)), you: I.daysLeftTxt(left) });
  return { res, ok: res.every(x => x.ok) };
}

export function matchJob(j: Job, p: Profile) {
  const have = j.skills.filter(k => p.skills.includes(k)), miss = j.skills.filter(k => !p.skills.includes(k));
  let total = j.skills.length, got = have.length;
  const specNeed = !j.spec.includes('all');
  if (specNeed) { total++; if (j.spec.includes(p.spec)) got++; }
  const hasExp = !!(p.exp && p.exp.trim());
  total++;
  if (hasExp) got++;
  return { pct: total ? Math.round(100 * got / total) : 0, have, miss, specNeed, specOk: j.spec.includes(p.spec), hasExp };
}

export interface Cert { title: string; org: string; date: Date; src: 'event' | 'course' | 'manual'; i?: number }
export function certificates(S: AppState, I: I18n): Cert[] {
  const list: Cert[] = [];
  EVENTS.filter(e => e.past && e.cert && S.events[e.id]).forEach(e => list.push({ title: I.L(e.title), org: I.tt('Университеттің студенттер ісі бөлімі', 'Отдел по делам студентов'), date: dOff(e.day), src: 'event' }));
  COURSES.filter(c => S.done[c.id]).forEach(c => list.push({ title: I.L(c.title), org: c.platform, date: new Date(S.done[c.id]), src: 'course' }));
  S.certs.forEach((c, i) => list.push({ title: c.title, org: c.org, date: new Date(c.date), src: 'manual', i }));
  return list.sort((a, b) => b.date.getTime() - a.date.getTime());
}

export function allDecks(S: AppState, I: I18n): Deck[] {
  const out: Deck[] = [];
  if (!S.hideDemoDeck) out.push({ id: DEMO_DECK.id, demo: true, name: I.L(DEMO_DECK.name), cards: DEMO_DECK.cards.map(c => ({ q: I.L(c.q), a: I.L(c.a) })) });
  return out.concat(S.decks);
}

/** exam plan generator: learn chunks with a test after every two, then review hard topics and a mock exam */
export function buildPlan(subject: string, examDate: string, lectures: number, hard: string, I: I18n): Plan | null {
  const { tt } = I;
  const D = diffDays(examDate);
  if (!(D >= 1)) return null;
  const items: Omit<PlanItem, 'done'>[] = [];
  const learn = (a: number, b: number) => (a === b ? tt(`${a}-дәрісті оқу, конспект жасау`, `Изучить лекцию ${a}, сделать конспект`) : tt(`${a}–${b} дәрістерді оқу, конспект жасау`, `Изучить лекции ${a}–${b}, сделать конспект`));
  const test = (a: number, b: number) => (a === b ? tt(`${a}-дәріс бойынша тест`, `Тест по лекции ${a}`) : tt(`${a}–${b} дәрістер бойынша тест`, `Тест по лекциям ${a}–${b}`));
  if (D === 1) {
    items.push({ d: 0, k: 'mock', t: tt(`Барлық дәрісті (${lectures}) қайталау және сынақ тест`, `Повторить все лекции (${lectures}) и пройти пробный тест`) });
  } else if (D <= 3) {
    items.push({ d: 0, k: 'learn', t: learn(1, lectures) });
    if (D === 3) items.push({ d: 1, k: 'review', t: tt('Қиын тақырыптарды қайталау', 'Повторить сложные темы') + (hard ? `: ${hard}` : '') });
    items.push({ d: D - 1, k: 'mock', t: tt('Сынақ емтихан (уақытпен, көмексіз)', 'Пробный экзамен (на время, без подсказок)') });
  } else {
    const slots = D - 2;
    const chunks = Math.max(1, Math.min(lectures, Math.floor(slots * 2 / 3)));
    const per: number[] = [];
    for (let i = 0; i < chunks; i++) per.push(Math.floor(lectures / chunks) + (i < lectures % chunks ? 1 : 0));
    let start = 1, day = 0, sinceTest = 1;
    per.forEach((n, i) => {
      const end = start + n - 1;
      items.push({ d: day++, k: 'learn', t: learn(start, end) });
      if ((i + 1) % 2 === 0 && day < slots) {
        items.push({ d: day++, k: 'test', t: test(sinceTest, end) });
        sinceTest = end + 1;
      }
      start = end + 1;
    });
    items.push({ d: D - 2, k: 'review', t: tt('Қиын тақырыптар мен қате жауаптарды қайталау', 'Повторить сложные темы и ошибки') + (hard ? `: ${hard}` : '') });
    items.push({ d: D - 1, k: 'mock', t: tt('Сынақ емтихан (уақытпен, көмексіз)', 'Пробный экзамен (на время, без подсказок)') });
  }
  return { id: 'pl' + Date.now(), subject, exam: new Date(examDate).toISOString(), lectures, hard, created: T0().toISOString(), items: items.map(x => ({ ...x, done: false })) };
}
export const planDay = (pl: Plan, d: number) => new Date(new Date(pl.created).getTime() + d * DAY);

export function cardsFromText(text: string) {
  const out: { q: string; a: string }[] = [];
  text.split(/\n+/).forEach(line => {
    const l = line.trim().replace(/^[-•*\d.)\s]+/, '');
    const m = l.match(/^(.{2,80}?)\s*(?:—|–|-|:|\|)\s+(.{3,})$/);
    if (m) out.push({ q: m[1].trim(), a: m[2].trim() });
  });
  return out;
}

export function resumeText(lang: 'kk' | 'ru' | 'en', S: AppState, I: I18n) {
  const p = S.profile;
  const l = (o: LText | string) => (typeof o === 'string' ? o : o[lang] || o.ru);
  const uni = { kk: 'С. Сейфуллин атындағы Қазақ агротехникалық зерттеу университеті', ru: 'Казахский агротехнический исследовательский университет им. С. Сейфуллина', en: 'S. Seifullin Kazakh Agrotechnical Research University' }[lang];
  const H = {
    kk: ['ӨЗІМ ТУРАЛЫ', 'БІЛІМІ', 'ТӘЖІРИБЕ', 'ДАҒДЫЛАР', 'ТІЛДЕР', 'СЕРТИФИКАТТАР', 'ҚОҒАМДЫҚ БЕЛСЕНДІЛІК'],
    ru: ['О СЕБЕ', 'ОБРАЗОВАНИЕ', 'ОПЫТ', 'НАВЫКИ', 'ЯЗЫКИ', 'СЕРТИФИКАТЫ', 'ОБЩЕСТВЕННАЯ АКТИВНОСТЬ'],
    en: ['SUMMARY', 'EDUCATION', 'EXPERIENCE', 'SKILLS', 'LANGUAGES', 'CERTIFICATES', 'ACTIVITIES'],
  }[lang];
  const spec = l(SPECS[p.spec] || SPECS.other);
  const headline = { kk: `${spec} мамандығының ${p.course}-курс студенті`, ru: `Студент(ка) ${p.course} курса, специальность «${spec}»`, en: `${p.course}${['st', 'nd', 'rd'][p.course - 1] || 'th'}-year ${spec} student` }[lang];
  const about = p.about && p.about.trim() ? p.about.trim() : {
    kk: `${spec} саласында тәжірибе жинауға ұмтыламын. Жауапкершілігім жоғары, жаңа нәрсені тез үйренемін, командада жұмыс істей аламын.`,
    ru: `Стремлюсь получить практический опыт в сфере «${spec}». Ответственная, быстро обучаюсь, умею работать в команде.`,
    en: `Motivated ${spec.toLowerCase()} student looking for hands-on experience. Responsible, quick to learn and comfortable working in a team.`,
  }[lang];
  const contacts = [p.phone, p.email].filter(Boolean).join(' · ');
  const langs: string[] = [];
  if (p.skills.includes('kz')) langs.push(l(SKILLS.kz));
  if (p.skills.includes('ru')) langs.push(l(SKILLS.ru));
  langs.push(`${l(SKILLS.en)} — ${p.en}`);
  const skills = p.skills.filter(k => !['kz', 'ru', 'en'].includes(k) && SKILLS[k]).map(k => l(SKILLS[k]));
  const certs = certificates(S, I).map(c => `• ${c.title} — ${c.org}, ${c.date.getFullYear()}`);
  const acts = EVENTS.filter(e => e.past && S.events[e.id]).map(e => `• ${l(e.title)}, ${dOff(e.day).getFullYear()}`);
  const exp = (p.exp || '').split('\n').map(s => s.trim()).filter(Boolean).map(s => '• ' + s);
  const gpaLine = p.gpa == null ? '' : `GPA: ${p.gpa.toFixed(2)} / 4.0`;
  const grad = { kk: `бітіру жылы: ${gradYear(p)}`, ru: `год выпуска: ${gradYear(p)}`, en: `expected graduation: ${gradYear(p)}` }[lang];
  const out = [p.name.toUpperCase(), headline, contacts, '', H[0], about, '', H[1], `${uni}`, `${spec} · ${grad}`, gpaLine];
  if (exp.length) out.push('', H[2], ...exp);
  if (skills.length) out.push('', H[3], skills.join(' · '));
  out.push('', H[4], langs.join(' · '));
  if (certs.length) out.push('', H[5], ...certs);
  if (acts.length) out.push('', H[6], ...acts);
  return out.filter((x, i, a) => !(x === '' && a[i - 1] === '')).join('\n').replace(/^\n+/, '');
}

export function profileFill(S: AppState, I: I18n) {
  const p = S.profile;
  const checks = [!!p.name, !!p.email, !!(p.about || '').trim(), !!(p.exp || '').trim(), p.skills.length >= 3, p.credits > 0, certificates(S, I).length > 0, !p.demo, !!p.en];
  return Math.round(checks.filter(Boolean).length / checks.length * 100);
}

export const plainOrg = (s: string) => String(s).replace(/[«»"]/g, '').trim();
export function hue(id: string) {
  let h = 0;
  for (const c of String(id)) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return h % 5;
}

/* ---------- notifications: derived from the student's own data, so they stay truthful ---------- */
/** demo: built from a sample record — the list marks it «үлгі» (11-025) */
export interface Notif { id: string; k: string; icon: IconName; tone: number; go?: string; act?: 'deal'; data?: string; h: number; title: string; text: string; demo?: boolean }
export function notifs(S: AppState, I: I18n, C: Content): Notif[] {
  const { tt, L } = I;
  const p = S.profile, out: Notif[] = [];
  C.jobs.filter(j => j.spec.includes('all') || j.spec.includes(p.spec)).slice(0, 2).forEach((j, i) => out.push({
    id: 'nj-' + j.id, k: 'jobs', icon: 'briefcase', tone: 0, go: 'job:' + j.id, h: 2 + i * 5, demo: !!j.is_demo,
    title: tt('Жаңа жұмыс мүмкіндігі', 'Новая вакансия для вас'), text: tt(`${plainOrg(L(j.org))}: «${L(j.title)}», сізге сәйкес келеді`, `${plainOrg(L(j.org))}: «${L(j.title)}» подходит вам`),
  }));
  EVENTS.filter(e => !e.past).sort((a, b) => a.day - b.day).slice(0, 2).forEach((e, i) => out.push({
    id: 'ne-' + e.id, k: 'events', icon: 'calendar', tone: 1, go: 'event:' + e.id, h: 20 + i * 14, demo: true, // EVENTS are the sample list
    title: S.events[e.id] ? tt('Сіз тіркелген іс-шара жақында', 'Скоро ваше мероприятие') : tt('Іс-шара есігінде', 'Скоро мероприятие'),
    text: `${L(e.title)}, ${I.fDate(dOff(e.day))}, ${e.time}`,
  }));
  Object.keys(S.applied).forEach(id => {
    const j = C.jobs.find(x => x.id === id);
    if (!j) return;
    out.push(j.is_demo
      // a sample job has no employer behind it: nothing is sent and no reply will come
      ? { id: 'na-' + id, k: 'jobs', icon: 'check', tone: 0, go: 'job:' + id, h: 1, demo: true, title: tt('Өтінім белгіленді', 'Отклик отмечен'), text: tt(`«${L(j.title)}» — үлгі вакансия: өтінім тек осы құрылғыда сақталды, жұмыс берушіге жіберілмейді`, `«${L(j.title)}» — пример вакансии: отклик сохранён только на этом устройстве и работодателю не отправляется`) }
      : { id: 'na-' + id, k: 'jobs', icon: 'check', tone: 0, go: 'job:' + id, h: 1, title: tt('Өтінім жіберілді', 'Отклик отправлен'), text: tt(`«${L(j.title)}» бойынша жұмыс берушінің жауабын күтіңіз`, `Ждите ответа работодателя по вакансии «${L(j.title)}»`) });
  });
  C.discounts.slice(0, 2).forEach((d, i) => out.push({
    id: 'nd-' + d.id, k: 'deals', icon: 'tag', tone: 2, act: 'deal', data: d.id, h: 26 + i * 22, demo: !!d.is_demo,
    title: tt('Жеңілдік жаңартылды', 'Скидка обновлена'), text: `${plainOrg(L(d.name))}: ${d.pct}, ${L(d.what).toLowerCase()}`,
  }));
  openOpps(C.opportunities).filter(o => oppLeft(o) <= 21).slice(0, 1).forEach(o => out.push({
    id: 'no-' + o.id, k: 'jobs', icon: 'clock', tone: 3, go: 'opp:' + o.id, h: 30, demo: !!o.is_demo,
    title: tt('Өтінім мерзімі жақын', 'Скоро дедлайн'), text: `${L(o.title)}: ${I.daysLeftTxt(oppLeft(o)).toLowerCase()}`,
  }));
  out.push({ id: 'nm-study', k: 'study', icon: 'book', tone: 4, go: 'materials', h: 72, title: tt('Жаңа оқу материалы', 'Новые учебные материалы'), text: tt('Емтиханға дайындық пен карточкалар бөлімін қараңыз', 'Загляните в подготовку к экзаменам и карточки') });
  return out.sort((a, b) => a.h - b.h);
}
export const unreadCount = (S: AppState, I: I18n, C: Content) => notifs(S, I, C).filter(n => !S.read[n.id]).length;

/* ---------- one list for jobs, internships and deals ---------- */
export interface SearchItem { k: string; id: string; go?: string; act?: 'deal' | 'course'; org: string; title: string; meta: string; badge: [string, string] | null; fav: boolean; demo: boolean; noFav?: boolean; detail?: string; truncateDetail?: boolean }
export function searchItems(S: AppState, I: I18n, C: Content): SearchItem[] {
  const { tt, L } = I;
  const items: SearchItem[] = [];
  C.jobs.forEach((j, i) => items.push({
    k: 'jobs', id: j.id, go: 'job:' + j.id, org: L(j.org), title: L(j.title), meta: `${L(CITIES[j.city])} · ${tt('Жұмыс', 'Работа')}`,
    badge: j.today ? [tt('Бүгін', 'Сегодня'), 'new'] : i < 6 ? [tt('Жаңа', 'Новое'), 'new'] : null, fav: !!S.fav[j.id], demo: j.is_demo, detail: tt('Өтінім: ', 'Отклик: ') + jobApplyText(j, I),
  }));
  openOpps(C.opportunities).forEach(o => {
    const left = oppLeft(o);
    items.push({
      k: oppKind(o), id: o.id, go: 'opp:' + o.id, org: L(o.org), title: L(o.title), meta: typeLabel(o.type, I),
      badge: left <= 14 ? [I.daysLeftTxt(left), 'warn'] : o.funded ? [tt('Қаржыландырылады', 'С финансированием'), 'pick'] : null, fav: !!S.saved[o.id], demo: o.is_demo,
    });
  });
  C.discounts.forEach(d => items.push({ k: 'deals', id: d.id, act: 'deal', org: L(d.name), title: L(d.what), meta: L(DEAL_CATS[d.cat]), badge: [d.pct, 'deal'], fav: !!S.fav[d.id], demo: d.is_demo, detail: tt('Шарт: ', 'Условие: ') + L(d.cond), truncateDetail: true }));
  return items;
}
