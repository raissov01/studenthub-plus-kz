import type { Page } from '@playwright/test';
import { test, expect, onboard } from './fixtures';
import { config } from './content';
import { attendance, dashboardExtra, payments, settings, userNotes, userOrders, weekEvents } from './admin-data';

/* Design screenshots of every /admin screen at 390×844 and 1440×900 into docs/design/admin/ (mocked API, illustrative
   numbers for the picture only). Runs only with ADMIN_SHOTS=1:
     ADMIN_SHOTS=1 npx playwright test e2e/admin-shots.spec.ts --project=kk-390 --workers=1 */
test.skip(process.env.ADMIN_SHOTS !== '1', 'design screenshots on demand');

const none = { admin: false, moderator: false, blocked: false, moderation: null, cafes: [], printShops: [], clubs: [], university: null, feedback: [], employer: null, ambassador: false, mentor: false };
const days = (n: number, f: (i: number) => number) => Array.from({ length: n }, (_, i) => ({ day: new Date(Date.UTC(2026, 10, 6 - n + i)).toISOString().slice(0, 10), n: f(i) }));
const user = (id: number, name: string, extra = {}) => ({ id, telegramId: 9000 + id, email: `${name.split(' ')[0].toLowerCase().replace(/[^a-zа-яәіңғүұқөһ]/gi, '')}${id}@example.kz`,
  username: `user${id}`, name, role: 'student', university: 'KATU',
  consentAt: '2026-10-01T00:00:00Z', createdAt: '2026-10-01T00:00:00Z', lastActive: '2026-11-05', blocked: false, scopes: [], places: [], ...extra });

async function mock(page: Page, role: string, scopes: string[], panels: object) {
  await page.route('**/api/clubs?**', r => r.fulfill({ json: { clubs: [] } }));
  await page.route('**/api/clubs', r => r.fulfill({ json: { clubs: [] } }));
  await page.route('**/api/cafe/orders/mine', r => r.fulfill({ json: { orders: [] } }));
  await page.route('**/api/print/jobs/mine', r => r.fulfill({ json: { jobs: [] } }));
  await page.route('**/api/points/**', r => r.fulfill({ status: 404, json: { error: { code: 'not_found' } } }));
  await page.route('**/api/config', r => r.fulfill({ json: { ...config, features: { ...config.features, cafe: true, print: true, clubs: true, market: true, feeds: true, surveys: true, feedback: true, b2b: true, points: true, events: true } } }));
  await page.route('**/api/events/**', r => {
    const p = new URL(r.request().url()).pathname;
    if (p === '/api/events/manage') return r.fulfill({ json: { events: weekEvents() } });
    if (p === '/api/events/attendance') return r.fulfill({ json: attendance() });
    return r.fulfill({ status: 404, json: { error: { code: 'not_found' } } });
  });
  await page.route('**/api/support', r => r.fulfill({ json: { support: settings().support } }));
  await page.route('**/api/me', r => r.fulfill({ json: { user: { id: 501, name: 'Аружан Тест', role, scopes, lang: 'kk', consentAt: '2026-01-01', isAdult: true, university: 'KATU' }, needsConsent: false, inboxUnread: 0, console: true } }));
  await page.route('**/api/console/**', r => {
    const p = new URL(r.request().url()).pathname;
    if (p === '/api/console/me') return r.fulfill({ json: { ...none, ...panels } });
    if (p.endsWith('/stats')) return r.fulfill({ json: {
      role: 'manager', shop: { id: 3, kind: 'cafe', nameKk: 'Бас ғимарат кофесі', nameRu: 'Кофе в главном корпусе', paymentMode: 'both', active: true },
      today: { byStatus: { new: 2, accepted: 1, ready: 1, picked_up: 14, cancelled: 1 }, created: 19, done: 14, refundsToReturn: 0, revenue: 1260000 },
      days: Array.from({ length: 14 }, (_, i) => ({ day: new Date(Date.UTC(2026, 9, 23 + i)).toISOString().slice(0, 10), orders: 10 + (i % 5) * 3, done: 9 + (i % 5) * 3, revenue: (9 + (i % 5) * 3) * 90000 })),
      kaspi: { connected: true, broken: false, phoneMasked: '+7 7** *** 12 34', orgName: 'ИП Кофе' },
      team: [{ userId: 501, name: 'Аружан Тест', username: 'aru', role: 'manager', addedAt: '2026-10-01T00:00:00Z' }, { userId: 77, name: 'Дана Е.', username: 'dana', role: 'barista', addedAt: '2026-10-10T00:00:00Z' }],
    } });
    return r.fulfill({ status: 404, json: { error: { code: 'not_found' } } });
  });
  await page.route('**/api/admin/**', r => {
    const url = new URL(r.request().url()), p = url.pathname;
    const j = (json: unknown) => r.fulfill({ json });
    switch (p) {
      case '/api/admin/dashboard': return j({
        users: { total: 1284, accounts: 1102, new7: 64, new30: 231, blocked: 2, moderators: 3, byRole: { student: 1260, admin: 2, ambassador: 14, university_staff: 5, employer: 3 } },
        signups: days(30, i => (i * 7) % 13), active: { dau: 212, wau: 640, mau: 905, since: '2026-11-06', days: days(14, i => 150 + ((i * 37) % 90)) }, devices: { dau: 340, wau: 1020 },
        content: { adminContent: 48, community: 22, jobs: 17, events: 6, clubs: 31, market: 12, lostfound: 9, qa: 40, roommates: 5, surveys: 2 },
        pending: { queue: 7, queueModerator: 5, reports: 2, feedbackHeld: 1, feedbackOverdue: 3, redemptions: 4, employers: 1, cafeRefunds: 0, printRefunds: 1 },
        shops: { cafe: { orders: 61, done: 55, revenue: 4875000 }, print: { orders: 23, done: 19, revenue: 912000 } },
        features: { cafe: true, print: true, clubs: true, feedback: true, points: true, b2b: true, market: true, lostfound: true, courseqa: true, events: true, surveys: true, roommates: true },
        ...dashboardExtra(),
      });
      case '/api/admin/invites': return j({ invites: [{ id: 1, email: 'aigerim.new@example.kz', grant: 'moderator', reason: 'Командаға шақырылды', createdAt: '2026-11-07T10:00:00Z', by: { id: 501, name: 'Аружан Тест' } }] });
      case '/api/admin/views': return j({ views: url.searchParams.get('screen') === 'users' ? [{ id: 1, name: 'Модераторлар', query: 'scope=moderator' }] : [{ id: 2, name: 'Қайтару керек', query: 'payment=refund_manual' }] });
      case '/api/admin/payments': return j(payments());
      case '/api/admin/config': return j(settings());
      case '/api/admin/users/77/orders': return j(userOrders());
      case '/api/admin/users/77/notes': return j(userNotes());
      case '/api/admin/cafes': return j({ cafes: [{ id: 3, nameKk: 'Бас ғимарат кофесі', nameRu: 'Кофе' }] });
      case '/api/admin/analytics': return j({ devices: 420, returned_day1: 120, eligible_day1: 400, returned_day7: 80, eligible_day7: 350, saved_first: 150, from_share: 30, ambassadors: { amb_aru: 12 }, feedback_avg: 4.4, feedback_count: 18, feedback_recent: ['Ыңғайлы'] });
      case '/api/admin/queue': return j({ items: [], pendingByKind: { opportunity: 3, feed_item: 2 } });
      case '/api/admin/reports': return j({ reports: [] });
      case '/api/admin/users': if ((url.searchParams.get('q') || '').includes('@')) return j({ total: 0, page: 1, limit: 25, universities: [], users: [] });
        return j({ total: 1284, page: 1, limit: 25, universities: [{ name: 'KATU', count: 1100 }],
        users: [user(77, 'Дана Ерланқызы', { places: ['cafe_barista'] }), user(78, 'Нұрлан Сейіт', { scopes: ['moderator'] }), user(79, 'Айгерім Б.', { blocked: true, lastActive: '2026-10-28' }),
          user(80, 'Тимур К.', { role: 'ambassador' }), user(81, 'Мадина О.', { consentAt: null, lastActive: null })] });
      case '/api/admin/users/77': return j({
        user: { id: 77, telegramId: 9077, email: 'dana@example.kz', emailVerified: true, username: 'dana', name: 'Дана Ерланқызы', lang: 'kk', role: 'student', university: 'KATU', faculty: 'IT', course: 2, city: 'Астана', isAdult: true,
          consentAt: '2026-10-01T00:00:00Z', createdAt: '2026-10-01T00:00:00Z', lastSignIn: '2026-11-05T00:00:00Z', referredBy: null, botInactive: false },
        scopes: [], block: null, memberships: { cafes: [{ id: 3, nameKk: 'Бас ғимарат кофесі', nameRu: 'Кофе', role: 'barista' }], printShops: [], clubs: [{ id: 2, nameKk: 'Дебат клубы', nameRu: null }], university: null, feedback: [], employer: null, ambassador: null, mentor: null },
        activity: { lastActive: '2026-11-05', activeDays30: 18, cafeOrders: 6, printJobs: 2, submissions: 1, reportsFiled: 0, marketItems: 1, lostfoundPosts: 0, qaQuestions: 3, qaAnswers: 5, clubMemberships: 1, eventRegs: 2 },
        reportsAgainst: [{ kind: 'qa_answer', id: '41', reports: 1, dismissed: 1, firstAt: '2026-11-01T00:00:00Z', lastAt: '2026-11-01T00:00:00Z' }],
        audit: [{ id: 9, action: 'staff_grant', reason: 'жаңа ауысым', meta: { shop: 'cafe', shopId: 3, role: 'barista' }, createdAt: '2026-10-10T09:00:00Z', actor: { id: 501, name: 'Аружан Тест' }, target: { id: 77, name: 'Дана Ерланқызы' } }],
      });
      case '/api/admin/audit': return j({ next: null, actions: [{ action: 'role_grant', count: 4 }, { action: 'scope_grant', count: 3 }, { action: 'export', count: 2 }],
        entries: [
          { id: 12, action: 'scope_grant', reason: 'кезекті жүргізеді', meta: { scope: 'moderator' }, createdAt: '2026-11-05T09:12:00Z', actor: { id: 501, name: 'Аружан Тест' }, target: { id: 78, name: 'Нұрлан Сейіт' } },
          { id: 11, action: 'user_block', reason: 'Q&A-да спам', meta: {}, createdAt: '2026-11-04T15:40:00Z', actor: { id: 501, name: 'Аружан Тест' }, target: { id: 79, name: 'Айгерім Б.' } },
          { id: 10, action: 'export', reason: null, meta: { kind: 'users', rows: 1284 }, createdAt: '2026-11-04T10:02:00Z', actor: { id: 501, name: 'Аружан Тест' }, target: null },
        ] });
      case '/api/admin/grants': return j({ scopes: ['moderator'], admins: [{ userId: 501, name: 'Аружан Тест', username: 'aru' }],
        grants: [{ userId: 78, name: 'Нұрлан Сейіт', username: 'nurlan', university: 'KATU', role: 'student', scope: 'moderator', reason: 'кезекті жүргізеді', createdAt: '2026-11-05T09:12:00Z', grantedBy: { id: 501, name: 'Аружан Тест' } }],
        cafes: [{ shopId: 3, nameKk: 'Бас ғимарат кофесі', nameRu: 'Кофе', userId: 77, name: 'Дана Ерланқызы', username: 'dana', role: 'barista' }], printShops: [], clubs: [], universityStaff: [], feedbackStaff: [] });
      case '/api/admin/voice': return j({ universities: [{ university: 'KATU', surveys: { draft: 1, published: 2, closed: 4, respondents: 312 }, feedback: { held: 1, open: 6, overdue: 2, answered: 40, recipients: 5, unstaffed: 1 }, wellbeing: 3, staff: 4 }],
        surveys: [{ id: 12, university: 'KATU', title: 'Асхана сапасы', status: 'published', anonymous: true, respondents: 140, createdAt: '2026-11-01T00:00:00Z', closesAt: null }] });
      case '/api/admin/feeds': return j({ counts: { pending: 3, published: 20 }, sources: [], usage: { calls: 12, failures: 0, tokens: 30000, limit: 200000, ai: true, autopublish_min_confidence: 0, channel: false } });
      case '/api/admin/feeds/items': return j({ items: url.searchParams.get('status') !== 'pending' ? [] : [0.95, 0.82, 0.61].map((c, i) => ({
        id: i + 1, status: 'pending', category: 'grant', source: 'Grants KZ', source_id: 'grants', confidence: c, auto: false, submission_id: 100 + i, deadline: '2026-12-1' + i,
        created_at: '2026-11-05T10:00:00Z', published_at: null, extra_sources: [],
        data: { title_kk: ['Erasmus+ гранты', 'Хакатон Astana Code', 'Жазғы мектеп'][i], title_ru: '', summary_kk: 'Студенттерге арналған мүмкіндік, түпнұсқасын тексеріңіз.', source_url: 'https://t.me/a/1', apply_url: 'https://example.org' } })) });
    }
    return r.fulfill({ status: 404, json: { error: { code: 'not_found' } } });
  });
}

const VIEWPORTS = [{ w: 390, h: 844 }, { w: 1440, h: 900 }];
const SCREENS: { name: string; role: string; scopes: string[]; panels: object; path: string; click?: string }[] = [
  { name: 'give-role', role: 'admin', scopes: [], panels: {}, path: '/admin/users', click: 'Рөл беру' },
  { name: 'invite', role: 'admin', scopes: [], panels: {}, path: '/admin/users', click: 'search:dina.new@example.kz' },
  { name: 'user-orders', role: 'admin', scopes: [], panels: {}, path: '/admin/users?id=77', click: 'tab:Тапсырыстар' },
  { name: 'user-notes', role: 'admin', scopes: [], panels: {}, path: '/admin/users?id=77', click: 'tab:Ішкі жазбалар' },
  { name: 'schedule', role: 'admin', scopes: [], panels: {}, path: '/admin/schedule' },
  { name: 'attendance', role: 'admin', scopes: [], panels: {}, path: '/admin/attendance' },
  { name: 'payments', role: 'admin', scopes: [], panels: {}, path: '/admin/payments' },
  { name: 'settings', role: 'admin', scopes: [], panels: {}, path: '/admin/settings' },
  { name: 'home-admin', role: 'admin', scopes: [], panels: { admin: true, moderator: true, moderation: { pending: 7, reports: 2 } }, path: '/admin' },
  { name: 'users', role: 'admin', scopes: [], panels: {}, path: '/admin/users' },
  { name: 'user-detail', role: 'admin', scopes: [], panels: {}, path: '/admin/users?id=77' },
  { name: 'audit', role: 'admin', scopes: [], panels: {}, path: '/admin/audit' },
  { name: 'roles', role: 'admin', scopes: [], panels: {}, path: '/admin/roles' },
  { name: 'exports', role: 'admin', scopes: [], panels: {}, path: '/admin/exports' },
  { name: 'voice', role: 'admin', scopes: [], panels: {}, path: '/admin/voice' },
  { name: 'home-moderator', role: 'student', scopes: ['moderator'], panels: { moderator: true, moderation: { pending: 5, reports: 2 } }, path: '/admin' },
  { name: 'moderation', role: 'student', scopes: ['moderator'], panels: {}, path: '/admin/moderation' },
  { name: 'feeds-bulk', role: 'student', scopes: ['moderator'], panels: {}, path: '/admin/feeds' },
  { name: 'home-staff', role: 'student', scopes: [], panels: {
    cafes: [{ id: 3, nameKk: 'Бас ғимарат кофесі', nameRu: 'Кофе', role: 'manager', active: true, open: 4 }],
    clubs: [{ id: 2, nameKk: 'Дебат клубы', nameRu: null, status: 'active', members: 34, requests: 3, pendingPosts: 1 }] }, path: '/admin' },
  { name: 'shop', role: 'student', scopes: [], panels: {}, path: '/admin/shop?cafe=3' },
];

for (const s of SCREENS) {
  test(`admin screenshot: ${s.name}`, async ({ page, lang }) => {
    test.skip(lang !== 'kk', 'one language is enough for the design shots');
    await mock(page, s.role, s.scopes, s.panels);
    await onboard(page, lang);
    for (const v of VIEWPORTS) {
      await page.setViewportSize({ width: v.w, height: v.h });
      await page.goto(s.path);
      await expect(page.locator('main')).toBeVisible();
      await page.waitForTimeout(600);
      if (s.click?.startsWith('search:')) { await page.getByRole('searchbox').fill(s.click.slice(7)); await page.getByRole('searchbox').press('Enter'); }
      else if (s.click?.startsWith('tab:')) await page.getByRole('tab', { name: s.click.slice(4) }).click();
      else if (s.click) await page.getByRole('button', { name: s.click, exact: true }).filter({ visible: true }).first().click();
      if (s.click) await page.waitForTimeout(500);
      await page.screenshot({ path: `../docs/design/admin/${s.name}-${v.w}.png`, fullPage: v.w < 1000 });
    }
  });
}
