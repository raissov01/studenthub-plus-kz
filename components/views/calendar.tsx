'use client';
/* «Күнтізбе» (/calendar): a Google-Calendar-like month grid (Monday first, Almaty days) and a week list of
   application deadlines (opportunities, vacancies), events and the student's own deadlines kept on the device.
   Every item can go to Google Calendar or download as .ics. Date logic lives in lib/calendar.ts. */
import { useEffect, useRef, type KeyboardEvent } from 'react';
import { useApp } from '@/components/app';
import { ConfirmSheet, DemoBadge, Empty, Icon, SectionHead } from '@/components/ui';
import {
  addDays, almatyHm, almatyYmd, byDay, daysBetween, googleCalendarUrl, isHm, isYmd, monthGrid, monthKey, monthOf, MONTHS,
  shiftMonth, WEEK_SHORT, weekdayMon, weekdaySun, type OwnDeadline, type Ymd,
} from '@/lib/calendar';
import { typeLabel } from '@/lib/domain';
import { buildOneIcs } from '@/lib/ics';
import { pathOf } from '@/lib/routes';
import type { I18n } from '@/lib/format';
import type { AppState, Content } from '@/lib/types';

export type CalKind = 'opp' | 'job' | 'event' | 'own';
export interface CalItem {
  key: string;
  kind: CalKind;
  date: Ymd;
  time: string;
  title: string;
  sub: string;
  go?: string;
  saved: boolean;
  demo: boolean;
  own?: OwnDeadline;
}

export const todayYmd = () => almatyYmd(Date.now());

/** everything the calendar shows, from the content feed and the device state */
export function calItems(S: AppState, I: I18n, C: Content): CalItem[] {
  const { tt, L } = I;
  const today = todayYmd();
  const out: CalItem[] = [];
  for (const o of C.opportunities) {
    if (!o.deadline || !isYmd(o.deadline.slice(0, 10))) continue;
    out.push({
      key: 'opp:' + o.id, kind: 'opp', date: o.deadline.slice(0, 10), time: '', title: L(o.title),
      sub: `${typeLabel(o.type, I)} · ${tt('өтінім мерзімі', 'приём заявок до')}`, go: 'opp:' + o.id, saved: !!S.saved[o.id], demo: !!o.is_demo,
    });
  }
  for (const j of C.jobs) {
    if (!j.deadline || !isYmd(j.deadline.slice(0, 10)) || j.deadline.slice(0, 10) < today) continue;
    out.push({
      key: 'job:' + j.id, kind: 'job', date: j.deadline.slice(0, 10), time: '', title: L(j.title),
      sub: `${L(j.org)} · ${tt('өтінім мерзімі', 'отклик до')}`, go: 'job:' + j.id, saved: !!S.fav[j.id], demo: !!j.is_demo,
    });
  }
  for (const e of C.events ?? []) {
    if (!e.starts_at || Number.isNaN(Date.parse(e.starts_at))) continue;
    out.push({
      key: 'event:' + e.id, kind: 'event', date: almatyYmd(e.starts_at), time: almatyHm(e.starts_at),
      title: (S.lang === 'ru' ? e.title_ru || e.title_kk : e.title_kk) || '', sub: [tt('Іс-шара', 'Мероприятие'), e.place_text].filter(Boolean).join(' · '),
      go: 'event:' + e.id, saved: false, demo: !!e.is_demo,
    });
  }
  for (const d of S.deadlines ?? []) {
    out.push({ key: 'own:' + d.id, kind: 'own', date: d.date, time: d.time, title: d.title, sub: d.note || tt('Менің дедлайным', 'Мой дедлайн'), saved: true, demo: false, own: d });
  }
  return out;
}

/** the next deadlines from today (not events): the home card */
export function nextDeadlines(S: AppState, I: I18n, C: Content, n: number): CalItem[] {
  const today = todayYmd();
  return calItems(S, I, C).filter(i => i.kind !== 'event' && i.date >= today)
    .sort((a, b) => a.date.localeCompare(b.date) || (a.time || '').localeCompare(b.time || '')).slice(0, n);
}

/** «4 қараша, сәрсенбі» / «4 ноября, среда» */
export function dayLabel(d: Ymd, I: I18n, year = false) {
  const x = new Date(+d.slice(0, 4), +d.slice(5, 7) - 1, +d.slice(8, 10));
  return `${year ? I.fDateY(x) : I.fDate(x)}, ${I.wd[weekdaySun(d)]}`;
}
/** «Бүгін», «Ертең», «5 күннен кейін» */
export function relLabel(d: Ymd, I: I18n) {
  const n = daysBetween(todayYmd(), d);
  if (n === 0) return I.tt('Бүгін', 'Сегодня');
  if (n === 1) return I.tt('Ертең', 'Завтра');
  if (n < 0) return I.tt('Өтті', 'Прошло');
  return I.daysLeftTxt(n);
}

/** a text file the browser saves (the .ics export) */
export function saveFile(text: string, name: string, type = 'text/calendar;charset=utf-8') {
  const url = URL.createObjectURL(new Blob([text], { type }));
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

const KIND_ICON = { opp: 'globe', job: 'briefcase', event: 'calendar', own: 'clock' } as const;

function exportOf(it: CalItem, I: I18n) {
  const link = it.go && typeof window !== 'undefined' ? window.location.origin + pathOf(it.go) : '';
  const details = [it.kind === 'own' ? it.own?.note : it.sub, link, 'Student Hub'].filter(Boolean).join('\n');
  const title = it.kind === 'opp' || it.kind === 'job' ? I.tt('Мерзім: ', 'Дедлайн: ') + it.title : it.title;
  return { title, date: it.date, time: it.time, details };
}

/** «Google Calendar-ға қосу» + «.ics жүктеу» for one item */
function ExportButtons({ it }: { it: CalItem }) {
  const { tt, I, lang, toast } = useApp();
  const ex = exportOf(it, I);
  const ics = () => {
    saveFile(buildOneIcs({ key: it.key, date: ex.date, time: ex.time, summary: ex.title, description: ex.details }, { now: new Date(), lang }),
      `studenthub-${it.date}.ics`);
    toast(tt('Файл жүктелді — оны күнтізбемен ашыңыз', 'Файл скачан — откройте его календарём'));
  };
  return (
    <div className="btn-row">
      <a className="btn secondary" href={googleCalendarUrl(ex)} target="_blank" rel="noopener"><Icon n="ext" s={18} /> {tt('Google Calendar-ға қосу', 'Добавить в Google Calendar')}</a>
      <button className="btn ghost" type="button" onClick={ics}><Icon n="calendar" s={18} /> {tt('.ics жүктеу', 'Скачать .ics')}</button>
    </div>
  );
}

function ItemSheet({ it }: { it: CalItem }) {
  const { tt, I, go, openSheet } = useApp();
  return (
    <>
      <span className="eyebrow">{it.kind === 'own' ? tt('Менің дедлайным', 'Мой дедлайн') : it.sub}</span>
      <h2>{it.title}</h2>
      <p className="muted">{dayLabel(it.date, I, true)}{it.time ? `, ${it.time}` : ''} · {relLabel(it.date, I)}</p>
      {it.kind === 'own' && it.own?.note ? <p className="cal-note">{it.own.note}</p> : null}
      <ExportButtons it={it} />
      {it.go ? <button className="btn" type="button" onClick={() => go(it.go!)}>{tt('Толығырақ', 'Подробнее')}</button> : null}
      {it.own ? (
        <div className="btn-row">
          <button className="btn ghost" type="button" onClick={() => openSheet(<DeadlineSheet edit={it.own} />)}><Icon n="edit" s={18} /> {tt('Өзгерту', 'Изменить')}</button>
          <DeleteButton d={it.own} />
        </div>
      ) : null}
    </>
  );
}

function DeleteButton({ d }: { d: OwnDeadline }) {
  const { tt, update, toast, openSheet } = useApp();
  return (
    <button className="btn danger" type="button" onClick={() => openSheet(
      <ConfirmSheet msg={tt(`«${d.title}» дедлайнын жою керек пе?`, `Удалить дедлайн «${d.title}»?`)} yes={() => {
        update(s => { s.deadlines = (s.deadlines ?? []).filter(x => x.id !== d.id); });
        toast(tt('Дедлайн жойылды', 'Дедлайн удалён'));
      }} />,
    )}><Icon n="trash" s={18} /> {tt('Жою', 'Удалить')}</button>
  );
}

/** «Дедлайн қосу» / edit form */
export function DeadlineSheet({ edit, date }: { edit?: OwnDeadline; date?: Ymd }) {
  const { tt, update, toast, closeSheet, setUi } = useApp();
  return (
    <>
      <h2>{edit ? tt('Дедлайнды өзгерту', 'Изменить дедлайн') : tt('Дедлайн қосу', 'Добавить дедлайн')}</h2>
      <form className="form" id="deadlineForm" onSubmit={e => {
        e.preventDefault();
        const f = e.currentTarget;
        const val = (id: string) => (f.querySelector<HTMLInputElement | HTMLTextAreaElement>('#' + id)?.value || '').trim();
        const title = val('dlTitle'), d = val('dlDate'), time = val('dlTime'), note = val('dlNote');
        if (!title || !isYmd(d)) return;
        const rec: OwnDeadline = { id: edit?.id ?? 'dl' + Date.now().toString(36), title, date: d, time: isHm(time) ? time : '', note, created: edit?.created ?? Date.now() };
        update(s => {
          const list = (s.deadlines ?? []).filter(x => x.id !== rec.id);
          list.push(rec);
          s.deadlines = list;
        });
        setUi({ calSel: d, calMonth: '' });
        toast(edit ? tt('Сақталды', 'Сохранено') : tt('Дедлайн қосылды', 'Дедлайн добавлен'));
        closeSheet();
      }}>
        <div className="field"><label htmlFor="dlTitle">{tt('Атауы', 'Название')}</label>
          <input className="input" id="dlTitle" required maxLength={200} autoComplete="off" defaultValue={edit?.title ?? ''}
            placeholder={tt('Мысалы: курстық жұмысты тапсыру', 'Например: сдать курсовую')} /></div>
        <div className="cal-form-row">
          <div className="field"><label htmlFor="dlDate">{tt('Күні', 'Дата')}</label>
            <input className="input" id="dlDate" type="date" required defaultValue={edit?.date ?? date ?? todayYmd()} /></div>
          <div className="field"><label htmlFor="dlTime">{tt('Уақыты (міндетті емес)', 'Время (необязательно)')}</label>
            <input className="input" id="dlTime" type="time" defaultValue={edit?.time ?? ''} /></div>
        </div>
        <div className="field"><label htmlFor="dlNote">{tt('Ескертпе (міндетті емес)', 'Заметка (необязательно)')}</label>
          <textarea className="input" id="dlNote" rows={3} maxLength={2000} defaultValue={edit?.note ?? ''} /></div>
        <button className="btn" type="submit">{edit ? tt('Сақтау', 'Сохранить') : tt('Қосу', 'Добавить')}</button>
        <p className="tiny muted">{tt('Дедлайн тек осы құрылғыда сақталады.', 'Дедлайн хранится только на этом устройстве.')}</p>
      </form>
    </>
  );
}

function ItemRow({ it }: { it: CalItem }) {
  const { tt, go, openSheet } = useApp();
  const open = () => (it.go ? go(it.go) : openSheet(<ItemSheet it={it} />));
  return (
    <div className="sr cal-item" data-kind={it.kind}>
      <button className="sr-main" type="button" onClick={open}>
        <span className={`cal-ico cal-k-${it.kind}`} aria-hidden="true"><Icon n={KIND_ICON[it.kind]} s={20} /></span>
        <span className="row-main">
          <span className="row-title">{it.title}</span>
          <span className="row-sub">{it.time ? <b className="num">{it.time} </b> : null}{it.sub}{it.demo ? <> <DemoBadge show /></> : null}</span>
          {it.saved && it.kind !== 'own' ? <span className="badges"><span className="badge b-sun"><Icon n="heart" s={12} /> {tt('Сақталған', 'Сохранено')}</span></span> : null}
        </span>
      </button>
      <span className="sr-end">
        <button className="icon-btn" type="button" onClick={() => openSheet(<ItemSheet it={it} />)}
          aria-label={(it.own ? tt('Өзгерту немесе күнтізбеге қосу: ', 'Изменить или добавить в календарь: ') : tt('Күнтізбеге қосу: ', 'Добавить в календарь: ')) + it.title}>
          <Icon n={it.own ? 'edit' : 'plus'} s={20} />
        </button>
      </span>
    </div>
  );
}

function DayList({ d, items, heading }: { d: Ymd; items: CalItem[]; heading?: boolean }) {
  const { tt, I } = useApp();
  const title = <>{dayLabel(d, I)}{d === todayYmd() ? <span className="badge b-sky cal-today-badge">{tt('Бүгін', 'Сегодня')}</span> : null}</>;
  return (
    <section className="section cal-day" aria-label={dayLabel(d, I, true)}>
      {heading ? <h3 className="cal-day-h">{title}</h3> : <SectionHead title={title} />}
      {items.length
        ? <div className="group">{items.map(it => <ItemRow key={it.key} it={it} />)}</div>
        : <p className="note cal-none">{tt('Бұл күні жазба жоқ.', 'На этот день записей нет.')}</p>}
    </section>
  );
}

export function CalendarView() {
  const { S, I, tt, lang, ui, setUi, openSheet, content } = useApp();
  const today = todayYmd();
  const sel = isYmd(ui.calSel) ? ui.calSel : today;
  const month = /^\d{4}-\d{2}$/.test(ui.calMonth) ? { y: +ui.calMonth.slice(0, 4), m: +ui.calMonth.slice(5, 7) - 1 } : monthOf(sel);
  const all = calItems(S, I, content).filter(i => !ui.calSaved || i.saved);
  const days = byDay(all);
  const gridRef = useRef<HTMLDivElement>(null);
  const wantFocus = useRef(false);

  useEffect(() => {
    if (!wantFocus.current) return;
    wantFocus.current = false;
    gridRef.current?.querySelector<HTMLButtonElement>(`button[data-ymd="${sel}"]`)?.focus();
  }, [sel, ui.calMonth]);

  const pick = (d: Ymd, focus = false) => {
    wantFocus.current = focus;
    setUi({ calSel: d, calMonth: '' });
  };
  const moveMonth = (n: number) => setUi({ calMonth: monthKey(shiftMonth(month, n)) });
  const goToday = () => setUi({ calSel: today, calMonth: '' });

  const onGridKey = (e: KeyboardEvent<HTMLDivElement>) => {
    const step: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 };
    let next: Ymd | null = null;
    if (e.key in step) next = addDays(sel, step[e.key]);
    else if (e.key === 'Home') next = addDays(sel, -weekdayMon(sel));
    else if (e.key === 'End') next = addDays(sel, 6 - weekdayMon(sel));
    else if (e.key === 'PageUp' || e.key === 'PageDown') {
      const m = shiftMonth(monthOf(sel), e.key === 'PageUp' ? -1 : 1);
      const last = +addDays(`${monthKey(shiftMonth(m, 1))}-01`, -1).slice(8, 10);
      next = `${monthKey(m)}-${String(Math.min(+sel.slice(8, 10), last)).padStart(2, '0')}`;
    }
    if (!next) return;
    e.preventDefault();
    pick(next, true);
  };

  const weeks = monthGrid(month);
  const inShown = sel.slice(0, 7) === monthKey(month);
  // the roving tab stop: the selected day when it is on this page, else the 1st of the month
  const tabStop = inShown ? sel : `${monthKey(month)}-01`;
  const monthTitle = `${MONTHS[lang][month.m]} ${month.y}`;
  const weekStart = addDays(sel, -weekdayMon(sel));
  const weekDays = Array.from({ length: 7 }, (_, i) => addDays(weekStart, i));
  const weekTitle = (() => {
    const a = weekDays[0], b = weekDays[6];
    const da = new Date(+a.slice(0, 4), +a.slice(5, 7) - 1, +a.slice(8, 10)), db = new Date(+b.slice(0, 4), +b.slice(5, 7) - 1, +b.slice(8, 10));
    return a.slice(0, 7) === b.slice(0, 7) ? `${da.getDate()}–${I.fDate(db)}` : `${I.fDate(da)} – ${I.fDate(db)}`;
  })();
  const week = ui.calView === 'week';
  const countTxt = (n: number) => (n ? tt(`${n} жазба`, `${n} ${n === 1 ? 'запись' : n < 5 ? 'записи' : 'записей'}`) : tt('жазба жоқ', 'нет записей'));

  return (
    <>
      <div className="cal-top">
        <div className="seg" role="group" aria-label={tt('Көрініс', 'Вид')}>
          <button type="button" aria-pressed={!week} onClick={() => setUi({ calView: 'month' })}>{tt('Ай', 'Месяц')}</button>
          <button type="button" aria-pressed={week} onClick={() => setUi({ calView: 'week' })}>{tt('Апта', 'Неделя')}</button>
        </div>
        <button className="btn cal-add" type="button" onClick={() => openSheet(<DeadlineSheet date={sel < today ? today : sel} />)}>
          <Icon n="plus" s={18} /> {tt('Дедлайн қосу', 'Добавить дедлайн')}
        </button>
      </div>

      <div className="cal-nav">
        <button className="icon-btn" type="button" onClick={() => (week ? pick(addDays(sel, -7)) : moveMonth(-1))}
          aria-label={week ? tt('Алдыңғы апта', 'Предыдущая неделя') : tt('Алдыңғы ай', 'Предыдущий месяц')}><Icon n="back" s={20} /></button>
        <h2 className="cal-title" id="calTitle" aria-live="polite">{week ? weekTitle : monthTitle}</h2>
        <button className="icon-btn" type="button" onClick={() => (week ? pick(addDays(sel, 7)) : moveMonth(1))}
          aria-label={week ? tt('Келесі апта', 'Следующая неделя') : tt('Келесі ай', 'Следующий месяц')}><Icon n="chev" s={20} /></button>
        <button className="chip" type="button" onClick={goToday}>{tt('Бүгін', 'Сегодня')}</button>
      </div>

      <div className="chips">
        <button className="chip" type="button" aria-pressed={ui.calSaved} onClick={() => setUi({ calSaved: !ui.calSaved })}>
          <Icon n="heart" s={16} /> {tt('Сақталғандар мен менікі', 'Сохранённое и моё')}
        </button>
      </div>

      {week ? (
        <div className="cal-week">
          {weekDays.map(d => <DayList key={d} d={d} items={days.get(d) ?? []} heading />)}
        </div>
      ) : (
        <>
          <div className="cal-grid" role="grid" aria-labelledby="calTitle" ref={gridRef} onKeyDown={onGridKey}>
            <div className="cal-row" role="row">
              {WEEK_SHORT[lang].map((w, i) => (
                <span key={w} className="cal-wd" role="columnheader" aria-label={I.wd[(i + 1) % 7]}>{w}</span>
              ))}
            </div>
            {weeks.map(w => (
              <div className="cal-row" role="row" key={w[0].ymd}>
                {w.map(d => {
                  const its = days.get(d.ymd) ?? [];
                  const kinds = [...new Set(its.map(i => i.kind))].slice(0, 3);
                  return (
                    <span role="gridcell" key={d.ymd} className="cal-cell">
                      <button type="button" data-ymd={d.ymd} className={`cal-day-btn${d.inMonth ? '' : ' out'}${its.length ? ' has' : ''}`}
                        tabIndex={d.ymd === tabStop ? 0 : -1}
                        aria-current={d.ymd === today ? 'date' : undefined} aria-pressed={d.ymd === sel}
                        aria-label={`${dayLabel(d.ymd, I, true)}, ${countTxt(its.length)}`}
                        onClick={() => pick(d.ymd)}>
                        <span className="cal-num num">{d.day}</span>
                        <span className="cal-dots" aria-hidden="true">
                          {kinds.map(k => <i key={k} className={`cal-dot cal-k-${k}`} />)}
                        </span>
                      </button>
                    </span>
                  );
                })}
              </div>
            ))}
          </div>
          <p className="cal-legend tiny muted" aria-hidden="true">
            <span><i className="cal-dot cal-k-opp" /> {tt('Мүмкіндік', 'Возможность')}</span>
            <span><i className="cal-dot cal-k-job" /> {tt('Вакансия', 'Вакансия')}</span>
            <span><i className="cal-dot cal-k-event" /> {tt('Іс-шара', 'Мероприятие')}</span>
            <span><i className="cal-dot cal-k-own" /> {tt('Менікі', 'Моё')}</span>
          </p>
          <DayList d={sel} items={days.get(sel) ?? []} />
        </>
      )}
      {all.length === 0 && !week ? (
        <Empty icon="calendar">{ui.calSaved
          ? tt('Сақталған мерзімдер мен өз дедлайндарыңыз әзірге жоқ.', 'Сохранённых сроков и своих дедлайнов пока нет.')
          : tt('Әзірге мерзімдер жоқ. Өз дедлайныңызды қосыңыз.', 'Сроков пока нет. Добавьте свой дедлайн.')}</Empty>
      ) : null}
    </>
  );
}

/** home: «Жақын дедлайндар», the next three, with a link to the calendar */
export function UpcomingDeadlines() {
  const { S, I, tt, go, setUi, content } = useApp();
  const list = nextDeadlines(S, I, content, 3);
  if (!list.length) return null;
  return (
    <section className="section" id="homeDeadlines">
      <SectionHead title={tt('Жақын дедлайндар', 'Ближайшие дедлайны')}
        more={<button className="link-btn" type="button" onClick={() => go('calendar')}>{tt('Күнтізбе', 'Календарь')}</button>} />
      <div className="group">
        {list.map(it => (
          <button key={it.key} className="row" type="button" onClick={() => { if (it.go) { go(it.go); return; } setUi({ calSel: it.date, calMonth: '', calView: 'month' }); go('calendar'); }}>
            <span className={`cal-ico cal-k-${it.kind}`} aria-hidden="true"><Icon n={KIND_ICON[it.kind]} s={20} /></span>
            <span className="row-main"><span className="row-title">{it.title}</span><span className="row-sub">{dayLabel(it.date, I)}{it.time ? `, ${it.time}` : ''}</span></span>
            <span className={`tag ${daysBetween(todayYmd(), it.date) <= 3 ? 'tag-warn' : 'tag-new'}`}>{relLabel(it.date, I)}</span>
          </button>
        ))}
      </div>
    </section>
  );
}
