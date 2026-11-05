'use client';
/* /admin — the role-scoped console. GET /api/console/me says which panels the signed-in person has: the super admin
   (role admin) gets the overview with real numbers and every management screen; a moderator the moderation
   workspace; café / print shop staff their shop; club admins their clubs; university staff their voice inbox;
   an employer the company cabinet; ambassadors and mentors their cabinets. Anyone without a panel gets the same
   neutral screen as an unknown page. The server enforces every right again on each request. */
import { useState, type ReactNode } from 'react';
import { useApp } from '@/components/app';
import { Icon, Row, SectionHead } from '@/components/ui';
import { ErrorLine, Loading, NotFound, useLoad } from '@/components/views/common';
import { ModerationLinks } from '@/components/views/moderation';
import { roleLabel } from '@/components/views/account';
import { api, ApiError, useAccount, type Role } from '@/lib/account';
import { money } from '@/lib/cafe';
import { bars, panelCount, total, type DayCount, type Panels } from '@/lib/console';
import type { IconName } from '@/lib/icons';

interface Report {
  devices: number; returned_day1: number; eligible_day1: number; returned_day7: number; eligible_day7: number; saved_first: number;
  from_share: number; ambassadors: Record<string, number> | null; feedback_avg: number; feedback_count: number; feedback_recent: string[] | null;
}
export interface UserRow { id: number; telegramId: number | null; username: string | null; name: string | null; role: Role; university: string | null; consentAt: string | null; createdAt: string }
interface Dashboard {
  users: { total: number; accounts: number; new7: number; new30: number; blocked: number; moderators: number; byRole: Record<string, number> };
  signups: DayCount[];
  active: { dau: number; wau: number; mau: number; since: string | null; days: DayCount[] };
  devices: { dau: number; wau: number };
  content: Record<string, number>;
  pending: { queue: number; queueModerator: number; reports: number; feedbackHeld: number; feedbackOverdue: number; redemptions: number; employers: number; cafeRefunds: number; printRefunds: number };
  shops: { cafe: { orders: number; done: number; revenue: number }; print: { orders: number; done: number; revenue: number } };
  features: Record<string, boolean>;
}

export const ROLES: Role[] = ['student', 'ambassador', 'admin', 'university_staff', 'employer'];
const pct = (a: number, b: number) => (b > 0 ? `${Math.round((a / b) * 100)}%` : '—');

/** change users.role, with the reason the audit log keeps */
export function RoleSheet({ u, onDone }: { u: UserRow; onDone: () => void }) {
  const { tt, lang, toast, closeSheet } = useApp();
  const [role, setRole] = useState<Role>(u.role);
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const okReason = reason.trim().length >= 3 && reason.trim().length <= 500;
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      if (!okReason || role === u.role) return;
      setBusy(true); setErr('');
      try {
        await api('/api/admin/role', { method: 'POST', body: { userId: u.id, role, reason: reason.trim() }, lang });
        closeSheet(); toast(tt('Рөл өзгертілді', 'Роль изменена')); onDone();
      } catch (e2) { setErr(e2 instanceof ApiError ? e2.message : String(e2)); } finally { setBusy(false); }
    }}>
      <h2>{u.name || (u.username ? '@' + u.username : `#${u.id}`)}</h2>
      <p className="small muted">{tt('Қазіргі рөлі', 'Текущая роль')}: {roleLabel(u.role, tt)}</p>
      <div className="field"><label htmlFor="admRole">{tt('Жаңа рөл', 'Новая роль')}</label>
        <select className="input" id="admRole" value={role} onChange={e => setRole(e.target.value as Role)}>
          {ROLES.map(r => <option key={r} value={r}>{roleLabel(r, tt)}</option>)}
        </select></div>
      <div className="field"><label htmlFor="admReason">{tt('Себебі (міндетті, 3–500 таңба)', 'Причина (обязательно, 3–500 символов)')}</label>
        <textarea className="input" id="admReason" maxLength={500} value={reason} onChange={e => setReason(e.target.value)} />
        <span className="tiny muted">{tt('Себеп журналға жазылады.', 'Причина записывается в журнал.')}</span></div>
      {err ? <p className="small urgent" role="alert">{err}</p> : null}
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn" type="submit" disabled={busy || !okReason || role === u.role}>{role === 'student' && u.role !== 'student' ? tt('Рөлді алу', 'Снять роль') : tt('Рөл беру', 'Выдать роль')}</button>
      </div>
    </form>
  );
}

export function Stat({ value, label, tone }: { value: string | number; label: string; tone?: 'warn' }) {
  return <div className={`stat${tone ? ' stat-' + tone : ''}`}><b className="num">{value}</b><span>{label}</span></div>;
}

/** a small flat bar chart of daily counts; the numbers are also in the accessible label */
export function DayBars({ series, label }: { series: DayCount[]; label: string }) {
  const { tt } = useApp();
  const b = bars(series);
  if (!b.length) return null;
  const last = b[b.length - 1];
  return (
    <figure className="bars-fig">
      <div className="bars" role="img" aria-label={`${label}: ${b.map(d => `${d.day.slice(5)} ${d.n}`).join(', ')}`}>
        {b.map((d, i) => <i key={d.day} className={i === b.length - 1 ? 'today' : ''} style={{ height: `${d.h}%` }} title={`${d.day}: ${d.n}`} />)}
      </div>
      <figcaption className="tiny muted num">
        <span>{b[0].day.slice(5)}</span><span>{label} · {tt('барлығы', 'всего')} {total(series).toLocaleString('ru-RU')} · {tt('бүгін', 'сегодня')} {last.n.toLocaleString('ru-RU')}</span><span>{last.day.slice(5)}</span>
      </figcaption>
    </figure>
  );
}

/** one workspace on the console home: a title, a few honest numbers, the ways in */
function PanelCard({ icon, title, badge, stats, children }: {
  icon: IconName; title: string; badge?: string; stats?: [string | number, string][]; children?: ReactNode;
}) {
  return (
    <article className="card panel-card">
      <div className="panel-head">
        <span className="ico" aria-hidden="true"><Icon n={icon} /></span>
        <h3>{title}</h3>
        {badge ? <span className="badge b-line">{badge}</span> : null}
      </div>
      {stats?.length ? (
        <dl className="panel-stats">{stats.map(([v, l]) => <div key={l}><dt>{l}</dt><dd className="num">{v}</dd></div>)}</dl>
      ) : null}
      {children ? <div className="panel-actions">{children}</div> : null}
    </article>
  );
}

function Go({ to, children, primary }: { to: string; children: ReactNode; primary?: boolean }) {
  const { go } = useApp();
  return <button className={`btn ${primary ? 'secondary' : 'ghost'}`} type="button" onClick={() => go(to)}>{children}</button>;
}

function shopRole(r: string, tt: (kk: string, ru: string) => string) {
  return r === 'manager' ? tt('менеджер', 'менеджер') : r === 'barista' ? tt('бариста', 'бариста') : r === 'operator' ? tt('оператор', 'оператор') : r;
}

function MyPanels({ p }: { p: Panels }) {
  const { tt, lang } = useApp();
  const nm = (o: { nameKk: string; nameRu: string | null }) => (lang !== 'kk' && o.nameRu ? o.nameRu : o.nameKk);
  const cards: ReactNode[] = [];
  if (p.moderator && !p.admin && p.moderation) {
    cards.push(
      <PanelCard key="mod" icon="shield" title={tt('Модерация', 'Модерация')} badge={tt('модератор', 'модератор')}
        stats={[[p.moderation.pending, tt('кезекте', 'в очереди')], [p.moderation.reports, tt('шағым', 'жалоб')]]}>
        <Go to="admin:moderation" primary>{tt('Жұмыс орнын ашу', 'Открыть рабочее место')}</Go>
      </PanelCard>,
    );
  }
  for (const s of p.cafes) {
    cards.push(
      <PanelCard key={'c' + s.id} icon="cup" title={nm(s)} badge={shopRole(s.role, tt)}
        stats={[[s.open, tt('тақтада', 'на доске')], [s.active ? tt('ашық', 'открыта') : tt('өшірулі', 'выключена'), tt('күйі', 'статус')]]}>
        <Go to="cafe:staff" primary>{tt('Тапсырыстар тақтасы', 'Доска заказов')}</Go>
        <Go to={`admin:shop?cafe=${s.id}`}>{tt('Панель', 'Панель')}</Go>
      </PanelCard>,
    );
  }
  for (const s of p.printShops) {
    cards.push(
      <PanelCard key={'p' + s.id} icon="file" title={nm(s)} badge={shopRole(s.role, tt)}
        stats={[[s.open, tt('кезекте', 'в очереди')], [s.active ? tt('ашық', 'открыта') : tt('өшірулі', 'выключена'), tt('күйі', 'статус')]]}>
        <Go to="print:staff" primary>{tt('Кезек тақтасы', 'Доска очереди')}</Go>
        <Go to={`admin:shop?print=${s.id}`}>{tt('Панель', 'Панель')}</Go>
      </PanelCard>,
    );
  }
  for (const c of p.clubs) {
    cards.push(
      <PanelCard key={'k' + c.id} icon="user" title={nm(c)} badge={c.status === 'active' ? tt('клуб әкімшісі', 'админ клуба') : tt('архивте', 'в архиве')}
        stats={[[c.members, tt('мүше', 'участников')], [c.requests, tt('өтінім', 'заявок')], [c.pendingPosts, tt('жаңалық тексеруде', 'новостей на проверке')]]}>
        <Go to={`club:${c.id}`} primary>{tt('Клуб бетін басқару', 'Управлять страницей клуба')}</Go>
      </PanelCard>,
    );
  }
  if (p.university || p.feedback.length) {
    const open = p.feedback.reduce((a, f) => a + f.open, 0), overdue = p.feedback.reduce((a, f) => a + f.overdue, 0);
    const stats: [string | number, string][] = [];
    if (p.feedback.length) stats.push([open, tt('ашық өтініш', 'открытых обращений')], [overdue, tt('мерзімі өткен', 'просрочено')]);
    if (p.university) stats.push([p.university.published, tt('ашық сауалнама', 'открытых опросов')], [p.university.drafts, tt('жоба', 'черновиков')]);
    cards.push(
      <PanelCard key="univ" icon="cap" title={p.university?.name ?? tt('Студент дауысы', 'Голос студентов')} badge={tt('университет', 'университет')} stats={stats}>
        <Go to="voice" primary>{tt('Сауалнама мен өтініштер', 'Опросы и обращения')}</Go>
        {p.university ? <Go to="university">{tt('Университет кабинеті', 'Кабинет университета')}</Go> : null}
      </PanelCard>,
    );
  }
  if (p.employer) {
    cards.push(
      <PanelCard key="emp" icon="briefcase" title={p.employer.name} badge={p.employer.role === 'owner' ? tt('иесі', 'владелец') : tt('қызметкер', 'сотрудник')}
        stats={[[p.employer.jobs, tt('жарияланған', 'опубликовано')], [p.employer.pending, tt('тексеруде', 'на проверке')], [p.employer.newApply, tt('жаңа өтінім', 'новых откликов')]]}>
        <Go to="employer" primary>{tt('Компания кабинеті', 'Кабинет компании')}</Go>
      </PanelCard>,
    );
  }
  if (p.ambassador) cards.push(<PanelCard key="amb" icon="award" title={tt('Амбассадор кабинеті', 'Кабинет амбассадора')}><Go to="ambassador" primary>{tt('Ашу', 'Открыть')}</Go><Go to="organize">{tt('Іс-шаралар', 'Мероприятия')}</Go></PanelCard>);
  if (p.mentor) cards.push(<PanelCard key="men" icon="cap" title={tt('Менің менторлығым', 'Моё менторство')}><Go to="mentors:me" primary>{tt('Ашу', 'Открыть')}</Go></PanelCard>);
  if (!cards.length) return null;
  return (
    <section className="section">
      <SectionHead title={p.admin ? tt('Менің басқа панельдерім', 'Мои другие панели') : tt('Менің панельдерім', 'Мои панели')} />
      <div className="panel-grid">{cards}</div>
    </section>
  );
}

function AdminOverview() {
  const { tt } = useApp();
  const d = useLoad<Dashboard>('/api/admin/dashboard');
  if (!d.data) return <><ErrorLine err={d.err} />{d.err ? null : <Loading />}</>;
  const x = d.data, f = x.features;
  const pend = x.pending;
  const waiting: [number, string, string][] = ([
    [pend.queue, tt('Модерация кезегі', 'Очередь модерации'), 'admin:queue'],
    [pend.reports, tt('Шағым жасалған жазбалар', 'Записи с жалобами'), 'admin:reports'],
    ...(f.feedback ? [[pend.feedbackHeld, tt('Белгіленген өтініштер (әкімшіге)', 'Помеченные обращения (админу)'), 'voice'], [pend.feedbackOverdue, tt('Мерзімі өткен өтініштер', 'Просроченные обращения'), 'admin:voice']] : []),
    ...(f.points ? [[pend.redemptions, tt('Сыйлық өтінімдері', 'Заявки на награды'), 'admin:points']] : []),
    ...(f.b2b ? [[pend.employers, tt('Тексеруді күткен компаниялар', 'Компании ждут проверки'), 'admin:employers']] : []),
    ...(f.cafe ? [[pend.cafeRefunds, tt('Кофе: қолмен қайтару', 'Кофе: вернуть деньги вручную'), 'admin:cafe']] : []),
    ...(f.print ? [[pend.printRefunds, tt('Полиграфия: қолмен қайтару', 'Полиграфия: вернуть деньги вручную'), 'admin:print']] : []),
  ] as [number, string, string][]);
  const content: [string, number, string][] = ([
    ['adminContent', x.content.adminContent, tt('Ресми жазбалар', 'Официальные записи')],
    ['community', x.content.community, tt('Студенттер ұсынған', 'От студентов')],
    ...(f.b2b ? [['jobs', x.content.jobs, tt('Вакансиялар', 'Вакансии')]] : []),
    ...(f.events ? [['events', x.content.events, tt('Алдағы іс-шаралар', 'Будущие мероприятия')]] : []),
    ...(f.clubs ? [['clubs', x.content.clubs, tt('Клубтар', 'Клубы')]] : []),
    ...(f.market ? [['market', x.content.market, tt('Тегін заттар', 'Отдам даром')]] : []),
    ...(f.lostfound ? [['lostfound', x.content.lostfound, tt('Табылған заттар', 'Бюро находок')]] : []),
    ...(f.courseqa ? [['qa', x.content.qa, tt('Сұрақтар', 'Вопросы')]] : []),
    ...(f.roommates ? [['roommates', x.content.roommates, tt('Пәтерлес', 'Соседи')]] : []),
    ...(f.surveys ? [['surveys', x.content.surveys, tt('Ашық сауалнамалар', 'Открытые опросы')]] : []),
  ] as [string, number, string][]);
  return (
    <>
      <section className="section">
        <SectionHead title={tt('Шолу', 'Обзор')} more={<button className="link-btn" type="button" onClick={d.reload}>{tt('Жаңарту', 'Обновить')}</button>} />
        <div className="stat-grid">
          <Stat value={x.users.accounts} label={tt('Аккаунттар (келісіммен)', 'Аккаунты (с согласием)')} />
          <Stat value={x.users.new7} label={tt('Жаңа, 7 күн', 'Новые, 7 дней')} />
          <Stat value={x.active.dau} label={tt('Бүгін белсенді (DAU)', 'Активны сегодня (DAU)')} />
          <Stat value={x.active.wau} label={tt('7 күнде белсенді (WAU)', 'Активны за 7 дней (WAU)')} />
          <Stat value={x.active.mau} label={tt('30 күнде белсенді', 'Активны за 30 дней')} />
          <Stat value={x.devices.dau} label={tt('Бүгін құрылғылар (анонимді)', 'Устройства сегодня (анонимно)')} />
        </div>
        <p className="tiny muted">{x.active.since
          ? tt(`Белсенділік — кірген аккаунттың сайтқа күніне бір сұрауы; ${x.active.since} бастап саналады.`, `Активность — хотя бы один запрос вошедшего аккаунта за день; считается с ${x.active.since}.`)
          : tt('Белсенділік әлі жиналмаған: кірген аккаунттар келгенде санала бастайды.', 'Активность ещё не собрана: начнёт считаться, когда зайдут аккаунты.')}</p>
        <DayBars series={x.signups} label={tt('Тіркелу, 30 күн', 'Регистрации, 30 дней')} />
        {x.active.since ? <DayBars series={x.active.days} label={tt('Белсенді аккаунттар, 14 күн', 'Активные аккаунты, 14 дней')} /> : null}
      </section>
      <section className="section">
        <SectionHead title={tt('Шешім күтіп тұр', 'Ждёт решения')} />
        <div className="group">
          {waiting.map(([n, label, to]) => (
            <Row key={label} go={to} title={label} end={<b className={`num${n ? ' urgent' : ' muted'}`}>{n}</b>} />
          ))}
        </div>
      </section>
      {f.cafe || f.print ? (
        <section className="section">
          <SectionHead title={tt('Бүгін дүкендер', 'Магазины сегодня')} />
          <div className="stat-grid">
            {f.cafe ? <Stat value={money(x.shops.cafe.revenue)} label={tt(`Кофе: ${x.shops.cafe.done} берілді / ${x.shops.cafe.orders} тапсырыс`, `Кофе: выдано ${x.shops.cafe.done} / заказов ${x.shops.cafe.orders}`)} /> : null}
            {f.print ? <Stat value={money(x.shops.print.revenue)} label={tt(`Полиграфия: ${x.shops.print.done} берілді / ${x.shops.print.orders} тапсырыс`, `Полиграфия: выдано ${x.shops.print.done} / заказов ${x.shops.print.orders}`)} /> : null}
          </div>
          <p className="tiny muted">{tt('Сома — бүгін берілген тапсырыстар (қайтарылғандарсыз). Ақша дүкеннің өз шотына түседі.', 'Сумма — выданные сегодня заказы (без возвратов). Деньги идут на счёт самого магазина.')}</p>
        </section>
      ) : null}
      <section className="section">
        <SectionHead title={tt('Сайттағы контент', 'Контент на сайте')} />
        <div className="stat-grid">{content.map(([k, n, label]) => <Stat key={k} value={n} label={label} />)}</div>
      </section>
      <section className="section">
        <SectionHead title={tt('Басқару', 'Управление')} />
        <div className="group">
          <Row go="admin:users" icon="user" title={tt('Қолданушылар', 'Пользователи')} sub={tt('Іздеу, сүзгі, профиль, шектеу, өшіру', 'Поиск, фильтры, профиль, ограничение, удаление')} end={<b className="num">{x.users.total}</b>} />
          <Row go="admin:roles" icon="shield" title={tt('Рөлдер мен құқықтар', 'Роли и права')} sub={tt('Модераторлар және кім нені басқарады', 'Модераторы и кто чем управляет')} end={<b className="num">{x.users.moderators}</b>} />
          <Row go="admin:audit" icon="clock" title={tt('Әрекеттер журналы', 'Журнал действий')} sub={tt('Әр рөл, шектеу, экспорт және шешім', 'Каждая роль, ограничение, экспорт и решение')} />
          <Row go="admin:exports" icon="doc" title={tt('CSV экспорт', 'Экспорт CSV')} sub={tt('Excel-ге: қолданушылар, тапсырыстар, өтініштер', 'Для Excel: пользователи, заказы, обращения')} />
          {f.surveys || f.feedback || f.wellbeing ? <Row go="admin:voice" icon="chat" title={tt('Студент дауысы: барлық университет', 'Голос студентов: все вузы')} sub={tt('Сауалнамалар, өтініштер, психолог байланыстары', 'Опросы, обращения, контакты психологов')} /> : null}
          <Row go="admin:health" icon="bolt" title={tt('Денсаулық', 'Состояние системы')} sub={tt('Қателер, бот және күнделікті тапсырмалар', 'Ошибки, бот и ежедневные задачи')} />
        </div>
      </section>
      <ModerationLinks />
      <ProductMetrics features={f} byRole={x.users.byRole} />
    </>
  );
}

/** the three metrics of docs/ANALYTICS.md, referral sources, in-app feedback and the feature switches */
function ProductMetrics({ features, byRole }: { features: Record<string, boolean>; byRole: Record<string, number> }) {
  const { tt } = useApp();
  const q = useLoad<Report>('/api/admin/analytics?days=30');
  const rep = q.data;
  const amb = Object.entries(rep?.ambassadors || {}).sort((a, b) => b[1] - a[1]);
  return (
    <>
      <section className="section">
        <SectionHead title={tt('Басты 3 көрсеткіш (30 күн)', 'Главные 3 показателя (30 дней)')} />
        <ErrorLine err={q.err} />
        {!rep ? (q.err ? null : <Loading />) : (
          <>
            <div className="stat-grid">
              <Stat value={pct(rep.returned_day1, rep.eligible_day1)} label={tt(`Келесі күні қайта оралғандар (${rep.returned_day1} / ${rep.eligible_day1})`, `Вернулись на следующий день (${rep.returned_day1} / ${rep.eligible_day1})`)} />
              <Stat value={pct(rep.returned_day7, rep.eligible_day7)} label={tt(`7-күні қайта оралғандар (${rep.returned_day7} / ${rep.eligible_day7})`, `Вернулись на 7-й день (${rep.returned_day7} / ${rep.eligible_day7})`)} />
              <Stat value={pct(rep.saved_first, rep.devices)} label={tt(`Бірінші мүмкіндікті сақтағандар (${rep.saved_first} / ${rep.devices})`, `Сохранили первую возможность (${rep.saved_first} / ${rep.devices})`)} />
              <Stat value={rep.devices} label={tt('Жаңа құрылғылар', 'Новые устройства')} />
              <Stat value={rep.from_share} label={tt('Досының сілтемесімен', 'По ссылке друга')} />
            </div>
            <p className="tiny muted">{tt('Құрылғылар анонимді. «Оралғандар» тек сол күнге жетіп үлгерген құрылғылар арасынан саналады.', 'Устройства анонимны. «Вернувшиеся» считаются только среди устройств, для которых этот день уже наступил.')}</p>
            <div className="group">
              {amb.length ? amb.map(([code, n]) => <Row key={code} icon="user" title={code.replace(/^amb_/, '')} end={<b className="num">{n}</b>} chev={false} />)
                : <div className="empty">{tt('Амбассадор сілтемелерімен әлі ешкім келмеді', 'По ссылкам амбассадоров пока никто не пришёл')}</div>}
            </div>
          </>
        )}
      </section>
      {rep ? (
        <section className="section">
          <SectionHead title={tt('Кері байланыс', 'Обратная связь')} more={<span className="tiny muted num">{rep.feedback_count ? `${rep.feedback_avg.toFixed(1)} / 5 · ${rep.feedback_count}` : ''}</span>} />
          <div className="group">
            {(rep.feedback_recent || []).length ? (rep.feedback_recent || []).map((t, i) => <div key={i} className="row"><span className="row-main"><span className="row-sub">{t}</span></span></div>)
              : <div className="empty">{tt('Әзірге жауап жоқ', 'Ответов пока нет')}</div>}
          </div>
        </section>
      ) : null}
      <section className="section">
        <SectionHead title={tt('Рөлдер бойынша', 'По ролям')} />
        <div className="stat-grid">{ROLES.filter(r => r !== 'student').map(r => <Stat key={r} value={byRole[r] ?? 0} label={roleLabel(r, tt)} />)}</div>
      </section>
      <section className="section">
        <SectionHead title={tt('Ажыратқыштар', 'Переключатели функций')} />
        <div className="group">
          {Object.entries(features).sort().map(([k, on]) => (
            <Row key={k} title={k} end={<span className={`badge ${on ? 'b-ok' : 'b-line'}`}>{on ? tt('қосулы', 'включено') : tt('өшірулі', 'выключено')}</span>} chev={false} />
          ))}
        </div>
        <p className="tiny muted">{tt('Қосу үшін серверде FEATURE_<АТЫ>=true қойып, қайта іске қосу керек.', 'Чтобы включить, задайте на сервере FEATURE_<ИМЯ>=true и перезапустите.')}</p>
      </section>
    </>
  );
}

export function Admin() {
  const acc = useAccount();
  const { tt } = useApp();
  const q = useLoad<Panels>(acc.user ? '/api/console/me' : null);
  if (!acc.loaded) return <Loading />;
  if (!acc.user) return <NotFound />;
  if (!q.data) return q.err ? <ErrorLine err={q.err} /> : <Loading />;
  const p = q.data;
  if (!p.admin && panelCount(p) === 0) return <NotFound />;
  return (
    <div className="stack">
      {p.blocked ? <p className="note banner card" role="status">{tt('Аккаунтыңыз шектелген: панельдерді көре аласыз, бірақ өзгерте алмайсыз.', 'Ваш аккаунт ограничен: панели видны, но изменять ничего нельзя.')}</p> : null}
      <p className="small muted">{p.admin
        ? tt('Сіз — бас әкімшісіз: төменде бүкіл сайттың нақты көрсеткіштері мен басқару бөлімдері.', 'Вы — главный администратор: ниже реальные показатели всего сайта и разделы управления.')
        : tt('Мұнда тек өзіңіз басқаратын бөлімдер. Әр әрекетті сервер қайта тексереді.', 'Здесь только разделы, которыми вы управляете. Каждое действие сервер проверяет заново.')}</p>
      {p.admin ? <AdminOverview /> : null}
      <MyPanels p={p} />
    </div>
  );
}
