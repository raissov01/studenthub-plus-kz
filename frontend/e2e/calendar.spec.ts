import { test, expect, onboard, fits, label, deviceState, seedTimetable, withContent } from './fixtures';
import { event, EVENT_DAY, OPP_DEADLINE } from './content';

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

  // open from the home card (the calendar is a bottom tab, not a hamburger-menu item)
  await card.getByRole('button', { name: t('Күнтізбе', 'Календарь'), exact: true }).click();
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

  // add an own event (the «Іс-шара қосу» button under the grid)
  const mine = plusDays(today, 3);
  const myTitle = t('Курстық жұмысты тапсыру', 'Сдать курсовую');
  await page.getByRole('button', { name: t('Іс-шара қосу', 'Добавить событие'), exact: true }).click();
  const sheet = page.locator('.sheet');
  await sheet.getByLabel(t('Атауы', 'Название')).fill(myTitle);
  await sheet.locator('#evDate').fill(mine);
  await sheet.locator('#evStart').fill('10:30');
  await sheet.locator('#evNote').fill(t('Кафедраға', 'На кафедру'));
  await fits(page);
  await sheet.getByRole('button', { name: t('Қосу', 'Добавить'), exact: true }).click();
  await expect(sheet).toHaveCount(0);
  await expect(day(mine)).toHaveAttribute('aria-pressed', 'true');
  await expect(day(mine)).toHaveAttribute('aria-label', new RegExp(t('1 жазба', '1 запись')));
  await expect(page.locator('.cal-day .cal-item', { hasText: myTitle })).toBeVisible();
  expect((await deviceState(page)).deadlines).toMatchObject([{ title: myTitle, date: mine, time: '10:30', color: 1 }]);

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

/** the computed colour of a CSS value such as 'var(--ev-7)' in the current theme */
const cssColor = (page: import('@playwright/test').Page, value: string) => page.evaluate(v => {
  const i = document.createElement('i');
  i.style.color = v;
  document.body.append(i);
  const c = getComputedStyle(i).color;
  i.remove();
  return c;
}, value);

test('own events: a chosen colour and time, titles inside the month cell with «+N», day timeline in time order, reload, edit, delete', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await onboard(page, lang);
  await page.goto('/calendar');
  const d = plusDays(almatyToday(), 2);
  const grid = page.getByRole('grid');
  const cell = grid.locator(`button[data-ymd="${d}"]`);
  const sheet = page.locator('.sheet');
  const add = async (title: string, start: string, end: string, colour?: [string, string], place = '') => {
    await page.getByRole('button', { name: t('Іс-шара қосу', 'Добавить событие'), exact: true }).click();
    await sheet.getByLabel(t('Атауы', 'Название')).fill(title);
    await sheet.locator('#evDate').fill(d);
    if (start) await sheet.locator('#evStart').fill(start);
    if (end) await sheet.locator('#evEnd').fill(end);
    if (place) await sheet.locator('#evPlace').fill(place);
    if (colour) await sheet.getByRole('radio', { name: t(...colour), exact: true }).check();
    await sheet.getByRole('button', { name: t('Қосу', 'Добавить'), exact: true }).click();
    await expect(sheet).toHaveCount(0);
  };

  // the sheet: eight colour swatches as radio buttons with names; the end must be after the start
  await page.getByRole('button', { name: t('Іс-шара қосу', 'Добавить событие'), exact: true }).click();
  await expect(sheet.getByRole('group', { name: t('Түсі', 'Цвет') }).getByRole('radio')).toHaveCount(8);
  await expect(sheet.getByRole('radio', { name: t('Көк', 'Синий'), exact: true })).toBeChecked();
  await sheet.getByLabel(t('Атауы', 'Название')).fill('X');
  await sheet.locator('#evStart').fill('12:00');
  await sheet.locator('#evEnd').fill('11:00');
  await fits(page);
  await sheet.getByRole('button', { name: t('Қосу', 'Добавить'), exact: true }).click();
  await expect(sheet.getByRole('alert')).toContainText(t('Аяқталуы басталуынан кейін', 'Окончание должно быть позже'));
  await page.keyboard.press('Escape');
  await expect(sheet).toHaveCount(0);

  const meet = t('Топпен кездесу', 'Встреча с группой');
  await add(meet, '14:00', '15:30', ['Жасыл', 'Зелёный'], t('Кітапхана', 'Библиотека'));
  // the title is inside the cell, in the chosen colour; the day button names it
  const chip = cell.locator('.cal-chip', { hasText: meet });
  await expect(chip).toBeVisible();
  await expect(chip).toHaveCSS('background-color', await cssColor(page, 'var(--ev-7-soft)'));
  await expect(chip).toHaveCSS('border-left-color', await cssColor(page, 'var(--ev-7)'));
  await expect(cell).toHaveAttribute('aria-label', new RegExp(`14:00 ${meet}`));
  expect((await deviceState(page)).deadlines).toMatchObject([{ title: meet, date: d, time: '14:00', end: '15:30', color: 7 }]);

  // an earlier one and an all-day one: the timeline is all-day first, then by time
  const run = t('Таңғы жүгіру', 'Утренняя пробежка');
  const bday = t('Досымның туған күні', 'День рождения друга');
  await add(run, '08:00', '', ['Қызғылт сары', 'Оранжевый']);
  await add(bday, '', '', ['Күлгін', 'Фиолетовый']);
  const rows = page.locator('.cal-day .cal-item');
  await expect(rows.locator('.row-title')).toHaveText([bday, run, meet]);
  await expect(rows.nth(0).locator('.cal-time')).toHaveText(t('Күні бойы', 'Весь день'));
  await expect(rows.nth(2).locator('.cal-time')).toHaveText('14:0015:30');
  await expect(rows.nth(2)).toContainText(t('Кітапхана', 'Библиотека'));
  await expect(rows.nth(2).locator('.cal-tl-main')).toHaveCSS('border-left-color', await cssColor(page, 'var(--ev-7)'));
  await expect(cell.locator('.cal-chip')).toHaveCount(3);
  await fits(page);

  // a fourth: the cell shows two titles and «+2»
  await add(t('Кітап оқу', 'Чтение'), '20:00', '');
  await expect(cell.locator('.cal-chip')).toHaveCount(2);
  await expect(cell.locator('.cal-more')).toHaveText('+2');
  await expect(cell).toHaveAttribute('aria-label', new RegExp(t('4 жазба', '4 записи')));
  await fits(page);

  // survives a reload
  await page.reload();
  await grid.locator(`button[data-ymd="${d}"]`).click();
  await expect(rows).toHaveCount(4);
  await expect(rows.locator('.row-title')).toHaveText([bday, run, meet, t('Кітап оқу', 'Чтение')]);

  // edit: rename, move it to 07:00 and change the colour → it moves to the top of the timed ones
  await rows.filter({ hasText: meet }).getByRole('button', { name: t('Өзгерту: ', 'Изменить: ') + meet }).click();
  await expect(sheet.getByLabel(t('Атауы', 'Название'))).toHaveValue(meet);
  await expect(sheet.getByRole('radio', { name: t('Жасыл', 'Зелёный'), exact: true })).toBeChecked();
  const meet2 = meet + ' 2';
  await sheet.getByLabel(t('Атауы', 'Название')).fill(meet2);
  await sheet.locator('#evStart').fill('07:00');
  await sheet.locator('#evEnd').fill('07:45');
  await sheet.getByRole('radio', { name: t('Көк', 'Синий'), exact: true }).check();
  await sheet.getByRole('button', { name: t('Сақтау', 'Сохранить'), exact: true }).click();
  await expect(sheet).toHaveCount(0);
  await expect(rows.locator('.row-title')).toHaveText([bday, meet2, run, t('Кітап оқу', 'Чтение')]);
  expect((await deviceState(page)).deadlines.find((x: { title: string }) => x.title === meet2)).toMatchObject({ time: '07:00', end: '07:45', color: 1 });

  // delete from the row
  await rows.filter({ hasText: meet2 }).getByRole('button', { name: t('Жою: ', 'Удалить: ') + meet2 }).click();
  await sheet.getByRole('button', { name: t('Иә, жою', 'Да, удалить') }).click();
  await expect(rows).toHaveCount(3);
  await expect(rows.filter({ hasText: meet2 })).toHaveCount(0);
  expect((await deviceState(page)).deadlines).toHaveLength(3);
  // own events are not to-do tasks
  await page.getByRole('button', { name: t('Тапсырмалар', 'Задачи'), exact: true }).click();
  await expect(page.locator('.task', { hasText: run })).toHaveCount(0);
});

test('year view: 12 months with marked days, a month opens in the month view; the month/year picker jumps', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await onboard(page, lang);
  await page.goto('/calendar');
  const y = +almatyToday().slice(0, 4);
  const title = page.locator('#calTitle');
  const monthTitle = (yy: number, m: number) => `${MONTHS[lang][m]} ${yy}`;

  await page.getByRole('button', { name: t('Жыл', 'Год'), exact: true }).click();
  await expect(page.getByRole('button', { name: t('Жыл', 'Год'), exact: true })).toHaveAttribute('aria-pressed', 'true');
  await expect(title).toHaveText(String(y));
  const year = page.getByRole('list', { name: String(y) });
  await expect(year.getByRole('listitem')).toHaveCount(12);
  // the seeded opportunity's day is marked in its month (when it falls in this year)
  if (+OPP_DEADLINE.slice(0, 4) === y) {
    const m = +OPP_DEADLINE.slice(5, 7) - 1;
    await expect(year.getByRole('button', { name: new RegExp(`^${monthTitle(y, m)}, ${t('1 жазба', '1 запись')}`) })).toBeVisible();
    await expect(year.getByRole('listitem').nth(m).locator('.cal-mini-d.has')).toHaveText(String(+OPP_DEADLINE.slice(8, 10)));
  }
  await fits(page);

  // year arrows
  await page.getByRole('button', { name: t('Келесі жыл', 'Следующий год') }).click();
  await expect(title).toHaveText(String(y + 1));
  await page.getByRole('button', { name: t('Алдыңғы жыл', 'Предыдущий год') }).click();
  await expect(title).toHaveText(String(y));

  // a month opens in the month view
  await page.getByRole('button', { name: new RegExp(`^${monthTitle(y, 3)},`) }).click();
  await expect(page.getByRole('button', { name: t('Ай', 'Месяц'), exact: true })).toHaveAttribute('aria-pressed', 'true');
  await expect(title).toHaveText(monthTitle(y, 3));
  await expect(page.getByRole('grid').locator(`button[data-ymd="${y}-04-01"]`)).toHaveAttribute('aria-pressed', 'true');

  // the picker: a popover under the month title
  await title.getByRole('button').click();
  const picker = page.getByRole('group', { name: t('Айды таңдау', 'Выбрать месяц') });
  await picker.getByRole('combobox', { name: t('Ай', 'Месяц'), exact: true }).selectOption({ label: MONTHS[lang][1] });
  await expect(title).toHaveText(monthTitle(y, 1));
  await picker.getByRole('combobox', { name: t('Жыл', 'Год'), exact: true }).selectOption(String(y + 1));
  await expect(title).toHaveText(monthTitle(y + 1, 1));
  await expect(page.getByRole('grid').locator(`button[data-ymd="${y + 1}-02-01"]`)).toHaveAttribute('aria-pressed', 'true');
  await fits(page);
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

test('pin an event into the calendar from the events list: toast «Ашу», kind colour and label, saved-only filter, unpin', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const evTitle = t(event.title_kk, event.title_ru!);
  await withContent(page, { events: [event] });
  await onboard(page, lang);
  await page.goto('/events');
  const pin = page.getByRole('button', { name: t('Күнтізбеге қосу: ', 'Добавить в календарь: ') + evTitle });
  await expect(pin).toHaveAttribute('aria-pressed', 'false');
  await fits(page);
  await pin.click();
  await expect(pin).toHaveAttribute('aria-pressed', 'true');
  const pins = (await deviceState(page)).calendarPins;
  expect(pins).toMatchObject([{ ref: 'event:501', kind: 'event', date: EVENT_DAY, time: '11:00', prio: 'mid', done: 0, title: { kk: event.title_kk, ru: event.title_ru } }]);
  const date: string = pins[0].date;

  // «Ашу» in the toast jumps to that day
  const toast = page.locator('.toast');
  await expect(toast).toContainText(t('Күнтізбеге қосылды', 'Добавлено в календарь'));
  await toast.getByRole('button', { name: t('Ашу', 'Открыть') }).click();
  await expect(page).toHaveURL(/\/calendar$/);
  await expect(page.getByRole('grid').locator(`button[data-ymd="${date}"]`)).toHaveAttribute('aria-pressed', 'true');
  await expect(page.getByRole('grid').locator(`button[data-ymd="${date}"]`)).toHaveAttribute('aria-label', new RegExp(t('Іс-шара', 'Мероприятие')));
  const row = page.locator('.cal-day .cal-item', { hasText: evTitle });
  await expect(row).toHaveAttribute('data-kind', 'event');
  await expect(row.locator('.cal-kind-tag')).toHaveText(t('Іс-шара', 'Мероприятие'));
  await expect(row.locator('.cal-tl-main')).toHaveCSS('border-left-color', await cssColor(page, 'var(--cal-event)'));
  await expect(page.getByRole('grid').locator(`button[data-ymd="${date}"] .cal-chip`, { hasText: evTitle })).toBeVisible();
  await expect(row).toContainText(t('Күнтізбеде', 'В календаре'));
  await expect(page.locator('.cal-legend')).toContainText(t('Іс-шара', 'Мероприятие'));
  // a pinned item stays when only saved items are shown
  await page.getByRole('button', { name: t('Сақталғандар мен менікі', 'Сохранённое и моё') }).click();
  await expect(row).toBeVisible();
  await fits(page);

  // a priority from the item's sheet
  await row.getByRole('button', { name: new RegExp(t('Өзгерту немесе күнтізбеге қосу', 'Изменить или добавить в календарь')) }).click();
  const sheet = page.locator('.sheet');
  await sheet.getByRole('radio', { name: t('Жоғары', 'Высокий') }).check();
  expect((await deviceState(page)).calendarPins[0].prio).toBe('high');
  await page.keyboard.press('Escape');
  await expect(sheet).toHaveCount(0);
  await expect(row).toHaveClass(/prio-high/);

  // unpin from the list
  await page.goto('/events');
  await pin.click();
  await expect(page.locator('.toast')).toContainText(t('Күнтізбеден алынды', 'Убрано из календаря'));
  await expect(pin).toHaveAttribute('aria-pressed', 'false');
  expect((await deviceState(page)).calendarPins).toEqual([]);
  await page.goto('/calendar');
  await page.getByRole('grid').locator(`button[data-ymd="${date}"]`).click();
  // the event itself stays on its day (it is in the feed), just no longer pinned
  await expect(row).toHaveCount(1);
  await expect(row).not.toContainText(t('Күнтізбеде', 'В календаре'));
});

test('pin from a list row: an internship card in search toggles its calendar pin', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const oppTitle = t('Зерттеу тағылымдамасы', 'Исследовательская стажировка');
  await onboard(page, lang);
  await page.goto('/internships');
  const btn = page.getByRole('button', { name: t('Күнтізбеге қосу: ', 'Добавить в календарь: ') + oppTitle });
  await expect(btn).toHaveAttribute('aria-pressed', 'false');
  await fits(page);
  await btn.click();
  await expect(btn).toHaveAttribute('aria-pressed', 'true');
  expect((await deviceState(page)).calendarPins).toMatchObject([{ ref: 'opp:e2e-intern', date: OPP_DEADLINE }]);
  await btn.click();
  await expect(btn).toHaveAttribute('aria-pressed', 'false');
  expect((await deviceState(page)).calendarPins).toEqual([]);
});

test('«Тапсырмалар»: quick-add with a date and priority, check off (also from home), done survives reload, undo', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const task = t('Зертханалық есепті жазу', 'Написать лабораторный отчёт');
  const loose = t('Кітапханаға бару', 'Сходить в библиотеку');
  await onboard(page, lang);
  await page.goto('/calendar');
  const tasksTab = page.getByRole('button', { name: t('Тапсырмалар', 'Задачи'), exact: true });
  await tasksTab.click();
  await expect(tasksTab).toHaveAttribute('aria-pressed', 'true');
  await expect(page.getByRole('grid')).toHaveCount(0);

  const input = page.getByLabel(t('Жаңа тапсырма', 'Новая задача'));
  await input.fill(task);
  await page.getByRole('group', { name: t('Мерзімі', 'Срок') }).getByRole('button', { name: t('Ертең', 'Завтра') }).click();
  await page.getByLabel(t('Маңыздылығы', 'Важность')).selectOption('high');
  await fits(page);
  await page.getByRole('button', { name: t('Қосу', 'Добавить'), exact: true }).click();
  await expect(input).toHaveValue('');
  await input.fill(loose);
  await input.press('Enter');

  const group = (kk: string, ru: string) => page.getByRole('region', { name: new RegExp('^' + t(kk, ru)) });
  const tomorrow = group('Ертең', 'Завтра');
  await expect(tomorrow.getByRole('listitem').filter({ hasText: task })).toContainText(t('Жоғары', 'Высокий'));
  await expect(group('Кейін', 'Позже').getByRole('listitem').filter({ hasText: loose })).toContainText(t('Мерзімі жоқ', 'Без срока'));
  const st = (await deviceState(page)).deadlines;
  expect(st).toMatchObject([{ title: task, date: plusDays(almatyToday(), 1), prio: 'high', done: 0 }, { title: loose, date: '', prio: 'mid', done: 0 }]);
  // sorted by date then priority: the dated, high task first; the seeded opportunity is not a task until pinned
  await expect(page.locator('.task-title').first()).toHaveText(task);
  await expect(page.locator('.task', { hasText: t('Зерттеу тағылымдамасы', 'Исследовательская стажировка') })).toHaveCount(0);
  await fits(page);

  // home: the card lists the task with a checkbox; checking it off hides it there
  await page.goto('/');
  const card = page.locator('#homeDeadlines');
  const homeCheck = card.getByRole('checkbox', { name: new RegExp(task) });
  await expect(homeCheck).not.toBeChecked();
  await homeCheck.click();
  await expect(card.getByRole('button', { name: new RegExp(task) })).toHaveCount(0);
  expect((await deviceState(page)).deadlines[0].done).toBeGreaterThan(0);

  // calendar: it is in «Орындалды», struck through; survives a reload
  await page.goto('/calendar');
  await page.getByRole('button', { name: t('Тапсырмалар', 'Задачи'), exact: true }).click();
  const done = group('Орындалды', 'Выполнено');
  const doneBox = done.getByRole('checkbox', { name: new RegExp(task) });
  await expect(doneBox).toBeChecked();
  await expect(done.locator('.task-title', { hasText: task })).toHaveCSS('text-decoration-line', 'line-through');
  await page.reload();
  await page.getByRole('button', { name: t('Тапсырмалар', 'Задачи'), exact: true }).click();
  await expect(group('Орындалды', 'Выполнено').getByRole('checkbox', { name: new RegExp(task) })).toBeChecked();

  // undo: uncheck → back under «Ертең»; check the undated one with the keyboard
  await group('Орындалды', 'Выполнено').getByRole('checkbox', { name: new RegExp(task) }).click();
  await expect(group('Ертең', 'Завтра').getByRole('checkbox', { name: new RegExp(task) })).not.toBeChecked();
  await page.getByRole('checkbox', { name: new RegExp(loose) }).focus();
  await page.keyboard.press('Space');
  await expect(group('Орындалды', 'Выполнено').getByRole('checkbox', { name: new RegExp(loose) })).toBeChecked();
  const after = (await deviceState(page)).deadlines;
  expect(after.find((d: { title: string }) => d.title === task).done).toBe(0);
  expect(after.find((d: { title: string }) => d.title === loose).done).toBeGreaterThan(0);
});

test('«Сабақтар» layer: off in the month grid by default, on in the week list, toggled by its chip', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const cls = 'Микроэкономика';
  await onboard(page, lang);
  await seedTimetable(page); // Monday–Friday, three classes a day
  await page.goto('/calendar');
  const today = almatyToday();
  let weekday = today;
  while ([0, 6].includes(new Date(weekday + 'T00:00:00Z').getUTCDay())) weekday = plusDays(weekday, 1);
  const chip = page.getByRole('button', { name: t('Сабақтар', 'Занятия'), exact: true });
  const classRows = page.locator('.cal-item[data-kind="class"]');

  // month: hidden by default
  const grid = page.getByRole('grid');
  if (weekday.slice(0, 7) !== today.slice(0, 7)) await page.getByRole('button', { name: t('Келесі ай', 'Следующий месяц') }).click();
  await grid.locator(`button[data-ymd="${weekday}"]`).click();
  await expect(chip).toHaveAttribute('aria-pressed', 'false');
  await expect(classRows).toHaveCount(0);

  // week: shown by default, three classes on each of five weekdays
  await page.getByRole('button', { name: t('Апта', 'Неделя'), exact: true }).click();
  await expect(chip).toHaveAttribute('aria-pressed', 'true');
  await expect(classRows).toHaveCount(15);
  await expect(classRows.first()).toContainText(t('Сабақ', 'Занятие'));
  await fits(page);
  await chip.click();
  await expect(classRows).toHaveCount(0);

  // month again: still off (the chip set it); turning it on shows the day's classes and their grid marker
  await page.getByRole('button', { name: t('Ай', 'Месяц'), exact: true }).click();
  await expect(chip).toHaveAttribute('aria-pressed', 'false');
  await chip.click();
  await expect(chip).toHaveAttribute('aria-pressed', 'true');
  await expect(page.locator('.cal-day .cal-item[data-kind="class"]', { hasText: cls })).toBeVisible();
  await expect(classRows).toHaveCount(3);
  await expect(grid.locator(`button[data-ymd="${weekday}"]`)).toHaveAttribute('aria-label', new RegExp(t('Сабақ', 'Занятие')));
  await expect(page.locator('.cal-legend')).toContainText(t('Сабақ', 'Занятие'));
  await fits(page);
});
