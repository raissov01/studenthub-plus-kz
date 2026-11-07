import { test, expect, label, onboard } from './fixtures';
import { content, opportunity, OPP_DEADLINE } from './content';
import type { Opportunity } from '../src/lib/types';

// An opportunity-feed record (docs/feeds.md) as /api/content serves it is listed in search and in the grants list
// with its source. The approval flow and the item page (AI note, original post) run against the real API
// (e2e/real-api/feeds.spec.ts).
const feedOpp: Opportunity = {
  ...opportunity, id: 'feed-7', type: 'grant', funded: false, docs: [], steps: [], source: 'feed',
  title: { kk: 'Astana Code хакатоны', ru: 'Хакатон Astana Code' }, org: { kk: 'Example Hub', ru: 'Example Hub' },
  desc: { kk: 'Студенттерге арналған хакатон.', ru: 'Хакатон для студентов.' }, source_url: 'https://t.me/example_channel/42',
  contact: { site: 'https://example.org/apply/astana-code' }, deadline: OPP_DEADLINE,
  feed: {
    category: 'hackathon', source_id: 'tg-example', source_name: 'Example Channel', original_url: 'https://t.me/example_channel/42',
    apply_url: 'https://example.org/apply/astana-code', event_date: '', online: false, eligibility: 'Bachelor students', funding: 'none',
    country: 'Kazakhstan', reviewed: true, note: { kk: 'ИИ қорытындысы, түпнұсқасын тексеріңіз', ru: 'Краткое изложение ИИ, проверьте оригинал' },
    extra_sources: [{ name: 'Second Channel', url: 'https://t.me/second_channel/9' }], confidence: 0.9,
  },
};

test('feed opportunity: search and the grants list show the AI source label', async ({ page, lang }) => {
  await page.route('**/api/content', route => route.fulfill({ json: { ...content, opportunities: [...content.opportunities, feedOpp] } }));
  await onboard(page, lang);
  await page.goto('/search');
  await page.getByRole('searchbox').fill('Astana Code');
  await expect(page.locator('main')).toContainText(`${label(lang, 'ЖИ', 'ИИ')}: Example Channel`);
  // the grants list renders the card with the «ИИ · source» badge (the page itself is server-routed: e2e/real-api/feeds.spec.ts)
  await page.goto('/internships?t=grants');
  const card = page.locator('.fav-card').filter({ hasText: label(lang, 'Astana Code хакатоны', 'Хакатон Astana Code') });
  await expect(card.locator('.feed-badge')).toContainText('Example Channel');
});
