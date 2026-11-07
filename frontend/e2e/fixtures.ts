import { test as base, expect, type Page } from '@playwright/test';
import { config, content } from './content';
import type { Content } from '../src/lib/types';
import type { Lang } from '../src/lib/types';

/** the projects run in Kazakh and Russian; English has its own spec (i18n-en.spec.ts) */
export type E2ELang = Exclude<Lang, 'en'>;
export const test = base.extend<{ lang: E2ELang }>({
  lang: async ({}, run, info) => { await run(info.project.metadata.lang as E2ELang); },
  page: async ({ page }, run) => {
    const failures: string[] = [];
    page.on('pageerror', err => failures.push(err.message));
    // Enforced CSP violations are test failures, including late loads and service worker registration.
    await page.exposeFunction('__reportCspViolation', (message: string) => failures.push(`CSP: ${message}`));
    await page.addInitScript(() => {
      const violations: string[] = [];
      Object.assign(window, { __cspViolations: violations });
      document.addEventListener('securitypolicyviolation', e => {
        if (e.disposition === 'enforce') {
          const message = `${e.effectiveDirective}: ${e.blockedURI}`;
          violations.push(message);
          void (window as unknown as { __reportCspViolation: (m: string) => Promise<void> }).__reportCspViolation(message);
        }
      });
    });
    if (process.env.E2E_REAL_API !== '1') {
      await page.route('**/api/**', async route => {
        const path = new URL(route.request().url()).pathname;
        let json: unknown;
        if (path === '/api/config') json = config;
        else if (path === '/api/content') json = content;
        else if (path === '/api/me') json = { user: null, needsConsent: false };
        else if (path === '/api/support') json = { support: {} }; // the settings page's «Қолдау қызметі» (nothing published)
        else if (path === '/api/chat') {
          const body = route.request().postDataJSON();
          expect(body.messages.at(-1)?.role).toBe('user');
          expect(body.profile.gpa).toBeNull();
          json = { text: body.lang === 'ru' ? 'Начните с короткого резюме и выберите стажировку.' : 'Қысқа түйіндемеден бастап, тағылымдама таңдаңыз.' };
        } else if (/^\/api\/(events|analytics|client-errors)/.test(path)) json = { ok: true };
        else if (/^\/api\/discounts\//.test(path)) json = { worked: 0, failed: 0, mine: null };
        else {
          failures.push(`Unmocked API request: ${route.request().method()} ${path}`);
          await route.fulfill({ status: 501, json: { error: { code: 'unmocked' } } });
          return;
        }
        await route.fulfill({ json });
      });
    }
    // External services are isolated in the deterministic suite. A separate test loads the real widget source.
    await page.route('https://fonts.googleapis.com/**', route => route.fulfill({ contentType: 'text/css', body: '' }));
    await page.route('https://telegram.org/js/**', route => route.fulfill({ contentType: 'application/javascript', body: '' }));
    await run(page);
    if (!page.isClosed()) {
      const violations = await page.evaluate(() => (window as unknown as { __cspViolations: string[] }).__cspViolations ?? []).catch(() => []);
      expect(violations, 'CSP violations').toEqual([]);
    }
    expect(failures, 'Browser errors / unexpected API calls').toEqual([]);
  },
});
export { expect };

export const label = (lang: Lang, kk: string, ru: string) => lang === 'ru' ? ru : kk;
export async function onboard(page: Page, lang: Lang, course = '2') {
  await page.goto('/');
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect(page.locator('body')).not.toContainText(/Құралай|Куралай/);
  if (lang === 'ru') await dialog.getByRole('tab', { name: 'Русский', exact: true }).click();
  await dialog.locator('#fName').fill('Аружан Тест');
  await dialog.locator('#fSpec').selectOption('econ');
  await dialog.locator('#fCourse').selectOption(course);
  await expect(dialog.locator('#fGpa')).not.toHaveAttribute('required');
  await dialog.getByRole('button', { name: label(lang, 'Бастау', 'Начать'), exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect(await deviceState(page)).toMatchObject({ onboarded: true, lang, profile: { name: 'Аружан Тест', gpa: null, phone: '', skills: [] } });
  expect((await deviceState(page)).profile).not.toHaveProperty('demo');
}
export async function deviceState(page: Page) {
  return page.evaluate(() => JSON.parse(localStorage.getItem('studenthub.v1') || '{}'));
}
export async function fits(page: Page) {
  const overflow = await page.evaluate(() => ({ width: innerWidth, scroll: document.documentElement.scrollWidth }));
  expect(overflow.scroll).toBeLessThanOrEqual(overflow.width);
}

/** this page's /api/content answers with `over` merged into the fixture (a later route wins over the default one) */
export async function withContent(page: Page, over: Partial<Content>) {
  await page.route('**/api/content', route => route.fulfill({ json: { ...content, ...over } }));
}

/** a student's own timetable (the «Сабақ кестесі» screen's data) on the given weekdays, Monday = 0 */
export const timetable = (days: number[] = [0, 1, 2, 3, 4]) => days.flatMap(wd => [
  { id: `m${wd}`, name: 'Микроэкономика', wd, t: '09:00', end: '09:50', room: '4-214', kind: 'Дәріс' },
  { id: `e${wd}`, name: 'Ағылшын тілі', wd, t: '10:50', end: '11:40', room: '4-305', kind: 'Практика' },
  { id: `s${wd}`, name: 'Статистика', wd, t: '13:00', end: '13:50', room: '4-108', kind: 'Семинар' },
]);
/** store that timetable on the device (after onboarding) and reload */
export async function seedTimetable(page: Page, days?: number[]) {
  await page.evaluate(tt => {
    const s = JSON.parse(localStorage.getItem('studenthub.v1') || '{}');
    s.timetable = tt;
    localStorage.setItem('studenthub.v1', JSON.stringify(s));
  }, timetable(days));
  await page.reload();
}

/** sample-content markers that must not appear anywhere for a real student: «үлгі», a standalone «пример», «demo» */
export const DEMO_TEXT = /үлгі|(?<![а-яё])пример(?![а-яё])|demo/i;
