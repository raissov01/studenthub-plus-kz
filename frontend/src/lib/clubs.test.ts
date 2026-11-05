import { test } from "node:test";
import assert from "node:assert/strict";
import { DIRECTIONS, filterClubs, NO_FILTER, recruitingPick, safeSocial, usedBuildings, usedDirections, type Club } from "./clubs";

const club = (id: number, o: Partial<Club>): Club => ({
  id, name_kk: "Клуб " + id, name_ru: null, direction: "other", summary_kk: null, summary_ru: null, about_kk: null, about_ru: null,
  university: "U", building_id: null, room: null, schedule_kk: null, schedule_ru: null, leader_title_kk: null, leader_title_ru: null,
  leader_contact: null, leader_contact_public: false, telegram_url: null, instagram_url: null, join_mode: "request", join_link: null,
  recruiting: false, status: "active", source_url: null, verified_at: null, members: 0, ...o,
});
const LIST = [
  club(1, { name_kk: "«Шырақ» пікір-сайыс клубы", name_ru: "Дебатный клуб «Шырақ»", direction: "debate", building_id: "b1" }),
  club(2, { name_kk: "Python үйірмесі", name_ru: "Кружок Python", direction: "it", building_id: "b5", recruiting: true, summary_ru: "Моделирование задач" }),
  club(3, { name_kk: "Спорт клубы", direction: "sport", building_id: "b5" }),
  club(4, { name_kk: "Ағылшын клубы", name_ru: "English speaking club", direction: "languages" }),
];

test("no filter: every club, recruiting first, then by name", () => {
  const out = filterClubs(LIST, NO_FILTER, "kk").map(c => c.id);
  assert.equal(out[0], 2);
  assert.deepEqual(new Set(out), new Set([1, 2, 3, 4]));
});

test("direction and building chips combine", () => {
  assert.deepEqual(filterClubs(LIST, { ...NO_FILTER, direction: "sport" }, "kk").map(c => c.id), [3]);
  assert.deepEqual(filterClubs(LIST, { ...NO_FILTER, building: "b5" }, "kk").map(c => c.id), [2, 3]);
  assert.deepEqual(filterClubs(LIST, { direction: "it", building: "b1", q: "" }, "kk"), []);
  assert.deepEqual(filterClubs(LIST, { ...NO_FILTER, building: "none" }, "kk").map(c => c.id), [4]);
  assert.deepEqual(filterClubs(LIST, { ...NO_FILTER, recruiting: true }, "kk").map(c => c.id), [2]);
});

test("search matches both languages, quotes and case, every word", () => {
  assert.deepEqual(filterClubs(LIST, { ...NO_FILTER, q: "шырақ" }, "kk").map(c => c.id), [1]);
  assert.deepEqual(filterClubs(LIST, { ...NO_FILTER, q: "ДЕБАТНЫЙ" }, "ru").map(c => c.id), [1]);
  assert.deepEqual(filterClubs(LIST, { ...NO_FILTER, q: "english club" }, "ru").map(c => c.id), [4]);
  assert.deepEqual(filterClubs(LIST, { ...NO_FILTER, q: "моделирование" }, "ru").map(c => c.id), [2]);
  // the direction label is searchable too
  assert.deepEqual(filterClubs(LIST, { ...NO_FILTER, q: "спорт" }, "kk").map(c => c.id), [3]);
  assert.deepEqual(filterClubs(LIST, { ...NO_FILTER, q: "шахмат" }, "kk"), []);
});

test("chips list only what the catalogue has; one recruiting club for the home carousel", () => {
  assert.deepEqual(usedDirections(LIST).map(d => d.id), ["it", "debate", "sport", "languages"]);
  assert.deepEqual(usedBuildings(LIST), ["b1", "b5"]);
  assert.equal(recruitingPick(LIST)?.id, 2);
  assert.equal(recruitingPick([LIST[0]]), null);
  assert.equal(DIRECTIONS.length, 12);
});

test("only Telegram and Instagram links open", () => {
  assert.equal(safeSocial("https://t.me/club"), "https://t.me/club");
  assert.equal(safeSocial("https://www.instagram.com/club/"), "https://www.instagram.com/club/");
  assert.equal(safeSocial("javascript:alert(1)"), null);
  assert.equal(safeSocial("https://t.me.evil.com/x"), null);
});
