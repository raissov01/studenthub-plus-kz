'use client';
/* University cabinet (prompt 10): /university for bound university staff. Official announcements and opportunities
   (published at once with the «Университеттен ресми» badge; hide / delete), a link to the event organizer screens,
   and aggregate statistics only — every group under 5 people arrives as «< 5» from the API, no names, no
   per-student rows — with a CSV download. Anyone who is not bound staff gets the neutral not-found screen. */
import { useState } from 'react';
import { useApp } from '@/components/app';
import { ConfirmSheet, Icon, Row, SectionHead } from '@/components/ui';
import { ErrorLine, errCode, errText, Feature, hasAccount, Loading, NotFound, SignInCard, useFeature, useLoad, ymd } from '@/components/views/common';
import { api, apiDownload, useAccount } from '@/lib/account';
import type { Count, UniversityPost, UniversityStats } from '@/lib/b2b';
import { CITIES, HOME_CITIES, OPP_TYPES } from '@/lib/data/static';
import { isoLocal, T0 } from '@/lib/format';
import type { LText } from '@/lib/types';

export function UniversityCabinet() {
  return <Feature name="b2b"><Gate /></Feature>;
}

function Gate() {
  const { tt } = useApp();
  const acc = useAccount();
  const me = useLoad<{ university: string }>(hasAccount(acc) ? '/api/university/me' : null);
  if (!acc.loaded) return <Loading />;
  if (!hasAccount(acc)) return <SignInCard text={tt('Университет кабинеті Telegram аккаунтымен ашылады.', 'Кабинет университета открывается через аккаунт Telegram.')} />;
  if (me.code === 'not_staff' || me.code === 'not_found') return <NotFound />;
  if (me.err) return <ErrorLine err={me.err} />;
  if (!me.data) return <Loading />;
  return <Cabinet university={me.data.university} />;
}

function Cabinet({ university }: { university: string }) {
  const { tt, go, openSheet } = useApp();
  const events = useFeature('events');
  const posts = useLoad<{ posts: UniversityPost[] }>('/api/university/posts');
  return (
    <>
      <div className="card">
        <span className="eyebrow">{tt('Университет қызметкері', 'Сотрудник университета')}</span>
        <h2 style={{ overflowWrap: 'anywhere' }}>{university}</h2>
        <p className="tiny muted">{tt('Жазбаларыңыз модерациясыз бірден жарияланады және «Университеттен ресми» белгісімен көрінеді.', 'Ваши записи публикуются сразу, без модерации, со значком «Официально от университета».')}</p>
        <div className="btn-row">
          <button className="btn" type="button" onClick={() => openSheet(<PostSheet kind="announcement" onDone={posts.reload} />)}><Icon n="bell" s={18} /> {tt('Хабарландыру', 'Объявление')}</button>
          <button className="btn secondary" type="button" onClick={() => openSheet(<PostSheet kind="opportunity" onDone={posts.reload} />)}><Icon n="globe" s={18} /> {tt('Мүмкіндік', 'Возможность')}</button>
        </div>
        {events ? <button className="btn ghost" type="button" onClick={() => go('organize')}><Icon n="calendar" s={18} /> {tt('Университет іс-шаралары', 'Мероприятия университета')}</button> : null}
      </div>
      <section className="section">
        <SectionHead title={tt('Жазбаларым', 'Мои записи')} more={<span className="tiny muted num">{posts.data?.posts.length ?? ''}</span>} />
        {!posts.data ? (posts.err ? <ErrorLine err={posts.err} /> : <Loading />) : posts.data.posts.length ? (
          <div className="list">{posts.data.posts.map(p => <PostCard key={p.id} p={p} onDone={posts.reload} />)}</div>
        ) : <div className="group"><div className="empty">{tt('Әзірге жазба жоқ', 'Записей пока нет')}</div></div>}
      </section>
      <Stats />
    </>
  );
}

const str = (v: unknown) => (typeof v === 'string' ? v : '');

function PostCard({ p, onDone }: { p: UniversityPost; onDone: () => void }) {
  const { tt, L, lang, toast, openSheet, go } = useApp();
  const [busy, setBusy] = useState(false);
  const title = L({ kk: str(p.payload.title_kk), ru: str(p.payload.title_ru) });
  const run = async (fn: () => Promise<unknown>, done: string) => {
    setBusy(true);
    try { await fn(); toast(done); onDone(); } catch (e) { toast(errText(e)); } finally { setBusy(false); }
  };
  return (
    <div className="card">
      <div className="badges">
        <span className="badge b-sky">{p.kind === 'announcement' ? tt('Хабарландыру', 'Объявление') : tt('Мүмкіндік', 'Возможность')}</span>
        {p.hidden ? <span className="badge b-line">{tt('Жасырылған', 'Скрыто')}</span> : <span className="badge b-ok"><Icon n="cap" s={12} /> {tt('Университеттен ресми', 'Официально от университета')}</span>}
      </div>
      <b style={{ overflowWrap: 'anywhere' }}>{title}</b>
      <span className="tiny muted">{ymd(p.publishedAt)}{p.deadline ? ` · ${tt('мерзімі', 'срок')} ${ymd(p.deadline)}` : ''}</span>
      <div className="btn-row">
        {p.kind === 'opportunity' && !p.hidden ? <button className="btn ghost" type="button" onClick={() => go('opp:uni-' + p.id)}>{tt('Ашу', 'Открыть')}</button> : null}
        <button className="btn ghost" type="button" disabled={busy} onClick={() => run(() => api(`/api/university/posts/${p.id}`, { method: 'PATCH', body: { hidden: !p.hidden }, lang }), p.hidden ? tt('Қайта көрсетілді', 'Снова видно') : tt('Жасырылды', 'Скрыто'))}>
          {p.hidden ? tt('Көрсету', 'Показать') : tt('Жасыру', 'Скрыть')}</button>
        <button className="btn danger" type="button" disabled={busy} onClick={() => openSheet(<ConfirmSheet msg={tt('Жазба біржола өшірілсін бе?', 'Удалить запись навсегда?')}
          yes={() => run(() => api(`/api/university/posts/${p.id}`, { method: 'DELETE', lang }), tt('Өшірілді', 'Удалено'))} />)}>{tt('Өшіру', 'Удалить')}</button>
      </div>
    </div>
  );
}

function BiList({ id, label, value, onChange }: { id: string; label: string; value: LText[]; onChange: (v: LText[]) => void }) {
  const { tt } = useApp();
  return (
    <div className="field" role="group" aria-labelledby={id}>
      <span className="lbl" id={id}>{label}</span>
      {value.map((r, i) => (
        <div key={i} className="grid-2">
          <input className="input" aria-label={`${label} ${i + 1} (${tt('қазақша', 'на казахском')})`} placeholder={tt('қазақша', 'на казахском')} maxLength={200} value={r.kk} onChange={e => onChange(value.map((x, k) => (k === i ? { ...x, kk: e.target.value } : x)))} />
          <input className="input" aria-label={`${label} ${i + 1} (${tt('орысша', 'на русском')})`} placeholder={tt('орысша', 'на русском')} maxLength={200} value={r.ru} onChange={e => onChange(value.map((x, k) => (k === i ? { ...x, ru: e.target.value } : x)))} />
        </div>
      ))}
      {value.length < 10 ? <button className="link-btn" type="button" style={{ alignSelf: 'flex-start' }} onClick={() => onChange([...value, { kk: '', ru: '' }])}>+ {tt('Қосу', 'Добавить')}</button> : null}
    </div>
  );
}

function PostSheet({ kind, onDone }: { kind: 'announcement' | 'opportunity'; onDone: () => void }) {
  const { tt, tp, L, lang, toast, closeSheet } = useApp();
  const [f, setF] = useState<Record<string, string>>({ type: 'grant', city: 'astana' });
  const [funded, setFunded] = useState(false);
  const [free, setFree] = useState(true);
  const [docs, setDocs] = useState<LText[]>([{ kk: '', ru: '' }]);
  const [steps, setSteps] = useState<LText[]>([{ kk: '', ru: '' }]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const v = (k: string) => (f[k] ?? '').trim();
  const set = (k: string) => (e: { target: { value: string } }) => setF(x => ({ ...x, [k]: e.target.value }));
  const today = isoLocal(T0());
  const field = (k: string, label: string, max: number, o: { area?: boolean; type?: string; min?: string } = {}) => (
    <div className="field"><label htmlFor={'up_' + k}>{label}</label>
      {o.area ? <textarea className="input" id={'up_' + k} maxLength={max} value={f[k] ?? ''} onChange={set(k)} />
        : <input className="input" id={'up_' + k} type={o.type || 'text'} min={o.min} maxLength={max} value={f[k] ?? ''} onChange={set(k)} />}</div>
  );
  const bl = (l: LText[]) => l.map(x => ({ kk: x.kk.trim(), ru: x.ru.trim() })).filter(x => x.kk);
  const url = v(kind === 'announcement' ? 'url' : 'source_url');
  const okUrl = !url || /^https:\/\/[^\s<>"'`]{2,}$/i.test(url);
  const ok = kind === 'announcement'
    ? v('title_kk').length >= 3 && v('body_kk').length >= 10 && okUrl
    : v('title_kk').length >= 3 && v('org_kk').length >= 2 && v('desc_kk').length >= 10 && !!url && okUrl;
  const payload = kind === 'announcement'
    ? { title_kk: v('title_kk'), title_ru: v('title_ru'), body_kk: v('body_kk'), body_ru: v('body_ru'), url, until: v('until') }
    : {
      type: v('type'), title_kk: v('title_kk'), title_ru: v('title_ru'), org_kk: v('org_kk'), org_ru: v('org_ru'), desc_kk: v('desc_kk'), desc_ru: v('desc_ru'),
      source_url: url, city: v('city'), deadline: v('deadline'), funded, free, docs: bl(docs), steps: bl(steps),
    };
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault(); if (!ok || busy) return;
      setBusy(true); setErr('');
      try {
        await api('/api/university/posts', { method: 'POST', body: { kind, payload }, lang });
        closeSheet(); toast(tt('Жарияланды', 'Опубликовано')); onDone();
      } catch (e2) { setErr(errText(e2)); } finally { setBusy(false); }
    }}>
      <h2>{kind === 'announcement' ? tt('Ресми хабарландыру', 'Официальное объявление') : tt('Ресми мүмкіндік', 'Официальная возможность')}</h2>
      {kind === 'announcement' ? (
        <>
          {field('title_kk', tt('Тақырып (қазақша, 3–140)', 'Заголовок (на казахском, 3–140)'), 140)}
          {field('title_ru', tt('Тақырып (орысша)', 'Заголовок (на русском)'), 140)}
          {field('body_kk', tt('Мәтін (қазақша, 10–2000)', 'Текст (на казахском, 10–2000)'), 2000, { area: true })}
          {field('body_ru', tt('Мәтін (орысша)', 'Текст (на русском)'), 2000, { area: true })}
          {field('url', tt('Сілтеме (https, міндетті емес)', 'Ссылка (https, необязательно)'), 300, { type: 'url' })}
          {field('until', tt('Көрсету мерзімі (міндетті емес)', 'Показывать до (необязательно)'), 10, { type: 'date', min: today })}
        </>
      ) : (
        <>
          <div className="field"><label htmlFor="up_type">{tt('Түрі', 'Тип')}</label>
            <select className="input" id="up_type" value={f.type} onChange={set('type')}>{Object.entries(OPP_TYPES).map(([k, l]) => <option key={k} value={k}>{tp(l)}</option>)}</select></div>
          {field('title_kk', tt('Атауы (қазақша, 3–140)', 'Название (на казахском, 3–140)'), 140)}
          {field('title_ru', tt('Атауы (орысша)', 'Название (на русском)'), 140)}
          {field('org_kk', tt('Ұйым (қазақша)', 'Организация (на казахском)'), 140)}
          {field('org_ru', tt('Ұйым (орысша)', 'Организация (на русском)'), 140)}
          {field('desc_kk', tt('Сипаттама (қазақша, 10–2000)', 'Описание (на казахском, 10–2000)'), 2000, { area: true })}
          {field('desc_ru', tt('Сипаттама (орысша)', 'Описание (на русском)'), 2000, { area: true })}
          {field('source_url', tt('Ресми дереккөз сілтемесі (https)', 'Ссылка на официальный источник (https)'), 300, { type: 'url' })}
          <div className="grid-2">
            {field('deadline', tt('Мерзімі', 'Дедлайн'), 10, { type: 'date', min: today })}
            <div className="field"><label htmlFor="up_city">{tt('Қала', 'Город')}</label>
              <select className="input" id="up_city" value={f.city} onChange={set('city')}>{HOME_CITIES.map(k => <option key={k} value={k}>{L(CITIES[k])}</option>)}</select></div>
          </div>
          <label className="check"><input type="checkbox" checked={funded} onChange={e => setFunded(e.target.checked)} /><span className="small">{tt('Қаржыландырылады', 'Финансируется')}</span></label>
          <label className="check"><input type="checkbox" checked={free} onChange={e => setFree(e.target.checked)} /><span className="small">{tt('Қатысу тегін', 'Участие бесплатное')}</span></label>
          <BiList id="upDocs" label={tt('Құжаттар', 'Документы')} value={docs} onChange={setDocs} />
          <BiList id="upSteps" label={tt('Қалай рәсімдеу керек (қадамдар)', 'Как оформить (шаги)')} value={steps} onChange={setSteps} />
        </>
      )}
      {!okUrl ? <p className="tiny urgent">{tt('Сілтеме https:// деп басталуы керек', 'Ссылка должна начинаться с https://')}</p> : null}
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={!ok || busy}>{tt('Жариялау', 'Опубликовать')}</button>
      </div>
    </form>
  );
}

/* ---------- aggregate statistics ---------- */
function Stat({ v, l }: { v: Count | null; l: string }) {
  return <div className="stat"><b className="num">{v == null ? '—' : v}</b><span>{l}</span></div>;
}

function Breakdown({ title, rows, label }: { title: string; rows: Record<string, Count>; label: (k: string) => string }) {
  const { tt } = useApp();
  const list = Object.entries(rows);
  return (
    <section className="section">
      <h3>{title}</h3>
      {list.length ? <div className="group">{list.map(([k, n]) => <Row key={k} title={label(k)} end={<b className="num">{n}</b>} chev={false} />)}</div>
        : <div className="group"><div className="empty">{tt('Дерек жоқ', 'Нет данных')}</div></div>}
    </section>
  );
}

function Stats() {
  const { tt, lang, toast } = useApp();
  const s = useLoad<UniversityStats>('/api/university/stats');
  const [busy, setBusy] = useState(false);
  if (s.code === 'not_staff') return null;
  return (
    <section className="section">
      <SectionHead title={tt('Жиынтық статистика', 'Сводная статистика')} more={s.data ? <span className="tiny muted">{tt(`соңғы ${s.data.periodDays} күн`, `за ${s.data.periodDays} дн.`)}</span> : null} />
      <p className="tiny muted">{tt('Тек жиынтық сандар. 5 адамнан аз топ «< 5» болып көрсетіледі; жеке аттар мен жеке студенттің әрекеті жоқ.', 'Только сводные числа. Группы меньше 5 человек показаны как «< 5»; имён и действий отдельных студентов нет.')}</p>
      {!s.data ? (s.err ? <ErrorLine err={s.err} /> : <Loading />) : (
        <>
          <div className="stat-grid">
            <Stat v={s.data.students} l={tt('Студенттер (аккаунт)', 'Студентов (аккаунты)')} />
            <Stat v={s.data.weeklyActive} l={tt('Аптасына белсенді', 'Активны за неделю')} />
            <Stat v={s.data.savedDeadlines} l={tt('Сақталған мерзімдер', 'Сохранённые сроки')} />
            <Stat v={s.data.events} l={tt('Іс-шаралар', 'Мероприятия')} />
            <Stat v={s.data.registrations} l={tt('Тіркелулер', 'Регистрации')} />
            <Stat v={s.data.checkIns} l={tt('Келгендер (QR)', 'Пришли (QR)')} />
            <Stat v={s.data.ratings} l={tt('Бағалар', 'Оценки')} />
            <Stat v={s.data.avgRating != null ? s.data.avgRating.toFixed(1) : null} l={tt('Орташа баға (5-тен)', 'Средняя оценка (из 5)')} />
            <Stat v={s.data.mentors} l={tt('Менторлар', 'Менторы')} />
            <Stat v={s.data.mentorRequests} l={tt('Ментор сұраныстары', 'Запросы на ментора')} />
            <Stat v={s.data.mentorPairs} l={tt('Ментор жұптары', 'Пары с ментором')} />
          </div>
          <Breakdown title={tt('Факультет бойынша', 'По факультетам')} rows={s.data.byFaculty} label={k => k || tt('Көрсетілмеген', 'Не указан')} />
          <Breakdown title={tt('Курс бойынша', 'По курсам')} rows={s.data.byCourse} label={k => (/^\d+$/.test(k) ? tt(`${k}-курс`, `${k} курс`) : k || tt('Көрсетілмеген', 'Не указан'))} />
          <button className="btn ghost" type="button" disabled={busy} onClick={async () => {
            setBusy(true);
            try { await apiDownload('/api/university/stats.csv', 'studenthub-stats.csv', lang); } catch (e) { toast(errCode(e) ? errText(e) : tt('Файлды жүктеу мүмкін болмады', 'Не удалось скачать файл')); } finally { setBusy(false); }
          }}><Icon n="file" s={18} /> {tt('CSV жүктеу', 'Скачать CSV')}</button>
        </>
      )}
    </section>
  );
}
