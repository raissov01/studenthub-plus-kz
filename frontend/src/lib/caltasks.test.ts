import { test } from "node:test";
import assert from "node:assert/strict";
import { cleanDeadlines, cleanPins } from "./calendar";
import {
  CAL_KINDS, compareTasks, groupTasks, KIND_STYLE, kindColor, prioClass, taskGroup, type TaskLike,
} from "./caltasks";

const T = (title: string, date: string, prio: TaskLike["prio"] = "mid", done = 0, time = ""): TaskLike => ({ title, date, time, prio, done });

test("every calendar kind has its own colour token, icon and label (colour is never the only signal)", () => {
  const tokens = CAL_KINDS.map(k => KIND_STYLE[k].token);
  const icons = CAL_KINDS.map(k => KIND_STYLE[k].icon);
  assert.equal(new Set(tokens).size, CAL_KINDS.length);
  assert.equal(new Set(icons).size, CAL_KINDS.length);
  for (const k of CAL_KINDS) {
    assert.ok(KIND_STYLE[k].kk && KIND_STYLE[k].ru);
    assert.equal(kindColor(k), `var(--cal-${k})`);
  }
  assert.equal(prioClass("high"), "prio-high");
  assert.equal(prioClass("low"), "prio-low");
  assert.equal(prioClass("mid"), "");
  assert.equal(prioClass(undefined), "");
});

test("tasks sort by date, then priority, then time; undated last", () => {
  const list = [
    T("c", "2026-11-06", "low"), T("none", ""), T("b", "2026-11-06", "high", 0, "18:00"), T("a", "2026-11-05", "low"),
    T("b2", "2026-11-06", "high", 0, "09:00"),
  ].sort(compareTasks);
  assert.deepEqual(list.map(t => t.title), ["a", "b2", "b", "c", "none"]);
});

test("tasks group into overdue / today / tomorrow / this week / later / done (weeks end on Sunday)", () => {
  const today = "2026-11-04"; // a Wednesday
  assert.equal(taskGroup(T("x", "2026-11-03"), today), "overdue");
  assert.equal(taskGroup(T("x", today), today), "today");
  assert.equal(taskGroup(T("x", "2026-11-05"), today), "tomorrow");
  assert.equal(taskGroup(T("x", "2026-11-08"), today), "week");
  assert.equal(taskGroup(T("x", "2026-11-09"), today), "later");
  assert.equal(taskGroup(T("x", ""), today), "later");
  assert.equal(taskGroup(T("x", "2026-11-03", "mid", 5), today), "done");
  // on a Sunday, tomorrow is already next week: nothing is «this week»
  assert.equal(taskGroup(T("x", "2026-11-09"), "2026-11-08"), "tomorrow");
  assert.equal(taskGroup(T("x", "2026-11-10"), "2026-11-08"), "later");

  const groups = groupTasks([
    T("later", "2026-12-01"), T("done old", "2026-11-01", "mid", 10), T("today low", today, "low"), T("today high", today, "high"),
    T("late", "2026-11-01"), T("done new", "2026-11-02", "mid", 20),
  ], today);
  assert.deepEqual(groups.map(g => g.key), ["overdue", "today", "later", "done"]);
  assert.deepEqual(groups[1].items.map(t => t.title), ["today high", "today low"]);
  assert.deepEqual(groups[3].items.map(t => t.title), ["done new", "done old"]);
});


test("old saved deadlines gain a priority and done flag; undated tasks are kept", () => {
  const out = cleanDeadlines([
    { id: "a", title: "Old", date: "2026-12-01", time: "10:00", note: "", created: 1 },
    { id: "b", title: "Task", date: "", time: "10:00", prio: "high", done: 7 },
    { id: "c", title: "Bad prio", date: "2026-12-01", prio: "urgent", done: -1 },
  ]);
  assert.deepEqual(out.map(d => [d.id, d.date, d.time, d.prio, d.done]), [
    ["a", "2026-12-01", "10:00", "mid", 0], ["b", "", "", "high", 7], ["c", "2026-12-01", "", "mid", 0],
  ]);
});

test("pins: one per item, valid refs and dates only, titles filled in both languages", () => {
  assert.deepEqual(cleanPins(undefined), []);
  const out = cleanPins([
    { ref: "opp:x", title: { kk: "Грант" }, date: "2026-12-01", prio: "high" },
    { ref: "opp:x", title: { kk: "Dup" }, date: "2026-12-02" },
    { ref: "deal:y", title: { kk: "No" }, date: "2026-12-01" },
    { ref: "event:e1", title: { kk: "", ru: "" }, date: "2026-12-01" },
    { ref: "event:5", title: { ru: "Форум" }, date: "2026-12-03", time: "15:00", done: 3 },
    { ref: "job:j", title: { kk: "Жұмыс" }, date: "03.12.2026" },
  ]);
  assert.deepEqual(out, [
    { ref: "opp:x", kind: "opp", title: { kk: "Грант", ru: "Грант" }, date: "2026-12-01", time: "", prio: "high", done: 0, created: 0 },
    { ref: "event:5", kind: "event", title: { kk: "Форум", ru: "Форум" }, date: "2026-12-03", time: "15:00", prio: "mid", done: 3, created: 0 },
  ]);
});
