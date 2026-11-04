import { test } from "node:test";
import assert from "node:assert/strict";
import {
  addDays, agenda, almatyHm, almatyYmd, byDay, cleanDeadlines, daysBetween, googleCalendarUrl, monthGrid, shiftMonth, weekdayMon,
} from "./calendar";
import { buildOneIcs } from "./ics";

test("Almaty day and time do not depend on the device zone (UTC+5)", () => {
  assert.equal(almatyYmd(new Date("2026-11-04T18:59:00Z")), "2026-11-04");
  assert.equal(almatyYmd(new Date("2026-11-04T19:00:00Z")), "2026-11-05");
  assert.equal(almatyYmd("2026-11-05T09:30:00+05:00"), "2026-11-05");
  assert.equal(almatyHm("2026-11-05T04:30:00Z"), "09:30");
});

test("date arithmetic crosses months and years", () => {
  assert.equal(addDays("2026-12-31", 1), "2027-01-01");
  assert.equal(addDays("2028-03-01", -1), "2028-02-29");
  assert.equal(daysBetween("2026-11-04", "2026-12-04"), 30);
  assert.deepEqual(shiftMonth({ y: 2026, m: 0 }, -1), { y: 2025, m: 11 });
  assert.deepEqual(shiftMonth({ y: 2026, m: 11 }, 1), { y: 2027, m: 0 });
});

test("weekdayMon: Monday = 0, Sunday = 6", () => {
  assert.equal(weekdayMon("2026-11-02"), 0); // Monday
  assert.equal(weekdayMon("2026-11-08"), 6); // Sunday
});

test("month grid: whole Monday-first weeks, only the month's days are inMonth", () => {
  const g = monthGrid({ y: 2026, m: 10 }); // November 2026 starts on a Sunday
  assert.equal(g[0][0].ymd, "2026-10-26");
  assert.equal(g[0][6].ymd, "2026-11-01");
  assert.equal(g[0][6].inMonth, true);
  assert.equal(g[0][5].inMonth, false);
  assert.ok(g.every(w => w.length === 7 && weekdayMon(w[0].ymd) === 0));
  const last = g.at(-1)!;
  assert.equal(last[6].ymd, "2026-12-06");
  assert.equal(g.flat().filter(d => d.inMonth).length, 30);
  assert.equal(g.length, 6);
  // February 2027 starts on a Monday and fills exactly four weeks
  const f = monthGrid({ y: 2027, m: 1 });
  assert.equal(f.length, 4);
  assert.equal(f[0][0].ymd, "2027-02-01");
});

test("byDay sorts all-day first, then by time; agenda keeps only the window", () => {
  const items = [
    { id: "a", date: "2026-11-05", time: "14:00" },
    { id: "b", date: "2026-11-05", time: "" },
    { id: "c", date: "2026-11-06", time: "09:00" },
    { id: "d", date: "2026-11-20", time: "" },
    { id: "e", date: "2026-11-03", time: "" },
  ];
  assert.deepEqual(byDay(items).get("2026-11-05")!.map(i => i.id), ["b", "a"]);
  const ag = agenda(items, "2026-11-04", 7);
  assert.deepEqual(ag.map(([d]) => d), ["2026-11-05", "2026-11-06"]);
});

test("cleanDeadlines keeps valid entries and drops garbage (old saved state)", () => {
  assert.deepEqual(cleanDeadlines(undefined), []);
  assert.deepEqual(cleanDeadlines({}), []);
  const out = cleanDeadlines([
    { id: "x", title: "Курстық жұмыс", date: "2026-12-01", time: "25:00", note: 5 },
    { id: "y", title: "  ", date: "2026-12-01" },
    { id: "z", title: "Есеп", date: "01.12.2026" },
    null,
  ]);
  assert.deepEqual(out, [{ id: "x", title: "Курстық жұмыс", date: "2026-12-01", time: "", note: "", created: 0 }]);
});

test("Google Calendar link: all-day spans to the next day; timed is one hour in Asia/Almaty", () => {
  const all = new URL(googleCalendarUrl({ title: "Өтінім мерзімі", date: "2026-12-31", details: "Student Hub" }));
  assert.equal(all.origin + all.pathname, "https://calendar.google.com/calendar/render");
  assert.equal(all.searchParams.get("action"), "TEMPLATE");
  assert.equal(all.searchParams.get("text"), "Өтінім мерзімі");
  assert.equal(all.searchParams.get("dates"), "20261231/20270101");
  assert.equal(all.searchParams.get("details"), "Student Hub");
  assert.equal(all.searchParams.get("ctz"), null);
  const timed = new URL(googleCalendarUrl({ title: "Емтихан", date: "2026-11-05", time: "23:30" }));
  assert.equal(timed.searchParams.get("dates"), "20261105T233000/20261106T003000");
  assert.equal(timed.searchParams.get("ctz"), "Asia/Almaty");
});

test(".ics for one item: all-day DATE or TZID wall clock, CRLF, stable UID", () => {
  const now = new Date("2026-11-04T06:00:00Z");
  const all = buildOneIcs({ key: "opp:o1", date: "2026-12-31", summary: "Грант; өтінім", description: "a\nb" }, { now, lang: "kk" });
  assert.ok(all.includes("DTSTART;VALUE=DATE:20261231\r\n"));
  assert.ok(all.includes("DTEND;VALUE=DATE:20270101\r\n"));
  assert.ok(all.includes("SUMMARY:Грант\\; өтінім\r\n"));
  assert.ok(all.includes("DESCRIPTION:a\\nb\r\n"));
  assert.ok(!all.includes("VTIMEZONE"));
  assert.equal(all.replace(/\r\n/g, "").includes("\n"), false);
  const timed = buildOneIcs({ key: "own:1", date: "2026-11-05", time: "23:30", summary: "Емтихан" }, { now, lang: "ru" });
  assert.ok(timed.includes("BEGIN:VTIMEZONE"));
  assert.ok(timed.includes("DTSTART;TZID=Asia/Almaty:20261105T233000\r\n"));
  assert.ok(timed.includes("DTEND;TZID=Asia/Almaty:20261106T003000\r\n"));
  const uid = (s: string) => s.match(/UID:(.+)\r\n/)![1];
  assert.equal(uid(timed), uid(buildOneIcs({ key: "own:1", date: "2027-01-01", summary: "x" }, { now, lang: "kk" })));
});
