/* The big sections (Жұмыс, Тағылымдама, Іс-шаралар, Курстар, Жеңілдіктер) and their own sub-tabs.
   Each section lives at its old URL (/jobs, /internships, /events, /courses, /discounts); the sub-tab rides in the
   query string (/jobs?t=youth), so the prerendered pages stay static and a link or «back» lands on the same tab.
   Where an item goes is decided here from its data (Job.subtype, Opportunity.type + subtype, event cat), with a
   default when the optional field is absent (docs/CONTENT.md, «Бөлімдер мен ішкі қойындылар»). Pure: no React. */
import type { Job, Opportunity } from '@/lib/types';

export type SectionId = 'jobs' | 'intern' | 'events' | 'courses' | 'deals';
export type Pair = [string, string];

export interface SectionTab { id: string; label: Pair }
export interface Section { id: SectionId; view: string; label: Pair; tabs: SectionTab[] }

export const SECTIONS: Record<SectionId, Section> = {
  jobs: {
    id: 'jobs', view: 'opps:jobs', label: ['Жұмыс', 'Работа'],
    tabs: [
      { id: 'parttime', label: ['Подработка', 'Подработка'] },
      { id: 'youth', label: ['Жастар практикасы', 'Молодёжная практика'] },
      { id: 'fulltime', label: ['Тұрақты жұмыс', 'Постоянная работа'] },
    ],
  },
  intern: {
    id: 'intern', view: 'opps:intern', label: ['Тағылымдама', 'Стажировки'],
    tabs: [
      { id: 'company', label: ['Компаниядағы тағылымдама', 'Стажировки в компаниях'] },
      { id: 'mobility', label: ['Академиялық ұтқырлық', 'Академическая мобильность'] },
      { id: 'wat', label: ['Work & Travel', 'Work & Travel'] },
      { id: 'exchange', label: ['Алмасу және саяхат', 'Обмены и поездки'] },
      { id: 'grants', label: ['Гранттар мен стипендиялар', 'Гранты и стипендии'] },
    ],
  },
  events: {
    id: 'events', view: 'opps:events', label: ['Іс-шаралар', 'Мероприятия'],
    tabs: [
      { id: 'all', label: ['Барлығы', 'Все'] },
      { id: 'contests', label: ['Байқаулар', 'Конкурсы'] },
      { id: 'hackathons', label: ['Хакатондар', 'Хакатоны'] },
      { id: 'clubs', label: ['Клубтар', 'Клубы'] },
      { id: 'conf', label: ['Конференциялар мен форумдар', 'Конференции и форумы'] },
    ],
  },
  courses: { id: 'courses', view: 'opps:courses', label: ['Курстар', 'Курсы'], tabs: [] },
  deals: {
    id: 'deals', view: 'bonus', label: ['Жеңілдіктер', 'Скидки'],
    tabs: [
      { id: 'deals', label: ['Жеңілдіктер', 'Скидки'] },
      { id: 'free', label: ['Тегін беремін', 'Отдам даром'] },
    ],
  },
};
export const SECTION_IDS = Object.keys(SECTIONS) as SectionId[];
export const isSectionId = (k: string | null | undefined): k is SectionId => !!k && k in SECTIONS;

/** the section a view id shows ('opps:intern' -> 'intern', 'bonus' -> 'deals'); a '?t=' suffix is ignored */
export function sectionOfView(name: string, prm: string): SectionId | undefined {
  if (name === 'bonus') return 'deals';
  if (name !== 'opps') return undefined;
  const p = prm.replace(/\?.*$/, '');
  return isSectionId(p) && p !== 'deals' ? p : 'jobs';
}

/** the view id of a section and sub-tab; the default (first) tab gets no query, so the plain old URL stays canonical */
export function sectionView(id: SectionId, tab?: string): string {
  const s = SECTIONS[id];
  return tab && s.tabs.length && tab !== s.tabs[0].id ? `${s.view}?t=${encodeURIComponent(tab)}` : s.view;
}

/** what the data and switches allow on this device */
export interface TabCtx { jobs: Pick<Job, 'subtype'>[]; clubs: boolean; market: boolean }

/** the sub-tabs shown: «Тұрақты жұмыс» only when such a job exists, «Клубтар» behind FEATURE_CLUBS,
    «Тегін беремін» behind FEATURE_MARKET */
export function visibleTabs(id: SectionId, c: TabCtx): SectionTab[] {
  return SECTIONS[id].tabs.filter(t => {
    if (id === 'jobs' && t.id === 'fulltime') return c.jobs.some(j => jobTab(j) === 'fulltime');
    if (id === 'events' && t.id === 'clubs') return c.clubs;
    if (id === 'deals' && t.id === 'free') return c.market;
    return true;
  });
}

/** the tab the URL asks for when it is shown, else the first one */
export function pickTab(tabs: SectionTab[], t: string | null | undefined): string {
  return tabs.find(x => x.id === t)?.id ?? tabs[0]?.id ?? '';
}

/* ---------- where an item goes ---------- */
export const JOB_SUBTYPES = ['parttime', 'youthpractice', 'fulltime'] as const;
export const OPP_SUBTYPES = ['exchange', 'workandtravel', 'scholarship', 'competition'] as const;

/** Job.subtype: 'youthpractice' -> «Жастар практикасы», 'fulltime' -> «Тұрақты жұмыс», anything else (or none) -> «Подработка» */
export function jobTab(j: Pick<Job, 'subtype'>): 'parttime' | 'youth' | 'fulltime' {
  return j.subtype === 'youthpractice' ? 'youth' : j.subtype === 'fulltime' ? 'fulltime' : 'parttime';
}

/** Opportunity type + subtype -> section and sub-tab.
    subtype 'competition' -> Іс-шаралар / Байқаулар (whatever the type); 'workandtravel' -> Work & Travel;
    type company -> Компаниядағы тағылымдама, mobility -> Академиялық ұтқырлық, intl -> Алмасу және саяхат,
    grant (no subtype, or 'scholarship') -> Гранттар мен стипендиялар. */
export function oppPlace(o: Pick<Opportunity, 'type' | 'subtype'>): { section: 'intern' | 'events'; tab: string } {
  if (o.subtype === 'competition') return { section: 'events', tab: 'contests' };
  if (o.subtype === 'workandtravel') return { section: 'intern', tab: 'wat' };
  switch (o.type) {
    case 'mobility': return { section: 'intern', tab: 'mobility' };
    case 'intl': return { section: 'intern', tab: 'exchange' };
    case 'grant': return { section: 'intern', tab: 'grants' };
    default: return { section: 'intern', tab: 'company' };
  }
}

/** event cat -> the sub-tabs of «Іс-шаралар» it shows under (besides «Барлығы») */
export const EVENT_TAB_CATS: Record<string, string[]> = {
  contests: ['contest'],
  hackathons: ['hackathon'],
  conf: ['conf', 'career'],
};
export const eventInTab = (cat: string, tab: string) => tab === 'all' || (EVENT_TAB_CATS[tab] ?? []).includes(cat);

/** old /search?k=<category> links -> the section view that now holds them (null: stay on «Іздеу») */
export function viewOfSearchCat(k: string | null | undefined): string | null {
  switch (k) {
    case 'jobs': return sectionView('jobs');
    case 'intern': return sectionView('intern');
    case 'mobility': return sectionView('intern', 'mobility');
    case 'grant': return sectionView('intern', 'grants');
    case 'events': return sectionView('events');
    case 'courses': return sectionView('courses');
    case 'deals': return sectionView('deals');
    case 'market': return sectionView('deals', 'free');
    default: return null;
  }
}
