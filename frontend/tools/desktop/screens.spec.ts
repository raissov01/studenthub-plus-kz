import { test, expect, type Page } from '@playwright/test';
import { config, content } from '../../e2e/content';
import { onboard, seedTimetable } from '../../e2e/fixtures';

/* The screens the laptop layout touches, at every size of the config, against a mocked API (signed-in admin, cafe,
   print and clubs switched on). Writes docs/design/desktop/<screen>-<width>.png; SHOT_ONLY=home,cafe limits the set. */
const dir = process.env.SHOT_DIR || '../docs/design/desktop';
const only = (process.env.SHOT_ONLY || '').split(',').filter(Boolean);

const user = { id: 1, name: 'Аружан Тест', role: 'admin', lang: 'kk', consentAt: '2026-01-01', isAdult: true, university: 'Сейфуллин университеті', faculty: 'IT', course: 2, email: 'a@example.org', emailVerified: true, telegramId: null, username: null, city: 'astana', createdAt: '2026-01-01' };
const cafe = { id: 3, nameKk: 'Тест кофейня', nameRu: 'Тест кофейня', building: 'Бас ғимарат', placeKk: '1-қабат', placeRu: '1 этаж', openTime: '08:00', closeTime: '20:00', openDays: [1, 2, 3, 4, 5, 6, 7], slotMinutes: 10, slotCapacity: 6, leadMinutes: 10, active: true, paymentMode: 'both', isDemo: false, openNow: true, payments: ['pickup', 'kaspi'] };
const item = (id: number, categoryId: number, nameKk: string, price: number, descKk = '') => ({ id, cafeId: 3, categoryId, nameKk, nameRu: nameKk, descKk, descRu: descKk, price, available: true, archived: false, sort: id, options: [] });
const menu = { cafe, categories: [{ id: 1, nameKk: 'Кофе', nameRu: 'Кофе', sort: 1, active: true }, { id: 2, nameKk: 'Тағамдар', nameRu: 'Еда', sort: 2, active: true }],
  items: [item(11, 1, 'Латте', 100000, 'Сүтті'), item(12, 1, 'Капучино', 90000, 'Көпіршікті'), item(13, 1, 'Американо', 70000), item(14, 2, 'Круассан', 60000), item(15, 2, 'Сэндвич', 120000, 'Тауық етімен')],
  slots: [{ at: '2026-11-05T07:20:00Z', full: false }, { at: '2026-11-05T07:40:00Z', full: false }] };
const shop = { id: 4, nameKk: 'Тест полиграфия', nameRu: 'Тест полиграфия', building: 'Бас ғимарат', placeKk: '1-қабат, 104', placeRu: '1 этаж, 104', openTime: '08:00', closeTime: '20:00', openDays: [1, 2, 3, 4, 5, 6, 7], slotMinutes: 15, leadMinutes: 20, minOrder: 0, accepts: ['pdf', 'docx', 'jpg', 'png'], retentionDays: 3, active: true, paymentMode: 'both', noteKk: '', noteRu: '', openNow: true, payments: ['pickup', 'kaspi'] };
const printSvc = { id: 21, shopId: 4, kind: 'print', nameKk: 'Басып шығару', nameRu: 'Печать', descKk: '', descRu: '', online: true, active: true, sort: 0, pricing: { sizes: { A4: { bw: 2000, color: 10000, bwDuplex: 1500, colorDuplex: 9000 } } } };
const catalog = { shop, services: [printSvc, { ...printSvc, id: 22, kind: 'binding', nameKk: 'Түптеу', nameRu: 'Переплёт', pricing: { tiers: [{ maxSheets: 100, price: 50000 }] } }], goods: [{ id: 31, shopId: 4, nameKk: 'Папка', nameRu: 'Папка', price: 30000, inStock: true, archived: false, sort: 0 }], slots: [{ at: '2026-11-05T09:00:00Z' }], limits: { maxFileBytes: 20971520, maxFiles: 5, dailyFiles: 30, dailyBytes: 209715200 } };
const clubBase = { name_ru: null, summary_ru: null, about_kk: null, about_ru: null, university: 'Сейфуллин университеті', room: null, schedule_ru: null, leader_title_ru: null, leader_contact: null, leader_contact_public: false, telegram_url: null, instagram_url: null, join_mode: 'request', join_link: null, recruiting: false, status: 'active', source_url: 'https://kazatu.edu.kz', verified_at: '2026-11-05', schedule_kk: null, leader_title_kk: null, building_id: null, summary_kk: null, members: 0 };
const clubs = [
  { ...clubBase, id: 1, name_kk: '«Шырақ» пікір-сайыс клубы', direction: 'debate', room: '214', schedule_kk: 'Сейсенбі, 18:00', summary_kk: 'Қазақ тіліндегі дебат клубы.', recruiting: true },
  { ...clubBase, id: 2, name_kk: 'Спорт клубы', direction: 'sport', summary_kk: 'Футбол, волейбол, жүзу.' },
  { ...clubBase, id: 3, name_kk: '«Arrow crew» би тобы', direction: 'art', join_mode: 'link', join_link: 'https://t.me/arrow_crew' },
  { ...clubBase, id: 4, name_kk: 'IT клубы', direction: 'it', summary_kk: 'Хакатондар мен жобалар.' },
];

async function mockApi(page: Page) {
  await page.route('https://fonts.googleapis.com/**', r => r.fulfill({ contentType: 'text/css', body: '' }));
  await page.route('**/api/**', async route => {
    const p = new URL(route.request().url()).pathname;
    const json = (j: unknown) => route.fulfill({ json: j });
    if (p === '/api/config') return json({ ...config, features: { ...config.features, cafe: true, print: true, clubs: true, events: true } });
    if (p === '/api/content') return json(content);
    if (p === '/api/me') return json({ user, needsConsent: false, inboxUnread: 2 });
    if (p === '/api/me/inbox') return json({ items: [], unread: 0, nextBefore: null });
    if (p === '/api/cafes') return json({ cafes: [cafe] });
    if (p === '/api/cafes/3/menu') return json(menu);
    if (p === '/api/cafe/orders/mine') return json({ orders: [] });
    if (p === '/api/print/shops') return json({ shops: [shop] });
    if (p === '/api/print/shops/4') return json(catalog);
    if (p === '/api/print/jobs/mine') return json({ jobs: [] });
    if (p === '/api/clubs') return json({ clubs });
    if (p === '/api/events') return json({ events: [] });
    if (p === '/api/admin/summary') return json({ users: { total: 1240, byRole: { student: 1200, admin: 3, ambassador: 25, employer: 12 }, withConsent: 1100 }, activeReminders: 310, features: { cafe: true, print: true, clubs: true } });
    if (p === '/api/admin/analytics') return json({ devices: 2100, returned_day1: 640, eligible_day1: 1500, returned_day7: 380, eligible_day7: 1300, saved_first: 520, from_share: 90, ambassadors: null, feedback_avg: 4.4, feedback_count: 37, feedback_recent: [] });
    if (p === '/api/admin/users') return json({ users: [] });
    return json({});
  });
}

async function seed(page: Page) {
  await onboard(page, 'kk');
  await seedTimetable(page);
}

const SCREENS: [string, string, ((p: Page) => Promise<void>)?][] = [
  ['home', '/'],
  ['search', '/search'],
  ['section', '/internships'],
  ['detail', '/opportunity/e2e-intern'],
  ['job', '/job/job-101'],
  ['calendar', '/calendar'],
  ['profile', '/profile'],
  ['settings', '/settings'],
  ['clubs', '/clubs'],
  ['cafe', '/cafe'],
  ['cafe-menu', '/cafe?id=3', async p => { await p.locator('.cafe-add').first().click(); await p.locator('.cafe-add').nth(3).click(); }],
  ['print', '/print'],
  ['admin', '/admin'],
  ['notif', '/notifications'],
  ['sheet', '/calendar', async p => { await p.getByRole('button', { name: 'Іс-шара қосу', exact: true }).click(); await expect(p.locator('.sheet')).toBeVisible(); }],
  ['filters', '/search', async p => { const b = p.locator('.filter-btn:visible'); if (await b.count()) { await b.click(); await expect(p.locator('.sheet')).toBeVisible(); } }],
  ['menu', '/', async p => { const b = p.locator('#menu-btn:visible'); if (await b.count()) { await b.click(); await expect(p.locator('#nav-drawer')).toBeVisible(); } }],
];

test('screens', async ({ page }, info) => {
  test.setTimeout(300_000);
  page.on('pageerror', e => console.log('pageerror:', e.message));
  await mockApi(page);
  await seed(page);
  const w = info.project.name.split('x')[0];
  for (const [name, path, act] of SCREENS) {
    if (only.length && !only.includes(name)) continue;
    await page.goto(path);
    await page.waitForLoadState('networkidle', { timeout: 8000 }).catch(() => {});
    await page.waitForTimeout(700);
    if (act) await act(page);
    await page.waitForTimeout(300);
    await page.screenshot({ path: `${dir}/${name}-${w}.png` });
  }
});
