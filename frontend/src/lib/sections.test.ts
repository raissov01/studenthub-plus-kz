import { test } from "node:test";
import assert from "node:assert/strict";
import { eventInTab, jobTab, oppPlace, pickTab, sectionOfView, sectionView, viewOfSearchCat, visibleTabs } from "./sections";
import { pathOf, staticSlugs, viewOfPath } from "./routes";

test("old URLs land on their section; the sub-tab rides in ?t= and the default tab has no query", () => {
  assert.equal(viewOfPath("/jobs"), "opps:jobs");
  assert.equal(viewOfPath("/internships"), "opps:intern");
  assert.equal(viewOfPath("/events"), "opps:events");
  assert.equal(viewOfPath("/courses"), "opps:courses");
  assert.equal(viewOfPath("/discounts"), "bonus");
  assert.equal(sectionOfView("opps", "intern"), "intern");
  assert.equal(sectionOfView("opps", "intern?t=wat"), "intern");
  assert.equal(sectionOfView("bonus", ""), "deals");
  assert.equal(sectionOfView("opps", ""), "jobs");
  assert.equal(pathOf(sectionView("jobs", "parttime")), "/jobs");
  assert.equal(pathOf(sectionView("jobs", "youth")), "/jobs?t=youth");
  assert.equal(pathOf(sectionView("intern", "mobility")), "/internships?t=mobility");
  assert.equal(pathOf(sectionView("deals", "free")), "/discounts?t=free");
  assert.equal(pathOf("opps:intern?t=grants"), "/internships?t=grants");
});

test("/search?k=<old category> maps to the section that now holds it", () => {
  assert.equal(viewOfSearchCat("jobs"), "opps:jobs");
  assert.equal(viewOfSearchCat("mobility"), "opps:intern?t=mobility");
  assert.equal(viewOfSearchCat("grant"), "opps:intern?t=grants");
  assert.equal(viewOfSearchCat("market"), "bonus?t=free");
  assert.equal(viewOfSearchCat("deals"), "bonus");
  assert.equal(viewOfSearchCat("all"), null);
  assert.equal(viewOfSearchCat(null), null);
  assert.equal(pathOf("search:grant"), "/internships?t=grants");
  assert.equal(pathOf("search:all"), "/search");
  assert.equal(pathOf("search"), "/search");
});

test("the section pages stay in the static list (no query in a prerendered path)", () => {
  const paths = staticSlugs().map(s => "/" + s.join("/"));
  for (const p of ["/jobs", "/internships", "/events", "/courses", "/discounts", "/search"]) assert.ok(paths.includes(p), p);
  assert.ok(paths.every(p => !p.includes("?")));
});

test("items go to their sub-tab; absent subtype falls back to the default", () => {
  assert.equal(jobTab({}), "parttime");
  assert.equal(jobTab({ subtype: "youthpractice" }), "youth");
  assert.equal(jobTab({ subtype: "fulltime" }), "fulltime");
  assert.equal(jobTab({ subtype: "unknown" }), "parttime");
  assert.deepEqual(oppPlace({ type: "company" }), { section: "intern", tab: "company" });
  assert.deepEqual(oppPlace({ type: "mobility" }), { section: "intern", tab: "mobility" });
  assert.deepEqual(oppPlace({ type: "intl" }), { section: "intern", tab: "exchange" });
  assert.deepEqual(oppPlace({ type: "intl", subtype: "workandtravel" }), { section: "intern", tab: "wat" });
  assert.deepEqual(oppPlace({ type: "grant" }), { section: "intern", tab: "grants" });
  assert.deepEqual(oppPlace({ type: "grant", subtype: "scholarship" }), { section: "intern", tab: "grants" });
  assert.deepEqual(oppPlace({ type: "grant", subtype: "competition" }), { section: "events", tab: "contests" });
  assert.ok(eventInTab("hackathon", "hackathons"));
  assert.ok(eventInTab("conf", "conf") && eventInTab("career", "conf"));
  assert.ok(!eventInTab("sport", "conf") && eventInTab("sport", "all"));
});

test("optional sub-tabs: full-time only with data, clubs and market behind their switches", () => {
  const off = { jobs: [{}], clubs: false, market: false };
  assert.deepEqual(visibleTabs("jobs", off).map(t => t.id), ["parttime", "youth"]);
  assert.deepEqual(visibleTabs("jobs", { ...off, jobs: [{ subtype: "fulltime" }] }).map(t => t.id), ["parttime", "youth", "fulltime"]);
  assert.deepEqual(visibleTabs("events", off).map(t => t.id), ["all", "contests", "hackathons", "conf"]);
  assert.deepEqual(visibleTabs("events", { ...off, clubs: true }).map(t => t.id), ["all", "contests", "hackathons", "clubs", "conf"]);
  assert.deepEqual(visibleTabs("deals", off).map(t => t.id), ["deals"]);
  assert.deepEqual(visibleTabs("deals", { ...off, market: true }).map(t => t.id), ["deals", "free"]);
  assert.deepEqual(visibleTabs("courses", off), []);
  assert.equal(pickTab(visibleTabs("deals", off), "free"), "deals");
  assert.equal(pickTab(visibleTabs("intern", off), "wat"), "wat");
  assert.equal(pickTab(visibleTabs("intern", off), null), "company");
});
