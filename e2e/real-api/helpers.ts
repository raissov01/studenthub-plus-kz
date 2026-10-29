import { test as base, expect, type Page, type BrowserContext } from '@playwright/test';
import { createHash, createHmac, randomInt } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { defaults } from '../../lib/defaults';
import { botToken, adminTelegramID } from './settings';

export type Lang = 'kk' | 'ru';
export const label = (lang: Lang, kk: string, ru: string) => lang === 'ru' ? ru : kk;
export function sql<T = Record<string, unknown>>(SQL: string, ...Args: unknown[]): T[] {
  return JSON.parse(execFileSync('.fe-deps/real-api', ['sql'], {
    input: JSON.stringify({ SQL, Args }), encoding: 'utf8', env: process.env,
  }));
}
export function widget(id: number, name: string, age = 0, token = botToken) {
  const fields: Record<string, string> = {
    id: String(id), first_name: name, username: `e2e_${id}`,
    auth_date: String(Math.floor(Date.now() / 1000) - age),
  };
  const data = Object.keys(fields).sort().map(k => `${k}=${fields[k]}`).join('\n');
  return { ...fields, hash: createHmac('sha256', createHash('sha256').update(token).digest()).update(data).digest('hex') };
}
export function initData(id: number) {
  const fields = new URLSearchParams({ auth_date: String(Math.floor(Date.now() / 1000)), user: JSON.stringify({ id, first_name: 'Mini App' }) });
  fields.sort();
  const data = [...fields].map(([k, v]) => `${k}=${v}`).join('\n');
  const secret = createHmac('sha256', 'WebAppData').update(botToken).digest();
  fields.set('hash', createHmac('sha256', secret).update(data).digest('hex'));
  return fields.toString();
}
export async function response(page: Page, path: string, action: () => Promise<unknown>, method = 'POST') {
  const [r] = await Promise.all([
    page.waitForResponse(r => new URL(r.url()).pathname === path && r.request().method() === method),
    action(),
  ]);
  expect(r.ok(), `${method} ${path}: ${await r.text()}`).toBe(true);
  return r.json();
}
export async function login(page: Page, lang: Lang, id: number, name: string) {
  await page.goto('/settings');
  await page.getByRole('button', { name: label(lang, 'Telegram арқылы кір', 'Войти через Telegram') }).click();
  await expect.poll(() => page.evaluate(() => typeof window.onTelegramAuth)).toBe('function');
  const signed = await response(page, '/api/auth/telegram/widget', () => page.evaluate(data => window.onTelegramAuth!(data), widget(id, name)));
  if (signed.needsConsent) {
    const dialog = page.getByRole('dialog');
    await dialog.getByRole('radio', { name: label(lang, '18 жасқа толдым', 'Мне есть 18 лет'), exact: true }).check();
    await dialog.getByRole('checkbox').check();
    await response(page, '/api/me/consent', () => dialog.getByRole('button', { name: label(lang, 'Растау', 'Подтвердить'), exact: true }).click());
  }
  await expect(page.getByRole('dialog')).toBeHidden();
  return signed.user as { id: number; name: string; role: string };
}

export type Actor = { page: Page; id: number; telegramID: number; name: string };
export const test = base.extend<{
  lang: Lang;
  actor: (name: string, options?: { admin?: boolean; course?: number }) => Promise<Actor>;
}>({
  lang: async ({}, run, info) => run(info.project.metadata.lang as Lang),
  actor: async ({ browser, baseURL, lang }, run, info) => {
    const contexts: BrowserContext[] = [];
    const failures: string[] = [];
    await run(async (name, options = {}) => {
      const context = await browser.newContext({ ...info.project.use, baseURL });
      contexts.push(context);
      // Only Telegram's external widget script is replaced. Every /api request is real.
      await context.route('https://telegram.org/js/**', route => route.fulfill({ contentType: 'application/javascript', body: '' }));
      const state = defaults(lang);
      state.onboarded = true;
      state.profile = { ...state.profile, name, course: options.course ?? 2, spec: 'econ', city: 'astana' };
      await context.addInitScript(s => {
        if (!localStorage.getItem('studenthub.v1')) localStorage.setItem('studenthub.v1', JSON.stringify(s));
      }, state);
      const page = await context.newPage();
      page.on('pageerror', e => failures.push(e.message));
      const telegramID = options.admin ? adminTelegramID : randomInt(1000000000, 2000000000);
      const user = await login(page, lang, telegramID, name);
      await response(page, '/api/me', () => page.getByRole('button', { name: label(lang, 'Аккаунтпен синхрондау', 'Синхронизировать с аккаунтом') }).click(), 'PATCH');
      return { page, id: user.id, telegramID, name };
    });
    for (const [i, context] of contexts.entries()) {
      if (info.status !== info.expectedStatus || failures.length) {
        for (const [j, page] of context.pages().entries()) {
          const screenshot = info.outputPath(`actor-${i}-page-${j}.png`);
          await page.screenshot({ path: screenshot, fullPage: true }).catch(() => {});
          await info.attach(`actor-${i}-page-${j}`, { path: screenshot, contentType: 'image/png' });
        }
      }
      await context.close();
    }
    expect(failures, 'Uncaught browser errors').toEqual([]);
  },
});
export { expect };

export async function approve(admin: Page, lang: Lang, uniqueText: string) {
  await admin.goto('/admin/queue');
  const card = admin.locator('.card').filter({ hasText: uniqueText });
  await expect(card).toHaveCount(1);
  await card.getByRole('button', { name: label(lang, 'Бекіту', 'Одобрить'), exact: true }).click();
  const waiting = admin.waitForResponse(r => /\/api\/admin\/queue\/\d+\/approve$/.test(r.url()));
  await admin.getByRole('dialog').getByRole('button', { name: label(lang, 'Бекіту', 'Одобрить'), exact: true }).click();
  const r = await waiting;
  expect(r.ok(), await r.text()).toBe(true);
  await expect(admin.getByRole('dialog')).toBeHidden();
}
