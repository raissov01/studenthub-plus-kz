import { test, expect, label, approve, sql } from './helpers';

// Opportunity feed (docs/feeds.md). The collector's output is the SQL fixture (a pending item exactly as
// cmd/feeds-collect stores it after the AI and validation); review, publishing and the site are real.
test('feeds: admin approves a pending feed item → it appears in search and on its page with the source label', async ({ actor, lang }) => {
  const title = `Feed hackathon ${lang} ${Date.now()}`;
  const deadline = new Date(Date.now() + 20 * 86400_000).toISOString().slice(0, 10);
  const data = {
    is_relevant: true, category: 'hackathon', title_kk: title, title_ru: title, summary_kk: 'Студенттерге арналған хакатон, командамен қатысуға болады.',
    summary_ru: 'Хакатон для студентов, можно участвовать командой.', organizer: 'E2E Org', country: 'Kazakhstan', city: 'Astana', online: false,
    deadline, event_date: '', eligibility: 'Bachelor students', funding: 'none', apply_url: 'https://example.org/apply/e2e-hack',
    source_url: `https://t.me/e2e_channel/${lang === 'kk' ? 1 : 2}`, language: 'ru', confidence: 0.7, reason: 'hackathon announcement',
  };
  sql(`insert into feed_sources (id, kind, handle, name, trust, enabled) values ('e2e-channel', 'tg_public', 'e2e_channel', 'E2E Channel', 'medium', true)
    on conflict (id) do nothing`);
  const [{ id: itemId }] = sql<{ id: number }>(`with p as (
      insert into feed_posts (source_id, external_id, url, text, status) values ('e2e-channel', $1, $2, $3, 'processed') returning id)
    insert into feed_items (post_id, source_id, status, category, data, title_norm, deadline, confidence)
      select p.id, 'e2e-channel', 'pending', 'hackathon', $4::jsonb, lower($5), $6::date, 0.7 from p returning id`,
  `e2e-${lang}-${Date.now()}`, data.source_url, `${title}. Приём заявок до ${deadline}. ${data.apply_url}`, JSON.stringify(data), title, deadline);
  const payload = {
    feed_item_id: itemId, source: 'E2E Channel', trust: 'medium', category: 'hackathon', title_kk: title, title_ru: title,
    summary_kk: data.summary_kk, summary_ru: data.summary_ru, organizer: data.organizer, deadline, event_date: '', eligibility: data.eligibility,
    funding: 'none', online: false, city: 'Astana', country: 'Kazakhstan', apply_url: data.apply_url, source_url: data.source_url,
    confidence: 0.7, ai_reason: data.reason, ai_notes: [],
  };
  sql(`with s as (insert into submissions (kind, payload) values ('feed_item', $1::jsonb) returning id)
    update feed_items set submission_id = (select id from s) where id = $2`, JSON.stringify(payload), itemId);

  const student = await actor(`Feed student ${lang}`);
  const admin = await actor(`Admin ${lang}`, { admin: true });
  const p = student.page;
  // pending: not on the site
  let content = await (await p.request.get('/api/content')).json();
  expect(content.opportunities.some((o: { id: string }) => o.id === `feed-${itemId}`)).toBe(false);

  // the admin sees it under the feed kind in the queue and in /admin/feeds, then approves in the queue
  await admin.page.goto('/admin/feeds');
  await expect(admin.page.locator('.card').filter({ hasText: title })).toHaveCount(1);
  await approve(admin.page, lang, title);

  content = await (await p.request.get('/api/content')).json();
  const item = content.opportunities.find((o: { id: string }) => o.id === `feed-${itemId}`);
  expect(item).toBeTruthy();
  expect(item.source).toBe('feed');
  expect(item.feed.source_name).toBe('E2E Channel');
  expect(item.feed.reviewed).toBe(true);

  // search lists it with the source label
  await p.goto('/search');
  await p.getByRole('searchbox').fill(title);
  const row = p.locator('main').getByText(title, { exact: true }).first();
  await expect(row).toBeVisible();
  await expect(p.locator('main')).toContainText(`${label(lang, 'ЖИ', 'ИИ')}: E2E Channel`);

  // its page: AI note, original post link, application link, deadline
  await p.goto(`/opportunity/${item.id}`);
  await expect(p.getByRole('heading', { name: title, exact: true })).toBeVisible();
  await expect(p.locator('.feed-badge')).toContainText('E2E Channel');
  const note = p.locator('.feed-note');
  await expect(note).toContainText(label(lang, 'ЖИ қорытындысы, түпнұсқасын тексеріңіз', 'Краткое изложение ИИ, проверьте оригинал'));
  await expect(note.getByRole('link', { name: 'E2E Channel', exact: true })).toHaveAttribute('href', data.source_url);
  await expect(note.getByRole('link', { name: 'example.org', exact: true })).toHaveAttribute('href', data.apply_url);
});
