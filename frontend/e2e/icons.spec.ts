/* Icon review sheet and before/after shots of the screens that carry the most icons.
   Off by default; run with ICON_SHOTS=<dir under docs/design/icons> (e.g. ICON_SHOTS=after) on the kk-390 project. */
import { test, expect, type Page } from '@playwright/test';
import { IC, type IconName } from '../src/lib/icons';
import { config, content } from './content';

const OUT = process.env.ICON_SHOTS;
const DIR = `../docs/design/icons/${OUT}`;
test.skip(!OUT, 'review shots only on request (ICON_SHOTS=<dir>)');
test.beforeEach(({}, info) => test.skip(info.project.name !== 'kk-390', 'one project is enough'));

const ALL_ON = { ...config, features: Object.fromEntries(['events', 'ambassador', 'mentors', 'market', 'portfolio', 'b2b', 'quiz', 'points', 'cafe', 'print', 'surveys',
  'feedback', 'wellbeing', 'lostfound', 'courseqa', 'roommates', 'clubs', 'feeds', 'streak', 'fairs'].map(k => [k, true])) };

async function mock(page: Page) {
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname;
    const json = path === '/api/config' ? ALL_ON : path === '/api/content' ? content : path === '/api/me' ? { user: null, needsConsent: false } : {};
    return route.fulfill({ json });
  });
  await page.route('https://fonts.googleapis.com/**', route => route.fulfill({ contentType: 'text/css', body: '' }));
  await page.route('https://telegram.org/js/**', route => route.fulfill({ contentType: 'application/javascript', body: '' }));
}

const THEMES = {
  light: { bg: '#FFFFFF', cell: '#FFFFFF', line: '#E9E5F2', text: '#1D1930', muted: '#615B78', tint: '#EEE9FD', ink: '#5B45C9' },
  dark: { bg: '#14111D', cell: '#1C1828', line: '#372F4D', text: '#EDE9F7', muted: '#ABA4C2', tint: '#2B2443', ink: '#CDBFFF' },
};

test('icon review sheet: every icon at 20 and 32 px, light and dark', async ({ page }) => {
  const names = Object.keys(IC) as IconName[];
  const svg = (n: IconName, s: number) => `<svg viewBox="0 0 24 24" width="${s}" height="${s}" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">${IC[n]}</svg>`;
  for (const [theme, c] of Object.entries(THEMES)) {
    await page.setViewportSize({ width: 1180, height: 800 });
    await page.setContent(`<!doctype html><html><head><style>
      body { margin: 0; padding: 24px; background: ${c.bg}; color: ${c.text}; font: 13px/1.3 system-ui, sans-serif; }
      h1 { font-size: 16px; margin: 0 0 16px; font-weight: 600; }
      .grid { display: grid; grid-template-columns: repeat(8, 1fr); gap: 8px; }
      .cell { background: ${c.cell}; border: 1px solid ${c.line}; border-radius: 12px; padding: 12px 8px 10px; display: grid; justify-items: center; gap: 8px; }
      .row { display: flex; align-items: center; gap: 14px; height: 40px; }
      .chip { width: 40px; height: 40px; border-radius: 10px; background: ${c.tint}; color: ${c.ink}; display: grid; place-items: center; }
      .name { color: ${c.muted}; font-family: ui-monospace, monospace; font-size: 12px; }
    </style></head><body><h1>Student Hub icons — ${names.length} — ${theme}</h1><div class="grid">${names.map(n =>
      `<div class="cell"><div class="row">${svg(n, 20)}<span class="chip">${svg(n, 20)}</span>${svg(n, 32)}</div><div class="name">${n}</div></div>`).join('')}</div></body></html>`);
    await page.screenshot({ path: `${DIR}/sheet-${theme}.png`, fullPage: true });
  }
});

test('screens with icons: tab bar, hamburger menu, profile at 390×844', async ({ page }) => {
  await mock(page);
  await page.goto('/');
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await dialog.locator('#fName').fill('Аружан Тест');
  await dialog.locator('#fSpec').selectOption('econ');
  await dialog.locator('#fCourse').selectOption('2');
  await dialog.getByRole('button', { name: 'Бастау', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate(t => document.documentElement.setAttribute('data-theme', t), theme);
    await page.goto('/');
    await page.evaluate(t => document.documentElement.setAttribute('data-theme', t), theme);
    await page.waitForTimeout(300);
    await page.screenshot({ path: `${DIR}/home-${theme}.png` });
    await page.locator('nav.tabbar').screenshot({ path: `${DIR}/tabbar-${theme}.png` });
    await page.getByRole('button', { name: 'Мәзір', exact: true }).click();
    const menu = page.getByRole('dialog', { name: 'Мәзір' });
    await expect(menu).toBeVisible();
    await page.waitForTimeout(400);
    await page.screenshot({ path: `${DIR}/menu-${theme}.png` });
    await page.evaluate(() => document.querySelector('.drawer')?.scrollTo(0, 99999));
    await page.waitForTimeout(200);
    await page.screenshot({ path: `${DIR}/menu-end-${theme}.png` });
    await page.keyboard.press('Escape');
    await page.goto('/profile');
    await page.evaluate(t => document.documentElement.setAttribute('data-theme', t), theme);
    await page.waitForTimeout(300);
    await page.screenshot({ path: `${DIR}/profile-${theme}.png`, fullPage: true });
  }
});
