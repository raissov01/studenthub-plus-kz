/* Mentors (prompt 07) on the client: API shapes (docs/api/mentors.md, the Go code wins) and labels. */
import type { LText } from '@/lib/types';

export interface MentorStats { university: string; mentors: number; freeMentors: number; waiting: number; typicalDays: number | null }
export interface MentorRequest { id: number; need: string; goal: string; expect: string; status: 'waiting' | 'matched' | 'closed'; createdAt: string; matchedAt: string | null }
export interface GroupMeeting { id: string; title: string; startsAt: string; place: string; online: boolean; url: string }
export interface MentorSummary {
  university: string | null; stats?: MentorStats;
  me?: { isAdult: boolean | null; request: MentorRequest | null; mentorStatus: string | null };
  groupMeetings: GroupMeeting[];
}
export interface Mentor {
  userId: number; university: string; areas: string[]; format: string; capacity: number; groupCapacity: number; about: string;
  alumni: boolean; status: 'pending' | 'approved' | 'paused' | 'rejected' | 'removed'; approvedAt: string | null; activePairs: number; badge: boolean;
}
export interface Pair {
  id: number; role: 'mentor' | 'mentee'; need: string; university: string; startedAt: string; continuedAt: string | null;
  endedAt: string | null; endReason: string | null; myScore: number | null;
  partner: { id: number; name: string; university: string; faculty: string; username: string | null };
  prompts: { kind: string; sentAt: string | null; answered: boolean }[]; due: ('check3' | 'month')[];
}
export interface WaitingRequest {
  id: number; userId: number; name: string; university: string; faculty: string; course: number | null; isAdult: boolean;
  need: string; goal: string; expect: string; createdAt: string; waitingDays: number; groupOnly: boolean;
}
export interface Candidate {
  userId: number; name: string; university: string; faculty: string; course: number | null; alumni: boolean; areas: string[]; format: string;
  about: string; capacity: number; activePairs: number; freeSlots: number; pastPairs: number; avgScore: number | null; score: number;
  reasons: { areaMatch: boolean; sameFaculty: boolean; freeSlots: number; avgScore: number | null };
}
export interface AdminPair {
  id: number; university: string; need: string; mentorId: number; mentorName: string; menteeId: number; menteeName: string;
  startedAt: string; continuedAt: string | null; endedAt: string | null; endReason: string | null; menteeScore: number | null; mentorScore: number | null;
}

export const NEEDS: Record<string, LText> = {
  adaptation: { kk: 'Бейімделу: жатақхана, құжаттар', ru: 'Адаптация: общежитие, документы' },
  study: { kk: 'Оқу', ru: 'Учёба' },
  career: { kk: 'Мансап', ru: 'Карьера' },
  life: { kk: 'Тұрмыс', ru: 'Быт' },
};
export const EXPECTS: Record<string, LText> = {
  one_on_one: { kk: 'Жеке ментор', ru: 'Личный ментор' },
  group: { kk: 'Топтық кездесу', ru: 'Групповая встреча' },
  any: { kk: 'Кез келгені', ru: 'Любой вариант' },
};
export const FORMATS: Record<string, LText> = {
  one_on_one: { kk: 'Тек жеке', ru: 'Только лично' },
  group: { kk: 'Тек топтық', ru: 'Только группы' },
  both: { kk: 'Екеуі де', ru: 'Оба формата' },
};
export const PAIR_REPORT_REASONS: Record<string, LText> = {
  disrespect: { kk: 'Құрметсіздік', ru: 'Неуважение' },
  money: { kk: 'Ақша не қаржылық дерек сұрады', ru: 'Просил(а) деньги или финансовые данные' },
  unsafe_meeting: { kk: 'Қауіпті кездесу орны', ru: 'Небезопасное место встречи' },
  no_show: { kk: 'Келмеді / жауап бермейді', ru: 'Не приходит / не отвечает' },
  other: { kk: 'Басқа', ru: 'Другое' },
};
export const SAFETY: LText = {
  kk: 'Әдеп: бір-біріңізді құрметтеңіз; ақша мен жеке қаржылық дерек сұрамаңыз; кездесулер — тек университет аумағында, адам көп жерде не онлайн (ересектерге де).',
  ru: 'Правила: уважайте друг друга; не просите деньги и личные финансовые данные; встречи — только на территории вуза, в людном месте или онлайн (для взрослых тоже).',
};
