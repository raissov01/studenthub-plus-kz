'use client';
import { useEffect, useState } from 'react';
import { useApp } from '@/components/app';
import { Row, SectionHead } from '@/components/ui';
import { AdminOnly, Loading, NotFound } from '@/components/views/common';
import { api, ApiError } from '@/lib/account';
import type { Config, FeatureName } from '@/lib/config';

interface Health {
  dbReachable: boolean;
  errors: { message: string; count: number; lastSeen: string }[] | null;
  cronRuns: { endpoint: string; lastSuccessAt: string | null }[];
  moderationQueue: number | null;
  telegram: { configured: boolean; reachable: boolean; installed: boolean; pendingUpdates: number; lastErrorAt: string | null };
}
const JOBS: [string, FeatureName | null, string, string][] = [
  ['/api/cron', null, 'Еске салулар және тазалау', 'Напоминания и очистка'],
  ['/api/cron/events', 'events', 'Іс-шаралар', 'Мероприятия'],
  ['/api/cron/ambassador', 'ambassador', 'Амбассадорлар', 'Амбассадоры'],
  ['/api/cron/mentors', 'mentors', 'Менторлар', 'Менторы'],
  ['/api/cron/market', 'market', 'Тегін заттар', 'Бесплатные вещи'],
  ['/api/cron/b2b', 'b2b', 'Жұмыс берушілер', 'Работодатели'],
];
function isHealth(v: unknown): v is Health {
  return !!v && typeof v === 'object' && typeof (v as Health).dbReachable === 'boolean'
    && Array.isArray((v as Health).cronRuns) && !!(v as Health).telegram;
}

function HealthDashboard() {
  const { tt, lang } = useApp();
  const [state, setState] = useState<{ health: Health | null; config: Config | null; partial: boolean; failed: boolean; denied: boolean; at: number }>({ health: null, config: null, partial: false, failed: false, denied: false, at: 0 });
  const [revision, refresh] = useState(0);
  const [busy, setBusy] = useState(true);
  useEffect(() => {
    let live = true, running = false;
    const load = async () => {
      if (running) return;
      running = true;
      setBusy(true);
      const [h, c] = await Promise.allSettled([
        api<Health>('/api/admin/health', { lang, cache: 'no-store', signal: AbortSignal.timeout(12_000) }),
        api<Config>('/api/config', { cache: 'no-store', signal: AbortSignal.timeout(12_000) }),
      ]);
      if (!live) return;
      const error = h.status === 'rejected' ? h.reason : null;
      const partial = error instanceof ApiError && error.status === 503 && isHealth(error.data);
      const health = h.status === 'fulfilled' && isHealth(h.value) ? h.value : partial ? error.data as Health : null;
      setState(old => ({
        health: health ?? old.health, config: c.status === 'fulfilled' ? c.value : null,
        partial, failed: !health, denied: error instanceof ApiError && [401, 403].includes(error.status),
        at: health ? Date.now() : old.at,
      }));
      running = false;
      setBusy(false);
    };
    void load();
    const timer = setInterval(() => { void load(); }, 60_000);
    return () => { live = false; clearInterval(timer); };
  }, [lang, revision]);

  if (state.denied) return <NotFound />;
  const { health: h, config, partial, failed, at } = state;
  const unknown = tt('Дерек қолжетімсіз', 'Данные недоступны');
  const date = (v: string | number) => new Intl.DateTimeFormat(lang === 'ru' ? 'ru-RU' : 'kk-KZ', { dateStyle: 'short', timeStyle: 'short', timeZone: 'Asia/Almaty' }).format(new Date(v));
  const unavailable = partial || failed;
  return <div className="launch-admin">
    <p className="small muted">{tt('Әр 60 секунд сайын жаңарады. Уақыт — Алматы бойынша.', 'Обновляется каждые 60 секунд. Время — по Алматы.')}</p>
    <button className="btn ghost" type="button" disabled={busy} onClick={() => refresh(n => n + 1)}>{busy ? tt('Жаңартылып жатыр…', 'Обновление…') : tt('Қазір жаңарту', 'Обновить сейчас')}</button>
    <p className="tiny muted" role="status">{at ? tt(`Соңғы жауап: ${date(at)}`, `Последний ответ: ${date(at)}`) : ''}</p>
    {unavailable ? <p className="note urgent" role="alert">{tt('Барлық деректі жаңарту мүмкін болмады. Соңғы деректер толық емес не ескірген болуы мүмкін. Қайта жаңартыңыз.', 'Не удалось обновить все данные. Последние данные могут быть неполными или устаревшими. Повторите обновление.')}</p> : null}
    {!h && busy ? <Loading /> : null}
    {h ? <>
      <section className="section">
        <SectionHead title={tt('Негізгі қызметтер', 'Основные службы')} />
        <div className="group">
          <Row title={tt('Дерекқорға қосылу', 'Связь с базой данных')} sub={h.dbReachable ? tt('Байланыс бар', 'Связь есть') : tt('Қосылу мүмкін болмады', 'Не удалось подключиться')} chev={false} />
          <Row title={tt('Модерацияны күткен өтінімдер', 'Заявки, ожидающие модерации')} sub={h.moderationQueue === null ? unknown : String(h.moderationQueue)} chev={false} />
        </div>
      </section>
      <section className="section">
        <SectionHead title={tt('Telegram боты', 'Бот Telegram')} />
        <div className="group">
          <Row title={tt('Боттан хабар алу (webhook)', 'Приём сообщений от бота (webhook)')} sub={!h.telegram.configured ? tt('Бот бапталмаған', 'Бот не настроен') : !h.telegram.reachable ? tt('Telegram-ға қосылу мүмкін болмады', 'Не удалось связаться с Telegram') : h.telegram.installed ? tt('Қосылған', 'Подключён') : tt('Қосылмаған', 'Не подключён')} chev={false} />
          <Row title={tt('Өңдеуді күткен хабарлар', 'Сообщения, ожидающие обработки')} sub={h.telegram.reachable ? String(h.telegram.pendingUpdates) : unknown} chev={false} />
          <Row title={tt('Соңғы жеткізу қатесі', 'Последняя ошибка доставки')} sub={!h.telegram.reachable ? unknown : h.telegram.lastErrorAt ? date(h.telegram.lastErrorAt) : tt('Тіркелмеген', 'Не зарегистрирована')} chev={false} />
        </div>
      </section>
      <section className="section">
        <SectionHead title={tt('Күнделікті тапсырмалар', 'Ежедневные задачи')} />
        <p className="tiny muted">{tt('Соңғы сәтті орындалудан 26 сағат өтсе, тапсырма кешіккен деп белгіленеді. Өшірулі бөлімдер бөлек көрсетіледі.', 'Задача отмечается как просроченная через 26 часов после успешного выполнения. Отключённые разделы отмечены отдельно.')}</p>
        <div className="group">{JOBS.map(([endpoint, feature, kk, ru]) => {
          const run = h.cronRuns.find(r => r.endpoint === endpoint);
          const enabled = feature === null ? true : config ? config.features[feature] : null;
          const stale = !!run?.lastSuccessAt && at - Date.parse(run.lastSuccessAt) > 26 * 3600_000;
          const status = enabled === false ? tt('Бөлім өшірулі', 'Раздел отключён') : enabled === null ? tt('Бөлім күйі белгісіз', 'Состояние раздела неизвестно') : unavailable ? unknown : !run?.lastSuccessAt ? tt('Әлі орындалмаған', 'Ещё не выполнялась') : stale ? tt('Кешіккен', 'Просрочена') : tt('Уақытында орындалған', 'Выполнена вовремя');
          return <Row key={endpoint} title={tt(kk, ru)} sub={<><span className={enabled && !unavailable && (stale || !run?.lastSuccessAt) ? 'urgent' : ''}>{status}</span><br />{tt('Соңғы сәтті орындалуы: ', 'Последнее успешное выполнение: ')}{run?.lastSuccessAt ? date(run.lastSuccessAt) : unavailable ? unknown : tt('Әлі тіркелмеген', 'Ещё не зарегистрировано')}</>} chev={false} />;
        })}</div>
      </section>
      <section className="section">
        <SectionHead title={tt('Соңғы 24 сағаттағы браузер қателері', 'Ошибки браузера за последние 24 часа')} />
        <p className="tiny muted">{tt('Бірдей хабарлар біріктірілген. Жеке деректер жіберілмейді; белгісіз қате мәліметтерін API әдейі алып тастайды.', 'Одинаковые сообщения объединены. Личные данные не отправляются; API намеренно удаляет неизвестные подробности ошибок.')}</p>
        <div className="group">{h.errors?.length ? h.errors.map(e => <Row key={e.message} title={e.message} sub={tt(`Саны: ${e.count} · Соңғы рет: ${date(e.lastSeen)}`, `Количество: ${e.count} · Последний раз: ${date(e.lastSeen)}`)} chev={false} />) : <div className="empty">{unavailable ? unknown : tt('Қате тіркелмеген', 'Ошибок не зарегистрировано')}</div>}</div>
      </section>
    </> : null}
  </div>;
}
export function AdminHealth() { return <AdminOnly><HealthDashboard /></AdminOnly>; }
