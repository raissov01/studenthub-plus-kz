import type { Page } from '@playwright/test';
import { test, expect, onboard, fits, label } from './fixtures';
import { config } from './content';
import type { Lang } from '../src/lib/types';

/* Community features with every API call mocked: lost & found, course Q&A, roommates. Each has a happy path and a
   privacy case (no contact before consent, contacts refused in free text, signed-out reads). */

type Call = { method: string; path: string; body: unknown };
const on = { lostfound: true, courseqa: true, roommates: true };

async function setup(page: Page, lang: Lang, signedIn: boolean) {
  await page.route('**/api/config', route => route.fulfill({ json: { ...config, features: { ...config.features, ...on } } }));
  if (signedIn) {
    await page.route('**/api/me', route => route.fulfill({ json: { user: { id: 501, name: 'Аружан Тест', role: 'student', lang, consentAt: '2026-01-01', isAdult: true, university: 'KATU', faculty: 'IT', username: 'aru_test' }, needsConsent: false, inboxUnread: 0 } }));
  }
  const calls: Call[] = [];
  const record = (route: Parameters<Parameters<Page['route']>[1]>[0]) => {
    const req = route.request();
    const url = new URL(req.url());
    calls.push({ method: req.method(), path: url.pathname + url.search, body: req.postData() ? req.postDataJSON() : null });
    return { method: req.method(), path: url.pathname, url };
  };
  return { calls, record };
}

const post = (over: Record<string, unknown> = {}) => ({
  id: 7, kind: 'found', category: 'keys', title: 'Көк брелокты кілттер', desc: '', building: 'b1', room: '1-305', happenedOn: '2026-11-04',
  photo: null, handedTo: '', university: 'KATU', status: 'active', createdAt: '2026-11-04T05:00:00Z', expiresAt: '2026-12-04T05:00:00Z', mine: false, ...over,
});

test('lost & found: found card goes to the dean, poster confirms a claim, contacts open', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const { calls, record } = await setup(page, lang, true);
  let confirmed = false;
  await page.route('**/api/lostfound/**', async route => {
    const { method, path } = record(route);
    if (method === 'GET' && path === '/api/lostfound/posts') return route.fulfill({ json: { posts: [post()], university: 'KATU' } });
    if (method === 'POST' && path === '/api/lostfound/posts') return route.fulfill({ status: 201, json: { post: post({ id: 9, mine: true, category: 'cards', title: 'Студенттік билет', handedTo: 'dean', building: '' }) } });
    if (method === 'GET' && path === '/api/lostfound/posts/9') {
      return route.fulfill({ json: {
        post: post({ id: 9, mine: true, category: 'cards', title: 'Студенттік билет', handedTo: 'dean', building: '' }), myClaim: null,
        claims: [{ id: 3, postId: 9, message: 'Билеттің қабында жапсырма бар', status: confirmed ? 'confirmed' : 'pending', createdAt: '2026-11-04T06:00:00Z',
          iReturned: false, theyReturned: false, contact: confirmed ? { name: 'Дана К.', username: 'dana_k', telegramUrl: 'https://t.me/dana_k', email: null } : null }],
      } });
    }
    if (method === 'POST' && path === '/api/lostfound/claims/3/decide') { confirmed = true; return route.fulfill({ json: { ok: true, status: 'confirmed' } }); }
    return route.fulfill({ status: 501, json: { error: { code: 'unmocked_lf' } } });
  });
  await onboard(page, lang);
  await page.getByRole('button', { name: t('Мәзір', 'Меню'), exact: true }).click();
  await page.getByRole('dialog', { name: t('Мәзір', 'Меню') }).getByRole('button', { name: t('Табылған заттар', 'Бюро находок'), exact: true }).click();
  await expect(page).toHaveURL(/\/lostfound$/);
  await expect(page.getByText('Көк брелокты кілттер')).toBeVisible();
  await fits(page);

  await page.getByRole('button', { name: t('Таптым', 'Нашёл'), exact: true }).click();
  // the form may remount once while the account loads: retry the pick until the warning shows
  await expect(async () => {
    await page.getByLabel(t('Санат', 'Категория')).selectOption('cards');
    await expect(page.getByText(t('Құжат пен карта — жеке дерек', 'Документы и карты — личные данные'))).toBeVisible({ timeout: 1000 });
  }).toPass();
  await expect(page.getByText(t('Фото (міндетті емес)', 'Фото (необязательно)'))).toHaveCount(0);
  await page.getByLabel(t('Не?', 'Что?')).fill('Студенттік билет 202612345678');
  await page.getByRole('button', { name: t('Жариялау', 'Опубликовать'), exact: true }).click();
  // the dean / security choice is required, then the number is refused — nothing is sent
  await expect(page.locator('p[role=alert]')).toBeVisible();
  await page.getByLabel(t('Деканатқа тапсырылды', 'Сдано в деканат')).check();
  await expect(page.locator('p[role=alert]')).toContainText(t('нөмірін жарияламаңыз', 'Не публикуйте номер'));
  expect(calls.filter(c => c.method === 'POST')).toHaveLength(0);
  await page.getByLabel(t('Не?', 'Что?')).fill('Студенттік билет');
  await fits(page);
  await page.getByRole('button', { name: t('Жариялау', 'Опубликовать'), exact: true }).click();
  await expect(page).toHaveURL(/\/lostfound\?id=9$/);
  const sent = calls.find(c => c.method === 'POST' && c.path === '/api/lostfound/posts')!.body as Record<string, unknown>;
  expect(sent).toMatchObject({ kind: 'found', category: 'cards', title: 'Студенттік билет', handedTo: 'dean', photo: '' });

  // the claim's secret detail is shown to the poster; no contact until «Растау»
  await expect(page.getByText('Билеттің қабында жапсырма бар')).toBeVisible();
  await expect(page.getByText('@dana_k')).toHaveCount(0);
  await page.getByRole('button', { name: t('Растау', 'Подтвердить'), exact: true }).click();
  await expect(page.getByText('@dana_k')).toBeVisible();
  await expect(page.getByRole('button', { name: t('Telegram-да жазу', 'Написать в Telegram') })).toBeVisible();
  await fits(page);
});

test('lost & found privacy: signed-out readers get no claim form, contacts in a post are refused', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const { calls, record } = await setup(page, lang, false);
  await page.route('**/api/lostfound/**', async route => {
    const { path } = record(route);
    if (path === '/api/lostfound/posts/7') return route.fulfill({ json: { post: post(), myClaim: null } });
    return route.fulfill({ status: 501, json: { error: { code: 'unmocked_lf' } } });
  });
  await onboard(page, lang);
  await page.goto('/lostfound?id=7');
  await expect(page.getByRole('heading', { name: 'Көк брелокты кілттер' })).toBeVisible();
  await expect(page.getByRole('button', { name: t('Бұл менікі', 'Это моё') })).toHaveCount(0);
  await expect(page.getByText(t('Байланыстар екі жақ келіскенде ғана ашылады', 'Контакты открываются только по согласию обеих сторон'), { exact: false })).toBeVisible();
  // signed out: the form asks to sign in instead
  await page.goto('/lostfound?id=new:lost');
  await expect(page.getByRole('button', { name: t('Telegram арқылы кір', 'Войти через Telegram') })).toBeVisible();
  expect(calls.filter(c => c.method !== 'GET')).toHaveLength(0);
});

const question = (over: Record<string, unknown> = {}) => ({
  id: 12, subject: 'Математика 1', subjectKey: 'математика 1', facultyOnly: false, title: 'Туындыны қалай табамыз?', body: 'x^2 үшін', tags: ['туынды'],
  anonymous: true, author: null, status: 'active', locked: false, lockedReason: null, acceptedAnswerId: null, answers: 1, score: 2, voted: false, mine: true,
  createdAt: '2026-11-04T05:00:00Z', ...over,
});

test('course Q&A: ask anonymously with the honesty rule, accept an answer', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const { calls, record } = await setup(page, lang, true);
  let accepted: number | null = null;
  await page.route('**/api/qa/**', async route => {
    const { method, path } = record(route);
    if (method === 'GET' && path === '/api/qa/questions') return route.fulfill({ json: { questions: [], subjectKey: 'математика 1', university: 'KATU', facultyScoped: true } });
    if (method === 'POST' && path === '/api/qa/questions') return route.fulfill({ status: 201, json: { question: question() } });
    if (method === 'GET' && path === '/api/qa/questions/12') {
      return route.fulfill({ json: { question: question({ acceptedAnswerId: accepted }), answers: [
        { id: 40, body: '2x болады', anonymous: false, author: { name: 'Дана К.' }, score: 1, voted: false, mine: false, accepted: accepted === 40, createdAt: '2026-11-04T06:00:00Z' },
      ] } });
    }
    if (method === 'POST' && path === '/api/qa/questions/12/accept') { accepted = 40; return route.fulfill({ json: { ok: true, acceptedAnswerId: 40, award: null } }); }
    return route.fulfill({ status: 501, json: { error: { code: 'unmocked_qa' } } });
  });
  await onboard(page, lang);
  await page.goto('/qa?id=s:' + encodeURIComponent('Математика 1'));
  await expect(page.getByRole('heading', { name: 'Математика 1' })).toBeVisible();
  await expect(page.getByText(t('Бұл пән бойынша әзірге сұрақ жоқ.', 'По этому предмету пока нет вопросов.'))).toBeVisible();
  expect(calls.find(c => c.path.startsWith('/api/qa/questions?'))!.path).toContain('subject=');
  await fits(page);

  await page.getByRole('button', { name: t('Сұрақ қою', 'Задать вопрос'), exact: true }).click();
  await expect(page.getByLabel(t('Пән', 'Предмет'))).toHaveValue('Математика 1');
  await page.getByLabel(t('Сұрақ', 'Вопрос'), { exact: true }).fill('Туындыны қалай табамыз?');
  await page.getByLabel(t('Тегтер (5-ке дейін, үтірмен)', 'Теги (до 5, через запятую)')).fill('#туынды');
  await page.getByLabel(t('Курстастарға атымды көрсетпеу (модераторлар көреді)', 'Не показывать имя однокурсникам (модераторы видят)')).check();
  const submit = page.getByRole('button', { name: t('Жариялау', 'Опубликовать'), exact: true });
  await expect(submit).toBeDisabled();
  await page.getByLabel(t('Бұл емтихан не бақылау тапсырмасының жауабын сұрау емес', 'Это не просьба дать ответы на экзамен или контрольную')).check();
  await fits(page);
  await submit.click();
  await expect(page).toHaveURL(/\/qa\?id=12$/);
  expect(calls.find(c => c.method === 'POST' && c.path === '/api/qa/questions')!.body).toMatchObject({ subject: 'Математика 1', anonymous: true, honesty: true, tags: ['туынды'], scope: 'university' });
  await expect(page.getByText(t('Аноним', 'Аноним')).first()).toBeVisible();
  await page.getByRole('button', { name: t('Шешім деп белгілеу', 'Отметить решением') }).click();
  await expect(page.getByRole('article', { name: t('Шешім', 'Решение') })).toBeVisible();
  await fits(page);
});

test('course Q&A privacy: phone numbers are refused before sending; locked threads take no answers', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const { calls, record } = await setup(page, lang, true);
  await page.route('**/api/qa/**', async route => {
    const { path } = record(route);
    if (path === '/api/qa/questions/12') return route.fulfill({ json: { question: question({ mine: false, locked: true, lockedReason: t('Емтихан жүріп жатыр', 'Идёт экзамен') }), answers: [] } });
    return route.fulfill({ status: 501, json: { error: { code: 'unmocked_qa' } } });
  });
  await onboard(page, lang);
  await page.goto('/qa?id=new');
  await page.getByLabel(t('Пән', 'Предмет')).fill('Физика');
  await page.getByLabel(t('Сұрақ', 'Вопрос'), { exact: true }).fill('Маған жазыңыз 8 701 123 45 67');
  await page.getByLabel(t('Бұл емтихан не бақылау тапсырмасының жауабын сұрау емес', 'Это не просьба дать ответы на экзамен или контрольную')).check();
  await expect(page.locator('p[role=alert]')).toContainText('@username');
  await expect(page.getByRole('button', { name: t('Жариялау', 'Опубликовать'), exact: true })).toBeDisabled();
  await page.goto('/qa?id=12');
  await expect(page.getByText(t('Тақырып модератормен жабылған', 'Тема закрыта модератором'))).toBeVisible();
  await expect(page.getByLabel(t('Сіздің жауабыңыз', 'Ваш ответ'))).toHaveCount(0);
  expect(calls.filter(c => c.method === 'POST')).toHaveLength(0);
});

const listing = (over: Record<string, unknown> = {}) => ({
  id: 21, kind: 'have_room', housing: 'flat', district: 'Сарыарқа ауданы', budgetMin: 40000, budgetMax: 80000, moveIn: '2026-11-20',
  habits: ['quiet', 'tidy'], about: 'Тыныш студентпін', sameGender: true, gender: 'f', university: 'KATU', status: 'active',
  createdAt: '2026-11-04T05:00:00Z', expiresAt: '2026-12-04T05:00:00Z', mine: false, myRequest: null, ...over,
});

test('roommates: request a listing, the owner accepts, contacts open', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const { calls, record } = await setup(page, lang, true);
  let requested = false;
  await page.route('**/api/roommates/**', async route => {
    const { method, path } = record(route);
    if (path === '/api/roommates/me') return route.fulfill({ json: { gender: 'f', listing: null, requestsToday: requested ? 1 : 0, dailyLimit: 10, blockers: [] } });
    if (path === '/api/roommates/listings') return route.fulfill({ json: { listings: [listing({ myRequest: requested ? 'pending' : null })], university: 'KATU' } });
    if (method === 'POST' && path === '/api/roommates/listings/21/requests') { requested = true; return route.fulfill({ status: 201, json: { request: { id: 5, status: 'pending' } } }); }
    if (path === '/api/roommates/requests') {
      return route.fulfill({ json: { requests: [{ id: 5, listingId: 21, status: 'accepted', createdAt: '2026-11-04T06:00:00Z', incoming: false, listing: listing({ myRequest: 'accepted' }),
        contact: { name: 'Дана К.', username: 'dana_k', telegramUrl: 'https://t.me/dana_k', email: null } }] } });
    }
    return route.fulfill({ status: 501, json: { error: { code: 'unmocked_rm' } } });
  });
  await onboard(page, lang);
  await page.goto('/roommates');
  await expect(page).toHaveURL(/\/roommates$/);
  const card = page.getByRole('article').filter({ hasText: 'Сарыарқа ауданы' });
  await expect(card).toContainText(t('Қыздар ғана', 'Только девушки'));
  await expect(card).not.toContainText('@');
  await fits(page);
  await card.getByRole('button', { name: t('Қызығамын', 'Интересно') }).click();
  await page.getByRole('button', { name: t('Жіберу', 'Отправить'), exact: true }).click();
  await expect(page.getByText(t('Сұрау жіберілді', 'Запрос отправлен')).first()).toBeVisible();
  expect(calls.find(c => c.method === 'POST')!.body).toBeNull();
  await page.goto('/roommates?id=requests');
  await expect(page.getByText('@dana_k')).toBeVisible();
  await expect(page.getByText(t('Алдын ала ақша аудармаңыз', 'Не переводите деньги заранее'), { exact: false })).toBeVisible();
  await fits(page);
});

test('roommates privacy: signed out sees nothing, a @handle in «about» is refused', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await page.route('**/api/config', route => route.fulfill({ json: { ...config, features: { ...config.features, ...on } } }));
  await onboard(page, lang);
  await page.goto('/roommates');
  await expect(page.getByRole('button', { name: t('Telegram арқылы кір', 'Войти через Telegram') })).toBeVisible();
  await expect(page.getByRole('article')).toHaveCount(0);

  const { calls, record } = await setup(page, lang, true);
  await page.route('**/api/roommates/**', async route => {
    const { path } = record(route);
    if (path === '/api/roommates/me') return route.fulfill({ json: { gender: null, listing: null, requestsToday: 0, dailyLimit: 10, blockers: [] } });
    return route.fulfill({ status: 501, json: { error: { code: 'unmocked_rm' } } });
  });
  await page.goto('/roommates?id=new');
  await page.getByLabel(t('Аудан не жатақхана', 'Район или общежитие')).fill('Есіл ауданы');
  await page.getByLabel(t('Өзім туралы (міндетті емес)', 'О себе (необязательно)')).fill('жазыңыз @my_handle');
  await expect(page.locator('p[role=alert]')).toContainText('@username');
  await page.getByRole('button', { name: t('Жариялау', 'Опубликовать'), exact: true }).click();
  expect(calls.filter(c => c.method === 'POST')).toHaveLength(0);
  await fits(page);
});
