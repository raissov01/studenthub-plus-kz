'use client';
/* /admin/users — the super admin's user directory (personal data: role admin only, the server checks it too):
   search, filters, pages, CSV of the current filter; /admin/users?id=N — one account: profile, roles and
   memberships, activity, reports against their content, audit trail; role change, moderator grant, block /
   unblock and deletion on request, each with a reason that goes to the audit log. */
import { useState, type ReactNode } from 'react';
import { useApp } from '@/components/app';
import { Row, SectionHead } from '@/components/ui';
import { AdminOnly, ErrorLine, errText, Loading, ReasonSheet, useLoad, useQueryParam, ymd } from '@/components/views/common';
import { ROLES, Stat, type UserRow } from '@/components/views/admin';
import { AccessBadges, CopyLink, GiveRoleSheet, InviteSheet, PendingInvites, type InviteRow } from '@/components/views/admin-access';
import { roleLabel } from '@/components/views/account';
import { api, apiDownload, useAccount, type Role } from '@/lib/account';
import { AUDIT_ACTIONS, query, reasonOk, who } from '@/lib/console';
import { money } from '@/lib/cafe';
import { parseQuery, SavedViews } from '@/components/views/admin-views';

interface ListRow extends UserRow { email: string | null; lastActive: string | null; blocked: boolean; scopes: string[]; places?: string[] }
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
  const { tt, lang, go, toast, openSheet } = useApp();
  const inv = useLoad<{ invites: InviteRow[] }>('/api/admin/invites');
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
  const give = (id: number) => openSheet(<GiveRoleSheet userId={id} onDone={r.reload} />);
  const asEmail = /^\S+@\S+\.\S+$/.test(f.q);
  const status = (u: ListRow) => u.blocked ? <span className="badge b-bad">{tt('Шектелген', 'Ограничен')}</span>
    : !u.consentAt ? <span className="badge b-line">{tt('Тек бот', 'Только бот')}</span> : <span className="badge b-ok">{tt('Белсенді', 'Активен')}</span>;
  const last = (u: ListRow) => u.lastActive || tt('кірмеген', 'не входил');
  return (
    <div className="stack">
      <p className="small muted">{tt('Адамды email-і не аты бойынша тауып, «Рөл беру» басыңыз.', 'Найдите человека по email или имени и нажмите «Выдать роль».')}</p>
      <form className="search adm-search" role="search" onSubmit={e => { e.preventDefault(); set('q', q.trim()); }}>
        <input className="input" type="search" aria-label={tt('Email не аты бойынша іздеу', 'Поиск по email или имени')} placeholder={tt('Email не аты', 'Email или имя')}
          value={q} onChange={e => setQ(e.target.value)} style={{ paddingLeft: 14 }} />
        <button className="btn" type="submit">{tt('Іздеу', 'Найти')}</button>
      </form>
      <PendingInvites list={inv.data?.invites ?? []} onChange={inv.reload} />
      <SavedViews screen="users" query={query(f)} onApply={v => { const n = parseQuery(v, EMPTY); setF(n); setQ(n.q); setPage(1); }} />
      <details className="card" open={filtered}>
        <summary className="small"><b>{tt('Қосымша сүзгілер', 'Дополнительные фильтры')}</b>{filtered ? ' · ' + tt('қосулы', 'включены') : ''}</summary>
        <div className="adm-filters">
          {sel('role', tt('Рөлі', 'Роль'), [['', tt('Барлығы', 'Все')], ...ROLES.map(x => [x, roleLabel(x, tt)] as [string, string])])}
          {sel('scope', tt('Модератор', 'Модератор'), [['', tt('Маңызды емес', 'Неважно')], ['moderator', tt('Тек модераторлар', 'Только модераторы')]])}
          {sel('active', tt('Соңғы кірген', 'Последний вход'), [['', tt('Кез келген', 'Любой')], ['7', tt('7 күн ішінде', 'За 7 дней')], ['30', tt('30 күн ішінде', 'За 30 дней')], ['never', tt('Ешқашан', 'Ни разу')]])}
          {sel('consent', tt('Тіркелу', 'Регистрация'), [['', tt('Кез келген', 'Любая')], ['yes', tt('Сайтта тіркелген', 'Зарегистрирован на сайте')], ['no', tt('Тек ботта', 'Только в боте')]])}
          {sel('university', tt('Университет', 'Университет'), [['', tt('Барлығы', 'Все')], ['-', tt('Көрсетілмеген', 'Не указан')],
            ...(r.data?.universities ?? []).map(u => [u.name, `${u.name} (${u.count})`] as [string, string]),
            ...(f.university && f.university !== '-' && !(r.data?.universities ?? []).some(u => u.name === f.university) ? [[f.university, f.university] as [string, string]] : [])])}
          {sel('sort', tt('Реті', 'Порядок'), [['', tt('Жаңасы бірінші', 'Сначала новые')], ['old', tt('Ескісі бірінші', 'Сначала старые')], ['active', tt('Жақында кіргендер', 'Недавно входившие')], ['name', tt('Аты бойынша', 'По имени')]])}
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
          .catch(e => toast(errText(e, tt('Файлды жүктеу мүмкін болмады', 'Не удалось скачать файл'))))}>{tt('Excel-ге жүктеу', 'Скачать для Excel')}</button>
      </div>
      <ErrorLine err={r.err} />
      {!r.data ? (r.err ? null : <Loading />) : r.data.users.length ? (<>
        <div className="group table-wrap adm-users-table">
          <table>
            <thead><tr><th scope="col">{tt('Аты', 'Имя')}</th><th scope="col">Email</th><th scope="col">{tt('Рөлі', 'Роль')}</th>
              <th scope="col">{tt('Соңғы кірген', 'Последний вход')}</th><th scope="col">{tt('Күйі', 'Статус')}</th><th scope="col"><span className="sr-only">{tt('Әрекет', 'Действие')}</span></th></tr></thead>
            <tbody>{r.data.users.map(u => (
              <tr key={u.id} className={current === u.id ? 'is-current' : ''}>
                <td><button className="link-btn" type="button" onClick={() => go('admin:users?id=' + u.id)}>{who(u)}</button>
                  {u.username ? <span className="tiny muted"> @{u.username}</span> : null}</td>
                <td className="adm-email">{u.email || <span className="muted">—</span>}</td>
                <td><span className="badges"><AccessBadges role={u.role} scopes={u.scopes} places={u.places} /></span></td>
                <td className="num">{last(u)}</td><td>{status(u)}</td>
                <td><button className="btn secondary adm-give" type="button" onClick={() => give(u.id)}>{tt('Рөл беру', 'Выдать роль')}</button></td>
              </tr>
            ))}</tbody>
          </table>
        </div>
        <div className="group adm-users-cards">
          {r.data.users.map(u => (
            <div key={u.id} className={`adm-user${current === u.id ? ' is-current' : ''}`}>
              <span className="row-main">
                <button className="link-btn row-title" type="button" onClick={() => go('admin:users?id=' + u.id)}>{who(u)}</button>
                <span className="row-sub adm-email">{[u.email, u.username ? '@' + u.username : ''].filter(Boolean).join(' · ') || tt('email жоқ', 'нет email')}</span>
                <span className="badges"><AccessBadges role={u.role} scopes={u.scopes} places={u.places} />{u.blocked || !u.consentAt ? status(u) : null}</span>
                <span className="tiny muted">{tt('Соңғы кірген', 'Последний вход')}: {last(u)}</span>
              </span>
              <button className="btn secondary adm-give" type="button" onClick={() => give(u.id)}>{tt('Рөл беру', 'Выдать роль')}</button>
            </div>
          ))}
        </div>
      </>) : asEmail ? (
        <section className="card stack" data-testid="invite-card">
          <b>{tt(`${f.q} әлі тіркелмеген`, `${f.q} ещё не зарегистрирован`)}</b>
          <p className="small">{tt('Адамға сайтқа кіру сілтемесін жіберіңіз. Рөлді қазір беріп қоюға болады: ол осы email-мен кіргенде өзі қосылады.',
            'Отправьте человеку ссылку для входа. Роль можно назначить уже сейчас: она добавится, когда он войдёт с этим email.')}</p>
          <CopyLink />
          <button className="btn" type="button" onClick={() => openSheet(<InviteSheet email={f.q} onDone={inv.reload} />)}>{tt('Рөлді алдын ала беру', 'Назначить роль заранее')}</button>
        </section>
      ) : <div className="group"><div className="empty">{filtered || f.q ? tt('Бұл сүзгімен ешкім табылмады', 'По этому фильтру никого нет') : tt('Әзірге қолданушы жоқ', 'Пользователей пока нет')}</div></div>}
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
  if (!entries.length) return <div className="group"><div className="empty">{tt('Тарихта жазба жоқ', 'В истории нет записей')}</div></div>;
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

const TABS: [Tab, [string, string]][] = [['profile', ['Профиль', 'Профиль']], ['activity', ['Белсенділік', 'Активность']], ['orders', ['Тапсырыстар', 'Заказы']],
  ['reports', ['Шағымдар', 'Жалобы']], ['notes', ['Ішкі жазбалар', 'Заметки']], ['history', ['Тарих', 'История']]];
type Tab = 'profile' | 'activity' | 'orders' | 'reports' | 'notes' | 'history';

interface OrderRow { id: number; shopId: number; shopKk: string; shopRu: string; total: number; paid: number | null; status: string; method: string; payment: string; refundManual: boolean; createdAt: string }

/** the person's last café orders and print jobs (admin only) */
function UserOrders({ id }: { id: number }) {
  const { tt, lang } = useApp();
  const r = useLoad<{ cafe: OrderRow[]; print: OrderRow[] }>(`/api/admin/users/${id}/orders`);
  if (!r.data) return <><ErrorLine err={r.err} />{r.err ? null : <Loading />}</>;
  const all = [...r.data.cafe.map(o => ({ ...o, kind: 'cafe' as const })), ...r.data.print.map(o => ({ ...o, kind: 'print' as const }))]
    .sort((x, y) => y.createdAt.localeCompare(x.createdAt));
  if (!all.length) return <div className="group"><div className="empty">{tt('Тапсырыс болмаған', 'Заказов не было')}</div></div>;
  return (
    <div className="group" data-testid="user-orders">
      {all.map(o => (
        <Row key={o.kind + o.id} icon={o.kind === 'cafe' ? 'cup' : 'file'} chev={false}
          title={`${lang !== 'kk' ? o.shopRu : o.shopKk} · ${money(o.paid ?? o.total)}`}
          sub={[ymd(o.createdAt), orderStatus(o.status, tt), payLabel(o.payment, o.method, tt), o.refundManual ? tt('ақшаны қолмен қайтару керек', 'нужно вернуть деньги вручную') : ''].filter(Boolean).join(' · ')} />
      ))}
    </div>
  );
}

/** the team's internal notes about the person: never shown to them */
function UserNotes({ id }: { id: number }) {
  const { tt, lang, toast } = useApp();
  const r = useLoad<{ notes: { id: number; body: string; createdAt: string; author: { id: number; name: string | null; username: string | null } | null }[] }>(`/api/admin/users/${id}/notes`);
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const add = async () => {
    if (!text.trim() || busy) return;
    setBusy(true);
    try { await api(`/api/admin/users/${id}/notes`, { method: 'POST', body: { body: text.trim() }, lang }); setText(''); toast(tt('Жазба сақталды', 'Заметка сохранена')); r.reload(); }
    catch (e) { toast(errText(e)); } finally { setBusy(false); }
  };
  return (
    <div className="stack">
      <p className="small muted">{tt('Тек әкімшілер көреді. Студентке ешқашан көрсетілмейді.', 'Видят только администраторы. Студенту никогда не показывается.')}</p>
      <form className="form" onSubmit={e => { e.preventDefault(); void add(); }}>
        <div className="field"><label htmlFor="noteText">{tt('Жаңа жазба', 'Новая заметка')}</label>
          <textarea className="input" id="noteText" maxLength={2000} value={text} onChange={e => setText(e.target.value)}
            placeholder={tt('Мысалы: «Телефонмен хабарласты, аккаунтын өшіруді сұрады»', 'Например: «Звонил, просил удалить аккаунт»')} /></div>
        <button className="btn" type="submit" disabled={!text.trim() || busy}>{tt('Сақтау', 'Сохранить')}</button>
      </form>
      <ErrorLine err={r.err} />
      {!r.data ? (r.err ? null : <Loading />) : r.data.notes.length ? (
        <ol className="group adm-log" data-testid="user-notes">
          {r.data.notes.map(n => (
            <li key={n.id}>
              <span className="small muted">{who(n.author)} · <span className="num">{ymd(n.createdAt)} {n.createdAt.slice(11, 16)}</span></span>
              <span style={{ whiteSpace: 'pre-wrap' }}>{n.body}</span>
              <button className="link-btn" type="button" style={{ alignSelf: 'flex-start' }} onClick={async () => {
                try { await api(`/api/admin/users/${id}/notes/${n.id}`, { method: 'DELETE', lang }); r.reload(); } catch (e) { toast(errText(e)); }
              }}>{tt('Өшіру', 'Удалить')}</button>
            </li>
          ))}
        </ol>
      ) : <div className="group"><div className="empty">{tt('Әзірге жазба жоқ', 'Заметок пока нет')}</div></div>}
    </div>
  );
}

export function orderStatus(s: string, tt: (kk: string, ru: string) => string): string {
  return ({ new: tt('жаңа', 'новый'), accepted: tt('қабылданды', 'принят'), printing: tt('басылуда', 'печатается'), ready: tt('дайын', 'готов'),
    picked_up: tt('берілді', 'выдан'), cancelled: tt('тоқтатылды', 'отменён'), no_show: tt('алып кетпеді', 'не забрали') } as Record<string, string>)[s] ?? s;
}
export function payLabel(p: string, method: string, tt: (kk: string, ru: string) => string): string {
  if (method === 'pickup' && p === 'unpaid') return tt('кассада төлейді', 'оплата на кассе');
  return ({ unpaid: tt('төленбеген', 'не оплачен'), pending: tt('төлем күтілуде', 'ждёт оплаты'), paid: tt('Kaspi арқылы төленді', 'оплачен через Kaspi'),
    refunded: tt('ақша қайтарылды', 'деньги возвращены'), failed: tt('төлем өтпеді', 'оплата не прошла') } as Record<string, string>)[p] ?? p;
}

function UserDetail({ id }: { id: number }) {
  const { tt, lang, go, openSheet, closeSheet, toast } = useApp();
  const [tab, setTab] = useState<Tab>('profile');
  const me = useAccount().user;
  const r = useLoad<Detail>(`/api/admin/users/${id}`);
  if (!r.data) return <><ErrorLine err={r.err} />{r.err ? <button className="btn ghost" type="button" onClick={() => go('admin:users')}>{tt('Тізімге оралу', 'К списку')}</button> : <Loading />}</>;
  const d = r.data, u = d.user, m = d.memberships, a = d.activity;
  const self = me?.id === u.id;
  const moderator = d.scopes.some(s => s.scope === 'moderator');
  const nm = (o: { nameKk: string; nameRu: string | null }) => (lang !== 'kk' && o.nameRu ? o.nameRu : o.nameKk);
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
        <p className="small adm-email">{u.email || tt('Email көрсетілмеген', 'Email не указан')}{u.username ? ' · @' + u.username : ''}</p>
        <div className="badges">
          <AccessBadges role={u.role} scopes={moderator ? ['moderator'] : []} places={[...m.cafes.map(c => 'cafe_' + c.role), ...m.printShops.map(c => 'print_' + c.role), ...(m.clubs.length ? ['club_admin'] : [])]} />
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
          <button className="btn" type="button" onClick={() => openSheet(<GiveRoleSheet userId={u.id} onDone={r.reload} />)}>{tt('Рөл беру', 'Выдать роль')}</button>
          {u.role === 'admin' ? null : d.block
            ? <button className="btn ghost" type="button" onClick={() => openSheet(<ReasonSheet title={tt('Шектеуді алу', 'Снять ограничение')} label={tt('Себебі (тарихта сақталады)', 'Причина (сохранится в истории)')}
              action={tt('Шектеуді алу', 'Снять ограничение')} onSubmit={async reason => { await api(`/api/admin/users/${u.id}/unblock`, { method: 'POST', body: { reason }, lang }); closeSheet(); toast(tt('Шектеу алынды', 'Ограничение снято')); r.reload(); }} />)}>
              {tt('Шектеуді алу', 'Снять ограничение')}</button>
            : <button className="btn danger" type="button" onClick={() => openSheet(<ReasonSheet title={tt('Аккаунтты шектеу', 'Ограничить аккаунт')} label={tt('Себебі (міндетті, тарихта сақталады)', 'Причина (обязательно, сохранится в истории)')}
              action={tt('Шектеу', 'Ограничить')} danger extra={<p className="small muted">{tt('Студент сайтты оқи алады, кіре-шыға алады және аккаунтын өшіре алады, бірақ жаза, жариялай, тапсырыс бере алмайды.',
                'Студент сможет читать, входить и выходить и удалить аккаунт, но не сможет писать, публиковать и заказывать.')}</p>}
              onSubmit={async reason => { await api(`/api/admin/users/${u.id}/block`, { method: 'POST', body: { reason }, lang }); closeSheet(); toast(tt('Аккаунт шектелді', 'Аккаунт ограничен')); r.reload(); }} />)}>
              {tt('Шектеу', 'Ограничить')}</button>}
          {u.role === 'admin' ? null : <button className="btn ghost" type="button" onClick={() => openSheet(<DeleteUserSheet id={u.id} name={who(u)} onDone={() => go('admin:users')} />)}>{tt('Сұрау бойынша өшіру', 'Удалить по просьбе')}</button>}
        </div>
      )}
      <div className="adm-tabs" role="tablist" aria-label={tt('Қолданушы бөлімдері', 'Разделы пользователя')}>
        {TABS.map(([k, label]) => (
          <button key={k} type="button" role="tab" id={'ut-' + k} aria-selected={tab === k} aria-controls="ut-panel" onClick={() => setTab(k)}>{tt(...label)}</button>
        ))}
      </div>
      <div id="ut-panel" role="tabpanel" aria-labelledby={'ut-' + tab} className="stack">
      {tab === 'profile' ? <>
      <section className="section">
        <SectionHead title={tt('Құқықтары', 'Права')} />
        {memberRows.length || d.scopes.length ? (
          <div className="card">
            <Kv rows={[...d.scopes.map(s => [tt('Модератор', 'Модератор'), `${ymd(s.createdAt)} · ${who(s.grantedBy as { id: number; name: string | null })} · «${s.reason}»`] as [string, ReactNode]), ...memberRows]} />
          </div>
        ) : <div className="group"><div className="empty">{tt('Қосымша рөл не мүшелік жоқ', 'Дополнительных ролей и членства нет')}</div></div>}
      </section>
      </> : null}
      {tab === 'activity' ? <>
      <section className="section">
        <SectionHead title={tt('Белсенділік', 'Активность')} />
        <div className="stat-grid">{stats.map(([k, l]) => <Stat key={k} value={Number(a[k] ?? 0)} label={l} />)}</div>
      </section>
      </> : null}
      {tab === 'orders' ? <UserOrders id={u.id} /> : null}
      {tab === 'reports' ? <>
      <section className="section">
        <SectionHead title={tt('Контентіне шағымдар', 'Жалобы на его контент')} />
        {d.reportsAgainst.length ? (
          <div className="group">{d.reportsAgainst.map(x => (
            <Row key={x.kind + x.id} icon="bell" title={`${x.kind} #${x.id}`} chev={false}
              sub={tt(`${x.reports} шағым · ${x.dismissed} негізсіз · соңғысы ${ymd(x.lastAt)}`, `${x.reports} жалоб · ${x.dismissed} необоснованных · последняя ${ymd(x.lastAt)}`)} />
          ))}</div>
        ) : <div className="group"><div className="empty">{tt('Шағым жоқ', 'Жалоб нет')}</div></div>}
      </section>
      </> : null}
      {tab === 'notes' ? <UserNotes id={u.id} /> : null}
      {tab === 'history' ? <>
      <section className="section">
        <SectionHead title={tt('Өзгерістер тарихы', 'История изменений')} more={<button className="link-btn" type="button" onClick={() => go(`admin:audit?target=${u.id}`)}>{tt('Толық тарих', 'Вся история')}</button>} />
        <AuditList entries={d.audit} />
      </section>
      </> : null}
      </div>
    </div>
  );
}
