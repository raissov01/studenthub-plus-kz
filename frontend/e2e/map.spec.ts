import { test, expect, onboard, fits, label } from './fixtures';

test('campus map lists university buildings elsewhere in Astana, finds them by search, links to the right point', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await onboard(page, lang);
  await page.goto('/map');

  const section = page.locator('#citySites');
  await expect(section.getByRole('heading', { name: t('Қаладағы басқа ғимараттар', 'Другие здания в городе') })).toBeVisible();
  for (const name of [t('№1А жатақхана', 'Общежитие №1А'), t('№6 жатақхана', 'Общежитие №6'), t('№8 жатақхана', 'Общежитие №8'),
    t('Пәтер үлгісіндегі №14 жатақхана', 'Общежитие квартирного типа №14'), t('Оқу шаруашылығы (Учхоз): №8 корпус, ветеринариялық клиника', 'Учхоз: корпус №8, ветеринарная клиника')]) {
    await expect(section.getByRole('article', { name, exact: true })).toBeVisible();
  }
  await expect(section.getByRole('article')).toHaveCount(6);
  await fits(page);

  // search by street finds the off-campus dormitory, and the campus buildings drop out
  await page.locator('#mapQ').fill('Сарыарка');
  const found = page.locator('#citySites').getByRole('article');
  await expect(found).toHaveCount(1);
  const dorm = found.first();
  await expect(dorm).toHaveAccessibleName(t('№1А жатақхана', 'Общежитие №1А'));
  await expect(dorm).toContainText(t('Сарыарқа даңғылы, 38', 'пр. Сарыарка, 38'));
  await expect(page.locator('#mapRes .bcard')).toHaveCount(0);

  const gis = dorm.getByRole('link', { name: new RegExp(t('2GIS-те ашу', 'Открыть в 2GIS')) });
  await expect(gis).toHaveAttribute('href', 'https://2gis.kz/astana/geo/71.407224,51.174073?m=71.407224,51.174073/17');
  await expect(gis).toHaveAttribute('target', '_blank');
  await expect(dorm.getByRole('link', { name: new RegExp(t('Google Maps-та ашу', 'Открыть в Google Maps')) }))
    .toHaveAttribute('href', 'https://www.google.com/maps/search/?api=1&query=51.174073,71.407224');
  await fits(page);

  // a faculty name finds the vet faculty buildings out in Интернациональный
  await page.locator('#mapQ').fill(t('ветеринария', 'ветеринар'));
  await expect(page.locator('#citySites').getByRole('article', { name: t('Оқу шаруашылығы (Учхоз): №8 корпус, ветеринариялық клиника', 'Учхоз: корпус №8, ветеринарная клиника') })).toBeVisible();

  // keyboard: the search field then the first map link are reachable
  await page.locator('#mapQ').fill('Армандастар');
  await expect(page.locator('#citySites').getByRole('article')).toHaveCount(3);
  await page.locator('#mapQ').focus();
  let reached = false;
  for (let i = 0; i < 40 && !reached; i++) {
    await page.keyboard.press('Tab');
    reached = await page.evaluate(() => !!document.activeElement?.closest('#citySites a'));
  }
  expect(reached).toBe(true);

  // nearby tab shows the same list
  await page.goto('/map/near');
  await expect(page.locator('#citySites').getByRole('article')).toHaveCount(6);
});
