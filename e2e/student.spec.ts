import { test, expect, onboard, deviceState, fits, label } from './fixtures';
import { discount, experiencedJob, job, opportunity } from './content';
import { ITEMS } from '../features/roadmap/items';

test('eight core paths: onboarding → search → opportunity → roadmap → AI → settings → offline → sign-in', async ({ page, context, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await test.step('1. Empty onboarding works without GPA or phone', async () => {
    await onboard(page, lang);
    await fits(page);
  });
  await test.step('2. Search query, category and filter sheet', async () => {
    await page.getByRole('navigation').getByRole('button', { name: t('Іздеу', 'Поиск'), exact: true }).click();
    await page.getByRole('group', { name: t('Санат', 'Категория'), exact: true }).getByRole('button', { name: t('Жұмыс', 'Работа'), exact: true }).click();
    await expect(page.getByRole('button', { name: new RegExp(job.title[lang]) })).toBeVisible();
    await expect(page.getByRole('button', { name: new RegExp(experiencedJob.title[lang]) })).toHaveCount(0);
    await expect(page.getByText(job.apply![lang], { exact: false })).toBeVisible();
    await page.getByRole('searchbox').fill(t('кітапхана', 'библиотеки'));
    await expect(page.getByRole('button', { name: new RegExp(job.title[lang]) })).toBeVisible();
    await page.getByRole('searchbox').fill('no-such-job');
    await expect(page.getByText(t('Сәйкес вакансия жоқ. Сүзгіні азайтып көріңіз.', 'Подходящих вакансий нет. Попробуйте убрать фильтры.'))).toBeVisible();
    await page.getByRole('searchbox').fill('');
    await page.getByRole('button', { name: new RegExp(t('^Сүзгілер', '^Фильтры')) }).click();
    const dialog = page.getByRole('dialog');
    const noexp = dialog.getByRole('button', { name: t('Тәжірибесіз', 'Без опыта'), exact: true });
    await expect(noexp).toHaveAttribute('aria-pressed', 'true');
    await noexp.click();
    await dialog.getByRole('button', { name: t('Көрсету', 'Показать'), exact: true }).click();
    await expect(dialog).not.toBeVisible();
    await expect(page.getByRole('button', { name: new RegExp(experiencedJob.title[lang]) })).toBeVisible();
    await page.reload();
    await expect(page.getByRole('button', { name: new RegExp(experiencedJob.title[lang]) })).toBeVisible();
    expect((await deviceState(page)).jobNoExperience).toBe(false);
    await fits(page);
  });
  await test.step('3. Opportunity detail, unknown GPA, share and save', async () => {
    await page.getByRole('group', { name: t('Санат', 'Категория'), exact: true }).getByRole('button', { name: t('Тағылымдама', 'Стажировки'), exact: true }).click();
    await page.getByRole('button', { name: new RegExp(opportunity.title[lang]) }).click();
    await expect(page.getByRole('heading', { name: opportunity.title[lang], exact: true })).toBeVisible();
    await page.getByRole('button', { name: t('Тексеру', 'Проверить'), exact: true }).click();
    await expect(page.getByText(t('GPA көрсетілмеген (міндетті емес)', 'GPA не указан (необязательно)'))).toBeVisible();
    await context.grantPermissions(['clipboard-read', 'clipboard-write']);
    await page.getByRole('button', { name: t('Бөлісу', 'Поделиться'), exact: true }).click();
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain('/opportunity/e2e-intern?ref=share');
    await page.getByRole('button', { name: t('Сақтау және мерзімді бақылау', 'Сохранить и следить за сроком'), exact: true }).click();
    await page.reload();
    await expect(page.getByRole('button', { name: t('Сақталған', 'Сохранено'), exact: true })).toBeVisible();
    expect((await deviceState(page)).saved['e2e-intern']).toBeGreaterThan(0);
    await fits(page);
  });
  await test.step('4. Choose roadmap goals and complete a step', async () => {
    await page.goto('/roadmap');
    for (const item of ITEMS.slice(0, 3)) await page.getByRole('button', { name: item.title[lang], exact: true }).click();
    await page.getByRole('button', { name: t('Жоспарды сақтау', 'Сохранить план'), exact: true }).click();
    await page.locator('.rm-step input[type=checkbox]').first().click();
    expect(Object.values((await deviceState(page)).roadmap.done).flat()).toHaveLength(1);
    await page.reload();
    await expect(page.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '11');
    await fits(page);
  });
  await test.step('5. AI quick question receives mocked /api/chat answer', async () => {
    await page.goto('/ai');
    await page.locator('.chips .chip').first().click();
    await expect(page.locator('.msg.ai')).toContainText(t('Қысқа түйіндемеден бастап, тағылымдама таңдаңыз.', 'Начните с короткого резюме и выберите стажировку.'));
    await fits(page);
  });
  await test.step('6. Settings switches and persists language', async () => {
    await page.goto('/settings');
    await page.getByRole('tab', { name: lang === 'kk' ? 'Русский' : 'Қазақша', exact: true }).click();
    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('lang', lang === 'kk' ? 'ru' : 'kk');
    await page.getByRole('tab', { name: lang === 'kk' ? 'Қазақша' : 'Русский', exact: true }).click();
    await fits(page);
  });
  await test.step('7. Real service worker serves settings after offline reload', async () => {
    await expect.poll(() => page.evaluate(async () => (await navigator.serviceWorker.getRegistration())?.active?.state), { timeout: 20_000 }).toBe('activated');
    await expect.poll(() => page.evaluate(() => !!navigator.serviceWorker.controller)).toBe(true);
    await expect.poll(() => page.evaluate(async () => !!(await caches.match(location.origin + '/settings')))).toBe(true);
    await context.setOffline(true);
    const response = await page.reload();
    expect(response?.fromServiceWorker()).toBe(true);
    await expect(page.getByText(t('Интернет жоқ — сақталған деректер көрсетілуде', 'Нет интернета — показаны сохранённые данные'))).toBeVisible();
    await fits(page);
    await context.setOffline(false);
    await expect(page.locator('.offline-bar')).toHaveCount(0);
  });
  await test.step('8. Account sign-in sheet opens without logging in', async () => {
    await page.getByRole('button', { name: new RegExp(t('Аккаунтқа кіру', 'Войти в аккаунт')) }).click();
    await expect(page.getByRole('dialog').getByRole('heading', { name: t('Аккаунтқа кіру', 'Вход в аккаунт'), exact: true })).toBeVisible();
    await fits(page);
  });
});

test('discount conditions are visible in one line and fully readable on tap', async ({ page, lang }) => {
  await onboard(page, lang);
  await page.goto('/discounts');
  const preview = page.locator('.deals .card-detail-truncate');
  await expect(preview).toContainText(discount.cond[lang]);
  expect(await preview.evaluate(el => getComputedStyle(el).textOverflow)).toBe('ellipsis');
  await preview.click();
  await expect(page.getByRole('dialog').getByText(discount.cond[lang], { exact: true })).toBeVisible();
  await fits(page);
});
