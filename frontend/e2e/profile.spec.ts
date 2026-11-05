import { test, expect, onboard, deviceState, fits, label, DEMO_TEXT } from './fixtures';
import { defaults } from '../src/lib/defaults';
import { experiencedJob, job } from './content';

test('a device that kept the removed sample profile and sample events starts clean, without errors', async ({ page, lang }) => {
  const old = {
    ...defaults(lang), onboarded: true,
    profile: { ...defaults(lang).profile, name: lang === 'ru' ? 'Куралай' : 'Құралай', spec: 'econ', course: 2, gpa: 3.13, demo: true },
    events: { e1: { code: 'EV-E1-1234' } }, hideDemoDeck: false,
    decks: [{ id: 'deck-demo', name: 'Микроэкономика (үлгі)', cards: [] }],
    calendarPins: [{ ref: 'event:e1', kind: 'event', title: { kk: 'Мансап жәрмеңкесі', ru: 'Ярмарка карьеры' }, date: '2026-11-07', time: '11:00', prio: 'mid', done: 0 }],
  };
  await page.addInitScript(s => { if (!sessionStorage.getItem('seeded')) { localStorage.setItem('studenthub.v1', JSON.stringify(s)); sessionStorage.setItem('seeded', '1'); } }, old);
  await page.goto('/');
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible(); // the sample profile was never theirs: onboarding again
  await expect(page.locator('body')).not.toContainText(/Құралай|Куралай/);
  await expect(dialog.getByRole('button', { name: label(lang, 'Үлгі профильмен қарау', 'Посмотреть с примером профиля') })).toHaveCount(0);
  await page.keyboard.press('Escape');
  for (const path of ['/calendar', '/cards', '/profile', '/mine', '/notifications']) {
    await page.goto(path);
    await expect(page.locator('#main')).toBeVisible();
    await expect(page.locator('#main')).not.toContainText(/Құралай|Куралай|Мансап жәрмеңкесі|Ярмарка карьеры/);
    await expect(page.locator('#main')).not.toContainText(DEMO_TEXT);
  }
  // an old link to a sample event lands on the events list
  await page.goto('/event/e1');
  await expect(page).toHaveURL(/\/events$/);
});

test('legacy storage migrates without losing personal data; GPA and phone can be cleared', async ({ page, lang }) => {
  const stored = { ...defaults(lang), onboarded: true, profile: { ...defaults(lang).profile, name: 'Ескі Профиль', spec: 'econ', course: 2, gpa: 3.1, credits: 60, en: 'B1', city: 'astana', skills: ['excel', 'comm'], phone: '+77001234567' }, saved: { existing: 42 }, fav: { old: 24 }, gpaPlan: [{ n: 'Custom', cr: 3, g: 'A' }] };
  await page.addInitScript(s => {
    if (!localStorage.getItem('studenthub.v1')) localStorage.setItem('studygo.v1', JSON.stringify(s));
  }, stored);
  await page.goto('/edit');
  await expect(page.locator('#fName')).toHaveValue(stored.profile.name);
  expect(await deviceState(page)).toEqual(stored);
  expect(await page.evaluate(() => localStorage.getItem('studygo.v1'))).toBeNull();
  await page.locator('#fGpa').fill('');
  await page.locator('#fPhone').fill('');
  await page.getByRole('button', { name: label(lang, 'Сақтау', 'Сохранить'), exact: true }).click();
  await page.goto('/edit');
  await expect(page.locator('#fGpa')).toHaveValue('');
  await expect(page.locator('#fPhone')).toHaveValue('');
  expect(await deviceState(page)).toMatchObject({ profile: { gpa: null, phone: '', name: stored.profile.name }, saved: stored.saved, fav: stored.fav, gpaPlan: stored.gpaPlan });
  for (const path of ['/profile', '/gpa', '/resume', '/discounts']) {
    await page.goto(path);
    await expect(page.locator('body')).not.toContainText(/NaN|undefined|null/);
    await fits(page);
    await page.screenshot({ path: test.info().outputPath(`${path.replaceAll('/', '-')}.png`), fullPage: true });
  }
});

test('course default and explicit no-experience choice survive profile changes and reloads', async ({ page, lang }) => {
  await onboard(page, lang, '3');
  await page.goto('/jobs');
  await expect(page.getByRole('button', { name: new RegExp(experiencedJob.title[lang]) })).toBeVisible();
  await page.goto('/edit');
  await page.locator('#fCourse').selectOption('1');
  await page.getByRole('button', { name: label(lang, 'Сақтау', 'Сохранить'), exact: true }).click();
  await page.goto('/jobs');
  await expect(page.getByRole('button', { name: new RegExp(job.title[lang]) })).toBeVisible();
  await expect(page.getByRole('button', { name: new RegExp(experiencedJob.title[lang]) })).toHaveCount(0);
  await page.getByRole('button', { name: new RegExp(label(lang, '^Сүзгіні алып тастау:', '^Убрать фильтр:')) }).click();
  await page.reload();
  await expect(page.getByRole('button', { name: new RegExp(experiencedJob.title[lang]) })).toBeVisible();
  expect((await deviceState(page)).jobNoExperience).toBe(false);
});

test('320 px: onboarding, cards, profile form and GPA fit without horizontal scroll', async ({ page, lang }) => {
  await page.setViewportSize({ width: 320, height: 740 });
  await onboard(page, lang);
  for (const path of ['/jobs', '/discounts', '/edit', '/gpa', '/opportunity/e2e-intern']) {
    await page.goto(path);
    await expect(page.locator('#main')).toBeVisible();
    await fits(page);
    if (path === '/jobs') {
      expect(await page.locator('.row .badge').evaluateAll(badges => badges.every(b => {
        const bounds = b.getBoundingClientRect();
        const row = b.closest('.row-main')!.getBoundingClientRect();
        return bounds.right <= row.right + 1 && bounds.left >= row.left - 1;
      }))).toBe(true);
      await page.locator('#sList .sr').first().screenshot({ path: test.info().outputPath('job-card.png') });
    }
    if (path === '/discounts') await page.locator('.deals .fav-card').first().screenshot({ path: test.info().outputPath('discount-card.png') });
    await page.screenshot({ path: test.info().outputPath(`${path.replaceAll('/', '-')}.png`), fullPage: true });
  }
});
