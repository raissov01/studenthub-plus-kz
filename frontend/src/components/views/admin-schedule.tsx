'use client';
/* /admin/schedule — «Күнтізбе»: events and club meetings on a week (ported from the kurs schedule). From 768px a week
   grid 08:00–22:00: click an empty half hour to create an event there, drag an event to move it (15-minute steps,
   another day sideways), click it for the details. On a phone the same week as a list of days. Everything goes
   through the event endpoints, so the server applies the same rights as /organize: admin every event, ambassadors
   and university staff their university, club admins their clubs. Times are Astana time. */
import { useEffect, useMemo, useRef, useState, type PointerEvent as RPointerEvent } from 'react';
import { useApp } from '@/components/app';
import { Icon } from '@/components/ui';
import { almatyWall, ErrorLine, errText, Feature, fromLocalInput, hhmm, Loading, NotFound, useLoad } from '@/components/views/common';
import { EventForm } from '@/components/views/events';
import { api, useAccount } from '@/lib/account';
import { canCreateEvents, evTitle, type DbEvent } from '@/lib/events';
import { addDays, lanes, minutesOf, mm, mondayOf, TONE, wallYmd } from '@/lib/schedule';

const DAY_START = 8 * 60, DAY_END = 22 * 60, SLOT = 30, SLOT_PX = 24, PX_PER_MIN = SLOT_PX / SLOT, SNAP = 15, GUTTER = 52;
const SLOTS = (DAY_END - DAY_START) / SLOT;
/** wall day + minutes -> the API's UTC instant */
export const toIso = (day: Date, min: number) => fromLocalInput(`${wallYmd(day)}T${mm(min)}`);

function useWide() {
  const [w, setW] = useState(false);
  useEffect(() => {
    const mq = window.matchMedia('(min-width: 768px)');
    const on = () => setW(mq.matches);
    on(); mq.addEventListener('change', on);
    return () => mq.removeEventListener('change', on);
  }, []);
  return w;
}

export function AdminSchedule() {
  return <Feature name="events"><Schedule /></Feature>;
}

function Schedule() {
  const { tt, lang, wd, monS, openSheet, closeSheet, toast } = useApp();
  const acc = useAccount();
  const wide = useWide();
  const [week, setWeek] = useState(() => mondayOf(almatyWall(new Date().toISOString())));
  const from = toIso(week, 0), to = toIso(addDays(week, 7), 0);
  const q = useLoad<{ events: DbEvent[] }>(acc.user ? `/api/events/manage?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}` : null);
  const [status, setStatus] = useState('');
  if (!acc.loaded) return <Loading />;
  if (!acc.user) return <NotFound />;
  if (q.code === 'forbidden') return <NotFound />;
  const can = canCreateEvents(acc.user.role);
  const events = (q.data?.events ?? []).filter(e => !status || e.status === status);
  const days = Array.from({ length: 7 }, (_, i) => addDays(week, i));
  const label = `${days[0].getDate()} ${monS[days[0].getMonth()]} – ${days[6].getDate()} ${monS[days[6].getMonth()]}`;
  const thisWeek = wallYmd(mondayOf(almatyWall(new Date().toISOString()))) === wallYmd(week);
  const create = (day: Date, min: number) => {
    if (!can) return;
    const end = Math.min(DAY_END, min + 90);
    openSheet(<div className="stack"><h2>{tt('Жаңа іс-шара', 'Новое мероприятие')}</h2>
      <EventForm prefill={{ starts: `${wallYmd(day)}T${mm(min)}`, ends: `${wallYmd(day)}T${mm(end)}` }}
        onCreated={() => { closeSheet(); q.reload(); }} /></div>);
  };
  const open = (e: DbEvent) => openSheet(<EventDetails e={e} onChanged={q.reload} />);
  const move = async (e: DbEvent, startsAt: string, endsAt: string) => {
    try { await api(`/api/events/${e.id}`, { method: 'PATCH', body: { starts_at: startsAt, ends_at: endsAt }, lang }); toast(tt('Уақыты өзгерді', 'Время изменено')); }
    catch (x) { toast(errText(x, tt('Жылжыту мүмкін болмады', 'Не удалось перенести'))); }
    q.reload();
  };
  return (
    <div className="stack sched">
      <p className="small muted">{wide && can
        ? tt('Бос уақытты басып жаңа іс-шара қосыңыз, іс-шараны сүйреп басқа уақытқа жылжытыңыз. Уақыт — Астана уақыты.', 'Нажмите на свободное время, чтобы добавить мероприятие; перетащите мероприятие, чтобы перенести. Время — по Астане.')
        : tt('Аптаның іс-шаралары мен клуб кездесулері. Уақыт — Астана уақыты.', 'Мероприятия и встречи клубов за неделю. Время — по Астане.')}</p>
      <div className="sched-bar">
        <div className="sched-nav">
          <button className="btn ghost sched-arrow" type="button" aria-label={tt('Алдыңғы апта', 'Предыдущая неделя')} onClick={() => setWeek(addDays(week, -7))}><Icon n="back" s={18} /></button>
          <button className="btn ghost sched-arrow" type="button" aria-label={tt('Келесі апта', 'Следующая неделя')} onClick={() => setWeek(addDays(week, 7))}><Icon n="chev" s={18} /></button>
          <button className="btn ghost" type="button" disabled={thisWeek} onClick={() => setWeek(mondayOf(almatyWall(new Date().toISOString())))}>{tt('Бүгін', 'Сегодня')}</button>
          <h2 className="sched-label num" aria-live="polite" data-testid="week-label">{label}</h2>
        </div>
        <div className="sched-tools">
          <label className="sr-only" htmlFor="schedStatus">{tt('Күйі', 'Статус')}</label>
          <select className="input" id="schedStatus" value={status} onChange={e => setStatus(e.target.value)}>
            <option value="">{tt('Барлығы', 'Все')}</option>
            <option value="published">{tt('Жарияланған', 'Опубликованные')}</option>
            <option value="draft">{tt('Черновиктер', 'Черновики')}</option>
            <option value="finished">{tt('Өткендер', 'Прошедшие')}</option>
            <option value="cancelled">{tt('Тоқтатылғандар', 'Отменённые')}</option>
          </select>
          {can ? <button className="btn" type="button" onClick={() => {
            const today = almatyWall(new Date().toISOString());
            create(today >= week && today < addDays(week, 7) ? today : week, 16 * 60);
          }}><Icon n="plus" s={18} /> {tt('Іс-шара қосу', 'Добавить мероприятие')}</button> : null}
        </div>
      </div>
      <ErrorLine err={q.err} />
      {!q.data ? (q.err ? null : <Loading />) : wide
        ? <WeekGrid days={days} events={events} canCreate={can} wd={wd} onSlot={create} onOpen={open} onMove={move} lang={lang} />
        : <Agenda days={days} events={events} wd={wd} monS={monS} onOpen={open} lang={lang} />}
      {q.data && !q.data.events.length ? <p className="small muted">{tt('Бұл аптада іс-шара жоқ.', 'На этой неделе мероприятий нет.')}</p> : null}
    </div>
  );
}

type Drag = { id: number; pid: number; x0: number; y0: number; colW: number; day0: number; min0: number; dur: number; day: number; min: number; moved: boolean };

function WeekGrid({ days, events, canCreate, wd, onSlot, onOpen, onMove, lang }: {
  days: Date[]; events: DbEvent[]; canCreate: boolean; wd: string[]; lang: string;
  onSlot: (day: Date, min: number) => void; onOpen: (e: DbEvent) => void; onMove: (e: DbEvent, s: string, en: string) => void;
}) {
  const { tt } = useApp();
  const overlay = useRef<HTMLDivElement>(null);
  const scroller = useRef<HTMLDivElement>(null);
  const [drag, setDrag] = useState<Drag | null>(null);
  const justDragged = useRef(false);
  const today = wallYmd(almatyWall(new Date().toISOString()));
  const placed = useMemo(() => days.flatMap((d, i) => lanes(events.filter(e => wallYmd(almatyWall(e.starts_at)) === wallYmd(d))).map(p => ({ ...p, day: i }))), [days, events]);
  useEffect(() => {
    const el = scroller.current; if (!el || !events.length) return;
    const first = Math.min(...events.map(e => minutesOf(almatyWall(e.starts_at))));
    el.scrollTop = Math.max(0, (first - 60 - DAY_START) * PX_PER_MIN);
  }, [events]);
  const movable = (e: DbEvent) => e.can_manage && (e.status === 'draft' || e.status === 'published');
  const down = (ev: RPointerEvent<HTMLButtonElement>, e: DbEvent, day: number) => {
    if (!movable(e) || ev.button !== 0 || !overlay.current) return;
    ev.currentTarget.setPointerCapture(ev.pointerId);
    const st = almatyWall(e.starts_at);
    const dur = Math.round((Date.parse(e.ends_at) - Date.parse(e.starts_at)) / 60_000);
    setDrag({ id: e.id, pid: ev.pointerId, x0: ev.clientX, y0: ev.clientY, colW: overlay.current.getBoundingClientRect().width / 7, day0: day, min0: minutesOf(st), dur, day, min: minutesOf(st), moved: false });
  };
  const moveP = (ev: RPointerEvent<HTMLButtonElement>) => {
    if (!drag || ev.pointerId !== drag.pid) return;
    const dx = ev.clientX - drag.x0, dy = ev.clientY - drag.y0;
    const moved = drag.moved || Math.abs(dx) > 4 || Math.abs(dy) > 4;
    const day = Math.max(0, Math.min(6, drag.day0 + Math.round(dx / drag.colW)));
    const min = Math.max(DAY_START, Math.min(DAY_END - Math.min(drag.dur, DAY_END - DAY_START), drag.min0 + Math.round(dy / PX_PER_MIN / SNAP) * SNAP));
    if (moved !== drag.moved || day !== drag.day || min !== drag.min) setDrag({ ...drag, moved, day, min });
  };
  const up = (ev: RPointerEvent<HTMLButtonElement>, e: DbEvent) => {
    if (!drag || ev.pointerId !== drag.pid) return;
    const d = drag; setDrag(null);
    if (!d.moved) return;
    justDragged.current = true; setTimeout(() => (justDragged.current = false), 0);
    if (d.day === d.day0 && d.min === d.min0) return;
    const s = toIso(days[d.day], d.min);
    onMove(e, s, new Date(Date.parse(s) + d.dur * 60_000).toISOString().replace('.000Z', 'Z'));
  };
  const cols = `${GUTTER}px repeat(7, minmax(0, 1fr))`;
  return (
    <div ref={scroller} className="wk" data-testid="week-grid">
      <div className="wk-in">
        <div className="wk-head" style={{ gridTemplateColumns: cols }}>
          <div />
          {days.map((d, i) => (
            <div key={i} className={`wk-day${wallYmd(d) === today ? ' is-today' : ''}`} aria-current={wallYmd(d) === today ? 'date' : undefined}>
              <span className="small muted">{wd[d.getDay()]}</span> <b className="num">{d.getDate()}</b>
            </div>
          ))}
        </div>
        <div className="wk-body" style={{ gridTemplateColumns: cols, height: SLOTS * SLOT_PX }}>
          <div className="wk-hours" aria-hidden>
            {Array.from({ length: SLOTS / 2 }, (_, h) => h === 0 ? null : <span key={h} style={{ top: h * SLOT_PX * 2 }}>{mm(DAY_START + h * 60)}</span>)}
          </div>
          {days.map((d, i) => (
            <div key={i} className={`wk-col${wallYmd(d) === today ? ' is-today' : ''}`} data-testid={`day-${wallYmd(d)}`}>
              {Array.from({ length: SLOTS }, (_, k) => {
                const m = DAY_START + k * SLOT;
                return canCreate
                  ? <button key={k} type="button" tabIndex={-1} className={`wk-slot${k % 2 ? ' half' : ''}`} style={{ top: k * SLOT_PX, height: SLOT_PX }}
                    data-testid={`slot-${wallYmd(d)}-${mm(m)}`} aria-label={tt(`${wd[d.getDay()]}, ${mm(m)}: іс-шара қосу`, `${wd[d.getDay()]}, ${mm(m)}: добавить мероприятие`)}
                    onClick={() => onSlot(d, m)} />
                  : <div key={k} className={`wk-slot${k % 2 ? ' half' : ''}`} style={{ top: k * SLOT_PX, height: SLOT_PX }} aria-hidden />;
              })}
            </div>
          ))}
          <div ref={overlay} className="wk-overlay" style={{ left: GUTTER }}>
            {placed.map(({ e, day, lane, lanes: n }) => {
              const dragging = drag?.id === e.id && drag.moved;
              const st = almatyWall(e.starts_at);
              const dur = (Date.parse(e.ends_at) - Date.parse(e.starts_at)) / 60_000;
              const dDay = dragging ? drag!.day : day, dMin = dragging ? drag!.min : minutesOf(st);
              const top = Math.max(0, (dMin - DAY_START) * PX_PER_MIN);
              const height = Math.max(SLOT_PX - 2, Math.min(dur, DAY_END - dMin) * PX_PER_MIN - 2);
              const laneW = dragging ? 1 : 1 / n;
              const left = ((dDay + (dragging ? 0 : lane * laneW)) / 7) * 100;
              const from = dragging ? mm(dMin) : hhmm(e.starts_at), to = dragging ? mm(dMin + dur) : hhmm(e.ends_at);
              return (
                <button key={e.id} type="button" data-testid="event-block" data-event-id={e.id}
                  className={`wk-ev ${TONE[e.status] ?? ''}${movable(e) ? ' movable' : ''}${dragging ? ' dragging' : ''}`}
                  title={movable(e) ? tt('Басу — толығырақ, сүйреу — жылжыту', 'Нажать — подробнее, перетащить — перенести') : undefined}
                  style={{ top, height, left: `calc(${left}% + 2px)`, width: `calc(${(laneW / 7) * 100}% - 4px)` }}
                  onPointerDown={ev => down(ev, e, day)} onPointerMove={moveP} onPointerUp={ev => up(ev, e)} onPointerCancel={() => setDrag(null)}
                  onClick={() => { if (!justDragged.current) onOpen(e); }}>
                  <span className="sr-only">{`${evTitle(e, lang as 'kk')}, ${wd[st.getDay()]} ${hhmm(e.starts_at)}–${hhmm(e.ends_at)}`}</span>
                  <span aria-hidden className="wk-ev-in">
                    <b className="num">{from}–{to}</b>
                    <span className="wk-ev-title">{evTitle(e, lang as 'kk')}</span>
                    {height >= 60 ? <span className="tiny num">{tt(`${e.registered} тіркелді`, `${e.registered} записались`)}</span> : null}
                  </span>
                </button>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}

function Agenda({ days, events, wd, monS, onOpen, lang }: { days: Date[]; events: DbEvent[]; wd: string[]; monS: string[]; onOpen: (e: DbEvent) => void; lang: string }) {
  const { tt } = useApp();
  const today = wallYmd(almatyWall(new Date().toISOString()));
  return (
    <ol className="agenda" data-testid="agenda">
      {days.map(d => {
        const key = wallYmd(d);
        const items = events.filter(e => wallYmd(almatyWall(e.starts_at)) === key).sort((a, b) => a.starts_at.localeCompare(b.starts_at));
        return (
          <li key={key} aria-current={key === today ? 'date' : undefined}>
            <h3 className="agenda-day">{wd[d.getDay()]}, {d.getDate()} {monS[d.getMonth()]}{key === today ? <span className="badge b-sky">{tt('бүгін', 'сегодня')}</span> : null}</h3>
            {items.length ? (
              <ul className="agenda-list">
                {items.map(e => (
                  <li key={e.id}>
                    <button type="button" data-testid="agenda-item" className={`agenda-item ${TONE[e.status] ?? ''}`} onClick={() => onOpen(e)}>
                      <span className="agenda-time num"><b>{hhmm(e.starts_at)}</b><span className="small muted">{hhmm(e.ends_at)}</span></span>
                      <span className="agenda-main"><b>{evTitle(e, lang as 'kk')}</b>
                        <span className="small muted">{[statusLabel(e.status, tt), e.place_text, tt(`${e.registered} тіркелді`, `${e.registered} записались`)].filter(Boolean).join(' · ')}</span></span>
                      <Icon n="chev" s={18} />
                    </button>
                  </li>
                ))}
              </ul>
            ) : <p className="small muted">{tt('Іс-шара жоқ', 'Мероприятий нет')}</p>}
          </li>
        );
      })}
    </ol>
  );
}

export function statusLabel(s: string, tt: (kk: string, ru: string) => string) {
  return ({ draft: tt('Черновик', 'Черновик'), published: tt('Жарияланған', 'Опубликовано'), finished: tt('Өтті', 'Прошло'), cancelled: tt('Тоқтатылды', 'Отменено') } as Record<string, string>)[s] ?? s;
}

/** the event's details in a sheet: status, numbers, publish / edit / cancel, the check-in page */
function EventDetails({ e, onChanged }: { e: DbEvent; onChanged: () => void }) {
  const { tt, lang, I, toast, closeSheet, go } = useApp();
  const [edit, setEdit] = useState(false);
  const [busy, setBusy] = useState(false);
  const [confirmCancel, setConfirmCancel] = useState(false);
  const st = almatyWall(e.starts_at);
  const patch = async (body: Record<string, unknown>, done: string) => {
    setBusy(true);
    try { await api(`/api/events/${e.id}`, { method: 'PATCH', body, lang }); toast(done); closeSheet(); onChanged(); }
    catch (x) { toast(errText(x)); } finally { setBusy(false); }
  };
  if (edit) return <div className="stack"><h2>{tt('Іс-шараны өзгерту', 'Изменить мероприятие')}</h2><EventForm edit={e} onSaved={() => { closeSheet(); onChanged(); }} /></div>;
  const open = e.status === 'draft' || e.status === 'published';
  return (
    <div className="stack" data-testid="event-details">
      <h2>{evTitle(e, lang)}</h2>
      <div className="badges"><span className={`badge ${e.status === 'published' ? 'b-ok' : e.status === 'cancelled' ? 'b-bad' : 'b-line'}`}>{statusLabel(e.status, tt)}</span>
        {e.club_id ? <span className="badge b-sky">{tt('Клуб кездесуі', 'Встреча клуба')}</span> : null}</div>
      <dl className="kv">
        <div className="kv-row"><dt>{tt('Уақыты', 'Когда')}</dt><dd className="num">{I.fDate(st)}, {hhmm(e.starts_at)}–{hhmm(e.ends_at)}</dd></div>
        {e.place_text ? <div className="kv-row"><dt>{tt('Орны', 'Где')}</dt><dd>{e.place_text}</dd></div> : null}
        <div className="kv-row"><dt>{tt('Университет', 'Университет')}</dt><dd>{e.university}</dd></div>
        <div className="kv-row"><dt>{tt('Тіркелгендер', 'Записались')}</dt><dd className="num">{e.registered}{e.capacity ? ` / ${e.capacity}` : ''}{e.waitlist ? tt(` · кезекте ${e.waitlist}`, ` · в очереди ${e.waitlist}`) : ''}</dd></div>
        <div className="kv-row"><dt>{tt('Келгендер', 'Пришли')}</dt><dd className="num">{e.checked_in}</dd></div>
      </dl>
      {e.can_manage ? (
        <div className="btn-col">
          {e.status === 'draft' ? <button className="btn" type="button" disabled={busy} onClick={() => void patch({ status: 'published' }, tt('Жарияланды', 'Опубликовано'))}>{tt('Жариялау', 'Опубликовать')}</button> : null}
          <button className="btn secondary" type="button" onClick={() => { closeSheet(); go('organize:' + e.id); }}>{tt('Келгендерді белгілеу (QR)', 'Отметить пришедших (QR)')}</button>
          {open ? <button className="btn ghost" type="button" onClick={() => setEdit(true)}>{tt('Өзгерту', 'Изменить')}</button> : null}
          {open && !confirmCancel ? <button className="btn ghost" type="button" onClick={() => setConfirmCancel(true)}>{tt('Іс-шараны тоқтату', 'Отменить мероприятие')}</button> : null}
          {confirmCancel ? (
            <div className="card stack" role="alert">
              <p className="small">{tt(`Тіркелген ${e.registered} адамға хабар кетеді. Қайтару мүмкін емес.`, `${e.registered} записавшихся получат уведомление. Вернуть нельзя.`)}</p>
              <div className="btn-row">
                <button className="btn ghost" type="button" onClick={() => setConfirmCancel(false)}>{tt('Жоқ', 'Нет')}</button>
                <button className="btn danger" type="button" disabled={busy} onClick={() => void patch({ status: 'cancelled' }, tt('Тоқтатылды', 'Отменено'))}>{tt('Иә, тоқтату', 'Да, отменить')}</button>
              </div>
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
