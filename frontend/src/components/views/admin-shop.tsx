'use client';
/* /admin/shop?cafe=N, /admin/shop?print=N — the shop workspace of a café or print shop for its own staff (and admins):
   today's numbers for everyone on the staff; for the manager also revenue by day, the shop's Kaspi status, the menu /
   price list and the team (add / remove baristas or operators, audited). Any other shop answers 403 on the server,
   shown as the neutral not-found screen. Money is the handed-over orders' sum; it goes to the shop's own account. */
import { useState } from 'react';
import { useApp } from '@/components/app';
import { SectionHead } from '@/components/ui';
import { ErrorLine, errText, Feature, Loading, NotFound, useLoad, useQueryParam, ymd } from '@/components/views/common';
import { DayBars, Stat } from '@/components/views/admin';
import { StaffMenu } from '@/components/views/cafe';
import { PrintCatalog } from '@/components/views/print';
import { api, useAccount } from '@/lib/account';
import { money } from '@/lib/cafe';
import { who } from '@/lib/console';

interface Stats {
  role: string;
  shop: { id: number; kind: 'cafe' | 'print'; nameKk: string; nameRu: string; paymentMode: string; active: boolean };
  today: { byStatus: Record<string, number>; created: number; done: number; refundsToReturn: number; revenue?: number };
  days?: { day: string; orders: number; done: number; revenue: number }[];
  kaspi?: { connected: boolean; broken: boolean; phoneMasked: string; orgName: string };
  team?: { userId: number; name: string | null; username: string | null; role: string; addedAt: string }[];
}

export function AdminShop() {
  const cafe = useQueryParam('cafe'), print = useQueryParam('print');
  if (cafe && /^\d+$/.test(cafe)) return <Feature name="cafe"><Shop kind="cafe" id={Number(cafe)} /></Feature>;
  if (print && /^\d+$/.test(print)) return <Feature name="print"><Shop kind="print" id={Number(print)} /></Feature>;
  return <NotFound />;
}

function Shop({ kind, id }: { kind: 'cafe' | 'print'; id: number }) {
  const { tt, lang, go, openSheet } = useApp();
  const acc = useAccount();
  const r = useLoad<Stats>(acc.user ? `/api/console/${kind}/${id}/stats?days=14` : null);
  if (!acc.loaded) return <Loading />;
  if (!acc.user || r.code === 'not_staff' || r.code === 'not_found' || r.code === 'forbidden') return <NotFound />;
  if (!r.data) return <><ErrorLine err={r.err} />{r.err ? null : <Loading />}</>;
  const s = r.data, t = s.today, manager = s.role === 'manager' || s.role === 'admin';
  const name = lang !== 'kk' ? s.shop.nameRu : s.shop.nameKk;
  const working = kind === 'cafe' ? 'accepted' : 'printing';
  const onBoard = (t.byStatus.new ?? 0) + (t.byStatus[working] ?? 0) + (t.byStatus.ready ?? 0);
  const closedBad = (t.byStatus.cancelled ?? 0) + (t.byStatus.no_show ?? 0);
  const roleName = s.role === 'manager' ? tt('менеджер', 'менеджер') : s.role === 'admin' ? tt('әкімші', 'администратор') : kind === 'cafe' ? tt('бариста', 'бариста') : tt('оператор', 'оператор');
  return (
    <div className="stack">
      <article className="card">
        <h2 style={{ margin: 0 }}>{name}</h2>
        <div className="badges">
          <span className={`badge ${s.shop.active ? 'b-ok' : 'b-line'}`}>{s.shop.active ? tt('Белсенді', 'Активна') : tt('Өшірулі', 'Выключена')}</span>
          <span className="badge b-line">{roleName}</span>
          <span className="badge b-line">{s.shop.paymentMode === 'kaspi' ? 'Kaspi' : s.shop.paymentMode === 'both' ? tt('Kaspi не кассада', 'Kaspi или на кассе') : tt('Кассада төлеу', 'Оплата на кассе')}</span>
        </div>
        <div className="panel-actions">
          <button className="btn secondary" type="button" onClick={() => go(kind === 'cafe' ? 'cafe:staff' : 'print:staff')}>{kind === 'cafe' ? tt('Тапсырыстар тақтасы', 'Доска заказов') : tt('Кезек тақтасы', 'Доска очереди')}</button>
          {manager ? <button className="btn ghost" type="button" onClick={() => openSheet(kind === 'cafe'
            ? <><h2>{tt('Мәзір', 'Меню')}</h2><StaffMenu cafeId={id} role={s.role} /></>
            : <><h2>{tt('Прайс', 'Прайс')}</h2><PrintCatalog shopId={id} role={s.role} /></>)}>{kind === 'cafe' ? tt('Мәзір', 'Меню') : tt('Прайс', 'Прайс')}</button> : null}
          {s.role === 'admin' ? <button className="btn ghost" type="button" onClick={() => go(kind === 'cafe' ? 'admin:cafe' : 'admin:print')}>{tt('Баптау (әкімші)', 'Настройки (админ)')}</button> : null}
        </div>
      </article>
      <section className="section">
        <SectionHead title={tt('Бүгін', 'Сегодня')} more={<button className="link-btn" type="button" onClick={r.reload}>{tt('Жаңарту', 'Обновить')}</button>} />
        <div className="stat-grid">
          <Stat value={t.created} label={tt('Бүгін түскен', 'Поступило сегодня')} />
          <Stat value={onBoard} label={tt('Қазір тақтада', 'Сейчас на доске')} />
          <Stat value={t.done} label={tt('Берілді', 'Выдано')} />
          <Stat value={closedBad} label={tt('Бас тартылды / келмеді', 'Отменено / не пришли')} />
          {t.revenue !== undefined ? <Stat value={money(t.revenue)} label={tt('Берілгендер сомасы', 'Сумма выданных')} /> : null}
          {t.refundsToReturn ? <Stat value={t.refundsToReturn} label={tt('Ақшаны қолмен қайтару керек', 'Нужно вернуть деньги вручную')} tone="warn" /> : null}
        </div>
      </section>
      {manager && s.days ? (
        <section className="section">
          <SectionHead title={tt('Соңғы 14 күн', 'Последние 14 дней')} />
          <DayBars series={s.days.map(d => ({ day: d.day, n: Math.round(d.revenue / 100) }))} label={tt('Түсім, ₸', 'Выручка, ₸')} />
          <div className="group table-wrap">
            <table>
              <thead><tr><th scope="col">{tt('Күн', 'День')}</th><th scope="col">{tt('Түскен', 'Поступило')}</th><th scope="col">{tt('Берілді', 'Выдано')}</th><th scope="col">{tt('Сома', 'Сумма')}</th></tr></thead>
              <tbody>{[...s.days].reverse().map(d => (
                <tr key={d.day}><td className="num">{d.day.slice(5)}</td><td className="num">{d.orders}</td><td className="num">{d.done}</td><td className="num">{money(d.revenue)}</td></tr>
              ))}</tbody>
            </table>
          </div>
        </section>
      ) : null}
      {manager && s.kaspi ? (
        <section className="section">
          <SectionHead title="Kaspi" />
          <div className={`card${s.kaspi.broken ? ' adm-danger' : ''}`}>
            <p className="small">{!s.kaspi.connected ? tt('Kaspi кассир сессиясы қосылмаған: тек кассада төлеу.', 'Сессия кассира Kaspi не подключена: только оплата на кассе.')
              : s.kaspi.broken ? tt(`Kaspi сессиясы үзілді (${s.kaspi.phoneMasked}): әкімші қайта кіруі керек.`, `Сессия Kaspi прервана (${s.kaspi.phoneMasked}): администратору нужно войти снова.`)
                : tt(`Қосулы: ${s.kaspi.orgName || '—'} · ${s.kaspi.phoneMasked}`, `Подключено: ${s.kaspi.orgName || '—'} · ${s.kaspi.phoneMasked}`)}</p>
            <p className="tiny muted">{tt('Kaspi-ге қосуды (SMS коды) SIVA әкімшісі жасайды. Ақша әрқашан дүкеннің өз шотына түседі.', 'Подключение к Kaspi (код из SMS) делает администратор SIVA. Деньги всегда идут на счёт самого магазина.')}</p>
          </div>
        </section>
      ) : null}
      {manager && s.team ? <Team kind={kind} id={id} role={s.role} team={s.team} onDone={r.reload} /> : null}
    </div>
  );
}

function Team({ kind, id, role, team, onDone }: { kind: 'cafe' | 'print'; id: number; role: string; team: NonNullable<Stats['team']>; onDone: () => void }) {
  const { tt, lang, toast } = useApp();
  const me = useAccount().user;
  const worker = kind === 'cafe' ? 'barista' : 'operator';
  const [handle, setHandle] = useState('');
  const [newRole, setNewRole] = useState(worker);
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const label = (r: string) => r === 'manager' ? tt('менеджер', 'менеджер') : kind === 'cafe' ? tt('бариста', 'бариста') : tt('оператор', 'оператор');
  const remove = async (userId: number) => {
    setErr('');
    try { await api(`/api/console/${kind}/${id}/team/${userId}`, { method: 'DELETE', lang }); toast(tt('Алынды', 'Убран')); onDone(); }
    catch (e) { setErr(errText(e)); }
  };
  return (
    <section className="section">
      <SectionHead title={tt('Команда', 'Команда')} />
      <div className="group">
        {team.map(m => (
          <div key={m.userId} className="row">
            <span className="row-main">
              <span className="row-title">{who(m)}{m.userId === me?.id ? ` (${tt('сіз', 'вы')})` : ''}</span>
              <span className="row-sub">{label(m.role)} · {tt('қосылған', 'добавлен')} {ymd(m.addedAt)}</span>
            </span>
            {m.userId !== me?.id && (m.role !== 'manager' || role === 'admin')
              ? <span className="row-end"><button className="btn ghost" type="button" onClick={() => remove(m.userId)} aria-label={tt(`${who(m)}: алып тастау`, `${who(m)}: убрать`)}>{tt('Алып тастау', 'Убрать')}</button></span> : null}
          </div>
        ))}
      </div>
      <form className="card form" onSubmit={async e => {
        e.preventDefault();
        if (!handle.trim() || busy) return;
        setBusy(true); setErr('');
        try {
          await api(`/api/console/${kind}/${id}/team`, { method: 'POST', body: { user: handle.trim(), role: newRole, reason: reason.trim() }, lang });
          setHandle(''); setReason(''); toast(tt('Қосылды', 'Добавлен')); onDone();
        } catch (x) { setErr(errText(x)); } finally { setBusy(false); }
      }}>
        <div className="field"><label htmlFor="tmUser">{tt('Жаңа қызметкер: @username, email не id', 'Новый сотрудник: @username, email или id')}</label>
          <input className="input" id="tmUser" autoComplete="off" maxLength={120} value={handle} onChange={e => setHandle(e.target.value)} /></div>
        {role === 'admin' ? (
          <div className="field"><label htmlFor="tmRole">{tt('Рөлі', 'Роль')}</label>
            <select className="input" id="tmRole" value={newRole} onChange={e => setNewRole(e.target.value)}>
              <option value={worker}>{label(worker)}</option><option value="manager">{label('manager')}</option>
            </select></div>
        ) : <p className="tiny muted">{tt(`Менеджер тек ${label(worker)} қоса алады; жаңа менеджерді әкімші тағайындайды.`, `Менеджер добавляет только: ${label(worker)}; нового менеджера назначает администратор.`)}</p>}
        <div className="field"><label htmlFor="tmReason">{tt('Ескертпе журналға (міндетті емес)', 'Комментарий в журнал (необязательно)')}</label>
          <input className="input" id="tmReason" maxLength={500} value={reason} onChange={e => setReason(e.target.value)} /></div>
        <ErrorLine err={err} />
        <button className="btn secondary" type="submit" disabled={!handle.trim() || busy}>{tt('Қосу', 'Добавить')}</button>
      </form>
    </section>
  );
}
