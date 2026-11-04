/* University clubs (FEATURE_CLUBS) on the client: API shapes (docs/clubs.md, the Go code wins), the direction list
   with its pastel family, and the pure catalogue filters. Kept free of React so unit tests can import it. */
import type { LText } from '@/lib/types';

export type Direction = 'it' | 'science' | 'debate' | 'sport' | 'art' | 'volunteer' | 'media' | 'business' | 'languages' | 'kvn' | 'culture' | 'other';

export interface ClubMy { status: '' | 'pending' | 'member'; admin: boolean }
export interface Club {
  id: number; name_kk: string; name_ru: string | null; direction: Direction;
  summary_kk: string | null; summary_ru: string | null; about_kk: string | null; about_ru: string | null;
  university: string; building_id: string | null; room: string | null; schedule_kk: string | null; schedule_ru: string | null;
  leader_title_kk: string | null; leader_title_ru: string | null; leader_contact: string | null; leader_contact_public: boolean;
  telegram_url: string | null; instagram_url: string | null; join_mode: 'request' | 'link'; join_link: string | null;
  recruiting: boolean; status: 'active' | 'archived'; source_url: string | null; verified_at: string | null;
  members: number; my?: ClubMy;
}
export interface ClubPost { id: number; body_kk: string; body_ru: string | null; link_url: string | null; status: string; source: string; created_at: string; published_at: string | null }
export interface ClubDetail { club: Club; posts: ClubPost[]; can_manage: boolean; pending_requests?: number }
export interface ClubMember {
  user_id: number; name: string | null; username: string | null; faculty: string | null; course: number | null;
  status: 'pending' | 'member'; message: string | null; created_at: string; decided_at: string | null;
}
export interface AdminClub extends Club { admins: { user_id: number; name: string | null; username: string | null }[]; pending_requests: number }

/** directions in display order, each with a pastel family from globals.css (--p-<tone>) */
export const DIRECTIONS: { id: Direction; label: LText; tone: 'sky' | 'violet' | 'rose' | 'mint' | 'peach' | 'line' }[] = [
  { id: 'it', label: { kk: 'IT', ru: 'IT' }, tone: 'sky' },
  { id: 'science', label: { kk: 'Ғылым', ru: 'Наука' }, tone: 'violet' },
  { id: 'debate', label: { kk: 'Дебат', ru: 'Дебаты' }, tone: 'peach' },
  { id: 'sport', label: { kk: 'Спорт', ru: 'Спорт' }, tone: 'mint' },
  { id: 'art', label: { kk: 'Өнер мен музыка', ru: 'Искусство и музыка' }, tone: 'rose' },
  { id: 'volunteer', label: { kk: 'Еріктілер', ru: 'Волонтёрство' }, tone: 'mint' },
  { id: 'media', label: { kk: 'Медиа', ru: 'Медиа' }, tone: 'sky' },
  { id: 'business', label: { kk: 'Бизнес, стартап', ru: 'Бизнес, стартапы' }, tone: 'peach' },
  { id: 'languages', label: { kk: 'Тілдер', ru: 'Языки' }, tone: 'violet' },
  { id: 'kvn', label: { kk: 'КВН, юмор', ru: 'КВН, юмор' }, tone: 'peach' },
  { id: 'culture', label: { kk: 'Мәдениет', ru: 'Культура' }, tone: 'rose' },
  { id: 'other', label: { kk: 'Басқа', ru: 'Другое' }, tone: 'line' },
];
export const directionOf = (d: string) => DIRECTIONS.find(x => x.id === d) ?? DIRECTIONS[DIRECTIONS.length - 1];

type Lang = 'kk' | 'ru';
const pickL = (kk: string | null, ru: string | null, lang: Lang) => (lang === 'ru' && ru ? ru : kk || ru || '');
export const clubName = (c: Pick<Club, 'name_kk' | 'name_ru'>, lang: Lang) => pickL(c.name_kk, c.name_ru, lang);
export const clubSummary = (c: Pick<Club, 'summary_kk' | 'summary_ru'>, lang: Lang) => pickL(c.summary_kk, c.summary_ru, lang);
export const clubAbout = (c: Pick<Club, 'about_kk' | 'about_ru'>, lang: Lang) => pickL(c.about_kk, c.about_ru, lang);
export const clubSchedule = (c: Pick<Club, 'schedule_kk' | 'schedule_ru'>, lang: Lang) => pickL(c.schedule_kk, c.schedule_ru, lang);
export const clubLeader = (c: Pick<Club, 'leader_title_kk' | 'leader_title_ru'>, lang: Lang) => pickL(c.leader_title_kk, c.leader_title_ru, lang);
export const postBody = (p: Pick<ClubPost, 'body_kk' | 'body_ru'>, lang: Lang) => pickL(p.body_kk, p.body_ru, lang);

export interface ClubFilter { direction: string; building: string; q: string; recruiting?: boolean }
export const NO_FILTER: ClubFilter = { direction: 'all', building: 'all', q: '' };

const norm = (s: string) => s.toLowerCase().replace(/ё/g, 'е').replace(/[«»"'.,]/g, ' ').replace(/\s+/g, ' ').trim();

/** the catalogue after the direction / building chips and the search box; recruiting clubs first, then by name */
export function filterClubs(list: Club[], f: ClubFilter, lang: Lang): Club[] {
  const words = norm(f.q).split(' ').filter(Boolean);
  return list
    .filter(c => f.direction === 'all' || c.direction === f.direction)
    .filter(c => f.building === 'all' || (f.building === 'none' ? !c.building_id : c.building_id === f.building))
    .filter(c => !f.recruiting || c.recruiting)
    .filter(c => {
      if (!words.length) return true;
      const hay = norm([c.name_kk, c.name_ru, c.summary_kk, c.summary_ru, c.room, directionOf(c.direction).label[lang]].filter(Boolean).join(' '));
      return words.every(w => hay.includes(w));
    })
    .sort((a, b) => Number(b.recruiting) - Number(a.recruiting) || clubName(a, lang).localeCompare(clubName(b, lang), lang));
}

/** the direction chips worth showing: only directions that have at least one club, in display order */
export const usedDirections = (list: Club[]) => DIRECTIONS.filter(d => list.some(c => c.direction === d.id));
/** building ids that host at least one club, in first-seen order */
export const usedBuildings = (list: Club[]) => [...new Set(list.map(c => c.building_id).filter((b): b is string => !!b))];

/** the home carousel takes one recruiting club at most */
export function recruitingPick(list: Club[]): Club | null {
  return list.find(c => c.recruiting && c.status === 'active') ?? null;
}

/** a Telegram or Instagram link the club page may open (the API validates the same shapes) */
export const safeSocial = (u: string | null) => (u && /^https:\/\/(t\.me|(www\.)?instagram\.com)\//.test(u) ? u : null);
