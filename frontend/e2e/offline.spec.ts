import { test, expect, onboard, label } from './fixtures';

// 11-020: the student QR card opens without internet when /discounts is entered again directly
test('student QR on /discounts opens after an offline reload', async ({ page, context, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await onboard(page, lang);
  await page.goto('/discounts');
  await expect.poll(() => page.evaluate(async () => (await navigator.serviceWorker.getRegistration())?.active?.state), { timeout: 20_000 }).toBe('activated');
  // a page that loaded while the worker was still installing may stay uncontrolled: load it once more
  if (!(await page.evaluate(() => !!navigator.serviceWorker.controller))) await page.reload();
  await expect.poll(() => page.evaluate(() => !!navigator.serviceWorker.controller), { timeout: 20_000 }).toBe(true);
  await expect.poll(() => page.evaluate(async () => !!(await caches.match(location.origin + '/discounts'))), { timeout: 20_000 }).toBe(true);
  await context.setOffline(true);
  const response = await page.reload();
  expect(response?.fromServiceWorker()).toBe(true);
  await expect(page.getByText(t('Интернет жоқ — сақталған деректер көрсетілуде', 'Нет интернета — показаны сохранённые данные'))).toBeVisible();
  await page.getByRole('button', { name: new RegExp(t('Цифрлық студенттік', 'Цифровой студенческий')) }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await expect(page.getByRole('dialog').locator('svg, canvas, img').first()).toBeVisible();
  // nothing private is kept by the worker
  const keys = await page.evaluate(async () => (await Promise.all((await caches.keys()).map(async k => (await (await caches.open(k)).keys()).map(r => new URL(r.url).pathname)))).flat());
  expect(keys.filter(p => /^\/(?:api|p)(?:\/|$)/.test(p))).toEqual([]);
  await context.setOffline(false);
});
