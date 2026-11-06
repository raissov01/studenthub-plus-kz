import { test, expect, type Page } from '@playwright/test';
import { config, content } from '../../e2e/content';
import { onboard, seedTimetable } from '../../e2e/fixtures';
import { quizEdit, quizMeta, quizPlay, quizResult } from './quiz-fixture';

/* The screens the laptop layout touches, at every size of the config, against a mocked API (signed-in admin, cafe,
   print and clubs switched on). Writes docs/design/desktop/<screen>-<width>.png; SHOT_ONLY=home,cafe limits the set. */
const dir = process.env.SHOT_DIR || '../docs/design/desktop';
const only = (process.env.SHOT_ONLY || '').split(',').filter(Boolean);

const user = { id: 1, name: 'Аружан Тест', role: 'admin', lang: 'kk', consentAt: '2026-01-01', isAdult: true, university: 'Сейфуллин университеті', faculty: 'IT', course: 2, email: 'a@example.org', emailVerified: true, telegramId: null, username: null, city: 'astana', createdAt: '2026-01-01' };
const cafe = { id: 3, nameKk: 'Тест кофейня', nameRu: 'Тест кофейня', building: 'Бас ғимарат', placeKk: '1-қабат', placeRu: '1 этаж', openTime: '08:00', closeTime: '20:00', openDays: [1, 2, 3, 4, 5, 6, 7], slotMinutes: 10, slotCapacity: 6, leadMinutes: 10, active: true, paymentMode: 'both', isDemo: false, openNow: true, payments: ['pickup', 'kaspi'] };
const item = (id: number, categoryId: number, nameKk: string, price: number, descKk = '') => ({ id, cafeId: 3, categoryId, nameKk, nameRu: nameKk, descKk, descRu: descKk, price, available: true, archived: false, sort: id, options: [] });
const menu = { cafe, categories: [{ id: 1, nameKk: 'Кофе', nameRu: 'Кофе', sort: 1, active: true }, { id: 2, nameKk: 'Тағамдар', nameRu: 'Еда', sort: 2, active: true }],
  items: [item(11, 1, 'Латте', 100000, 'Сүтті'), item(12, 1, 'Капучино', 90000, 'Көпіршікті'), item(13, 1, 'Американо', 70000), item(14, 2, 'Круассан', 60000), item(15, 2, 'Сэндвич', 120000, 'Тауық етімен')],
  slots: [{ at: '2026-11-05T07:20:00Z', full: false }, { at: '2026-11-05T07:40:00Z', full: false }] };
const shop = { id: 4, nameKk: 'Тест полиграфия', nameRu: 'Тест полиграфия', building: 'Бас ғимарат', placeKk: '1-қабат, 104', placeRu: '1 этаж, 104', openTime: '08:00', closeTime: '20:00', openDays: [1, 2, 3, 4, 5, 6, 7], slotMinutes: 15, leadMinutes: 20, minOrder: 0, accepts: ['pdf', 'docx', 'jpg', 'png'], retentionDays: 3, active: true, paymentMode: 'both', noteKk: '', noteRu: '', openNow: true, payments: ['pickup', 'kaspi'] };
const printSvc = { id: 21, shopId: 4, kind: 'print', nameKk: 'Басып шығару', nameRu: 'Печать', descKk: '', descRu: '', online: true, active: true, sort: 0, pricing: { sizes: { A4: { bw: 2000, color: 10000, bwDuplex: 1500, colorDuplex: 9000 } } } };
const catalog = { shop, services: [printSvc, { ...printSvc, id: 22, kind: 'binding', nameKk: 'Түптеу', nameRu: 'Переплёт', pricing: { tiers: [{ maxSheets: 100, price: 50000 }] } }], goods: [{ id: 31, shopId: 4, nameKk: 'Папка', nameRu: 'Папка', price: 30000, inStock: true, archived: false, sort: 0 }], slots: [{ at: '2026-11-05T09:00:00Z' }], limits: { maxFileBytes: 20971520, maxFiles: 5, dailyFiles: 30, dailyBytes: 209715200 } };
const clubBase = { name_ru: null, summary_ru: null, about_kk: null, about_ru: null, university: 'Сейфуллин университеті', room: null, schedule_ru: null, leader_title_ru: null, leader_contact: null, leader_contact_public: false, telegram_url: null, instagram_url: null, join_mode: 'request', join_link: null, recruiting: false, status: 'active', source_url: 'https://kazatu.edu.kz', verified_at: '2026-11-05', schedule_kk: null, leader_title_kk: null, building_id: null, summary_kk: null, members: 0 };
const clubs = [
  { ...clubBase, id: 1, name_kk: '«Шырақ» пікір-сайыс клубы', direction: 'debate', room: '214', schedule_kk: 'Сейсенбі, 18:00', summary_kk: 'Қазақ тіліндегі дебат клубы.', recruiting: true },
  { ...clubBase, id: 2, name_kk: 'Спорт клубы', direction: 'sport', summary_kk: 'Футбол, волейбол, жүзу.' },
  { ...clubBase, id: 3, name_kk: '«Arrow crew» би тобы', direction: 'art', join_mode: 'link', join_link: 'https://t.me/arrow_crew' },
  { ...clubBase, id: 4, name_kk: 'IT клубы', direction: 'it', summary_kk: 'Хакатондар мен жобалар.' },
];

/* mutable mock state: how many cafés and print shops the API lists (one place → the laptop shows it as a workspace) */
const state = { cafes: 1, shops: 1 };
const cafes = () => [
  cafe,
  { ...cafe, id: 5, nameKk: 'Coffee Point', nameRu: 'Coffee Point', building: '4-корпус', placeKk: 'Фойе', placeRu: 'Фойе', openNow: false, openTime: '09:00', closeTime: '17:00', openDays: [1, 2, 3, 4, 5], payments: ['pickup'] },
  { ...cafe, id: 6, nameKk: 'Асхана-кофейня «Дәм»', nameRu: 'Столовая-кофейня «Дәм»', building: 'Жатақхана №3', placeKk: '1-қабат', placeRu: '1 этаж' },
].slice(0, state.cafes);
const shops = () => [shop, { ...shop, id: 7, nameKk: 'Copy Center', nameRu: 'Copy Center', building: '2-корпус', placeKk: '012', placeRu: '012', openNow: false, leadMinutes: 40, accepts: ['pdf'] }].slice(0, state.shops);
const at = (days: number, h: number) => { const d = new Date(); d.setDate(d.getDate() + days); d.setHours(h, 0, 0, 0); return d.toISOString(); };
const evBase = {
  desc_ru: null, title_ru: null, university: 'Сейфуллин университеті', building_id: 'b1', capacity: 60, walk_in_allowed: true, gives_cert: true,
  teams_enabled: false, status: 'published', created_by: 9, checkin_started_at: null, finished_at: null, created_at: '2026-10-01', registered: 34,
  waitlist: 0, checked_in: 0, spots_left: 26, can_manage: false, is_demo: false, club_id: 1,
};
const events = [
  { ...evBase, id: 9001, title_kk: 'Жастар форумы: цифрлық экономикадағы мансап', cat: 'career', starts_at: at(3, 15), ends_at: at(3, 18), place_text: 'Бас ғимарат, акт залы',
    desc_kk: 'Жұмыс берушілер мен түлектер цифрлық экономикадағы мамандықтар туралы айтады.\n\nБағдарлама: панельдік талқылау, нетворкинг, түйіндеме кеңесі.' },
  { ...evBase, id: 9002, title_kk: 'Дебат турнирі', cat: 'youth', starts_at: at(6, 18), ends_at: at(6, 20), place_text: '214 аудитория', desc_kk: null, gives_cert: false },
  { ...evBase, id: 9003, title_kk: 'Python шеберханасы', cat: 'training', starts_at: at(9, 14), ends_at: at(9, 17), place_text: 'IT-хаб', desc_kk: null },
];
const market = (id: number, title: string, over: Record<string, unknown> = {}) => ({ id, title, desc: 'Жақсы күйде, тек алып кетіңіз.', category: 'books', condition: 'good', university: 'Сейфуллин университеті', photos: [], status: 'active', createdAt: '2026-11-01', expiresAt: '2026-12-01', daysAgo: 2, mine: false, pickup: 'Бас ғимарат, 1-қабат', pickupHint: '', author: { name: 'Дана', username: 'dana', telegramUrl: 'https://t.me/dana' }, ...over });
const lfPost = (over: Record<string, unknown> = {}) => ({ id: 7, kind: 'found', category: 'keys', title: 'Көк брелокты кілттер', desc: 'Кітапханада үстел үстінде қалыпты.', building: 'b1', room: '1-305', happenedOn: '2026-11-04', photo: null, handedTo: '', university: 'KATU', status: 'active', createdAt: '2026-11-04T05:00:00Z', expiresAt: '2026-12-04T05:00:00Z', mine: false, ...over });
const question = (over: Record<string, unknown> = {}) => ({ id: 12, subject: 'Математика 1', subjectKey: 'математика 1', facultyOnly: false, title: 'Туындыны қалай табамыз?', body: 'x^2 + 3x функциясының туындысы қалай шығады? Қадамдап түсіндіріңізші.', tags: ['туынды'], anonymous: true, author: null, status: 'active', locked: false, lockedReason: null, acceptedAnswerId: null, answers: 1, score: 2, voted: false, mine: false, createdAt: '2026-11-04T05:00:00Z', ...over });
const listing = (over: Record<string, unknown> = {}) => ({ id: 21, kind: 'have_room', housing: 'flat', district: 'Сарыарқа ауданы', budgetMin: 40000, budgetMax: 80000, moveIn: '2026-11-20', habits: ['quiet', 'tidy'], about: 'Тыныш студентпін, кешке оқимын.', sameGender: true, gender: 'f', university: 'KATU', status: 'active', createdAt: '2026-11-04T05:00:00Z', expiresAt: '2026-12-04T05:00:00Z', mine: false, myRequest: null, ...over });

/* the live quiz's WebSocket (components/quiz/live.tsx): a host and a player, a lobby, one question, a reveal */
const players = [{ id: 'p1', nickname: 'Дана', online: true }, { id: 'p2', nickname: 'Ерлан', online: true }, { id: 'p3', nickname: 'Әсел', online: true }, { id: 'p4', nickname: 'Нұрлан', online: false }];
const leaderboard = [{ rank: 1, id: 'p1', nickname: 'Дана', score: 980, correct: 1 }, { rank: 2, id: 'p3', nickname: 'Әсел', score: 860, correct: 1 }, { rank: 3, id: 'p2', nickname: 'Ерлан', score: 0, correct: 0 }];
async function mockLive(page: Page) {
  await page.routeWebSocket(/\/api\/quiz\/live\/ws/, ws => {
    const host = new URL(ws.url()).searchParams.get('token') === 'host-token';
    const out = (type: string, data: unknown) => ws.send(JSON.stringify({ type, data }));
    const question = () => out('question', { index: 0, total: 2, question: quizPlay.questions[0], deadline: Date.now() + 25_000, serverTime: Date.now(), timeLimitMs: 30_000 });
    out('hello', { code: 'K7Q2M', status: host ? 'lobby' : 'question', title: quizMeta.title, total: 2, index: 0 });
    out('lobby', { players, count: players.length, max: 200 });
    if (!host) question();
    ws.onMessage(raw => {
      const m = JSON.parse(String(raw));
      if (m.type === 'start') { question(); out('progress', { index: 0, answered: 3, total: 4 }); }
      if (m.type === 'next') out('reveal', { index: 0, correct: 0, explanation: 'Сұраныс заңы.', leaderboard, last: false, answered: 3, correctCount: 2, distribution: { 0: 2, 1: 1 } });
    });
  });
}

async function mockApi(page: Page) {
  await mockLive(page);
  await page.route('https://fonts.googleapis.com/**', r => r.fulfill({ contentType: 'text/css', body: '' }));
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url());
    const p = url.pathname;
    const json = (j: unknown) => route.fulfill({ json: j });
    if (p === '/api/config') return json({ ...config, features: { ...config.features, cafe: true, print: true, clubs: true, events: true, market: true, lostfound: true, courseqa: true, roommates: true, mentors: true, ambassador: true, quiz: true, portfolio: true } });
    if (p === '/api/content') return json(content);
    if (p === '/api/me') return json({ user, needsConsent: false, inboxUnread: 2 });
    if (p === '/api/me/inbox') return json({ items: [], unread: 0, nextBefore: null });
    if (p === '/api/cafes') return json({ cafes: cafes() });
    let mm = /^\/api\/cafes\/(\d+)\/menu$/.exec(p);
    if (mm) { const c = cafes().find(x => x.id === Number(mm![1])) ?? cafe; return json({ ...menu, cafe: c }); }
    if (p === '/api/cafe/orders/mine') return json({ orders: [] });
    if (p === '/api/print/shops') return json({ shops: shops() });
    mm = /^\/api\/print\/shops\/(\d+)$/.exec(p);
    if (mm) { const sh = shops().find(x => x.id === Number(mm![1])) ?? shop; return json({ ...catalog, shop: sh }); }
    if (p === '/api/print/jobs/mine') return json({ jobs: [] });
    if (p === '/api/clubs') return json({ clubs });
    if (p === '/api/clubs/1') return json({ club: { ...clubs[0], about_kk: 'Клуб 2019 жылы құрылған. Әр апта сайын пікір-сайыс өтеді, республикалық турнирлерге қатысамыз.\nЖаңа мүшелерге шешендік өнер негіздерін үйретеміз.', building_id: 'b1', leader_title_kk: 'Төраға: Дана Серікқызы', leader_contact: '@dana_s', leader_contact_public: true, telegram_url: 'https://t.me/shyraq', instagram_url: 'https://www.instagram.com/shyraq/', members: 42 }, posts: [{ id: 1, body_kk: 'Сейсенбі күні іріктеу турнирі өтеді. Барлығын шақырамыз!', body_ru: null, link_url: null, status: 'published', published_at: '2026-11-03', created_at: '2026-11-03' }], can_manage: false });
    if (p === '/api/events') return json({ events: url.searchParams.get('club') ? events.slice(1) : events });
    if (p === '/api/events/9001') return json({ event: events[0] });
    if (p === '/api/events/9001/attendees') return json({ count: 34 });
    if (p === '/api/market/items') return json({ items: [market(5, 'Экономика оқулығы'), market(6, 'Үстел шамы', { category: 'home' }), market(8, 'Калькулятор Casio', { category: 'tech', status: 'reserved' }), market(9, 'Қысқы күртке', { category: 'clothes' })], university: 'Сейфуллин университеті', season: null });
    if (p === '/api/market/items/5') return json({ item: market(5, 'Экономика оқулығы') });
    if (p === '/api/market/me') return json({ canPublish: true, blockers: [], rulesAccepted: true, hasUsername: true, banUntil: null, today: 0, dailyLimit: 3 });
    if (p === '/api/lostfound/posts') return json({ posts: [lfPost(), lfPost({ id: 8, kind: 'lost', category: 'cards', title: 'Студенттік билет', building: 'b5', room: '' }), lfPost({ id: 9, title: 'Қара қолшатыр', category: 'other' })], university: 'KATU' });
    if (p === '/api/lostfound/posts/7') return json({ post: lfPost(), myClaim: null });
    if (p === '/api/qa/questions') return json({ questions: [question(), question({ id: 13, title: 'Ағылшын тілінен эссе құрылымы', subject: 'Ағылшын тілі', answers: 3, acceptedAnswerId: 2 }), question({ id: 14, title: 'Статистикадан дисперсия формуласы', subject: 'Статистика', answers: 0 })], subjectKey: '', university: 'KATU', facultyScoped: true });
    if (p === '/api/qa/questions/12') return json({ question: question(), answers: [{ id: 40, questionId: 12, body: 'Дәреже ережесі: (x^n)\' = n·x^(n-1). Сонда 2x + 3 шығады.', anonymous: false, author: { name: 'Ерлан' }, score: 3, voted: false, mine: false, accepted: false, createdAt: '2026-11-04T07:00:00Z', status: 'active' }] });
    if (p === '/api/quiz/quizzes') return json({ quizzes: [quizMeta, { ...quizMeta, id: 2, title: 'Ағылшын тілі: Present Perfect', subject: 'Ағылшын тілі', plays: 120 }, { ...quizMeta, id: 3, title: 'Статистика негіздері', subject: 'Статистика', plays: 9 }], hasMore: false });
    if (p === '/api/quiz/mine') return json({ quizzes: [] });
    if (p === '/api/quiz/attempts') return json({ attempts: [] });
    if (p === '/api/quiz/quizzes/1') return json(quizEdit);
    if (p === '/api/quiz/live/sessions') return json({ code: 'K7Q2M', hostToken: 'host-token', wsUrl: `ws://127.0.0.1:${new URL(route.request().url()).port}/api/quiz/live/ws`, expiresAt: Date.now() + 3_600_000, title: quizMeta.title });
    if (p === '/api/quiz/live/sessions/K7Q2M/host-token') return json({ hostToken: 'host-token', wsUrl: `ws://127.0.0.1:${new URL(route.request().url()).port}/api/quiz/live/ws`, expiresAt: Date.now() + 3_600_000 });
    if (p === '/api/quiz/live/join') return json({ token: 'player-token', wsUrl: `ws://127.0.0.1:${new URL(route.request().url()).port}/api/quiz/live/ws`, playerId: 'p1', nickname: 'Дана', rejoinKey: 'rk', expiresAt: Date.now() + 3_600_000 });
    if (p === '/api/quiz/quizzes/1/play') return json(quizPlay);
    if (p === '/api/quiz/quizzes/1/attempts') return json(quizResult);
    if (p === '/api/portfolio/me') return json({
      settings: { enabled: true, slug: 'aruzhan-t', url: 'https://studenthub.example/p/aruzhan-t', displayName: 'Аружан Тест', headline: 'Экономика, 2-курс', bio: 'Деректерді талдауға қызығамын.', showUniversity: true, showCourse: true, showEvents: true, showCerts: true, showExperience: true, showSkills: true, showBadges: true, showContact: false, syncConsentAt: '2026-11-01T05:00:00Z', slugRotatedAt: null, updatedAt: '2026-11-01T05:00:00Z' },
      items: [], preview: { noindex: true, profile: { name: 'Аружан Тест', headline: 'Экономика, 2-курс', bio: null, university: 'Сейфуллин университеті', course: 2 }, verified: { events: [], certificates: [], badges: [] }, selfReported: { certificates: [], experience: [], skills: [], badges: [] }, contact: null, og: { title: 'Аружан Тест', description: null, image: '', url: '' }, site: 'Student Hub' },
    });
    if (p === '/api/admin/dashboard') return json({
      users: { total: 1240, accounts: 980, new7: 64, new30: 210, blocked: 2, moderators: 4, byRole: { student: 1200, admin: 3, ambassador: 25, employer: 12 } },
      signups: [], active: { dau: 310, wau: 820, mau: 1100, since: '2026-09-01', days: [] }, devices: { dau: 420, wau: 990 },
      content: { jobs: 40, opportunities: 75, events: 18 },
      pending: { queue: 3, queueModerator: 1, reports: 1, feedbackHeld: 0, feedbackOverdue: 0, redemptions: 2, employers: 1, cafeRefunds: 0, printRefunds: 0 },
      shops: { cafe: { orders: 120, done: 115, revenue: 9800000 }, print: { orders: 60, done: 58, revenue: 2400000 } },
      features: { cafe: true, print: true, clubs: true },
    });
    if (p === '/api/console/me') return json({ admin: true, moderator: false, blocked: false, moderation: { pending: 3, reports: 1 }, cafes: [], printShops: [], clubs: [], university: null, feedback: [], employer: null, ambassador: false, mentor: false });
    if (p === '/api/announcements') return json({ announcements: [] });
    if (p === '/api/events/my/attendance') return json({ attendance: [] });
    if (p === '/api/portfolio/certificates') return json({ certificates: [], usage: { files: 0, bytes: 0, maxFiles: 20, maxBytes: 52428800 }, pageVisible: true });
    if (p === '/api/certificates/ABC123') return json({ name: 'Аружан Тест', university: 'Сейфуллин университеті', role_in_team: 'member', term_start: '2026-02-01', term_end: '2026-10-31', issued_at: '2026-11-01', events_organized: 4, published_suggestions: 11, verify_url: 'https://studenthub.example/certificate/ABC123' });
    if (p === '/api/ambassador/me') return json({
      term: { id: 1, user_id: 1, university: 'Сейфуллин университеті', role_in_team: 'lead', term_start: '2026-09-01', term_end: '2027-01-31', hours_per_week: 4, ref_code: 'aru', status: 'active', paused_until: null, renew_answer: null, cert_code: 'ABC123', cert_issued_at: null },
      days_left: 85, renewal: { ask: false, answer: null }, referral: { code: 'aru', ref: 'aru', link: 'https://studenthub.example/?ref=aru', joined: 17 },
      workload: { hours_per_week: 4, items_this_week: 2, paused_until: null, paused: false, available: true },
      team: [{ name: 'Аружан Тест', username: 'aru', role_in_team: 'lead', term_start: '2026-09-01', term_end: '2027-01-31', hours_per_week: 4, paused: false }, { name: 'Ерлан', username: null, role_in_team: 'member', term_start: '2026-09-01', term_end: '2027-01-31', hours_per_week: 3, paused: false }],
      resources: [{ title_kk: 'Амбассадор нұсқаулығы', title_ru: 'Руководство амбассадора', url: 'https://studenthub.example/guide' }],
    });
    if (p === '/api/ambassador/stats') return json({ university: 'Сейфуллин университеті', students: 1240, weekly_active: 610, saved_deadlines: 2300, events: 18, registrations: 940, check_ins: 610, by_faculty: { 'Экономика': 320, 'IT': 280, other: '< 5' } });
    if (p === '/api/ambassador/rating') return json({ rating: [], mine: { rank: 2, university: 'Сейфуллин университеті', weekly_active: 610, event_check_ins: 610, score: 88 } });
    if (p === '/api/submissions/my') return json({ submissions: [] });
    if (p === '/api/mentors/summary') return json({ university: 'Сейфуллин университеті', stats: { university: 'Сейфуллин университеті', mentors: 14, freeMentors: 3, waiting: 5, typicalDays: 3 }, me: { isAdult: true, request: null, mentorStatus: null }, groupMeetings: [] });
    if (p === '/api/roommates/me') return json({ gender: 'f', listing: null, requestsToday: 0, dailyLimit: 10, blockers: [] });
    if (p === '/api/roommates/listings') return json({ listings: [listing(), listing({ id: 22, kind: 'looking', housing: 'dorm', district: 'Жатақхана №3', habits: ['early'] }), listing({ id: 23, district: 'Есіл ауданы', budgetMin: 60000, budgetMax: 100000 })], university: 'KATU' });
    if (p === '/api/roommates/listings/21') return json({ listing: listing() });
    if (p === '/api/admin/summary') return json({ users: { total: 1240, byRole: { student: 1200, admin: 3, ambassador: 25, employer: 12 }, withConsent: 1100 }, activeReminders: 310, features: { cafe: true, print: true, clubs: true } });
    if (p === '/api/admin/analytics') return json({ devices: 2100, returned_day1: 640, eligible_day1: 1500, returned_day7: 380, eligible_day7: 1300, saved_first: 520, from_share: 90, ambassadors: null, feedback_avg: 4.4, feedback_count: 37, feedback_recent: [] });
    if (p === '/api/admin/users') return json({ users: [] });
    return json({});
  });
}

async function seed(page: Page) {
  await onboard(page, 'kk');
  await seedTimetable(page);
}

/** long subject names and four classes a day: the laptop week strip must keep them readable */
async function seedLong(page: Page) {
  await page.evaluate(() => {
    const s = JSON.parse(localStorage.getItem('studenthub.v1') || '{}');
    const names = ['Микроэкономика және макроэкономика негіздері', 'Кәсіби қазақ (орыс) тілі', 'Ақпараттық-коммуникациялық технологиялар', 'Философия', 'Экономикалық теория'];
    s.timetable = [0, 1, 2, 3, 4].flatMap(wd => names.slice(0, wd === 2 ? 5 : 4).map((name, i) => ({ id: `l${wd}${i}`, name, wd, t: `${String(8 + i * 2).padStart(2, '0')}:00`, end: `${String(8 + i * 2).padStart(2, '0')}:50`, room: '4-214', kind: 'Дәріс' })));
    localStorage.setItem('studenthub.v1', JSON.stringify(s));
  });
  await page.reload();
}

const cafeN = (n: number) => async () => { state.cafes = n; };
const shopN = (n: number) => async () => { state.shops = n; };
/* [name, path, act after load, prepare before load] */
const SCREENS: [string, string, ((p: Page) => Promise<void>)?, (() => Promise<void>)?][] = [
  ['home', '/'],
  ['search', '/search'],
  ['section', '/internships'],
  ['detail', '/opportunity/e2e-intern'],
  ['job', '/job/job-101'],
  ['calendar', '/calendar'],
  ['profile', '/profile'],
  ['settings', '/settings'],
  ['clubs', '/clubs'],
  ['club', '/club?id=1'],
  ['event', '/event/9001'],
  ['cafe', '/cafe', undefined, cafeN(1)],
  // the phone keeps the list; a laptop with one café shows its menu right here (add two items to fill the cart)
  ['cafe-1', '/cafe', async p => { if (await p.locator('.cafe-add').count()) { await p.locator('.cafe-add').first().click(); await p.locator('.cafe-add').nth(3).click(); } }, cafeN(1)],
  ['cafe-3', '/cafe', undefined, cafeN(3)],
  ['cafe-menu', '/cafe?id=3', async p => { await p.locator('.cafe-add').first().click(); await p.locator('.cafe-add').nth(3).click(); }, cafeN(3)],
  ['print', '/print', undefined, shopN(1)],
  ['print-2', '/print', undefined, shopN(2)],
  ['print-shop', '/print?id=4', undefined, shopN(2)],
  ['market', '/market'],
  ['market-item', '/market/5'],
  ['market-new', '/market/new'],
  ['lostfound', '/lostfound'],
  ['lostfound-post', '/lostfound?id=7'],
  ['lostfound-new', '/lostfound?id=new:lost'],
  ['qa', '/qa'],
  ['qa-thread', '/qa?id=12'],
  ['qa-ask', '/qa?id=new'],
  ['roommates', '/roommates'],
  ['roommates-listing', '/roommates?id=21'],
  ['roommates-new', '/roommates?id=new'],
  ['mentors', '/mentors'],
  ['mentors-become', '/mentors/become'],
  ['ambassador-apply', '/ambassador/apply'],
  ['discounts', '/discounts'],
  ['inbox', '/notifications'],
  ['certificate', '/certificate/ABC123'],
  ['portfolio', '/portfolio'],
  ['ambassador', '/ambassador'],
  ['privacy', '/privacy'],
  ['settings-notify', '/settings?s=notify'],
  ['quiz-edit', '/quiz/1/edit'],
  ['quiz-new', '/quiz/new'],
  ['quiz-host', '/quiz/1/host', async p => { await p.getByRole('button', { name: 'Ойын бөлмесін ашу' }).click(); await expect(p.locator('.quiz-live-code')).toBeVisible(); }],
  ['quiz-host-question', '/quiz/1/host', async p => {
    await p.getByRole('button', { name: 'Бастау' }).click(); await expect(p.locator('.qh-grid')).toBeVisible();
  }],
  ['quiz-host-reveal', '/quiz/1/host', async p => {
    await p.getByRole('button', { name: 'Бастау' }).click();
    await p.getByRole('button', { name: 'Жауаптарды көрсету' }).click(); await expect(p.locator('.qh-bar').first()).toBeVisible();
  }],
  ['quiz-player', '/quiz/live/K7Q2M', async p => { await expect(p.locator('.quiz-choice').first()).toBeVisible(); }],
  ['docs-page', '/docs'],
  ['pay', '/pay'],
  ['quiz-play', '/quiz/1/play'],
  ['quiz-result', '/quiz/1/play', async p => {
    for (let i = 0; i < 2; i++) { await p.locator('.quiz-choice').first().click(); await p.getByRole('button', { name: /Келесі|Аяқтау/ }).click(); }
    await expect(p.locator('.quiz-score')).toBeVisible();
  }],
  ['edit', '/edit'],
  ['timetable', '/timetable'],
  ['subjects', '/subjects'],
  ['grades', '/grades'],
  ['notes', '/notes'],
  ['quiz', '/quiz'],
  ['points', '/points'],
  ['gpa', '/gpa'],
  ['docs', '/docs'],
  ['saved', '/saved'],
  ['applications', '/applications'],
  ['suggest', '/suggest'],
  ['feedback', '/feedback'],
  ['surveys', '/surveys'],
  ['wellbeing', '/wellbeing'],
  ['announcements', '/announcements'],
  ['ai', '/ai'],
  ['resume', '/resume'],
  ['cards', '/cards'],
  ['roadmap', '/roadmap'],
  ['materials', '/materials'],
  ['services', '/services'],
  ['map', '/map'],
  ['admin', '/admin'],
  ['notif', '/notifications'],
  ['sheet', '/calendar', async p => { await p.getByRole('button', { name: 'Іс-шара қосу', exact: true }).click(); await expect(p.locator('.sheet')).toBeVisible(); }],
  ['filters', '/search', async p => { const b = p.locator('.filter-btn:visible'); if (await b.count()) { await b.click(); await expect(p.locator('.sheet')).toBeVisible(); } }],
  ['menu', '/', async p => { const b = p.locator('#menu-btn:visible'); if (await b.count()) { await b.click(); await expect(p.locator('#nav-drawer')).toBeVisible(); } }],
  ['home-long', '/', undefined, undefined],
];

test('screens', async ({ page }, info) => {
  test.setTimeout(900_000);
  page.on('pageerror', e => console.log('pageerror:', e.message, (e.stack || '').split('\n').slice(1, 3).join(' | ')));
  await mockApi(page);
  await seed(page);
  const w = info.project.name.split('x')[0];
  for (const [name, path, act, prep] of SCREENS) {
    if (only.length && !only.includes(name)) continue;
    if (name === 'home-long') await seedLong(page);
    if (prep) await prep();
    if (name === 'quiz-player') {
      await page.goto('/');
      await page.evaluate(() => sessionStorage.setItem('studenthub.quiz.live.player.K7Q2M', JSON.stringify({ role: 'player', code: 'K7Q2M', nickname: 'Дана', rejoinKey: 'rk' })));
    }
    await page.goto(path);
    await page.waitForLoadState('networkidle', { timeout: 8000 }).catch(() => {});
    await page.waitForTimeout(700);
    if (act) await act(page);
    await page.waitForTimeout(300);
    await page.screenshot({ path: `${dir}/${name}-${w}.png`, fullPage: !!process.env.SHOT_FULL });
  }
});
