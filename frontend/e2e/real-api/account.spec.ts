import { test, expect, label, response, sql, widget, initData } from './helpers';

test('11-008: Telegram → device profile sync → account deletion', async ({ actor, lang }) => {
  const user = await actor(`Account ${lang}`);
  const { page } = user;
  const me = await (await page.request.get('/api/me')).json();
  expect(me.user).toMatchObject({ id: user.id, name: user.name, lang, course: 2, city: 'Астана' });
  expect(me.user.university).toBeTruthy();
  expect(me.user.faculty).toBeTruthy();
  expect(me.user).not.toHaveProperty('gpa');
  expect(me.user).not.toHaveProperty('phone');
  // The authenticated account and persisted profile survive a page reload.
  await page.reload();
  await expect(page.getByRole('button', { name: label(lang, 'Аккаунтпен синхрондау', 'Синхронизировать с аккаунтом') })).toBeVisible();
  const cookies = await page.context().cookies();
  await page.getByRole('button', { name: label(lang, 'Аккаунтты өшіру', 'Удалить аккаунт') }).click();
  await page.locator('#delConfirm').fill('DELETE');
  await response(page, '/api/me', () => page.getByRole('button', { name: label(lang, 'Біржола өшіру', 'Удалить навсегда') }).click(), 'DELETE');
  expect((await (await page.request.get('/api/me')).json()).user).toBeNull();
  expect(sql('select id from users where id=$1', user.id)).toEqual([]);
  await page.context().addCookies(cookies);
  expect((await (await page.request.get('/api/me')).json()).user).toBeNull();
  expect(await page.evaluate(() => JSON.parse(localStorage.getItem('studenthub.v1')!).profile.name)).toBe(user.name);
});

test('11-016: real HMAC required for widget and Mini App; isolated test database', async ({ page, baseURL }) => {
  const headers = { Origin: baseURL! };
  for (const data of [widget(2222222, 'Forged', 0, 'wrong-token'), widget(2222222, 'Expired', 86401)]) {
    const r = await page.request.post('/api/auth/telegram/widget', { headers, data });
    expect(r.status()).toBe(401);
  }
  const invalid = await page.request.post('/api/auth/telegram/webapp', { headers, data: { initData: initData(2222222).replace('Mini', 'Fake') } });
  expect(invalid.status()).toBe(401);
  const valid = await page.request.post('/api/auth/telegram/webapp', { headers, data: { initData: initData(2222222) } });
  expect(valid.ok()).toBe(true);
  expect((await valid.json()).token).toBeTruthy();
  expect(sql<{ current_database: string }>('select current_database()')[0].current_database).toBe(process.env.E2E_DB_NAME);
  expect((await page.request.post('/api/auth/test', { headers, data: { role: 'admin' } })).status()).toBe(404);
});
