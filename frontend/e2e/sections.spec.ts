import path from 'node:path';
import type { Page } from '@playwright/test';
import { test, expect, onboard, fits, label } from './fixtures';
import { config, contestOpp, job, watOpp, youthJob } from './content';

async function withData(page: Page, market = false) {
  if (market) {
    await page.route('**/api/config', route => route.fulfill({ json: { ...config, features: { ...config.features, market: true } } }));
    await page.route('**/api/market/**', route => route.fulfill({ json: { items: [], nextBefore: null } }));
  }
}

const tabs = (page: Page) => page.getByRole('tablist');
const selected = (page: Page) => tabs(page).locator('[role=tab][aria-selected=true]');

test('each section opens from the menu with its own sub-tabs and no generic chip row', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await withData(page);
  await onboard(page, lang);
  const sections: [string, RegExp, string[]][] = [
    [t('Жұмыс', 'Работа'), /\/jobs$/, [t('Қосымша жұмыс', 'Подработка'), t('Жастар практикасы', 'Молодёжная практика')]],
    [t('Тағылымдама', 'Стажировки'), /\/internships$/, [t('Компаниядағы тағылымдама', 'Стажировки в компаниях'), t('Академиялық ұтқырлық', 'Академическая мобильность'),
      'Work & Travel', t('Алмасу және саяхат', 'Обмены и поездки'), t('Гранттар мен стипендиялар', 'Гранты и стипендии')]],
    [t('Іс-шаралар', 'Мероприятия'), /\/events$/, [t('Барлығы', 'Все'), t('Байқаулар', 'Конкурсы'), t('Хакатондар', 'Хакатоны'), t('Конференциялар мен форумдар', 'Конференции и форумы')]],
    [t('Курстар', 'Курсы'), /\/courses$/, []],
    [t('Жеңілдіктер', 'Скидки'), /\/discounts$/, []], // «Тегін беремін» is behind FEATURE_MARKET (off here)
  ];
  for (const [name, url, want] of sections) {
    await page.getByRole('button', { name: t('Мәзір', 'Меню'), exact: true }).click();
    await page.getByRole('dialog', { name: t('Мәзір', 'Меню') }).getByRole('button', { name, exact: true }).click();
    await expect(page).toHaveURL(url);
    await expect(page.locator('#title')).toHaveText(name);
    await expect(page.locator('.search-cats')).toHaveCount(0);
    await expect(page.getByRole('group', { name: t('Санат', 'Категория'), exact: true })).toHaveCount(0);
    if (want.length) {
      await expect(tabs(page)).toHaveAttribute('aria-label', name);
      await expect(tabs(page).getByRole('tab')).toHaveText(want);
      await expect(selected(page)).toHaveText(want[0]);
      await expect(page.getByRole('tabpanel')).toBeVisible();
    } else {
      await expect(tabs(page)).toHaveCount(0);
    }
    await fits(page);
  }
  // «Мобильдік» is no longer a section of its own, and «Іздеу» has the search box without chips
  await page.goto('/search');
  await expect(page.getByRole('searchbox')).toBeVisible();
  await expect(tabs(page)).toHaveCount(0);
  await expect(page.locator('.chip')).toHaveCount(0);
});

test('old URLs land on the right section and sub-tab', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await withData(page);
  await onboard(page, lang);
  const cases: [string, RegExp, string][] = [
    ['/jobs', /\/jobs$/, t('Қосымша жұмыс', 'Подработка')],
    ['/jobs?t=youth', /\/jobs\?t=youth$/, t('Жастар практикасы', 'Молодёжная практика')],
    ['/internships', /\/internships$/, t('Компаниядағы тағылымдама', 'Стажировки в компаниях')],
    ['/internships?t=wat', /\/internships\?t=wat$/, 'Work & Travel'],
    ['/events?t=hackathons', /\/events\?t=hackathons$/, t('Хакатондар', 'Хакатоны')],
    ['/search?k=jobs', /\/jobs$/, t('Қосымша жұмыс', 'Подработка')],
    ['/search?k=mobility', /\/internships\?t=mobility$/, t('Академиялық ұтқырлық', 'Академическая мобильность')],
    ['/search?k=grant', /\/internships\?t=grants$/, t('Гранттар мен стипендиялар', 'Гранты и стипендии')],
    ['/search?k=intern', /\/internships$/, t('Компаниядағы тағылымдама', 'Стажировки в компаниях')],
    ['/search?k=events', /\/events$/, t('Барлығы', 'Все')],
    ['/jobs?t=nonsense', /\/jobs\?t=nonsense$/, t('Қосымша жұмыс', 'Подработка')],
  ];
  for (const [from, url, tab] of cases) {
    await page.goto(from);
    await expect(page).toHaveURL(url);
    await expect(selected(page)).toHaveText(tab);
  }
  for (const [from, url] of [['/search?k=courses', /\/courses$/], ['/search?k=deals', /\/discounts$/], ['/search?k=market', /\/discounts\?t=free$/]] as const) {
    await page.goto(from);
    await expect(page).toHaveURL(url);
    await expect(page.locator('#title')).toHaveText(from.endsWith('courses') ? t('Курстар', 'Курсы') : t('Жеңілдіктер', 'Скидки'));
  }
});

test('sub-tabs: items sort by subtype, arrow keys move, the tab is in the URL and survives «back»', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await withData(page);
  await onboard(page, lang);
  await page.goto('/jobs');
  await expect(page.getByRole('link', { name: new RegExp(job.title[lang]) })).toBeVisible();
  await expect(page.getByRole('button', { name: new RegExp(youthJob.title[lang]) })).toHaveCount(0);
  await page.getByRole('tab', { name: t('Жастар практикасы', 'Молодёжная практика'), exact: true }).click();
  await expect(page).toHaveURL(/\/jobs\?t=youth$/);
  await expect(page.getByRole('link', { name: new RegExp(job.title[lang]) })).toHaveCount(0);
  await page.getByRole('button', { name: new RegExp(youthJob.title[lang]) }).click();
  await expect(page).toHaveURL(/\/job\/e2e-youth$/);
  await page.goBack();
  await expect(page).toHaveURL(/\/jobs\?t=youth$/);
  await expect(selected(page)).toHaveText(t('Жастар практикасы', 'Молодёжная практика'));

  // keyboard: one tab stop, arrows and Home/End select and focus
  await page.goto('/internships');
  const first = selected(page);
  await first.focus();
  await page.keyboard.press('ArrowRight');
  await expect(page).toHaveURL(/\/internships\?t=mobility$/);
  await expect(page.getByRole('tab', { name: t('Академиялық ұтқырлық', 'Академическая мобильность'), exact: true })).toBeFocused();
  await page.keyboard.press('End');
  await expect(selected(page)).toHaveText(t('Гранттар мен стипендиялар', 'Гранты и стипендии'));
  await expect(page.locator('[role=tab][tabindex="0"]')).toHaveCount(1);
  await page.keyboard.press('ArrowRight');
  await expect(page).toHaveURL(/\/internships$/);
  await page.keyboard.press('ArrowLeft');
  await expect(page).toHaveURL(/\/internships\?t=grants$/);
  // a competition is not a grant: it sits under «Іс-шаралар / Байқаулар»
  await expect(page.getByText(contestOpp.title[lang])).toHaveCount(0);
  await page.getByRole('tab', { name: 'Work & Travel', exact: true }).click();
  await expect(page.getByText(watOpp.title[lang]).first()).toBeVisible();
  await page.goto('/events?t=contests');
  await expect(page.getByText(contestOpp.title[lang]).first()).toBeVisible();
  // the long row scrolls inside itself at 360 px, the page does not
  await page.goto('/internships');
  const scrolls = await tabs(page).evaluate(el => ({ sw: el.scrollWidth, cw: el.clientWidth, ox: getComputedStyle(el).overflowX }));
  expect(scrolls.ox).toBe('auto');
  expect(scrolls.sw).toBeGreaterThan(scrolls.cw);
  await fits(page);
});

// keys pressed back to back: the router applies history.replaceState to useSearchParams later, so each key must see the
// tab the previous key picked, not the one the URL hook still reports
test('sub-tab keys pressed back to back land on the right tab', async ({ page, lang }) => {
  await onboard(page, lang);
  await page.goto('/internships');
  await selected(page).focus();
  for (const k of ['ArrowRight', 'End', 'ArrowRight', 'ArrowLeft', 'ArrowLeft', 'Home', 'ArrowLeft']) await page.keyboard.press(k);
  // company -> mobility -> grants -> company -> grants -> exchange -> company -> grants
  await expect(page).toHaveURL(/\/internships\?t=grants$/);
  await expect(selected(page)).toHaveAttribute('id', 'st-intern-grants');
  await expect(page.locator('#st-intern-grants')).toBeFocused();
});

test('«Жеңілдіктер» has «Тегін беремін» when the market is on', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await withData(page, true);
  await onboard(page, lang);
  await page.goto('/discounts');
  await expect(tabs(page).getByRole('tab')).toHaveText([t('Жеңілдіктер', 'Скидки'), t('Тегін беремін', 'Отдам даром')]);
  await page.getByRole('tab', { name: t('Тегін беремін', 'Отдам даром'), exact: true }).click();
  await expect(page).toHaveURL(/\/discounts\?t=free$/);
  await expect(page.getByRole('button', { name: t('Зат беру', 'Отдать вещь'), exact: true }).first()).toBeVisible();
  await page.getByRole('button', { name: t('Мәзір', 'Меню'), exact: true }).click();
  await expect(page.getByRole('dialog', { name: t('Мәзір', 'Меню') }).getByRole('button', { name: t('Жеңілдіктер', 'Скидки'), exact: true }))
    .toHaveAccessibleDescription(`${t('Жеңілдіктер', 'Скидки')} · ${t('Тегін беремін', 'Отдам даром')}`);
});

test('home starts with search, news, deadlines and classes: no hero, no greeting', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await onboard(page, lang);
  await expect(page.locator('.hero3d, .hero3d-scene, canvas')).toHaveCount(0);
  await expect(page.getByText(/Сәлем|Привет/)).toHaveCount(0);
  await expect(page.locator('main > *').first()).toHaveAttribute('id', 'homeSearch');
  await expect(page.locator('main > section').first().getByRole('heading').first()).toHaveText(t('Жаңалықтар', 'Новости'));
});

// Screenshots for docs/design/sections (E2E_SHOTS=1, 390 px projects only)
test('section screenshots', async ({ page, lang }, info) => {
  test.skip(process.env.E2E_SHOTS !== '1' || !info.project.name.endsWith('-390'), 'E2E_SHOTS=1 at 390 px only');
  await withData(page, true);
  await onboard(page, lang);
  const dir = path.resolve(process.cwd(), '../docs/design/sections');
  const shots: [string, string][] = [
    ['home', '/'], ['search', '/search'], ['jobs', '/jobs'], ['jobs-youth', '/jobs?t=youth'],
    ['internships', '/internships'], ['internships-mobility', '/internships?t=mobility'], ['internships-grants', '/internships?t=grants'],
    ['events', '/events'], ['events-contests', '/events?t=contests'], ['courses', '/courses'], ['discounts', '/discounts'], ['discounts-free', '/discounts?t=free'],
  ];
  for (const [name, url] of shots) {
    await page.goto(url);
    await expect(page.locator('main')).not.toBeEmpty();
    await page.waitForTimeout(300);
    await page.screenshot({ path: path.join(dir, `${name}-${lang}.png`) });
  }
  await page.goto('/');
  await page.getByRole('button', { name: label(lang, 'Мәзір', 'Меню'), exact: true }).click();
  await page.waitForTimeout(300);
  await page.screenshot({ path: path.join(dir, `menu-${lang}.png`) });
});
