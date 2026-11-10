import type { Page, Route } from '@playwright/test';
import { test, expect, onboard, fits, label, deviceState } from './fixtures';
import { config } from './content';
import type { Lang } from '../src/lib/types';

/* Clubs (FEATURE_CLUBS) against a small in-memory mock of /api/clubs and the club's events. The Go side of the same
   flows (RLS, moderation, notifications) is covered by backend/internal/clubs tests. */

const base = {
  name_ru: null, summary_ru: null, about_kk: null, about_ru: null, university: 'Сейфуллин университеті', room: null,
  schedule_ru: null, leader_title_ru: null, leader_contact: null, leader_contact_public: false, telegram_url: null, instagram_url: null,
  join_mode: 'request', join_link: null, recruiting: false, status: 'active', source_url: 'https://kazatu.edu.kz/ru/page/studenceskie-kluby',
  verified_at: '2026-11-05', schedule_kk: null, leader_title_kk: null, building_id: null, summary_kk: null,
};

function mock(lang: Lang) {
  const STUDENT = { id: 501, name: 'Аружан Тест', role: 'student', lang, consentAt: '2026-01-01', isAdult: true, university: 'Сейфуллин университеті', faculty: 'IT' };
  const LEADER = { ...STUDENT, id: 777, name: 'Дана Жетекші' };
  const s = {
    user: STUDENT as typeof STUDENT,
    clubs: [
      { ...base, id: 1, name_kk: '«Шырақ» пікір-сайыс клубы', name_ru: 'Дебатный клуб «Шырақ»', direction: 'debate', building_id: 'b1', room: '214',
        schedule_kk: 'Сейсенбі, 18:00', schedule_ru: 'Вторник, 18:00', leader_title_kk: 'Төраға', leader_title_ru: 'Председатель',
        summary_kk: 'Қазақ тіліндегі дебат клубы.', summary_ru: 'Казахоязычный дебатный клуб.', recruiting: true, instagram_url: 'https://www.instagram.com/shyraq/' },
      { ...base, id: 2, name_kk: 'Спорт клубы', name_ru: 'Спортивный клуб', direction: 'sport', building_id: 'b5' },
      { ...base, id: 3, name_kk: '«Arrow crew» би тобы', direction: 'art', join_mode: 'link', join_link: 'https://t.me/arrow_crew' },
    ],
    pending: [] as number[], members: [] as number[], admins: [777],
    events: [] as Record<string, unknown>[],
    inbox: [] as { id: number; category: string; text: string; link: string; createdAt: string; readAt: null; read: boolean }[],
    calls: [] as string[],
  };
  const my = (id: number) => s.user.id === 777 && s.admins.includes(id) && id === 1 ? { status: '', admin: true }
    : s.members.includes(s.user.id) && id === 1 ? { status: 'member', admin: false }
      : s.pending.includes(s.user.id) && id === 1 ? { status: 'pending', admin: false } : undefined;
  const club = (c: typeof s.clubs[number]) => ({ ...c, members: c.id === 1 ? s.members.length : 0, my: my(c.id) });
  const handle = async (route: Route) => {
    const req = route.request();
    const url = new URL(req.url());
    const p = url.pathname, m = req.method();
    const body = req.postData() ? req.postDataJSON() : null;
    s.calls.push(`${m} ${p}`);
    if (p === '/api/config') return route.fulfill({ json: { ...config, features: { ...config.features, clubs: true, events: true } } });
    if (p === '/api/me') return route.fulfill({ json: { user: s.user, needsConsent: false, inboxUnread: s.user.id === 501 ? s.inbox.filter(i => !i.read).length : 0 } });
    if (p === '/api/me/inbox') return route.fulfill({ json: { items: s.user.id === 501 ? s.inbox : [], unread: 0, nextBefore: null } });
    if (p === '/api/me/inbox/read') { s.inbox.forEach(i => { i.read = true; }); return route.fulfill({ json: { ok: true, marked: 1, unread: 0 } }); }
    if (m === 'GET' && p === '/api/clubs') return route.fulfill({ json: { clubs: s.clubs.map(club) } });
    let r = /^\/api\/clubs\/(\d+)$/.exec(p);
    if (r && m === 'GET') {
      const c = s.clubs.find(x => x.id === Number(r![1]));
      if (!c) return route.fulfill({ status: 404, json: { error: { code: 'not_found', message: 'no' } } });
      const manage = s.user.id === 777 && c.id === 1;
      return route.fulfill({ json: { club: club(c), posts: [], can_manage: manage, ...(manage ? { pending_requests: s.pending.length } : {}) } });
    }
    if (p === '/api/clubs/1/join' && m === 'POST') {
      expect(body.message).toBe('Дебатқа қызығамын');
      s.pending.push(s.user.id);
      return route.fulfill({ json: { status: 'pending', created: true } });
    }
    if (p === '/api/clubs/1/members') {
      expect(s.user.id, 'only the club admin loads the member list').toBe(777);
      return route.fulfill({ json: { members: [
        ...s.pending.map(id => ({ user_id: id, name: 'Аружан Тест', username: null, faculty: 'IT', course: 2, status: 'pending', message: 'Дебатқа қызығамын', created_at: '2026-11-05T05:00:00Z', decided_at: null })),
        ...s.members.map(id => ({ user_id: id, name: 'Аружан Тест', username: null, faculty: 'IT', course: 2, status: 'member', message: null, created_at: '2026-11-05T05:00:00Z', decided_at: '2026-11-05T06:00:00Z' })),
      ] } });
    }
    r = /^\/api\/clubs\/1\/requests\/(\d+)$/.exec(p);
    if (r && m === 'POST') {
      expect(body.decision).toBe('approve');
      const uid = Number(r[1]);
      s.pending = s.pending.filter(x => x !== uid); s.members.push(uid);
      s.inbox.push({ id: 1, category: 'community', text: lang === 'ru' ? 'Вас приняли в клуб «Дебатный клуб «Шырақ»».' : 'Сіз ««Шырақ» пікір-сайыс клубы» клубына қабылдандыңыз.', link: '/club?id=1', createdAt: new Date().toISOString(), readAt: null, read: false });
      return route.fulfill({ json: { ok: true } });
    }
    // the club's events through the events API
    if (p === '/api/events' && m === 'GET') {
      const clubId = Number(url.searchParams.get('club'));
      return route.fulfill({ json: { events: s.events.filter(e => !clubId || e.club_id === clubId).map(e => ({ ...e, can_manage: s.user.id === 777 })) } });
    }
    if (p === '/api/events/create' && m === 'POST') {
      expect(body.club_id).toBe(1);
      expect(body.university).toBeUndefined();
      const e = {
        id: 9001, title_kk: body.title_kk, title_ru: body.title_ru, desc_kk: null, desc_ru: null, cat: body.cat, starts_at: body.starts_at, ends_at: body.ends_at,
        university: 'Сейфуллин университеті', building_id: body.building_id, place_text: body.place_text, capacity: null, walk_in_allowed: false, gives_cert: false,
        teams_enabled: false, status: body.status, created_by: 777, checkin_started_at: null, finished_at: null, created_at: new Date().toISOString(),
        registered: 0, waitlist: 0, checked_in: 0, spots_left: null, club_id: 1, is_demo: false,
      };
      s.events.push(e);
      return route.fulfill({ status: 201, json: { event: { ...e, can_manage: true } } });
    }
    if (p === '/api/events/9001') return route.fulfill({ json: { event: { ...s.events[0], can_manage: s.user.id === 777 } } });
    if (p === '/api/events/9001/attendees') return route.fulfill({ json: { count: 0 } });
    if (p === '/api/events/9001/regs') return route.fulfill({ json: { registered: 0, checked_in: 0, waitlist: 0, regs: [] } });
    return route.fallback();
  };
  return { s, handle, STUDENT, LEADER };
}

async function setup(page: Page, lang: Lang) {
  const m = mock(lang);
  await page.route('**/api/**', m.handle);
  return m;
}

test('clubs: catalogue filters → club page → join request → club admin approves → student notified', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const m = await setup(page, lang);
  await onboard(page, lang);

  // clubs live under «Іс-шаралар»; the hamburger menu no longer lists them separately
  await page.goto('/');
  await page.getByRole('button', { name: t('Мәзір', 'Меню') }).first().click();
  const menu = page.getByRole('dialog', { name: t('Мәзір', 'Меню') });
  for (const [kk, ru] of [['Клубтар', 'Клубы'], ['Сауалнамалар', 'Опросы'], ['Әл-ауқат', 'Благополучие'], ['Пәтерлес', 'Соседи по жилью'], ['Ұпайлар', 'Баллы'], ['Студент дауысы: басқару', 'Голос студентов: управление']]) {
    await expect(menu.getByRole('button', { name: t(kk, ru), exact: true })).toHaveCount(0);
  }
  await page.keyboard.press('Escape');
  await page.goto('/clubs');
  await expect(page).toHaveURL(/\/clubs$/);
  const cards = page.locator('#clubList .club-card');
  await expect(cards).toHaveCount(3);
  // the recruiting club is first and carries the badge
  await expect(cards.first()).toContainText(t('Мүше қабылдап жатыр', 'Идёт набор'));
  await page.getByRole('button', { name: t('Спорт', 'Спорт'), exact: true }).click();
  await expect(cards).toHaveCount(1);
  await expect(cards.first()).toContainText(t('Спорт клубы', 'Спортивный клуб'));
  await page.getByRole('button', { name: t('Барлығы', 'Все'), exact: true }).click();
  await page.locator('#clubBuilding').selectOption('b1');
  await expect(cards).toHaveCount(1);
  await page.locator('#clubBuilding').selectOption('all');
  await page.locator('#clubQ').fill(t('шырақ', 'ДЕБАТНЫЙ'));
  await expect(cards).toHaveCount(1);
  await fits(page);
  await cards.first().click();

  // club page (query id)
  await expect(page).toHaveURL(/\/club\?id=1$/);
  await expect(page.getByRole('heading', { name: t('«Шырақ» пікір-сайыс клубы', 'Дебатный клуб «Шырақ»') })).toBeVisible();
  await expect(page.getByText(t('Сейсенбі, 18:00', 'Вторник, 18:00'))).toBeVisible();
  await expect(page.getByRole('link', { name: /Instagram/ })).toHaveAttribute('href', 'https://www.instagram.com/shyraq/');
  await expect(page.getByText(t('Клубты басқару', 'Управление клубом'))).toHaveCount(0);
  await page.locator('#clubJoin').click();
  await page.locator('#joinMsg').fill('Дебатқа қызығамын');
  await page.getByRole('button', { name: t('Өтінім жіберу', 'Отправить заявку') }).click();
  await expect(page.locator('#clubPending')).toBeVisible();
  await fits(page);
  expect(m.s.calls).not.toContain('GET /api/clubs/1/members');

  // the club admin approves
  m.s.user = m.LEADER;
  await page.reload();
  await expect(page.locator('#clubPendingCount')).toHaveText('1');
  await page.getByRole('button', { name: t('Өтінімдер мен мүшелер', 'Заявки и участники') }).click();
  const req = page.locator('#clubRequests .card');
  await expect(req).toContainText('Дебатқа қызығамын');
  await req.getByRole('button', { name: t('Қабылдау', 'Принять') }).click();
  await expect(page.locator('.toast')).toContainText(t('Қабылданды', 'Принят'));
  expect(m.s.members).toEqual([501]);
  // the sheet reloads the list after the decision: let it settle before switching accounts
  await expect(page.locator('#clubRequests')).toHaveCount(0);
  await expect(page.getByText(t('Мүшелер', 'Участники') + ' (1)')).toBeVisible();

  // the student finds the notification and is a member now
  m.s.user = m.STUDENT;
  await page.goto('/notifications');
  await expect(page.getByText(t('клубына қабылдандыңыз', 'Вас приняли в клуб'))).toBeVisible();
  await page.goto('/club?id=1');
  await expect(page.getByText(t('Сіз клуб мүшесісіз', 'Вы участник клуба'))).toBeVisible();

  // a link-mode club opens its Telegram group instead of a request
  await page.goto('/club?id=3');
  await expect(page.locator('#clubJoin')).toHaveAttribute('href', 'https://t.me/arrow_crew');
});

test('clubs: club admin creates an event that shows on the club page and goes to the calendar', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const m = await setup(page, lang);
  m.s.user = m.LEADER;
  await onboard(page, lang);
  await page.goto('/club?id=1');
  await page.getByRole('button', { name: t('Іс-шара қосу', 'Добавить мероприятие') }).click();
  await expect(page.locator('#efUni')).toHaveCount(0);
  await page.locator('#efTitleKk').fill('Дебат турнирі');
  await page.getByRole('button', { name: t('Жариялау', 'Опубликовать'), exact: true }).click();
  await expect(page).toHaveURL(/\/organize\/9001$/);

  await page.goto('/club?id=1');
  const ev = page.locator('#clubEvents .card');
  await expect(ev).toHaveCount(1);
  await expect(ev).toContainText('Дебат турнирі');
  await ev.click();
  await expect(page).toHaveURL(/\/event\/9001$/);
  await page.getByRole('button', { name: t('Күнтізбеге қосу', 'В календарь'), exact: true }).click();
  const pins = (await deviceState(page)).calendarPins;
  expect(pins).toMatchObject([{ ref: 'event:9001', kind: 'event' }]);
  await page.locator('.toast').getByRole('button', { name: t('Ашу', 'Открыть') }).click();
  await expect(page).toHaveURL(/\/calendar$/);
  await expect(page.locator('.cal-day .cal-item', { hasText: 'Дебат турнирі' })).toBeVisible();
  await fits(page);
});

test('clubs: empty catalogue invites club leaders to apply', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const m = await setup(page, lang);
  m.s.clubs = [];
  await onboard(page, lang);
  await page.goto('/clubs');
  await expect(page.getByText(t('Клубтар әлі қосылмаған — клуб жетекшісі болсаңыз, өтінім беріңіз', 'Клубы ещё не добавлены — если вы руководитель клуба, подайте заявку'))).toBeVisible();
  await page.locator('#clubLeaderApply').click();
  await expect(page.locator('#laProof')).toBeVisible();
  await fits(page);
});

test('events: an admin picks the university from the list and is told what blocks Publish', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  const m = await setup(page, lang);
  m.s.user = { ...m.STUDENT, id: 900, role: 'admin', university: null as unknown as string };
  await onboard(page, lang);
  await page.goto('/organize/new');
  const uni = page.locator('#efUni');
  await expect(uni).toHaveValue('Сейфуллин университеті'); // an admin without a profile university starts at the pilot one
  expect(await uni.locator('option').count()).toBeGreaterThan(100);
  const publish = page.getByRole('button', { name: t('Жариялау', 'Опубликовать'), exact: true });
  await expect(publish).toBeDisabled();
  await expect(page.getByRole('status').filter({ hasText: t('Толтыру керек', 'Нужно заполнить') })).toBeVisible();
  await page.locator('#efTitleKk').fill('Карьера күні');
  await expect(publish).toBeEnabled();
  await uni.selectOption('Назарбаев Университет');
  await expect(publish).toBeEnabled();
  await page.screenshot({ path: `test-results/event-form-admin-${lang}.png`, fullPage: true });
  await fits(page);
});
