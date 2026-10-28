import type { Content, Discount, Opportunity } from '../lib/types';
import type { AnyJob } from '../lib/b2b';

// Fixed bilingual fixtures, deliberately independent of production and data/*.json.
const provenance = {
  source_url: 'https://example.org/student', posted_at: '2026-01-01', verified_at: '2026-01-02',
  verified_by: 'E2E', contact: {}, is_demo: false, deadline: null, city: 'astana',
};
export const job: AnyJob = {
  ...provenance, id: 'job-101', title: { kk: 'Кітапхана көмекшісі', ru: 'Помощник библиотеки' },
  org: { kk: 'Кампус кітапханасы', ru: 'Библиотека кампуса' }, pay: 1500, per: 'hour', hours: 3,
  sched: { kk: 'Сабақтан кейін', ru: 'После занятий' }, tags: ['noexp', 'near'], skills: [], spec: ['all'],
  desc: { kk: 'Студенттерге кітап табуға көмектесу', ru: 'Помогать студентам находить книги' }, req: [],
  source: 'employer', apply: { kk: 'Сайттағы өтінімге қысқа хат қосыңыз.', ru: 'Добавьте короткое письмо к отклику на сайте.' },
  employer: { name: 'Campus', website: 'https://example.org', about: '', verified: true, verified_at: '2026-01-02', verification_meaning: { kk: 'Байланыс тексерілген', ru: 'Контакты проверены' } },
};
export const experiencedJob: AnyJob = { ...job, id: 'job-102', tags: [], title: { kk: 'Тәжірибелі кітапханашы', ru: 'Опытный библиотекарь' } };
export const opportunity: Opportunity = {
  ...provenance, id: 'e2e-intern', type: 'company', funded: true,
  title: { kk: 'Зерттеу тағылымдамасы', ru: 'Исследовательская стажировка' },
  org: { kk: 'Кампус зертханасы', ru: 'Лаборатория кампуса' }, req: { course: 1, gpa: 3 }, spec: ['all'],
  desc: { kk: 'Студенттерге арналған зерттеу бағдарламасы.', ru: 'Исследовательская программа для студентов.' },
  docs: [{ kk: 'Түйіндеме', ru: 'Резюме' }], steps: [{ kk: 'Өтінім жіберіңіз', ru: 'Отправьте заявку' }],
};
export const discount: Discount = {
  ...provenance, id: 'e2e-deal', cat: 'food', pct: '15%',
  name: { kk: 'Кампус асханасы', ru: 'Столовая кампуса' }, what: { kk: 'Түскі асқа жеңілдік', ru: 'Скидка на обед' },
  cond: {
    kk: 'Студенттік куәлікті кассада көрсетіңіз. Жеңілдік тек жұмыс күндері сағат 14:00–16:00 аралығында беріледі.',
    ru: 'Покажите студенческий билет на кассе. Скидка действует только в рабочие дни с 14:00 до 16:00.',
  },
};
export const content: Content = { jobs: [job, experiencedJob], opportunities: [opportunity], discounts: [discount], events: [], places: [], source: 'e2e', updated_at: '2026-01-02' };
export const config = { botUsername: 'StudentHubTestBot', emailAuth: true, features: { events: false, ambassador: false, mentors: false, market: false, portfolio: false, b2b: true } };
