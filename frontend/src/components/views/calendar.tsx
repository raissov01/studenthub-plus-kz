'use client';
/* «Күнтізбе» (/calendar): a Google-Calendar-like month grid (Monday first, Almaty days), a week list and a
   «Тапсырмалар» to-do list. It shows application deadlines (opportunities, vacancies), events, exam plans, the
   student's own class timetable (lib/timetable.ts) as a «Сабақтар» layer, the student's own deadlines and the items pinned with
   «Күнтізбеге қосу» (PinButton, used on detail pages and list rows). Every kind has its own colour, icon and label;
   own deadlines and pins carry a priority. The student's own events («Іс-шара қосу») carry a colour of their choice and
   show as titled chips inside the month cells. Date logic: lib/calendar.ts; grouping and kind colours: lib/caltasks.ts;
   year grid, cell packing, agenda order and event colours: lib/calview.ts. */
import { useEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from 'react';
import { useApp } from '@/components/app';
import { ConfirmSheet, Empty, Icon, SectionHead } from '@/components/ui';
import {
  addDays, almatyHm, almatyYmd, byDay, daysBetween, googleCalendarUrl, isHm, isPriority, isYmd, monthGrid, monthKey, monthOf, MONTHS,
  PRIORITIES, shiftMonth, WEEK_SHORT, weekdayMon, weekdaySun, type CalPin, type OwnDeadline, type PinKind, type Priority, type Ymd,
} from '@/lib/calendar';
import {
  CAL_KINDS, GROUP_LABEL, groupTasks, KIND_STYLE, kindColor, PRIO_LABEL, prioClass, type CalKind,
} from '@/lib/caltasks';
import {
  DEFAULT_EV_COLOR, EV_COLORS, evColorName, evColorVars, isEvColor, monthCounts, packCell, sortAgenda, timeRange, yearGrid, yearOptions,
} from '@/lib/calview';
import { typeLabel } from '@/lib/domain';
import { buildOneIcs } from '@/lib/ics';
import { pathOf } from '@/lib/routes';
import { type I18n } from '@/lib/format';
import type { AppState, Content, TimetableEntry } from '@/lib/types';

export type { CalKind } from '@/lib/caltasks';
export interface CalItem {
  key: string;
  kind: CalKind;
  date: Ymd;
  time: string;
  /** a class's or own event's end time */
  end?: string;
  /** an own event's place */
  place?: string;
  /** an own event's colour 1–8 (lib/calview.ts EV_COLORS); absent for everything else */
  color?: number;
  title: string;
  sub: string;
  go?: string;
  saved: boolean;
  own?: OwnDeadline;
  pin?: CalPin;
  /** own deadlines and pinned items only */
  prio?: Priority;
  /** when it was checked off in the to-do list; 0 = open */
  done: number;
}

export const todayYmd = () => almatyYmd(Date.now());
const kindLabel = (k: CalKind, I: I18n) => I.tt(KIND_STYLE[k].kk, KIND_STYLE[k].ru);
const kindStyle = (k: CalKind) => ({ '--k': kindColor(k) }) as CSSProperties;
/** an item's colour variables: its kind's colour, or an own event's chosen colour with its chip background and ink */
function itemStyle(it: Pick<CalItem, 'kind' | 'color'>): CSSProperties {
  if (!it.color) return kindStyle(it.kind);
  const c = evColorVars(it.color);
  return { '--k': c.solid, '--kbg': c.soft, '--kink': c.ink } as CSSProperties;
}
const isOwnEvent = (it: CalItem) => !!it.own?.color;
/** «Менің іс-шарам» for an own event, else the kind's name */
const itemKindLabel = (it: CalItem, I: I18n) => (isOwnEvent(it) ? I.tt('Менің іс-шарам', 'Моё событие') : kindLabel(it.kind, I));

function ownItem(d: OwnDeadline, I: I18n): CalItem {
  const ev = d.color > 0;
  return {
    key: 'own:' + d.id, kind: 'own', date: d.date, time: d.time, title: d.title,
    sub: ev ? d.place || d.note : d.note || I.tt('Менің дедлайным', 'Мой дедлайн'),
    saved: true, own: d, prio: ev ? undefined : d.prio, done: d.done,
    ...(ev ? { color: d.color, end: d.end || undefined, place: d.place } : {}),
  };
}

/** the student's own weekly timetable on the days from..to (each class on its weekday) */
export function classItems(list: TimetableEntry[], from: Ymd, to: Ymd): CalItem[] {
  const out: CalItem[] = [];
  if (!list.length) return out;
  for (let d = from; d <= to; d = addDays(d, 1)) {
    const wd = weekdayMon(d);
    for (const c of list) {
      if (c.wd !== wd) continue;
      out.push({
        key: `class:${d}:${c.id}`, kind: 'class', date: d, time: c.t, end: c.end, title: c.name,
        sub: [c.kind, c.room, `${c.t}–${c.end}`].filter(Boolean).join(' · '), saved: true, done: 0,
      });
    }
  }
  return out;
}

/** everything the calendar shows, from the content feed and the device state (classes come separately: classItems) */
export function calItems(S: AppState, I: I18n, C: Content): CalItem[] {
  const { tt, L } = I;
  const today = todayYmd();
  const pins = new Map((S.calendarPins ?? []).map(p => [p.ref, p]));
  const out: CalItem[] = [];
  const add = (it: Omit<CalItem, 'pin' | 'prio' | 'done'>) => {
    const p = pins.get(it.key);
    pins.delete(it.key);
    out.push(p ? { ...it, saved: true, pin: p, prio: p.prio, done: p.done } : { ...it, done: 0 });
  };
  for (const o of C.opportunities) {
    if (!o.deadline || !isYmd(o.deadline.slice(0, 10))) continue;
    add({
      key: 'opp:' + o.id, kind: 'opp', date: o.deadline.slice(0, 10), time: '', title: L(o.title),
      sub: `${typeLabel(o.type, I)} · ${tt('өтінім мерзімі', 'приём заявок до')}`, go: 'opp:' + o.id, saved: !!S.saved[o.id],
    });
  }
  for (const j of C.jobs) {
    if (!j.deadline || !isYmd(j.deadline.slice(0, 10)) || j.deadline.slice(0, 10) < today) continue;
    add({
      key: 'job:' + j.id, kind: 'job', date: j.deadline.slice(0, 10), time: '', title: L(j.title),
      sub: `${L(j.org)} · ${tt('өтінім мерзімі', 'отклик до')}`, go: 'job:' + j.id, saved: !!S.fav[j.id],
    });
  }
  for (const e of C.events ?? []) {
    if (!e.starts_at || Number.isNaN(Date.parse(e.starts_at))) continue;
    add({
      key: 'event:' + e.id, kind: 'event', date: almatyYmd(e.starts_at), time: almatyHm(e.starts_at),
      title: (S.lang === 'ru' ? e.title_ru || e.title_kk : e.title_kk) || '', sub: [tt('Іс-шара', 'Мероприятие'), e.place_text].filter(Boolean).join(' · '),
      go: 'event:' + e.id, saved: false,
    });
  }
  // pinned items that are not in the feed (a closed vacancy, a finished event): their saved copy
  for (const p of pins.values()) {
    out.push({
      key: p.ref, kind: p.kind, date: p.date, time: p.time, title: p.title[S.lang === 'ru' ? 'ru' : 'kk'] || p.title.kk,
      sub: kindLabel(p.kind, I), go: p.ref, saved: true, pin: p, prio: p.prio, done: p.done,
    });
  }
  for (const pl of S.plans ?? []) {
    if (!pl.exam || Number.isNaN(Date.parse(pl.exam))) continue;
    out.push({
      key: 'exam:' + pl.id, kind: 'exam', date: almatyYmd(pl.exam), time: '', title: pl.subject || tt('Емтихан', 'Экзамен'),
      sub: tt('Емтихан · дайындық жоспары', 'Экзамен · план подготовки'), go: 'plan:' + pl.id, saved: true, done: 0,
    });
  }
  for (const d of S.deadlines ?? []) if (d.date) out.push(ownItem(d, I));
  return out;
}

/** the next open deadlines from today (not feed events or exams, not done) and the student's own timed events of today
    and tomorrow: the home card */
export function nextDeadlines(S: AppState, I: I18n, C: Content, n: number): CalItem[] {
  const today = todayYmd(), tomorrow = addDays(today, 1);
  return calItems(S, I, C).filter(i => i.kind !== 'event' && i.kind !== 'exam' && !i.done && i.date >= today
    && (!isOwnEvent(i) || (!!i.time && i.date <= tomorrow)))
    .sort((a, b) => a.date.localeCompare(b.date) || (a.time || '').localeCompare(b.time || '')).slice(0, n);
}

/** the to-do list: own deadlines (with undated tasks) and pinned items; own events stay on the calendar only */
export function taskItems(S: AppState, I: I18n, C: Content): CalItem[] {
  return [
    ...calItems(S, I, C).filter(i => i.pin || (i.own && !i.own.color)),
    ...(S.deadlines ?? []).filter(d => !d.date).map(d => ownItem(d, I)),
  ];
}

/* ---------- pins ---------- */
export interface PinTarget { ref: string; kind: PinKind; title: { kk: string; ru: string }; date: Ymd; time: string }


/** what «Күнтізбеге қосу» would pin for a view id ('opp:x', 'job:x', 'event:12'); null = no date to pin */
export function pinTarget(ref: string, C: Content): PinTarget | null {
  const i = ref.indexOf(':');
  const kind = ref.slice(0, i), id = ref.slice(i + 1);
  if (kind === 'opp' || kind === 'job') {
    const x = kind === 'opp' ? C.opportunities.find(o => o.id === id) : C.jobs.find(j => j.id === id);
    const d = x?.deadline?.slice(0, 10);
    if (!x || !isYmd(d)) return null;
    return { ref, kind, title: { kk: x.title.kk, ru: x.title.ru || x.title.kk }, date: d, time: '' };
  }
  if (kind === 'event') {
    const e = (C.events ?? []).find(x => String(x.id) === id);
    if (e && !Number.isNaN(Date.parse(e.starts_at))) {
      return { ref, kind, title: { kk: e.title_kk, ru: e.title_ru || e.title_kk }, date: almatyYmd(e.starts_at), time: almatyHm(e.starts_at) };
    }
  }
  return null;
}

/** a vacancy's or opportunity's own data as a pin target (an employer vacancy may not be in the content feed) */
export function deadlinePinTarget(kind: 'opp' | 'job', x: { id: string; title: { kk: string; ru: string }; deadline: string | null }): PinTarget | null {
  const d = x.deadline?.slice(0, 10);
  return isYmd(d) ? { ref: `${kind}:${x.id}`, kind, title: { kk: x.title.kk, ru: x.title.ru || x.title.kk }, date: d, time: '' } : null;
}

/** an event page's own data as a pin target (the event may not be in the content feed) */
export function eventPinTarget(id: string | number, title: { kk: string; ru?: string | null }, startsAt: string): PinTarget | null {
  if (Number.isNaN(Date.parse(startsAt))) return null;
  return { ref: 'event:' + id, kind: 'event', title: { kk: title.kk, ru: title.ru || title.kk }, date: almatyYmd(startsAt), time: almatyHm(startsAt) };
}

function useOpenDay() {
  const { setUi, go } = useApp();
  return (d: Ymd) => { setUi({ calSel: d || todayYmd(), calMonth: '', calView: d ? 'month' : 'tasks' }); go('calendar'); };
}

/** «Күнтізбеге қосу» ⇄ «Күнтізбеде»: pins an item into the student's own calendar; `full` = the detail-page button,
    otherwise an icon button for list rows. Renders nothing when the item has no date. */
export function PinButton({ item, target, full }: { item: string; target?: PinTarget | null; full?: boolean }) {
  const { S, content, tt, lang, update, toast } = useApp();
  const openDay = useOpenDay();
  const t = target === undefined ? pinTarget(item, content) : target;
  if (!t) return null;
  const pinned = (S.calendarPins ?? []).some(p => p.ref === t.ref);
  const title = t.title[lang] || t.title.kk;
  const flip = () => {
    if (pinned) {
      update(s => { s.calendarPins = (s.calendarPins ?? []).filter(p => p.ref !== t.ref); });
      toast(tt('Күнтізбеден алынды', 'Убрано из календаря'));
      return;
    }
    update(s => { s.calendarPins = [...(s.calendarPins ?? []).filter(p => p.ref !== t.ref), { ...t, prio: 'mid', done: 0, created: Date.now() }]; });
    toast(tt('Күнтізбеге қосылды', 'Добавлено в календарь'), { label: tt('Ашу', 'Открыть'), run: () => openDay(t.date) });
  };
  if (full) {
    return (
      <button className={`btn ${pinned ? 'secondary' : ''} cal-pin`} type="button" aria-pressed={pinned} onClick={flip}>
        <Icon n={pinned ? 'check' : 'calendar'} s={18} /> {pinned ? tt('Күнтізбеде', 'В календаре') : tt('Күнтізбеге қосу', 'В календарь')}
      </button>
    );
  }
  return (
    <button className="icon-btn cal-pin-ico" type="button" aria-pressed={pinned} onClick={flip}
      aria-label={tt('Күнтізбеге қосу: ', 'Добавить в календарь: ') + title} title={pinned ? tt('Күнтізбеде', 'В календаре') : tt('Күнтізбеге қосу', 'В календарь')}>
      <Icon n={pinned ? 'check' : 'calendar'} s={20} />
    </button>
  );
}

/** detail pages: «Күнтізбеге қосу» with the Google Calendar / .ics export next to it */
export function CalendarActions({ item, target }: { item: string; target?: PinTarget | null }) {
  const { content, I, lang, tt } = useApp();
  const t = target === undefined ? pinTarget(item, content) : target;
  if (!t) return null;
  const title = t.title[lang] || t.title.kk;
  const deadline = t.kind !== 'event';
  return (
    <section className="cal-actions" aria-label={tt('Күнтізбе', 'Календарь')}>
      <PinButton item={item} target={t} full />
      <ExportButtons ex={exportOf({ key: t.ref, title: deadline ? I.tt('Мерзім: ', 'Дедлайн: ') + title : title, date: t.date, time: t.time, go: t.ref, note: '' })} />
    </section>
  );
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

interface Exp { key: string; title: string; date: Ymd; time: string; end: string; place: string; details: string }
function exportOf(x: { key: string; title: string; date: Ymd; time: string; end?: string; place?: string; go?: string; note: string }): Exp {
  const link = x.go && typeof window !== 'undefined' ? window.location.origin + pathOf(x.go) : '';
  return {
    key: x.key, title: x.title, date: x.date, time: x.time, end: x.end ?? '', place: x.place ?? '',
    details: [x.note, link, 'Student Hub'].filter(Boolean).join('\n'),
  };
}
function itemExport(it: CalItem, I: I18n): Exp {
  const title = it.kind === 'opp' || it.kind === 'job' ? I.tt('Мерзім: ', 'Дедлайн: ') + it.title : it.title;
  return exportOf({
    key: it.key, title, date: it.date, time: it.time, end: it.own ? it.own.end : '', place: it.own?.place ?? '', go: it.go,
    note: it.kind === 'own' ? it.own?.note ?? '' : it.sub,
  });
}
const minutesOf = (hm: string) => +hm.slice(0, 2) * 60 + +hm.slice(3, 5);

/** «Google Calendar-ға қосу» + «.ics жүктеу» for one item */
function ExportButtons({ ex }: { ex: Exp }) {
  const { tt, lang, toast } = useApp();
  const ics = () => {
    const minutes = ex.time && ex.end > ex.time ? minutesOf(ex.end) - minutesOf(ex.time) : undefined;
    saveFile(buildOneIcs({ key: ex.key, date: ex.date, time: ex.time, minutes, summary: ex.title, description: ex.details, location: ex.place }, { now: new Date(), lang }),
      `studenthub-${ex.date}.ics`);
    toast(tt('Файл жүктелді — оны күнтізбемен ашыңыз', 'Файл скачан — откройте его календарём'));
  };
  return (
    <div className="btn-row">
      <a className="btn secondary" href={googleCalendarUrl({ ...ex, location: ex.place })} target="_blank" rel="noopener"><Icon n="ext" s={18} /> {tt('Google Calendar-ға қосу', 'Добавить в Google Calendar')}</a>
      <button className="btn ghost" type="button" onClick={ics}><Icon n="calendar" s={18} /> {tt('.ics жүктеу', 'Скачать .ics')}</button>
    </div>
  );
}

/* ---------- priority and done ---------- */
/** «Маңыздылығы»: three radio buttons; `onPick` = change at once (a pin's sheet), else read from the form by `name` */
function PriorityField({ name, value, onPick }: { name: string; value: Priority; onPick?: (p: Priority) => void }) {
  const { tt } = useApp();
  return (
    <fieldset className="prio-pick">
      <legend>{tt('Маңыздылығы', 'Важность')}</legend>
      <div className="prio-opts">
        {PRIORITIES.map(p => (
          <label key={p} className={`prio-opt prio-opt-${p}`}>
            <input type="radio" name={name} value={p}
              {...(onPick ? { checked: value === p, onChange: () => onPick(p) } : { defaultChecked: value === p })} />
            <span>{tt(PRIO_LABEL[p].kk, PRIO_LABEL[p].ru)}</span>
          </label>
        ))}
      </div>
    </fieldset>
  );
}

/** the «Жоғары» / «Төмен» tag (nothing for «Орта») */
function PrioTag({ p }: { p?: Priority }) {
  const { tt } = useApp();
  if (p !== 'high' && p !== 'low') return null;
  return <span className={`prio-tag prio-tag-${p}`}>{p === 'high' ? <Icon n="bolt" s={12} /> : null}{tt(PRIO_LABEL[p].kk, PRIO_LABEL[p].ru)}</span>;
}

/** checks an item off (or back on); an unpinned feed item is pinned as done so it leaves the home card */
export function setItemDone(s: AppState, it: CalItem, on: boolean, target?: PinTarget | null) {
  const at = on ? Date.now() : 0;
  if (it.own) {
    const d = (s.deadlines ?? []).find(x => x.id === it.own!.id);
    if (d) d.done = at;
    return;
  }
  const pins = (s.calendarPins ??= []);
  const p = pins.find(x => x.ref === it.key);
  if (p) { p.done = at; return; }
  if ((it.kind === 'opp' || it.kind === 'job' || it.kind === 'event') && on) {
    pins.push({ ref: it.key, kind: it.kind, title: target?.title ?? { kk: it.title, ru: it.title }, date: it.date, time: it.time, prio: 'mid', done: at, created: Date.now() });
  }
}

function useDoneToggle() {
  const { tt, update, toast, content } = useApp();
  return (it: CalItem, on: boolean) => {
    const target = it.pin || it.own ? null : pinTarget(it.key, content);
    update(s => setItemDone(s, it, on, target));
    if (on) toast(tt('Орындалды', 'Выполнено'), { label: tt('Қайтару', 'Отменить'), run: () => update(s => setItemDone(s, it, false)) });
    else toast(tt('Қайта ашылды', 'Снова в работе'));
  };
}

/* ---------- sheets ---------- */
function ItemSheet({ it }: { it: CalItem }) {
  const { S, tt, I, go, openSheet, update, toast, closeSheet } = useApp();
  const pin = it.pin ? (S.calendarPins ?? []).find(p => p.ref === it.pin!.ref) : undefined;
  const ev = isOwnEvent(it);
  const range = timeRange(it.time, it.end);
  return (
    <>
      <span className={`eyebrow cal-eyebrow cal-k-${it.kind}`} style={itemStyle(it)}>
        {ev ? <i className="cal-sw-dot" aria-hidden="true" /> : <Icon n={KIND_STYLE[it.kind].icon} s={14} />}
        {' '}{ev ? `${itemKindLabel(it, I)} · ${evColorName(it.color!, I.lang)}` : it.kind === 'own' ? tt('Менің дедлайным', 'Мой дедлайн') : kindLabel(it.kind, I)}
      </span>
      <h2>{it.title}</h2>
      <p className="muted">{it.date ? <>{dayLabel(it.date, I, true)}{range ? `, ${range}` : ''} · {relLabel(it.date, I)}</> : tt('Мерзімі жоқ', 'Без срока')}</p>
      {it.kind === 'class' ? <p>{it.sub}</p> : null}
      {ev && it.place ? <p className="cal-place"><Icon n="pin" s={16} /> {it.place}</p> : null}
      {it.kind === 'own' && it.own?.note ? <p className="cal-note">{it.own.note}</p> : null}
      {pin ? (
        <PriorityField name="pinPrio" value={pin.prio} onPick={p => update(s => { const x = (s.calendarPins ?? []).find(y => y.ref === pin.ref); if (x) x.prio = p; })} />
      ) : null}
      {it.date && it.kind !== 'class' ? <ExportButtons ex={itemExport(it, I)} /> : null}
      {it.go ? <button className="btn" type="button" onClick={() => go(it.go!)}>{tt('Толығырақ', 'Подробнее')}</button> : null}
      {pin ? (
        <button className="btn ghost" type="button" onClick={() => {
          update(s => { s.calendarPins = (s.calendarPins ?? []).filter(p => p.ref !== pin.ref); });
          toast(tt('Күнтізбеден алынды', 'Убрано из календаря'));
          closeSheet();
        }}><Icon n="x" s={18} /> {tt('Күнтізбеден алу', 'Убрать из календаря')}</button>
      ) : null}
      {it.own ? (
        <div className="btn-row">
          <button className="btn ghost" type="button" onClick={() => openSheet(ev ? <EventSheet edit={it.own} /> : <DeadlineSheet edit={it.own} />)}>
            <Icon n="edit" s={18} /> {tt('Өзгерту', 'Изменить')}
          </button>
          <DeleteButton d={it.own} />
        </div>
      ) : null}
    </>
  );
}

function useDeleteOwn() {
  const { tt, update, toast, openSheet } = useApp();
  return (d: OwnDeadline) => openSheet(
    <ConfirmSheet msg={d.color
      ? tt(`«${d.title}» іс-шарасын жою керек пе?`, `Удалить событие «${d.title}»?`)
      : tt(`«${d.title}» дедлайнын жою керек пе?`, `Удалить дедлайн «${d.title}»?`)} yes={() => {
      update(s => { s.deadlines = (s.deadlines ?? []).filter(x => x.id !== d.id); });
      toast(d.color ? tt('Іс-шара жойылды', 'Событие удалено') : tt('Дедлайн жойылды', 'Дедлайн удалён'));
    }} />,
  );
}

function DeleteButton({ d }: { d: OwnDeadline }) {
  const { tt } = useApp();
  const del = useDeleteOwn();
  return <button className="btn danger" type="button" onClick={() => del(d)}><Icon n="trash" s={18} /> {tt('Жою', 'Удалить')}</button>;
}

/** «Дедлайн қосу» / edit form (own deadlines; the to-do list adds them too) */
export function DeadlineSheet({ edit, date }: { edit?: OwnDeadline; date?: Ymd }) {
  const { tt, update, toast, closeSheet, setUi } = useApp();
  const dateRequired = !edit || !!edit.date;
  return (
    <>
      <h2>{edit ? tt('Дедлайнды өзгерту', 'Изменить дедлайн') : tt('Дедлайн қосу', 'Добавить дедлайн')}</h2>
      <form className="form" id="deadlineForm" onSubmit={e => {
        e.preventDefault();
        const f = e.currentTarget;
        const val = (id: string) => (f.querySelector<HTMLInputElement | HTMLTextAreaElement>('#' + id)?.value || '').trim();
        const title = val('dlTitle'), d = val('dlDate'), time = val('dlTime'), note = val('dlNote');
        const pr = f.querySelector<HTMLInputElement>('input[name="dlPrio"]:checked')?.value;
        if (!title || (d ? !isYmd(d) : dateRequired)) return;
        const rec: OwnDeadline = {
          id: edit?.id ?? 'dl' + Date.now().toString(36), title, date: d, time: d && isHm(time) ? time : '', note, created: edit?.created ?? Date.now(),
          prio: isPriority(pr) ? pr : 'mid', done: edit?.done ?? 0, end: '', place: edit?.place ?? '', color: 0,
        };
        update(s => {
          const list = (s.deadlines ?? []).filter(x => x.id !== rec.id);
          list.push(rec);
          s.deadlines = list;
        });
        if (d) setUi({ calSel: d, calMonth: '' });
        toast(edit ? tt('Сақталды', 'Сохранено') : tt('Дедлайн қосылды', 'Дедлайн добавлен'));
        closeSheet();
      }}>
        <div className="field"><label htmlFor="dlTitle">{tt('Атауы', 'Название')}</label>
          <input className="input" id="dlTitle" required maxLength={200} autoComplete="off" defaultValue={edit?.title ?? ''}
            placeholder={tt('Мысалы: курстық жұмысты тапсыру', 'Например: сдать курсовую')} /></div>
        <div className="cal-form-row">
          <div className="field"><label htmlFor="dlDate">{dateRequired ? tt('Күні', 'Дата') : tt('Күні (міндетті емес)', 'Дата (необязательно)')}</label>
            <input className="input" id="dlDate" type="date" required={dateRequired} defaultValue={edit ? edit.date : date ?? todayYmd()} /></div>
          <div className="field"><label htmlFor="dlTime">{tt('Уақыты (міндетті емес)', 'Время (необязательно)')}</label>
            <input className="input" id="dlTime" type="time" defaultValue={edit?.time ?? ''} /></div>
        </div>
        <PriorityField name="dlPrio" value={edit?.prio ?? 'mid'} />
        <div className="field"><label htmlFor="dlNote">{tt('Ескертпе (міндетті емес)', 'Заметка (необязательно)')}</label>
          <textarea className="input" id="dlNote" rows={3} maxLength={2000} defaultValue={edit?.note ?? ''} /></div>
        <button className="btn" type="submit">{edit ? tt('Сақтау', 'Сохранить') : tt('Қосу', 'Добавить')}</button>
        <p className="tiny muted">{tt('Дедлайн тек осы құрылғыда сақталады.', 'Дедлайн хранится только на этом устройстве.')}</p>
      </form>
    </>
  );
}

/** «Іс-шара қосу» / edit form: an own event with a date, an optional time span, place and note, and a colour */
export function EventSheet({ edit, date }: { edit?: OwnDeadline; date?: Ymd }) {
  const { tt, lang, update, toast, closeSheet, setUi } = useApp();
  const [err, setErr] = useState('');
  return (
    <>
      <h2>{edit ? tt('Іс-шараны өзгерту', 'Изменить событие') : tt('Іс-шара қосу', 'Добавить событие')}</h2>
      <form className="form cal-ev-form" id="eventForm" onSubmit={e => {
        e.preventDefault();
        const f = e.currentTarget;
        const val = (id: string) => (f.querySelector<HTMLInputElement | HTMLTextAreaElement>('#' + id)?.value || '').trim();
        const title = val('evTitle'), d = val('evDate'), start = val('evStart'), end = val('evEnd'), place = val('evPlace'), note = val('evNote');
        const color = Number(f.querySelector<HTMLInputElement>('input[name="evColor"]:checked')?.value);
        if (!title || !isYmd(d)) return;
        const time = isHm(start) ? start : '';
        if (end && !time) { setErr(tt('Алдымен басталу уақытын көрсетіңіз.', 'Сначала укажите время начала.')); f.querySelector<HTMLInputElement>('#evStart')?.focus(); return; }
        if (end && isHm(end) && end <= time) { setErr(tt('Аяқталуы басталуынан кейін болуы керек.', 'Окончание должно быть позже начала.')); f.querySelector<HTMLInputElement>('#evEnd')?.focus(); return; }
        const rec: OwnDeadline = {
          id: edit?.id ?? 'ev' + Date.now().toString(36), title: title.slice(0, 200), date: d, time, end: time && isHm(end) ? end : '',
          place: place.slice(0, 200), note: note.slice(0, 2000), color: isEvColor(color) ? color : DEFAULT_EV_COLOR,
          created: edit?.created ?? Date.now(), prio: edit?.prio ?? 'mid', done: edit?.done ?? 0,
        };
        update(s => {
          const list = (s.deadlines ?? []).filter(x => x.id !== rec.id);
          list.push(rec);
          s.deadlines = list;
        });
        setUi({ calSel: d, calMonth: '', calView: 'month' });
        toast(edit ? tt('Сақталды', 'Сохранено') : tt('Іс-шара қосылды', 'Событие добавлено'));
        closeSheet();
      }}>
        <div className="field"><label htmlFor="evTitle">{tt('Атауы', 'Название')}</label>
          <input className="input" id="evTitle" required maxLength={200} autoComplete="off" defaultValue={edit?.title ?? ''}
            placeholder={tt('Мысалы: топпен кездесу', 'Например: встреча с группой')} /></div>
        <div className="field"><label htmlFor="evDate">{tt('Күні', 'Дата')}</label>
          <input className="input" id="evDate" type="date" required defaultValue={edit ? edit.date : date ?? todayYmd()} /></div>
        <div className="cal-form-row cal-time-row">
          <div className="field"><label htmlFor="evStart">{tt('Басталуы', 'Начало')} <span className="cal-opt">{tt('міндетті емес', 'необязательно')}</span></label>
            <input className="input" id="evStart" type="time" defaultValue={edit?.time ?? ''} onChange={() => setErr('')} /></div>
          <div className="field"><label htmlFor="evEnd">{tt('Аяқталуы', 'Окончание')} <span className="cal-opt">{tt('міндетті емес', 'необязательно')}</span></label>
            <input className="input" id="evEnd" type="time" defaultValue={edit?.end ?? ''} onChange={() => setErr('')}
              aria-invalid={err ? true : undefined} aria-describedby={err ? 'evTimeErr' : undefined} /></div>
        </div>
        {err ? <p className="field-err" id="evTimeErr" role="alert">{err}</p> : null}
        <fieldset className="ev-colors">
          <legend>{tt('Түсі', 'Цвет')}</legend>
          <div className="ev-swatches">
            {EV_COLORS.map(c => (
              <label key={c.n} className="ev-sw" style={{ '--sw': evColorVars(c.n).solid } as CSSProperties}>
                <input type="radio" name="evColor" value={c.n} defaultChecked={(edit?.color || DEFAULT_EV_COLOR) === c.n} aria-label={c[lang]} />
                <span className="ev-sw-dot" aria-hidden="true"><Icon n="check" s={16} /></span>
                <span className="ev-sw-name" aria-hidden="true">{c[lang]}</span>
              </label>
            ))}
          </div>
        </fieldset>
        <div className="field"><label htmlFor="evPlace">{tt('Орны', 'Место')} <span className="cal-opt">{tt('міндетті емес', 'необязательно')}</span></label>
          <input className="input" id="evPlace" maxLength={200} autoComplete="off" defaultValue={edit?.place ?? ''}
            placeholder={tt('Мысалы: 3-корпус, 214', 'Например: корпус 3, 214')} /></div>
        <div className="field"><label htmlFor="evNote">{tt('Ескертпе', 'Заметка')} <span className="cal-opt">{tt('міндетті емес', 'необязательно')}</span></label>
          <textarea className="input" id="evNote" rows={2} maxLength={2000} defaultValue={edit?.note ?? ''} /></div>
        <button className="btn" type="submit">{edit ? tt('Сақтау', 'Сохранить') : tt('Қосу', 'Добавить')}</button>
        <p className="tiny muted">{tt('Іс-шара тек осы құрылғыда сақталады.', 'Событие хранится только на этом устройстве.')}</p>
      </form>
    </>
  );
}

/* ---------- the day agenda (month and week) ---------- */
function AgendaRow({ it }: { it: CalItem }) {
  const { tt, I, go, openSheet, update, toast } = useApp();
  const del = useDeleteOwn();
  const open = () => (it.go ? go(it.go) : openSheet(<ItemSheet it={it} />));
  const ev = isOwnEvent(it);
  const sub = it.kind === 'class' ? it.sub.split(' · ').slice(0, 2).join(' · ') : it.sub;
  const unpin = () => {
    update(s => { s.calendarPins = (s.calendarPins ?? []).filter(p => p.ref !== it.key); });
    toast(tt('Күнтізбеден алынды', 'Убрано из календаря'));
  };
  const savedBadge = !it.pin && it.saved && it.kind !== 'own' && it.kind !== 'class' && it.kind !== 'exam';
  return (
    <li className={`cal-item cal-k-${it.kind} ${prioClass(it.prio)}${it.done ? ' is-done' : ''}${ev ? ' is-own-ev' : ''}`} data-kind={it.kind} style={itemStyle(it)}>
      <span className="cal-time num">
        {it.time ? <><b>{it.time}</b>{it.end ? <span>{it.end}</span> : null}</> : <span className="cal-allday">{tt('Күні бойы', 'Весь день')}</span>}
      </span>
      <button className="cal-tl-main" type="button" onClick={open}>
        <span className="row-title">{it.title}</span>
        {sub ? <span className="row-sub">{ev && it.place ? <Icon n="pin" s={13} /> : null}{sub}</span> : null}
        <span className="badges">
          <span className="cal-kind-tag">{ev ? <i className="cal-sw-dot" aria-hidden="true" /> : <Icon n={KIND_STYLE[it.kind].icon} s={12} />}{itemKindLabel(it, I)}</span>
          <PrioTag p={it.prio} />
          {it.done ? <span className="badge b-ok"><Icon n="check" s={12} /> {tt('Орындалды', 'Выполнено')}</span> : null}
          {it.pin ? <span className="badge b-line"><Icon n="bookmark" s={12} /> {tt('Күнтізбеде', 'В календаре')}</span>
            : savedBadge ? <span className="badge b-sun"><Icon n="heart" s={12} /> {tt('Сақталған', 'Сохранено')}</span> : null}
        </span>
      </button>
      {it.kind === 'class' ? null : (
        <span className="cal-tl-act">
          {it.own ? (
            <>
              <button className="icon-btn" type="button" onClick={() => openSheet(ev ? <EventSheet edit={it.own} /> : <DeadlineSheet edit={it.own} />)}
                aria-label={tt('Өзгерту: ', 'Изменить: ') + it.title}><Icon n="edit" s={20} /></button>
              <button className="icon-btn" type="button" onClick={() => del(it.own!)} aria-label={tt('Жою: ', 'Удалить: ') + it.title}><Icon n="trash" s={20} /></button>
            </>
          ) : (
            <>
              <button className="icon-btn" type="button" onClick={() => openSheet(<ItemSheet it={it} />)}
                aria-label={(it.pin ? tt('Өзгерту немесе күнтізбеге қосу: ', 'Изменить или добавить в календарь: ') : tt('Күнтізбеге қосу: ', 'Добавить в календарь: ')) + it.title}>
                <Icon n={it.pin ? 'edit' : 'plus'} s={20} />
              </button>
              {it.pin ? (
                <button className="icon-btn" type="button" onClick={unpin} aria-label={tt('Күнтізбеден алу: ', 'Убрать из календаря: ') + it.title}><Icon n="x" s={20} /></button>
              ) : null}
            </>
          )}
        </span>
      )}
    </li>
  );
}

const countTxt = (n: number, I: I18n) => (n ? I.tt(`${n} жазба`, `${n} ${n % 10 === 1 && n % 100 !== 11 ? 'запись' : [2, 3, 4].includes(n % 10) && ![12, 13, 14].includes(n % 100) ? 'записи' : 'записей'}`) : I.tt('жазба жоқ', 'нет записей'));

/** one day's items as a timeline: all-day first, then by time. `onAdd` = the month view's empty state with an add button */
function DayAgenda({ d, items, heading, onAdd }: { d: Ymd; items: CalItem[]; heading?: boolean; onAdd?: () => void }) {
  const { tt, I } = useApp();
  const list = sortAgenda(items);
  const H = heading ? 'h3' : 'h2';
  return (
    <section className="section cal-day" aria-label={dayLabel(d, I, true)}>
      <div className="cal-day-head">
        <H className="cal-day-h">{dayLabel(d, I)}{d === todayYmd() ? <span className="badge b-sky cal-today-badge">{tt('Бүгін', 'Сегодня')}</span> : null}</H>
        <span className="tiny muted num">{countTxt(list.length, I)}</span>
      </div>
      {list.length ? <ol className="group cal-tl">{list.map(it => <AgendaRow key={it.key} it={it} />)}</ol>
        : onAdd ? (
          <div className="cal-empty">
            <Icon n="calendar" s={28} />
            <p>{tt('Бұл күні жазба жоқ. Сабақтан тыс кездесу, дайындық немесе демалыс — өзіңіз қосыңыз.', 'На этот день записей нет. Встреча, подготовка или отдых — добавьте сами.')}</p>
            <button className="btn ghost" type="button" onClick={onAdd}><Icon n="plus" s={18} /> {tt('Осы күнге қосу', 'Добавить на этот день')}</button>
          </div>
        ) : <p className="note cal-none">{tt('Бұл күні жазба жоқ.', 'На этот день записей нет.')}</p>}
    </section>
  );
}

/** the colour key: every kind with its icon and name, own events, and what the priority marks look like */
function Legend({ classes }: { classes: boolean }) {
  const { tt, I } = useApp();
  return (
    <details className="cal-legend tiny">
      <summary>{tt('Түстер мен белгілер', 'Цвета и обозначения')}<Icon n="chev" s={16} /></summary>
      <ul aria-label={tt('Түстер', 'Цвета')}>
        {CAL_KINDS.filter(k => classes || k !== 'class').map(k => (
          <li key={k} className={`cal-k-${k}`} style={kindStyle(k)}><span className="cal-sw" aria-hidden="true"><Icon n={KIND_STYLE[k].icon} s={12} /></span> {kindLabel(k, I)}</li>
        ))}
        <li><span className="cal-sw cal-sw-own" aria-hidden="true" /> {tt('Менің іс-шарам — өзім таңдаған түс', 'Моё событие — выбранный мной цвет')}</li>
      </ul>
      <ul aria-label={tt('Маңыздылығы', 'Важность')}>
        <li><PrioTag p="high" /></li>
        <li><PrioTag p="low" /></li>
      </ul>
    </details>
  );
}

/** a month cell's titled chips: the kind's colour (or an own event's) as a stripe and soft background */
function CellChips({ items }: { items: CalItem[] }) {
  const { shown, more } = packCell(items, 3);
  if (!items.length) return null;
  return (
    <span className="cal-chips" aria-hidden="true">
      {shown.map(it => (
        <span key={it.key} className={`cal-chip cal-k-${it.kind} ${prioClass(it.prio)}${it.done ? ' is-done' : ''}${it.color ? ' is-own-ev' : ''}`} style={itemStyle(it)}>{it.title}</span>
      ))}
      {more ? <span className="cal-more num">+{more}</span> : null}
    </span>
  );
}

/** «Жыл»: 12 mini months; a month opens in the month view */
function YearView({ y, dates, onOpen }: { y: number; dates: Map<Ymd, unknown[]>; onOpen: (m: number) => void }) {
  const { I, lang } = useApp();
  const today = todayYmd();
  const counts = monthCounts([...dates.entries()].flatMap(([d, a]) => a.map(() => d)), y);
  return (
    <div className="cal-year" role="list" aria-labelledby="calTitle">
      {yearGrid(y).map(({ ref, weeks }) => {
        const cur = monthKey(ref) === today.slice(0, 7);
        return (
          <div role="listitem" key={ref.m} className="cal-mini-wrap">
            <button type="button" className={`cal-mini${cur ? ' cur' : ''}`} onClick={() => onOpen(ref.m)}
              aria-label={`${MONTHS[lang][ref.m]} ${y}, ${countTxt(counts[ref.m], I)}`}>
              <span className="cal-mini-name">{MONTHS[lang][ref.m]}</span>
              <span className="cal-mini-grid" aria-hidden="true">
                {weeks.flat().map((d, i) => d
                  ? <span key={d.ymd} className={`cal-mini-d num${dates.has(d.ymd) ? ' has' : ''}${d.ymd === today ? ' today' : ''}`}>{d.day}</span>
                  : <span key={'x' + i} />)}
              </span>
            </button>
          </div>
        );
      })}
    </div>
  );
}

type CalViewKind = 'month' | 'week' | 'year' | 'tasks';

export function CalendarView() {
  const { S, I, tt, lang, ui, setUi, openSheet, content, go } = useApp();
  const today = todayYmd();
  const sel = isYmd(ui.calSel) ? ui.calSel : today;
  const month = /^\d{4}-\d{2}$/.test(ui.calMonth) ? { y: +ui.calMonth.slice(0, 4), m: +ui.calMonth.slice(5, 7) - 1 } : monthOf(sel);
  const view: CalViewKind = ui.calView === 'week' || ui.calView === 'tasks' || ui.calView === 'year' ? ui.calView : 'month';
  const week = view === 'week', year = view === 'year';
  // the «Сабақтар» layer: by default off in the month grid (too noisy), on in the week list; the chip sets it for both
  const classesOn = ui.calClasses === 'auto' ? week : ui.calClasses === 'on';
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
  const openMonth = (y: number, m: number) => {
    const key = monthKey({ y, m });
    // keep the selected day when it is in that month, else select the 1st (today when it is today's month)
    const d = sel.slice(0, 7) === key ? sel : today.slice(0, 7) === key ? today : `${key}-01`;
    setUi({ calSel: d, calMonth: key, calView: 'month' });
  };
  const addOn = (d: Ymd) => openSheet(<EventSheet date={d < today ? today : d} />);

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
  const gridClasses = classesOn && !week && !year ? byDay(classItems(S.timetable, weeks[0][0].ymd, weeks[weeks.length - 1][6].ymd)) : null;
  const withClasses = (d: Ymd, list: CalItem[], on: boolean) => (on ? [...list, ...classItems(S.timetable, d, d)] : list);
  const setView = (v: CalViewKind) => setUi({ calView: v });
  const nav = (n: number) => (week ? pick(addDays(sel, 7 * n)) : year ? moveMonth(12 * n) : moveMonth(n));
  const navLabel = (n: number) => (week ? (n < 0 ? tt('Алдыңғы апта', 'Предыдущая неделя') : tt('Келесі апта', 'Следующая неделя'))
    : year ? (n < 0 ? tt('Алдыңғы жыл', 'Предыдущий год') : tt('Келесі жыл', 'Следующий год'))
      : n < 0 ? tt('Алдыңғы ай', 'Предыдущий месяц') : tt('Келесі ай', 'Следующий месяц'));
  const addBtn = (
    <button className="btn block cal-add" type="button" onClick={() => addOn(sel)}>
      <Icon n="plus" s={20} /> {tt('Іс-шара қосу', 'Добавить событие')}
    </button>
  );

  const dayName = (d: Ymd, its: CalItem[]) => {
    const hi = its.some(i => i.prio === 'high' && !i.done);
    const titles = sortAgenda(its).map(i => `${i.time ? i.time + ' ' : ''}${i.title} (${itemKindLabel(i, I)})`);
    return `${dayLabel(d, I, true)}, ${countTxt(its.length, I)}${titles.length ? ': ' + titles.join('; ') : ''}${hi ? ', ' + tt('маңызды бар', 'есть важное') : ''}`;
  };

  return (
    <div className="cal-screen">
      <div className="seg cal-views" role="group" aria-label={tt('Көрініс', 'Вид')}>
        <button type="button" aria-pressed={view === 'month'} onClick={() => setView('month')}>{tt('Ай', 'Месяц')}</button>
        <button type="button" aria-pressed={week} onClick={() => setView('week')}>{tt('Апта', 'Неделя')}</button>
        <button type="button" aria-pressed={year} onClick={() => setView('year')}>{tt('Жыл', 'Год')}</button>
        <button type="button" aria-pressed={view === 'tasks'} onClick={() => setView('tasks')}>{tt('Тапсырмалар', 'Задачи')}</button>
      </div>

      {view === 'tasks' ? <TasksView /> : (
        <>
          <div className="cal-filters">
            <div className="chips">
              <button className="chip" type="button" aria-pressed={ui.calSaved} onClick={() => setUi({ calSaved: !ui.calSaved })}>
                <Icon n="heart" s={16} /> {tt('Сақталғандар мен менікі', 'Сохранённое и моё')}
              </button>
              {year ? null : S.timetable.length ? (
                <button className="chip cal-classes-chip" type="button" aria-pressed={classesOn} onClick={() => setUi({ calClasses: classesOn ? 'off' : 'on' })}>
                  <Icon n="book" s={16} /> {tt('Сабақтар', 'Занятия')}
                </button>
              ) : (
                <button className="chip cal-classes-chip" type="button" id="calAddTimetable" onClick={() => go('timetable')}>
                  <Icon n="plus" s={16} /> {tt('Сабақ кестесін қосу', 'Добавить расписание')}
                </button>
              )}
            </div>
            {week ? null : (
              <MonthPicker month={month} onPick={(y, m) => (year ? setUi({ calMonth: monthKey({ y, m }) }) : openMonth(y, m))} onMonth={m => openMonth(month.y, m)} />
            )}
          </div>

          <div className="cal-nav">
            <button className="icon-btn" type="button" onClick={() => nav(-1)} aria-label={navLabel(-1)}><Icon n="back" s={20} /></button>
            <h2 className="cal-title" id="calTitle" aria-live="polite">{week ? weekTitle : year ? String(month.y) : monthTitle}</h2>
            <button className="icon-btn" type="button" onClick={() => nav(1)} aria-label={navLabel(1)}><Icon n="chev" s={20} /></button>
            <button className="chip cal-today-chip" type="button" onClick={year ? () => setUi({ calSel: today, calMonth: today.slice(0, 7) }) : goToday}>{tt('Бүгін', 'Сегодня')}</button>
          </div>

          {year ? (
            <YearView y={month.y} dates={days} onOpen={m => openMonth(month.y, m)} />
          ) : week ? (
            <>
              {addBtn}
              <div className="cal-week">
                {weekDays.map(d => <DayAgenda key={d} d={d} items={withClasses(d, days.get(d) ?? [], classesOn)} heading />)}
              </div>
            </>
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
                      const its = sortAgenda([...(days.get(d.ymd) ?? []), ...(gridClasses?.get(d.ymd) ?? [])]);
                      return (
                        <span role="gridcell" key={d.ymd} className="cal-cell">
                          <button type="button" data-ymd={d.ymd} className={`cal-day-btn${d.inMonth ? '' : ' out'}${its.length ? ' has' : ''}`}
                            tabIndex={d.ymd === tabStop ? 0 : -1}
                            aria-current={d.ymd === today ? 'date' : undefined} aria-pressed={d.ymd === sel}
                            aria-label={dayName(d.ymd, its)}
                            onClick={() => pick(d.ymd)}>
                            <span className="cal-num num">{d.day}</span>
                            <CellChips items={its} />
                          </button>
                        </span>
                      );
                    })}
                  </div>
                ))}
              </div>
              {addBtn}
              <DayAgenda d={sel} items={withClasses(sel, days.get(sel) ?? [], classesOn)} onAdd={() => addOn(sel)} />
            </>
          )}
          {year ? null : <Legend classes={classesOn} />}
          {all.length === 0 && year ? (
            <Empty icon="calendar">{ui.calSaved
              ? tt('Сақталған мерзімдер мен өз жазбаларыңыз әзірге жоқ.', 'Сохранённых сроков и своих записей пока нет.')
              : tt('Әзірге мерзімдер жоқ. Өз іс-шараңызды қосыңыз.', 'Сроков пока нет. Добавьте своё событие.')}</Empty>
          ) : null}
        </>
      )}
    </div>
  );
}

/** «Айды таңдау: [Қараша ▾] [2026 ▾]»: jumps the month view (in the year view the year select changes the year) */
function MonthPicker({ month, onPick, onMonth }: { month: { y: number; m: number }; onPick: (y: number, m: number) => void; onMonth: (m: number) => void }) {
  const { tt, lang } = useApp();
  const thisYear = +todayYmd().slice(0, 4);
  return (
    <div className="cal-pick" role="group" aria-labelledby="calPickL">
      <span id="calPickL" className="cal-pick-l">{tt('Айды таңдау', 'Выбрать месяц')}:</span>
      <select className="input" aria-label={tt('Ай', 'Месяц')} value={month.m} onChange={e => onMonth(+e.target.value)}>
        {MONTHS[lang].map((n, i) => <option key={n} value={i}>{n}</option>)}
      </select>
      <select className="input cal-pick-y" aria-label={tt('Жыл', 'Год')} value={month.y} onChange={e => onPick(+e.target.value, month.m)}>
        {yearOptions(thisYear, month.y).map(y => <option key={y} value={y}>{y}</option>)}
      </select>
    </div>
  );
}

/* ---------- «Тапсырмалар»: the to-do list ---------- */
type DateMode = '' | 'today' | 'tomorrow' | 'pick';

/** a quick-added to-do task: an own deadline, maybe without a date */
function newTask(title: string, date: Ymd, prio: Priority): OwnDeadline {
  const now = Date.now();
  return { id: 'dl' + now.toString(36), title: title.slice(0, 200), date, time: '', note: '', created: now, prio, done: 0, end: '', place: '', color: 0 };
}

function TaskRow({ it }: { it: CalItem }) {
  const { tt, I, openSheet } = useApp();
  const toggle = useDoneToggle();
  const id = 'tk-' + it.key.replace(/[^\w-]/g, '_');
  return (
    <li className={`task cal-k-${it.kind} ${prioClass(it.prio)}${it.done ? ' is-done' : ''}`} style={kindStyle(it.kind)}>
      <input type="checkbox" id={id} className="task-check" checked={!!it.done} onChange={e => toggle(it, e.target.checked)} />
      <label htmlFor={id} className="task-main">
        <span className="task-title">{it.title}</span>
        <span className="task-meta">
          <span className="cal-kind-tag"><Icon n={KIND_STYLE[it.kind].icon} s={12} /> {it.kind === 'own' ? tt('Тапсырма', 'Задача') : kindLabel(it.kind, I)}</span>
          <span className="num">{it.date ? `${dayLabel(it.date, I)}${it.time ? ', ' + it.time : ''}` : tt('Мерзімі жоқ', 'Без срока')}</span>
          <PrioTag p={it.prio} />
        </span>
      </label>
      <button className="icon-btn" type="button" onClick={() => openSheet(<ItemSheet it={it} />)} aria-label={tt('Толығырақ: ', 'Подробнее: ') + it.title}>
        <Icon n="chev" s={20} />
      </button>
    </li>
  );
}

function TasksView() {
  const { S, I, tt, update, toast, content } = useApp();
  const [mode, setMode] = useState<DateMode>('');
  const [picked, setPicked] = useState('');
  const [prio, setPrio] = useState<Priority>('mid');
  const today = todayYmd();
  const groups = groupTasks(taskItems(S, I, content), today);
  const dateOf = (): Ymd => (mode === 'today' ? today : mode === 'tomorrow' ? addDays(today, 1) : mode === 'pick' && isYmd(picked) ? picked : '');
  const chip = (m: DateMode, label: string) => (
    <button type="button" className="chip" aria-pressed={mode === m} onClick={() => setMode(mode === m ? '' : m)}>{label}</button>
  );
  return (
    <>
      <form className="task-add" onSubmit={e => {
        e.preventDefault();
        const input = e.currentTarget.querySelector<HTMLInputElement>('#taskNew')!;
        const title = input.value.trim();
        if (!title) { input.focus(); return; }
        const date = dateOf();
        const rec = newTask(title, date, prio);
        update(s => { s.deadlines = [...(s.deadlines ?? []), rec]; });
        toast(tt('Тапсырма қосылды', 'Задача добавлена'));
        input.value = '';
        setMode(''); setPicked(''); setPrio('mid');
        input.focus();
      }}>
        <label htmlFor="taskNew" className="visually-hidden">{tt('Жаңа тапсырма', 'Новая задача')}</label>
        <div className="task-add-row">
          <input className="input" id="taskNew" maxLength={200} autoComplete="off" placeholder={tt('Жаңа тапсырма…', 'Новая задача…')} />
          <button className="btn" type="submit"><Icon n="plus" s={18} /> {tt('Қосу', 'Добавить')}</button>
        </div>
        <div className="task-add-opts">
          <div className="chips" role="group" aria-label={tt('Мерзімі', 'Срок')}>
            {chip('today', tt('Бүгін', 'Сегодня'))}
            {chip('tomorrow', tt('Ертең', 'Завтра'))}
            {chip('pick', tt('Күн таңдау', 'Выбрать дату'))}
          </div>
          {mode === 'pick' ? (
            <input className="input task-date" type="date" aria-label={tt('Күні', 'Дата')} value={picked} min={today} onChange={e => setPicked(e.target.value)} />
          ) : null}
          <label className="task-prio">
            <span>{tt('Маңыздылығы', 'Важность')}</span>
            <select className="input" value={prio} onChange={e => setPrio(isPriority(e.target.value) ? e.target.value : 'mid')}>
              {PRIORITIES.map(p => <option key={p} value={p}>{tt(PRIO_LABEL[p].kk, PRIO_LABEL[p].ru)}</option>)}
            </select>
          </label>
        </div>
      </form>
      {groups.length ? groups.map(g => (
        <section key={g.key} className={`section task-group task-g-${g.key}`} aria-labelledby={'tg-' + g.key}>
          <h3 className="cal-day-h" id={'tg-' + g.key}>{tt(GROUP_LABEL[g.key].kk, GROUP_LABEL[g.key].ru)} <span className="tiny muted num">{g.items.length}</span></h3>
          <ul className="group task-list">{g.items.map(it => <TaskRow key={it.key} it={it} />)}</ul>
        </section>
      )) : (
        <Empty icon="check">{tt('Тапсырмалар әзірге жоқ. Жоғарыда қосыңыз немесе іс-шара, тағылымдама бетінде «Күнтізбеге қосу» түймесін басыңыз.',
          'Задач пока нет. Добавьте сверху или нажмите «В календарь» на странице события или стажировки.')}</Empty>
      )}
    </>
  );
}

/** home: «Жақын дедлайндар», the next three open ones, each with a checkbox to check it off, and a link to the calendar */
export function UpcomingDeadlines() {
  const { S, I, tt, go, content } = useApp();
  const openDay = useOpenDay();
  const toggle = useDoneToggle();
  const list = nextDeadlines(S, I, content, 3);
  if (!list.length) return null;
  return (
    <section className="section" id="homeDeadlines">
      <SectionHead title={tt('Жақын дедлайндар', 'Ближайшие дедлайны')}
        more={<button className="link-btn" type="button" onClick={() => go('calendar')}>{tt('Күнтізбе', 'Календарь')}</button>} />
      <div className="group">
        {list.map(it => (
          <div key={it.key} className={`sr cal-home-row cal-k-${it.kind} ${prioClass(it.prio)}`} style={itemStyle(it)}>
            <span className="cal-home-check">
              <input type="checkbox" className="task-check" checked={false} onChange={e => toggle(it, e.target.checked)}
                aria-label={tt('Орындалды деп белгілеу: ', 'Отметить выполненным: ') + it.title} />
            </span>
            <button className="row" type="button" onClick={() => { if (it.go) { go(it.go); return; } openDay(it.date); }}>
              <span className="cal-ico" aria-hidden="true"><Icon n={KIND_STYLE[it.kind].icon} s={20} /></span>
              <span className="row-main"><span className="row-title">{it.title}</span><span className="row-sub">{itemKindLabel(it, I)} · {dayLabel(it.date, I)}{it.time ? `, ${timeRange(it.time, it.end)}` : ''}</span></span>
              <span className={`tag ${daysBetween(todayYmd(), it.date) <= 3 ? 'tag-warn' : 'tag-new'}`}>{relLabel(it.date, I)}</span>
            </button>
          </div>
        ))}
      </div>
    </section>
  );
}
