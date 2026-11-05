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

  // grant the moderator scope from the user page, with a reason
  await a.goto(`/admin/users?id=${mod.id}`);
  await a.getByRole('button', { name: t('Модератор ету', 'Сделать модератором') }).click();
  await a.getByRole('dialog').getByRole('textbox').fill('runs the queue this term');
  await response(a, '/api/admin/grants', () => a.getByRole('dialog').getByRole('button', { name: t('Тағайындау', 'Назначить'), exact: true }).click());
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
