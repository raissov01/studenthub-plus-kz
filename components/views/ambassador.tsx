'use client';
/* Ambassadors (prompt 06): /ambassador/apply, the /ambassador cabinet (university stats, add buttons, my
   suggestions, team, resources, referral link, workload + pause, renewal banner), /ambassador/rating,
   /certificate/<code>, the «Мүмкіндік ұсыну» / «Пайдалы орын» suggestion sheets, and the report button for
   ambassador-published records. */
import { useState } from 'react';
import { copyText, useApp } from '@/components/app';
import { Chip, Empty, Icon, Row, Seg, SectionHead } from '@/components/ui';
import { ErrorLine, errText, Feature, hasAccount, Loading, NotFound, SignInCard, useFeature, useLoad, useNeedAccount, ymd } from '@/components/views/common';
import { api, useAccount } from '@/lib/account';
import { BUILDINGS } from '@/lib/data/campus';
import { CITIES, OPP_TYPES } from '@/lib/data/static';
import { canCreateEvents } from '@/lib/events';

type Count = number | string;
interface Application { id: number; university: string; motivation: string; hours_per_week: string; status: 'pending' | 'approved' | 'rejected'; reason: string | null; created_at: string; reviewed_at: string | null }
interface Term {
  id: number; user_id: number; university: string; role_in_team: 'lead' | 'member'; term_start: string; term_end: string; hours_per_week: number;
  ref_code: string; status: string; paused_until: string | null; renew_answer: boolean | null; cert_code: string | null; cert_issued_at: string | null;
}
interface Cabinet {
  term: Term; days_left: number; renewal: { ask: boolean; answer: boolean | null };
  referral: { code: string; ref: string; link: string; joined: number };
  workload: { hours_per_week: number; items_this_week: number; paused_until: string | null; paused: boolean; available: boolean };
  team: { name: string; username: string | null; role_in_team: string; term_start: string; term_end: string; hours_per_week: number; paused: boolean }[];
  resources: { title_kk: string; title_ru: string; url: string }[];
}
interface UniStats { university: string; students: Count; weekly_active: Count; saved_deadlines: Count; events: Count; registrations: Count; check_ins: Count; by_faculty: Record<string, Count> }
export interface Submission { id: number; kind: string; status: 'pending' | 'approved' | 'rejected'; reason: string | null; university: string | null; payload: Record<string, unknown>; created_at: string; reviewed_at: string | null }
interface RatingRow { rank: number; university: string; weekly_active: Count; event_check_ins: Count; score: number }
interface Certificate { name: string; university: string; role_in_team: string; term_start: string; term_end: string; issued_at: string; events_organized: number; published_suggestions: number; verify_url: string }

export function Ambassador({ prm }: { prm: string }) {
  return (
    <Feature name="ambassador">
      {!prm ? <CabinetGate /> : prm === 'apply' ? <Apply /> : prm === 'rating' ? <Rating /> : <NotFound />}
    </Feature>
  );
}

/* ---------- apply ---------- */
function Apply() {
  const { tt, lang, toast } = useApp();
  const acc = useAccount();
  const app = useLoad<{ application: Application | null }>(hasAccount(acc) ? '/api/ambassador/application' : null);
  const [uni, setUni] = useState('');
  const [motivation, setMotivation] = useState('');
  const [hours, setHours] = useState('2-3');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  if (!acc.loaded) return <Loading />;
  if (!hasAccount(acc)) return <SignInCard text={tt('Өтінім беру үшін Telegram арқылы кіріңіз.', 'Чтобы подать заявку, войдите через Telegram.')} />;
  if (!app.data) return app.err ? <ErrorLine err={app.err} /> : <Loading />;
  const a = app.data.application;
  const m = motivation.trim();
  return (
    <>
      <div className="card">
        <h2>{tt('Амбассадор не істейді', 'Что делает амбассадор')}</h2>
        <p className="small">{tt('Өз университетіңіздің мүмкіндіктерін, іс-шараларын және пайдалы орындарын қосасыз. Бос тапсырма жоқ — тек нақты пайдалы әрекеттер. Мерзімі — бір семестр, аптасына 1–5 сағат.', 'Вы добавляете возможности, мероприятия и полезные места своего университета. Никаких пустых заданий — только реальная польза. Срок — один семестр, 1–5 часов в неделю.')}</p>
      </div>
      {a ? (
        <div className="card" role="status">
          <span className="eyebrow">{tt('Соңғы өтінім', 'Последняя заявка')} · {ymd(a.created_at)}</span>
          <b>{a.university}</b>
          <span className="badges"><span className={`badge ${a.status === 'approved' ? 'b-ok' : a.status === 'rejected' ? 'b-bad' : 'b-line'}`}>
            {a.status === 'approved' ? tt('Бекітілді', 'Одобрена') : a.status === 'rejected' ? tt('Қайтарылды', 'Отклонена') : tt('Тексерілуде', 'На проверке')}</span></span>
          {a.reason ? <p className="small">{tt('Себебі', 'Причина')}: {a.reason}</p> : null}
        </div>
      ) : null}
      {a?.status !== 'pending' && a?.status !== 'approved' ? (
        <form className="form" onSubmit={async e => {
          e.preventDefault();
          if (m.length < 10 || busy) return;
          setBusy(true); setErr('');
          try {
            const body: Record<string, string> = { motivation: m, hours_per_week: hours };
            if (uni.trim()) body.university = uni.trim();
            await api('/api/ambassador/apply', { method: 'POST', body, lang });
            toast(tt('Өтінім жіберілді', 'Заявка отправлена')); app.reload();
          } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
        }}>
          <div className="field"><label htmlFor="apUni">{tt('Университет', 'Университет')}</label>
            <input className="input" id="apUni" maxLength={120} placeholder={acc.user?.university || tt('Университет атауы', 'Название университета')} value={uni} onChange={e => setUni(e.target.value)} />
            <span className="tiny muted">{tt('Бос қалдырсаңыз — профильдегі университет.', 'Если оставить пустым — университет из профиля.')}</span></div>
          <div className="field"><label htmlFor="apWhy">{tt('Неге амбассадор болғыңыз келеді?', 'Почему хотите стать амбассадором?')}</label>
            <textarea className="input" id="apWhy" maxLength={300} value={motivation} onChange={e => setMotivation(e.target.value)} />
            <span className="tiny muted num">{m.length} / 300</span></div>
          <div className="field"><span className="lbl">{tt('Аптасына қанша сағат бере аласыз?', 'Сколько часов в неделю готовы уделять?')}</span>
            <Seg items={[['1-2', tt('1–2 сағ', '1–2 ч')], ['2-3', tt('2–3 сағ', '2–3 ч')], ['3-5', tt('3–5 сағ', '3–5 ч')]]} cur={hours} onPick={setHours} /></div>
          <ErrorLine err={err} />
          <button className="btn block" type="submit" disabled={m.length < 10 || busy}>{tt('Өтінім жіберу', 'Отправить заявку')}</button>
        </form>
      ) : null}
    </>
  );
}

/* ---------- cabinet ---------- */
function CabinetGate() {
  const { tt, go } = useApp();
  const acc = useAccount();
  const cab = useLoad<Cabinet>(acc.user ? '/api/ambassador/me' : null);
  if (!acc.loaded) return <Loading />;
  if (!acc.user) return <NotFound />;
  if (cab.code === 'not_ambassador' || cab.code === 'forbidden') {
    return <Empty icon="award" action={<button className="btn secondary" type="button" onClick={() => go('ambassador:apply')}>{tt('Амбассадор болу', 'Стать амбассадором')}</button>}>
      {tt('Кабинет тек белсенді амбассадорларға ашық.', 'Кабинет открыт только действующим амбассадорам.')}</Empty>;
  }
  if (cab.err) return <ErrorLine err={cab.err} />;
  if (!cab.data) return <Loading />;
  return <Cabinet_ c={cab.data} reload={cab.reload} />;
}

function Stat({ v, l }: { v: Count; l: string }) {
  return <div className="stat"><b>{v}</b><span>{l}</span></div>;
}

function Cabinet_({ c, reload }: { c: Cabinet; reload: () => void }) {
  const { tt, lang, toast, go, openSheet } = useApp();
  const acc = useAccount();
  const mentorsOn = useFeature('mentors');
  const eventsOn = useFeature('events');
  const stats = useLoad<UniStats>('/api/ambassador/stats');
  const subs = useLoad<{ submissions: Submission[] }>('/api/submissions/my');
  const rating = useLoad<{ mine: RatingRow | null }>('/api/ambassador/rating');
  const [busy, setBusy] = useState(false);
  const t = c.term, w = c.workload;
  const run = async (fn: () => Promise<unknown>, done: string) => {
    setBusy(true);
    try { await fn(); toast(done); reload(); } catch (e) { toast(errText(e)); } finally { setBusy(false); }
  };
  const s = stats.data;
  return (
    <>
      <div className="card">
        <span className="eyebrow">{t.role_in_team === 'lead' ? tt('Команда жетекшісі', 'Лидер команды') : tt('Команда мүшесі', 'Участник команды')}</span>
        <h2>{t.university}</h2>
        <span className="small muted">{tt(`Мерзімі: ${ymd(t.term_end)} дейін (${c.days_left} күн)`, `Срок: до ${ymd(t.term_end)} (${c.days_left} дн.)`)}</span>
        {rating.data?.mine ? <span className="badges"><span className="badge b-sun"><Icon n="award" s={12} /> {tt(`Университетіміз ${rating.data.mine.rank}-орында`, `Наш университет на ${rating.data.mine.rank}-м месте`)}</span></span> : null}
      </div>
      {c.renewal.ask ? (
        <div className="card banner"><b>{tt('Мерзім бітуге жақын. Жалғастырасың ба?', 'Срок скоро закончится. Продолжишь?')}</b>
          <div className="btn-row">
            <button className="btn" type="button" disabled={busy} onClick={() => run(() => api('/api/ambassador/renew', { method: 'POST', body: { continue: true }, lang }), tt('Өтінім әкімшіге жіберілді', 'Заявка отправлена администратору'))}>{tt('Иә, жалғастырамын', 'Да, продолжу')}</button>
            <button className="btn ghost" type="button" disabled={busy} onClick={() => run(() => api('/api/ambassador/renew', { method: 'POST', body: { continue: false }, lang }), tt('Рақмет! Мерзім соңында рөл аяқталады', 'Спасибо! Роль завершится в конце срока'))}>{tt('Жоқ', 'Нет')}</button>
          </div></div>
      ) : null}
      <section className="section"><SectionHead title={tt('Менің университетім', 'Мой университет')} />
        {s ? (
          <>
            <div className="stat-grid">
              <Stat v={s.students} l={tt('Студенттер', 'Студенты')} />
              <Stat v={s.weekly_active} l={tt('Апталық белсенділер', 'Активные за неделю')} />
              <Stat v={s.saved_deadlines} l={tt('Сақталған мерзімдер', 'Сохранённые сроки')} />
              <Stat v={s.events} l={tt('Іс-шаралар (90 күн)', 'Мероприятия (90 дней)')} />
              <Stat v={s.registrations} l={tt('Тіркелу', 'Записи')} />
              <Stat v={s.check_ins} l={tt('Келгендер', 'Пришли')} />
            </div>
            {Object.keys(s.by_faculty).length ? <div className="group">{Object.entries(s.by_faculty).map(([f, n]) => <Row key={f} title={f} end={<b className="num">{n}</b>} chev={false} />)}</div> : null}
            <p className="tiny muted">{tt('Тек жиынтық сандар. 5-тен аз топ «< 5» болып көрсетіледі.', 'Только общие числа. Группы меньше 5 показаны как «< 5».')}</p>
          </>
        ) : stats.err ? <ErrorLine err={stats.err} /> : <Loading />}
      </section>
      <section className="section"><SectionHead title={tt('Қосу', 'Добавить')} />
        <div className="group">
          <Row onClick={() => openSheet(<SuggestSheet kind="opportunity" onDone={subs.reload} />)} icon="globe" title={tt('Мүмкіндік', 'Возможность')} sub={tt('Грант, тағылымдама, байқау', 'Грант, стажировка, конкурс')} />
          {eventsOn && canCreateEvents(acc.user?.role) ? <Row go="organize:new" icon="calendar" title={tt('Іс-шара', 'Мероприятие')} sub={tt('Модерациясыз, тек өз университетіңізге', 'Без модерации, только для вашего вуза')} /> : null}
          <Row onClick={() => openSheet(<SuggestSheet kind="place" onDone={subs.reload} />)} icon="pin" title={tt('Пайдалы орын', 'Полезное место')} sub={tt('Асхана, баспахана, кітапхана…', 'Столовая, печать, библиотека…')} />
          {mentorsOn && t.role_in_team === 'lead' ? <Row go="ambassador:mentors" icon="cap" title={tt('Менторларды жұптау', 'Подбор менторов')} /> : null}
        </div>
      </section>
      <section className="section"><SectionHead title={tt('Шақыру сілтемесі', 'Ссылка-приглашение')} />
        <div className="card">
          <span className="code" style={{ overflowWrap: 'anywhere' }}>{c.referral.link}</span>
          <span className="small">{tt(`Маған қосылған студенттер: ${c.referral.joined}`, `Присоединились по моей ссылке: ${c.referral.joined}`)}</span>
          <button className="btn secondary" type="button" onClick={() => copyText(c.referral.link, toast, tt)}><Icon n="copy" s={18} /> {tt('Көшіру', 'Скопировать')}</button>
        </div>
      </section>
      <section className="section"><SectionHead title={tt('Жүктеме', 'Нагрузка')} />
        <div className="card">
          <span className="small">{tt(`Осы апта: ${w.items_this_week} / ${w.hours_per_week} пайдалы әрекет (≈1 сағат әрқайсысы)`, `На этой неделе: ${w.items_this_week} из ${w.hours_per_week} полезных действий (≈1 час каждое)`)}</span>
          <div className="meter"><i style={{ width: `${Math.min(100, (w.items_this_week / Math.max(1, w.hours_per_week)) * 100)}%` }} /></div>
          {w.paused
            ? <><span className="small">{tt(`Демалыс: ${ymd(w.paused_until)} дейін`, `Отдых до ${ymd(w.paused_until)}`)}</span>
              <button className="btn ghost" type="button" disabled={busy} onClick={() => run(() => api('/api/ambassador/pause', { method: 'DELETE', lang }), tt('Демалыс тоқтатылды', 'Отдых отменён'))}>{tt('Демалысты тоқтату', 'Завершить отдых')}</button></>
            : <button className="btn ghost" type="button" disabled={busy} onClick={() => run(() => api('/api/ambassador/pause', { method: 'POST', body: { weeks: 1 }, lang }), tt('Демалыс аптасы басталды', 'Неделя отдыха началась'))}>{tt('Демалыс аптасы', 'Неделя отдыха')}</button>}
          <span className="tiny muted">{tt('Уәде еткен сағаттан көп тапсырма берілмейді.', 'Заданий не больше обещанных часов.')}</span>
        </div>
      </section>
      <section className="section"><SectionHead title={tt('Менің ұсыныстарым', 'Мои предложения')} />
        <MySubmissions list={subs.data?.submissions ?? null} />
      </section>
      <section className="section"><SectionHead title={tt('Команда', 'Команда')} />
        <div className="group">{c.team.map((m, i) => (
          <Row key={i} icon="user" title={m.name + (m.username ? ` · @${m.username}` : '')} chev={false}
            sub={`${m.role_in_team === 'lead' ? tt('жетекші', 'лидер') : tt('мүше', 'участник')} · ${tt(`${m.hours_per_week} сағ/апта`, `${m.hours_per_week} ч/нед`)} · ${tt(`${ymd(m.term_end)} дейін`, `до ${ymd(m.term_end)}`)}${m.paused ? ' · ' + tt('демалыста', 'отдыхает') : ''}`} />
        ))}</div>
      </section>
      <section className="section"><SectionHead title={tt('Ресурстар', 'Ресурсы')} />
        <div className="group">{c.resources.length ? c.resources.map((r, i) => <Row key={i} href={r.url} icon="file" title={lang === 'ru' ? r.title_ru || r.title_kk : r.title_kk} />)
          : <div className="empty">{tt('Нұсқаулық пен үлгілер жақында қосылады', 'Инструкции и шаблоны появятся скоро')}</div>}</div>
      </section>
      {t.cert_code ? <button className="btn ghost block" type="button" onClick={() => go('certificate:' + t.cert_code)}><Icon n="award" s={18} /> {tt('Менің сертификатым', 'Мой сертификат')}</button> : null}
      <button className="link-btn" type="button" style={{ alignSelf: 'flex-start' }} onClick={() => go('ambassador:rating')}>{tt('Университеттер рейтингі', 'Рейтинг университетов')}</button>
    </>
  );
}

export function MySubmissions({ list }: { list: Submission[] | null }) {
  const { tt, lang } = useApp();
  const kind: Record<string, string> = {
    opportunity: tt('Мүмкіндік', 'Возможность'), place: tt('Пайдалы орын', 'Полезное место'), ambassador_application: tt('Амбассадор өтінімі', 'Заявка амбассадора'),
    ambassador_renewal: tt('Мерзімді ұзарту', 'Продление срока'), mentor_application: tt('Ментор өтінімі', 'Заявка ментора'),
  };
  if (!list) return <Loading />;
  if (!list.length) return <div className="group"><div className="empty">{tt('Әзірге ұсыныс жоқ', 'Предложений пока нет')}</div></div>;
  const title = (s: Submission) => {
    const p = s.payload as Record<string, string>;
    return (lang === 'ru' && (p.title_ru || p.name_ru)) || p.title_kk || p.name_kk || p.title || kind[s.kind] || s.kind;
  };
  return (
    <div className="group">{list.map(s => (
      <Row key={s.id} icon={s.kind === 'place' ? 'pin' : 'globe'} title={title(s)} chev={false}
        sub={<>{kind[s.kind] ?? s.kind} · {ymd(s.created_at)}
          <span className="badges"><span className={`badge ${s.status === 'approved' ? 'b-ok' : s.status === 'rejected' ? 'b-bad' : 'b-line'}`}>
            {s.status === 'approved' ? tt('Жарияланды', 'Опубликовано') : s.status === 'rejected' ? tt('Қайтарылды', 'Возвращено') : tt('Тексерілуде', 'На проверке')}</span></span>
          {s.reason && s.status === 'rejected' ? <span className="row-sub">{tt('Себебі', 'Причина')}: {s.reason}</span> : null}</>} />
    ))}</div>
  );
}

/* ---------- suggestion sheets (students and ambassadors) ---------- */
const PLACE_CATS: Record<string, [string, string]> = {
  canteen: ['Асхана', 'Столовая'], cafe: ['Кафе', 'Кафе'], print: ['Басып шығару', 'Печать'], copy: ['Көшірме', 'Ксерокс'],
  library: ['Кітапхана', 'Библиотека'], shop: ['Дүкен', 'Магазин'], pharmacy: ['Дәріхана', 'Аптека'], sport: ['Спорт', 'Спорт'],
  cowork: ['Коворкинг', 'Коворкинг'], other: ['Басқа', 'Другое'],
};

export function SuggestSheet({ kind, onDone }: { kind: 'opportunity' | 'place'; onDone?: () => void }) {
  const { tt, tp, L, lang, toast, closeSheet } = useApp();
  const [f, setF] = useState<Record<string, string>>({ type: 'grant', city: 'other', cat: 'canteen' });
  const [funded, setFunded] = useState(false);
  const [free, setFree] = useState(true);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const v = (k: string) => f[k] ?? '';
  const set = (k: string) => (e: { target: { value: string } }) => setF(x => ({ ...x, [k]: e.target.value }));
  const input = (k: string, label: string, max: number, o: { type?: string; area?: boolean; req?: boolean } = {}) => (
    <div className="field"><label htmlFor={'sg_' + k}>{label}</label>
      {o.area ? <textarea className="input" id={'sg_' + k} maxLength={max} value={v(k)} onChange={set(k)} required={o.req} />
        : <input className="input" id={'sg_' + k} type={o.type || 'text'} maxLength={max} value={v(k)} onChange={set(k)} required={o.req} />}</div>
  );
  const clean = (o: Record<string, unknown>) => Object.fromEntries(Object.entries(o).filter(([, x]) => x !== '' && x != null));
  const payload = kind === 'opportunity'
    ? clean({ type: v('type'), title_kk: v('title_kk').trim(), title_ru: v('title_ru').trim(), org_kk: v('org_kk').trim(), org_ru: v('org_ru').trim(), desc_kk: v('desc_kk').trim(), desc_ru: v('desc_ru').trim(), source_url: v('source_url').trim(), deadline: v('deadline'), city: v('city'), funded, free })
    : clean({ cat: v('cat'), name_kk: v('name_kk').trim(), name_ru: v('name_ru').trim(), address: v('address').trim(), building_id: v('building_id'), desc_kk: v('desc_kk').trim(), desc_ru: v('desc_ru').trim(), hours: v('hours').trim(), source_url: v('source_url').trim() });
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      setBusy(true); setErr('');
      try {
        await api('/api/submissions', { method: 'POST', body: { kind, payload }, lang });
        closeSheet(); toast(tt('Жіберілді. Әкімші тексерген соң жарияланады', 'Отправлено. Опубликуем после проверки администратором')); onDone?.();
      } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <h2>{kind === 'opportunity' ? tt('Мүмкіндік ұсыну', 'Предложить возможность') : tt('Пайдалы орын ұсыну', 'Предложить полезное место')}</h2>
      <p className="tiny muted">{tt('Автоматты жариялау жоқ: әкімші тексереді.', 'Автопубликации нет: проверит администратор.')}</p>
      {kind === 'opportunity' ? (
        <>
          <div className="field"><label htmlFor="sg_type">{tt('Түрі', 'Тип')}</label>
            <select className="input" id="sg_type" value={v('type')} onChange={set('type')}>{Object.entries(OPP_TYPES).map(([k, l]) => <option key={k} value={k}>{tp(l)}</option>)}</select></div>
          {input('title_kk', tt('Атауы (қазақша, 3–140)', 'Название (на казахском, 3–140)'), 140, { req: true })}
          {input('title_ru', tt('Атауы (орысша)', 'Название (на русском)'), 140)}
          {input('org_kk', tt('Ұйым (қазақша)', 'Организация (на казахском)'), 140, { req: true })}
          {input('org_ru', tt('Ұйым (орысша)', 'Организация (на русском)'), 140)}
          {input('desc_kk', tt('Сипаттама (қазақша, 10–2000)', 'Описание (на казахском, 10–2000)'), 2000, { area: true, req: true })}
          {input('desc_ru', tt('Сипаттама (орысша)', 'Описание (на русском)'), 2000, { area: true })}
          {input('source_url', tt('Ресми дереккөз сілтемесі (https)', 'Ссылка на официальный источник (https)'), 500, { type: 'url', req: true })}
          <div className="grid-2">
            {input('deadline', tt('Мерзімі', 'Дедлайн'), 10, { type: 'date' })}
            <div className="field"><label htmlFor="sg_city">{tt('Қала', 'Город')}</label>
              <select className="input" id="sg_city" value={v('city')} onChange={set('city')}>{Object.entries(CITIES).map(([k, c]) => <option key={k} value={k}>{L(c)}</option>)}</select></div>
          </div>
          <label className="check"><input type="checkbox" checked={funded} onChange={e => setFunded(e.target.checked)} /><span className="small">{tt('Қаржыландырылады', 'Финансируется')}</span></label>
          <label className="check"><input type="checkbox" checked={free} onChange={e => setFree(e.target.checked)} /><span className="small">{tt('Қатысу тегін (ақы сұрамайды)', 'Участие бесплатное')}</span></label>
        </>
      ) : (
        <>
          <div className="field"><label htmlFor="sg_cat">{tt('Түрі', 'Тип')}</label>
            <select className="input" id="sg_cat" value={v('cat')} onChange={set('cat')}>{Object.entries(PLACE_CATS).map(([k, l]) => <option key={k} value={k}>{tp(l)}</option>)}</select></div>
          {input('name_kk', tt('Атауы (қазақша)', 'Название (на казахском)'), 120, { req: true })}
          {input('name_ru', tt('Атауы (орысша)', 'Название (на русском)'), 120)}
          {input('address', tt('Мекенжайы', 'Адрес'), 200, { req: true })}
          <div className="field"><label htmlFor="sg_building_id">{tt('Корпус (міндетті емес)', 'Корпус (необязательно)')}</label>
            <select className="input" id="sg_building_id" value={v('building_id')} onChange={set('building_id')}><option value="">—</option>{BUILDINGS.map(b => <option key={b.id} value={b.id}>{b.n}. {L(b.name)}</option>)}</select></div>
          {input('hours', tt('Жұмыс уақыты', 'Часы работы'), 100)}
          {input('desc_kk', tt('Сипаттама (қазақша)', 'Описание (на казахском)'), 1000, { area: true })}
          {input('desc_ru', tt('Сипаттама (орысша)', 'Описание (на русском)'), 1000, { area: true })}
          {input('source_url', tt('Сілтеме (https, міндетті емес)', 'Ссылка (https, необязательно)'), 500, { type: 'url' })}
        </>
      )}
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={busy}>{tt('Жіберу', 'Отправить')}</button>
      </div>
    </form>
  );
}

/** «Мүмкіндік білесің бе? Ұсын»: the site's own form /suggest (it asks for sign-in and consent itself) */
export function SuggestButton() {
  const { tt, go } = useApp();
  return (
    <button className="btn ghost block suggest" type="button" onClick={() => go('suggest')}>
      <Icon n="plus" s={18} /> {tt('Мүмкіндік білесің бе? Ұсын', 'Знаешь возможность? Предложи')}
    </button>
  );
}

/** report button for ambassador-published records (amb-<n>): POST /api/reports */
export function CommunityReport({ kind, id }: { kind: 'opportunity' | 'place'; id: string }) {
  const { tt, lang, toast, openSheet, closeSheet } = useApp();
  const need = useNeedAccount();
  const open = () => {
    if (!need()) return;
    openSheet(<ReportReason onSend={async reason => {
      const r = await api<{ hidden: boolean }>('/api/reports', { method: 'POST', body: { kind, id, reason }, lang });
      closeSheet(); toast(r.hidden ? tt('Шағым қабылданды, жазба жасырылды', 'Жалоба принята, запись скрыта') : tt('Шағым жіберілді. Рақмет!', 'Жалоба отправлена. Спасибо!'));
    }} />);
  };
  return <button className="link-btn" type="button" style={{ alignSelf: 'flex-start' }} onClick={open}>{tt('Шағымдану', 'Пожаловаться')}</button>;
}

function ReportReason({ onSend }: { onSend: (reason: string) => Promise<void> }) {
  const { tt, closeSheet } = useApp();
  const [r, setR] = useState('');
  const [err, setErr] = useState('');
  const quick = [tt('Ақы сұрайды', 'Требуют оплату'), tt('Алаяқтық', 'Мошенничество'), tt('Мерзімі қате', 'Неверный срок'), tt('Ақпарат қате', 'Неверная информация')];
  const t = r.trim();
  return (
    <form className="form" onSubmit={async e => { e.preventDefault(); try { await onSend(t); } catch (e2) { setErr(errText(e2)); } }}>
      <h2>{tt('Шағымдану', 'Пожаловаться')}</h2>
      <div className="chips wrap">{quick.map(q => <Chip key={q} pressed={r === q} onClick={() => setR(q)}>{q}</Chip>)}</div>
      <div className="field"><label htmlFor="crReason">{tt('Себебі (3–300)', 'Причина (3–300)')}</label>
        <textarea className="input" id="crReason" maxLength={300} value={r} onChange={e => setR(e.target.value)} /></div>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={t.length < 3}>{tt('Жіберу', 'Отправить')}</button>
      </div>
    </form>
  );
}

/* ---------- rating ---------- */
function Rating() {
  const { tt } = useApp();
  const { data, err } = useLoad<{ rating: RatingRow[]; mine: RatingRow | null }>('/api/ambassador/rating');
  if (err) return <ErrorLine err={err} />;
  if (!data) return <Loading />;
  return (
    <>
      {data.mine ? <div className="card"><b>{tt(`Университетіміз ${data.mine.rank}-орында`, `Наш университет на ${data.mine.rank}-м месте`)}</b><span className="small muted">{data.mine.university}</span></div> : null}
      <p className="note">{tt('Командалық рейтинг: апталық белсенді студенттер + соңғы 30 күндегі іс-шараға келгендер.', 'Командный рейтинг: активные за неделю студенты + пришедшие на мероприятия за 30 дней.')}</p>
      <div className="group">{data.rating.length ? data.rating.map(r => (
        <Row key={r.university} title={`${r.rank}. ${r.university}`} chev={false}
          sub={tt(`Апталық белсенді: ${r.weekly_active} · Келгендер: ${r.event_check_ins}`, `Активные за неделю: ${r.weekly_active} · Пришли: ${r.event_check_ins}`)} end={<b className="num">{r.score}</b>} />
      )) : <div className="empty">{tt('Әзірге рейтинг жоқ', 'Рейтинга пока нет')}</div>}</div>
    </>
  );
}

/** «Университетіміз N-орында» line for students (profile) */
export function RatingEntry() {
  const { tt } = useApp();
  const on = useFeature('ambassador');
  const acc = useAccount();
  const { data } = useLoad<{ mine: RatingRow | null }>(on ? '/api/ambassador/rating' + (acc.user ? '?u=' + acc.user.id : '') : null);
  if (!on) return null;
  return <Row go="ambassador:rating" icon="award" title={tt('Университеттер рейтингі', 'Рейтинг университетов')}
    sub={data?.mine ? tt(`Университетіміз ${data.mine.rank}-орында`, `Наш университет на ${data.mine.rank}-м месте`) : tt('Амбассадор командаларының жұмысы', 'Работа команд амбассадоров')} />;
}

/* ---------- certificate ---------- */
export function CertificateView({ code }: { code: string }) {
  return <Feature name="ambassador"><Certificate_ code={code} /></Feature>;
}
function Certificate_({ code }: { code: string }) {
  const { tt } = useApp();
  const { data, code: ec, err } = useLoad<Certificate>(`/api/certificates/${encodeURIComponent(code)}`);
  if (ec === 'not_found') return <NotFound />;
  if (err) return <ErrorLine err={err} />;
  if (!data) return <Loading />;
  return (
    <>
      <article className="cert">
        <span className="eyebrow">Student Hub</span>
        <h1>{tt('Амбассадор сертификаты', 'Сертификат амбассадора')}</h1>
        <p className="cert-name">{data.name}</p>
        <p>{tt(`${data.university} университетінің амбассадор командасында ${data.role_in_team === 'lead' ? 'жетекші' : 'мүше'} болды.`, `Был(а) ${data.role_in_team === 'lead' ? 'лидером' : 'участником'} команды амбассадоров: ${data.university}.`)}</p>
        <dl className="kv">
          <dt>{tt('Мерзімі', 'Срок')}</dt><dd>{ymd(data.term_start)} — {ymd(data.term_end)}</dd>
          <dt>{tt('Ұйымдастырған іс-шаралар', 'Организовано мероприятий')}</dt><dd className="num">{data.events_organized}</dd>
          <dt>{tt('Жарияланған ұсыныстар', 'Опубликовано предложений')}</dt><dd className="num">{data.published_suggestions}</dd>
          <dt>{tt('Берілген күні', 'Дата выдачи')}</dt><dd>{ymd(data.issued_at)}</dd>
        </dl>
        <p className="tiny muted" style={{ overflowWrap: 'anywhere' }}>{tt('Тексеру', 'Проверка')}: {data.verify_url}</p>
      </article>
      <button className="btn ghost block no-print" type="button" onClick={() => window.print()}><Icon n="file" s={18} /> {tt('Басып шығару', 'Распечатать')}</button>
    </>
  );
}
