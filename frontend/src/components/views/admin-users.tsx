'use client';
/* /admin/users — the super admin's user directory (personal data: role admin only, the server checks it too):
   search, filters, pages, CSV of the current filter; /admin/users?id=N — one account: profile, roles and
   memberships, activity, reports against their content, audit trail; role change, moderator grant, block /
   unblock and deletion on request, each with a reason that goes to the audit log. */
import { useState, type ReactNode } from 'react';
import { useApp } from '@/components/app';
import { Icon, Row, SectionHead } from '@/components/ui';
import { AdminOnly, ErrorLine, errText, Loading, ReasonSheet, useLoad, useQueryParam, ymd } from '@/components/views/common';
import { RoleSheet, ROLES, Stat, type UserRow } from '@/components/views/admin';
import { roleLabel } from '@/components/views/account';
import { api, apiDownload, useAccount, type Role } from '@/lib/account';
import { AUDIT_ACTIONS, query, reasonOk, who } from '@/lib/console';

interface ListRow extends UserRow { email: string | null; lastActive: string | null; blocked: boolean; scopes: string[] }
interface ListResp { users: ListRow[]; total: number; page: number; limit: number; universities: { name: string; count: number }[] }

const EMPTY = { q: '', role: '', scope: '', blocked: false, university: '', active: '', consent: '', from: '', to: '', sort: '' };
type Filter = typeof EMPTY;

export function AdminUsers() {
  const { tt } = useApp();
  const id = useQueryParam('id');
  const open = id && /^\d+$/.test(id) ? Number(id) : 0;
  // a phone shows the list or the account; a wide screen shows both, the account as a side panel
  return (
    <AdminOnly>
      <div className={`adm-split${open ? ' has-detail' : ''}`}>
        <div className="adm-split-list"><UsersList current={open} /></div>
        {open ? <aside className="adm-split-detail" aria-label={tt('Қолданушы', 'Пользователь')}><UserDetail key={open} id={open} /></aside> : null}
      </div>
    </AdminOnly>
  );
}

function UsersList({ current }: { current: number }) {
  const { tt, lang, go, toast } = useApp();
  const [f, setF] = useState<Filter>(EMPTY);
  const [q, setQ] = useState('');
  const [page, setPage] = useState(1);
  const qs = query({ ...f, page: page > 1 ? page : '', limit: 25 });
  const r = useLoad<ListResp>('/api/admin/users' + qs);
  const set = (k: keyof Filter, v: string | boolean) => { setPage(1); setF(x => ({ ...x, [k]: v })); };
  const pages = r.data ? Math.max(1, Math.ceil(r.data.total / r.data.limit)) : 1;
  const filtered = JSON.stringify({ ...f, q: '' }) !== JSON.stringify({ ...EMPTY, q: '' });
  const sel = (k: keyof Filter, label: string, opts: [string, string][]) => (
    <div className="field">
      <label htmlFor={'uf_' + k}>{label}</label>
      <select className="input" id={'uf_' + k} value={String(f[k])} onChange={e => set(k, e.target.value)}>
        {opts.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
      </select>
    </div>
  );
  return (
    <div className="stack">
      <form className="search" role="search" onSubmit={e => { e.preventDefault(); set('q', q.trim()); }}>
        <input className="input" type="search" aria-label={tt('Қолданушыны іздеу', 'Поиск пользователя')} placeholder={tt('Аты, @username, email не id', 'Имя, @username, email или id')}
          value={q} onChange={e => setQ(e.target.value)} style={{ paddingLeft: 14 }} />
      </form>
      <details className="card" open={filtered}>
        <summary className="small"><b>{tt('Сүзгілер', 'Фильтры')}</b>{filtered ? ' · ' + tt('қосулы', 'включены') : ''}</summary>
        <div className="adm-filters">
          {sel('role', tt('Рөлі', 'Роль'), [['', tt('Барлығы', 'Все')], ...ROLES.map(x => [x, roleLabel(x, tt)] as [string, string])])}
          {sel('scope', tt('Құқық', 'Право'), [['', tt('Кез келген', 'Любое')], ['moderator', tt('Модератор', 'Модератор')]])}
          {sel('active', tt('Белсенділік', 'Активность'), [['', tt('Кез келген', 'Любая')], ['7', tt('7 күнде болған', 'Был за 7 дней')], ['30', tt('30 күнде болған', 'Был за 30 дней')], ['never', tt('Ешқашан', 'Ни разу')]])}
          {sel('consent', tt('Келісім', 'Согласие'), [['', tt('Кез келген', 'Любое')], ['yes', tt('Берген', 'Есть')], ['no', tt('Жоқ (тек бот)', 'Нет (только бот)')]])}
          {sel('university', tt('Университет', 'Университет'), [['', tt('Барлығы', 'Все')], ['-', tt('Көрсетілмеген', 'Не указан')],
            ...(r.data?.universities ?? []).map(u => [u.name, `${u.name} (${u.count})`] as [string, string]),
            ...(f.university && f.university !== '-' && !(r.data?.universities ?? []).some(u => u.name === f.university) ? [[f.university, f.university] as [string, string]] : [])])}
          {sel('sort', tt('Реті', 'Порядок'), [['', tt('Жаңасы бірінші', 'Сначала новые')], ['old', tt('Ескісі бірінші', 'Сначала старые')], ['active', tt('Белсенділігі бойынша', 'По активности')], ['name', tt('Аты бойынша', 'По имени')]])}
          <div className="field"><label htmlFor="uf_from">{tt('Тіркелген, бастап', 'Создан, с')}</label>
            <input className="input" id="uf_from" type="date" value={f.from} onChange={e => set('from', e.target.value)} /></div>
          <div className="field"><label htmlFor="uf_to">{tt('Тіркелген, дейін', 'Создан, по')}</label>
            <input className="input" id="uf_to" type="date" value={f.to} onChange={e => set('to', e.target.value)} /></div>
          <label className="check small"><input type="checkbox" checked={f.blocked} onChange={e => set('blocked', e.target.checked)} /> {tt('Тек шектелгендер', 'Только ограниченные')}</label>
        </div>
        {filtered ? <button className="link-btn" type="button" onClick={() => { setF({ ...EMPTY, q: f.q }); setPage(1); }}>{tt('Сүзгілерді тазалау', 'Сбросить фильтры')}</button> : null}
      </details>
      <div className="section-head">
        <span className="small muted num" role="status">{r.data ? tt(`Табылды: ${r.data.total}`, `Найдено: ${r.data.total}`) : ''}</span>
        <button className="link-btn" type="button" disabled={!r.data?.total} onClick={() => apiDownload('/api/admin/export/users.csv' + query(f), 'studenthub-users.csv', lang)
          .catch(e => toast(errText(e, tt('Файлды жүктеу мүмкін болмады', 'Не удалось скачать файл'))))}>CSV</button>
      </div>
      <ErrorLine err={r.err} />
      {!r.data ? (r.err ? null : <Loading />) : r.data.users.length ? (<>
        <div className="group table-wrap adm-users-table">
          <table>
            <thead><tr><th scope="col">{tt('Аты', 'Имя')}</th><th scope="col">{tt('Рөлі', 'Роль')}</th><th scope="col">{tt('Университет', 'Университет')}</th>
              <th scope="col">{tt('Соңғы белсенділік', 'Последняя активность')}</th><th scope="col">{tt('Тіркелген', 'Создан')}</th></tr></thead>
            <tbody>{r.data.users.map(u => (
              <tr key={u.id} className={current === u.id ? 'is-current' : ''}>
                <td><button className="link-btn" type="button" onClick={() => go('admin:users?id=' + u.id)}>{who(u)}</button>
                  {u.blocked ? <span className="badge b-bad">{tt('шектелген', 'ограничен')}</span> : null}
                  {u.scopes?.includes('moderator') ? <span className="badge b-sky">{tt('модератор', 'модератор')}</span> : null}
                  {u.username && u.name ? <span className="tiny muted"> @{u.username}</span> : null}</td>
                <td>{roleLabel(u.role, tt)}</td><td>{u.university || '—'}</td>
                <td className="num">{u.lastActive || '—'}</td><td className="num">{ymd(u.createdAt)}</td>
              </tr>
            ))}</tbody>
          </table>
        </div>
        <div className="group adm-users-cards">
          {r.data.users.map(u => (
            <button key={u.id} type="button" className={`adm-user${current === u.id ? ' is-current' : ''}`} onClick={() => go('admin:users?id=' + u.id)}>
              <span className="ico" aria-hidden="true"><Icon n="user" /></span>
              <span className="row-main">
                <span className="row-title">{who(u)}</span>
                <span className="row-sub">{[u.username && u.name ? '@' + u.username : '', roleLabel(u.role, tt), u.university || '',
                  u.lastActive ? tt(`соңғы рет ${u.lastActive}`, `был ${u.lastActive}`) : tt('белсенділік жоқ', 'нет активности')].filter(Boolean).join(' · ')}</span>
                {u.blocked || u.scopes?.length || !u.consentAt ? (
                  <span className="badges">
                    {u.blocked ? <span className="badge b-bad">{tt('шектелген', 'ограничен')}</span> : null}
                    {u.scopes?.includes('moderator') ? <span className="badge b-sky">{tt('модератор', 'модератор')}</span> : null}
                    {!u.consentAt ? <span className="badge b-line">{tt('келісімсіз', 'без согласия')}</span> : null}
                  </span>
                ) : null}
              </span>
              <span className="chev" aria-hidden="true"><Icon n="chev" s={18} /></span>
            </button>
          ))}
        </div>
      </>) : <div className="group"><div className="empty">{filtered || f.q ? tt('Бұл сүзгімен ешкім табылмады', 'По этому фильтру никого нет') : tt('Әзірге қолданушы жоқ', 'Пользователей пока нет')}</div></div>}
      {pages > 1 ? (
        <nav className="adm-pager" aria-label={tt('Беттер', 'Страницы')}>
          <button className="btn ghost" type="button" disabled={page <= 1} onClick={() => setPage(p => p - 1)}>{tt('Алдыңғы', 'Назад')}</button>
          <span className="small muted num">{tt(`${page}-бет / ${pages}`, `Стр. ${page} из ${pages}`)}</span>
          <button className="btn ghost" type="button" disabled={page >= pages} onClick={() => setPage(p => p + 1)}>{tt('Келесі', 'Далее')}</button>
        </nav>
      ) : null}
    </div>
  );
}

interface Detail {
  user: {
    id: number; telegramId: number | null; email: string | null; emailVerified: boolean; username: string | null; name: string | null; lang: string;
    role: Role; university: string | null; faculty: string | null; course: number | null; city: string | null; isAdult: boolean | null;
    consentAt: string | null; createdAt: string; lastSignIn: string | null; referredBy: string | null; botInactive: boolean;
  };
  scopes: { scope: string; reason: string; createdAt: string; grantedBy: { id: number | null; name: string | null } }[];
  block: { reason: string; createdAt: string; by: { id: number | null; name: string | null } } | null;
  memberships: {
    cafes: { id: number; nameKk: string; nameRu: string; role: string }[]; printShops: { id: number; nameKk: string; nameRu: string; role: string }[];
    clubs: { id: number; nameKk: string; nameRu: string | null }[]; university: string | null;
    feedback: { id: number; university: string; nameKk: string; nameRu: string }[];
    employer: { id: number; name: string; status: string; role: string } | null;
    ambassador: { university: string; status: string; termEnd: string } | null;
    mentor: { university: string; status: string } | null;
  };
  activity: Record<string, number | string | null>;
  reportsAgainst: { kind: string; id: string; reports: number; dismissed: number; firstAt: string; lastAt: string }[];
  audit: AuditEntry[];
}
export interface AuditEntry {
  id: number; action: string; reason: string | null; meta: Record<string, unknown>; createdAt: string;
  actor: { id: number; name: string | null; username?: string | null } | null; target: { id: number; name: string | null; username?: string | null } | null;
}

export function actionLabel(a: string, tt: (kk: string, ru: string) => string): string {
  const l = AUDIT_ACTIONS[a];
  return l ? tt(l[0], l[1]) : a;
}

export function AuditList({ entries }: { entries: AuditEntry[] }) {
  const { tt, go } = useApp();
  if (!entries.length) return <div className="group"><div className="empty">{tt('Журналда жазба жоқ', 'В журнале нет записей')}</div></div>;
  return (
    <ol className="group adm-log">
      {entries.map(e => (
        <li key={e.id}>
          <span><b>{actionLabel(e.action, tt)}</b> <span className="tiny muted num">{ymd(e.createdAt)} {e.createdAt.slice(11, 16)}</span></span>
          <span className="small">
            {e.actor ? <button className="link-btn" type="button" onClick={() => go('admin:users?id=' + e.actor!.id)}>{who(e.actor)}</button> : <span className="muted">{tt('жүйе', 'система')}</span>}
            {e.target ? <> → <button className="link-btn" type="button" onClick={() => go('admin:users?id=' + e.target!.id)}>{who(e.target)}</button></> : null}
          </span>
          {e.reason ? <span className="small">«{e.reason}»</span> : null}
          {Object.keys(e.meta || {}).length ? <code className="meta">{JSON.stringify(e.meta)}</code> : null}
        </li>
      ))}
    </ol>
  );
}

function DeleteUserSheet({ id, name, onDone }: { id: number; name: string; onDone: () => void }) {
  const { tt, lang, closeSheet, toast } = useApp();
  const [reason, setReason] = useState('');
  const [confirm, setConfirm] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const ok = reasonOk(reason) && confirm === 'DELETE';
  return (
    <form className="form" onSubmit={async e => {
      e.preventDefault();
      if (!ok || busy) return;
      setBusy(true); setErr('');
      try {
        await api(`/api/admin/users/${id}/delete`, { method: 'POST', body: { reason: reason.trim(), confirm }, lang });
        closeSheet(); toast(tt('Аккаунт өшірілді', 'Аккаунт удалён')); onDone();
      } catch (x) { setErr(errText(x)); } finally { setBusy(false); }
    }}>
      <h2>{tt('Аккаунтты өшіру', 'Удалить аккаунт')}: {name}</h2>
      <p className="small">{tt('Студенттің өзі «Аккаунтты өшіру» басқандағыдай: барлық деректері мен файлдары қайтарусыз өшеді. Тек студент өзі сұраса ғана қолданыңыз.',
        'Как если бы студент сам нажал «Удалить аккаунт»: все данные и файлы удаляются безвозвратно. Только по просьбе самого студента.')}</p>
      <div className="field"><label htmlFor="duReason">{tt('Сұрау қайдан келді (міндетті)', 'Откуда пришла просьба (обязательно)')}</label>
        <textarea className="input" id="duReason" maxLength={500} value={reason} onChange={e => setReason(e.target.value)} /></div>
      <div className="field"><label htmlFor="duConfirm">{tt('Растау үшін DELETE деп жазыңыз', 'Для подтверждения введите DELETE')}</label>
        <input className="input" id="duConfirm" autoComplete="off" value={confirm} onChange={e => setConfirm(e.target.value)} /></div>
      <ErrorLine err={err} />
      <div className="btn-row">
        <button className="btn ghost" type="button" onClick={closeSheet}>{tt('Бас тарту', 'Отмена')}</button>
        <button className="btn danger" type="submit" disabled={!ok || busy}>{tt('Өшіру', 'Удалить')}</button>
      </div>
    </form>
  );
}

function Kv({ rows }: { rows: [string, ReactNode][] }) {
  return <dl className="kv">{rows.filter(([, v]) => v !== null && v !== '' && v !== undefined).map(([k, v]) => <div key={k} className="kv-row"><dt>{k}</dt><dd>{v}</dd></div>)}</dl>;
}

function UserDetail({ id }: { id: number }) {
  const { tt, lang, go, openSheet, closeSheet, toast } = useApp();
  const me = useAccount().user;
  const r = useLoad<Detail>(`/api/admin/users/${id}`);
  if (!r.data) return <><ErrorLine err={r.err} />{r.err ? <button className="btn ghost" type="button" onClick={() => go('admin:users')}>{tt('Тізімге оралу', 'К списку')}</button> : <Loading />}</>;
  const d = r.data, u = d.user, m = d.memberships, a = d.activity;
  const self = me?.id === u.id;
  const moderator = d.scopes.some(s => s.scope === 'moderator');
  const nm = (o: { nameKk: string; nameRu: string | null }) => (lang !== 'kk' && o.nameRu ? o.nameRu : o.nameKk);
  const post = (path: string, done: string) => async (reason: string) => {
    await api(path, { method: 'POST', body: { userId: u.id, scope: 'moderator', reason }, lang });
    closeSheet(); toast(done); r.reload();
  };
  const memberRows: [string, ReactNode][] = [
    ...m.cafes.map(c => [tt('Кофехана', 'Кофейня'), `${nm(c)} · ${c.role}`] as [string, ReactNode]),
    ...m.printShops.map(c => [tt('Полиграфия', 'Полиграфия'), `${nm(c)} · ${c.role}`] as [string, ReactNode]),
    ...m.clubs.map(c => [tt('Клуб әкімшісі', 'Админ клуба'), nm(c)] as [string, ReactNode]),
    ...(m.university ? [[tt('Университет қызметкері', 'Сотрудник университета'), m.university] as [string, ReactNode]] : []),
    ...m.feedback.map(f => [tt('Өтініш алушы', 'Получатель обращений'), `${f.university} · ${lang !== 'kk' ? f.nameRu : f.nameKk}`] as [string, ReactNode]),
    ...(m.employer ? [[tt('Компания', 'Компания'), `${m.employer.name} · ${m.employer.role} · ${m.employer.status}`] as [string, ReactNode]] : []),
    ...(m.ambassador ? [[tt('Амбассадор', 'Амбассадор'), `${m.ambassador.university} · ${m.ambassador.status} · ${tt('мерзімі', 'до')} ${m.ambassador.termEnd}`] as [string, ReactNode]] : []),
    ...(m.mentor ? [[tt('Ментор', 'Ментор'), `${m.mentor.university} · ${m.mentor.status}`] as [string, ReactNode]] : []),
  ];
  const stats: [string, string][] = [
    ['activeDays30', tt('Белсенді күн (30)', 'Активных дней (30)')], ['cafeOrders', tt('Кофе тапсырыстары', 'Заказы кофе')],
    ['printJobs', tt('Басып шығару', 'Печать')], ['submissions', tt('Ұсыныстар', 'Предложения')], ['reportsFiled', tt('Жіберген шағымдары', 'Отправленные жалобы')],
    ['marketItems', tt('Тегін заттар', 'Отдам даром')], ['lostfoundPosts', tt('Табылған заттар', 'Бюро находок')],
    ['qaQuestions', tt('Сұрақтар', 'Вопросы')], ['qaAnswers', tt('Жауаптар', 'Ответы')], ['clubMemberships', tt('Клубтар', 'Клубы')], ['eventRegs', tt('Іс-шараға тіркелу', 'Регистрации')],
  ];
  return (
    <div className="stack">
      <button className="link-btn adm-back" type="button" onClick={() => go('admin:users')} style={{ alignSelf: 'flex-start' }}>← {tt('Барлық қолданушылар', 'Все пользователи')}</button>
      <article className={`card${d.block ? ' adm-danger' : ''}`}>
        <h2 style={{ margin: 0 }}>{who(u)}</h2>
        <div className="badges">
          <span className="badge b-sky">{roleLabel(u.role, tt)}</span>
          {moderator ? <span className="badge b-sky">{tt('модератор', 'модератор')}</span> : null}
          {d.block ? <span className="badge b-bad">{tt('шектелген', 'ограничен')}</span> : null}
          {!u.consentAt ? <span className="badge b-line">{tt('келісімсіз', 'без согласия')}</span> : null}
          {self ? <span className="badge b-line">{tt('бұл сіз', 'это вы')}</span> : null}
        </div>
        {d.block ? <p className="small urgent">{tt('Шектеу себебі', 'Причина ограничения')}: «{d.block.reason}» · {who(d.block.by)} · {ymd(d.block.createdAt)}</p> : null}
        <Kv rows={[
          ['id', <span key="id" className="num">{u.id}</span>],
          ['Telegram', u.telegramId ? <span key="tg" className="num">{u.telegramId}{u.username ? ' · @' + u.username : ''}{u.botInactive ? ' · ' + tt('ботты бұғаттаған', 'бот заблокирован') : ''}</span> : '—'],
          ['Email', u.email ? `${u.email}${u.emailVerified ? '' : ' (' + tt('расталмаған', 'не подтверждён') + ')'}` : '—'],
          [tt('Университет', 'Университет'), [u.university, u.faculty, u.course ? tt(`${u.course}-курс`, `${u.course} курс`) : '', u.city].filter(Boolean).join(' · ') || '—'],
          [tt('Тіркелген', 'Создан'), ymd(u.createdAt)],
          [tt('Келісім', 'Согласие'), u.consentAt ? ymd(u.consentAt) : '—'],
          [tt('Соңғы кіру', 'Последний вход'), u.lastSignIn ? ymd(u.lastSignIn) : '—'],
          [tt('Соңғы белсенділік', 'Последняя активность'), String(a.lastActive ?? '—')],
          [tt('Келген сілтеме', 'Пришёл по ссылке'), u.referredBy ?? ''],
        ]} />
      </article>
      {self ? <p className="small muted">{tt('Өз аккаунтыңызға рөл, шектеу және өшіру қолданылмайды.', 'К своему аккаунту роль, ограничение и удаление не применяются.')}</p> : (
        <div className="panel-actions">
          <button className="btn ghost" type="button" onClick={() => openSheet(<RoleSheet u={{ ...u, role: u.role }} onDone={r.reload} />)}>{tt('Рөлді өзгерту', 'Изменить роль')}</button>
          {moderator
            ? <button className="btn ghost" type="button" onClick={() => openSheet(<ReasonSheet title={tt('Модератор құқығын алу', 'Снять права модератора')} label={tt('Себебі (журналға жазылады)', 'Причина (пишется в журнал)')}
              action={tt('Алу', 'Снять')} danger onSubmit={post('/api/admin/grants/revoke', tt('Құқық алынды', 'Права сняты'))} />)}>{tt('Модераторлықты алу', 'Снять модератора')}</button>
            : <button className="btn ghost" type="button" onClick={() => openSheet(<ReasonSheet title={tt('Модератор тағайындау', 'Назначить модератором')} label={tt('Себебі (журналға жазылады)', 'Причина (пишется в журнал)')}
              action={tt('Тағайындау', 'Назначить')} extra={<p className="small muted">{tt('Модератор кезекті, шағымдарды, тегін заттарды, ағын мен контентті қарайды. Қолданушылар, экспорт, ақша және рөл беретін өтінімдер оған жабық.',
                'Модератор разбирает очередь, жалобы, «Отдам даром», ленту и контент. Пользователи, экспорт, деньги и заявки, выдающие роли, ему закрыты.')}</p>}
              onSubmit={post('/api/admin/grants', tt('Модератор тағайындалды', 'Назначен модератор'))} />)}>{tt('Модератор ету', 'Сделать модератором')}</button>}
          {u.role === 'admin' ? null : d.block
            ? <button className="btn ghost" type="button" onClick={() => openSheet(<ReasonSheet title={tt('Шектеуді алу', 'Снять ограничение')} label={tt('Себебі (журналға жазылады)', 'Причина (пишется в журнал)')}
              action={tt('Шектеуді алу', 'Снять ограничение')} onSubmit={async reason => { await api(`/api/admin/users/${u.id}/unblock`, { method: 'POST', body: { reason }, lang }); closeSheet(); toast(tt('Шектеу алынды', 'Ограничение снято')); r.reload(); }} />)}>
              {tt('Шектеуді алу', 'Снять ограничение')}</button>
            : <button className="btn danger" type="button" onClick={() => openSheet(<ReasonSheet title={tt('Аккаунтты шектеу', 'Ограничить аккаунт')} label={tt('Себебі (міндетті, журналға жазылады)', 'Причина (обязательно, пишется в журнал)')}
              action={tt('Шектеу', 'Ограничить')} danger extra={<p className="small muted">{tt('Студент сайтты оқи алады, кіре-шыға алады және аккаунтын өшіре алады, бірақ жаза, жариялай, тапсырыс бере алмайды.',
                'Студент сможет читать, входить и выходить и удалить аккаунт, но не сможет писать, публиковать и заказывать.')}</p>}
              onSubmit={async reason => { await api(`/api/admin/users/${u.id}/block`, { method: 'POST', body: { reason }, lang }); closeSheet(); toast(tt('Аккаунт шектелді', 'Аккаунт ограничен')); r.reload(); }} />)}>
              {tt('Шектеу', 'Ограничить')}</button>}
          {u.role === 'admin' ? null : <button className="btn ghost" type="button" onClick={() => openSheet(<DeleteUserSheet id={u.id} name={who(u)} onDone={() => go('admin:users')} />)}>{tt('Сұрау бойынша өшіру', 'Удалить по просьбе')}</button>}
        </div>
      )}
      <section className="section">
        <SectionHead title={tt('Рөлдер мен мүшеліктер', 'Роли и членство')} />
        {memberRows.length || d.scopes.length ? (
          <div className="card">
            <Kv rows={[...d.scopes.map(s => [tt('Модератор', 'Модератор'), `${ymd(s.createdAt)} · ${who(s.grantedBy as { id: number; name: string | null })} · «${s.reason}»`] as [string, ReactNode]), ...memberRows]} />
          </div>
        ) : <div className="group"><div className="empty">{tt('Қосымша рөл не мүшелік жоқ', 'Дополнительных ролей и членства нет')}</div></div>}
      </section>
      <section className="section">
        <SectionHead title={tt('Белсенділік', 'Активность')} />
        <div className="stat-grid">{stats.map(([k, l]) => <Stat key={k} value={Number(a[k] ?? 0)} label={l} />)}</div>
      </section>
      <section className="section">
        <SectionHead title={tt('Контентіне шағымдар', 'Жалобы на его контент')} />
        {d.reportsAgainst.length ? (
          <div className="group">{d.reportsAgainst.map(x => (
            <Row key={x.kind + x.id} icon="bell" title={`${x.kind} #${x.id}`} chev={false}
              sub={tt(`${x.reports} шағым · ${x.dismissed} негізсіз · соңғысы ${ymd(x.lastAt)}`, `${x.reports} жалоб · ${x.dismissed} необоснованных · последняя ${ymd(x.lastAt)}`)} />
          ))}</div>
        ) : <div className="group"><div className="empty">{tt('Шағым жоқ', 'Жалоб нет')}</div></div>}
      </section>
      <section className="section">
        <SectionHead title={tt('Журнал', 'Журнал')} more={<button className="link-btn" type="button" onClick={() => go(`admin:audit?target=${u.id}`)}>{tt('Толық журнал', 'Весь журнал')}</button>} />
        <AuditList entries={d.audit} />
      </section>
    </div>
  );
}
