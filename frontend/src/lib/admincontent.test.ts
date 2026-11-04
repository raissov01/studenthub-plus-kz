import { test } from "node:test";
import assert from "node:assert/strict";
import { bodyOf, check, emptyForm, formOfItem, previewOf, slotOf } from "./admincontent";
import { checkSug, emptySug, fieldOfCode, sugOfPayload, sugPayload } from "./suggest";

const filledOpp = () => {
  const f = emptyForm("opportunities");
  Object.assign(f, {
    type: "intl", title: { kk: " Жазғы мектеп ", ru: "" }, city: "almaty", source_url: "https://uni.kz/summer", deadline: "2026-11-27",
    course: "2", gpa: "3,2", en: "B1", docs: [{ kk: "Транскрипт", ru: "" }, { kk: "", ru: "" }], steps: [{ kk: "Өтінім", ru: "Заявка" }],
  });
  return f;
};

test("a filled opportunity passes and becomes the data/*.json record", () => {
  const f = filledOpp();
  assert.deepEqual(check(f), {});
  const b = bodyOf(f);
  assert.equal(b.kind, "opportunities");
  assert.equal(b.no_deadline, false);
  assert.deepEqual(b.record.title, { kk: "Жазғы мектеп", ru: "" });
  assert.deepEqual(b.record.req, { course: 2, gpa: 3.2, en: "B1" });
  // empty rows dropped, an empty ru takes the kk text (the server pairs the lists by position)
  assert.deepEqual(b.record.docs, [{ kk: "Транскрипт", ru: "Транскрипт" }]);
  assert.equal(b.record.deadline, "2026-11-27");
});

test("the docs table rules mark the right fields", () => {
  const f = filledOpp();
  Object.assign(f, { source_url: "http://uni.kz", course: "9", gpa: "5", email: "a b@c.kz", site: "uni.kz", deadline: "", steps: [{ kk: "", ru: "Только ru" }] });
  const e = check(f);
  assert.deepEqual(Object.keys(e).sort(), ["contact.email", "contact.site", "deadline", "req.course", "req.gpa", "source_url", "steps[]"].sort());
  f.noDeadline = true;
  assert.equal(check(f).deadline, undefined);
  f.docs = [{ kk: "a | b", ru: "" }];
  assert.ok(check(f)["docs[]"][0].includes("|"));
});

test("jobs: hours only required per shift; discounts need cat and pct", () => {
  const j = emptyForm("jobs");
  Object.assign(j, { title: { kk: "Бариста", ru: "" }, city: "astana", source_url: "https://a.kz", noDeadline: true, pay: "9000", per: "shift" });
  assert.deepEqual(Object.keys(check(j)), ["hours"]);
  j.per = "month";
  assert.deepEqual(check(j), {});
  j.today = true;
  assert.equal(bodyOf(j).record.today, true);
  const d = emptyForm("discounts");
  Object.assign(d, { title: { kk: "Кофе", ru: "" }, city: "astana", source_url: "https://a.kz", noDeadline: true });
  assert.deepEqual(Object.keys(check(d)).sort(), ["cat", "pct"]);
  assert.deepEqual(bodyOf({ ...d, cat: "food", pct: "−15%" }).record.name, { kk: "Кофе", ru: "" });
});

test("item -> form -> body keeps the record", () => {
  const rec = {
    id: "adm-3", type: "grant", funded: true, title: { kk: "Грант", ru: "Грант" }, org: { kk: "Қор", ru: "Фонд" }, req: { course: 1 }, spec: ["all"],
    desc: { kk: "Сипат", ru: "Описание" }, docs: [], steps: [], source_url: "https://f.kz", posted_at: "2026-10-20", verified_at: null, verified_by: "",
    contact: { phone: "", email: "x@f.kz", site: "" }, is_demo: false, deadline: null, city: "astana", source: "admin",
  };
  const f = formOfItem("opportunities", "hidden", true, rec);
  assert.equal(f.status, "hidden");
  assert.equal(f.noDeadline, true);
  assert.deepEqual(f.spec, []);
  const b = bodyOf(f);
  assert.equal(b.status, "hidden");
  assert.equal(b.record.deadline, null);
  assert.equal("id" in b.record || "source" in b.record, false);
  const p = previewOf(f);
  assert.equal(p.id, "preview");
  assert.deepEqual((p as { spec: string[] }).spec, ["all"]);
});

test("server paths land in the form's slots", () => {
  assert.equal(slotOf("docs[].ru"), "docs[]");
  assert.equal(slotOf("no_deadline"), "deadline");
  assert.equal(slotOf("name.kk"), "title.kk");
  assert.equal(slotOf("contact.email"), "contact.email");
});

test("suggestion rules and payload", () => {
  const f = emptySug();
  Object.assign(f, { type: "grant", title_kk: "Гр", org_kk: "Қ", desc_kk: "қысқа", source_url: "http://x.kz", deadline: "2026-01-01" });
  assert.deepEqual(Object.keys(checkSug("opportunity", f, "2026-10-28")).sort(), ["deadline", "desc_kk", "org_kk", "source_url", "title_kk"]);
  Object.assign(f, { title_kk: "Грант 2026", org_kk: "Қор", desc_kk: "Студенттерге арналған грант", source_url: "https://x.kz", deadline: "2026-12-01", city: "astana" });
  assert.deepEqual(checkSug("opportunity", f, "2026-10-28"), {});
  const p = sugPayload("opportunity", { ...f, noDeadline: true });
  assert.equal(p.deadline, undefined);
  assert.equal(p.free, true);
  assert.equal("name_kk" in p, false);
  assert.equal(sugOfPayload({ ...p, author_role: "student" }).noDeadline, true);
  assert.equal(fieldOfCode("invalid_title_kk"), "title_kk");
  assert.equal(fieldOfCode("invalid_submission"), "");
  const place = { ...emptySug(), cat: "print", name_kk: "Баспа", address: "Жеңіс 62" };
  assert.deepEqual(checkSug("place", place, "2026-10-28"), {});
});
