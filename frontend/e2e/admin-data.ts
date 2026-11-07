/* Mock API answers for the admin workspace screens (calendar, attendance, payments, settings, user tabs), shared by
   e2e/admin-workspace.spec.ts and the design screenshots of e2e/admin-shots.spec.ts. Illustrative numbers only. */

const DAY = 86_400_000;
/** this week's Monday 00:00 Astana time, as a UTC instant */
export function mondayUtc(now = Date.now()): number {
  const wall = new Date(now + 5 * 3600_000);
  const dow = (wall.getUTCDay() + 6) % 7;
  return Date.UTC(wall.getUTCFullYear(), wall.getUTCMonth(), wall.getUTCDate() - dow) - 5 * 3600_000;
}
/** an instant on day `d` (0 = Monday) of this week at hh:mm Astana time */
export const at = (d: number, hh: number, mm = 0) => new Date(mondayUtc() + d * DAY + (hh * 60 + mm) * 60_000).toISOString().replace('.000Z', 'Z');

export function weekEvent(id: number, title: string, day: number, h: number, dur: number, status = 'published', extra: object = {}) {
  return {
    id, title_kk: title, title_ru: null, desc_kk: null, desc_ru: null, cat: 'career', starts_at: at(day, h), ends_at: at(day, h + dur), university: 'KATU',
    building_id: null, place_text: 'Акт залы', capacity: 80, walk_in_allowed: false, gives_cert: false, teams_enabled: false, status, created_by: 501,
    checkin_started_at: null, finished_at: null, created_at: at(0, 9), registered: 42, waitlist: 0, checked_in: status === 'finished' ? 35 : 0, spots_left: 38,
    can_manage: true, club_id: null, ...extra,
  };
}

export const weekEvents = () => [
  weekEvent(11, 'Түйіндеме жазу шеберханасы', 0, 15, 2, 'finished'),
  weekEvent(12, 'Дебат клубы: кездесу', 1, 18, 1.5, 'published', { club_id: 2 }),
  weekEvent(13, 'IT мансап күні', 2, 10, 3),
  weekEvent(14, 'Волонтерлер жиналысы', 2, 11, 1, 'draft'),
  weekEvent(15, 'Ағылшын тілі сөйлесу клубы', 4, 17, 1.5, 'published', { club_id: 3 }),
];

export const attendance = () => ({
  weeks: Array.from({ length: 12 }, (_, i) => ({ week: new Date(mondayUtc() - (11 - i) * 7 * DAY + 5 * 3600_000).toISOString().slice(0, 10),
    events: i % 4 === 1 ? 0 : 2 + (i % 3), registered: i % 4 === 1 ? 0 : 60 + i * 5, checkedIn: i % 4 === 1 ? 0 : 38 + i * 3 })),
  events: [
    { id: 11, title_kk: 'Түйіндеме жазу шеберханасы', title_ru: null, cat: 'career', university: 'KATU', club_id: null, status: 'finished', starts_at: at(0, 15), ends_at: at(0, 17), capacity: 80, registered: 42, checked_in: 35, qr: 31, manual: 3, walk_in: 1 },
    { id: 9, title_kk: 'Дебат турнирі', title_ru: null, cat: 'contest', university: 'KATU', club_id: 2, status: 'finished', starts_at: at(-4, 16), ends_at: at(-4, 19), capacity: 40, registered: 38, checked_in: 15, qr: 15, manual: 0, walk_in: 0 },
    { id: 8, title_kk: 'Стартап кеші', title_ru: null, cat: 'career', university: 'KATU', club_id: null, status: 'finished', starts_at: at(-9, 18), ends_at: at(-9, 20), capacity: null, registered: 64, checked_in: 51, qr: 44, manual: 5, walk_in: 2 },
  ],
  totals: { events: 3, registered: 144, checkedIn: 101 },
});

export const payments = (scope: 'all' | 'managed' = 'all') => ({
  scope, page: 1, limit: 50,
  shops: [{ kind: 'cafe', id: 3, nameKk: 'Бас ғимарат кофесі', nameRu: 'Кофе в главном корпусе' }, ...(scope === 'all' ? [{ kind: 'print', id: 2, nameKk: 'Полиграфия №1', nameRu: 'Полиграфия №1' }] : [])],
  totals: { count: 186, revenue: 16_740_000, paidKaspi: 9_120_000, refunded: 180_000, toReturn: 1, toReturnSum: 90_000 },
  days: Array.from({ length: 30 }, (_, i) => ({ day: new Date(Date.now() - (29 - i) * DAY).toISOString().slice(0, 10), orders: 4 + (i * 7) % 9, revenue: (4 + (i * 7) % 9) * 90_000 })),
  rows: [
    { kind: 'cafe', id: 9012, shopId: 3, shopKk: 'Бас ғимарат кофесі', shopRu: 'Кофе в главном корпусе', customer: 'Дана Ерланқызы', customerId: 77, total: 90_000, paid: 90_000, status: 'cancelled', method: 'kaspi', payment: 'paid', refundManual: true, createdAt: at(2, 9, 40), closedAt: at(2, 9, 55) },
    { kind: 'cafe', id: 9011, shopId: 3, shopKk: 'Бас ғимарат кофесі', shopRu: 'Кофе в главном корпусе', customer: 'Нұрлан Сейіт', customerId: 78, total: 120_000, paid: 120_000, status: 'picked_up', method: 'kaspi', payment: 'paid', refundManual: false, createdAt: at(2, 9, 20), closedAt: at(2, 9, 31) },
    ...(scope === 'all' ? [{ kind: 'print', id: 410, shopId: 2, shopKk: 'Полиграфия №1', shopRu: 'Полиграфия №1', customer: 'Айгерім Б.', customerId: 79, total: 60_000, paid: null, status: 'picked_up', method: 'pickup', payment: 'unpaid', refundManual: false, createdAt: at(1, 14, 5), closedAt: at(1, 15) }] : []),
    { kind: 'cafe', id: 9008, shopId: 3, shopKk: 'Бас ғимарат кофесі', shopRu: 'Кофе в главном корпусе', customer: 'Тимур К.', customerId: 80, total: 75_000, paid: null, status: 'picked_up', method: 'pickup', payment: 'unpaid', refundManual: false, createdAt: at(1, 12, 10), closedAt: at(1, 12, 20) },
  ],
});

export const settings = () => ({
  features: { events: true, cafe: true, print: true, clubs: true, feeds: true, market: true, points: true, b2b: false, surveys: true, feedback: true, wellbeing: false, quiz: false },
  featureOrder: ['events', 'cafe', 'print', 'clubs', 'feeds', 'market', 'points', 'b2b', 'surveys', 'feedback', 'wellbeing', 'quiz'],
  support: { email: 'help@studenthub.kz', telegram: 'studenthub_help', phone: '+7 700 000 00 00', address: 'Бас ғимарат, 105-кабинет', hoursKk: 'Дс–Жм, 9:00–18:00', hoursRu: 'Пн–Пт, 9:00–18:00' },
  updatedAt: '2026-11-07T10:00:00Z', updatedBy: { id: 501, name: 'Аружан Тест' },
});

export const userOrders = () => ({
  cafe: [
    { id: 9011, shopId: 3, shopKk: 'Бас ғимарат кофесі', shopRu: 'Кофе в главном корпусе', total: 120_000, paid: 120_000, status: 'picked_up', method: 'kaspi', payment: 'paid', refundManual: false, createdAt: '2026-11-06T04:20:00Z' },
    { id: 8990, shopId: 3, shopKk: 'Бас ғимарат кофесі', shopRu: 'Кофе в главном корпусе', total: 90_000, paid: 90_000, status: 'cancelled', method: 'kaspi', payment: 'paid', refundManual: true, createdAt: '2026-11-04T04:20:00Z' },
  ],
  print: [{ id: 410, shopId: 2, shopKk: 'Полиграфия №1', shopRu: 'Полиграфия №1', total: 60_000, paid: null, status: 'picked_up', method: 'pickup', payment: 'unpaid', refundManual: false, createdAt: '2026-11-03T09:00:00Z' }],
});

export const userNotes = () => ({ notes: [
  { id: 2, body: 'Телефонмен хабарласты: Kaspi-ден 900 ₸ қайтарылды.', createdAt: '2026-11-05T09:00:00Z', author: { id: 501, name: 'Аружан Тест', username: 'aru' } },
] });

export const dashboardExtra = () => ({
  orders: Array.from({ length: 30 }, (_, i) => ({ day: new Date(Date.UTC(2026, 10, 6 - 29 + i)).toISOString().slice(0, 10), cafe: 30 + (i * 11) % 25, print: 6 + (i * 5) % 9, revenue: (36 + (i * 13) % 30) * 85_000 })),
  checkins: Array.from({ length: 12 }, (_, i) => ({ week: new Date(Date.UTC(2026, 7, 17 + i * 7)).toISOString().slice(0, 10), events: i % 5 === 2 ? 0 : 3, registered: i % 5 === 2 ? 0 : 90 + i * 4, checkedIn: i % 5 === 2 ? 0 : 55 + i * 4 })),
});
