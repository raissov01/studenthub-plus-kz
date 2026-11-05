/* Header title and owning tab of every screen (the legacy view's {title, tab}). Pure, so the server can use it
   for <title> and the client for the header. */
import { allDecks } from '@/lib/domain';
import type { I18n } from '@/lib/format';
import { parseView } from '@/lib/routes';
import type { UiState } from '@/lib/defaults';
import type { AppState } from '@/lib/types';

interface Ctx { I: I18n; S: AppState; ui: UiState }
interface Meta { title: (prm: string, x: Ctx) => string; tab?: string }

const t = (kk: string, ru: string) => ({ I }: Ctx) => I.tt(kk, ru);

const META: Record<string, Meta> = {
  quiz: { title: (_, x) => t('Квиздер', 'Квизы')(x), tab: 'home' },
  home: { title: () => 'Student Hub' },
  search: { title: (_, x) => t('Іздеу', 'Поиск')(x) },
  notif: { title: (_, x) => t('Хабарламалар', 'Уведомления')(x), tab: 'home' },
  services: { title: (_, x) => t('Университет қызметтері', 'Услуги университета')(x), tab: 'home' },
  materials: { title: (_, x) => t('Оқу материалдары', 'Учебные материалы')(x), tab: 'home' },
  saved: { title: (_, x) => t('Сақталғандар', 'Сохранённое')(x), tab: 'profile' },
  // the sections at the old URLs (lib/sections.ts); the header and the page <title> name the section
  opps: {
    title: (prm, x) => ({
      intern: t('Тағылымдама', 'Стажировки')(x), events: t('Іс-шаралар', 'Мероприятия')(x), courses: t('Курстар', 'Курсы')(x),
    } as Record<string, string>)[prm] ?? t('Жұмыс', 'Работа')(x),
  },
  mine: { title: (_, x) => t('Менің тізімім', 'Мой список')(x) },
  job: { title: (_, x) => t('Вакансия', 'Вакансия')(x), tab: 'opps' },
  opp: { title: (_, x) => t('Мүмкіндік', 'Возможность')(x), tab: 'opps' },
  event: { title: (_, x) => t('Іс-шара', 'Мероприятие')(x), tab: 'opps' },
  ai: { title: (_, x) => t('AI-көмекші', 'AI-помощник')(x) },
  chat: { title: (_, x) => t('AI чат', 'AI-чат')(x), tab: 'ai' },
  resume: { title: (_, x) => t('Түйіндеме', 'Резюме')(x), tab: 'ai' },
  interview: { title: (_, x) => t('Сұхбатқа дайындық', 'Собеседование')(x), tab: 'ai' },
  exam: { title: (_, x) => t('Емтиханға дайындық', 'Подготовка к экзамену')(x), tab: 'ai' },
  plan: { title: (id, x) => x.S.plans.find(p => p.id === id)?.subject ?? t('Емтиханға дайындық', 'Подготовка к экзамену')(x), tab: 'ai' },
  cards: { title: (_, x) => t('Карточкалар', 'Карточки')(x), tab: 'ai' },
  deck: { title: (id, x) => allDecks(x.S).find(d => d.id === id)?.name ?? t('Карточкалар', 'Карточки')(x), tab: 'ai' },
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
      content: t('Контент', 'Контент')(x), points: t('Ұпайлар мен сыйлықтар', 'Баллы и награды')(x), cafe: t('Кофейнялар', 'Кофейни')(x), print: t('Полиграфиялар', 'Полиграфии')(x),
      clubs: t('Клубтар', 'Клубы')(x),
      feeds: t('Мүмкіндіктер ағыны', 'Лента возможностей')(x),
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
  calendar: { title: (_, x) => t('Күнтізбе', 'Календарь')(x) },
  points: { title: (_, x) => t('Ұпайлар', 'Баллы')(x), tab: 'profile' },
  cafe: {
    title: (prm, x) => (prm === 'staff' ? t('Тапсырыстар тақтасы', 'Доска заказов')(x) : prm === 'orders' || /^o\d+$/.test(prm) ? t('Тапсырыстарым', 'Мои заказы')(x) : t('Кофе', 'Кофе')(x)),
    tab: 'home',
  },
  print: {
    title: (prm, x) => (prm === 'staff' ? t('Кезек тақтасы', 'Доска очереди')(x) : prm === 'jobs' || /^j\d+$/.test(prm) ? t('Басып шығару тапсырыстарым', 'Мои заказы печати')(x) : t('Полиграфия', 'Полиграфия')(x)),
    tab: 'home',
  },
  subjects: { title: (_, x) => t('Пәндер', 'Предметы')(x), tab: 'home' },
  timetable: { title: (_, x) => t('Сабақ кестесі', 'Расписание занятий')(x), tab: 'home' },
  notes: { title: (_, x) => t('Конспектілер', 'Конспекты')(x), tab: 'home' },
  review: { title: (_, x) => t('Қайталау', 'Повторение')(x), tab: 'home' },
  surveys: { title: (_, x) => t('Сауалнамалар', 'Опросы')(x), tab: 'home' },
  feedback: { title: (_, x) => t('Ұсыныс және шағым', 'Предложения и жалобы')(x), tab: 'home' },
  wellbeing: { title: (_, x) => t('Әл-ауқат', 'Благополучие')(x), tab: 'home' },
  voice: { title: (_, x) => t('Студент дауысы', 'Голос студентов')(x), tab: 'profile' },
  lostfound: { title: (prm, x) => (prm === 'new' ? t('Жариялау', 'Опубликовать')(x) : prm === 'mine' ? t('Менің жазбаларым', 'Мои записи')(x) : t('Табылған заттар', 'Бюро находок')(x)), tab: 'home' },
  qa: { title: (prm, x) => (prm === 'new' || prm.startsWith('new:') ? t('Сұрақ қою', 'Задать вопрос')(x) : t('Сұрақ-жауап', 'Вопросы и ответы')(x)), tab: 'home' },
  roommates: { title: (prm, x) => (prm === 'new' ? t('Хабарландыру', 'Объявление')(x) : prm === 'requests' ? t('Сұраулар', 'Запросы')(x) : t('Пәтерлес', 'Соседи по жилью')(x)), tab: 'home' },
  clubs: { title: (_, x) => t('Клубтар', 'Клубы')(x), tab: 'home' },
  club: { title: (_, x) => t('Клуб', 'Клуб')(x), tab: 'home' },
  grades: { title: (_, x) => t('Бағалар', 'Оценки')(x), tab: 'profile' },
  applications: { title: (_, x) => t('Менің өтінімдерім', 'Мои отклики')(x), tab: 'profile' },
  booth: { title: (_, x) => t('Жәрмеңке стенді', 'Стенд ярмарки')(x), tab: 'home' },
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
