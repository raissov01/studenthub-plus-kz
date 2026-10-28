import type { RoadmapState } from '@/features/roadmap/logic';
import type { IconName } from '@/lib/icons';

export type { IconName };
export type Lang = 'kk' | 'ru';
/** Bilingual text. `en` exists on a few dictionaries (specs, skills) used by the English résumé. */
export type LText = { kk: string; ru: string; en?: string };
/** [kk, ru] label pair, as in the legacy dictionaries. */
export type Pair = [string, string];

/* ---------- content served by GET /api/content (same shape as data/*.json) ---------- */
export interface Contact { phone?: string; email?: string; site?: string }
export interface Provenance {
  source_url: string;
  posted_at: string;
  verified_at: string;
  verified_by: string;
  contact: Contact;
  is_demo: boolean;
  /** YYYY-MM-DD or null when there is no deadline */
  deadline: string | null;
  city: string;
}
export interface Job extends Provenance {
  id: string;
  today?: boolean;
  title: LText;
  org: LText;
  pay: number;
  per: 'shift' | 'month' | 'hour';
  hours: number;
  sched: LText;
  tags: string[];
  skills: string[];
  spec: string[];
  desc: LText;
  req: LText[];
}
export type OppType = 'mobility' | 'intl' | 'company' | 'grant';
export interface Opportunity extends Provenance {
  id: string;
  type: OppType;
  funded: boolean;
  title: LText;
  org: LText;
  req: { course: number; gpa?: number; en?: string; skills?: string[] };
  spec: string[];
  desc: LText;
  docs: LText[];
  steps: LText[];
}
export interface Discount extends Provenance {
  id: string;
  cat: string;
  pct: string;
  name: LText;
  what: LText;
  cond: LText;
}
/** a useful place an ambassador or student suggested and an admin approved (GET /api/content → places[]) */
export interface CommunityPlace {
  id: string; cat: string; name: LText; desc: LText; address: string; building_id: string; hours: string;
  source_url: string; posted_at: string; university: string; source: 'ambassador';
}
/** Public event feed: intentionally excludes attendee and management data. */
export interface ContentEvent {
  id: number; title_kk: string; title_ru: string | null; desc_kk: string | null; desc_ru: string | null;
  cat: string; starts_at: string; ends_at: string; university: string; place_text: string | null;
  building_id: string | null; is_demo?: boolean;
}
export interface Content {
  events?: ContentEvent[];
  places?: CommunityPlace[];
  opportunities: Opportunity[];
  jobs: Job[];
  discounts: Discount[];
  source: string;
  updated_at: string;
}

/* ---------- static demo data (lib/data) ---------- */
export interface EventItem {
  id: string;
  day: number;
  time: string;
  cat: string;
  cert: boolean;
  past?: boolean;
  title: LText;
  place: LText;
  desc: LText;
}
export interface Course {
  id: string;
  title: LText;
  platform: string;
  url: string;
  weeks: number;
  cert: 'free' | 'paid' | 'badge';
  spec: string[];
  skills: string[];
}
export interface DocItem { id: string; title: LText; why: LText; where: LText; need: LText[]; online: LText }
export interface ClassItem { t: string; end: string; name: LText; room: string; kind: LText }
export interface Payment {
  year: number;
  paid: number;
  nextAmount: number;
  nextDue: number;
  history: { day: number; amount: number; via: LText }[];
  change: { from: number; to: number; date: number; basis: LText };
}
export interface Building {
  id: string;
  a?: string;
  n: number;
  corp?: number;
  kind: 'study' | 'dorm' | 'other';
  name: LText;
  has: LText;
}
export interface NearPlace {
  c: string;
  n: string;
  a?: string;
  d: number;
  dir: string;
  h: string;
  lat?: number;
  lon?: number;
  src: 'osm' | '2gis';
  note?: string;
}

/* ---------- device-local state (localStorage "studenthub.v1") ---------- */
export interface Profile {
  name: string;
  spec: string;
  course: number;
  gpa: number;
  credits: number;
  en: string;
  city: string;
  skills: string[];
  exp: string;
  about: string;
  phone: string;
  email: string;
  demo: boolean;
}
export interface PlanItem { d: number; k: 'learn' | 'test' | 'review' | 'mock'; t: string; done: boolean }
export interface Plan { id: string; subject: string; exam: string; lectures: number; hard: string; created: string; items: PlanItem[] }
export interface Card { q: string; a: string }
export interface Deck { id: string; name: string; cards: Card[]; demo?: boolean }
export interface ChatMsg { role: 'user' | 'assistant'; content: string }
export interface AppState {
  v: 1;
  lang: Lang;
  onboarded: boolean;
  theme: 'light' | 'dark' | string;
  noteSeen: boolean;
  profile: Profile;
  applied: Record<string, number>;
  saved: Record<string, number>;
  oppDocs: Record<string, Record<string, boolean>>;
  fav: Record<string, number>;
  read: Record<string, number>;
  events: Record<string, { code: string }>;
  remind: Record<string, boolean>;
  done: Record<string, number>;
  certs: { title: string; org: string; date: string }[];
  plans: Plan[];
  decks: Deck[];
  hideDemoDeck: boolean;
  chat: ChatMsg[];
  gpaPlan: { n: string; cr: number; g: string }[];
  photo?: string;
  /** «Даму жоспары» (features/roadmap); added later, so load() fills it in for older stored data */
  roadmap: RoadmapState;
}
