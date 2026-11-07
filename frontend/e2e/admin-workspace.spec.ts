import type { Page } from '@playwright/test';
import { test, expect, onboard, fits, label } from './fixtures';
import { config } from './content';
import { attendance, payments, settings, userNotes, userOrders, weekEvents } from './admin-data';
import type { Lang } from '../src/lib/types';

/* The admin workspace taken from the kurs panel (docs/research/admin-from-kurs.md), with a mocked API: the calendar,
   attendance, payments with a manual refund, settings, the user page tabs (orders, internal notes) and saved filters.
   The server's rights are covered by the Go tests and e2e/real-api/admin.spec.ts; here a shop manager opening a shop
   they don't manage gets the neutral not-found screen the 403 leads to. */

const none = { admin: false, moderator: false, blocked: false, moderation: null, cafes: [], printShops: [], clubs: [], university: null, feedback: [], employer: null, ambassador: false, mentor: false };

async function signedIn(page: Page, lang: Lang, role: string, panels: object = {}) {
  await page.route('**/api/clubs?**', r => r.fulfill({ json: { clubs: [] } }));
  await page.route('**/api/clubs', r => r.fulfill({ json: { clubs: [] } }));
  await page.route('**/api/cafe/orders/mine', r => r.fulfill({ json: { orders: [] } }));
  await page.route('**/api/print/jobs/mine', r => r.fulfill({ json: { jobs: [] } }));
  await page.route('**/api/config', r => r.fulfill({ json: { ...config, features: { ...config.features, cafe: true, print: true, clubs: true, events: true } } }));
  await page.route('**/api/me', r => r.fulfill({ json: {
    user: { id: 501, name: 'Аружан Тест', role, scopes: [], blocked: false, lang, consentAt: '2026-01-01', isAdult: true, university: 'KATU', faculty: 'it', course: 2 },
    needsConsent: false, inboxUnread: 0, console: true,
  } }));
  await page.route('**/api/console/me', r => r.fulfill({ json: { ...none, admin: role === 'admin', ...panels } }));
}

test('calendar: the week of events; a slot opens a prefilled form, an event opens its details', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang, 'ambassador', { ambassador: true });
  const seen: string[] = [];
  let patched: unknown = null;
  await page.route('**/api/events/**', route => {
    const req = route.request(), url = new URL(req.url());
    seen.push(url.pathname + url.search);
    if (url.pathname === '/api/events/manage') return route.fulfill({ json: { events: weekEvents() } });
    if (url.pathname === '/api/events/14' && req.method() === 'PATCH') { patched = req.postDataJSON(); return route.fulfill({ json: { event: weekEvents()[3] } }); }
    return route.fulfill({ status: 404, json: { error: { code: 'not_found' } } });
  });
  await onboard(page, lang);
  await page.goto('/admin/schedule');
  await expect.poll(() => seen.some(p => p.startsWith('/api/events/manage?from='))).toBe(true);
  // a phone gets the day list
  await expect(page.getByTestId('agenda')).toBeVisible();
  await expect(page.getByTestId('agenda-item')).toHaveCount(5);
  await fits(page);
  await page.getByTestId('agenda-item').filter({ hasText: 'Волонтерлер жиналысы' }).click();
  const sheet = page.getByRole('dialog');
  await expect(sheet.getByTestId('event-details')).toContainText(t('Черновик', 'Черновик'));
  await sheet.getByRole('button', { name: t('Жариялау', 'Опубликовать') }).click();
  await expect.poll(() => patched).toEqual({ status: 'published' });
  // the laptop grid: click a free half hour, the form opens with that time
  await page.setViewportSize({ width: 1280, height: 900 });
  await expect(page.getByTestId('week-grid')).toBeVisible();
  await expect(page.getByTestId('event-block')).toHaveCount(5);
  const slot = page.locator('[data-testid^="slot-"][data-testid$="-12:00"]').nth(3);
  const id = await slot.getAttribute('data-testid');
  await slot.click();
  await expect(page.getByRole('dialog').locator('#efStart')).toHaveValue(`${id!.slice(5, 15)}T12:00`);
  await expect(page.getByRole('dialog').locator('#efEnd')).toHaveValue(`${id!.slice(5, 15)}T13:30`);
});

test('attendance: weekly rate, the low-turnout filter and the way to the check-in page', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang, 'admin');
  await page.route('**/api/events/attendance**', r => r.fulfill({ json: attendance() }));
  await onboard(page, lang);
  await page.goto('/admin/attendance');
  await expect(page.getByTestId('kpi-strip')).toContainText('70%');
  await expect(page.locator('.att-row')).toHaveCount(3);
  await page.getByLabel(t('Тек жартысынан азы келгендер', 'Только где пришли меньше половины')).check();
  await expect(page.locator('.att-row')).toHaveCount(1);
  await expect(page.locator('.att-row')).toContainText('Дебат турнирі');
  await page.getByTestId('chart-attendance').getByRole('button', { name: t('Кесте', 'Таблица') }).click();
  await expect(page.getByTestId('chart-attendance').locator('tbody tr')).toHaveCount(12);
  await fits(page);
});

test('payments: totals, a manual refund marked as returned, a saved filter', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang, 'admin');
  const calls: string[] = [];
  let saved: unknown = null;
  await page.route('**/api/admin/**', route => {
    const req = route.request(), url = new URL(req.url());
    calls.push(req.method() + ' ' + url.pathname + url.search);
    if (url.pathname === '/api/admin/payments') return route.fulfill({ json: payments() });
    if (url.pathname === '/api/admin/views' && req.method() === 'POST') { saved = req.postDataJSON(); return route.fulfill({ json: { id: 5 } }); }
    if (url.pathname === '/api/admin/views') return route.fulfill({ json: { views: saved ? [{ id: 5, name: 'Қайтару', query: 'payment=refund_manual' }] : [] } });
    return route.fulfill({ status: 404, json: { error: { code: 'not_found' } } });
  });
  let refunded = false;
  await page.route('**/api/cafe/staff/orders/9012/refunded', r => { refunded = true; return r.fulfill({ json: { order: {} } }); });
  await onboard(page, lang);
  await page.goto('/admin/payments');
  await expect(page.getByTestId('kpi-strip')).toContainText('167 400 ₸');
  await expect(page.locator('.pay-row.is-owed')).toHaveCount(1);
  await fits(page);
  await page.locator('.pay-row.is-owed').getByRole('button', { name: t('Ақша қайтарылды', 'Деньги возвращены') }).click();
  await expect.poll(() => refunded).toBe(true);
  await page.locator('#pf_payment').selectOption('refund_manual');
  await expect.poll(() => calls.some(c => c.startsWith('GET /api/admin/payments?payment=refund_manual'))).toBe(true);
  await page.getByRole('button', { name: t('Осы сүзгіні сақтау', 'Сохранить этот фильтр') }).click();
  await page.getByRole('dialog').locator('#svName').fill('Қайтару');
  await page.getByRole('dialog').getByRole('button', { name: t('Сақтау', 'Сохранить') }).click();
  expect(saved).toEqual({ screen: 'payments', name: 'Қайтару', query: 'payment=refund_manual' });
  await expect(page.locator('.saved-chip')).toContainText('Қайтару');
});

test('payments: a café manager sees only their shop; another shop answers 403 and shows nothing', async ({ page, lang }) => {
  await signedIn(page, lang, 'student', { cafes: [{ id: 3, nameKk: 'Бас ғимарат кофесі', nameRu: 'Кофе в главном корпусе', role: 'manager', active: true, open: 2 }] });
  await page.route('**/api/admin/views**', r => r.fulfill({ json: { views: [] } }));
  await page.route('**/api/admin/payments**', route => {
    const url = new URL(route.request().url());
    if (url.searchParams.get('shop') === '9') return route.fulfill({ status: 403, json: { error: { code: 'forbidden', message: 'x' } } });
    return route.fulfill({ json: payments('managed') });
  });
  await onboard(page, lang);
  await page.goto('/admin');
  await page.locator('.panel-card').getByRole('button', { name: label(lang, 'Төлемдер', 'Платежи') }).click();
  await expect(page).toHaveURL(/\/admin\/payments\?cafe=3$/);
  await expect(page.getByText(label(lang, 'Сіз басқаратын нүктелердің тапсырыстары мен ақшасы.', 'Заказы и деньги точек, которыми вы управляете.'))).toBeVisible();
  await expect(page.locator('#pf_shop option')).toHaveCount(4);
  await page.goto('/admin/payments?cafe=9');
  await expect(page.getByText(label(lang, 'Мұндай бет жоқ.', 'Такой страницы нет.'))).toBeVisible();
  await expect(page.getByTestId('kpi-strip')).toHaveCount(0);
});

test('settings: support contacts saved, switches read only; students see the contacts', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang, 'admin');
  let put: Record<string, string> | null = null;
  await page.route('**/api/admin/config**', route => {
    if (route.request().method() === 'PUT') { put = route.request().postDataJSON(); return route.fulfill({ json: { support: put } }); }
    return route.fulfill({ json: settings() });
  });
  await page.route('**/api/support', r => r.fulfill({ json: { support: settings().support } }));
  await page.route('**/api/me/notifications', r => r.fulfill({ json: { prefs: {
    categories: { deadlines: true, digest: true, events: true, mentors: true, market: true, jobs: true, community: true, moderation: true },
    quietHours: { enabled: false, start: '22:00', end: '08:00', timezone: 'Asia/Almaty' }, dailyCap: 3, locked: [] } } }));
  await onboard(page, lang);
  await page.goto('/admin/settings');
  await expect(page.getByTestId('feature-list').locator('li')).toHaveCount(12);
  await expect(page.locator('#st_email')).toHaveValue('help@studenthub.kz');
  await page.locator('#st_phone').fill('+7 701 111 22 33');
  await page.getByRole('button', { name: t('Сақтау', 'Сохранить') }).click();
  await expect.poll(() => put?.phone).toBe('+7 701 111 22 33');
  await fits(page);
  await page.goto('/settings');
  await expect(page.getByTestId('support-card')).toContainText('@studenthub_help');
});

test('user page tabs: orders and internal notes', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang, 'admin');
  let note: unknown = null;
  await page.route('**/api/admin/**', route => {
    const req = route.request(), url = new URL(req.url());
    switch (url.pathname) {
      case '/api/admin/invites': return route.fulfill({ json: { invites: [] } });
      case '/api/admin/views': return route.fulfill({ json: { views: [] } });
      case '/api/admin/users': return route.fulfill({ json: { total: 0, page: 1, limit: 25, universities: [], users: [] } });
      case '/api/admin/users/77': return route.fulfill({ json: {
        user: { id: 77, telegramId: 9077, email: 'dana@example.kz', emailVerified: true, username: 'dana', name: 'Дана Ерланқызы', lang: 'kk', role: 'student', university: 'KATU', faculty: 'IT', course: 2, city: null, isAdult: true,
          consentAt: '2026-10-01T00:00:00Z', createdAt: '2026-10-01T00:00:00Z', lastSignIn: '2026-11-05T00:00:00Z', referredBy: null, botInactive: false },
        scopes: [], block: null, memberships: { cafes: [], printShops: [], clubs: [], university: null, feedback: [], employer: null, ambassador: null, mentor: null },
        activity: {}, reportsAgainst: [], audit: [] } });
      case '/api/admin/users/77/orders': return route.fulfill({ json: userOrders() });
      case '/api/admin/users/77/notes':
        if (req.method() === 'POST') { note = req.postDataJSON(); return route.fulfill({ json: { id: 3 } }); }
        return route.fulfill({ json: userNotes() });
    }
    return route.fulfill({ status: 404, json: { error: { code: 'not_found' } } });
  });
  await onboard(page, lang);
  await page.goto('/admin/users?id=77');
  await expect(page.getByText('dana@example.kz').first()).toBeVisible();
  await page.getByRole('tab', { name: t('Тапсырыстар', 'Заказы') }).click();
  await expect(page.getByTestId('user-orders').locator('.row')).toHaveCount(3);
  await expect(page.getByTestId('user-orders')).toContainText(t('ақшаны қолмен қайтару керек', 'нужно вернуть деньги вручную'));
  await page.getByRole('tab', { name: t('Ішкі жазбалар', 'Заметки') }).click();
  await expect(page.getByTestId('user-notes')).toContainText('900 ₸');
  await page.locator('#noteText').fill('Аккаунтын өшіруді сұрады');
  await page.getByRole('button', { name: t('Сақтау', 'Сохранить') }).click();
  await expect.poll(() => note).toEqual({ body: 'Аккаунтын өшіруді сұрады' });
  await fits(page);
});
