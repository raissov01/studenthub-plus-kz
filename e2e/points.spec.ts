import type { Page } from '@playwright/test';
import { test, expect, onboard, fits, label } from './fixtures';
import { config } from './content';
import type { Lang } from '../lib/types';

const summary = (coins: number) => ({
  xp: 150, level: 2, levelXp: 100, nextLevelXp: 300, coins,
  streak: { days: 4, best: 6, freezes: 0, maxFreezes: 1, freezeCost: 20, activeToday: true },
  today: { xp: 5, coins: 1, xpCap: 150, coinsCap: 30 },
  tasks: [
    { id: 'daily_open', xp: 5, coins: 1, perDay: 1, deviceOnly: false, done: 1, week: 3 },
    { id: 'quiz', xp: 10, coins: 2, perDay: 5, deviceOnly: false, done: 0, week: 0 },
    { id: 'event_checkin', xp: 30, coins: 15, perDay: 3, deviceOnly: false, done: 0, week: 1 },
    { id: 'roadmap_step', xp: 5, coins: 0, perDay: 1, perWeek: 3, deviceOnly: true, done: 0, week: 0 },
  ],
  settings: { leaderboardOptOut: false, studyGroup: 'ИС-21' },
  recent: [{ delta: 1, reason: 'daily_open', createdAt: '2026-11-04T03:00:00Z' }],
});
const reward = {
  id: 7, kind: 'merch', titleKk: 'Университет футболкасы', titleRu: 'Футболка университета', descKk: 'M, L өлшемдері', descRu: 'Размеры M, L',
  cost: 40, stock: 3, perUserLimit: 1, discountId: null, active: true, mine: 0,
};

/** a signed-in student with the points switch on; every points call is answered by the mock server below */
async function signedIn(page: Page, lang: Lang) {
  const calls: { method: string; path: string; body: unknown }[] = [];
  let coins = 58;
  let redeemed = false;
  // the daily award answers only when the test says so, so its toast is not lost behind onboarding
  let release = () => {};
  const dailyGate = new Promise<void>(r => { release = r; });
  await page.route('**/api/config', route => route.fulfill({ json: { ...config, features: { ...config.features, points: true } } }));
  await page.route('**/api/me', route => route.fulfill({ json: { user: { id: 501, name: 'Аружан Тест', role: 'student', lang, consentAt: '2026-01-01', isAdult: true, university: 'KATU', faculty: 'IT' }, needsConsent: false, inboxUnread: 0 } }));
  await page.route('**/api/points/**', async route => {
    const req = route.request();
    const url = new URL(req.url());
    const body = req.postData() ? req.postDataJSON() : null;
    calls.push({ method: req.method(), path: url.pathname, body });
    switch (`${req.method()} ${url.pathname}`) {
      case 'POST /api/points/daily': await dailyGate; return route.fulfill({ json: { award: { xp: 5, coins: 1, reason: 'daily_open' }, me: summary(coins) } });
      case 'GET /api/points/me': return route.fulfill({ json: summary(coins) });
      case 'GET /api/points/leaderboard':
        return route.fulfill({ json: url.searchParams.get('scope') === 'group'
          ? { scope: 'group', status: 'too_small', members: 3, minMembers: 5, optedOut: false, weekStart: '2026-11-01T19:00:00Z', resetsAt: '2026-11-08T19:00:00Z' }
          : { scope: 'faculty', status: 'ok', members: 6, minMembers: 5, optedOut: false, weekStart: '2026-11-01T19:00:00Z', resetsAt: '2026-11-08T19:00:00Z',
            leaders: [{ rank: 1, name: 'Дана К.', xp: 120, me: false }, { rank: 2, name: 'Аружан Т.', xp: 45, me: true }] } });
      case 'GET /api/points/rewards': return route.fulfill({ json: { rewards: [{ ...reward, mine: redeemed ? 1 : 0 }] } });
      case 'POST /api/points/redeem':
        redeemed = true; coins -= reward.cost;
        return route.fulfill({ status: 201, json: { redemption: { id: 31, rewardId: 7, kind: 'merch', titleKk: reward.titleKk, titleRu: reward.titleRu, cost: 40, status: 'pending', code: null, note: null, createdAt: '2026-11-04T05:00:00Z', decidedAt: null }, me: summary(coins) } });
      case 'GET /api/points/redemptions':
        return route.fulfill({ json: { redemptions: [
          ...(redeemed ? [{ id: 31, rewardId: 7, kind: 'merch', titleKk: reward.titleKk, titleRu: reward.titleRu, cost: 40, status: 'pending', code: null, note: null, createdAt: '2026-11-04T05:00:00Z', decidedAt: null }] : []),
          { id: 12, rewardId: 2, kind: 'discount', titleKk: 'Кофеханаға 10 %', titleRu: 'Кофейня −10 %', cost: 25, status: 'approved', code: 'COFFEE-E2E-10', note: null, createdAt: '2026-11-01T05:00:00Z', decidedAt: '2026-11-02T05:00:00Z' },
        ] } });
      case 'PATCH /api/points/settings': return route.fulfill({ json: { award: null, me: { ...summary(coins), settings: { ...summary(coins).settings, ...(body as object) } } } });
    }
    return route.fulfill({ status: 501, json: { error: { code: 'unmocked_points' } } });
  });
  return { calls, release };
}

test('points: signed out sees a sign-in prompt and no coins anywhere', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await page.route('**/api/config', route => route.fulfill({ json: { ...config, features: { ...config.features, points: true } } }));
  await onboard(page, lang);
  await expect(page.locator('#pointsChip')).toHaveCount(0);
  await page.goto('/points');
  await expect(page.getByRole('heading', { name: t('Ұпайлар қалай жиналады', 'Как копятся баллы') })).toBeVisible();
  await expect(page.getByRole('button', { name: t('Telegram арқылы кір', 'Войти через Telegram') })).toBeVisible();
  await expect(page.locator('#ptsCoins')).toHaveCount(0);
  await page.getByRole('button', { name: t('Telegram арқылы кір', 'Войти через Telegram') }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await fits(page);
});

test('points: home chip, level, tasks, leaderboards, shop redeem and my codes', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const { calls, release } = await signedIn(page, lang);
  await onboard(page, lang);
  release();
  // the once-a-day award toasts and the single compact row appears under the hero
  await expect(page.getByText(t('+5 XP · +1 монета', '+5 XP · +1 монета'), { exact: true })).toBeVisible();
  const chip = page.locator('#pointsChip');
  await expect(chip).toBeVisible();
  await expect(chip).toHaveAttribute('aria-label', t('Ұпайлар: 2-деңгей, 4 күн қатарынан, 58 монета', 'Баллы: уровень 2, 4 дня подряд, 58 монет'));
  expect((await chip.boundingBox())!.height).toBeLessThanOrEqual(56);
  await fits(page);
  expect(calls.filter(c => c.path === '/api/points/daily')).toHaveLength(1);

  // keyboard: the chip is a real button that opens the screen
  await chip.focus();
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(/\/points$/);
  await expect(page.getByRole('heading', { name: t('2-деңгей', 'Уровень 2') })).toBeVisible();
  await expect(page.getByRole('progressbar', { name: t('2-деңгей прогресі', 'Прогресс уровня 2') })).toHaveAttribute('aria-valuenow', '25');
  await expect(page.locator('#ptsCoins')).toHaveText('58');
  await expect(page.getByRole('button', { name: new RegExp(t('Біреудің квизін өту', 'Пройти чужой квиз')) })).toBeVisible();
  await expect(page.getByText(t('Қосымшаны бүгін ашу', 'Открыть приложение сегодня'))).toBeVisible();
  await fits(page);

  // leaderboards: a small group is hidden, the faculty board marks «me»
  await expect(page.getByText(t('Рейтинг кемінде 5 адам болғанда ғана көрсетіледі', 'Рейтинг показывается, только когда участников не меньше 5'), { exact: false })).toBeVisible();
  await page.getByRole('tab', { name: t('Факультет', 'Факультет') }).click();
  const board = page.getByRole('list', { name: t('Факультет рейтингі', 'Рейтинг факультета') });
  await expect(board.getByRole('listitem')).toHaveCount(2);
  await expect(board.locator('[aria-current="true"]')).toContainText(t('Аружан Т. (сіз)', 'Аружан Т. (вы)'));
  await page.getByRole('checkbox', { name: t('Рейтингте көрсетпеу', 'Не показывать меня в рейтинге') }).click();
  await expect(page.getByRole('checkbox', { name: t('Рейтингте көрсетпеу', 'Не показывать меня в рейтинге') })).toBeChecked();
  await expect.poll(() => calls.find(c => c.method === 'PATCH')?.body).toEqual({ leaderboardOptOut: true });

  // shop: redeem sends only the reward id and an idempotency key, never an amount
  await page.getByRole('tab', { name: t('Дүкен', 'Магазин') }).click();
  await page.getByRole('button', { name: t('«Университет футболкасы» алу, 40 монета', 'Получить «Футболка университета» за 40 монет') }).click();
  const sheet = page.getByRole('dialog');
  await expect(sheet).toContainText(t('40 монета ұсталады', 'Спишется 40 монет'));
  await sheet.getByRole('button', { name: t('Растау', 'Подтвердить'), exact: true }).click();
  await expect(page.getByText(t('Өтінім жіберілді', 'Заявка отправлена'), { exact: true })).toBeVisible();
  const redeem = calls.find(c => c.path === '/api/points/redeem')!;
  expect(Object.keys(redeem.body as object).sort()).toEqual(['key', 'rewardId']);
  expect((redeem.body as { key: string }).key).toMatch(/^[A-Za-z0-9_-]{8,64}$/);
  await expect(page.locator('#ptsCoins')).toHaveText('18');

  // my requests: the new one is pending, an approved one shows its code with a copy button
  await expect(page.getByText(t('Қаралуда', 'На рассмотрении'))).toBeVisible();
  await expect(page.getByText('COFFEE-E2E-10', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: t('Көшіру', 'Копировать') })).toBeVisible();
  await fits(page);
  await page.screenshot({ path: test.info().outputPath('points.png'), fullPage: true });

  // menu entry and dark theme at this width
  await page.goto('/');
  await page.getByRole('button', { name: t('Мәзір', 'Меню'), exact: true }).click();
  await page.getByRole('dialog', { name: t('Мәзір', 'Меню') }).getByRole('button', { name: t('Ұпайлар', 'Баллы'), exact: true }).click();
  await expect(page).toHaveURL(/\/points$/);
  await page.evaluate(() => {
    const s = JSON.parse(localStorage.getItem('studenthub.v1') || '{}');
    localStorage.setItem('studenthub.v1', JSON.stringify({ ...s, theme: 'dark' }));
  });
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(page.locator('#ptsCoins')).toBeVisible();
  const bg = await page.locator('.pts-level').evaluate(el => getComputedStyle(el).backgroundImage);
  expect(bg).toBe('none'); // flat: no gradients
  await fits(page);
  await page.screenshot({ path: test.info().outputPath('points-dark.png'), fullPage: true });
  expect(calls.filter(c => c.path === '/api/points/daily')).toHaveLength(1); // once per Almaty day per device
});
