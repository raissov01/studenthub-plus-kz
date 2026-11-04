import { test } from "node:test";
import assert from "node:assert/strict";
import { belongs, cleanMeta, mentions, nextSlot, subjectId, subjectsFrom } from "./subjects";

// a student's own timetable (wd: Monday = 0)
const SCHED = [
  { t: "09:00", end: "09:50", name: "Микроэкономика", room: "4-214", kind: "Дәріс", wd: 0 },
  { t: "10:50", end: "11:40", name: "Ағылшын тілі", room: "4-305", kind: "Практика", wd: 2 },
  { t: "13:00", end: "13:50", name: "Микроэкономика", room: "4-108", kind: "Семинар", wd: 2 },
];

test("timetable subjects get stable ids, one per name, with every class slot", () => {
  const list = subjectsFrom(SCHED, []);
  assert.deepEqual(list.map(s => s.name), ["Микроэкономика", "Ағылшын тілі"]);
  assert.equal(list[0].id, subjectId("Микроэкономика"));
  assert.equal(list[0].slots.length, 2);
  assert.equal(subjectId("Микроэкономика"), subjectId(" микроэкономика "));
  assert.match(list[1].id, /^s[0-9a-z]+$/);
  assert.deepEqual(list[1].names, ["Ағылшын тілі"]);
});

test("own subjects are added, hidden ones dropped, exam dates kept", () => {
  const meta = cleanMeta([
    { id: subjectId("Ағылшын тілі"), name: "", custom: false, exam: "", hidden: true },
    { id: subjectId("Микроэкономика"), name: "", custom: false, exam: "2026-12-20" },
    { id: "c1", name: "Философия", custom: true, exam: "bad" },
    { id: "c1", name: "dup", custom: true, exam: "" },
    { nope: 1 },
  ]);
  assert.equal(meta.length, 3);
  const list = subjectsFrom(SCHED, meta);
  assert.deepEqual(list.map(s => s.name), ["Микроэкономика", "Философия"]);
  assert.equal(list[0].exam, "2026-12-20");
  assert.equal(list[1].exam, "");
  assert.equal(list[1].custom, true);
});

test("links by id when the data has one, else by name", () => {
  const s = subjectsFrom(SCHED, [])[1];
  assert.ok(mentions(s, "Ағылшын тілі: эссе"));
  assert.ok(mentions(s, "ағылшын тілі СӨЖ"));
  assert.ok(!mentions(s, "Статистика"));
  assert.ok(belongs(s, { courseId: s.id }, "anything"));
  assert.ok(!belongs(s, { courseId: "other" }, "Ағылшын тілі"));
  assert.ok(belongs(s, { subject: s.id }, ""));
  assert.ok(belongs(s, {}, "Ағылшын тілі"));
});

test("next class: later today on its weekday, else its next weekday; none without slots", () => {
  const micro = subjectsFrom(SCHED, [])[0];
  const wed = new Date(2026, 10, 4, 10, 0); // Wednesday
  const a = nextSlot(micro, wed)!;
  assert.equal(a.slot.t, "13:00");
  assert.ok(a.today);
  const fri = new Date(2026, 10, 6, 14, 0); // Friday: Micro is on Monday and Wednesday
  const b = nextSlot(micro, fri)!;
  assert.equal(b.slot.t, "09:00");
  assert.equal(b.at.getDay(), 1);
  assert.ok(!b.today);
  const wedLate = nextSlot(micro, new Date(2026, 10, 4, 14, 0))!; // after Wednesday's class -> next Monday
  assert.equal(wedLate.at.getDate(), 9);
  assert.equal(nextSlot({ slots: [] }, wed), null);
});
