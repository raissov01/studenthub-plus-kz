/* Default device state and transient UI state. No React here, so server code can use it too. */
import { emptyRoadmap } from '@/features/roadmap/logic';
import { cleanTimetable } from '@/lib/timetable';
import type { AppState, Lang } from '@/lib/types';

export function defaults(lang?: Lang): AppState {
  return {
    v: 1, lang: lang ?? 'kk', onboarded: false, theme: 'light', noteSeen: false,
    profile: {
      name: '', spec: '', course: 0, gpa: null, credits: 0, en: '', city: '', skills: [],
      exp: '', about: '', phone: '', email: '',
    },
    applied: {}, saved: {}, oppDocs: {}, fav: {}, read: {},
    remind: {}, done: {}, certs: [],
    plans: [], decks: [],
    chat: [],
    gpaPlan: [],
    roadmap: emptyRoadmap(),
    deadlines: [],
    calendarPins: [],
    timetable: [],
  };
}

/** Device state saved by older versions may still hold the removed sample content: the «Құралай» example profile,
    registrations for the sample events (e1…, p1…), the sample flashcard deck switch, pins of sample events. Drop it
    so nothing invented survives; the student's own data stays. Pure, so it is unit-tested (lib/profile.test.ts). */
export function dropDemo(s: AppState & { events?: unknown; hideDemoDeck?: unknown }): AppState {
  if (s.profile?.demo) {
    s.profile = defaults(s.lang).profile;
    s.onboarded = false; // they never entered their own profile
  }
  delete s.profile.demo;
  delete s.events;
  delete s.hideDemoDeck;
  s.decks = (Array.isArray(s.decks) ? s.decks : []).filter(d => d && d.id !== 'deck-demo');
  // sample events had ids like 'e1' / 'p2' (database events are numeric); the bundled sample vacancies and
  // opportunities were j1…j9 and o1…o9: their pinned copies carried invented titles
  s.calendarPins = (s.calendarPins ?? []).filter(p => !/^event:\D/.test(p.ref) && !/^(job:j|opp:o)[1-9]$/.test(p.ref));
  s.timetable = cleanTimetable(s.timetable);
  // courses that left the list: a «done» mark is the student's own record, so it becomes an own certificate
  for (const [id, at] of Object.entries(s.done ?? {})) {
    const c = OLD_COURSES[id];
    if (!c) continue;
    s.certs = [...(s.certs ?? []), { title: c[s.lang === 'ru' ? 1 : 0], org: c[2], date: new Date(at).toISOString() }];
    delete s.done[id];
  }
  return s;
}

/** the courses removed from lib/data/static COURSES (generic links, unverified titles): id -> [kk, ru, platform] */
const OLD_COURSES: Record<string, [string, string, string]> = {
  c1: ['Excel: деректерді талдау негіздері', 'Excel: основы анализа данных', 'Microsoft Learn'],
  c2: ['Power BI негіздері', 'Основы Power BI', 'Microsoft Learn'],
  c3: ['Қаржылық модельдеу', 'Финансовое моделирование', 'Coursera'],
  c4: ['Бизнеске арналған ағылшын тілі', 'Английский для бизнеса', 'Coursera'],
  c5: ['Корей тілі: алғашқы қадам', 'Корейский язык: первый шаг', 'Coursera'],
  c6: ['Қаржылық сауаттылық', 'Финансовая грамотность', 'Stepik'],
  c7: ['Бухгалтерлік есеп негіздері', 'Основы бухгалтерского учёта', 'Stepik'],
  c10: ['Маркетинг негіздері', 'Основы маркетинга', 'Coursera'],
  c11: ['Жобаларды басқару', 'Управление проектами', 'Coursera'],
  c12: ['Python-мен деректерді талдау', 'Анализ данных на Python', 'Stepik'],
};

export interface UiState {
  oppTab: string; q: string; jf: Record<string, boolean>; city: string; sortPay: boolean;
  evCat: string; courseMine: boolean; dealCat: string; mapQ: string; admTab: string; resumeLang: string | null;
  iv: number; chatBusy: boolean; chatErr: string; chatDraft: string; mapSel: string | null; mapNear: boolean;
  geoAuto: boolean; nearCat: string; sq: string; sk: string; nk: string; sqFocus: boolean; matchId: string;
  oppCheck: Record<string, boolean>; target: number; nextCr: number;
  /** «Күнтізбе»: selected day ('' = today), shown month 'YYYY-MM' ('' = the selected day's; its year in the year view), view, saved-only filter */
  calSel: string; calMonth: string; calView: 'month' | 'week' | 'year' | 'tasks'; calSaved: boolean;
  /** the «Сабақтар» layer: 'auto' = off in the month grid, on in the week list */
  calClasses: 'auto' | 'on' | 'off';
  study: null | { id: string; mode: string; queue: { q: string; a: string }[]; total: number; firstOk: number; wrong: string[]; flipped: boolean };
}
export const UI_INIT: UiState = {
  oppTab: 'jobs', q: '', jf: {}, city: 'all', sortPay: false, evCat: 'all', courseMine: false,
  dealCat: 'all', mapQ: '', admTab: 'bach', resumeLang: null, iv: 0, chatBusy: false, chatErr: '', chatDraft: '',
  mapSel: null, mapNear: false, geoAuto: false, nearCat: 'all', sq: '', sk: 'all', nk: 'all', sqFocus: false,
  matchId: '', oppCheck: {}, target: 3.5, nextCr: 60, study: null,
  calSel: '', calMonth: '', calView: 'month', calSaved: false, calClasses: 'auto',
};
