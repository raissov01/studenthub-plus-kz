import { test, expect } from '@playwright/test';

// Selected only by E2E_REAL_API=1. Read-only: no chat costs, account changes, registrations or notifications.
test('real API: public configuration/content and neutral first visit', async ({ page }) => {
  const config = await page.request.get('/api/config');
  expect(config.ok()).toBe(true);
  expect((await config.json()).features).toBeTruthy();
  const content = await page.request.get('/api/content');
  expect(content.ok()).toBe(true);
  const body = await content.json();
  for (const key of ['jobs', 'opportunities', 'discounts']) expect(Array.isArray(body[key])).toBe(true);
  await page.goto('/');
  await expect(page.getByRole('dialog')).toBeVisible();
  await expect(page.locator('body')).not.toContainText(/Құралай|Куралай/);
});
