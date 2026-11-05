import type { Page } from '@playwright/test';
import { test, expect, onboard, fits, label } from './fixtures';
import { config } from './content';
import type { Lang } from '../src/lib/types';

/* The role-scoped /admin console with a mocked API: each person sees only their own panels; the super admin's user
   directory filters, opens a user, and blocks with a reason. Server-side enforcement is covered by the Go tests and
   the real-API spec (e2e/real-api/admin.spec.ts). */

const none = { admin: false, moderator: false, blocked: false, moderation: null, cafes: [], printShops: [], clubs: [], university: null, feedback: [], employer: null, ambassador: false, mentor: false };

async function signedIn(page: Page, lang: Lang, role: string, scopes: string[], features: Record<string, boolean> = {}) {
  // the home screen's own widgets of the switched-on features (not under test here)
  await page.route('**/api/clubs?**', route => route.fulfill({ json: { clubs: [] } }));
  await page.route('**/api/clubs', route => route.fulfill({ json: { clubs: [] } }));
  await page.route('**/api/cafe/orders/mine', route => route.fulfill({ json: { orders: [] } }));
  await page.route('**/api/print/jobs/mine', route => route.fulfill({ json: { jobs: [] } }));
  await page.route('**/api/config', route => route.fulfill({ json: { ...config, features: { ...config.features, cafe: true, print: true, clubs: true, market: true, feeds: true, ...features } } }));
  await page.route('**/api/me', route => route.fulfill({ json: {
    user: { id: 501, name: 'Аружан Тест', role, scopes, blocked: false, lang, consentAt: '2026-01-01', isAdult: true, university: 'KATU', faculty: 'it', course: 2 },
    needsConsent: false, inboxUnread: 0, console: true,
  } }));
}

test('console: a café barista sees only their café panel, no admin sections', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang, 'student', []);
  const seen: string[] = [];
  await page.route('**/api/console/**', route => {
    const path = new URL(route.request().url()).pathname;
    seen.push(path);
    if (path === '/api/console/me') return route.fulfill({ json: { ...none, cafes: [{ id: 3, nameKk: 'Бас ғимарат кофесі', nameRu: 'Кофе в главном корпусе', role: 'barista', active: true, open: 4 }] } });
    if (path === '/api/console/cafe/3/stats') return route.fulfill({ json: { role: 'barista', shop: { id: 3, kind: 'cafe', nameKk: 'Бас ғимарат кофесі', nameRu: 'Кофе в главном корпусе', paymentMode: 'pickup', active: true },
      today: { byStatus: { new: 2, accepted: 1, ready: 1, picked_up: 6 }, created: 10, done: 6, refundsToReturn: 0 } } });
    return route.fulfill({ status: 403, json: { error: { code: 'forbidden' } } });
  });
  await onboard(page, lang);
  await page.goto('/admin');
  const card = page.locator('.panel-card');
  await expect(card).toHaveCount(1);
  await expect(card).toContainText(lang === 'ru' ? 'Кофе в главном корпусе' : 'Бас ғимарат кофесі');
  await expect(card).toContainText(t('бариста', 'бариста'));
  await expect(page.getByRole('button', { name: t('Қолданушылар', 'Пользователи') })).toHaveCount(0);
  await expect(page.getByText(t('Шолу', 'Обзор'), { exact: true })).toHaveCount(0);
  await fits(page);
  await card.getByRole('button', { name: t('Панель', 'Панель'), exact: true }).click();
  await expect(page).toHaveURL(/\/admin\/shop\?cafe=3$/);
  await expect(page.getByText(t('Қазір тақтада', 'Сейчас на доске'))).toBeVisible();
  // a barista gets today's counts, never the revenue, the team or the Kaspi state
  await expect(page.getByText(t('Берілгендер сомасы', 'Сумма выданных'))).toHaveCount(0);
  await expect(page.getByRole('heading', { name: t('Команда', 'Команда') })).toHaveCount(0);
  await fits(page);
  expect(seen.every(p => p.startsWith('/api/console/'))).toBe(true);
});

test('console: a moderator gets the moderation workspace only', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang, 'student', ['moderator']);
  await page.route('**/api/console/me', route => route.fulfill({ json: { ...none, moderator: true, moderation: { pending: 3, reports: 1 } } }));
  await page.route('**/api/admin/**', route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/admin/queue') return route.fulfill({ json: { items: [], pendingByKind: { opportunity: 3 } } });
    if (path === '/api/admin/reports') return route.fulfill({ json: { reports: [{ kind: 'qa_question', id: '4', total: 1, counted: 1, hideAt: 3, isHidden: false, firstAt: '2026-11-05T10:00:00Z', reasons: ['спам'] }] } });
    return route.fulfill({ status: 403, json: { error: { code: 'forbidden' } } });
  });
  await onboard(page, lang);
  await page.goto('/admin');
  await expect(page.locator('.panel-card')).toHaveCount(1);
  await expect(page.locator('.panel-card')).toContainText(t('модератор', 'модератор'));
  await page.getByRole('button', { name: t('Жұмыс орнын ашу', 'Открыть рабочее место') }).click();
  await expect(page).toHaveURL(/\/admin\/moderation$/);
  await expect(page.locator('.stat').first()).toContainText('3');
  await expect(page.getByRole('button', { name: new RegExp(t('Модерация кезегі', 'Очередь модерации')) })).toBeVisible();
  await fits(page);
  // the super admin's screens stay closed in the UI too (the server answers 403 anyway)
  await page.goto('/admin/users');
  await expect(page.getByRole('searchbox')).toHaveCount(0);
});

test('console: a student without panels sees the neutral not-found screen', async ({ page, lang }) => {
  await page.route('**/api/me', route => route.fulfill({ json: { user: { id: 502, name: 'Студент', role: 'student', scopes: [], lang, consentAt: '2026-01-01' }, needsConsent: false, inboxUnread: 0, console: false } }));
  await page.route('**/api/console/me', route => route.fulfill({ json: none }));
  await onboard(page, lang);
  await page.goto('/admin');
  await expect(page.getByText(label(lang, 'Мұндай бет жоқ.', 'Такой страницы нет.'))).toBeVisible();
  await expect(page.locator('.panel-card')).toHaveCount(0);
});

test('console: super admin overview, user directory filters, user detail and a block with a reason', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang, 'admin', []);
  await page.route('**/api/console/me', route => route.fulfill({ json: { ...none, admin: true, moderator: true, moderation: { pending: 0, reports: 0 } } }));
  const days = (n: number) => Array.from({ length: n }, (_, i) => ({ day: `2026-11-${String(i + 1).padStart(2, '0')}`, n: i % 3 }));
  const calls: { method: string; path: string; search: string; body: unknown }[] = [];
  let blocked = false;
  await page.route('**/api/admin/**', route => {
    const req = route.request(), url = new URL(req.url());
    calls.push({ method: req.method(), path: url.pathname, search: url.search, body: req.postData() ? req.postDataJSON() : null });
    switch (url.pathname) {
      case '/api/admin/dashboard': return route.fulfill({ json: {
        users: { total: 40, accounts: 31, new7: 4, new30: 12, blocked: 0, moderators: 1, byRole: { student: 36, admin: 2 } },
        signups: days(30), active: { dau: 5, wau: 14, mau: 22, since: '2026-11-01', days: days(14) }, devices: { dau: 9, wau: 30 },
        content: { adminContent: 3, community: 2, jobs: 0, events: 1, clubs: 4, market: 0, lostfound: 1, qa: 2, roommates: 0, surveys: 1 },
        pending: { queue: 2, queueModerator: 2, reports: 1, feedbackHeld: 0, feedbackOverdue: 0, redemptions: 0, employers: 0, cafeRefunds: 0, printRefunds: 0 },
        shops: { cafe: { orders: 3, done: 2, revenue: 180000 }, print: { orders: 0, done: 0, revenue: 0 } },
        features: { cafe: true, print: true, clubs: true },
      } });
      case '/api/admin/analytics': return route.fulfill({ json: { devices: 0, returned_day1: 0, eligible_day1: 0, returned_day7: 0, eligible_day7: 0, saved_first: 0, from_share: 0, ambassadors: null, feedback_avg: 0, feedback_count: 0, feedback_recent: null } });
      case '/api/admin/queue': return route.fulfill({ json: { items: [], pendingByKind: {} } });
      case '/api/admin/users': return route.fulfill({ json: {
        total: 1, page: 1, limit: 25, universities: [{ name: 'KATU', count: 30 }],
        users: [{ id: 77, telegramId: 900, email: null, username: 'dana', name: 'Дана Е.', role: 'student', university: 'KATU', consentAt: '2026-10-01T00:00:00Z', createdAt: '2026-10-01T00:00:00Z', lastActive: '2026-11-05', blocked, scopes: [] }],
      } });
      case '/api/admin/users/77': return route.fulfill({ json: {
        user: { id: 77, telegramId: 900, email: null, emailVerified: false, username: 'dana', name: 'Дана Е.', lang: 'kk', role: 'student', university: 'KATU', faculty: 'it', course: 2, city: null, isAdult: true,
          consentAt: '2026-10-01T00:00:00Z', createdAt: '2026-10-01T00:00:00Z', lastSignIn: '2026-11-05T00:00:00Z', referredBy: null, botInactive: false },
        scopes: [], block: blocked ? { reason: 'спам в вопросах', createdAt: '2026-11-05T12:00:00Z', by: { id: 501, name: 'Аружан Тест' } } : null,
        memberships: { cafes: [], printShops: [], clubs: [], university: null, feedback: [], employer: null, ambassador: null, mentor: null },
        activity: { lastActive: '2026-11-05', activeDays30: 6, cafeOrders: 2, printJobs: 0, submissions: 1, reportsFiled: 0, marketItems: 0, lostfoundPosts: 0, qaQuestions: 3, qaAnswers: 1, clubMemberships: 1, eventRegs: 0 },
        reportsAgainst: [{ kind: 'qa_question', id: '4', reports: 2, dismissed: 0, firstAt: '2026-11-04T00:00:00Z', lastAt: '2026-11-05T00:00:00Z' }],
        audit: blocked ? [{ id: 9, action: 'user_block', reason: 'спам в вопросах', meta: {}, createdAt: '2026-11-05T12:00:00Z', actor: { id: 501, name: 'Аружан Тест' }, target: { id: 77, name: 'Дана Е.' } }] : [],
      } });
      case '/api/admin/users/77/block': blocked = true; return route.fulfill({ json: { blocked: true, changed: true } });
    }
    return route.fulfill({ status: 404, json: { error: { code: 'not_found' } } });
  });
  await onboard(page, lang);
  await page.goto('/admin');
  await expect(page.getByText(t('Бүгін белсенді (DAU)', 'Активны сегодня (DAU)'))).toBeVisible();
  await expect(page.locator('.bars').first()).toBeVisible();
  await fits(page);
  await page.getByRole('button', { name: new RegExp(t('Қолданушылар', 'Пользователи')) }).first().click();
  await expect(page).toHaveURL(/\/admin\/users$/);
  await page.locator('details.card summary').click();
  await page.locator('#uf_role').selectOption('student');
  await page.locator('#uf_active').selectOption('7');
  await expect.poll(() => calls.some(c => c.path === '/api/admin/users' && c.search.includes('role=student') && c.search.includes('active=7'))).toBe(true);
  await fits(page);
  await page.getByRole('button', { name: /Дана Е\./ }).click();
  await expect(page).toHaveURL(/\/admin\/users\?id=77$/);
  await expect(page.getByText('qa_question #4')).toBeVisible();
  await page.getByRole('button', { name: t('Шектеу', 'Ограничить'), exact: true }).click();
  const dialog = page.getByRole('dialog');
  const confirm = dialog.getByRole('button', { name: t('Шектеу', 'Ограничить'), exact: true });
  await expect(confirm).toBeDisabled();
  await dialog.getByRole('textbox').fill('спам в вопросах');
  await confirm.click();
  await expect(dialog).toBeHidden();
  expect(calls.find(c => c.path === '/api/admin/users/77/block')?.body).toEqual({ reason: 'спам в вопросах' });
  await expect(page.locator('.badge.b-bad').first()).toBeVisible();
  await expect(page.locator('.adm-log')).toContainText('спам в вопросах');
  await fits(page);
});

test('feeds: a moderator filters the pending queue, selects items and bulk-publishes after a counted preview', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang, 'student', ['moderator']);
  const item = (id: number, confidence: number, source_id: string, deadline: string | null) => ({
    id, status: 'pending', category: 'grant', source: source_id === 'a' ? 'Channel A' : 'Channel B', source_id, confidence, auto: false, submission_id: 100 + id,
    deadline, created_at: '2026-11-05T10:00:00Z', published_at: null, extra_sources: [],
    data: { title_kk: `Грант ${id}`, title_ru: `Грант ${id}`, summary_kk: 'Қысқаша', source_url: 'https://t.me/a/1', apply_url: 'https://example.org' },
  });
  const items = [item(1, 0.95, 'a', '2099-01-01'), item(2, 0.5, 'a', null), item(3, 0.92, 'b', '2020-01-01')];
  const bodies: { action: string; filter: Record<string, unknown> }[] = [];
  await page.route('**/api/admin/feeds**', route => {
    const url = new URL(route.request().url());
    if (url.pathname === '/api/admin/feeds') return route.fulfill({ json: { counts: { pending: 3, published: 0 }, sources: [],
      usage: { calls: 0, failures: 0, tokens: 0, limit: 100000, ai: true, autopublish_min_confidence: 0, channel: false } } });
    if (url.pathname === '/api/admin/feeds/items') return route.fulfill({ json: { items: url.searchParams.get('status') === 'pending' ? items : [] } });
    if (url.pathname === '/api/admin/feeds/items/bulk') {
      const b = route.request().postDataJSON();
      bodies.push(b);
      if (b.action === 'preview') return route.fulfill({ json: { count: 2, max: 500, items: [
        { id: 1, title: 'Грант 1', confidence: 0.95, source: 'Channel A', deadline: '2099-01-01' },
        { id: 3, title: 'Грант 3', confidence: 0.92, source: 'Channel B', deadline: '2020-01-01' }] } });
      return route.fulfill({ json: { published: 1, rejected: 0, skipped: [{ id: 3, reason: 'expired' }], failed: [] } });
    }
    return route.fulfill({ status: 404, json: { error: { code: 'not_found' } } });
  });
  await onboard(page, lang);
  await page.goto('/admin/feeds');
  await expect(page.getByText('Грант 2')).toBeVisible();
  await page.getByRole('button', { name: '90%' }).click();
  await expect(page.getByText('Грант 2')).toHaveCount(0);
  await page.getByRole('button', { name: t('Көрініп тұрғанның бәрін белгілеу', 'Выделить все видимые') }).click();
  const bar = page.getByRole('region', { name: t('Таңдалғандар', 'Выбранные') });
  await expect(bar).toContainText('2');
  await fits(page);
  await bar.getByRole('button', { name: t('Таңдалғанды жариялау', 'Опубликовать выбранные') }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.locator('.bulk-preview li')).toHaveCount(2);
  await dialog.getByRole('button', { name: t('Жариялау (2)', 'Опубликовать (2)') }).click();
  await expect(dialog.getByText(t('мерзімі өткен, өткізілді', 'просрочены, пропущены'))).toBeVisible();
  expect(bodies.map(b => b.action)).toEqual(['preview', 'approve']);
  expect(bodies[1].filter).toEqual({ ids: [1, 3] });
});
