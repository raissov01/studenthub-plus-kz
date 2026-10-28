/* Header title and owning tab of every screen (the legacy view's {title, tab}). Pure, so the server can use it
   for <title> and the client for the header. */
import { EVENTS, EVENT_CATS } from '@/lib/data/static';
import { allDecks } from '@/lib/domain';
import type { I18n } from '@/lib/format';
import { parseView } from '@/lib/routes';
import type { UiState } from '@/lib/defaults';
import type { AppState } from '@/lib/types';

interface Ctx { I: I18n; S: AppState; ui: UiState }
interface Meta { title: (prm: string, x: Ctx) => string; tab?: string }

const t = (kk: string, ru: string) => ({ I }: Ctx) => I.tt(kk, ru);

const META: Record<string, Meta> = {
  home: { title: () => 'Student Hub' },
  search: { title: (_, x) => t('Іздеу', 'Поиск')(x) },
  notif: { title: (_, x) => t('Хабарламалар', 'Уведомления')(x) },
  services: { title: (_, x) => t('Университет қызметтері', 'Услуги университета')(x), tab: 'home' },
  materials: { title: (_, x) => t('Оқу материалдары', 'Учебные материалы')(x), tab: 'home' },
  saved: { title: (_, x) => t('Сақталғандар', 'Сохранённое')(x), tab: 'profile' },
  // the search screen under the old URLs; the header shows «Іздеу», the page <title> names the category
  opps: {
    title: (prm, x) => ({
      intern: t('Тағылымдама', 'Стажировки')(x), events: t('Іс-шаралар', 'Мероприятия')(x), courses: t('Курстар', 'Курсы')(x),
    } as Record<string, string>)[prm] ?? t('Жұмыс', 'Работа')(x),
  },
  mine: { title: (_, x) => t('Менің тізімім', 'Мой список')(x) },
  job: { title: (_, x) => t('Вакансия', 'Вакансия')(x), tab: 'opps' },
  opp: { title: (_, x) => t('Мүмкіндік', 'Возможность')(x), tab: 'opps' },
  event: {
    title: (id, x) => { const e = EVENTS.find(v => v.id === id); return e ? x.I.L(EVENT_CATS[e.cat]) : t('Іс-шара', 'Мероприятие')(x); },
    tab: 'opps',
  },
  ai: { title: (_, x) => t('AI-көмекші', 'AI-помощник')(x) },
  chat: { title: (_, x) => t('AI чат', 'AI-чат')(x), tab: 'ai' },
  resume: { title: (_, x) => t('Түйіндеме', 'Резюме')(x), tab: 'ai' },
  interview: { title: (_, x) => t('Сұхбатқа дайындық', 'Собеседование')(x), tab: 'ai' },
  exam: { title: (_, x) => t('Емтиханға дайындық', 'Подготовка к экзамену')(x), tab: 'ai' },
  plan: { title: (id, x) => x.S.plans.find(p => p.id === id)?.subject ?? t('Емтиханға дайындық', 'Подготовка к экзамену')(x), tab: 'ai' },
  cards: { title: (_, x) => t('Карточкалар', 'Карточки')(x), tab: 'ai' },
  deck: { title: (id, x) => allDecks(x.S, x.I).find(d => d.id === id)?.name ?? t('Карточкалар', 'Карточки')(x), tab: 'ai' },
  study: {
    title: (_, x) => {
      const s = x.ui.study;
      if (!s) return t('Карточкалар', 'Карточки')(x);
      if (!s.queue.length) return t('Нәтиже', 'Результат')(x);
      return s.mode === 'exam' ? t('Емтихан режимі', 'Режим экзамена')(x) : t('Оқу', 'Учёба')(x);
    },
    tab: 'ai',
  },
  match: { title: (_, x) => t('Вакансияға сәйкестік', 'Совпадение с вакансией')(x), tab: 'ai' },
  bonus: { title: (_, x) => t('Жеңілдіктер', 'Скидки')(x) },
  profile: { title: (_, x) => t('Профиль', 'Профиль')(x) },
  edit: { title: (_, x) => t('Профильді өзгерту', 'Изменить профиль')(x), tab: 'profile' },
  gpa: { title: (_, x) => t('Менің оқуым', 'Моя учёба')(x), tab: 'profile' },
  docs: { title: (_, x) => t('Құжаттар мен анықтамалар', 'Документы и справки')(x), tab: 'profile' },
  doc: { title: (_, x) => t('Анықтама', 'Справка')(x), tab: 'profile' },
  admission: { title: (_, x) => t('Түсу', 'Поступление')(x), tab: 'profile' },
  map: { title: (prm, x) => (prm === 'near' ? t('Жақын маңда', 'Рядом с кампусом')(x) : t('Кампус картасы', 'Карта кампуса')(x)), tab: 'home' },
  pay: { title: (_, x) => t('Оқу ақысы', 'Оплата обучения')(x), tab: 'profile' },
  portfolio: { title: (_, x) => t('Портфолио', 'Портфолио')(x), tab: 'profile' },
  settings: { title: (_, x) => t('Баптаулар', 'Настройки')(x), tab: 'profile' },
  roadmap: { title: (_, x) => t('Даму жоспары', 'План развития')(x), tab: 'home' },
  privacy: { title: (_, x) => t('Құпиялылық саясаты', 'Конфиденциальность')(x), tab: 'profile' },
  admin: {
    title: (prm, x) => ({
      health: t('Денсаулық', 'Состояние системы')(x),
      queue: t('Модерация кезегі', 'Очередь модерации')(x), reports: t('Шағымдар', 'Жалобы')(x), mentors: t('Менторларды жұптау', 'Подбор менторов')(x),
      ambassadors: t('Амбассадорлар', 'Амбассадоры')(x), market: t('Тегін заттар: модерация', 'Отдам даром: модерация')(x),
      employers: t('Жұмыс берушілер', 'Работодатели')(x), staff: t('Университет қызметкерлері', 'Сотрудники университетов')(x),
      content: t('Контент', 'Контент')(x),
    } as Record<string, string>)[prm.replace(/\?.*$/, '')]
      ?? (/^content\/new(\?|$)/.test(prm) ? t('Жаңа жазба', 'Новая запись')(x) : prm.startsWith('content/') ? t('Жазбаны өзгерту', 'Изменить запись')(x) : 'Student Hub'),
    tab: 'profile',
  },
  market: {
    title: (prm, x) => (prm === 'new' ? t('Зат беру', 'Отдать вещь')(x) : prm === 'mine' ? t('Менің заттарым', 'Мои вещи')(x) : t('Тегін заттар', 'Отдам даром')(x)),
    tab: 'home',
  },
  mentors: {
    title: (prm, x) => (prm === 'become' ? t('Ментор болу', 'Стать ментором')(x) : prm === 'me' ? t('Менің менторлығым', 'Моё менторство')(x) : t('Менторлар', 'Менторы')(x)),
    tab: 'home',
  },
  ambassador: {
    title: (prm, x) => (prm === 'apply' ? t('Амбассадор болу', 'Стать амбассадором')(x) : prm === 'mentors' ? t('Менторларды жұптау', 'Подбор менторов')(x)
      : prm === 'rating' ? t('Университеттер рейтингі', 'Рейтинг университетов')(x) : t('Амбассадор кабинеті', 'Кабинет амбассадора')(x)),
    tab: 'profile',
  },
  certificate: { title: (_, x) => t('Сертификат', 'Сертификат')(x) },
  employer: {
    title: (prm, x) => (prm === 'apply' ? t('Компанияны тіркеу', 'Регистрация компании')(x) : prm.startsWith('join/') ? t('Компанияға қосылу', 'Вступить в компанию')(x)
      : prm === 'jobs/new' ? t('Жаңа вакансия', 'Новая вакансия')(x) : /^jobs\/\d+\/edit$/.test(prm) ? t('Вакансияны өзгерту', 'Изменить вакансию')(x)
        : /^jobs\/\d+$/.test(prm) ? t('Өтінімдер', 'Отклики')(x) : t('Жұмыс беруші кабинеті', 'Кабинет работодателя')(x)),
    tab: 'profile',
  },
  university: { title: (_, x) => t('Университет кабинеті', 'Кабинет университета')(x), tab: 'profile' },
  announcements: { title: (_, x) => t('Ресми хабарландырулар', 'Официальные объявления')(x), tab: 'home' },
  suggest: { title: (_, x) => t('Мүмкіндік ұсыну', 'Предложить возможность')(x) },
  suggestions: { title: (_, x) => t('Менің ұсыныстарым', 'Мои предложения')(x), tab: 'profile' },
  checkin: { title: (_, x) => t('Іс-шараға келу', 'Отметка на мероприятии')(x), tab: 'home' },
  organize: {
    title: (prm, x) => (prm === 'new' ? t('Жаңа іс-шара', 'Новое мероприятие')(x) : prm ? t('Іс-шараны басқару', 'Управление мероприятием')(x) : t('Ұйымдастырушы', 'Организатор')(x)),
    tab: 'profile',
  },
};

export function screenMeta(v: string, x: Ctx): { name: string; title: string; tab: string } {
  const [name, prm] = parseView(v);
  const m = META[name] || META.home;
  return { name, title: m.title(prm, x), tab: m.tab ?? '' };
}
