/* «Sign in with Google» (docs/GOOGLE_LOGIN.md). The OAuth round trip itself runs on the server (Go tests with a fake
   Google); here the browser side: the button exists only while /api/config says googleLogin, it sends the browser to
   /api/auth/google/start with this page as `next`, the ?google=<status> the callback appends becomes a message (or the
   consent screen) and leaves the address, and Settings → Аккаунт links and unlinks Google. */
import type { Page, Route } from '@playwright/test';
import { test, expect, label, onboard } from './fixtures';
import { config } from './content';

const user = (lang: string, extra: Record<string, unknown> = {}) => ({
  id: 801, telegramId: 8801, email: null, emailVerified: false, googleLinked: false, username: 'aruzhan', name: 'Аружан Google',
  lang, role: 'student', university: 'KATU', faculty: 'IT', course: 2, city: null, consentAt: '2026-10-01T00:00:00Z', isAdult: true,
  createdAt: '2026-10-01T00:00:00Z', scopes: [], blocked: false, ...extra,
});

async function settingsMocks(page: Page) {
  await page.route('**/api/me/notifications', route => route.fulfill({ json: { prefs: {
    categories: { deadlines: true, digest: true, events: true, mentors: true, market: true, jobs: true, community: true, moderation: true },
    quietHours: { enabled: true, start: '22:00', end: '08:00', timezone: 'Asia/Almaty' }, dailyCap: 3, locked: [] } } }));
}

/** the server's /start, played by the test: remembers the query and answers like the callback would, with a redirect */
function fakeStart(page: Page, back: (q: URLSearchParams) => string) {
  const seen: URLSearchParams[] = [];
  void page.route('**/api/auth/google/start**', (route: Route) => {
    const q = new URL(route.request().url()).searchParams;
    seen.push(q);
    return route.fulfill({ status: 303, headers: { location: back(q) } });
  });
  return seen;
}

test('Google button: hidden while the server has it off', async ({ page, lang }) => {
  await onboard(page, lang);
  await page.getByRole('button', { name: label(lang, 'Кіру', 'Вход'), exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('heading')).toBeVisible();
  await expect(dialog.getByRole('button', { name: label(lang, 'Google арқылы кіру', 'Войти через Google') })).toHaveCount(0);
  await expect(dialog.locator('.btn-google')).toHaveCount(0);
});

test('Google button: shown when on; the round trip comes back signed in to the same page', async ({ page, lang }) => {
  await page.route('**/api/config', route => route.fulfill({ json: { ...config, googleLogin: true } }));
  let signedIn = false;
  await page.route('**/api/me', route => route.fulfill({ json: signedIn ? { user: user(lang), needsConsent: false, inboxUnread: 0 } : { user: null, needsConsent: false } }));
  const seen = fakeStart(page, q => { signedIn = true; return `${q.get('next')}${q.get('next')!.includes('?') ? '&' : '?'}google=ok`; });
  await onboard(page, lang);
  await page.goto('/calendar?view=week');
  await page.getByRole('button', { name: label(lang, 'Кіру', 'Вход'), exact: true }).click();
  const dialog = page.getByRole('dialog');
  const btn = dialog.getByRole('button', { name: label(lang, 'Google арқылы кіру', 'Войти через Google') });
  await expect(btn).toBeVisible();
  // Google's branding: white button, 1px #747775 border, the four-colour G
  expect(await btn.evaluate(el => { const s = getComputedStyle(el); return [s.backgroundColor, s.borderTopWidth, s.borderTopColor]; }))
    .toEqual(['rgb(255, 255, 255)', '1px', 'rgb(116, 119, 117)']);
  await expect(btn.locator('svg.google-g path')).toHaveCount(4);
  await btn.click();
  await expect.poll(() => seen.length).toBe(1);
  expect(seen[0].get('next')).toBe('/calendar?view=week');
  expect(seen[0].get('lang')).toBe(lang);
  expect(seen[0].has('link')).toBe(false);
  // back on the page it started on: a message, and ?google= gone from the address
  await expect(page.locator('.toast')).toHaveText(label(lang, 'Кірдіңіз', 'Вы вошли'));
  await expect(page).toHaveURL(/\/calendar\?view=week$/);
});

test('Google: a new account gets the consent screen, an error comes back as a message', async ({ page, lang }) => {
  await page.route('**/api/config', route => route.fulfill({ json: { ...config, googleLogin: true } }));
  let me: unknown = { user: null, needsConsent: false };
  await page.route('**/api/me', route => route.fulfill({ json: me }));
  await onboard(page, lang);
  me = { user: user(lang, { telegramId: null, email: 'aruzhan@gmail.com', emailVerified: true, googleLinked: true, consentAt: null }), needsConsent: true };
  await page.goto('/?google=ok');
  await expect(page.getByRole('dialog')).toContainText(label(lang, 'Аккаунт және деректеріңіз', 'Аккаунт и ваши данные'));
  await expect(page.getByRole('dialog')).toContainText(label(lang, 'Google аккаунтыңыздың тұрақты идентификаторы', 'Постоянный идентификатор вашего аккаунта Google'));
  await expect(page).toHaveURL(/\/$/);

  me = { user: null, needsConsent: false };
  await page.goto('/profile?google=email_unverified');
  await expect(page.locator('.toast')).toContainText(label(lang, 'email расталмаған', 'не подтверждён'));
  await expect(page).toHaveURL(/\/profile$/);
});

test('Settings: link Google, then unlink; the last sign-in method stays', async ({ page, lang }) => {
  await page.route('**/api/config', route => route.fulfill({ json: { ...config, googleLogin: true } }));
  await settingsMocks(page);
  let linked = false;
  await page.route('**/api/me', route => route.fulfill({ json: { user: user(lang, { googleLinked: linked }), needsConsent: false, inboxUnread: 0 } }));
  const seen = fakeStart(page, q => { linked = true; return `${q.get('next')}?google=linked`; });
  let unlinkAnswer: { status: number; json: unknown } = { status: 200, json: { user: user(lang, { googleLinked: false }) } };
  const deletes: string[] = [];
  await page.route('**/api/me/google', route => {
    deletes.push(route.request().method());
    if (unlinkAnswer.status === 200) linked = false;
    return route.fulfill(unlinkAnswer);
  });
  await page.addInitScript(l => {
    if (!localStorage.getItem('studenthub.v1')) localStorage.setItem('studenthub.v1', JSON.stringify({ v: 1, onboarded: true, lang: l, profile: { name: 'Аружан Google', spec: 'it', course: 2 } }));
  }, lang);
  await page.goto('/settings');
  const link = page.getByRole('button', { name: label(lang, 'Google-ды байланыстыру', 'Привязать Google') });
  await expect(link).toBeVisible();
  await link.click();
  await expect.poll(() => seen.length).toBe(1);
  expect(seen[0].get('link')).toBe('1');
  expect(seen[0].get('next')).toBe('/settings');
  await expect(page.locator('.toast')).toHaveText(label(lang, 'Google байланыстырылды', 'Google привязан'));
  await expect(page.locator('#set-account .row-title', { hasText: label(lang, 'Google байланысқан', 'Google привязан') })).toBeVisible();
  await expect(page).toHaveURL(/\/settings$/);

  // the only way in: the server refuses with its own message
  const lastMsg = label(lang, 'Google — аккаунтқа кірудің жалғыз жолы.', 'Google — единственный способ входа в аккаунт.');
  unlinkAnswer = { status: 409, json: { error: { code: 'last_method', message: lastMsg } } };
  const unlink = page.getByRole('button', { name: label(lang, 'Ажырату', 'Отвязать'), exact: true });
  await unlink.click();
  await expect(page.locator('.toast')).toContainText(lastMsg);
  await expect(unlink).toBeVisible();

  unlinkAnswer = { status: 200, json: { user: user(lang, { googleLinked: false }) } };
  await unlink.click();
  await expect(page.locator('.toast')).toHaveText(label(lang, 'Google ажыратылды', 'Google отвязан'));
  await expect(link).toBeVisible();
  expect(deletes).toEqual(['DELETE', 'DELETE']);
});

test('Settings: no Google row while the server has it off', async ({ page, lang }) => {
  await settingsMocks(page);
  await page.route('**/api/me', route => route.fulfill({ json: { user: user(lang), needsConsent: false, inboxUnread: 0 } }));
  await page.addInitScript(l => {
    if (!localStorage.getItem('studenthub.v1')) localStorage.setItem('studenthub.v1', JSON.stringify({ v: 1, onboarded: true, lang: l, profile: { name: 'Аружан Google', spec: 'it', course: 2 } }));
  }, lang);
  await page.goto('/settings');
  await expect(page.getByRole('button', { name: label(lang, 'Аккаунтты өшіру', 'Удалить аккаунт') })).toBeVisible();
  await expect(page.locator('.google-ico, .btn-google')).toHaveCount(0);
  await expect(page.getByRole('button', { name: label(lang, 'Google-ды байланыстыру', 'Привязать Google') })).toHaveCount(0);
});
