import { test, expect, onboard, deviceState, fits, label } from './fixtures';
import { defaults, demoProfile } from '../lib/defaults';
import { experiencedJob, job } from './content';

test('demo profile appears only after explicit selection', async ({ page, lang }) => {
  await page.goto('/');
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect(page.locator('body')).not.toContainText(/Құралай|Куралай/);
  await expect(page.locator('html')).toHaveAttribute('lang', 'kk');
  if (lang === 'ru') await dialog.getByRole('tab', { name: 'Русский', exact: true }).click();
  await dialog.getByRole('button', { name: label(lang, 'Үлгі профильмен қарау', 'Посмотреть с примером профиля'), exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect((await deviceState(page)).profile).toEqual(demoProfile(lang));
  await page.reload();
  await expect(page.locator('body')).toContainText(lang === 'kk' ? 'Құралай' : 'Куралай');
});

test('legacy storage migrates without losing personal data; GPA and phone can be cleared', async ({ page, lang }) => {
  const stored = { ...defaults(lang), onboarded: true, profile: { ...demoProfile(lang), demo: false, name: 'Ескі Профиль', phone: '+77001234567' }, saved: { existing: 42 }, fav: { old: 24 }, gpaPlan: [{ n: 'Custom', cr: 3, g: 'A' }] };
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
