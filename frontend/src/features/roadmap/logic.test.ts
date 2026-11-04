import { test } from "node:test";
import assert from "node:assert/strict";
import { ITEMS, CATEGORIES } from "./items";
import { emptyRoadmap, toggleChosen, weeklySteps, completeStep, streak, pause, resume, isPaused, weekKey, progress, statusLine } from "./logic";

const day = (iso: string) => new Date(iso + "T06:00:00Z"); // 11:00 Almaty
const weeksLater = (d: Date, w: number) => new Date(d.getTime() + w * 7 * 86400000);

test("24 items in 6 categories, every item has steps and kk/ru text", () => {
  assert.equal(ITEMS.length, 24);
  assert.equal(new Set(ITEMS.map((i) => i.cat)).size, 6);
  assert.equal(Object.keys(CATEGORIES).length, 6);
  for (const i of ITEMS) {
    assert.ok(i.steps.length >= 1 && i.title.kk && i.title.ru, i.key);
    for (const st of i.steps) assert.ok(st.kk && st.ru, i.key);
  }
  assert.equal(new Set(ITEMS.map((i) => i.key)).size, 24);
});

test("chooses at most 5 items and shows at most 3 steps a week", () => {
  let s = emptyRoadmap();
  for (const k of ["internship", "mobility", "ielts", "sport", "journal", "travel"]) s = toggleChosen(s, k);
  assert.equal(s.chosen.length, 5);
  const steps = weeklySteps(s, day("2026-10-28"));
  assert.equal(steps.length, 3);
  assert.deepEqual(steps.map((x) => x.index), [0, 0, 0]);
  s = completeStep(s, "internship", 0, day("2026-10-28"));
  assert.deepEqual(weeklySteps(s, day("2026-10-28"))[0], { item: "internship", index: 1 });
});

test("week keys follow Almaty time and ISO weeks", () => {
  assert.equal(weekKey(day("2026-10-28")), "2026-W44");
  assert.equal(weekKey(new Date("2026-11-01T19:30:00Z")), "2026-W45"); // Monday 00:30 in Almaty
  assert.equal(weekKey(day("2027-01-01")), "2026-W53");
});

test("one missed week does not reset the streak, two do", () => {
  const w0 = day("2026-09-02");
  let s = toggleChosen(toggleChosen(toggleChosen(emptyRoadmap(), "sport"), "journal"), "ielts");
  s = completeStep(s, "sport", 0, w0);
  s = completeStep(s, "journal", 0, weeksLater(w0, 1));
  // week 2 missed
  s = completeStep(s, "ielts", 0, weeksLater(w0, 3));
  assert.equal(streak(s, weeksLater(w0, 3)), 3);
  assert.equal(streak(s, weeksLater(w0, 4)), 3, "current empty week is not a miss yet");
  assert.equal(streak(s, weeksLater(w0, 6)), 0, "two empty weeks end the streak");
});

test("exam pause hides steps and its weeks never count as missed", () => {
  const w0 = day("2026-12-02");
  let s = toggleChosen(toggleChosen(toggleChosen(emptyRoadmap(), "sport"), "journal"), "ielts");
  s = completeStep(s, "sport", 0, w0);
  s = pause(s, weeksLater(w0, 1), 3);
  assert.ok(isPaused(s, weeksLater(w0, 2)));
  assert.deepEqual(weeklySteps(s, weeksLater(w0, 2)), []);
  assert.match(statusLine(s, weeksLater(w0, 2), "kk"), /сәтті/);
  s = resume(s);
  s = completeStep(s, "journal", 0, weeksLater(w0, 4));
  assert.equal(streak(s, weeksLater(w0, 4)), 2);
});

test("progress counts only chosen items and texts never blame", () => {
  let s = toggleChosen(toggleChosen(emptyRoadmap(), "photos"), "journal");
  s = completeStep(s, "photos", 0, day("2026-10-28"));
  assert.deepEqual(progress(s), { done: 1, total: 2, itemsDone: 1 });
  for (const lang of ["kk", "ru"] as const) {
    for (const t of [statusLine(emptyRoadmap(), day("2026-10-28"), lang), statusLine(s, weeksLater(day("2026-10-28"), 1), lang)]) {
      assert.doesNotMatch(t, /ұмыт|жалқау|үлгермеді|забыл|ленив|провал/i);
    }
  }
});
