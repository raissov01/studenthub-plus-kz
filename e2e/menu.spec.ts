import { test, expect, onboard, fits, label } from './fixtures';

test('hamburger menu: opens from the header and from «Барлығы», reaches every section, closes with Escape', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await onboard(page, lang);
  await fits(page);

  const menuBtn = page.getByRole('button', { name: t('Мәзір', 'Меню'), exact: true });
  await menuBtn.click();
  const menu = page.getByRole('dialog', { name: t('Мәзір', 'Меню') });
  await expect(menu).toBeVisible();
  await expect(menuBtn).toHaveAttribute('aria-expanded', 'true');
  for (const name of [t('Жұмыс', 'Работа'), t('Іс-шаралар', 'Мероприятия'), t('Даму жоспары', 'План развития'), t('Кампус картасы', 'Карта кампуса'), t('Сақталғандар', 'Сохранённое')]) {
    await expect(menu.getByRole('button', { name, exact: true })).toBeVisible();
  }
  await fits(page);
  await page.keyboard.press('Escape');
  await expect(menu).toHaveCount(0);
  await expect(menuBtn).toBeFocused();

  await page.getByRole('button', { name: t('Барлығы', 'Все разделы'), exact: true }).click();
  await menu.getByRole('button', { name: t('Даму жоспары', 'План развития'), exact: true }).click();
  await expect(page).toHaveURL(/\/roadmap$/);
  await expect(menu).toHaveCount(0);
});
