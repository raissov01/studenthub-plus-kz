'use client';
/* /admin — only for role admin (GET /api/me). Everyone else gets the same neutral screen as an unknown page.
   Users by role, role grant/revoke with a required reason, feature switches, and the three analytics metrics
   from docs/ANALYTICS.md in plain words. */
import { useCallback, useEffect, useState } from 'react';
import { DemoSettings } from '@/components/views/admin-settings';
import { useApp } from '@/components/app';
import { Row, SectionHead } from '@/components/ui';
import { NotFound } from '@/components/views/common';
import { ModerationLinks } from '@/components/views/moderation';
import { roleLabel } from '@/components/views/account';
import { api, ApiError, useAccount, type Role } from '@/lib/account';

interface Summary { users: { total: number; byRole: Record<string, number>; withConsent: number }; activeReminders: number; features: Record<string, boolean> }
interface Report {
  devices: number; returned_day1: number; eligible_day1: number; returned_day7: number; eligible_day7: number; saved_first: number;
  from_share: number; ambassadors: Record<string, number> | null; feedback_avg: number; feedback_count: number; feedback_recent: string[] | null;
}
interface UserRow { id: number; telegramId: number; username: string | null; name: string | null; role: Role; university: string | null; consentAt: string | null; createdAt: string }

const ROLES: Role[] = ['student', 'ambassador', 'admin', 'university_staff', 'employer'];
const pct = (a: number, b: number) => (b > 0 ? `${Math.round((a / b) * 100)}%` : '—');


function RoleSheet({ u, onDone }: { u: UserRow; onDone: () => void }) {
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

function Stat({ value, label }: { value: string | number; label: string }) {
  return <div className="stat"><b>{value}</b><span>{label}</span></div>;
}

function Dashboard() {
  const { tt, lang, openSheet } = useApp();
  const [sum, setSum] = useState<Summary | null>(null);
  const [rep, setRep] = useState<Report | null>(null);
  const [users, setUsers] = useState<UserRow[]>([]);
  const [q, setQ] = useState('');
  const [err, setErr] = useState('');
  const load = useCallback((query: string) => {
    let live = true;
    Promise.all([
      api<Summary>('/api/admin/summary', { lang }),
      api<Report>('/api/admin/analytics?days=30', { lang }),
      api<{ users: UserRow[] }>('/api/admin/users?q=' + encodeURIComponent(query), { lang }),
    ]).then(([s, r, u]) => {
      if (!live) return;
      setSum(s); setRep(r); setUsers(u.users); setErr('');
    }, e => { if (live) setErr(e instanceof ApiError ? e.message : String(e)); });
    return () => { live = false; };
  }, [lang]);
  useEffect(() => load(''), [load]);

  if (err) return <p className="small urgent" role="alert">{err}</p>;
  if (!sum || !rep) return <p className="muted small" role="status">{tt('Жүктеліп жатыр…', 'Загрузка…')}</p>;
  const amb = Object.entries(rep.ambassadors || {}).sort((a, b) => b[1] - a[1]);
  return (
    <>
      <div className="group"><Row go="admin:health" icon="shield" title={tt('Денсаулық', 'Состояние системы')} sub={tt('Қателер, бот және күнделікті тапсырмалар', 'Ошибки, бот и ежедневные задачи')} /></div>
      <DemoSettings />
      <ModerationLinks />
      <section className="section">
        <SectionHead title={tt('Басты 3 көрсеткіш (30 күн)', 'Главные 3 показателя (30 дней)')} />
        <div className="stat-grid">
          <Stat value={pct(rep.returned_day1, rep.eligible_day1)} label={tt(`Келесі күні қайта оралғандар (${rep.returned_day1} / ${rep.eligible_day1})`, `Вернулись на следующий день (${rep.returned_day1} / ${rep.eligible_day1})`)} />
          <Stat value={pct(rep.returned_day7, rep.eligible_day7)} label={tt(`7-күні қайта оралғандар (${rep.returned_day7} / ${rep.eligible_day7})`, `Вернулись на 7-й день (${rep.returned_day7} / ${rep.eligible_day7})`)} />
          <Stat value={pct(rep.saved_first, rep.devices)} label={tt(`Бірінші мүмкіндікті сақтағандар (${rep.saved_first} / ${rep.devices})`, `Сохранили первую возможность (${rep.saved_first} / ${rep.devices})`)} />
        </div>
        <p className="tiny muted">{tt('Құрылғылар анонимді. «Оралғандар» тек сол күнге жетіп үлгерген құрылғылар арасынан саналады.', 'Устройства анонимны. «Вернувшиеся» считаются только среди устройств, для которых этот день уже наступил.')}</p>
      </section>
      <section className="section">
        <SectionHead title={tt('Қайдан келді', 'Откуда пришли')} />
        <div className="stat-grid">
          <Stat value={rep.devices} label={tt('Жаңа құрылғылар', 'Новые устройства')} />
          <Stat value={rep.from_share} label={tt('Досының сілтемесімен', 'По ссылке друга')} />
        </div>
        <div className="group">
          {amb.length ? amb.map(([code, n]) => <Row key={code} icon="user" title={code.replace(/^amb_/, '')} end={<b className="num">{n}</b>} chev={false} />)
            : <div className="empty">{tt('Амбассадор сілтемелерімен әлі ешкім келмеді', 'По ссылкам амбассадоров пока никто не пришёл')}</div>}
        </div>
      </section>
      <section className="section">
        <SectionHead title={tt('Кері байланыс', 'Обратная связь')} more={<span className="tiny muted num">{rep.feedback_count ? `${rep.feedback_avg.toFixed(1)} / 5 · ${rep.feedback_count}` : ''}</span>} />
        <div className="group">
          {(rep.feedback_recent || []).length ? (rep.feedback_recent || []).map((t, i) => <div key={i} className="row"><span className="row-main"><span className="row-sub">{t}</span></span></div>)
            : <div className="empty">{tt('Әзірге жауап жоқ', 'Ответов пока нет')}</div>}
        </div>
      </section>
      <section className="section">
        <SectionHead title={tt('Қолданушылар', 'Пользователи')} />
        <div className="stat-grid">
          <Stat value={sum.users.total} label={tt('Барлығы', 'Всего')} />
          <Stat value={sum.users.withConsent} label={tt('Келісім бергендер', 'Дали согласие')} />
          <Stat value={sum.activeReminders} label={tt('Белсенді еске салулар', 'Активные напоминания')} />
          {ROLES.filter(r => r !== 'student').map(r => <Stat key={r} value={sum.users.byRole[r] ?? 0} label={roleLabel(r, tt)} />)}
        </div>
        <form className="search" role="search" onSubmit={e => { e.preventDefault(); load(q.trim()); }}>
          <input className="input" type="search" aria-label={tt('Қолданушыны іздеу', 'Поиск пользователя')} placeholder={tt('Аты, @username не Telegram id', 'Имя, @username или Telegram id')} value={q} onChange={e => setQ(e.target.value)} style={{ paddingLeft: 14 }} />
        </form>
        <div className="group">
          {users.length ? users.map(u => (
            <Row key={u.id} onClick={() => openSheet(<RoleSheet u={u} onDone={() => load(q.trim())} />)} icon="user"
              title={u.name || (u.username ? '@' + u.username : `#${u.id}`)}
              sub={[u.username ? '@' + u.username : '', roleLabel(u.role, tt), u.consentAt ? '' : tt('келісімсіз', 'без согласия')].filter(Boolean).join(' · ')} />
          )) : <div className="empty">{tt('Ешкім табылмады', 'Никого не найдено')}</div>}
        </div>
      </section>
      <section className="section">
        <SectionHead title={tt('Ажыратқыштар', 'Переключатели функций')} />
        <div className="group">
          {Object.entries(sum.features).sort().map(([k, on]) => (
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
  if (!acc.loaded) return null;
  if (acc.user?.role !== 'admin') return <NotFound />;
  return <Dashboard />;
}
