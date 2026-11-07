'use client';
/* /admin/attendance — «Келу»: how many of those who signed up for an event really came (QR check-in), per week and per
   event (kurs attendance, adapted). The list holds the events the server lets the person manage; marking someone by
   hand stays on the event's own check-in page. */
import { useState } from 'react';
import { useApp } from '@/components/app';
import { ChartCard, DataTable, KpiStrip, LineChart, type Point } from '@/components/charts';
import { almatyWall, ErrorLine, Feature, hhmm, Loading, NotFound, useLoad } from '@/components/views/common';
import { useAccount } from '@/lib/account';
import { evTitle } from '@/lib/events';

interface Row {
  id: number; title_kk: string; title_ru: string | null; cat: string; university: string; club_id: number | null; status: string;
  starts_at: string; ends_at: string; capacity: number | null; registered: number; checked_in: number; qr: number; manual: number; walk_in: number;
}
interface Resp { weeks: { week: string; events: number; registered: number; checkedIn: number }[]; events: Row[]; totals: { events: number; registered: number; checkedIn: number } }

export const rate = (came: number, reg: number) => (reg > 0 ? Math.round((came / reg) * 100) : null);

export function AdminAttendance() {
  return <Feature name="events"><Attendance /></Feature>;
}

function Attendance() {
  const { tt, lang, I, monS, go } = useApp();
  const acc = useAccount();
  const r = useLoad<Resp>(acc.user ? '/api/events/attendance?weeks=12' : null);
  const [low, setLow] = useState(false);
  if (!acc.loaded) return <Loading />;
  if (!acc.user || r.code === 'forbidden') return <NotFound />;
  if (!r.data) return <><ErrorLine err={r.err} />{r.err ? null : <Loading />}</>;
  const d = r.data;
  const total = rate(d.totals.checkedIn, d.totals.registered);
  const weeks: Point[] = d.weeks.map(w => {
    const [, m, dd] = w.week.split('-').map(Number);
    const p = rate(w.checkedIn, w.registered);
    return { label: `${dd}.${String(m).padStart(2, '0')}`, long: tt(`${dd} ${monS[m - 1]} аптасы`, `Неделя с ${dd} ${monS[m - 1]}`), value: p,
      text: p === null ? tt('іс-шара болмады', 'мероприятий не было') : tt(`${p}% (${w.checkedIn} / ${w.registered}, ${w.events} іс-шара)`, `${p}% (${w.checkedIn} из ${w.registered}, мероприятий: ${w.events})`) };
  });
  const list = low ? d.events.filter(e => (rate(e.checked_in, e.registered) ?? 100) < 50) : d.events;
  return (
    <div className="stack">
      <p className="small muted">{tt('Тіркелгендердің қаншасы шынымен келді (QR, қолмен не тіркеусіз келгендер). Соңғы 12 апта.', 'Сколько записавшихся действительно пришли (QR, вручную или без записи). Последние 12 недель.')}</p>
      <KpiStrip items={[
        { label: tt('Іс-шара өтті', 'Прошло мероприятий'), value: String(d.totals.events) },
        { label: tt('Тіркелді', 'Записались'), value: String(d.totals.registered) },
        { label: tt('Келді', 'Пришли'), value: String(d.totals.checkedIn) },
        { label: tt('Келу үлесі', 'Доля пришедших'), value: total === null ? '—' : `${total}%`, tone: total !== null && total < 50 ? 'warn' : undefined },
      ]} />
      <ChartCard testId="chart-attendance" title={tt('Келу үлесі, апта сайын', 'Доля пришедших по неделям')} subtitle={tt('Келгендер / тіркелгендер', 'Пришли / записались')}
        table={<DataTable head={[tt('Апта', 'Неделя'), tt('Келді', 'Пришли')]} rows={weeks.map(p => [p.long, p.text])} />}>
        <LineChart data={weeks} label={`${tt('Келу үлесі', 'Доля пришедших')}: ${weeks.map(p => `${p.long} ${p.text}`).join('; ')}`} />
      </ChartCard>
      <div className="section-head">
        <h2 className="h2-small">{tt('Іс-шаралар', 'Мероприятия')}</h2>
        <label className="check small"><input type="checkbox" checked={low} onChange={e => setLow(e.target.checked)} /> {tt('Тек жартысынан азы келгендер', 'Только где пришли меньше половины')}</label>
      </div>
      {list.length ? (
        <div className="group" data-testid="attendance-list">
          {list.map(e => {
            const p = rate(e.checked_in, e.registered);
            return (
              <button key={e.id} type="button" className="att-row" onClick={() => go('organize:' + e.id)}>
                <span className="att-main">
                  <b>{evTitle(e, lang)}</b>
                  <span className="small muted">{I.fDate(almatyWall(e.starts_at))}, {hhmm(e.starts_at)} · {e.university}{e.club_id ? tt(' · клуб', ' · клуб') : ''}</span>
                  <span className="att-bar" aria-hidden><i style={{ width: `${p ?? 0}%` }} /></span>
                  <span className="tiny muted">{tt(`QR ${e.qr} · қолмен ${e.manual} · тіркеусіз ${e.walk_in}`, `QR ${e.qr} · вручную ${e.manual} · без записи ${e.walk_in}`)}</span>
                </span>
                <span className="att-num num"><b className={p !== null && p < 50 ? 'urgent' : ''}>{p === null ? '—' : `${p}%`}</b>
                  <span className="small muted">{e.checked_in} / {e.registered}</span></span>
              </button>
            );
          })}
        </div>
      ) : <div className="group"><div className="empty">{low ? tt('Бәрінде жартысынан көбі келген', 'Везде пришло больше половины') : tt('Соңғы 12 аптада іс-шара болмады', 'За последние 12 недель мероприятий не было')}</div></div>}
    </div>
  );
}
