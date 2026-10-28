/* Default device state and transient UI state. No React here, so server code can use it too. */
import { emptyRoadmap } from '@/features/roadmap/logic';
import type { AppState, Lang } from '@/lib/types';

export function defaults(lang?: Lang): AppState {
  const ru = lang ? lang === 'ru' : typeof navigator !== 'undefined' && /^ru/i.test(navigator.language || '');
  return {
    v: 1, lang: ru ? 'ru' : 'kk', onboarded: false, theme: 'light', noteSeen: false,
    profile: {
      name: ru ? 'Куралай' : 'Құралай', spec: 'econ', course: 2, gpa: 3.13, credits: 60, en: 'B1',
      city: 'astana', skills: ['excel', 'comm', 'service', 'kz', 'ru', 'econ'],
      exp: ru ? 'Официант в кафе «Дала», 3 месяца (лето 2026)\nВолонтёр на городском молодёжном форуме'
        : '«Дала» кафесінде даяшы, 3 ай (2026 жылдың жазы)\nҚалалық жастар форумында ерікті',
      about: '', phone: '', email: '', demo: true,
    },
    applied: {}, saved: {}, oppDocs: {}, fav: {}, read: {},
    events: { p1: { code: 'EV-P1-4821' }, p2: { code: 'EV-P2-1937' } },
    remind: {}, done: {}, certs: [],
    plans: [], decks: [], hideDemoDeck: false,
    chat: [],
    gpaPlan: [
      { n: 'Микроэкономика', cr: 5, g: 'B+' },
      { n: 'Статистика', cr: 5, g: 'B' },
      { n: ru ? 'Английский язык' : 'Ағылшын тілі', cr: 4, g: 'A-' },
    ],
    roadmap: emptyRoadmap(),
  };
}

export interface UiState {
  oppTab: string; q: string; jf: Record<string, boolean>; city: string; sortPay: boolean; oppType: string;
  evCat: string; courseMine: boolean; dealCat: string; mapQ: string; admTab: string; resumeLang: string | null;
  iv: number; chatBusy: boolean; chatErr: string; chatDraft: string; mapSel: string | null; mapNear: boolean;
  geoAuto: boolean; nearCat: string; sq: string; sk: string; nk: string; sqFocus: boolean; matchId: string;
  oppCheck: Record<string, boolean>; target: number; nextCr: number;
  study: null | { id: string; mode: string; queue: { q: string; a: string }[]; total: number; firstOk: number; wrong: string[]; flipped: boolean };
}
export const UI_INIT: UiState = {
  oppTab: 'jobs', q: '', jf: {}, city: 'all', sortPay: false, oppType: 'all', evCat: 'all', courseMine: false,
  dealCat: 'all', mapQ: '', admTab: 'bach', resumeLang: null, iv: 0, chatBusy: false, chatErr: '', chatDraft: '',
  mapSel: null, mapNear: false, geoAuto: false, nearCat: 'all', sq: '', sk: 'all', nk: 'all', sqFocus: false,
  matchId: 'j8', oppCheck: {}, target: 3.5, nextCr: 60, study: null,
};
