/* Filters of «Іздеу» and of the sections (lib/sections.ts): which filters are on, the patch that clears them,
   and the query match. «Іздеу» (/search) searches everything; its only filter is the scope (ui.sk), picked in the
   filter sheet. Each section has its own filters (jobs: flags, city, order; events: category; ...). */
import type { UiState } from '@/lib/defaults';
import { isSectionId, type SectionId } from '@/lib/sections';

/** whose filters: «Іздеу» ('all'), a section, or the «Тегін беремін» tab ('market', no filters) */
export type FilterCat = 'all' | SectionId | 'market';

/** the scope of «Іздеу»: everything or one section */
export type Scope = 'all' | SectionId;
export const SCOPES: Scope[] = ['all', 'jobs', 'intern', 'events', 'courses', 'deals'];
export const isScope = (k: string | null | undefined): k is Scope => k === 'all' || isSectionId(k);

/** a search item's kind (lib/domain searchItems + events, courses) -> the section it belongs to */
export function scopeOfKind(k: string): SectionId {
  if (k === 'intern' || k === 'grant' || k === 'mobility') return 'intern';
  return isSectionId(k) ? k : 'jobs';
}

export const JOB_FLAGS = ['today', 'noexp', 'daily', 'remote', 'near'] as const;

/** One active filter: what it is and the patch that clears it. */
export interface ActiveFilter { key: string; clear: Partial<UiState> }
export function activeFilters(cat: FilterCat, ui: UiState): ActiveFilter[] {
  const out: ActiveFilter[] = [];
  if (cat === 'all') {
    if (isScope(ui.sk) && ui.sk !== 'all') out.push({ key: 'scope', clear: { sk: 'all' } });
  } else if (cat === 'jobs') {
    for (const k of JOB_FLAGS) if (ui.jf[k]) out.push({ key: 'jf:' + k, clear: { jf: { ...ui.jf, [k]: false } } });
    if (ui.city !== 'all') out.push({ key: 'city', clear: { city: 'all' } });
    if (ui.sortPay) out.push({ key: 'sortPay', clear: { sortPay: false } });
  } else if (cat === 'events') {
    if (ui.evCat !== 'all') out.push({ key: 'evCat', clear: { evCat: 'all' } });
  } else if (cat === 'courses') {
    if (ui.courseMine) out.push({ key: 'courseMine', clear: { courseMine: false } });
  } else if (cat === 'deals') {
    if (ui.dealCat !== 'all') out.push({ key: 'dealCat', clear: { dealCat: 'all' } });
  }
  return out;
}

/** clears every filter of the screen at once */
export function clearAll(cat: FilterCat): Partial<UiState> {
  switch (cat) {
    case 'all': return { sk: 'all' };
    case 'jobs': return { jf: {}, city: 'all', sortPay: false };
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
