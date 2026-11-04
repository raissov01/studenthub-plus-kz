/* The one search screen: categories, which URL each one lives at, and the active-filter list.
   Old URLs (/jobs, /internships, /events, /courses, /discounts) render the same screen with their category. */
import type { UiState } from '@/lib/defaults';

export type SearchCat = 'all' | 'jobs' | 'intern' | 'grant' | 'mobility' | 'events' | 'courses' | 'deals' | 'market';
export const SEARCH_CATS: SearchCat[] = ['all', 'jobs', 'intern', 'grant', 'mobility', 'events', 'courses', 'deals', 'market'];
export const isSearchCat = (k: string | null | undefined): k is SearchCat => !!k && (SEARCH_CATS as string[]).includes(k);

/** the old screens' view ids -> their category ('opps:intern' -> 'intern', 'bonus' -> 'deals') */
export function catOfView(name: string, prm: string): SearchCat | undefined {
  if (name === 'bonus') return 'deals';
  if (name === 'opps') return isSearchCat(prm) && ['jobs', 'intern', 'events', 'courses'].includes(prm) ? prm : 'jobs';
  return undefined;
}

export const SEARCH_LABELS: Record<SearchCat, [string, string]> = {
  all: ['Барлығы', 'Все'],
  jobs: ['Жұмыс', 'Работа'],
  intern: ['Тағылымдама', 'Стажировки'],
  grant: ['Грант / байқау', 'Гранты / конкурсы'],
  mobility: ['Ұтқырлық', 'Мобильность'],
  events: ['Іс-шара', 'События'],
  courses: ['Курстар', 'Курсы'],
  deals: ['Жеңілдіктер', 'Скидки'],
  market: ['Тегін заттар', 'Отдам даром'],
};

/** opportunity types shown under the «Тағылымдама» category (grants and mobility have their own) */
export const INTERN_TYPES = ['intl', 'company'];
export const JOB_FLAGS = ['today', 'noexp', 'daily', 'remote', 'near'] as const;

/** One active filter of a category: what it is and the patch that clears it. */
export interface ActiveFilter { key: string; clear: Partial<UiState> }
export function activeFilters(cat: SearchCat, ui: UiState): ActiveFilter[] {
  const out: ActiveFilter[] = [];
  if (cat === 'jobs') {
    for (const k of JOB_FLAGS) if (ui.jf[k]) out.push({ key: 'jf:' + k, clear: { jf: { ...ui.jf, [k]: false } } });
    if (ui.city !== 'all') out.push({ key: 'city', clear: { city: 'all' } });
    if (ui.sortPay) out.push({ key: 'sortPay', clear: { sortPay: false } });
  } else if (cat === 'intern') {
    if (INTERN_TYPES.includes(ui.oppType)) out.push({ key: 'oppType', clear: { oppType: 'all' } });
  } else if (cat === 'events') {
    if (ui.evCat !== 'all') out.push({ key: 'evCat', clear: { evCat: 'all' } });
  } else if (cat === 'courses') {
    if (ui.courseMine) out.push({ key: 'courseMine', clear: { courseMine: false } });
  } else if (cat === 'deals') {
    if (ui.dealCat !== 'all') out.push({ key: 'dealCat', clear: { dealCat: 'all' } });
  }
  return out;
}

/** clears every filter of the category at once */
export function clearAll(cat: SearchCat): Partial<UiState> {
  switch (cat) {
    case 'jobs': return { jf: {}, city: 'all', sortPay: false };
    case 'intern': return { oppType: 'all' };
    case 'events': return { evCat: 'all' };
    case 'courses': return { courseMine: false };
    case 'deals': return { dealCat: 'all' };
    default: return {};
  }
}

/** case-insensitive «every word of the query is somewhere in the parts» */
export function matches(q: string, ...parts: (string | null | undefined)[]): boolean {
  const words = q.trim().toLowerCase().split(/\s+/).filter(Boolean);
  if (!words.length) return true;
  const hay = parts.filter(Boolean).join(' ').toLowerCase();
  return words.every(w => hay.includes(w));
}
