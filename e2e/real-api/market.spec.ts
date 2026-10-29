import { test, expect, label, response, approve } from './helpers';

test('11-012: real photo upload → moderation → published listing → given', async ({ actor, lang }) => {
  const seller = await actor(`Seller ${lang}`);
  const admin = await actor(`Market admin ${lang}`, { admin: true });
  const p = seller.page;
  await p.goto('/market/new');
  await p.getByRole('checkbox', { name: label(lang, 'Ережелермен келісемін', 'Согласен(на) с правилами'), exact: true }).check();
  await response(p, '/api/market/rules/accept', () => p.getByRole('button', { name: label(lang, 'Жалғастыру', 'Продолжить'), exact: true }).click());
  // Generate a real PNG; the UI resizes it and the Go API decodes/re-encodes it as JPEG.
  const png = await p.evaluate(() => {
    const canvas = document.createElement('canvas'); canvas.width = 1800; canvas.height = 1000;
    const ctx = canvas.getContext('2d')!; ctx.fillStyle = '#24694f'; ctx.fillRect(0, 0, 1800, 1000);
    ctx.fillStyle = '#ffffff'; ctx.font = '80px sans-serif'; ctx.fillText('Campus textbook', 100, 500);
    return canvas.toDataURL('image/png').split(',')[1];
  });
  const upload = await response(p, '/api/market/photos', () => p.locator('input[type=file]').setInputFiles({ name: 'book.png', mimeType: 'image/png', buffer: Buffer.from(png, 'base64') }));
  expect(upload).toBeTruthy();
  await expect(p.locator('.mk-thumb img')).toBeVisible();
  expect(await p.locator('.mk-thumb img').evaluate((img: HTMLImageElement) => img.naturalWidth)).toBeLessThanOrEqual(1280);
  const title = `Free textbook ${lang}`;
  await p.locator('#mkTitle').fill(title);
  await p.locator('#mkCat').selectOption('books');
  await p.locator('#mkCondNew').selectOption('good');
  await p.locator('#mkDesc').fill('A clean textbook for first year students.');
  await p.locator('#mkPickup').fill('Campus library entrance');
  const listing = await response(p, '/api/market/items', () => p.getByRole('button', { name: label(lang, 'Жариялау', 'Опубликовать'), exact: true }).click());
  expect(listing.pending).toBe(true);
  await approve(admin.page, lang, title);
  await p.goto(`/market/${listing.item.id}`);
  await expect(p.getByRole('heading', { name: title, exact: true })).toBeVisible();
  const detail = await (await p.request.get(`/api/market/items/${listing.item.id}`)).json();
  expect(detail.item.status).toBe('active');
  const photo = await p.request.get(detail.item.photos[0]);
  expect(photo.ok()).toBe(true);
  expect(photo.headers()['content-type']).toContain('image/jpeg');
  await p.goto('/market/mine');
  const card = p.locator('.card').filter({ hasText: title });
  await response(p, `/api/market/items/${listing.item.id}/given`, () => card.getByRole('button', { name: label(lang, 'Берілді', 'Отдано'), exact: true }).click());
  await expect(card.locator('.badge')).toHaveText(label(lang, 'Берілді', 'Отдано'));
  await p.reload();
  await expect(p.locator('.card').filter({ hasText: title }).locator('.badge')).toHaveText(label(lang, 'Берілді', 'Отдано'));
  expect((await (await p.request.get(`/api/market/items/${listing.item.id}`)).json()).item.status).toBe('given');
});
