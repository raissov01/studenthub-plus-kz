/* Community features on the client (docs/api/community.md; the Go code in backend/internal/community wins):
   «Табылған заттар» (lost & found), «Пән бойынша сұрақ-жауап» (course Q&A) and «Пәтерлес» (roommates).
   API shapes, labels and the same text rules the server applies, so the form can warn before sending. Pure. */
import type { LText } from '@/lib/types';

/* ---------- shared ---------- */

/** the other side's contact: only after the poster confirmed / the owner accepted */
export interface Contact { name: string; username: string | null; telegramUrl: string | null; email: string | null }

// the server's rule (Bagdar's has_contacts plus e-mails and bare domains); keep in step with community.go
const CONTACT_RE = /@[a-z0-9_]{4,}|t\.me\/|wa\.me\/|https?:\/\/|www\.|[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}|\b[a-z0-9-]+\.(kz|ru|com|me|org|net|io)\b/i;
const PHONE_RE = /\+?\d{7,}/;
const HANDLE_RE = /(^|[^a-z0-9._%+-])@[a-z0-9_]{4,}/i;
const squeeze = (s: string) => s.replace(/[\s\-().]/g, '');

/** a phone, @handle, e-mail or link: contacts must wait until both sides agree */
export const hasContact = (s: string) => CONTACT_RE.test(s) || PHONE_RE.test(squeeze(s));
/** Q&A: study links are fine, personal contacts are not */
export const hasPhoneOrHandle = (s: string) => HANDLE_RE.test(s) || PHONE_RE.test(squeeze(s));
/** 6+ digits in a row: an ID, card or student-ticket number */
export const hasLongNumber = (s: string) => /\d{6,}/.test(squeeze(s));

/** the subject board key, as the server computes it: «Математика-1» = «математика 1» */
export function normKey(s: string): string {
  const parts = s.toLowerCase().replace(/ё/g, 'е').split(/[^\p{L}\p{N}]+/u).filter(Boolean);
  return Array.from(parts.join(' ')).slice(0, 80).join('').trim();
}

/** today in Asia/Almaty, YYYY-MM-DD */
export function almatyToday(now = Date.now()): string {
  return new Date(now + 5 * 3600_000).toISOString().slice(0, 10);
}
export function addDays(ymd: string, days: number): string {
  const d = new Date(ymd + 'T00:00:00Z');
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString().slice(0, 10);
}

/** «45 000 ₸» */
export const tenge = (n: number) => n.toLocaleString('ru-RU').replace(/ /g, ' ') + ' ₸';

/* ---------- lost & found ---------- */

export type LFKind = 'lost' | 'found';
export interface LFPost {
  id: number; kind: LFKind; category: string; title: string; desc: string; building: string; room: string; happenedOn: string;
  photo: string | null; handedTo: '' | 'dean' | 'security'; university: string;
  status: 'active' | 'closed' | 'resolved' | 'hidden' | 'archived'; hiddenReason?: 'reports' | 'admin';
  createdAt: string; expiresAt: string; mine: boolean; pendingClaims?: number;
}
export interface LFClaim {
  id: number; postId: number; message: string; status: 'pending' | 'confirmed' | 'declined' | 'returned' | 'withdrawn';
  createdAt: string; iReturned: boolean; theyReturned: boolean; contact: Contact | null; postTitle?: string | null; postKind?: LFKind | null;
}
export interface LFList { posts: LFPost[]; university: string; nextBefore?: number }
export interface LFDetail { post: LFPost; myClaim: LFClaim | null; claims?: LFClaim[] }

export const LF_LIMITS = { title: 80, desc: 500, room: 20, message: 300 };
export const LF_CATS: Record<string, LText> = {
  documents: { kk: 'Құжат (жеке куәлік, паспорт)', ru: 'Документ (удостоверение, паспорт)' },
  cards: { kk: 'Карта (студенттік, банк)', ru: 'Карта (студенческий, банковская)' },
  keys: { kk: 'Кілт', ru: 'Ключи' },
  phone: { kk: 'Телефон', ru: 'Телефон' },
  electronics: { kk: 'Техника, құлаққап', ru: 'Техника, наушники' },
  bag: { kk: 'Сөмке, әмиян', ru: 'Сумка, кошелёк' },
  clothes: { kk: 'Киім', ru: 'Одежда' },
  books: { kk: 'Кітап, дәптер', ru: 'Книги, тетради' },
  other: { kk: 'Басқа', ru: 'Другое' },
};
export const LF_HANDED: Record<string, LText> = {
  dean: { kk: 'Деканатқа тапсырылды', ru: 'Сдано в деканат' },
  security: { kk: 'Күзетке тапсырылды', ru: 'Сдано на охрану' },
};
/** identity documents and cards: no photo, no number, a found one goes to the dean's office or security */
export const isSensitive = (category: string) => category === 'documents' || category === 'cards';

export interface LFDraft { kind: LFKind; category: string; title: string; desc: string; room: string; photo: string; handedTo: string; happenedOn: string }
/** the first rule the draft breaks (the server's error code), or '' */
export function lfProblem(d: LFDraft, today = almatyToday()): string {
  const title = d.title.trim(), desc = d.desc.trim();
  if (Array.from(title).length < 3 || Array.from(title).length > LF_LIMITS.title || Array.from(desc).length > LF_LIMITS.desc) return 'bad_text';
  if (isSensitive(d.category) && d.photo) return 'sensitive_photo';
  if (isSensitive(d.category) && d.kind === 'found' && !d.handedTo) return 'handed_required';
  if (isSensitive(d.category) && hasLongNumber(title + '\n' + desc)) return 'has_number';
  if (hasContact(title + '\n' + desc + '\n' + d.room)) return 'has_contact';
  if (hasLongNumber(title + '\n' + desc)) return 'has_number';
  if (!d.happenedOn || d.happenedOn > today || d.happenedOn < addDays(today, -90)) return 'bad_date';
  return '';
}

/* ---------- course Q&A ---------- */

export interface QAAuthor { name: string }
export interface QAQuestion {
  id: number; subject: string; subjectKey: string; facultyOnly: boolean; title: string; body: string; tags: string[]; anonymous: boolean;
  author: QAAuthor | null; status: 'active' | 'hidden' | 'deleted'; locked: boolean; lockedReason: string | null; acceptedAnswerId: number | null;
  answers: number; score: number; voted: boolean; mine: boolean; createdAt: string; authorId?: number;
}
export interface QAAnswer {
  id: number; body: string; anonymous: boolean; author: QAAuthor | null; score: number; voted: boolean; mine: boolean; accepted: boolean;
  createdAt: string; authorId?: number;
}
export interface QAList { questions: QAQuestion[]; subjectKey: string; university: string; facultyScoped: boolean; nextBefore?: number }
export interface QADetail { question: QAQuestion; answers: QAAnswer[] }

export const QA_LIMITS = { subject: 80, title: 150, body: 2000, answer: 2000, tags: 5 };
const TAG_RE = /^[\p{L}\p{N}+#-]{2,24}$/u;
/** «#Туынды, емтихан» → ['туынды', 'емтихан']; null when a tag is invalid or there are too many */
export function parseTags(s: string): string[] | null {
  const out: string[] = [];
  for (const raw of s.split(/[,\s]+/)) {
    const t = raw.trim().replace(/^#/, '').toLowerCase();
    if (!t) continue;
    if (!TAG_RE.test(t)) return null;
    if (!out.includes(t)) out.push(t);
  }
  return out.length > QA_LIMITS.tags ? null : out;
}

/** the /qa?id= parameter: '' list, 'new', 'new:<subject>', 's:<subject>' a subject board, '<n>' a question */
export type QARoute = { kind: 'list' } | { kind: 'new'; subject: string } | { kind: 'board'; subject: string } | { kind: 'question'; id: string };
export function parseQaRoute(prm: string | null): QARoute {
  if (!prm) return { kind: 'list' };
  if (/^\d+$/.test(prm)) return { kind: 'question', id: prm };
  if (prm === 'new' || prm.startsWith('new:')) return { kind: 'new', subject: prm.slice(4) };
  if (prm.startsWith('s:')) return { kind: 'board', subject: prm.slice(2) };
  return { kind: 'list' };
}
export const qaBoard = (subject: string) => 'qa:s:' + subject;

/* ---------- roommates ---------- */

export type RMKind = 'have_room' | 'looking';
export interface RMListing {
  id: number; kind: RMKind; housing: 'dorm' | 'flat'; district: string; budgetMin: number; budgetMax: number; moveIn: string; habits: string[];
  about: string; sameGender: boolean; gender: 'f' | 'm' | null; university: string; status: 'active' | 'closed' | 'hidden';
  createdAt: string; expiresAt: string; mine: boolean; myRequest: 'pending' | 'accepted' | 'declined' | null; common?: number;
}
export interface RMRequest {
  id: number; listingId: number; status: 'pending' | 'accepted' | 'declined'; createdAt: string; incoming: boolean;
  course?: number | null; university?: string | null; listing: RMListing | null; contact: Contact | null;
}
export interface RMMe { gender: 'f' | 'm' | null; listing: RMListing | null; requestsToday: number; dailyLimit: number; blockers: string[] }

export const RM_LIMITS = { district: 60, about: 400, habits: 8, budgetMin: 5000, budgetMax: 1_000_000 };
export const RM_KINDS: Record<RMKind, LText> = {
  have_room: { kk: 'Бөлмем бар, көрші іздеймін', ru: 'Есть комната, ищу соседа' },
  looking: { kk: 'Бірге жалдайық', ru: 'Ищу, с кем снять' },
};
export const RM_HOUSING: Record<string, LText> = { dorm: { kk: 'Жатақхана', ru: 'Общежитие' }, flat: { kk: 'Пәтер', ru: 'Квартира' } };
export const RM_HABITS: Record<string, LText> = {
  no_smoking: { kk: 'Темекі шекпеймін', ru: 'Не курю' },
  smoker: { kk: 'Темекі шегемін', ru: 'Курю' },
  early_bird: { kk: 'Ерте тұрамын', ru: 'Жаворонок' },
  night_owl: { kk: 'Кеш жатамын', ru: 'Сова' },
  quiet: { kk: 'Тыныштық', ru: 'Тишина' },
  social: { kk: 'Қонақ жиі келеді', ru: 'Часто гости' },
  tidy: { kk: 'Тазалық', ru: 'Чистота' },
  pets_ok: { kk: 'Үй жануары болса болады', ru: 'Можно с питомцем' },
  no_pets: { kk: 'Жануарсыз', ru: 'Без животных' },
  cooks: { kk: 'Үйде тамақ істеймін', ru: 'Готовлю дома' },
  sport: { kk: 'Спорт', ru: 'Спорт' },
  studious: { kk: 'Көп оқимын', ru: 'Много учусь' },
};
export const RM_GENDER: Record<'f' | 'm', LText> = { f: { kk: 'Қыздар ғана', ru: 'Только девушки' }, m: { kk: 'Ұлдар ғана', ru: 'Только парни' } };

export interface RMDraft { district: string; about: string; budgetMin: number; budgetMax: number; moveIn: string; habits: string[]; sameGender: boolean }
export function rmProblem(d: RMDraft, gender: 'f' | 'm' | null, today = almatyToday()): string {
  const district = d.district.trim(), about = d.about.trim();
  if (Array.from(district).length < 2 || Array.from(district).length > RM_LIMITS.district || Array.from(about).length > RM_LIMITS.about) return 'bad_text';
  if (hasContact(district + '\n' + about)) return 'has_contact';
  if (!Number.isFinite(d.budgetMin) || !Number.isFinite(d.budgetMax) || d.budgetMin < 0 || d.budgetMax < RM_LIMITS.budgetMin
    || d.budgetMax > RM_LIMITS.budgetMax || d.budgetMin > d.budgetMax) return 'bad_budget';
  const six = new Date(today + 'T00:00:00Z');
  six.setUTCMonth(six.getUTCMonth() + 6);
  if (!d.moveIn || d.moveIn < today || d.moveIn > six.toISOString().slice(0, 10)) return 'bad_move_in';
  if (d.habits.length > RM_LIMITS.habits) return 'bad_text';
  if (d.sameGender && !gender) return 'need_gender';
  return '';
}

/** the client's copy of the server's messages for the codes above */
export const RULE_MSG: Record<string, LText> = {
  bad_text: { kk: 'Мәтін тым қысқа не тым ұзын.', ru: 'Текст слишком короткий или слишком длинный.' },
  has_contact: { kk: 'Мәтінге телефон, @username не сілтеме жазбаңыз: байланыс екі жақ келіскенде ғана ашылады.', ru: 'Не пишите в тексте телефон, @username или ссылку: контакты открываются только по согласию обеих сторон.' },
  has_number: { kk: 'Құжат не карта нөмірін жарияламаңыз (6+ цифр қатар). Иесі оны өзі айтады.', ru: 'Не публикуйте номер документа или карты (6+ цифр подряд). Владелец назовёт его сам.' },
  sensitive_photo: { kk: 'Құжат пен картаның фотосын жарияламаңыз.', ru: 'Не публикуйте фото документов и карт.' },
  handed_required: { kk: 'Табылған құжат не картаны деканатқа не күзетке тапсырып, қайсысы екенін белгілеңіз.', ru: 'Найденный документ или карту сдайте в деканат или охрану и отметьте, куда.' },
  bad_date: { kk: 'Күні соңғы 90 күн ішінде болсын.', ru: 'Дата должна быть в пределах последних 90 дней.' },
  bad_budget: { kk: 'Бюджет: 5 000 – 1 000 000 ₸, ең азы ең көбінен аспасын.', ru: 'Бюджет: 5 000 – 1 000 000 ₸, минимум не больше максимума.' },
  bad_move_in: { kk: 'Көшу күні бүгіннен бастап 6 ай ішінде болсын.', ru: 'Дата заезда — в ближайшие 6 месяцев.' },
  need_gender: { kk: '«Тек өз жынысым» шарты үшін жынысыңызды белгілеңіз (профильде көрсетілмейді).', ru: 'Для условия «только мой пол» укажите пол (в профиле не показывается).' },
  bad_tags: { kk: 'Тегтер: 5-ке дейін, әрқайсысы 2–24 әріп не цифр.', ru: 'Теги: до 5, каждый 2–24 буквы или цифры.' },
};
