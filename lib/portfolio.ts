/* Public portfolio (prompt 09) on the client and the server: API shapes (docs/api/portfolio.md, the Go code in
   pkg/portfolio wins), labels and the builder of the self-declared items the student may sync. Free of React. */
import { COURSES, SKILLS } from '@/lib/data/static';
import type { AppState, LText } from '@/lib/types';

export interface PortfolioSettings {
  enabled: boolean; slug: string; url: string; displayName: string | null; headline: string | null; bio: string | null;
  showUniversity: boolean; showCourse: boolean; showEvents: boolean; showCerts: boolean; showExperience: boolean;
  showSkills: boolean; showBadges: boolean; showContact: boolean; syncConsentAt: string | null; slugRotatedAt: string | null; updatedAt: string;
}
export type ToggleKey = 'showUniversity' | 'showCourse' | 'showEvents' | 'showCerts' | 'showExperience' | 'showSkills' | 'showBadges' | 'showContact';
export interface PortfolioItem { kind: 'cert' | 'experience' | 'skill'; title: string; org?: string; period?: string; desc?: string; url?: string }
export interface Bi { kk: string; ru: string }
export interface PfBadge { badge: string; awardedAt: string; verified: boolean }
export interface PfSelfItem { title: string; org: string; period: string; desc: string; url: string; verified: false }
/** GET /api/p/{slug} and the owner's `preview`: no ids, no GPA / phone / email */
export interface PortfolioPage {
  noindex: true;
  profile: { name: string; headline: string | null; bio: string | null; university: string | null; course: number | null };
  verified: {
    events: { title: Bi; date: string; organizer: string; verifyUrl: string; verified: true }[];
    certificates: { kind: 'event' | 'ambassador'; title: Bi; issuer: string; issuedAt: string; verifyUrl: string; verified: true }[];
    badges: PfBadge[];
  };
  selfReported: { certificates: PfSelfItem[]; experience: PfSelfItem[]; skills: PfSelfItem[]; badges: PfBadge[] };
  contact: { telegramUrl: string } | null;
  og: { title: string; description: string | null; image: string; url: string };
  site: string;
}
export interface PortfolioMe { settings: PortfolioSettings; items: PortfolioItem[]; preview: PortfolioPage }

export const PF_LIMITS = { displayName: 80, headline: 80, bio: 300, items: 60 };

export const BADGES: Record<string, LText> = {
  mentor: { kk: 'Ментор', ru: 'Ментор' },
  generous: { kk: 'Жомарт', ru: 'Щедрый' },
};

export const TOGGLES: [ToggleKey, LText][] = [
  ['showUniversity', { kk: 'Университет', ru: 'Университет' }],
  ['showCourse', { kk: 'Курс', ru: 'Курс' }],
  ['showEvents', { kk: 'Расталған іс-шаралар', ru: 'Подтверждённые мероприятия' }],
  ['showCerts', { kk: 'Сертификаттар', ru: 'Сертификаты' }],
  ['showExperience', { kk: 'Тәжірибе', ru: 'Опыт' }],
  ['showSkills', { kk: 'Дағдылар', ru: 'Навыки' }],
  ['showBadges', { kk: 'Белгілер (Ментор, Жомарт)', ru: 'Значки (Ментор, Щедрый)' }],
  ['showContact', { kk: '«Telegram арқылы жазу» батырмасы', ru: 'Кнопка «Написать в Telegram»' }],
];

const cut = (s: string, n: number) => (s.length > n ? s.slice(0, n).trimEnd() : s);

/** the device's self-declared items, in the PUT /api/portfolio/items shape (demo event certificates are not real, so they never go) */
export function localItems(S: AppState, lang: 'kk' | 'ru'): PortfolioItem[] {
  const out: PortfolioItem[] = [];
  const L = (t: LText) => t[lang] || t.kk;
  for (const c of S.certs) {
    if (!c.title.trim()) continue;
    const year = new Date(c.date).getFullYear();
    out.push({ kind: 'cert', title: cut(c.title.trim(), 120), org: cut(c.org.trim(), 120), period: Number.isFinite(year) ? String(year) : '' });
  }
  for (const c of COURSES) {
    if (!S.done[c.id]) continue;
    out.push({ kind: 'cert', title: cut(L(c.title), 120), org: cut(c.platform, 120), period: String(new Date(S.done[c.id]).getFullYear()), url: /^https:\/\//.test(c.url) ? c.url.slice(0, 500) : '' });
  }
  for (const line of (S.profile.exp || '').split('\n').map(s => s.trim()).filter(Boolean)) out.push({ kind: 'experience', title: cut(line, 120) });
  for (const k of S.profile.skills) if (SKILLS[k]) out.push({ kind: 'skill', title: cut(L(SKILLS[k]), 120) });
  return out.slice(0, PF_LIMITS.items);
}

/** résumé text with «Портфолио: <url>» added to the header block (name, headline, contacts) */
export function withPortfolioLink(text: string, url: string, lang: 'kk' | 'ru' | 'en'): string {
  const line = `${lang === 'en' ? 'Portfolio' : 'Портфолио'}: ${url}`;
  const i = text.indexOf('\n\n');
  return i < 0 ? `${text}\n${line}` : `${text.slice(0, i)}\n${line}${text.slice(i)}`;
}
