'use client';
/* «Күнтізбе» (/calendar): a Google-Calendar-like month grid (Monday first, Almaty days), a week list and a
   «Тапсырмалар» to-do list. It shows application deadlines (opportunities, vacancies), events, exam plans, the
   sample class timetable as a «Сабақтар» layer, the student's own deadlines and the items pinned with
   «Күнтізбеге қосу» (PinButton, used on detail pages and list rows). Every kind has its own colour, icon and label;
   own deadlines and pins carry a priority. Date logic: lib/calendar.ts; grouping and colours: lib/caltasks.ts. */
import { useEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from 'react';
import { useApp } from '@/components/app';
import { ConfirmSheet, DemoBadge, Empty, Icon, SectionHead } from '@/components/ui';
import {
  addDays, almatyHm, almatyYmd, byDay, daysBetween, googleCalendarUrl, isHm, isPriority, isYmd, monthGrid, monthKey, monthOf, MONTHS,
  PRIORITIES, shiftMonth, WEEK_SHORT, weekdayMon, weekdaySun, type CalPin, type OwnDeadline, type PinKind, type Priority, type Ymd,
} from '@/lib/calendar';
import {
  CAL_KINDS, GROUP_LABEL, groupTasks, isClassDay, KIND_STYLE, kindColor, PRIO_LABEL, prioClass, type CalKind,
} from '@/lib/caltasks';
import { EVENTS, SCHEDULE } from '@/lib/data/static';
import { typeLabel } from '@/lib/domain';
import { buildOneIcs } from '@/lib/ics';
import { pathOf } from '@/lib/routes';
import { dOff, type I18n } from '@/lib/format';
import type { AppState, Content } from '@/lib/types';

export type { CalKind } from '@/lib/caltasks';
export interface CalItem {
  key: string;
  kind: CalKind;
  date: Ymd;
  time: string;
  /** a class's end time */
  end?: string;
  title: string;
  sub: string;
  go?: string;
  saved: boolean;
  demo: boolean;
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

function ownItem(d: OwnDeadline, I: I18n): CalItem {
  return {
    key: 'own:' + d.id, kind: 'own', date: d.date, time: d.time, title: d.title, sub: d.note || I.tt('Менің дедлайным', 'Мой дедлайн'),
    saved: true, demo: false, own: d, prio: d.prio, done: d.done,
  };
}

/** the sample weekday timetable (lib/data/static SCHEDULE) on the weekdays from..to */
export function classItems(I: I18n, from: Ymd, to: Ymd): CalItem[] {
  const out: CalItem[] = [];
  for (let d = from; d <= to; d = addDays(d, 1)) {
    if (!isClassDay(d)) continue;
    SCHEDULE.forEach((c, i) => out.push({
      key: `class:${d}:${i}`, kind: 'class', date: d, time: c.t, end: c.end, title: I.L(c.name),
      sub: `${I.L(c.kind)} · ${c.room} · ${c.t}–${c.end}`, saved: true, demo: true, done: 0,
    }));
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
      sub: `${typeLabel(o.type, I)} · ${tt('өтінім мерзімі', 'приём заявок до')}`, go: 'opp:' + o.id, saved: !!S.saved[o.id], demo: !!o.is_demo,
    });
  }
  for (const j of C.jobs) {
    if (!j.deadline || !isYmd(j.deadline.slice(0, 10)) || j.deadline.slice(0, 10) < today) continue;
    add({
      key: 'job:' + j.id, kind: 'job', date: j.deadline.slice(0, 10), time: '', title: L(j.title),
      sub: `${L(j.org)} · ${tt('өтінім мерзімі', 'отклик до')}`, go: 'job:' + j.id, saved: !!S.fav[j.id], demo: !!j.is_demo,
    });
  }
  for (const e of C.events ?? []) {
    if (!e.starts_at || Number.isNaN(Date.parse(e.starts_at))) continue;
    add({
      key: 'event:' + e.id, kind: 'event', date: almatyYmd(e.starts_at), time: almatyHm(e.starts_at),
      title: (S.lang === 'ru' ? e.title_ru || e.title_kk : e.title_kk) || '', sub: [tt('Іс-шара', 'Мероприятие'), e.place_text].filter(Boolean).join(' · '),
      go: 'event:' + e.id, saved: false, demo: !!e.is_demo,
    });
  }
  // pinned items that are not in the feed (a sample event, a closed vacancy): their saved copy
  for (const p of pins.values()) {
    out.push({
      key: p.ref, kind: p.kind, date: p.date, time: p.time, title: p.title[S.lang === 'ru' ? 'ru' : 'kk'] || p.title.kk,
      sub: kindLabel(p.kind, I), go: p.ref, saved: true, demo: false, pin: p, prio: p.prio, done: p.done,
    });
  }
  for (const pl of S.plans ?? []) {
    if (!pl.exam || Number.isNaN(Date.parse(pl.exam))) continue;
    out.push({
      key: 'exam:' + pl.id, kind: 'exam', date: almatyYmd(pl.exam), time: '', title: pl.subject || tt('Емтихан', 'Экзамен'),
      sub: tt('Емтихан · дайындық жоспары', 'Экзамен · план подготовки'), go: 'plan:' + pl.id, saved: true, demo: false, done: 0,
    });
  }
  for (const d of S.deadlines ?? []) if (d.date) out.push(ownItem(d, I));
  return out;
}

/** the next open deadlines from today (not events, not done): the home card */
export function nextDeadlines(S: AppState, I: I18n, C: Content, n: number): CalItem[] {
  const today = todayYmd();
  return calItems(S, I, C).filter(i => i.kind !== 'event' && i.kind !== 'exam' && !i.done && i.date >= today)
    .sort((a, b) => a.date.localeCompare(b.date) || (a.time || '').localeCompare(b.time || '')).slice(0, n);
}

/** the to-do list: own deadlines (with undated tasks) and pinned items */
export function taskItems(S: AppState, I: I18n, C: Content): CalItem[] {
  return [
    ...calItems(S, I, C).filter(i => i.pin || i.own),
    ...(S.deadlines ?? []).filter(d => !d.date).map(d => ownItem(d, I)),
  ];
}

/* ---------- pins ---------- */
export interface PinTarget { ref: string; kind: PinKind; title: { kk: string; ru: string }; date: Ymd; time: string }

const ymdLocal = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;

/** what «Күнтізбеге қосу» would pin for a view id ('opp:x', 'job:x', 'event:12', 'event:e1'); null = no date to pin */
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
    const s = EVENTS.find(x => x.id === id);
    if (s && !s.past) {
      return { ref, kind, title: { kk: s.title.kk, ru: s.title.ru || s.title.kk }, date: ymdLocal(dOff(s.day)), time: isHm(s.time) ? s.time : '' };
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

interface Exp { key: string; title: string; date: Ymd; time: string; details: string }
function exportOf(x: { key: string; title: string; date: Ymd; time: string; go?: string; note: string }): Exp {
  const link = x.go && typeof window !== 'undefined' ? window.location.origin + pathOf(x.go) : '';
  return { key: x.key, title: x.title, date: x.date, time: x.time, details: [x.note, link, 'Student Hub'].filter(Boolean).join('\n') };
}
function itemExport(it: CalItem, I: I18n): Exp {
  const title = it.kind === 'opp' || it.kind === 'job' ? I.tt('Мерзім: ', 'Дедлайн: ') + it.title : it.title;
  return exportOf({ key: it.key, title, date: it.date, time: it.time, go: it.go, note: it.kind === 'own' ? it.own?.note ?? '' : it.sub });
}

/** «Google Calendar-ға қосу» + «.ics жүктеу» for one item */
function ExportButtons({ ex }: { ex: Exp }) {
  const { tt, lang, toast } = useApp();
  const ics = () => {
    saveFile(buildOneIcs({ key: ex.key, date: ex.date, time: ex.time, summary: ex.title, description: ex.details }, { now: new Date(), lang }),
      `studenthub-${ex.date}.ics`);
    toast(tt('Файл жүктелді — оны күнтізбемен ашыңыз', 'Файл скачан — откройте его календарём'));
  };
  return (
    <div className="btn-row">
      <a className="btn secondary" href={googleCalendarUrl(ex)} target="_blank" rel="noopener"><Icon n="ext" s={18} /> {tt('Google Calendar-ға қосу', 'Добавить в Google Calendar')}</a>
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
  return (
    <>
      <span className={`eyebrow cal-eyebrow cal-k-${it.kind}`} style={kindStyle(it.kind)}><Icon n={KIND_STYLE[it.kind].icon} s={14} /> {it.kind === 'own' ? tt('Менің дедлайным', 'Мой дедлайн') : kindLabel(it.kind, I)}</span>
      <h2>{it.title}</h2>
      <p className="muted">{it.date ? <>{dayLabel(it.date, I, true)}{it.time ? `, ${it.time}${it.end ? '–' + it.end : ''}` : ''} · {relLabel(it.date, I)}</> : tt('Мерзімі жоқ', 'Без срока')}</p>
      {it.kind === 'class' ? <p>{it.sub}</p> : null}
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
          prio: isPriority(pr) ? pr : 'mid', done: edit?.done ?? 0,
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

/* ---------- month / week ---------- */
function ItemRow({ it }: { it: CalItem }) {
  const { tt, I, go, openSheet } = useApp();
  const open = () => (it.go ? go(it.go) : openSheet(<ItemSheet it={it} />));
  return (
    <div className={`sr cal-item cal-k-${it.kind} ${prioClass(it.prio)}${it.done ? ' is-done' : ''}`} data-kind={it.kind} style={kindStyle(it.kind)}>
      <button className="sr-main" type="button" onClick={open}>
        <span className="cal-ico" aria-hidden="true"><Icon n={KIND_STYLE[it.kind].icon} s={20} /></span>
        <span className="row-main">
          <span className="row-title">{it.title}</span>
          <span className="row-sub">{it.time ? <b className="num">{it.time}{it.end ? '–' + it.end : ''} </b> : null}{it.kind === 'class' ? `${kindLabel('class', I)} · ${it.sub.split(' · ').slice(0, 2).join(' · ')}` : it.sub}{it.demo && it.kind !== 'class' ? <> <DemoBadge show /></> : null}</span>
          <span className="badges">
            <span className="cal-kind-tag">{kindLabel(it.kind, I)}</span>
            <PrioTag p={it.prio} />
            {it.done ? <span className="badge b-ok"><Icon n="check" s={12} /> {tt('Орындалды', 'Выполнено')}</span> : null}
            {it.pin ? <span className="badge b-line"><Icon n="bookmark" s={12} /> {tt('Күнтізбеде', 'В календаре')}</span>
              : it.saved && it.kind !== 'own' && it.kind !== 'class' && it.kind !== 'exam' ? <span className="badge b-sun"><Icon n="heart" s={12} /> {tt('Сақталған', 'Сохранено')}</span> : null}
          </span>
        </span>
      </button>
      {it.kind === 'class' ? null : (
        <span className="sr-end">
          <button className="icon-btn" type="button" onClick={() => openSheet(<ItemSheet it={it} />)}
            aria-label={(it.own || it.pin ? tt('Өзгерту немесе күнтізбеге қосу: ', 'Изменить или добавить в календарь: ') : tt('Күнтізбеге қосу: ', 'Добавить в календарь: ')) + it.title}>
            <Icon n={it.own || it.pin ? 'edit' : 'plus'} s={20} />
          </button>
        </span>
      )}
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

/** the colour key: every kind with its icon and name, and what the priority marks look like */
function Legend({ classes }: { classes: boolean }) {
  const { tt, I } = useApp();
  return (
    <div className="cal-legend tiny">
      <ul aria-label={tt('Түстер', 'Цвета')}>
        {CAL_KINDS.filter(k => classes || k !== 'class').map(k => (
          <li key={k} className={`cal-k-${k}`} style={kindStyle(k)}><span className="cal-sw" aria-hidden="true"><Icon n={KIND_STYLE[k].icon} s={12} /></span> {kindLabel(k, I)}</li>
        ))}
      </ul>
      <ul aria-label={tt('Маңыздылығы', 'Важность')}>
        <li><PrioTag p="high" /></li>
        <li><PrioTag p="low" /></li>
      </ul>
    </div>
  );
}

export function CalendarView() {
  const { S, I, tt, lang, ui, setUi, openSheet, content } = useApp();
  const today = todayYmd();
  const sel = isYmd(ui.calSel) ? ui.calSel : today;
  const month = /^\d{4}-\d{2}$/.test(ui.calMonth) ? { y: +ui.calMonth.slice(0, 4), m: +ui.calMonth.slice(5, 7) - 1 } : monthOf(sel);
  const view = ui.calView === 'week' || ui.calView === 'tasks' ? ui.calView : 'month';
  const week = view === 'week';
  // the «Сабақтар» layer: by default off in the month view (too noisy), on in the week list; the chip sets it for both
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
  const gridClasses = classesOn && !week ? byDay(classItems(I, weeks[0][0].ymd, weeks[weeks.length - 1][6].ymd)) : null;
  const withClasses = (d: Ymd, list: CalItem[], on: boolean) => (on && isClassDay(d) ? [...list, ...classItems(I, d, d)].sort((a, b) => (a.time || '').localeCompare(b.time || '')) : list);
  const countTxt = (n: number) => (n ? tt(`${n} жазба`, `${n} ${n === 1 ? 'запись' : n < 5 ? 'записи' : 'записей'}`) : tt('жазба жоқ', 'нет записей'));
  const setView = (v: 'month' | 'week' | 'tasks') => setUi({ calView: v });

  return (
    <>
      <div className="cal-top">
        <div className="seg" role="group" aria-label={tt('Көрініс', 'Вид')}>
          <button type="button" aria-pressed={view === 'month'} onClick={() => setView('month')}>{tt('Ай', 'Месяц')}</button>
          <button type="button" aria-pressed={week} onClick={() => setView('week')}>{tt('Апта', 'Неделя')}</button>
          <button type="button" aria-pressed={view === 'tasks'} onClick={() => setView('tasks')}>{tt('Тапсырмалар', 'Задачи')}</button>
        </div>
        <button className="btn cal-add" type="button" onClick={() => openSheet(<DeadlineSheet date={sel < today ? today : sel} />)}>
          <Icon n="plus" s={18} /> {tt('Дедлайн қосу', 'Добавить дедлайн')}
        </button>
      </div>

      {view === 'tasks' ? <TasksView /> : (
        <>
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
            <button className="chip cal-classes-chip" type="button" aria-pressed={classesOn} onClick={() => setUi({ calClasses: classesOn ? 'off' : 'on' })}>
              <Icon n="book" s={16} /> {tt('Сабақтар', 'Занятия')}
            </button>
          </div>

          {week ? (
            <div className="cal-week">
              {weekDays.map(d => <DayList key={d} d={d} items={withClasses(d, days.get(d) ?? [], classesOn)} heading />)}
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
                      const its = [...(days.get(d.ymd) ?? []), ...(gridClasses?.get(d.ymd) ?? [])];
                      const kinds = CAL_KINDS.filter(k => its.some(i => i.kind === k));
                      const hi = new Set(its.filter(i => i.prio === 'high' && !i.done).map(i => i.kind));
                      return (
                        <span role="gridcell" key={d.ymd} className="cal-cell">
                          <button type="button" data-ymd={d.ymd} className={`cal-day-btn${d.inMonth ? '' : ' out'}${its.length ? ' has' : ''}`}
                            tabIndex={d.ymd === tabStop ? 0 : -1}
                            aria-current={d.ymd === today ? 'date' : undefined} aria-pressed={d.ymd === sel}
                            aria-label={`${dayLabel(d.ymd, I, true)}, ${countTxt(its.length)}${kinds.length ? ': ' + kinds.map(k => kindLabel(k, I)).join(', ') : ''}${hi.size ? ', ' + tt('маңызды бар', 'есть важное') : ''}`}
                            onClick={() => pick(d.ymd)}>
                            <span className="cal-num num">{d.day}</span>
                            <span className="cal-dots" aria-hidden="true">
                              {kinds.slice(0, 4).map(k => <i key={k} className={`cal-dot cal-k-${k}${hi.has(k) ? ' hi' : ''}`} style={kindStyle(k)} />)}
                            </span>
                          </button>
                        </span>
                      );
                    })}
                  </div>
                ))}
              </div>
              <Legend classes={classesOn} />
              <DayList d={sel} items={withClasses(sel, days.get(sel) ?? [], classesOn)} />
            </>
          )}
          {week ? <Legend classes={classesOn} /> : null}
          {all.length === 0 && !week ? (
            <Empty icon="calendar">{ui.calSaved
              ? tt('Сақталған мерзімдер мен өз дедлайндарыңыз әзірге жоқ.', 'Сохранённых сроков и своих дедлайнов пока нет.')
              : tt('Әзірге мерзімдер жоқ. Өз дедлайныңызды қосыңыз.', 'Сроков пока нет. Добавьте свой дедлайн.')}</Empty>
          ) : null}
        </>
      )}
    </>
  );
}

/* ---------- «Тапсырмалар»: the to-do list ---------- */
type DateMode = '' | 'today' | 'tomorrow' | 'pick';

/** a quick-added to-do task: an own deadline, maybe without a date */
function newTask(title: string, date: Ymd, prio: Priority): OwnDeadline {
  const now = Date.now();
  return { id: 'dl' + now.toString(36), title: title.slice(0, 200), date, time: '', note: '', created: now, prio, done: 0 };
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
          <div key={it.key} className={`sr cal-home-row cal-k-${it.kind} ${prioClass(it.prio)}`} style={kindStyle(it.kind)}>
            <span className="cal-home-check">
              <input type="checkbox" className="task-check" checked={false} onChange={e => toggle(it, e.target.checked)}
                aria-label={tt('Орындалды деп белгілеу: ', 'Отметить выполненным: ') + it.title} />
            </span>
            <button className="row" type="button" onClick={() => { if (it.go) { go(it.go); return; } openDay(it.date); }}>
              <span className="cal-ico" aria-hidden="true"><Icon n={KIND_STYLE[it.kind].icon} s={20} /></span>
              <span className="row-main"><span className="row-title">{it.title}</span><span className="row-sub">{kindLabel(it.kind, I)} · {dayLabel(it.date, I)}{it.time ? `, ${it.time}` : ''}</span></span>
              <span className={`tag ${daysBetween(todayYmd(), it.date) <= 3 ? 'tag-warn' : 'tag-new'}`}>{relLabel(it.date, I)}</span>
            </button>
          </div>
        ))}
      </div>
    </section>
  );
}
