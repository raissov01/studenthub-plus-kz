import { test, expect, onboard } from './fixtures';

// 11-025: notices built from the sample events carry the «үлгі» / «пример» label
test('device notifications from sample events are labelled as demo', async ({ page, lang }) => {
  await onboard(page, lang);
  await page.goto('/notifications');
  const demo = page.locator('.nrow .b-demo');
  await expect(demo.first()).toBeVisible();
  await expect(demo.first()).toHaveText(lang === 'ru' ? 'пример' : 'үлгі');
});
