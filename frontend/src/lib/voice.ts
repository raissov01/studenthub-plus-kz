/* «University voice» client (docs/UNIVOICE.md): surveys, anonymous feedback tickets and the wellbeing section.
   Pure helpers and device storage only — tested in lib/voice.test.ts. */

export type QType = 'single' | 'multiple' | 'scale' | 'text' | 'matrix';
export interface Question {
  id: string; type: QType; text: string; help?: string; required: boolean;
  options?: string[]; max?: number; minLabel?: string; maxLabel?: string; rows?: string[];
}
export interface Audience { faculties: string[]; courses: number[]; groups: string[] }
export interface Survey {
  id: number; university: string; title: string; intro: string; questions: Question[]; audience: Audience; anonymous: boolean;
  status: 'draft' | 'published' | 'closed'; opensAt: string | null; closesAt: string | null; createdAt: string; publishedAt: string | null;
  responses: number; done?: boolean;
}
export type AnswerValue = number | number[] | string;
export type Answers = Record<string, AnswerValue>;

export interface QResult {
  id: string; type: QType; text: string; answered: number; hidden: boolean; labels?: string[]; counts?: number[];
  rows?: string[]; matrix?: number[][]; rowMeans?: number[]; mean?: number; texts?: string[];
}
export interface SurveyResults {
  survey: Survey; responses: number; minGroup: number; hidden: boolean; questions: QResult[];
  by?: string; segments?: { key: string; responses: number; questions: QResult[] }[]; breakdownHidden?: boolean;
}

/** the smallest group whose answers are ever shown (same as the server) */
export const K_MIN = 5;

/** is a value a real answer to q (an untouched optional question is not) */
export function isAnswered(q: Question, v: AnswerValue | undefined): boolean {
  if (v === undefined || v === null) return false;
  switch (q.type) {
    case 'single': return typeof v === 'number' && v >= 0 && v < (q.options?.length ?? 0);
    case 'multiple': return Array.isArray(v) && v.length > 0;
    case 'scale': return typeof v === 'number' && v >= 1 && v <= (q.max ?? 5);
    case 'text': return typeof v === 'string' && v.trim().length > 0;
    case 'matrix': return Array.isArray(v) && (q.required ? v.every(x => x > 0) : v.some(x => x > 0));
  }
}

/** may the student move past question q */
export const canContinue = (q: Question, v: AnswerValue | undefined) => !q.required || isAnswered(q, v);

/** the payload the server takes: unanswered optional questions are left out */
export function answersPayload(qs: Question[], a: Answers): Answers {
  const out: Answers = {};
  for (const q of qs) {
    const v = a[q.id];
    if (!isAnswered(q, v)) continue;
    out[q.id] = q.type === 'text' ? (v as string).trim() : v;
  }
  return out;
}

/** progress through the flow, 0..100 */
export const flowPct = (i: number, n: number) => (n <= 0 ? 0 : Math.round((Math.min(i, n) / n) * 100));

/** toggle an option of a multiple-choice answer */
export function toggle(v: AnswerValue | undefined, i: number): number[] {
  const cur = Array.isArray(v) ? v : [];
  return cur.includes(i) ? cur.filter(x => x !== i) : [...cur, i].sort((a, b) => a - b);
}

/** percentage of n in total, rounded */
export const pct = (n: number, total: number) => (total > 0 ? Math.round((n / total) * 100) : 0);

/* ---------- templates (structure only; no data) ---------- */
type TT = (kk: string, ru: string) => string;
export interface Template { key: string; title: string; questions: Question[] }

export function templates(tt: TT): Template[] {
  const scale5 = (id: string, text: string, required = true): Question =>
    ({ id, type: 'scale', text, required, max: 5, minLabel: tt('Өте нашар', 'Очень плохо'), maxLabel: tt('Өте жақсы', 'Отлично') });
  return [
    {
      key: 'teacher', title: tt('Оқытушыны бағалау', 'Оценка преподавателя'), questions: [
        { id: 'q1', type: 'matrix', text: tt('Оқытушыны бағалаңыз', 'Оцените преподавателя'), required: true, max: 5,
          minLabel: tt('Өте нашар', 'Очень плохо'), maxLabel: tt('Өте жақсы', 'Отлично'),
          rows: [tt('Түсіндіру анықтығы', 'Ясность объяснений'), tt('Сабаққа дайындық', 'Подготовка к занятиям'),
            tt('Бағалаудың әділдігі', 'Справедливость оценивания'), tt('Студенттерге құрмет', 'Уважение к студентам')] },
        { id: 'q2', type: 'single', text: tt('Сабақтар кестеге сай өте ме?', 'Проходят ли занятия по расписанию?'), required: true,
          options: [tt('Иә, әрқашан', 'Да, всегда'), tt('Көбіне', 'Чаще всего'), tt('Жиі ауысады', 'Часто переносятся')] },
        scale5('q3', tt('Жалпы бағаңыз', 'Общая оценка')),
        { id: 'q4', type: 'text', text: tt('Не жақсартуға болады?', 'Что можно улучшить?'), required: false },
      ],
    },
    {
      key: 'canteen', title: tt('Асхана сапасы', 'Качество столовой'), questions: [
        { id: 'q1', type: 'single', text: tt('Асханада қаншалықты жиі тамақтанасыз?', 'Как часто вы питаетесь в столовой?'), required: true,
          options: [tt('Күн сайын', 'Каждый день'), tt('Аптасына бірнеше рет', 'Несколько раз в неделю'), tt('Сирек', 'Редко'), tt('Мүлде', 'Никогда')] },
        { id: 'q2', type: 'matrix', text: tt('Бағалаңыз', 'Оцените'), required: false, max: 5,
          minLabel: tt('Өте нашар', 'Очень плохо'), maxLabel: tt('Өте жақсы', 'Отлично'),
          rows: [tt('Дәмі', 'Вкус'), tt('Бағасы', 'Цена'), tt('Тазалық', 'Чистота'), tt('Кезек', 'Очередь')] },
        { id: 'q3', type: 'multiple', text: tt('Мәзірге не қосу керек?', 'Что добавить в меню?'), required: false,
          options: [tt('Вегетариандық тағам', 'Вегетарианские блюда'), tt('Арзан ас', 'Бюджетный обед'), tt('Жеміс', 'Фрукты'), tt('Таңғы ас', 'Завтраки')] },
        { id: 'q4', type: 'text', text: tt('Қосымша пікір', 'Дополнительный комментарий'), required: false },
      ],
    },
    {
      key: 'dorm', title: tt('Жатақхана', 'Общежитие'), questions: [
        { id: 'q1', type: 'matrix', text: tt('Жатақхананы бағалаңыз', 'Оцените общежитие'), required: true, max: 5,
          minLabel: tt('Өте нашар', 'Очень плохо'), maxLabel: tt('Өте жақсы', 'Отлично'),
          rows: [tt('Жылу', 'Отопление'), tt('Ыстық су', 'Горячая вода'), tt('Интернет', 'Интернет'), tt('Қауіпсіздік', 'Безопасность'), tt('Тазалық', 'Чистота')] },
        { id: 'q2', type: 'scale', text: tt('Жатақханада оқуға жағдай қаншалықты қолайлы?', 'Насколько удобно учиться в общежитии?'), required: true, max: 10 },
        { id: 'q3', type: 'text', text: tt('Ең бірінші не шешілуі керек?', 'Что нужно решить в первую очередь?'), required: false },
      ],
    },
  ];
}

/* ---------- feedback ---------- */
export type TicketStatus = 'review' | 'new' | 'in_progress' | 'answered' | 'closed';
export interface Recipient { id: number; key: string; nameKk: string; nameRu: string; slaHours: number; active: boolean; university?: string; staff?: { userId: number; name: string | null; role: string }[] }
export interface TicketMessage { fromStaff: boolean; body: string; createdAt: string }
export interface Ticket {
  id: number; recipient: Recipient; category: string; body: string; photo?: string; hasPhoto: boolean; anonymous: boolean;
  status: TicketStatus | 'held'; flag?: string; unread: boolean; slaDueAt: string; overdue: boolean; createdAt: string; updatedAt: string;
  author?: { userId: number; name: string | null; role: string }; messages: TicketMessage[];
}
export const CATEGORIES = ['suggestion', 'complaint', 'question', 'gratitude'] as const;
export const FEEDBACK_MAX = 3000;

/* The same keyword heuristic as backend/internal/feedback/flags.go, so the notice shows BEFORE sending. */
const THREAT_ROOTS = ['убью', 'убьем', 'убить', 'убийств', 'зарежу', 'застрел', 'пристрел', 'взорв', 'бомб', 'теракт', 'стрельб', 'оружи',
  'суицид', 'самоубий', 'повешусь', 'повеситься', 'өлтір', 'жарып', 'жарылғыш', 'бомба', 'атып', 'қару', 'асылып', 'kill', 'bomb', 'shoot', 'suicide'];
const THREAT_PHRASES = ['покончить с собой', 'покончу с собой', 'не хочу жить', 'наложу на себя руки', 'свести счеты с жизнью',
  'өзіме қол жұмса', 'өмір сүргім келмейді', 'өлгім келеді', 'өзімді өлтір', 'end my life'];
const PROFANITY_ROOTS = ['хуй', 'хуе', 'хуё', 'хуя', 'пизд', 'ебат', 'ебан', 'ебал', 'ебу', 'заеб', 'уеб', 'выеб', 'бля', 'сука', 'суки', 'мудак', 'мудил',
  'долбоеб', 'гандон', 'пидор', 'пидар', 'шлюх', 'сіктір', 'сігейін', 'қотақ', 'амың', 'амыңды', 'fuck', 'shit'];

function normalize(s: string): string {
  return s.toLowerCase().replace(/ё/g, 'е').replace(/[^\p{L}\p{N}]+/gu, ' ').trim();
}

/** 'threat' | 'profanity' | '' */
export function detectFlag(text: string): '' | 'threat' | 'profanity' {
  const n = normalize(text);
  const words = n.split(' ').filter(Boolean);
  const has = (roots: string[]) => words.some(w => roots.some(r => w.startsWith(r)));
  if (has(THREAT_ROOTS)) return 'threat';
  const padded = ` ${n} `;
  if (THREAT_PHRASES.some(p => padded.includes(' ' + p))) return 'threat';
  if (has(PROFANITY_ROOTS)) return 'profanity';
  return '';
}

/* Anonymous ticket tokens live ONLY on this device (never on the account: that would link the ticket to a person). */
export interface StoredTicket { token: string; id: number; createdAt: string }
export const TOKENS_KEY = 'studenthub.feedback.v1';
export const TOKEN_RE = /^[A-Za-z0-9_-]{43}$/;

export function readTokens(store: Pick<Storage, 'getItem'> | null): StoredTicket[] {
  try {
    const v = JSON.parse(store?.getItem(TOKENS_KEY) || '[]');
    return Array.isArray(v) ? v.filter(x => x && TOKEN_RE.test(x.token) && Number.isInteger(x.id)).slice(0, 20) : [];
  } catch { return []; }
}
export function addToken(store: Pick<Storage, 'getItem' | 'setItem'> | null, t: StoredTicket): StoredTicket[] {
  const list = [t, ...readTokens(store).filter(x => x.token !== t.token)].slice(0, 20);
  try { store?.setItem(TOKENS_KEY, JSON.stringify(list)); } catch { /* private mode */ }
  return list;
}
export function removeToken(store: Pick<Storage, 'getItem' | 'setItem'> | null, token: string): StoredTicket[] {
  const list = readTokens(store).filter(x => x.token !== token);
  try { store?.setItem(TOKENS_KEY, JSON.stringify(list)); } catch { /* ignore */ }
  return list;
}

/* ---------- wellbeing ---------- */
export interface HelpLine { number: string; kk: string; ru: string; hours: { kk: string; ru: string }; sources: string[]; emergency?: boolean; extra?: string }

/** Kazakhstan numbers checked on official pages on 2026-11-05 (docs/UNIVOICE.md, «Тексерілген көздер»). */
export const HELP_LINES: HelpLine[] = [
  { number: '112', emergency: true, kk: 'Бірыңғай шұғыл көмек нөмірі (өрт, полиция, жедел жәрдем)', ru: 'Единый номер экстренных служб (пожарные, полиция, скорая)',
    hours: { kk: 'Тәулік бойы', ru: 'Круглосуточно' },
    sources: ['https://www.gov.kz/memleket/entities/emer/press/news/details/438709?lang=ru', 'https://www.gov.kz/article/19305?lang=ru'] },
  { number: '103', emergency: true, kk: 'Жедел медициналық жәрдем', ru: 'Скорая медицинская помощь', hours: { kk: 'Шұғыл жағдайда', ru: 'В экстренной ситуации' },
    sources: ['https://www.gov.kz/article/19305?lang=ru'] },
  { number: '102', emergency: true, kk: 'Полиция', ru: 'Полиция', hours: { kk: 'Шұғыл жағдайда', ru: 'В экстренной ситуации' },
    sources: ['https://www.gov.kz/article/19305?lang=ru'] },
  { number: '101', emergency: true, kk: 'Өрт сөндіру қызметі', ru: 'Служба пожаротушения', hours: { kk: 'Шұғыл жағдайда', ru: 'В экстренной ситуации' },
    sources: ['https://www.gov.kz/article/19305?lang=ru'] },
  { number: '150', kk: 'Балалар, жастар және зорлық-зомбылыққа тап болған адамдар үшін ұлттық сенім телефоны, тегін',
    ru: 'Национальная линия доверия для детей, молодёжи и людей в ситуации насилия, бесплатно',
    hours: { kk: 'Тәулік бойы', ru: 'Круглосуточно' }, extra: 'WhatsApp +7 708 106 08 10',
    sources: ['https://www.gov.kz/org/ombudsman-almaty/press/news/details/748239?lang=ru', 'https://childhelplineinternational.org/kazakhstan-telefon-150/'] },
  { number: '111', kk: 'Отбасы, әйелдер мен балалар құқығы жөніндегі байланыс орталығы, психологиялық қолдау',
    ru: 'Контакт-центр по вопросам семьи, прав женщин и детей, психологическая поддержка',
    hours: { kk: 'Тәулік бойы, қазақ және орыс тілінде', ru: 'Круглосуточно, на казахском и русском' },
    sources: ['https://www.gov.kz/situations/677/1457?lang=ru'] },
];
export const EMERGENCY_LINES = HELP_LINES.filter(h => h.emergency);

export interface SelfHelpCard { key: string; title: { kk: string; ru: string }; steps: { kk: string; ru: string }[]; source: { name: string; url: string } }
/** neutral, short, each from a public-health source; informational, not medical advice */
export const SELF_HELP: SelfHelpCard[] = [
  { key: 'breathing', title: { kk: 'Баяу тыныс алу', ru: 'Медленное дыхание' }, steps: [
    { kk: 'Ыңғайлы отырыңыз не жатыңыз, иығыңызды босатыңыз.', ru: 'Сядьте или лягте удобно, расслабьте плечи.' },
    { kk: 'Мұрынмен 5-ке дейін санап, ішпен баяу дем алыңыз.', ru: 'Медленно вдохните носом животом, считая до 5.' },
    { kk: 'Аузыңызбен 5-ке дейін санап, баяу дем шығарыңыз.', ru: 'Медленно выдохните ртом, считая до 5.' },
    { kk: 'Кемінде 5 минут қайталаңыз.', ru: 'Повторяйте не меньше 5 минут.' },
  ], source: { name: 'NHS — Breathing exercises for stress', url: 'https://www.nhs.uk/mental-health/self-help/guides-tools-and-activities/breathing-exercises-for-stress/' } },
  { key: 'sleep', title: { kk: 'Ұйқы', ru: 'Сон' }, steps: [
    { kk: 'Ересектерге тәулігіне кемінде 7 сағат ұйқы керек.', ru: 'Взрослым нужно не меньше 7 часов сна в сутки.' },
    { kk: 'Күн сайын бір уақытта жатып, бір уақытта тұрыңыз.', ru: 'Ложитесь и вставайте в одно и то же время каждый день.' },
    { kk: 'Бөлме тыныш, қараңғы және салқын болсын.', ru: 'Пусть в комнате будет тихо, темно и прохладно.' },
    { kk: 'Ұйықтардан 30 минут бұрын телефонды қойыңыз; кешке кофеиннен бас тартыңыз.', ru: 'Отложите телефон за 30 минут до сна; вечером откажитесь от кофеина.' },
  ], source: { name: 'CDC — About Sleep', url: 'https://www.cdc.gov/sleep/about/index.html' } },
  { key: 'exams', title: { kk: 'Емтихан алдындағы күйзеліс', ru: 'Стресс перед экзаменами' }, steps: [
    { kk: 'Шынайы қайталау кестесін жасаңыз.', ru: 'Составьте реалистичный план повторения.' },
    { kk: 'Жиі қысқа үзіліс жасаңыз.', ru: 'Регулярно делайте короткие перерывы.' },
    { kk: 'Қиын болса — оқытушыдан, курстастан не жақыныңыздан көмек сұраңыз.', ru: 'Если тяжело — попросите помощи у преподавателя, однокурсника или близких.' },
  ], source: { name: 'NHS — Tips on preparing for exams', url: 'https://www.nhs.uk/mental-health/children-and-young-adults/help-for-teenagers-young-adults-and-students/tips-on-preparing-for-exams/' } },
];

export interface WellbeingContact { university: string; name: string; role: string; phone: string | null; email: string | null; place: string; hours: string; updatedAt: string }

/** tel: link for a displayed number */
export const telHref = (n: string) => 'tel:' + n.replace(/[^\d+]/g, '');

/* Mood check-in: stays on this device, never sent anywhere. */
export interface MoodEntry { d: string; m: number }
export const MOOD_KEY = 'studenthub.mood.v1';
export function readMood(store: Pick<Storage, 'getItem'> | null): MoodEntry[] {
  try {
    const v = JSON.parse(store?.getItem(MOOD_KEY) || '[]');
    return Array.isArray(v) ? v.filter(x => x && /^\d{4}-\d{2}-\d{2}$/.test(x.d) && x.m >= 1 && x.m <= 5) : [];
  } catch { return []; }
}
/** one entry per day (the last pick wins), the newest 30 kept */
export function addMood(store: Pick<Storage, 'getItem' | 'setItem'> | null, day: string, m: number): MoodEntry[] {
  const list = [{ d: day, m }, ...readMood(store).filter(x => x.d !== day)].sort((a, b) => b.d.localeCompare(a.d)).slice(0, 30);
  try { store?.setItem(MOOD_KEY, JSON.stringify(list)); } catch { /* ignore */ }
  return list;
}
