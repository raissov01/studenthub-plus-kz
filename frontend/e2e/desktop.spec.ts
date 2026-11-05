import { test, expect, onboard, fits, seedTimetable } from './fixtures';

/* The laptop layout (≥1024px, app/desktop.css + components/SideNav.tsx), run only in the kk-1280 project:
   the sidebar replaces the bottom tabs and the menu, the home is a dashboard, sheets are centred dialogs,
   search filters sit in a side panel, a detail page has a right rail. */

test('sidebar: every destination, active state, collapse to icons remembered', async ({ page }) => {
  await onboard(page, 'kk');
  await expect(page.locator('.tabbar')).toBeHidden();
  await expect(page.locator('#menu-btn')).toBeHidden();
  const side = page.locator('.sidenav');
  await expect(side).toBeVisible();
  await expect(side.getByRole('button', { name: 'Басты бет', exact: true })).toHaveAttribute('aria-current', 'page');
  // the menu's sections are in the sidebar (same list as the phone menu)
  for (const name of ['Жұмыс', 'Тағылымдама', 'Іс-шаралар', 'Жеңілдіктер', 'Сабақ кестесі', 'Кампус картасы', 'Сақталғандар'])
    await expect(side.getByRole('button', { name, exact: true })).toBeVisible();

  await side.getByRole('button', { name: 'Тағылымдама', exact: true }).click();
  await expect(page).toHaveURL(/\/internships$/);
  await expect(side.getByRole('button', { name: 'Тағылымдама', exact: true })).toHaveAttribute('aria-current', 'page');
  await expect(side.getByRole('button', { name: 'Басты бет', exact: true })).not.toHaveAttribute('aria-current', 'page');
  // «Тағылымдама» has no filters beyond its tabs: no panel, no filter button
  await expect(page.getByRole('complementary', { name: 'Сүзгілер' })).toHaveCount(0);
  await expect(page.locator('.filter-btn')).toHaveCount(0);
  // «Жұмыс»: the filters in a side panel instead of a sheet
  await side.getByRole('button', { name: 'Жұмыс', exact: true }).click();
  await expect(page).toHaveURL(/\/jobs$/);
  const panel = page.getByRole('complementary', { name: 'Сүзгілер' });
  await expect(panel).toBeVisible();
  await expect(page.locator('.filter-btn')).toHaveCount(0);
  await expect(panel.locator('#fCity')).toBeVisible();
  await fits(page);

  await side.getByRole('button', { name: 'Күнтізбе', exact: true }).click();
  await expect(page).toHaveURL(/\/calendar$/);

  // collapse: labels leave the layout but stay as accessible names; the choice survives a reload
  const wide = (await side.boundingBox())!.width;
  await side.getByRole('button', { name: 'Мәзірді жию' }).click();
  await expect.poll(async () => (await side.boundingBox())!.width).toBeLessThan(wide / 2);
  await expect(side.getByRole('button', { name: 'Күнтізбе', exact: true })).toBeVisible();
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-side', 'mini');
  await side.getByRole('button', { name: 'Мәзірді ашу' }).click();
  await expect(page.locator('html')).not.toHaveAttribute('data-side', 'mini');
});

test('home is a dashboard grid; «/» focuses search', async ({ page }) => {
  await onboard(page, 'kk');
  await seedTimetable(page);
  const classes = page.locator('#homeClasses'), week = page.locator('#homeWeek');
  await expect(classes).toBeVisible();
  await expect(week).toBeVisible();
  await expect(page.locator('#homeQuick')).toBeVisible();
  // today's classes and the week sit side by side
  const a = (await classes.boundingBox())!, b = (await week.boundingBox())!;
  expect(b.x).toBeGreaterThan(a.x + a.width - 1);
  expect(Math.abs(b.y - a.y)).toBeLessThan(a.height);
  await expect(week.locator('.hw-day')).toHaveCount(7);
  await expect(week.locator('.hw-day[aria-current="date"]')).toHaveCount(1);
  await fits(page);

  await page.locator('#title').click();
  await page.keyboard.press('/');
  await expect(page).toHaveURL(/\/search$/);
  await expect(page.locator('#sq')).toBeFocused();
});

test('a sheet opens as a centred dialog; a detail page has a right rail', async ({ page }) => {
  await onboard(page, 'kk');
  await page.goto('/calendar');
  await page.getByRole('button', { name: 'Іс-шара қосу', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  const box = (await dialog.boundingBox())!;
  const vp = page.viewportSize()!;
  expect(box.width).toBeLessThanOrEqual(560);
  expect(vp.height - (box.y + box.height)).toBeGreaterThan(16); // not glued to the bottom edge
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();

  await page.goto('/opportunity/e2e-intern');
  const rail = page.locator('.detail-rail');
  await expect(rail).toBeVisible();
  await expect(rail.getByRole('button', { name: 'Сақтау және мерзімді бақылау' })).toBeVisible();
  const main = (await page.locator('.detail-main').boundingBox())!, r = (await rail.boundingBox())!;
  expect(r.x).toBeGreaterThan(main.x + main.width - 1);
  await fits(page);
});
