import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  compareAgenda, EV_COLORS, evColorName, evColorVars, isEvColor, monthCounts, packCell, sortAgenda, timeRange, yearGrid, yearOptions,
} from "./calview";
import { cleanDeadlines, googleCalendarUrl } from "./calendar";
import { buildOneIcs } from "./ics";

test("year grid: 12 Monday-first months, neighbouring days blank", () => {
  const y = yearGrid(2026);
  assert.equal(y.length, 12);
  assert.deepEqual(y.map(m => m.ref.m), [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11]);
  const nov = y[10];
  // November 2026 starts on a Sunday: six blanks, then the 1st
  assert.deepEqual(nov.weeks[0].map(d => d?.day ?? null), [null, null, null, null, null, null, 1]);
  assert.equal(nov.weeks.flat().filter(Boolean).length, 30);
  assert.ok(y.every(m => m.weeks.every(w => w.length === 7)));
  // February in a leap year
  assert.equal(yearGrid(2028)[1].weeks.flat().filter(Boolean).length, 29);
});

test("month counts and picker years", () => {
  assert.deepEqual(monthCounts(["2026-11-04", "2026-11-30", "2026-01-01", "2027-11-04"], 2026), [1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0]);
  assert.deepEqual(yearOptions(2026, 2026), [2024, 2025, 2026, 2027, 2028, 2029]);
  assert.deepEqual(yearOptions(2026, 2031), [2024, 2025, 2026, 2027, 2028, 2029, 2030, 2031]);
  assert.equal(yearOptions(2026, 2020)[0], 2020);
});

test("cell packing: up to max titles, else max − 1 and «+N»", () => {
  assert.deepEqual(packCell([], 3), { shown: [], more: 0 });
  assert.deepEqual(packCell(["a", "b", "c"], 3), { shown: ["a", "b", "c"], more: 0 });
  assert.deepEqual(packCell(["a", "b", "c", "d"], 3), { shown: ["a", "b"], more: 2 });
  assert.deepEqual(packCell(["a", "b", "c", "d", "e"], 2), { shown: ["a"], more: 4 });
  assert.deepEqual(packCell(["a", "b"], 0), { shown: [], more: 2 }); // a one-line cell: only «+2»
});

test("agenda order: all-day first, then by start, end and title", () => {
  const items = [
    { title: "Лекция", time: "10:30", end: "11:50" },
    { title: "Дедлайн", time: "" },
    { title: "Кездесу", time: "09:00", end: "10:20" },
    { title: "Ерте", time: "09:00", end: "09:30" },
    { title: "Арна", time: "" },
  ];
  assert.deepEqual(sortAgenda(items).map(i => i.title), ["Арна", "Дедлайн", "Ерте", "Кездесу", "Лекция"]);
  assert.equal(items[0].title, "Лекция"); // not sorted in place
  assert.ok(compareAgenda({ title: "a" }, { title: "b", time: "00:00" }) < 0);
  assert.equal(timeRange("09:00", "10:20"), "09:00–10:20");
  assert.equal(timeRange("09:00", "08:00"), "09:00");
  assert.equal(timeRange("", "10:00"), "");
});

test("own-event colours map to tokens defined in both themes", () => {
  assert.equal(EV_COLORS.length, 8);
  assert.deepEqual(evColorVars(2), { solid: "var(--ev-2)", soft: "var(--ev-2-soft)", ink: "var(--ev-2-ink)" });
  assert.deepEqual(evColorVars(42), evColorVars(1));
  assert.equal(isEvColor(8), true);
  assert.equal(isEvColor(0), false);
  assert.equal(isEvColor(1.5), false);
  assert.equal(evColorName(2, "kk"), "Күлгін");
  assert.equal(evColorName(2, "ru"), "Фиолетовый");
  const css = readFileSync(join(process.cwd(), "app/globals.css"), "utf8");
  for (const { n } of EV_COLORS) {
    for (const suffix of ["", "-soft", "-ink"]) {
      const defs = css.match(new RegExp(`--ev-${n}${suffix}:\\s*#[0-9A-Fa-f]{6}`, "g")) ?? [];
      assert.equal(defs.length, 2, `--ev-${n}${suffix} light + dark`);
    }
  }
});

/** WCAG relative luminance contrast of two #RRGGBB colours */
function contrast(a: string, b: string): number {
  const lum = (h: string) => {
    const c = [1, 3, 5].map(i => parseInt(h.slice(i, i + 2), 16) / 255).map(v => (v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
    return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
  };
  const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p);
  return (x + 0.05) / (y + 0.05);
}

test("chip ink passes WCAG AA (4.5:1) on its chip background in light and dark", () => {
  const css = readFileSync(join(process.cwd(), "app/globals.css"), "utf8");
  for (const { n } of EV_COLORS) {
    const soft = [...css.matchAll(new RegExp(`--ev-${n}-soft:\\s*(#[0-9A-Fa-f]{6})`, "g"))].map(m => m[1]);
    const ink = [...css.matchAll(new RegExp(`--ev-${n}-ink:\\s*(#[0-9A-Fa-f]{6})`, "g"))].map(m => m[1]);
    for (let i = 0; i < 2; i++) assert.ok(contrast(ink[i], soft[i]) >= 4.5, `--ev-${n} theme ${i}: ${contrast(ink[i], soft[i]).toFixed(2)}`);
  }
});

test("migration: old deadlines gain end, place and colour 0; own events keep a valid colour and end", () => {
  const out = cleanDeadlines([
    { id: "old", title: "Курстық", date: "2026-12-01", time: "10:00", note: "", created: 1, prio: "high", done: 0 },
    { id: "ev", title: "Кездесу", date: "2026-12-02", time: "09:00", end: "10:20", place: "3-аудитория", color: 5 },
    { id: "bad", title: "Қате", date: "2026-12-02", time: "09:00", end: "08:00", color: 12, place: 7 },
    { id: "noTime", title: "Күн бойы", date: "2026-12-02", end: "10:00", color: 3 },
    { id: "task", title: "Тапсырма", date: "", color: 3 },
  ]);
  assert.deepEqual(out.map(d => [d.id, d.time, d.end, d.place, d.color]), [
    ["old", "10:00", "", "", 0],
    ["ev", "09:00", "10:20", "3-аудитория", 5],
    ["bad", "09:00", "", "", 0],
    ["noTime", "", "", "", 3],
    ["task", "", "", "", 0],
  ]);
  assert.equal(out[0].prio, "high");
});

test("export of an own event: its end time and place", () => {
  const g = new URL(googleCalendarUrl({ title: "Кездесу", date: "2026-12-02", time: "09:00", end: "10:20", location: "3-аудитория" }));
  assert.equal(g.searchParams.get("dates"), "20261202T090000/20261202T102000");
  assert.equal(g.searchParams.get("location"), "3-аудитория");
  const ics = buildOneIcs({ key: "own:ev", date: "2026-12-02", time: "09:00", minutes: 80, summary: "Кездесу", location: "3-аудитория" }, { now: new Date("2026-11-04T00:00:00Z"), lang: "kk" });
  assert.match(ics, /DTEND;TZID=Asia\/Almaty:20261202T102000/);
  assert.match(ics, /LOCATION:3-аудитория/);
});
