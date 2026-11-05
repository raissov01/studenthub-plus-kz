import { test, expect, onboard } from '../../e2e/fixtures';

const tag = process.env.SHOT_TAG || 'after';
const dir = process.env.SHOT_DIR || 'docs/design/screens';

for (const theme of ['light', 'dark'] as const) {
  test(`screens ${theme}`, async ({ page, lang }) => {
    test.setTimeout(240_000);
    await onboard(page, lang);
    await page.evaluate(t => {
      const s = JSON.parse(localStorage.getItem('studenthub.v1') || '{}');
      localStorage.setItem('studenthub.v1', JSON.stringify({ ...s, theme: t }));
    }, theme);
    const settle = async () => {
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
      await page.waitForLoadState('networkidle', { timeout: 8000 }).catch(() => {});
      await page.waitForTimeout(900);
    };
    const shot = async (name: string, path: string) => {
      await page.goto(path);
      await settle();
      await page.screenshot({ path: `${dir}/${tag}-${theme}-${name}.png` });
    };
    await shot('home', '/');
    await shot('search', '/search');
    await shot('list', '/internships');
    await shot('detail', '/opportunity/e2e-intern');
    await shot('calendar', '/calendar');
    await shot('map', '/map');
    await shot('profile', '/profile');
    await page.goto('/calendar');
    await settle();
    await page.getByRole('button', { name: 'Іс-шара қосу', exact: true }).click();
    await expect(page.locator('.sheet')).toBeVisible();
    await page.waitForTimeout(500);
    await page.screenshot({ path: `${dir}/${tag}-${theme}-sheet.png` });
    await page.goto('/');
    await settle();
    await page.locator('#menu-btn').click();
    await expect(page.locator('#nav-drawer')).toBeVisible();
    await page.waitForTimeout(400);
    await page.screenshot({ path: `${dir}/${tag}-${theme}-menu.png` });
  });
}
