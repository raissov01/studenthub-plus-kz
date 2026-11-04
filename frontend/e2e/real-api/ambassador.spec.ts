import { test, expect, label, response, approve } from './helpers';

test('11-010: ambassador applies → admin approves → moderated post visible on site', async ({ actor, lang }) => {
  const applicant = await actor(`Ambassador ${lang}`);
  const admin = await actor(`Admin ${lang}`, { admin: true });
  const p = applicant.page;
  await p.goto('/ambassador/apply');
  await p.locator('#apWhy').fill(`Help students discover opportunities ${lang}`);
  await response(p, '/api/ambassador/apply', () => p.getByRole('button', { name: label(lang, 'Өтінім жіберу', 'Отправить заявку'), exact: true }).click());
  await expect(p.locator('.card[role=status]')).toContainText(label(lang, 'Тексерілуде', 'На проверке'));
  await approve(admin.page, lang, applicant.name);
  await p.goto('/ambassador');
  expect((await (await p.request.get('/api/me')).json()).user.role).toBe('ambassador');
  await p.getByRole('button', { name: label(lang, 'Мүмкіндік Грант, тағылымдама, байқау', 'Возможность Грант, стажировка, конкурс') }).click();
  const title = `Ambassador opportunity ${lang}`;
  for (const [id, value] of Object.entries({ title_kk: title, title_ru: title, org_kk: 'Campus Foundation', desc_kk: 'Free academic opportunity for university students.', source_url: 'https://example.org/campus-opportunity' })) {
    await p.locator(`#sg_${id}`).fill(value);
  }
  await response(p, '/api/submissions', () => p.getByRole('dialog').getByRole('button', { name: label(lang, 'Жіберу', 'Отправить'), exact: true }).click());
  let content = await (await p.request.get('/api/content')).json();
  expect(content.opportunities.some((o: { title: { kk: string } }) => o.title.kk === title)).toBe(false);
  await approve(admin.page, lang, title);
  content = await (await p.request.get('/api/content')).json();
  const item = content.opportunities.find((o: { title: { kk: string } }) => o.title.kk === title);
  expect(item).toBeTruthy();
  await p.goto(`/opportunity/${item.id}`);
  await expect(p.getByRole('heading', { name: title, exact: true })).toBeVisible();
  await expect(p.locator('.trust').getByRole('link', { name: 'example.org', exact: true })).toHaveAttribute('href', 'https://example.org/campus-opportunity');
});
