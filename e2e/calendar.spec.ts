import { test, expect, onboard, fits, label, deviceState } from './fixtures';
import { OPP_DEADLINE } from './content';

const almatyToday = () => new Date(Date.now() + 5 * 3600_000).toISOString().slice(0, 10);
const plusDays = (d: string, n: number) => new Date(Date.parse(d + 'T00:00:00Z') + n * 86400_000).toISOString().slice(0, 10);
const monthsBetween = (a: string, b: string) => (+b.slice(0, 4) - +a.slice(0, 4)) * 12 + (+b.slice(5, 7) - +a.slice(5, 7));
const MONTHS = {
  kk: ['Қаңтар', 'Ақпан', 'Наурыз', 'Сәуір', 'Мамыр', 'Маусым', 'Шілде', 'Тамыз', 'Қыркүйек', 'Қазан', 'Қараша', 'Желтоқсан'],
  ru: ['Январь', 'Февраль', 'Март', 'Апрель', 'Май', 'Июнь', 'Июль', 'Август', 'Сентябрь', 'Октябрь', 'Ноябрь', 'Декабрь'],
};

test('calendar: menu entry, month navigation, a seeded deadline, own deadline survives reload, export and delete', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const today = almatyToday();
  const oppTitle = t('Зерттеу тағылымдамасы', 'Исследовательская стажировка');
  const monthTitle = (ymd: string) => `${MONTHS[lang][+ymd.slice(5, 7) - 1]} ${ymd.slice(0, 4)}`;
  await onboard(page, lang);

  // home: a short «Жақын дедлайндар» card with the seeded opportunity and a link to the calendar
  const card = page.locator('#homeDeadlines');
  await expect(card.getByRole('heading', { name: t('Жақын дедлайндар', 'Ближайшие дедлайны') })).toBeVisible();
  await expect(card.getByRole('button', { name: new RegExp(oppTitle) })).toBeVisible();
  await expect(card.getByRole('button', { name: t('Күнтізбе', 'Календарь'), exact: true })).toBeVisible();

  // open from the hamburger menu
  await page.getByRole('button', { name: t('Мәзір', 'Меню'), exact: true }).click();
  await page.getByRole('dialog', { name: t('Мәзір', 'Меню') }).getByRole('button', { name: t('Күнтізбе', 'Календарь'), exact: true }).click();
  await expect(page).toHaveURL(/\/calendar$/);
  const grid = page.getByRole('grid');
  await expect(grid).toBeVisible();
  await expect(grid.getByRole('columnheader')).toHaveCount(7);
  await expect(grid.getByRole('columnheader').first()).toHaveText(t('Дс', 'Пн'));
  const day = (ymd: string) => grid.locator(`button[data-ymd="${ymd}"]`);
  await expect(day(today)).toHaveAttribute('aria-current', 'date');
  await expect(day(today)).toHaveAttribute('aria-pressed', 'true');
  await fits(page);

  // month navigation and «Бүгін»
  const title = page.locator('#calTitle');
  await expect(title).toHaveText(monthTitle(today));
  await page.getByRole('button', { name: t('Келесі ай', 'Следующий месяц') }).click();
  const nextMonth = plusDays(today.slice(0, 8) + '01', 32);
  await expect(title).toHaveText(monthTitle(nextMonth));
  await page.getByRole('button', { name: t('Алдыңғы ай', 'Предыдущий месяц') }).click();
  await page.getByRole('button', { name: t('Алдыңғы ай', 'Предыдущий месяц') }).click();
  await expect(title).not.toHaveText(monthTitle(today));
  await page.getByRole('button', { name: t('Бүгін', 'Сегодня'), exact: true }).click();
  await expect(title).toHaveText(monthTitle(today));

  // keyboard: arrows move the selection inside the grid
  await day(today).focus();
  await page.keyboard.press('ArrowRight');
  const tomorrow = plusDays(today, 1);
  await expect(day(tomorrow)).toBeFocused();
  await expect(day(tomorrow)).toHaveAttribute('aria-pressed', 'true');

  // the seeded opportunity deadline: go to its month, pick the day, see it and open it
  for (let i = 0; i < monthsBetween(tomorrow, OPP_DEADLINE); i++) await page.getByRole('button', { name: t('Келесі ай', 'Следующий месяц') }).click();
  await expect(day(OPP_DEADLINE)).toHaveAttribute('aria-label', new RegExp(t('1 жазба', '1 запись')));
  await day(OPP_DEADLINE).click();
  await expect(day(OPP_DEADLINE)).toHaveAttribute('aria-pressed', 'true');
  const item = page.locator('.cal-day .cal-item', { hasText: oppTitle });
  await expect(item).toBeVisible();
  await fits(page);
  await item.getByRole('button', { name: new RegExp(oppTitle) }).first().click();
  await expect(page).toHaveURL(/\/opportunity\/e2e-intern$/);
  await page.goBack();
  await expect(page).toHaveURL(/\/calendar$/);

  // add an own deadline
  const mine = plusDays(today, 3);
  const myTitle = t('Курстық жұмысты тапсыру', 'Сдать курсовую');
  await page.getByRole('button', { name: t('Дедлайн қосу', 'Добавить дедлайн') }).click();
  const sheet = page.locator('.sheet');
  await sheet.getByLabel(t('Атауы', 'Название')).fill(myTitle);
  await sheet.locator('#dlDate').fill(mine);
  await sheet.locator('#dlTime').fill('10:30');
  await sheet.locator('#dlNote').fill(t('Кафедраға', 'На кафедру'));
  await fits(page);
  await sheet.getByRole('button', { name: t('Қосу', 'Добавить'), exact: true }).click();
  await expect(sheet).toHaveCount(0);
  await expect(day(mine)).toHaveAttribute('aria-pressed', 'true');
  await expect(day(mine)).toHaveAttribute('aria-label', new RegExp(t('1 жазба', '1 запись')));
  await expect(page.locator('.cal-day .cal-item', { hasText: myTitle })).toBeVisible();
  expect((await deviceState(page)).deadlines).toMatchObject([{ title: myTitle, date: mine, time: '10:30' }]);

  // survives a reload
  await page.reload();
  await expect(page.getByRole('grid')).toBeVisible();
  await expect(day(mine)).toHaveAttribute('aria-label', new RegExp(t('1 жазба', '1 запись')));
  await day(mine).click();
  const myRow = page.locator('.cal-day .cal-item', { hasText: myTitle });
  await expect(myRow).toBeVisible();
  await expect(myRow).toContainText('10:30');

  // week list shows it too
  await page.getByRole('button', { name: t('Апта', 'Неделя'), exact: true }).click();
  await expect(page.locator('.cal-week .cal-item', { hasText: myTitle })).toBeVisible();
  await fits(page);
  await page.getByRole('button', { name: t('Ай', 'Месяц'), exact: true }).click();

  // export: Google Calendar link and .ics download
  await myRow.getByRole('button', { name: new RegExp(myTitle) }).first().click();
  const g = sheet.getByRole('link', { name: t('Google Calendar-ға қосу', 'Добавить в Google Calendar') });
  await expect(g).toHaveAttribute('href', new RegExp(`^https://calendar\\.google\\.com/calendar/render\\?action=TEMPLATE&text=.+&dates=${mine.replace(/-/g, '')}T103000/`));
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    sheet.getByRole('button', { name: t('.ics жүктеу', 'Скачать .ics') }).click(),
  ]);
  expect(download.suggestedFilename()).toBe(`studenthub-${mine}.ics`);

  // delete it
  await sheet.getByRole('button', { name: t('Жою', 'Удалить'), exact: true }).click();
  await sheet.getByRole('button', { name: t('Иә, жою', 'Да, удалить') }).click();
  await expect(page.locator('.cal-item', { hasText: myTitle })).toHaveCount(0);
  await expect(day(mine)).toHaveAttribute('aria-label', new RegExp(t('жазба жоқ', 'нет записей')));
  expect((await deviceState(page)).deadlines).toEqual([]);
});

test('tab bar: «Күнтізбе» replaces notifications, which open from the header bell on every root screen', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await onboard(page, lang);
  const tabs = page.getByRole('navigation', { name: 'Student Hub' });
  await expect(tabs.getByRole('button')).toHaveCount(4);
  await expect(tabs.getByRole('button', { name: t('Хабарлар', 'Уведомления') })).toHaveCount(0);
  const bell = page.locator('#mine-btn');

  await tabs.getByRole('button', { name: t('Күнтізбе', 'Календарь'), exact: true }).click();
  await expect(page).toHaveURL(/\/calendar$/);
  await expect(tabs.getByRole('button', { name: t('Күнтізбе', 'Календарь'), exact: true })).toHaveAttribute('aria-current', 'page');
  await expect(page.locator('#back')).toBeHidden();
  await expect(page.getByRole('button', { name: t('Мәзір', 'Меню'), exact: true })).toBeVisible();
  await expect(bell).toBeVisible();
  await expect(bell).toHaveAccessibleName(new RegExp('^' + t('Хабарламалар', 'Уведомления')));
  await expect(page.locator('#mine-count')).toBeVisible();
  await fits(page);

  await tabs.getByRole('button', { name: t('Іздеу', 'Поиск'), exact: true }).click();
  await expect(page).toHaveURL(/\/search$/);
  await expect(bell).toBeVisible();

  await tabs.getByRole('button', { name: t('Профиль', 'Профиль'), exact: true }).click();
  await expect(page).toHaveURL(/\/profile$/);
  await expect(bell).toHaveAccessibleName(t('Параметрлер', 'Параметры'));

  await tabs.getByRole('button', { name: t('Басты бет', 'Главная'), exact: true }).click();
  await bell.click();
  await expect(page).toHaveURL(/\/notifications$/);
  await expect(page.locator('#back')).toBeVisible();
  await page.locator('#back').click();
  await expect(page).toHaveURL(/\/$/);

  // links from the Telegram bot and elsewhere still land on /notifications
  await page.goto('/notifications');
  await expect(page.locator('#title')).toHaveText(t('Хабарламалар', 'Уведомления'));
});
