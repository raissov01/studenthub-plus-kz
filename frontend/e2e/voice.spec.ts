import type { Page } from '@playwright/test';
import { test, expect, onboard, fits, label } from './fixtures';
import { config } from './content';
import type { Lang } from '../src/lib/types';

type Call = { method: string; path: string; body: unknown };

async function as(page: Page, lang: Lang, role: string, features: Record<string, boolean>) {
  await page.route('**/api/config', route => route.fulfill({ json: { ...config, features: { ...config.features, ...features } } }));
  await page.route('**/api/me', route => route.fulfill({ json: { user: { id: 501, name: 'Аружан Тест', role, lang, consentAt: '2026-01-01', isAdult: true, university: 'KATU', faculty: 'it', course: 2 }, needsConsent: false, inboxUnread: 0 } }));
}

function recorder(page: Page, glob: string, handler: (c: Call, url: URL) => unknown | Promise<unknown>) {
  const calls: Call[] = [];
  void page.route(glob, async route => {
    const req = route.request();
    const url = new URL(req.url());
    const c = { method: req.method(), path: url.pathname, body: req.postData() ? (() => { try { return req.postDataJSON(); } catch { return req.postData(); } })() : null };
    calls.push(c);
    const out = await handler(c, url) as { status?: number; json: unknown } | undefined;
    if (!out) return route.fulfill({ status: 501, json: { error: { code: 'unmocked_voice' } } });
    return route.fulfill({ status: out.status ?? 200, json: out.json });
  });
  return calls;
}

const survey = {
  id: 12, university: 'KATU', title: 'Асхана сапасы', intro: '', anonymous: true, status: 'published', opensAt: null, closesAt: '2026-12-01T18:59:00Z',
  createdAt: '2026-11-01T00:00:00Z', publishedAt: '2026-11-01T00:00:00Z', responses: 0, audience: { faculties: [], courses: [], groups: [] },
  questions: [
    { id: 'q1', type: 'single', text: 'Асханада жиі тамақтанасыз ба?', required: true, options: ['Күн сайын', 'Сирек'] },
    { id: 'q2', type: 'scale', text: 'Жалпы баға', required: true, max: 5, minLabel: 'Нашар', maxLabel: 'Жақсы' },
    { id: 'q3', type: 'text', text: 'Пікір', required: false },
  ],
};

test('surveys: one question per screen, progress, submit once, then results for staff with k-anonymity', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await as(page, lang, 'student', { surveys: true, points: false });
  let done = false;
  const calls = recorder(page, '**/api/surveys**', c => {
    if (c.method === 'GET' && c.path === '/api/surveys') return { json: { surveys: [{ ...survey, done }] } };
    if (c.method === 'GET' && c.path === '/api/surveys/12') return { json: { survey: { ...survey, done } } };
    if (c.method === 'POST' && c.path === '/api/surveys/12/responses') { done = true; return { status: 201, json: { ok: true, award: null } }; }
  });
  await onboard(page, lang);
  await page.goto('/surveys');
  await expect(page.getByRole('heading', { name: 'Асхана сапасы' })).toBeVisible();
  await expect(page.getByText(t('Анонимді', 'Анонимно'), { exact: true })).toBeVisible();
  await page.getByRole('button', { name: t('Бастау', 'Начать'), exact: true }).click();
  await expect(page).toHaveURL(/\/surveys\?id=12$/);
  await expect(page.getByText(t('Анонимді сауалнама', 'Анонимный опрос'), { exact: false })).toBeVisible();
  await page.getByRole('button', { name: new RegExp(t('Бастау', 'Начать')) }).click();

  const bar = page.getByRole('progressbar', { name: t('Сауалнама прогресі', 'Прогресс опроса') });
  await expect(bar).toHaveAttribute('aria-valuenow', '33');
  const next = page.getByRole('button', { name: t('Келесі', 'Далее'), exact: true });
  await expect(next).toBeDisabled(); // required
  await page.getByRole('radio', { name: 'Сирек' }).check();
  await next.click();
  await expect(bar).toHaveAttribute('aria-valuenow', '67');
  await page.getByRole('radio', { name: '4', exact: true }).check();
  await next.click();
  await expect(page.getByText(t('Міндетті емес', 'Необязательный'), { exact: false })).toBeVisible();
  await page.getByRole('textbox').fill('Кезек ұзақ');
  await fits(page);
  await page.getByRole('button', { name: t('Жіберу', 'Отправить'), exact: true }).click();
  await expect(page.getByRole('heading', { name: t('Рақмет! Жауабыңыз жіберілді.', 'Спасибо! Ответ отправлен.') })).toBeVisible();
  const post = calls.find(c => c.method === 'POST')!;
  expect(post.body).toEqual({ answers: { q1: 1, q2: 4, q3: 'Кезек ұзақ' } });
  await page.goto('/surveys');
  await expect(page.getByText(t('Жауап берілді', 'Ответ отправлен'), { exact: false })).toBeVisible();
  await expect(page.getByRole('button', { name: t('Бастау', 'Начать'), exact: true })).toHaveCount(0);
});

test('surveys: staff results hide small groups and offer aggregate CSV', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await as(page, lang, 'university_staff', { surveys: true });
  const results = (by: string) => ({
    survey: { ...survey, responses: 7 }, responses: 7, minGroup: 5, hidden: false, by: by || undefined, breakdownHidden: by === 'faculty',
    questions: [
      { id: 'q1', type: 'single', text: survey.questions[0].text, answered: 7, hidden: false, labels: ['Күн сайын', 'Сирек'], counts: [5, 2] },
      { id: 'q2', type: 'scale', text: 'Жалпы баға', answered: 7, hidden: false, labels: ['1', '2', '3', '4', '5'], counts: [0, 1, 1, 3, 2], mean: 3.9 },
      { id: 'q3', type: 'text', text: 'Пікір', answered: 3, hidden: true },
    ],
  });
  recorder(page, '**/api/staff/surveys**', (c, url) => {
    if (c.path === '/api/staff/surveys') return { json: { surveys: [{ ...survey, responses: 7 }], university: 'KATU', admin: false } };
    if (c.path === '/api/staff/surveys/12/results') return { json: results(url.searchParams.get('by') || '') };
  });
  await onboard(page, lang);
  await page.goto('/voice');
  await expect(page.getByText(t('7 жауап', '7 ответ.'))).toBeVisible();
  await page.getByRole('button', { name: t('Нәтиже', 'Результаты') }).click();
  await expect(page.getByText(t('Орташа: ', 'Среднее: '), { exact: false })).toContainText('3.9');
  await expect(page.getByText('5 · 71%')).toBeVisible();
  await expect(page.getByText(t('Жасырылды: 5-тен аз жауап (3).', 'Скрыто: меньше 5 ответов (3).'))).toBeVisible();
  await expect(page.getByRole('button', { name: /CSV/ })).toBeVisible();
  await page.getByRole('tab', { name: t('Факультет', 'Факультет') }).click();
  await expect(page.getByText(t('Бұл бөлініс жасырылды', 'Эта разбивка скрыта'), { exact: false })).toBeVisible();
  await fits(page);
  await page.screenshot({ path: test.info().outputPath('voice-results.png'), fullPage: true });
});

test('feedback: anonymous ticket — threat notice first, device code, staff reply read back with the code', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await as(page, lang, 'student', { feedback: true });
  const token = 'Tk_' + 'a'.repeat(40);
  let replied = false;
  const ticket = () => ({
    id: 41, recipient: { id: 3, key: 'dorm', nameKk: 'Жатақхана', nameRu: 'Общежитие', slaHours: 48, active: true }, category: 'complaint',
    body: 'Үш күн бойы ыстық су жоқ.', hasPhoto: false, anonymous: true, status: replied ? 'answered' : 'new', unread: replied,
    slaDueAt: '2026-11-07T00:00:00Z', overdue: false, createdAt: '2026-11-05T05:00:00Z', updatedAt: '2026-11-05T06:00:00Z',
    messages: replied ? [{ fromStaff: true, body: 'Ертең жөндейміз.', createdAt: '2026-11-05T06:00:00Z' }] : [],
  });
  const calls = recorder(page, '**/api/feedback/**', c => {
    if (c.path === '/api/feedback/recipients') return { json: { recipients: [{ id: 3, key: 'dorm', nameKk: 'Жатақхана', nameRu: 'Общежитие', slaHours: 48, active: true }], categories: [], university: 'KATU' } };
    if (c.path === '/api/feedback/tickets') return { status: 201, json: { id: 41, status: 'new', held: false, flag: '', token } };
    if (c.path === '/api/feedback/mine') return { json: { tickets: [] } };
    if (c.path === '/api/feedback/lookup') { replied = true; return { json: { tickets: [{ token, ticket: ticket() }] } }; }
    if (c.path === '/api/feedback/tickets/41/read') return { json: { ok: true } };
    if (c.path === '/api/feedback/tickets/41/messages') return { status: 201, json: { ok: true } };
  });
  await onboard(page, lang);
  await page.goto('/feedback');
  await page.getByLabel(t('Кімге', 'Кому')).selectOption({ label: t('Жатақхана', 'Общежитие') });
  await page.getByRole('button', { name: t('Шағым', 'Жалоба'), exact: true }).click();
  const send = page.getByRole('button', { name: t('Жіберу', 'Отправить'), exact: true });
  // a threat shows the escalation notice with 112 BEFORE sending, and sending waits for the acknowledgement
  await page.getByLabel(t('Мәтін', 'Текст')).fill('Көршім мені өлтіремін деп қорқытты.');
  const alert = page.getByRole('alert').filter({ hasText: '112' });
  await expect(alert).toBeVisible();
  await expect(alert.getByRole('link', { name: /112/ })).toHaveAttribute('href', 'tel:112');
  await expect(send).toBeDisabled();
  await page.getByLabel(t('Мәтін', 'Текст')).fill('Үш күн бойы ыстық су жоқ.');
  await expect(alert).toHaveCount(0);
  await expect(page.getByRole('radio', { name: t('Анонимді', 'Анонимно') })).toBeChecked();
  await fits(page);
  await send.click();

  const sheet = page.getByRole('dialog');
  await expect(sheet.getByLabel(t('Өтініш коды', 'Код обращения'))).toHaveText(token);
  const create = calls.find(c => c.path === '/api/feedback/tickets')!;
  expect(create.body).toEqual({ recipientId: 3, category: 'complaint', body: 'Үш күн бойы ыстық су жоқ.', anonymous: true, ack: false });
  expect(await page.evaluate(() => JSON.parse(localStorage.getItem('studenthub.feedback.v1') || '[]')[0]?.token)).toBe(token);
  await sheet.getByRole('button', { name: t('Жақсы', 'Понятно') }).click();

  // «my tickets» asks with the device code only, shows the staff reply, and the student answers back with the code
  await expect(page.getByText(t('Жаңа жауап', 'Новый ответ'))).toBeVisible();
  const look = calls.find(c => c.path === '/api/feedback/lookup')!;
  expect(look.body).toEqual({ tokens: [token] });
  await page.getByRole('button', { name: new RegExp(t('Жатақхана', 'Общежитие')) }).click();
  await expect(page.getByText('Ертең жөндейміз.')).toBeVisible();
  await expect.poll(() => calls.find(c => c.path === '/api/feedback/tickets/41/read')?.body).toEqual({ token });
  await page.getByLabel(t('Жауап жазу', 'Написать ответ')).fill('Рақмет!');
  await page.getByRole('button', { name: t('Жіберу', 'Отправить'), exact: true }).click();
  await expect.poll(() => calls.find(c => c.path === '/api/feedback/tickets/41/messages')?.body).toEqual({ token, body: 'Рақмет!' });
  await fits(page);
});

test('wellbeing: «I need help now» shows emergency numbers first; contacts empty state; mood stays on the device', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await page.route('**/api/config', route => route.fulfill({ json: { ...config, features: { ...config.features, wellbeing: true } } }));
  const calls = recorder(page, '**/api/wellbeing/**', c => (c.path === '/api/wellbeing/contacts' ? { json: { contacts: [] } } : undefined));
  await onboard(page, lang);
  await page.goto('/wellbeing');
  await expect(page.getByText(t('Университет психологының байланыстары әлі қосылмаған', 'Контакты психолога университета пока не добавлены'), { exact: false })).toBeVisible();
  await expect(page.getByText(t('диагноз қоймайды', 'не ставит диагнозов'), { exact: false })).toBeVisible();
  await page.getByRole('button', { name: t('Маған қазір көмек керек', 'Мне нужна помощь сейчас') }).click();
  const sheet = page.getByRole('dialog');
  const urgent = sheet.getByRole('list', { name: t('Шұғыл нөмірлер', 'Экстренные номера') });
  await expect(urgent.getByRole('link').first()).toHaveAttribute('href', 'tel:112');
  const order = await sheet.getByRole('link').evaluateAll(els => els.map(e => e.textContent));
  expect(order.slice(0, 4)).toEqual(['112', '103', '102', '101']);
  await expect(sheet.getByRole('link', { name: /150/ })).toBeVisible();
  await fits(page);
  await sheet.getByRole('button', { name: t('Жабу', 'Закрыть') }).click();
  await page.getByRole('button', { name: t('Орташа', 'Средне'), exact: true }).click();
  await expect(page.getByRole('button', { name: t('Орташа', 'Средне'), exact: true })).toHaveAttribute('aria-pressed', 'true');
  expect(await page.evaluate(() => JSON.parse(localStorage.getItem('studenthub.mood.v1') || '[]').length)).toBe(1);
  expect(calls.every(c => c.method === 'GET' && c.path === '/api/wellbeing/contacts')).toBe(true); // nothing about mood leaves the device
  await page.evaluate(() => {
    const s = JSON.parse(localStorage.getItem('studenthub.v1') || '{}');
    localStorage.setItem('studenthub.v1', JSON.stringify({ ...s, theme: 'dark' }));
  });
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  const bg = await page.locator('#helpNow').evaluate(el => getComputedStyle(el).backgroundImage);
  expect(bg).toBe('none');
  await fits(page);
  await page.screenshot({ path: test.info().outputPath('wellbeing-dark.png'), fullPage: true });
});
