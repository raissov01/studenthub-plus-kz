import { test } from "node:test";
import assert from "node:assert/strict";
import { buildScheduleIcs, escText, foldLine, semesterRange } from "./ics";

const cls = (over: Partial<Parameters<typeof buildScheduleIcs>[0][0]> = {}) => ({
  key: "Микроэкономика|09:00|4-214", start: "09:00", end: "09:50", summary: "Микроэкономика (Дәріс)",
  location: "4-корпус, 2-қабат, 4-214 аудитория", description: "Student Hub; үлгі кесте", ...over,
});

test("CRLF only, every line at most 75 octets, unfolds back", () => {
  const long = "Өте ұзын сабақ атауы — ".repeat(12);
  const ics = buildScheduleIcs([cls({ summary: long })], { now: new Date("2026-10-28T06:00:00Z"), calName: "Кесте", lang: "kk" });
  assert.ok(ics.endsWith("\r\n"));
  assert.equal(ics.replace(/\r\n/g, "").includes("\n"), false, "no bare LF");
  assert.equal(ics.replace(/\r\n/g, "").includes("\r"), false, "no bare CR");
  for (const l of ics.split("\r\n")) assert.ok(new TextEncoder().encode(l).length <= 75, l);
  const unfolded = ics.replace(/\r\n /g, "");
  assert.ok(unfolded.includes("SUMMARY:" + escText(long)));
});

test("weekly RRULE until the semester end, TZID Asia/Almaty, stable UID", () => {
  const a = buildScheduleIcs([cls()], { now: new Date("2026-10-28T06:00:00Z"), calName: "K", lang: "kk" });
  const b = buildScheduleIcs([cls({ summary: "Микроэкономика (Лекция)" })], { now: new Date("2026-11-02T06:00:00Z"), calName: "K", lang: "ru" });
  assert.match(a, /\r\nDTSTART;TZID=Asia\/Almaty:20261028T090000\r\n/);
  assert.match(a, /\r\nDTEND;TZID=Asia\/Almaty:20261028T095000\r\n/);
  assert.match(a, /\r\nRRULE:FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR;UNTIL=20261231T185959Z\r\n/);
  assert.match(a, /BEGIN:VTIMEZONE\r\nTZID:Asia\/Almaty\r\n/);
  const uid = (s: string) => /\r\nUID:([^\r]+)\r\n/.exec(s)?.[1];
  assert.equal(uid(a), uid(b));
  assert.match(a, /LOCATION:4-корпус\\, 2-қабат\\, 4-214 аудитория/);
});

test("weekend export starts on Monday; summer exports the autumn semester", () => {
  const sat = buildScheduleIcs([cls()], { now: new Date("2026-10-31T06:00:00Z"), calName: "K", lang: "kk" });
  assert.match(sat, /DTSTART;TZID=Asia\/Almaty:20261102T090000/);
  const r = semesterRange(new Date("2027-07-10T06:00:00Z"));
  assert.equal(r.from.toISOString().slice(0, 10), "2027-09-01");
  const spring = semesterRange(new Date("2027-01-03T06:00:00Z"));
  assert.equal(spring.from.toISOString().slice(0, 10), "2027-01-15");
  assert.equal(spring.until.toISOString().slice(0, 10), "2027-05-31");
});

test("folding never splits a UTF-8 character", () => {
  const f = foldLine("X:" + "ә".repeat(100));
  for (const l of f.split("\r\n")) assert.ok(!l.includes("�") && new TextEncoder().encode(l).length <= 75);
  assert.equal(f.replace(/\r\n /g, ""), "X:" + "ә".repeat(100));
});
