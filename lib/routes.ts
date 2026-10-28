/* View ids (the legacy router's vocabulary: 'job:j1', 'opps:intern', 'map:near', ...) <-> real URL paths.
   Shared by the server (catch-all route, metadata) and the client (navigation, header, legacy #/ links). */

export const parseView = (v: string): [string, string] => {
  const i = v.indexOf(':');
  return i < 0 ? [v, ''] : [v.slice(0, i), v.slice(i + 1)];
};

/** screens that take no URL parameter */
export const PLAIN = [
  'quiz', 'search', 'notif', 'profile', 'services', 'materials', 'saved', 'mine', 'ai', 'chat', 'resume', 'interview',
  'exam', 'cards', 'study', 'match', 'edit', 'gpa', 'docs', 'admission', 'pay', 'portfolio', 'settings', 'map', 'bonus',
  'roadmap', 'privacy', 'admin', 'market', 'mentors', 'ambassador', 'checkin', 'organize',
  'employer', 'university', 'announcements',
] as const;
/** screens that need an id */
export const WITH_ID = ['quiz', 'job', 'opp', 'event', 'plan', 'deck', 'doc', 'match', 'market', 'mentors', 'ambassador', 'admin', 'certificate', 'organize', 'employer'] as const;
/** fixed sub-screens served by the catch-all route (market/<id>, organize/<id>, certificate/<code> have their own dynamic routes) */
export const SUB_SCREENS = [
  'admin:queue', 'admin:reports', 'admin:mentors', 'admin:ambassadors', 'admin:market',
  'ambassador:apply', 'ambassador:mentors', 'ambassador:rating', 'mentors:become', 'mentors:me',
  'admin:employers', 'admin:staff',
] as const;
/** screens whose parameter is a sub-path ('employer:jobs/12/edit' <-> /employer/jobs/12/edit); served by app/employer/[...rest] */
export const MULTI_SEGMENT = ['employer', 'quiz'] as const;
export const ROOTS = ['home', 'search', 'notif', 'profile'];

const FIXED: Record<string, string> = {
  home: '/',
  opps: '/jobs',
  'opps:jobs': '/jobs',
  'opps:intern': '/internships',
  'opps:events': '/events',
  'opps:courses': '/courses',
  bonus: '/discounts',
  notif: '/notifications',
  map: '/map',
  'map:campus': '/map',
  'map:near': '/map/near',
};
const SEGMENT: Record<string, string> = { opp: 'opportunity' };
/** search categories that kept their own old URL; the others are /search?k=<category> */
const SEARCH_PATH: Record<string, string> = { jobs: '/jobs', intern: '/internships', events: '/events', courses: '/courses', deals: '/discounts' };
const FIXED_BACK: Record<string, string> = Object.fromEntries(
  Object.entries(FIXED).filter(([v]) => v !== 'opps' && v !== 'map:campus').map(([v, p]) => [p, v]),
);
const SEGMENT_BACK: Record<string, string> = Object.fromEntries(Object.entries(SEGMENT).map(([v, s]) => [s, v]));

export function pathOf(v: string): string {
  if (v in FIXED) return FIXED[v];
  const [n, prm] = parseView(v);
  if (n === 'search') return SEARCH_PATH[prm] || (prm && prm !== 'all' ? '/search?k=' + encodeURIComponent(prm) : '/search');
  if (n === 'opps') return FIXED['opps:jobs'];
  if ((MULTI_SEGMENT as readonly string[]).includes(n) && prm) return '/' + n + '/' + prm.split('/').map(encodeURIComponent).join('/');
  return '/' + (SEGMENT[n] || n) + (prm ? '/' + encodeURIComponent(prm) : '');
}

/** URL path -> view id, or null when the path is not a screen */
export function viewOfPath(pathname: string): string | null {
  const clean = pathname.replace(/\/+$/, '') || '/';
  if (clean in FIXED_BACK) return FIXED_BACK[clean];
  const parts = clean.split('/').filter(Boolean).map(s => {
    try { return decodeURIComponent(s); } catch { return s; }
  });
  if (parts.length === 0) return 'home';
  const name = SEGMENT_BACK[parts[0]] || parts[0];
  if (parts.length === 1) {
    if (name === 'notif' || name === 'bonus') return null; // only reachable under their own paths
    return (PLAIN as readonly string[]).includes(name) ? name : null;
  }
  if (parts.length === 2 && (WITH_ID as readonly string[]).includes(name) && parts[0] !== 'opp') return `${name}:${parts[1]}`;
  if (parts.length > 2 && (MULTI_SEGMENT as readonly string[]).includes(name)) return `${name}:${parts.slice(1).join('/')}`;
  return null;
}

/** Old single-file links: '#/job/j1', '#/opportunity/o2', '#/jobs', '#/opps/courses', '#/map/near', '#/search/all' */
export function viewOfLegacyHash(hash: string): string | null {
  const path = (hash || '').replace(/^#\/?/, '').replace(/\?.*$/, '');
  if (!path) return null;
  const legacyFixed: Record<string, string> = { jobs: 'opps:jobs', internships: 'opps:intern', events: 'opps:events', discounts: 'bonus' };
  if (path in legacyFixed) return legacyFixed[path];
  const i = path.indexOf('/');
  const n = i < 0 ? path : path.slice(0, i);
  let prm = i < 0 ? '' : path.slice(i + 1);
  try { prm = decodeURIComponent(prm); } catch { /* keep raw */ }
  const name = n === 'opportunity' ? 'opp' : n === 'notifications' ? 'notif' : n;
  const known = name === 'home' || name === 'opps' || (PLAIN as readonly string[]).includes(name) || (WITH_ID as readonly string[]).includes(name);
  if (!known) return 'home';
  return prm ? `${name}:${prm}` : name;
}

/** every parameterless screen, for generateStaticParams of the catch-all route */
export function staticSlugs(): string[][] {
  const views = ['home', 'opps:jobs', 'opps:intern', 'opps:events', 'opps:courses', 'map:near', ...PLAIN, ...SUB_SCREENS];
  const seen = new Set<string>();
  const out: string[][] = [];
  for (const v of views) {
    const p = pathOf(v);
    if (seen.has(p)) continue;
    seen.add(p);
    out.push(p.split('/').filter(Boolean));
  }
  return out;
}
