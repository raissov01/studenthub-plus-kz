import { mkdirSync } from 'node:fs';
import { test, expect, onboard, fits, label, deviceState, withContent, DEMO_TEXT } from './fixtures';

// The site carries no sample content: a fresh student with an empty feed sees honest empty states with a next step,
// and their own timetable feeds the home «next class» card, the calendar and the subjects.
test.use({ timezoneId: 'Asia/Almaty' });

const SHOTS = '../docs/design/empty';
const EMPTY = { jobs: [], opportunities: [], discounts: [], events: [], places: [] };

test('fresh student, empty feed: empty states with a next step, no demo text on any main screen', async ({ page, lang }) => {
  test.setTimeout(180_000); // walks through every main screen
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const shoot = test.info().project.name.endsWith('-390');
  if (shoot) mkdirSync(SHOTS, { recursive: true });
  const snap = async (name: string) => { if (shoot) await page.screenshot({ path: `${SHOTS}/${name}-${lang}.png` }); };
  await withContent(page, EMPTY);
  await onboard(page, lang);
  const clean = async () => {
    await expect(page.locator('#main')).toBeVisible();
    await expect(page.locator('body')).not.toContainText(DEMO_TEXT);
    await expect(page.locator('body')).not.toContainText(/Құралай|Куралай|Микроэкономика|NaN|undefined/);
    await fits(page);
  };

  // home: no news carousel, no placeholder schedule — the «add your timetable» step instead
  await page.goto('/');
  await expect(page.locator('#addTimetable')).toBeVisible();
  await expect(page.locator('#homeNews')).toHaveCount(0);
  await expect(page.locator('#nextClass')).toHaveCount(0);
  await expect(page.locator('#scheduleIcs')).toHaveCount(0);
  await clean();
  await snap('home');

  // search: only the real courses; each empty category says so and offers the next step
  await page.goto('/search');
  await expect(page.locator('#sList')).toContainText('CS50');
  await clean();
  await snap('search');
  await page.goto('/jobs');
  await expect(page.locator('#sList')).toContainText(t('Әзірге вакансия жоқ — жаңалары жарияланғанда хабарлаймыз.', 'Вакансий пока нет — сообщим, когда появятся новые.'));
  await expect(page.locator('#notifySignIn')).toBeVisible();
  await clean();
  await snap('search-jobs');
  for (const [path, kk, ru] of [
    ['/internships', 'Әзірге тағылымдама жоқ', 'Стажировок пока нет'],
    ['/events', 'Әзірге іс-шара жоқ', 'Мероприятий пока нет'],
    ['/discounts', 'Әзірге серіктес жеңілдігі жоқ', 'Скидок партнёров пока нет'],
  ]) {
    await page.goto(path);
    await expect(page.locator('#sList [data-empty]')).toContainText(t(kk, ru));
    await clean();
  }
  // the notify step leads to the account settings
  await page.goto('/jobs');
  await page.locator('#notifySignIn').click();
  await expect(page).toHaveURL(/\/settings$/);

  // calendar: no classes layer to toggle until there is a timetable
  await page.goto('/calendar');
  await expect(page.locator('#calAddTimetable')).toBeVisible();
  await expect(page.locator('.cal-item[data-kind="class"]')).toHaveCount(0);
  await page.getByRole('button', { name: t('Апта', 'Неделя'), exact: true }).click();
  await expect(page.locator('.cal-item[data-kind="class"]')).toHaveCount(0);
  await page.getByRole('button', { name: t('Ай', 'Месяц'), exact: true }).click();
  await clean();
  await snap('calendar');

  // profile and its pages
  await page.goto('/profile');
  await clean();
  await snap('profile');
  await page.goto('/gpa');
  await expect(page.locator('#gpaAdd')).toBeVisible();
  await clean();
  await page.goto('/pay');
  await expect(page.locator('#main')).toContainText(t('Оқу ақысының деректері әзірге қосылмаған', 'Данные об оплате обучения пока не подключены'));
  await expect(page.locator('#main')).not.toContainText(/₸|\d00 000/);
  await expect(page.locator('#payDeadline')).toBeVisible();
  await clean();
  for (const path of ['/notifications', '/subjects', '/cards', '/portfolio', '/mine', '/courses', '/services', '/docs', '/admission']) {
    await page.goto(path);
    await clean();
  }
  await page.goto('/subjects');
  await expect(page.locator('#subjAddTimetable')).toBeVisible();
  await expect(page.locator('#subjectList .row')).toHaveCount(0);
  await page.goto('/cards');
  await expect(page.locator('#main')).toContainText(t('Әзірге жиын жоқ', 'Пока нет наборов'));
});

test('own timetable: add, validate, edit, delete; it feeds the next-class card, the calendar and the subjects', async ({ page, lang }) => {
  test.setTimeout(120_000);
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  // a Wednesday, 08:00 in Astana
  const wed = new Date(Date.now() + 86400_000);
  while (wed.getUTCDay() !== 3) wed.setUTCDate(wed.getUTCDate() + 1);
  wed.setUTCHours(3, 0, 0, 0);
  await page.clock.setFixedTime(wed);
  await withContent(page, EMPTY);
  await onboard(page, lang);

  await page.locator('#addTimetable').getByRole('button', { name: t('Кестені қосу', 'Добавить расписание') }).click();
  await expect(page).toHaveURL(/\/timetable$/);
  await expect(page.locator('#title')).toHaveText(t('Сабақ кестесі', 'Расписание занятий'));
  const form = page.locator('#timetableForm');
  // validation: a name and a start time are needed
  await form.getByRole('button', { name: t('Қосу', 'Добавить'), exact: true }).click();
  await expect(page.locator('#ttErr')).toBeVisible();
  await page.locator('#ttName').fill('Физика');
  await page.locator('#ttDay').selectOption('2'); // Wednesday
  await page.locator('#ttStart').fill('09:00');
  await page.locator('#ttEnd').fill('08:30');
  await form.getByRole('button', { name: t('Қосу', 'Добавить'), exact: true }).click();
  await expect(page.locator('#ttErr')).toBeVisible();
  await page.locator('#ttEnd').fill('09:50');
  await page.locator('#ttRoom').fill('4-214');
  await page.locator('#ttKind').fill(t('Дәріс', 'Лекция'));
  await form.getByRole('button', { name: t('Қосу', 'Добавить'), exact: true }).click();
  await expect(page.locator('.toast')).toContainText(t('Сабақ кестеге қосылды', 'Пара добавлена в расписание'));
  await expect(page.locator('#ttName')).toHaveValue('');
  await expect(page.locator('#ttDay')).toHaveValue('2'); // the day stays for the next class of that day
  await page.locator('#ttName').fill('Химия');
  await page.locator('#ttDay').selectOption('3'); // Thursday
  await page.locator('#ttStart').fill('10:00');
  await form.getByRole('button', { name: t('Қосу', 'Добавить'), exact: true }).click();
  const list = page.locator('#timetableList');
  await expect(list.locator('.tt-row')).toHaveCount(2);
  await expect(list.locator('.tt-row', { hasText: 'Химия' })).toContainText('10:50'); // end defaults to 50 minutes
  await fits(page);
  expect((await deviceState(page)).timetable).toMatchObject([
    { name: 'Физика', wd: 2, t: '09:00', end: '09:50', room: '4-214' },
    { name: 'Химия', wd: 3, t: '10:00', end: '10:50', room: '' },
  ]);
  // edit the room
  await list.getByRole('button', { name: t('Өзгерту: ', 'Изменить: ') + 'Физика' }).click();
  await expect(page.locator('#ttName')).toHaveValue('Физика');
  await page.locator('#ttRoom').fill('4-305');
  await form.getByRole('button', { name: t('Сақтау', 'Сохранить'), exact: true }).click();
  await expect(list.locator('.tt-row', { hasText: 'Физика' })).toContainText('4-305');
  if (test.info().project.name.endsWith('-390')) await page.screenshot({ path: `${SHOTS}/timetable-${lang}.png`, fullPage: true });

  // home: the next-class card with the room on the map and the subject page
  await page.goto('/');
  const card = page.locator('#nextClass');
  await expect(card).toContainText('Физика');
  await expect(card).toContainText('09:00');
  await expect(card).toContainText(t('Келесі сабақ', 'Следующая пара'));
  await expect(page.locator('#addTimetable')).toHaveCount(0);
  await expect(card.getByRole('button', { name: t('Картадан жолды көру', 'Показать путь на карте') })).toBeVisible();
  await page.locator('#ncSubject').click();
  await expect(page).toHaveURL(/\/subjects\?id=s[0-9a-z]+$/);
  await expect(page.getByRole('heading', { name: 'Физика', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: t('4-305 аудитория — картада', 'Аудитория 4-305 на карте') })).toBeVisible();
  await page.goto('/subjects');
  await expect(page.locator('#subjectList .row')).toHaveCount(2);

  // calendar: the week shows both classes on their own days; the month layer is a chip
  await page.goto('/calendar');
  await page.getByRole('button', { name: t('Апта', 'Неделя'), exact: true }).click();
  const classes = page.locator('.cal-item[data-kind="class"]');
  await expect(classes).toHaveCount(2);
  await expect(classes.filter({ hasText: 'Физика' })).toContainText('4-305');
  await expect(page.getByRole('button', { name: t('Сабақтар', 'Занятия'), exact: true })).toHaveAttribute('aria-pressed', 'true');
  await page.getByRole('button', { name: t('Ай', 'Месяц'), exact: true }).click();
  await page.getByRole('button', { name: t('Сабақтар', 'Занятия'), exact: true }).click();
  await expect(page.locator('.cal-day .cal-item[data-kind="class"]', { hasText: 'Физика' })).toBeVisible();
  await expect(page.locator('body')).not.toContainText(DEMO_TEXT);

  // delete one: it leaves the calendar too
  await page.goto('/timetable');
  await page.getByRole('button', { name: t('Өшіру: ', 'Удалить: ') + 'Химия' }).click();
  await page.locator('.sheet').getByRole('button', { name: t('Өшіру', 'Удалить'), exact: true }).click();
  await expect(page.locator('#timetableList .tt-row')).toHaveCount(1);
  expect((await deviceState(page)).timetable).toHaveLength(1);
});
