import { test } from "node:test";
import assert from "node:assert/strict";
import { emptyFeedback, onVisit, markAsked, cleanMissing } from "./logic";

test("asks once, on the 3rd distinct visit day", () => {
  let s = emptyFeedback();
  for (const [d, want] of [["2026-10-01", false], ["2026-10-01", false], ["2026-10-02", false], ["2026-10-05", true]] as const) {
    const r = onVisit(s, d);
    s = r.state;
    assert.equal(r.show, want, d);
  }
  s = markAsked(s);
  assert.equal(onVisit(s, "2026-10-06").show, false);
  assert.equal(cleanMissing("  a \n\n b ".padEnd(900, "x")).length, 500);
});
