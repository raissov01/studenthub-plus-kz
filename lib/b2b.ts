/* Employer and university cabinets (prompt 10) on the client: API shapes (docs/api/b2b.md, the Go code in pkg/b2b
   wins), labels, and the pure helpers for employer jobs merged into /api/content. Free of React. */
import type { Job, LText, Opportunity } from '@/lib/types';

/* ---------- public jobs (/api/content → jobs, /api/jobs) ---------- */
export interface ResponseStats { applications: number; days: number | null; rate: number | null }
export interface EmployerBadgeInfo {
  name: string; website: string; about: string; verified: boolean; verified_at: string | null;
  verification_meaning: { kk: string; ru: string };
}
/** a database job: the data/jobs.json shape plus these fields (source = "employer") */
export interface EmployerJobFields {
  source?: 'employer';
  employer?: EmployerBadgeInfo;
  adult_only?: boolean;
  remote?: boolean;
  address?: string;
  apply?: LText;
  response?: ResponseStats | null;
  low_response?: boolean;
}
export type AnyJob = Job & EmployerJobFields;

export const isEmployerJob = (j: Job): j is Job & Required<Pick<EmployerJobFields, 'employer'>> & EmployerJobFields =>
  (j as AnyJob).source === 'employer' && /^job-\d+$/.test(j.id);
/** "job-12" → 12 (the API path id); 0 for demo / scraped jobs */
export const jobDbId = (id: string) => (/^job-\d+$/.test(id) ? Number(id.slice(4)) : 0);

/** /api/content is public and cached, so it cannot filter by age: a signed-in student whose isAdult is not true
    never sees adult-only jobs (the API refuses their applications anyway). Anonymous visitors see everything. */
export function jobsForViewer(jobs: Job[], signedIn: boolean, isAdult: boolean | null | undefined): Job[] {
  if (!signedIn || isAdult === true) return jobs;
  return jobs.filter(j => !(j as AnyJob).adult_only);
}

/** «Университеттен ресми» opportunities (source = "university") */
export interface OfficialFields { source?: 'university' | string; official?: boolean; university?: string; free?: boolean }
export const isOfficial = (o: Opportunity) => !!(o as Opportunity & OfficialFields).official;

/* ---------- applications ---------- */
export type AppStatus = 'sent' | 'viewed' | 'invited' | 'rejected' | 'no_response' | 'withdrawn';
export interface SharedFields {
  name?: string; course?: number; specialty?: string; telegram?: string; telegramUrl?: string;
  skills?: string[]; experience?: { title: string; org: string; period: string }[]; phone?: string; letter?: string;
}
export interface MyApplication {
  id: number; jobId: string; title: string; titleRu: string; company: string; status: AppStatus;
  inviteMessage: string | null; sharedFields: SharedFields; createdAt: string; updatedAt: string;
}
export interface EmployerApplication {
  id: number; status: AppStatus; sharedFields: SharedFields; inviteMessage: string | null; createdAt: string;
  viewedAt: string | null; respondedAt: string | null; daysWaiting: number;
}
export const APP_STATUS: Record<AppStatus, LText> = {
  sent: { kk: 'Жіберілді', ru: 'Отправлен' },
  viewed: { kk: 'Қаралды', ru: 'Просмотрен' },
  invited: { kk: 'Шақырылды', ru: 'Приглашение' },
  rejected: { kk: 'Бас тартылды', ru: 'Отказ' },
  no_response: { kk: 'Жауап болмады', ru: 'Нет ответа' },
  withdrawn: { kk: 'Кері қайтарылды', ru: 'Отозван' },
};
export const appStatusTone = (s: AppStatus) => (s === 'invited' ? 'b-ok' : s === 'rejected' || s === 'no_response' ? 'b-bad' : s === 'withdrawn' ? 'b-line' : 'b-sky');
export const APPLY_LIMITS = { letter: 300, phone: 20, skills: 15, experience: 10 };

export const JOB_REPORT_REASONS: Record<string, LText> = {
  scam: { kk: 'Алаяқтық', ru: 'Мошенничество' },
  fee: { kk: 'Ақы сұрады', ru: 'Просили оплату' },
  fake: { kk: 'Сипаттама өтірік', ru: 'Описание не соответствует' },
  other: { kk: 'Басқа', ru: 'Другое' },
};

/* ---------- employer cabinet ---------- */
export type EmployerStatus = 'pending' | 'verified' | 'rejected' | 'suspended' | 'closed';
export interface Employer {
  id: number; name: string; bin: string; website: string; contactName: string; contactPhone: string; address: string; about: string;
  status: EmployerStatus; statusReason: string | null; verifiedAt: string | null; createdAt: string;
}
export interface EmployerMe {
  employer: Employer | null; role?: 'owner' | 'member'; rulesAccepted?: boolean; jobs?: Record<string, number>;
  response?: ResponseStats | null; verificationMeaning: { kk: string; ru: string };
}
export interface Member { userId: number; name: string | null; username: string | null; role: 'owner' | 'member'; joinedAt: string; me: boolean }
export type JobStatus = 'pending' | 'published' | 'rejected' | 'hidden' | 'closed' | 'expired';
export interface EmployerJob {
  id: number; titleKk: string; titleRu: string; descKk: string; descRu: string; city: string; remote: boolean; address: string;
  pay: number; per: 'shift' | 'hour' | 'month'; hours: number | null; schedKk: string; schedRu: string; tags: string[]; skills: string[];
  spec: string[]; req: LText[]; applyKk: string; applyRu: string; minorOk: boolean; status: JobStatus; hiddenReason: 'reports' | 'admin' | null;
  statusReason: string | null; publishedAt: string | null; expiresAt: string | null; createdAt: string; applications?: Partial<Record<AppStatus, number>>;
}
export const EMPLOYER_STATUS: Record<EmployerStatus, LText> = {
  pending: { kk: 'Тексеруде', ru: 'На проверке' },
  verified: { kk: 'Тексерілген', ru: 'Проверено' },
  rejected: { kk: 'Қайтарылды', ru: 'Отклонено' },
  suspended: { kk: 'Тоқтатылған', ru: 'Приостановлено' },
  closed: { kk: 'Жабылған', ru: 'Закрыто' },
};
export const JOB_STATUS: Record<JobStatus, LText> = {
  pending: { kk: 'Тексеруде', ru: 'На проверке' },
  published: { kk: 'Жарияланған', ru: 'Опубликовано' },
  rejected: { kk: 'Қайтарылды', ru: 'Отклонено' },
  hidden: { kk: 'Жасырылған', ru: 'Скрыто' },
  closed: { kk: 'Жабылған', ru: 'Закрыто' },
  expired: { kk: 'Мерзімі өтті', ru: 'Срок истёк' },
};
export const jobStatusTone = (s: JobStatus) => (s === 'published' ? 'b-ok' : s === 'rejected' || s === 'hidden' ? 'b-bad' : s === 'pending' ? 'b-sky' : 'b-line');
export const PER_LABEL: Record<'shift' | 'hour' | 'month', LText> = {
  shift: { kk: 'Ауысымға', ru: 'За смену' },
  hour: { kk: 'Сағатына', ru: 'В час' },
  month: { kk: 'Айына', ru: 'В месяц' },
};
/** the rules screen before the first job (the server also refuses these words: 400 forbidden_terms) */
export const JOB_RULES: LText[] = [
  { kk: 'Студенттен алдын ала ақы, депозит, «оқу курсын сатып алу», форма не құрал үшін төлем сұрауға тыйым салынады.', ru: 'Запрещено требовать от студента предоплату, депозит, «покупку обучающего курса», оплату формы или инструментов.' },
  { kk: 'Төлем мөлшері мен түрі (ауысым / сағат / ай), кесте және нақты мекенжай (не «қашықтан») міндетті түрде жазылады.', ru: 'Размер и вид оплаты (смена / час / месяц), график и точный адрес (или «удалённо») указываются обязательно.' },
  { kk: '18-ге толмағандарға сай емес жұмыс (түнгі ауысым, алкоголь сатылатын орын, ауыр жұмыс) солай белгіленеді.', ru: 'Работа, не подходящая несовершеннолетним (ночные смены, места продажи алкоголя, тяжёлый труд), отмечается как таковая.' },
  { kk: 'Әр өтінімге жауап беріңіз: 14 күн жауапсыз қалса, өтінім «жауап болмады» болады және көрсеткішіңіз төмендейді.', ru: 'Отвечайте на каждый отклик: без ответа 14 дней отклик получает статус «нет ответа», и ваш показатель снижается.' },
  { kk: 'Алғашқы 3 вакансияны әкімші тексереді; кейінгілері бірден жарияланады, бірақ шағым арқылы бақыланады.', ru: 'Первые 3 вакансии проверяет администратор; следующие публикуются сразу, но контролируются через жалобы.' },
];

/* ---------- university cabinet ---------- */
export type Count = number | string;
export interface UniversityStats {
  university: string; periodDays: number; students: Count; weeklyActive: Count; byFaculty: Record<string, Count>; byCourse: Record<string, Count>;
  savedDeadlines: Count; events: Count; registrations: Count; checkIns: Count; ratings: Count; avgRating: number | null;
  mentors: Count; mentorRequests: Count; mentorPairs: Count;
}
export interface UniversityPost { id: number; university: string; kind: 'opportunity' | 'announcement'; payload: Record<string, unknown>; deadline: string | null; hidden: boolean; publishedAt: string }
export interface Announcement {
  id: string; university: string; title: LText; body: LText; url: string; until: string | null; publishedAt: string; source: 'university'; official: true;
}

/* ---------- admin ---------- */
export interface AdminEmployer extends Employer { members: number; jobs: number; response: ResponseStats | null; lowRate: boolean }
export interface StaffRow { userId: number; name: string | null; username: string | null; university: string; role: string; createdAt: string }
