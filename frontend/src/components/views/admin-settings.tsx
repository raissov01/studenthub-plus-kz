'use client';
/* /admin/settings — «Баптаулар»: the support contacts and hours students see in the app (editable, saved to the
   history), and the feature switches (read only: the server's environment turns them on). Admin only. */
import { useState } from 'react';
import { useApp } from '@/components/app';
import { AdminOnly, ErrorLine, errText, Loading, useLoad, ymd } from '@/components/views/common';
import { api } from '@/lib/account';
import { who } from '@/lib/console';

export interface Support { email: string; telegram: string; phone: string; address: string; hoursKk: string; hoursRu: string }
interface Resp { features: Record<string, boolean>; featureOrder: string[]; support: Support; updatedAt: string | null; updatedBy: { id: number; name: string | null } | null }

const EMPTY: Support = { email: '', telegram: '', phone: '', address: '', hoursKk: '', hoursRu: '' };

/** what each switch turns on, in plain words */
export function featureName(k: string, tt: (kk: string, ru: string) => string): string {
  const m: Record<string, [string, string]> = {
    events: ['Іс-шаралар және QR арқылы келу', 'Мероприятия и отметка по QR'], ambassador: ['Амбассадорлар', 'Амбассадоры'], mentors: ['Менторлар', 'Менторы'],
    market: ['Тегін заттар', 'Отдам даром'], portfolio: ['Портфолио', 'Портфолио'], b2b: ['Жұмыс берушілер мен университеттер', 'Работодатели и университеты'],
    quiz: ['Квиздер', 'Квизы'], points: ['Ұпайлар мен сыйлықтар', 'Баллы и награды'], cafe: ['Кофехана', 'Кофейня'], print: ['Полиграфия', 'Полиграфия'],
    surveys: ['Сауалнамалар', 'Опросы'], feedback: ['Өтініштер', 'Обращения'], wellbeing: ['Психологиялық қолдау', 'Психологическая поддержка'],
    lostfound: ['Табылған заттар', 'Бюро находок'], courseqa: ['Пән бойынша сұрақтар', 'Вопросы по предметам'], roommates: ['Пәтерлес іздеу', 'Поиск соседей'],
    clubs: ['Клубтар', 'Клубы'], feeds: ['Мүмкіндіктер ағыны (ЖИ)', 'Лента возможностей (ИИ)'], streak: ['Күн сайынғы серия', 'Серия дней'], fairs: ['Жәрмеңкелер', 'Ярмарки'],
  };
  return m[k] ? tt(m[k][0], m[k][1]) : k;
}

export function AdminSettings() {
  return <AdminOnly><Settings /></AdminOnly>;
}

function Settings() {
  const r = useLoad<Resp>('/api/admin/config');
  if (!r.data) return <><ErrorLine err={r.err} />{r.err ? null : <Loading />}</>;
  return <SettingsBody key={r.data.updatedAt ?? ''} d={r.data} reload={r.reload} />;
}

function SettingsBody({ d, reload }: { d: Resp; reload: () => void }) {
  const { tt, lang, toast } = useApp();
  const [s, setS] = useState<Support>(() => ({ ...EMPTY, ...d.support }));
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const dirty = JSON.stringify(s) !== JSON.stringify({ ...EMPTY, ...d.support });
  const field = (k: keyof Support, label: string, extra: Record<string, string | number> = {}) => (
    <div className="field"><label htmlFor={'st_' + k}>{label}</label>
      <input className="input" id={'st_' + k} value={s[k]} onChange={e => setS(x => ({ ...x, [k]: e.target.value }))} {...extra} /></div>
  );
  return (
    <div className="stack">
      <section className="card stack">
        <h2 className="h2-small">{tt('Қолдау қызметі', 'Служба поддержки')}</h2>
        <p className="small muted">{tt('Студенттер мұны «Баптаулар» бетінде көреді. Бос қалдырған жол көрсетілмейді.', 'Студенты видят это на странице «Настройки». Пустые поля не показываются.')}</p>
        <form className="form" onSubmit={async e => {
          e.preventDefault();
          if (!dirty || busy) return;
          setBusy(true); setErr('');
          try { await api('/api/admin/config/support', { method: 'PUT', body: s, lang }); toast(tt('Сақталды', 'Сохранено')); reload(); }
          catch (x) { setErr(errText(x)); } finally { setBusy(false); }
        }}>
          <div className="grid-2">
            {field('email', 'Email', { type: 'email', inputMode: 'email', maxLength: 254 })}
            {field('telegram', 'Telegram', { maxLength: 64, placeholder: '@studenthub_help' })}
            {field('phone', tt('Телефон', 'Телефон'), { type: 'tel', inputMode: 'tel', maxLength: 40 })}
            {field('address', tt('Мекенжай', 'Адрес'), { maxLength: 200 })}
            {field('hoursKk', tt('Жұмыс уақыты (қазақша)', 'Часы работы (на казахском)'), { maxLength: 200, placeholder: 'Дс–Жм, 9:00–18:00' })}
            {field('hoursRu', tt('Жұмыс уақыты (орысша)', 'Часы работы (на русском)'), { maxLength: 200, placeholder: 'Пн–Пт, 9:00–18:00' })}
          </div>
          <ErrorLine err={err} />
          <button className="btn" type="submit" disabled={!dirty || busy}>{tt('Сақтау', 'Сохранить')}</button>
          {d.updatedAt ? <p className="tiny muted">{tt(`Соңғы өзгерткен: ${who(d.updatedBy)}, ${ymd(d.updatedAt)}`, `Последнее изменение: ${who(d.updatedBy)}, ${ymd(d.updatedAt)}`)}</p> : null}
        </form>
      </section>
      <section className="card stack">
        <h2 className="h2-small">{tt('Сайт бөлімдері', 'Разделы сайта')}</h2>
        <p className="small muted">{tt('Қай бөлім қосулы екені. Мұны сервер баптауы шешеді: өзгерту үшін әзірлеушіге жазыңыз.', 'Какие разделы включены. Это решает настройка сервера: чтобы изменить, напишите разработчику.')}</p>
        <ul className="feature-list" data-testid="feature-list">
          {(d.featureOrder?.length ? d.featureOrder : Object.keys(d.features)).map(k => (
            <li key={k}><span>{featureName(k, tt)}</span>
              <span className={`badge ${d.features[k] ? 'b-ok' : 'b-line'}`}>{d.features[k] ? tt('қосулы', 'включено') : tt('өшірулі', 'выключено')}</span></li>
          ))}
        </ul>
      </section>
    </div>
  );
}

/** «Қолдау қызметі» for students (the profile's settings page): only the fields the admin filled in */
export function SupportCard() {
  const { tt, lang } = useApp();
  const r = useLoad<{ support: Support }>('/api/support');
  const s = r.data?.support;
  if (!s) return null;
  const hours = lang !== 'kk' ? s.hoursRu || s.hoursKk : s.hoursKk || s.hoursRu;
  const rows: [string, React.ReactNode][] = [
    ...(s.email ? [['Email', <a key="e" href={`mailto:${s.email}`}>{s.email}</a>] as [string, React.ReactNode]] : []),
    ...(s.telegram ? [['Telegram', <a key="t" href={`https://t.me/${s.telegram}`} target="_blank" rel="noreferrer">@{s.telegram}</a>] as [string, React.ReactNode]] : []),
    ...(s.phone ? [[tt('Телефон', 'Телефон'), <a key="p" href={`tel:${s.phone.replace(/[^\d+]/g, '')}`}>{s.phone}</a>] as [string, React.ReactNode]] : []),
    ...(s.address ? [[tt('Мекенжай', 'Адрес'), s.address] as [string, React.ReactNode]] : []),
    ...(hours ? [[tt('Жұмыс уақыты', 'Часы работы'), hours] as [string, React.ReactNode]] : []),
  ];
  if (!rows.length) return null;
  return (
    <section className="section" data-testid="support-card">
      <h2 className="h2-small">{tt('Қолдау қызметі', 'Служба поддержки')}</h2>
      <dl className="kv card">{rows.map(([k, v]) => <div key={k} className="kv-row"><dt>{k}</dt><dd>{v}</dd></div>)}</dl>
    </section>
  );
}
