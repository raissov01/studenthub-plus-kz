import { test, expect, label, response, sql } from './helpers';

// The role-scoped console against the real API: a moderator granted (with a reason, audited) through the admin UI
// sees only the moderation panel and gets 403 from the super admin's and a shop's endpoints; a blocked student
// can read but not write; CSV exports are Excel-friendly.
test('admin console: moderator scope, 403 across scopes, block, CSV, audit', async ({ actor, lang, baseURL }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const admin = await actor(`Console admin ${lang}`, { admin: true });
  const mod = await actor(`Console moderator ${lang}`);
  const student = await actor(`Console student ${lang}`);
  const a = admin.page;

  // find the person by email, «Рөл беру» → «Модератор», with a reason
  const email = `console.mod.${lang}.${mod.id}@example.kz`;
  sql(`update users set email = $2, email_verified_at = now() where id = $1`, mod.id, email);
  await a.goto('/admin/users');
  await a.getByRole('searchbox').fill(email);
  await response(a, '/api/admin/users', () => a.getByRole('searchbox').press('Enter'), 'GET');
  await expect(a.getByText(email).filter({ visible: true }).first()).toBeVisible();
  await a.getByRole('button', { name: t('Рөл беру', 'Выдать роль') }).filter({ visible: true }).first().click();
  const dlg = a.getByRole('dialog');
  await dlg.getByRole('radio', { name: new RegExp(t('Модератор', 'Модератор')) }).check();
  await dlg.locator('#giveReason').fill('runs the queue this term');
  await response(a, '/api/admin/grants', () => dlg.getByRole('button', { name: t('Рөл беру', 'Выдать роль'), exact: true }).click());
  await expect(a.locator('.badge', { hasText: t('Модератор', 'Модератор') }).filter({ visible: true }).first()).toBeVisible();
  await a.goto(`/admin/audit?target=${mod.id}`);
  await expect(a.locator('.adm-log')).toContainText('runs the queue this term');
  await expect(a.locator('.adm-log')).toContainText(t('Модератор тағайындалды', 'Назначен модератор'));
  expect(sql<{ n: number }>(`select count(*)::int n from audit_log where action = 'scope_grant' and target_id = $1 and actor_id = $2`, mod.id, admin.id)[0].n).toBe(1);

  // the moderator's console: only the moderation panel; the super admin's and a shop's endpoints answer 403
  await mod.page.goto('/admin');
  await expect(mod.page.locator('.panel-card')).toHaveCount(1);
  await expect(mod.page.locator('.panel-card')).toContainText(t('Модерация', 'Модерация'));
  expect((await mod.page.request.get('/api/admin/users')).status()).toBe(403);
  expect((await mod.page.request.get('/api/admin/export/users.csv')).status()).toBe(403);
  expect((await mod.page.request.get('/api/admin/queue')).status()).toBe(200);
  const cafe = sql<{ id: number }>(`insert into cafes (name_kk, name_ru) values ('E2E кофе', 'E2E кофе') returning id`)[0].id;
  sql(`insert into cafe_staff (cafe_id, user_id, role) values ($1, $2, 'barista')`, cafe, student.id);
  expect((await mod.page.request.get(`/api/console/cafe/${cafe}/stats`)).status()).toBe(403);
  // the barista sees their café and nothing else
  await student.page.goto('/admin');
  await expect(student.page.locator('.panel-card')).toHaveCount(1);
  await expect(student.page.locator('.panel-card')).toContainText('E2E кофе');
  expect((await student.page.request.get('/api/admin/reports')).status()).toBe(403);

  // block the student with a reason: reads work, writes answer 403 account_restricted
  await a.goto(`/admin/users?id=${student.id}`);
  await a.getByRole('button', { name: t('Шектеу', 'Ограничить'), exact: true }).click();
  await a.getByRole('dialog').getByRole('textbox').fill('spam in Q&A');
  await response(a, `/api/admin/users/${student.id}/block`, () => a.getByRole('dialog').getByRole('button', { name: t('Шектеу', 'Ограничить'), exact: true }).click());
  const write = await student.page.request.patch('/api/me', { data: { name: 'Blocked' }, headers: { Origin: baseURL! } });
  expect(write.status()).toBe(403);
  expect((await write.json()).error.code).toBe('account_restricted');
  expect((await student.page.request.get('/api/me')).status()).toBe(200);

  // CSV: BOM, header row, the student is in it
  const csv = await a.request.get('/api/admin/export/users.csv?q=' + encodeURIComponent(student.name));
  expect(csv.status()).toBe(200);
  expect(csv.headers()['content-type']).toContain('text/csv');
  const text = (await csv.body()).toString('utf8');
  expect(text.charCodeAt(0)).toBe(0xFEFF);
  expect(text).toContain('id,name,username,email');
  expect(text).toContain(student.name);
});

// The workspace from the kurs panel against the real API: a café manager sees the payments of their own café only
// (another café answers 403, a barista 403), keeps a saved filter, and the super admin's notes, settings and the
// email invite stay closed to them; the admin's writes are in the audit log.
test('admin workspace: scoped payments 403, saved filter, notes, settings, invite', async ({ actor, lang, baseURL }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const admin = await actor(`Workspace admin ${lang}`, { admin: true });
  const manager = await actor(`Workspace manager ${lang}`);
  const barista = await actor(`Workspace barista ${lang}`);
  const headers = { Origin: baseURL! };
  const mine = sql<{ id: number }>(`insert into cafes (name_kk, name_ru) values ('WS кофе ${lang}', 'WS кофе ${lang}') returning id`)[0].id;
  const other = sql<{ id: number }>(`insert into cafes (name_kk, name_ru) values ('WS басқа ${lang}', 'WS другая ${lang}') returning id`)[0].id;
  sql(`insert into cafe_staff (cafe_id, user_id, role) values ($1, $2, 'manager'), ($1, $3, 'barista')`, mine, manager.id, barista.id);
  const order = sql<{ id: number }>(`insert into cafe_orders (cafe_id, user_id, items, total, pickup_at, pickup_code, payment_method, payment_status, paid_amount, status, refund_manual, idem_key)
    values ($1, $2, '[{"id":1}]', 90000, now(), '321', 'kaspi', 'paid', 90000, 'cancelled', true, $3) returning id`, mine, barista.id, `ws-${lang}-${mine}`)[0].id;

  // the manager: their café through the panel card, the money to return, «Ақша қайтарылды»
  const m = manager.page;
  await m.goto('/admin');
  await m.locator('.panel-card').getByRole('button', { name: t('Төлемдер', 'Платежи') }).click();
  await expect(m.locator('.pay-row.is-owed')).toHaveCount(1);
  await response(m, `/api/cafe/staff/orders/${order}/refunded`, () => m.locator('.pay-row.is-owed').getByRole('button', { name: t('Ақша қайтарылды', 'Деньги возвращены') }).click());
  await expect(m.locator('.pay-row.is-owed')).toHaveCount(0);
  expect(sql<{ n: number }>(`select count(*)::int n from audit_log where action = 'refund_returned' and actor_id = $1`, manager.id)[0].n).toBe(1);
  expect((await m.request.get(`/api/admin/payments?kind=cafe&shop=${other}`)).status()).toBe(403);
  expect((await m.request.get(`/api/admin/payments?kind=cafe&shop=${mine}`)).status()).toBe(200);
  expect((await barista.page.request.get('/api/admin/payments')).status()).toBe(403);
  // a saved filter is the manager's own
  expect((await m.request.post('/api/admin/views', { data: { screen: 'payments', name: 'Қайтару', query: 'payment=refund_manual' }, headers })).status()).toBe(200);
  expect((await m.request.post('/api/admin/views', { data: { screen: 'users', name: 'x', query: 'role=admin' }, headers })).status()).toBe(403);
  expect(((await (await admin.page.request.get('/api/admin/views?screen=payments')).json()).views as unknown[]).length).toBe(0);
  // the super admin's tools stay closed to a manager
  for (const p of [`/api/admin/users/${barista.id}/notes`, `/api/admin/users/${barista.id}/orders`, '/api/admin/config', '/api/admin/invites']) {
    expect((await m.request.get(p)).status(), p).toBe(403);
  }
  expect((await m.request.post(`/api/admin/users/${barista.id}/notes`, { data: { body: 'x' }, headers })).status()).toBe(403);
  expect((await m.request.post('/api/admin/invites', { data: { email: 'x@example.kz', grant: 'admin', reason: 'Командаға қосылды' }, headers })).status()).toBe(403);
  expect((await m.request.get('/api/events/attendance')).status()).toBe(403);

  // the admin: a note on the barista, the support contacts, an email invite
  const a = admin.page;
  await a.goto(`/admin/users?id=${barista.id}`);
  await a.getByRole('tab', { name: t('Ішкі жазбалар', 'Заметки') }).click();
  await a.locator('#noteText').fill('Kaspi қайтаруын сұрады');
  await response(a, `/api/admin/users/${barista.id}/notes`, () => a.getByRole('button', { name: t('Сақтау', 'Сохранить') }).click());
  await expect(a.getByTestId('user-notes')).toContainText('Kaspi қайтаруын сұрады');
  await a.getByRole('tab', { name: t('Тапсырыстар', 'Заказы') }).click();
  await expect(a.getByTestId('user-orders')).toContainText(`WS кофе ${lang}`);
  await a.goto('/admin/settings');
  await a.locator('#st_telegram').fill(`@ws_help_${lang}`);
  await response(a, '/api/admin/config/support', () => a.getByRole('button', { name: t('Сақтау', 'Сохранить') }).click(), 'PUT');
  expect(((await (await barista.page.request.get('/api/support')).json()).support.telegram)).toBe(`ws_help_${lang}`);
  const inv = await a.request.post('/api/admin/invites', { data: { email: `ws.invite.${lang}@example.kz`, grant: 'moderator', reason: 'Командаға шақырылды' }, headers });
  expect(inv.status()).toBe(200);
  expect(sql<{ n: number }>(`select count(*)::int n from audit_log where actor_id = $1 and action in ('note_add', 'settings_update', 'invite_create')`, admin.id)[0].n).toBe(3);
  await a.goto('/admin/attendance');
  await expect(a.getByTestId('kpi-strip')).toBeVisible();
});
