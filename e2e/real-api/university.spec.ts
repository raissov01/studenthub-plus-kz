import { test, expect, label, response, sql } from './helpers';

test('11-014: university staff → official content → privacy-safe aggregates and CSV', async ({ actor, lang }) => {
  const staff = await actor(`Staff ${lang}`);
  const admin = await actor(`University admin ${lang}`, { admin: true });
  const visitor = await actor(`Visitor ${lang}`);
  const university = `Privacy university ${lang}`;
  const p = staff.page;
  // Grant through the real administrator UI, including the university binding.
  await admin.page.goto('/admin/staff');
  const search = admin.page.getByRole('searchbox', { name: label(lang, 'Қолданушыны іздеу', 'Поиск пользователя') });
  await search.fill(String(staff.telegramID));
  await search.press('Enter');
  await admin.page.getByRole('button', { name: new RegExp(staff.name) }).click();
  await admin.page.locator('#stUniv').fill(university);
  await response(admin.page, '/api/admin/university-staff', () => admin.page.getByRole('dialog').getByRole('button', { name: label(lang, 'Байлау', 'Привязать'), exact: true }).click());
  // Seed only aggregate population, deliberately on both sides of the privacy floor.
  sql(`insert into users (name, email, university, faculty, course, consent_at, last_seen_at)
    select 'Private student ' || n, 'privacy-' || $2 || '-' || n || '@example.invalid', $1,
      case when n<=4 then 'Small faculty' else 'Large faculty' end,
      case when n<=4 then 1 else 2 end, now(), now() from generate_series(1,9) n`, university, lang);
  sql(`insert into users (name, email, university, faculty, course, consent_at)
    select 'Other university private ' || n, 'other-' || $2 || '-' || n || '@example.invalid',
      $1, 'Other faculty', 3, now() from generate_series(1,8) n`, `${university} other`, lang);
  await p.goto('/university');
  await expect(p.getByRole('heading', { name: university, exact: true })).toBeVisible();
  const stats = await (await p.request.get('/api/university/stats')).json();
  expect(stats).toMatchObject({ university, students: 9, byFaculty: { 'Small faculty': '< 5', 'Large faculty': 5 }, byCourse: { '1': '< 5', '2': 5 } });
  expect(stats.byFaculty).not.toHaveProperty('Other faculty');
  expect(JSON.stringify(stats)).not.toContain('Private student');
  await expect(p.locator('.row').filter({ hasText: 'Small faculty' }).locator('.num')).toHaveText('< 5');
  await expect(p.locator('.row').filter({ hasText: 'Large faculty' }).locator('.num')).toHaveText('5');
  const csv = await (await p.request.get('/api/university/stats.csv')).text();
  expect(csv).toContain('faculty:Small faculty,< 5');
  expect(csv).toContain('faculty:Large faculty,5');
  expect(csv).not.toContain('Private student');
  expect(csv).not.toContain('Other faculty');
  expect((await visitor.page.request.get('/api/university/stats')).status()).toBe(403);
  const title = `Official campus notice ${lang}`;
  await p.getByRole('button', { name: label(lang, 'Хабарландыру', 'Объявление'), exact: true }).click();
  for (const [id, text] of Object.entries({ title_kk: title, title_ru: title, body_kk: 'Campus library is open for all students this week.', body_ru: 'Библиотека открыта для всех студентов на этой неделе.' })) await p.locator(`#up_${id}`).fill(text);
  const post = await response(p, '/api/university/posts', () => p.getByRole('dialog').getByRole('button', { name: label(lang, 'Жариялау', 'Опубликовать'), exact: true }).click());
  expect(post.post.university).toBe(university);
  await visitor.page.goto('/announcements');
  const announcement = visitor.page.locator('.card').filter({ hasText: title });
  await expect(announcement).toContainText(label(lang, 'Университеттен ресми', 'Официально от университета'));
  await expect(announcement).toContainText(university);
});
