import { test, expect, label, response, sql } from './helpers';

// Real Go API + Postgres: check-in → XP and coins → shop redeem → admin approves with a code → the student sees it.
test('points: check-in pays once, redeem → admin approve → code and inbox notice', async ({ actor, lang, baseURL }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const admin = await actor(`Points admin ${lang}`, { admin: true });
  const student = await actor(`Points student ${lang}`);
  const [ev] = sql<{ id: number }>(`insert into events (title_kk, cat, starts_at, ends_at, university, status)
    values ($1, 'other', now() - interval '5 minutes', now() + interval '1 hour', 'POINTS', 'published') returning id`, `Points event ${lang}`);
  sql(`insert into event_regs (event_id, user_id, ticket) values ($1, $2, $3)`, ev.id, student.id, `points-${lang}-${ev.id}`);
  const checkin = () => admin.page.request.post(`/api/events/${ev.id}/checkin/manual`, { headers: { Origin: baseURL! }, data: { user_id: student.id } });
  const first = await (await checkin()).json();
  expect(first.points).toMatchObject({ xp: 30, coins: 15 });
  expect((await (await checkin()).json()).status).toBe('already');

  const s = student.page;
  await s.goto('/points');
  await expect(s.locator('#ptsCoins')).toHaveText(/^1[56]$/); // 15 for the check-in (+1 if the daily open already landed)
  const coins = Number(await s.locator('#ptsCoins').innerText());

  // admin adds a reward through the catalog form
  const a = admin.page;
  const title = `Points mug ${lang}`;
  await a.goto('/admin/points');
  await a.getByRole('tab', { name: t('Сыйлықтар', 'Награды') }).click();
  await a.getByRole('button', { name: t('Сыйлық қосу', 'Добавить награду') }).click();
  const form = a.getByRole('dialog');
  await form.locator('#rwTitleKk').fill(title);
  await form.locator('#rwTitleRu').fill(title);
  await form.locator('#rwCost').fill('10');
  await form.locator('#rwStock').fill('5');
  await response(a, '/api/admin/points/rewards', () => form.getByRole('button', { name: t('Сақтау', 'Сохранить'), exact: true }).click());

  // the student redeems it
  await s.reload();
  await s.getByRole('tab', { name: t('Дүкен', 'Магазин') }).click();
  await s.getByRole('button', { name: new RegExp(title) }).click();
  await response(s, '/api/points/redeem', () => s.getByRole('dialog').getByRole('button', { name: t('Растау', 'Подтвердить'), exact: true }).click());
  await expect(s.locator('#ptsCoins')).toHaveText(String(coins - 10));
  await expect(s.getByText(t('Қаралуда', 'На рассмотрении'))).toBeVisible();

  // the admin approves it with a code from the queue
  await a.getByRole('tab', { name: t('Кезек', 'Очередь') }).click();
  const card = a.locator('article.card').filter({ hasText: title });
  await card.getByRole('button', { name: t('Бекіту', 'Одобрить'), exact: true }).click();
  await a.getByRole('dialog').locator('#apCode').fill(`MUG-${lang.toUpperCase()}-42`);
  const approved = a.waitForResponse(r => /\/api\/admin\/points\/redemptions\/\d+\/approve$/.test(new URL(r.url()).pathname));
  await a.getByRole('dialog').getByRole('button', { name: t('Бекіту', 'Одобрить'), exact: true }).click();
  expect((await approved).ok()).toBe(true);

  // the student sees the code and an inbox notice
  await s.reload();
  await s.getByRole('tab', { name: t('Өтінімдерім', 'Мои заявки') }).click();
  await expect(s.getByText(`MUG-${lang.toUpperCase()}-42`, { exact: true })).toBeVisible();
  const notices = sql<{ n: number }>(`select count(*)::int as n from notifications where user_id = $1 and link = '/points'`, student.id);
  expect(notices[0].n).toBe(1);
  const [w] = sql<{ coins: number; ledger: number }>(`select coins, (select sum(delta)::int from coin_ledger where user_id = $1) as ledger
    from points_wallets where user_id = $1`, student.id);
  expect(w.coins).toBe(w.ledger);
});
