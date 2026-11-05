/* «Күнтізбе» layers, colours and the «Тапсырмалар» to-do list: pure logic (no DOM, no React).
   Colours live in app/globals.css as --cal-<kind> tokens; this file only maps a kind to its token, icon and label,
   so a kind is never told apart by colour alone. */
import { addDays, weekdayMon, type Priority, type Ymd } from './calendar';

/** what an item on the calendar is: a class, an internship/grant, a vacancy, an event, an exam or the student's own */
export type CalKind = 'class' | 'opp' | 'job' | 'event' | 'exam' | 'own';
/** legend order */
export const CAL_KINDS: CalKind[] = ['class', 'opp', 'job', 'event', 'exam', 'own'];

export interface KindStyle {
  /** components/ui Icon name */
  icon: 'book' | 'globe' | 'briefcase' | 'ticket' | 'cap' | 'clock';
  /** the CSS custom property with this kind's solid colour */
  token: string;
  kk: string;
  ru: string;
}
export const KIND_STYLE: Record<CalKind, KindStyle> = {
  class: { icon: 'book', token: '--cal-class', kk: 'Сабақ', ru: 'Занятие' },
  opp: { icon: 'globe', token: '--cal-opp', kk: 'Тағылымдама, грант', ru: 'Стажировка, грант' },
  job: { icon: 'briefcase', token: '--cal-job', kk: 'Вакансия', ru: 'Вакансия' },
  event: { icon: 'ticket', token: '--cal-event', kk: 'Іс-шара', ru: 'Мероприятие' },
  exam: { icon: 'cap', token: '--cal-exam', kk: 'Емтихан', ru: 'Экзамен' },
  own: { icon: 'clock', token: '--cal-own', kk: 'Менікі', ru: 'Моё' },
};
/** the CSS colour of a kind, e.g. 'var(--cal-opp)' */
export const kindColor = (k: CalKind) => `var(${KIND_STYLE[k].token})`;

export const PRIO_RANK: Record<Priority, number> = { high: 0, mid: 1, low: 2 };
export const PRIO_LABEL: Record<Priority, { kk: string; ru: string }> = {
  high: { kk: 'Жоғары', ru: 'Высокий' },
  mid: { kk: 'Орта', ru: 'Средний' },
  low: { kk: 'Төмен', ru: 'Низкий' },
};
/** the row modifier class: high = bold with a strong marker, low = muted, mid = plain */
export const prioClass = (p?: Priority) => (p === 'high' ? 'prio-high' : p === 'low' ? 'prio-low' : '');

/** an open or done task; no priority counts as «Орта» */
export interface TaskLike { title: string; date: Ymd; time: string; prio?: Priority; done: number }

/** dated before undated; then by day, priority (high first), time (all-day first) and title */
export function compareTasks(a: TaskLike, b: TaskLike): number {
  if (!a.date !== !b.date) return a.date ? -1 : 1;
  return a.date.localeCompare(b.date) || PRIO_RANK[a.prio ?? 'mid'] - PRIO_RANK[b.prio ?? 'mid']
    || (a.time || '').localeCompare(b.time || '') || a.title.localeCompare(b.title);
}

export type TaskGroupKey = 'overdue' | 'today' | 'tomorrow' | 'week' | 'later' | 'done';
export const TASK_GROUPS: TaskGroupKey[] = ['overdue', 'today', 'tomorrow', 'week', 'later', 'done'];
export const GROUP_LABEL: Record<TaskGroupKey, { kk: string; ru: string }> = {
  overdue: { kk: 'Мерзімі өтті', ru: 'Просрочено' },
  today: { kk: 'Бүгін', ru: 'Сегодня' },
  tomorrow: { kk: 'Ертең', ru: 'Завтра' },
  week: { kk: 'Осы апта', ru: 'На этой неделе' },
  later: { kk: 'Кейін', ru: 'Позже' },
  done: { kk: 'Орындалды', ru: 'Выполнено' },
};

/** which list a task falls into on `today` (weeks end on Sunday; an undated task is «Кейін») */
export function taskGroup(t: Pick<TaskLike, 'date' | 'done'>, today: Ymd): TaskGroupKey {
  if (t.done) return 'done';
  if (!t.date) return 'later';
  if (t.date < today) return 'overdue';
  if (t.date === today) return 'today';
  if (t.date === addDays(today, 1)) return 'tomorrow';
  if (t.date <= addDays(today, 6 - weekdayMon(today))) return 'week';
  return 'later';
}

/** the non-empty groups in display order; open tasks sorted by compareTasks, done ones most recently done first */
export function groupTasks<T extends TaskLike>(tasks: T[], today: Ymd): { key: TaskGroupKey; items: T[] }[] {
  const m = new Map<TaskGroupKey, T[]>();
  for (const t of tasks) {
    const k = taskGroup(t, today);
    const a = m.get(k);
    if (a) a.push(t); else m.set(k, [t]);
  }
  return TASK_GROUPS.filter(k => m.has(k)).map(k => ({
    key: k,
    items: m.get(k)!.sort(k === 'done' ? (a, b) => b.done - a.done || compareTasks(a, b) : compareTasks),
  }));
}
