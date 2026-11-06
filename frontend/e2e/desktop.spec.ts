import type { Page } from '@playwright/test';
import { test, expect, onboard, fits, seedTimetable } from './fixtures';
import { config } from './content';

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
  // the week takes the whole width above today's classes (seven readable days)
  const a = (await classes.boundingBox())!, b = (await week.boundingBox())!;
  expect(b.y + b.height).toBeLessThanOrEqual(a.y + 1);
  expect(b.width).toBeGreaterThan(a.width * 1.5);
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

/* ---------- laptop layouts for places, the week strip and detail pages (components/desk.tsx) ---------- */
const cafeBase = { id: 3, nameKk: 'Тест кофейня', nameRu: 'Тест кофейня', building: 'Бас ғимарат', placeKk: '1-қабат', placeRu: '1 этаж', openTime: '08:00', closeTime: '20:00', openDays: [1, 2, 3, 4, 5, 6, 7], slotMinutes: 10, slotCapacity: 6, leadMinutes: 10, active: true, paymentMode: 'both', isDemo: false, openNow: true, payments: ['pickup', 'kaspi'] };
const menuOf = (cafe: typeof cafeBase) => ({
  cafe, categories: [{ id: 1, nameKk: 'Кофе', nameRu: 'Кофе', sort: 1, active: true }],
  items: [{ id: 11, cafeId: cafe.id, categoryId: 1, nameKk: 'Латте', nameRu: 'Латте', descKk: '', descRu: '', price: 100000, available: true, archived: false, sort: 1, options: [] }],
  slots: [{ at: '2030-01-01T07:20:00Z', full: false }],
});
async function cafeApi(page: Page, ref: { cafes: typeof cafeBase[] }) {
  await page.route('**/api/config', r => r.fulfill({ json: { ...config, features: { ...config.features, cafe: true } } }));
  await page.route('**/api/cafes', r => r.fulfill({ json: { cafes: ref.cafes } }));
  await page.route(/\/api\/cafes\/\d+\/menu$/, r => {
    const id = Number(/cafes\/(\d+)\//.exec(r.request().url())![1]);
    return r.fulfill({ json: menuOf(ref.cafes.find(c => c.id === id) ?? ref.cafes[0]) });
  });
}

test('café: one place is a workspace (menu + cart); several are an open-first card grid', async ({ page }) => {
  const ref = { cafes: [cafeBase] };
  await cafeApi(page, ref);
  await onboard(page, 'kk');
  await page.goto('/cafe');
  await expect(page.locator('#cafeHead')).toContainText('Тест кофейня');
  const panel = page.locator('#cafeCartPanel');
  await expect(panel).toBeVisible();
  await expect(panel.getByRole('button', { name: /Тапсырыс беру/ })).toBeDisabled();
  await page.getByRole('button', { name: 'Латте: себетке қосу' }).click();
  await expect(panel).toContainText('Латте');
  await expect(panel.getByRole('button', { name: /Тапсырыс беру/ })).toBeEnabled();
  const menu = (await page.locator('.cafe-items').boundingBox())!, cart = (await panel.boundingBox())!;
  expect(cart.x).toBeGreaterThan(menu.x + menu.width - 1);
  await fits(page);

  ref.cafes = [{ ...cafeBase, id: 5, nameKk: 'Жабық кофейня', openNow: false }, cafeBase, { ...cafeBase, id: 6, nameKk: 'Үшінші' }];
  await page.goto('/cafe');
  const cards = page.locator('#cafeGrid .place-card');
  await expect(cards).toHaveCount(3);
  await expect(cards.last()).toContainText('Жабық кофейня'); // open first
  const a = (await cards.nth(0).boundingBox())!, b = (await cards.nth(1).boundingBox())!;
  expect(Math.abs(a.y - b.y)).toBeLessThan(2); // side by side
  await cards.first().click();
  await expect(page).toHaveURL(/\/cafe\?id=\d+$/);
  await fits(page);
});

test('week strip: long names wrap instead of shrinking; the full title stays in the accessible name', async ({ page }) => {
  await onboard(page, 'kk');
  await page.evaluate(() => {
    const s = JSON.parse(localStorage.getItem('studenthub.v1') || '{}');
    s.timetable = [0, 1, 2, 3, 4, 5, 6].flatMap(wd => ['Микроэкономика және макроэкономика негіздері', 'Философия', 'Статистика', 'Экология'].map((name, i) => ({ id: `l${wd}${i}`, name, wd, t: `${8 + i * 2}:00`.padStart(5, '0'), end: '23:50', room: '1', kind: 'Дәріс' })));
    localStorage.setItem('studenthub.v1', JSON.stringify(s));
  });
  await page.reload();
  const week = page.locator('#homeWeek');
  const day = week.locator('.hw-day').first();
  await expect(day.locator('.hw-more')).toHaveText('+1');
  await expect(day).toHaveAttribute('aria-label', /Микроэкономика және макроэкономика негіздері/);
  const name = day.locator('.hw-name').first();
  expect((await name.boundingBox())!.width).toBeGreaterThan(90);
  const lines = await name.evaluate(el => Math.round(el.getBoundingClientRect().height / parseFloat(getComputedStyle(el).lineHeight)));
  expect(lines).toBe(2);
  await fits(page);
});

test('event page: description in the column, facts and sign-up in a right rail', async ({ page }) => {
  const ev = {
    id: 9001, title_kk: 'Жастар форумы', title_ru: null, desc_kk: 'Сипаттама мәтіні', desc_ru: null, cat: 'career', starts_at: '2030-01-10T10:00:00Z', ends_at: '2030-01-10T13:00:00Z',
    university: 'Сейфуллин университеті', building_id: null, place_text: 'Акт залы', capacity: null, walk_in_allowed: true, gives_cert: false, teams_enabled: false,
    status: 'published', created_by: 1, checkin_started_at: null, finished_at: null, created_at: '2029-12-01', registered: 3, waitlist: 0, checked_in: 0, spots_left: null, can_manage: false,
  };
  await page.route('**/api/config', r => r.fulfill({ json: { ...config, features: { ...config.features, events: true } } }));
  await page.route(/\/api\/events(\?.*)?$/, r => r.fulfill({ json: { events: [ev, { ...ev, id: 9002, title_kk: 'Ұқсас кездесу' }] } }));
  await page.route(/\/api\/events\/9001(\?.*)?$/, r => r.fulfill({ json: { event: ev } }));
  await page.route(/\/api\/events\/9001\/attendees/, r => r.fulfill({ json: { count: 3 } }));
  await onboard(page, 'kk');
  await page.goto('/event/9001');
  const rail = page.getByRole('complementary', { name: 'Іс-шара туралы' });
  await expect(rail).toBeVisible();
  await expect(rail.getByRole('button', { name: 'Тіркелу' })).toBeVisible();
  await expect(page.locator('.desk-main')).toContainText('Сипаттама мәтіні');
  await expect(page.locator('.ev-similar')).toContainText('Ұқсас кездесу');
  const main = (await page.locator('.desk-main').boundingBox())!, r = (await rail.boundingBox())!;
  expect(r.x).toBeGreaterThan(main.x + main.width - 1);
  await fits(page);
});
