/* Specs 04 (grades), 06 (my applications), 08 (kind streak), 10 (career-fair booths): the acceptance tests of
   /home/midoriya/replica-studenthub/specs, against the production build with mocked API answers. */
import type { Page } from '@playwright/test';
import { test, expect, onboard, fits, label, deviceState } from './fixtures';
import { config, content, experiencedJob } from './content';
import type { Lang } from '../src/lib/types';

const user = (lang: Lang) => ({ id: 501, name: 'Аружан Тест', role: 'student', lang, consentAt: '2026-01-01', isAdult: true, university: 'KATU', faculty: 'IT' });

async function signedIn(page: Page, lang: Lang, features: Record<string, boolean>) {
  await page.route('**/api/config', route => route.fulfill({ json: { ...config, features: { ...config.features, ...features } } }));
  await page.route('**/api/me', route => route.fulfill({ json: { user: user(lang), needsConsent: false, inboxUnread: 0 } }));
}

/* ---------- 04 ---------- */
test('grades: RK → admission rating, «what do I need», exam → final and letter, absences, GPA fill — no network', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const api: string[] = [];
  page.on('request', r => { const p = new URL(r.url()).pathname; if (p.startsWith('/api/') && !/^\/api\/(config|content|me|events|analytics|client-errors)$/.test(p)) api.push(p); });
  await onboard(page, lang);
  await page.goto('/grades');
  await page.getByLabel(t('Пән қосу', 'Добавить предмет')).fill('Экология негіздері');
  await page.getByRole('button', { name: t('Қосу', 'Добавить'), exact: true }).click();
  await page.locator('#gradeList').getByRole('button', { name: /Экология негіздері/ }).click();
  await expect(page.getByRole('heading', { name: 'Экология негіздері' })).toBeVisible();
  await expect(page.getByText(t('Өз есебіңіз, ресми журнал емес', 'Ваш личный учёт, не официальный журнал'), { exact: true })).toBeVisible();

  // 1. RK1 78, RK2 85 → R 81.5
  await page.getByLabel(t('АБ1 (РК1)', 'РК1')).fill('78');
  await page.getByLabel(t('АБ2 (РК2)', 'РК2')).fill('85');
  await expect(page.locator('#gradeR')).toHaveText('81.5');
  // 2. A is out of reach, the next reachable letter is offered
  const need = page.locator('#gradeNeedTable');
  await expect(need.getByRole('row', { name: /^A / })).toContainText(t('жетпейді', 'недостижимо'));
  await expect(need.getByRole('row', { name: /^B\+/ })).toContainText('90.3');
  await expect(page.getByText(t('Ең жақын қолжетімді баға — B+', 'Ближайшая достижимая оценка — B+'))).toBeVisible();
  // validation: out of range is refused with a message
  await page.getByLabel(t('Емтихан', 'Экзамен'), { exact: true }).fill('120');
  await expect(page.locator('#gExamErr')).toContainText(t('0–100', 'От 0 до 100'));
  await page.getByLabel(t('Емтихан', 'Экзамен'), { exact: true }).fill('85');
  await expect(page.locator('#gradeF')).toHaveText('82.9 · B');

  // absences against a limit
  await page.getByLabel(t('Рұқсат етілген шек', 'Допустимый лимит')).fill('2');
  await page.getByLabel(t('Рұқсат етілген шек', 'Допустимый лимит')).blur();
  await page.getByRole('button', { name: t('+1 жоқ', '+1 пропуск') }).click();
  await page.getByRole('button', { name: t('+1 жоқ', '+1 пропуск') }).click();
  await expect(page.locator('#gradeAbsCount')).toHaveText('2 / 2');
  await expect(page.locator('#gradeAbsLimit')).toBeVisible();
  await fits(page);

  // 3. R 45 → not admitted, 5 points short
  await page.getByLabel(t('АБ1 (РК1)', 'РК1')).fill('40');
  await page.getByLabel(t('АБ2 (РК2)', 'РК2')).fill('50');
  await expect(page.locator('#gradeState')).toContainText(t('5 балл жетпейді', 'не хватает 5 балла'));
  await page.getByLabel(t('АБ1 (РК1)', 'РК1')).fill('78');
  await page.getByLabel(t('АБ2 (РК2)', 'РК2')).fill('85');
  await expect(page.locator('#gradeF')).toHaveText('82.9 · B');

  // 5. GPA «fill from grades» sets the letter of this subject only
  await page.evaluate(() => {
    const s = JSON.parse(localStorage.getItem('studenthub.v1')!);
    s.gpaPlan = [{ n: 'Философия', cr: 3, g: 'A' }];
    localStorage.setItem('studenthub.v1', JSON.stringify(s));
  });
  await page.goto('/gpa');
  await page.getByRole('button', { name: t('Бағалардан толтыру', 'Заполнить из оценок') }).click();
  const st = await deviceState(page);
  expect(st.gpaPlan).toEqual([{ n: 'Философия', cr: 3, g: 'A' }, { n: 'Экология негіздері', cr: 5, g: 'B' }]);
  expect(st.scores[0]).toMatchObject({ rk1: 78, rk2: 85, exam: 85, absences: 2, absenceLimit: 2, source: 'self' });
  // 6. nothing was sent anywhere
  expect(api).toEqual([]);
});

test('grades: an old stored state without scores loads with an empty list', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await onboard(page, lang);
  await page.evaluate(() => {
    const s = JSON.parse(localStorage.getItem('studenthub.v1')!);
    delete s.scores; delete s.applications;
    localStorage.setItem('studenthub.v1', JSON.stringify(s));
  });
  await page.goto('/grades');
  await expect(page.locator('#gradeFormula')).toContainText(t('расталмаған', 'не подтверждена'));
  await page.getByLabel(t('Пән қосу', 'Добавить предмет')).fill('Тарих');
  await page.getByRole('button', { name: t('Қосу', 'Добавить'), exact: true }).click();
  await page.locator('#gradeList').getByRole('button', { name: /Тарих/ }).click();
  await page.getByRole('button', { name: t('+1 жоқ', '+1 пропуск') }).click();
  expect((await deviceState(page)).scores).toHaveLength(1);
});

/* ---------- 06 ---------- */
const external = { ...experiencedJob, id: 'job-102', source: undefined, employer: undefined, apply: undefined };

test('applications: mark an external job, follow-up in the calendar, old marks migrate', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await page.route('**/api/content', route => route.fulfill({ json: { ...content, jobs: [content.jobs[0], external] } }));
  await onboard(page, lang);
  // 4. an old S.applied mark (no stage, no title) is kept
  await page.evaluate(() => {
    const s = JSON.parse(localStorage.getItem('studenthub.v1')!);
    s.applied = { 'job-101': 1767225600000 };
    delete s.applications;
    localStorage.setItem('studenthub.v1', JSON.stringify(s));
  });
  await page.goto('/job/job-102');
  // 1. «Өтінім бердім» → today's row in stage «sent»; the button now says so
  await page.getByRole('button', { name: t('Өтінім бердім', 'Я откликнулся') }).click();
  const sheet = page.getByRole('dialog');
  await sheet.getByLabel(t('Жазба (міндетті емес)', 'Заметка (необязательно)')).fill('HR: Айгүл');
  await sheet.getByRole('button', { name: t('Сақтау', 'Сохранить') }).click();
  await expect(page.getByRole('button', { name: t('Өтінім берілді', 'Отклик отмечен') })).toBeVisible();

  await page.goto('/applications');
  const row = page.locator('article[data-ref="job:job-102"]');
  await expect(row).toContainText(t('Жіберілді', 'Отправлено'));
  await expect(row).toContainText('HR: Айгүл');
  const old = page.locator('article[data-ref="job:job-101"]');
  await expect(old).toContainText(t('Кітапхана көмекшісі', 'Помощник библиотеки'));

  // 2. a follow-up date shows in the calendar on that day
  const day = new Date(Date.now() + 5 * 3600_000 + 7 * 86400_000).toISOString().slice(0, 10);
  await row.getByRole('button', { name: t('Өзгерту', 'Изменить') }).click();
  await sheet.getByRole('button', { name: t('Сұхбат', 'Собеседование') }).click();
  await sheet.getByLabel(t('Қашан хабарласу керек', 'Когда связаться')).fill(day);
  await sheet.getByRole('button', { name: t('Сақтау', 'Сохранить') }).click();
  await expect(row).toContainText(t('Сұхбат', 'Собеседование'));
  await expect(row).toContainText(t('Келесі қадам', 'Следующий шаг'));
  await fits(page);
  const st = await deviceState(page);
  expect(st.applications.find((a: { ref: string }) => a.ref === 'job:job-102')).toMatchObject({ stage: 'interview', followUp: day, note: 'HR: Айгүл' });
  expect(st.applied['job-101']).toBe(1767225600000);

  await page.goto('/calendar');
  await page.locator(`button[data-ymd="${day}"]`).click();
  await expect(page.getByText(t('Өтінім · хабарласу', 'Отклик · связаться'))).toBeVisible();
});

test('applications: an in-app application keeps the server stage', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang, { b2b: true });
  await page.route('**/api/applications/my', route => route.fulfill({ json: { applications: [
    { id: 7, jobId: 'job-101', title: 'Кітапхана көмекшісі', titleRu: 'Помощник библиотеки', company: 'Campus', status: 'viewed', inviteMessage: null, sharedFields: {}, createdAt: '2026-11-01T05:00:00Z', updatedAt: '2026-11-02T05:00:00Z' },
  ] } }));
  await onboard(page, lang);
  await page.goto('/applications');
  const row = page.locator('article[data-ref="job:job-101"]');
  // 3. «viewed» reads as such and the stage cannot be moved here
  await expect(row).toContainText(t('Қаралды', 'Просмотрен'));
  await row.getByRole('button', { name: t('Өзгерту', 'Изменить') }).click();
  const sheet = page.getByRole('dialog');
  await expect(sheet.getByText(t('Сатысын жұмыс беруші жаңартады', 'Стадию обновляет работодатель'))).toBeVisible();
  await expect(sheet.getByRole('button', { name: t('Ұсыныс', 'Оффер') })).toHaveCount(0);
  await sheet.getByLabel(t('Жазба', 'Заметка'), { exact: true }).fill('шақыру күтемін');
  await sheet.getByRole('button', { name: t('Сақтау', 'Сохранить') }).click();
  await expect(row).toContainText('шақыру күтемін');
  await expect(row).toContainText(t('Қаралды', 'Просмотрен'));
});

/* ---------- 08 ---------- */
const streakSummary = (over: Record<string, unknown> = {}) => ({
  xp: 150, level: 2, levelXp: 100, nextLevelXp: 300, coins: 58,
  streak: {
    days: 7, best: 7, freezes: 0, maxFreezes: 2, freezeCost: 20, activeToday: true, nextMilestone: 14, lastMilestone: 7,
    milestones: [3, 7, 14, 30, 50, 100, 200, 365], pause: { active: false, from: null, until: null, daysLeft: 0, usedThisSemester: false, maxDays: 14 },
    reminder: { on: true, time: '20:00' }, ...over,
  },
  today: { xp: 5, coins: 1, xpCap: 150, coinsCap: 30 }, tasks: [], settings: { leaderboardOptOut: false, studyGroup: null }, recent: [],
});

test('streak: milestone sheet once, next milestone, session pause and the reminder switch', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang, { points: true, streak: true });
  // the milestone is reached only after onboarding, so the onboarding dialog and the sheet do not overlap
  let me = streakSummary({ lastMilestone: 0 });
  const calls: { method: string; path: string; body: unknown }[] = [];
  await page.route('**/api/points/**', async route => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    const body = req.postData() ? req.postDataJSON() : null;
    calls.push({ method: req.method(), path, body });
    switch (`${req.method()} ${path}`) {
      case 'POST /api/points/daily': return route.fulfill({ json: { award: null, me } });
      case 'GET /api/points/me': return route.fulfill({ json: me });
      case 'GET /api/points/leaderboard': return route.fulfill({ json: { scope: 'group', status: 'no_scope', minMembers: 5, optedOut: false, weekStart: '2026-11-01T19:00:00Z', resetsAt: '2026-11-08T19:00:00Z' } });
      case 'POST /api/points/pause':
        me = streakSummary({ pause: { active: true, from: '2026-11-06', until: '2026-11-15', daysLeft: 10, usedThisSemester: true, maxDays: 14 } });
        return route.fulfill({ json: { award: null, me } });
      case 'PATCH /api/points/streak':
        me = streakSummary({ reminder: { on: (body as { reminder?: boolean }).reminder ?? true, time: '20:00' } });
        return route.fulfill({ json: { award: null, me } });
    }
    return route.fulfill({ status: 501, json: { error: { code: 'unmocked_points' } } });
  });
  await onboard(page, lang);
  me = streakSummary();
  await page.goto('/');
  // 3. the milestone sheet shows once
  const sheet = page.getByRole('dialog');
  await expect(sheet.getByRole('heading', { name: t('7 күн қатарынан!', '7 дней подряд!') })).toBeVisible();
  await sheet.getByRole('button', { name: t('Жалғастыру', 'Продолжить') }).click();
  await page.goto('/points');
  await expect(page.locator('#streakNext')).toContainText('14');
  await expect(page.getByRole('heading', { name: t('7 күн қатарынан!', '7 дней подряд!') })).toHaveCount(0);
  // 4. session pause: on, said in words
  await page.getByLabel(t('Үзіліс ұзақтығы', 'Длительность паузы')).selectOption('10');
  await page.getByRole('button', { name: t('Сессия режимін қосу', 'Включить режим сессии') }).click();
  await expect(page.locator('#streakPaused')).toContainText(t('Үзілісте', 'На паузе'));
  expect(calls.find(c => c.path === '/api/points/pause')?.body).toEqual({ days: 10 });
  // the evening reminder can be switched off
  await page.locator('#streakRemind').click();
  await expect(page.locator('#streakRemind')).not.toBeChecked();
  expect(calls.find(c => c.path === '/api/points/streak')?.body).toEqual({ reminder: false });
  await fits(page);
});

test('streak: switch off shows none of it', async ({ page, lang }) => {
  await signedIn(page, lang, { points: true });
  const plain = streakSummary();
  const { nextMilestone: _n, lastMilestone: _l, milestones: _m, pause: _p, reminder: _r, ...rest } = plain.streak;
  void _n; void _l; void _m; void _p; void _r;
  const me = { ...plain, streak: { ...rest, maxFreezes: 1 } };
  await page.route('**/api/points/**', route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/points/leaderboard') return route.fulfill({ json: { scope: 'group', status: 'no_scope', minMembers: 5, optedOut: false, weekStart: '2026-11-01T19:00:00Z', resetsAt: '2026-11-08T19:00:00Z' } });
    return route.fulfill({ json: path === '/api/points/daily' ? { award: null, me } : me });
  });
  await onboard(page, lang);
  await page.goto('/points');
  await expect(page.locator('#ptsStreak')).toHaveText('7');
  await expect(page.locator('#streakExtras')).toHaveCount(0);
  await expect(page.getByRole('dialog')).toHaveCount(0);
});

/* ---------- 10 ---------- */
const fairEvent = {
  id: 55, title_kk: 'Мансап жәрмеңкесі', title_ru: 'Ярмарка вакансий', desc_kk: null, desc_ru: null, cat: 'career',
  starts_at: '2026-11-05T04:00:00Z', ends_at: '2099-11-05T09:00:00Z', university: 'KATU', building_id: null, place_text: 'Бас корпус',
  capacity: null, walk_in_allowed: true, gives_cert: false, teams_enabled: false, status: 'published', created_by: 1, checkin_started_at: null,
  finished_at: null, created_at: '2026-11-01T00:00:00Z', registered: 3, waitlist: 0, checked_in: 0, spots_left: null, can_manage: false,
};

test('fairs: companies on the fair page, booth scan, already scanned, «booths I visited»', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang, { events: true, fairs: true });
  let scans = 0;
  const shares: unknown[] = [];
  await page.route('**/api/events/**', async route => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    if (path === '/api/events/55') return route.fulfill({ json: { event: fairEvent } });
    if (path === '/api/events/55/attendees') return route.fulfill({ json: { count: 3 } });
    if (path === '/api/events/55/exhibitors') return route.fulfill({ json: { exhibitors: [
      { employerId: 1, name: 'Alpha Agro', booth: 'A1', openJobs: 3 }, { employerId: 2, name: 'Beta Bank', booth: 'B2', openJobs: 0 },
    ] } });
    if (path === '/api/events/55/my-booths') return route.fulfill({ json: { booths: scans ? [{ employerId: 1, company: 'Alpha Agro', booth: 'A1', openJobs: 3, at: '2026-11-05T05:00:00Z', shared: [] }] : [] } });
    if (path === '/api/events/booth-checkin') {
      const body = req.postDataJSON();
      if (body.token === 'bt_late') return route.fulfill({ status: 409, json: { error: { code: 'fair_over', message: lang === 'ru' ? 'Ярмарка закончилась, отметка у стенда закрыта.' : 'Жәрмеңке аяқталды, стендке тіркелу жабық.' } } });
      if (body.share) shares.push(body.share);
      const status = scans++ ? 'already' : 'ok';
      return route.fulfill({ json: { status, eventId: 55, eventTitle: 'Мансап жәрмеңкесі', employerId: 1, company: 'Alpha Agro', booth: 'A1', openJobs: 3, shared: body.share ?? [] } });
    }
    return route.fallback();
  });
  await onboard(page, lang);
  await page.goto('/event/55');
  await expect(page.locator('#fairList')).toContainText('Alpha Agro');
  await expect(page.locator('#fairList')).toContainText('Beta Bank');
  await expect(page.locator('#myBooths')).toHaveCount(0);

  // C2: scan → saved; choose what the company sees
  await page.goto('/booth?b=bt_alpha');
  await expect(page.getByRole('heading', { name: t('Стенд тіркелді: Alpha Agro', 'Стенд отмечен: Alpha Agro') })).toBeVisible();
  const share = page.locator('#boothShare');
  await share.getByLabel(t('Аты-жөнім', 'Имя')).check();
  await share.getByRole('button', { name: t('Сақтау', 'Сохранить') }).click();
  await expect.poll(() => shares).toEqual([['name']]);
  // 1. a second scan of the same booth: friendly «already»
  await page.goto('/booth?b=bt_alpha');
  await expect(page.getByRole('heading', { name: t('Alpha Agro: бұрын тіркелгенсіз', 'Alpha Agro: вы уже отмечены') })).toBeVisible();
  // 4. after the fair: a clear refusal
  await page.goto('/booth?b=bt_late');
  await expect(page.getByRole('alert').filter({ hasText: t('Жәрмеңке аяқталды', 'Ярмарка закончилась') })).toBeVisible();
  await fits(page);
  // C3: the event page lists the booths I visited
  await page.goto('/event/55');
  await expect(page.locator('#myBooths')).toContainText('Alpha Agro');
});
