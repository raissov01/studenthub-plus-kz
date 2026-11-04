import { test } from "node:test";
import assert from "node:assert/strict";
import { cardKey, cleanState, dueQueue, isDue, MAX_DAYS, MIN_EASE, previewIntervals, review, type SrState } from "./srs";

const T = "2026-11-04";

test("a new card: hard 1 day, good 2, easy 4; again stays due today", () => {
  assert.deepEqual(previewIntervals(undefined), { again: 0, hard: 1, good: 2, easy: 4 });
  const good = review(undefined, "good", T);
  assert.equal(good.due, "2026-11-06");
  assert.equal(good.reps, 1);
  assert.equal(good.ease, 2.5);
  const again = review(undefined, "again", T);
  assert.equal(again.due, T);
  assert.equal(again.lapses, 1);
  assert.equal(again.ease, 2.3);
  assert.ok(isDue(again, T));
  assert.ok(!isDue(good, T));
});

test("intervals grow by the ease factor and keep hard < good < easy", () => {
  let s: SrState | undefined;
  let day = T;
  const seen: number[] = [];
  for (let i = 0; i < 5; i++) {
    const p = previewIntervals(s);
    assert.ok(p.hard < p.good && p.good < p.easy, JSON.stringify(p));
    s = review(s, "good", day);
    seen.push(s.interval);
    day = s.due;
  }
  assert.deepEqual(seen, [2, 5, 13, 33, 83]);
});

test("forgetting resets the streak, lowers ease (never under 1.3) and counts a lapse", () => {
  let s = review(review(undefined, "good", T), "good", "2026-11-06");
  for (let i = 0; i < 10; i++) s = review(s, "again", T);
  assert.equal(s.reps, 0);
  assert.equal(s.ease, MIN_EASE);
  assert.equal(s.lapses, 10);
  assert.equal(previewIntervals(s).good, 2);
});

test("easy raises ease; nothing passes the cap", () => {
  const s = review(undefined, "easy", T);
  assert.equal(s.ease, 2.65);
  const long: SrState = { reps: 9, interval: 300, ease: 3, due: T, lapses: 0, last: T };
  const p = previewIntervals(long);
  assert.ok(p.easy <= MAX_DAYS && p.good <= MAX_DAYS);
});

test("card keys are stable, differ by deck and text, ignore surrounding spaces", () => {
  assert.equal(cardKey("d1", "Медиана", "орта"), cardKey("d1", " Медиана ", "орта "));
  assert.notEqual(cardKey("d1", "Медиана", "орта"), cardKey("d2", "Медиана", "орта"));
  assert.notEqual(cardKey("d1", "Медиана", "орта"), cardKey("d1", "Медиана", "ортасы"));
  assert.match(cardKey("d1", "q", "a"), /^d1:[0-9a-z]+$/);
});

test("today's queue: overdue first, then new cards up to the limit, duplicates and empty cards skipped", () => {
  const deck = { id: "d", cards: [{ q: "a", a: "1" }, { q: "b", a: "2" }, { q: "c", a: "3" }, { q: "a", a: "1" }, { q: "", a: "x" }, { q: "e", a: "5" }] };
  const st: Record<string, SrState> = {
    [cardKey("d", "b", "2")]: { reps: 1, interval: 2, ease: 2.5, due: "2026-11-03", lapses: 0, last: "2026-11-01" },
    [cardKey("d", "c", "3")]: { reps: 1, interval: 2, ease: 2.5, due: "2026-11-09", lapses: 0, last: T },
    [cardKey("d", "e", "5")]: { reps: 1, interval: 2, ease: 2.5, due: "2026-11-01", lapses: 0, last: T },
  };
  const q = dueQueue([deck], st, T, 1);
  assert.deepEqual(q.map(c => c.q), ["e", "b", "a"]);
  assert.deepEqual(q.map(c => c.isNew), [false, false, true]);
  assert.equal(dueQueue([deck], st, T, 0).length, 2);
});

test("stored states are validated", () => {
  assert.equal(cleanState(null), undefined);
  assert.equal(cleanState({ due: "soon" }), undefined);
  assert.deepEqual(cleanState({ due: T, ease: 0.2, reps: -1, interval: 9999 }), { reps: 0, interval: MAX_DAYS, ease: MIN_EASE, due: T, lapses: 0, last: T });
});
