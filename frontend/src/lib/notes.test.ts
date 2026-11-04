import { test } from "node:test";
import assert from "node:assert/strict";
import {
  AI_NOTE_MAX, aiPrompt, block, cleanNote, fileName, newNote, noteTitle, noteToMarkdown, noteToText, orderedBlocks, parseAiResult,
  plainText, searchNotes, templateBlocks, type Note,
} from "./notes";

const note = (over: Partial<Note> = {}): Note => ({ ...newNote("lecture", "s1", "kk", 1000), ...over });

test("templates: lecture and seminar headings in the note's language, Cornell has the three zones", () => {
  const lec = templateBlocks("lecture", "ru");
  assert.deepEqual(lec.filter(b => b.t === "h").map(b => b.text), ["Главные мысли", "Термины", "Вопросы"]);
  const sem = templateBlocks("seminar", "kk");
  assert.ok(sem.some(b => b.t === "todo") && sem.some(b => b.t === "callout"));
  assert.equal(sem[0].text, "Тапсырма");
  const cor = templateBlocks("cornell", "kk");
  assert.deepEqual(cor.map(b => b.zone), ["cue", "main", "sum"]);
  assert.equal(new Set([...lec, ...sem, ...cor].map(b => b.id)).size, lec.length + sem.length + cor.length);
});

test("title falls back to template and date", () => {
  assert.equal(noteTitle({ title: "", template: "cornell", date: "2026-11-04" }, "kk"), "Cornell 04.11.2026");
  assert.equal(noteTitle({ title: " Сұраныс ", template: "lecture", date: "" }, "kk"), "Сұраныс");
});

test("Markdown export: headings, lists with one indent level, checklist, callout, image placeholder; empty slots skipped", () => {
  const n = note({
    title: "Сұраныс заңы", date: "2026-11-04", classT: "09:00",
    blocks: [
      block("h", "Негізгі ойлар"), block("li", "Баға өссе"), block("li", "сұраныс азаяды", { indent: 1 }), block("li", ""),
      block("todo", "Кестені қайталау", { checked: true }), block("callout", "Емтиханда болады"), block("p", "# не тақырып"),
      block("img", "тақта", { img: "i1" }),
    ],
  });
  const md = noteToMarkdown(n, { subjectName: "Микроэкономика", lang: "kk" });
  assert.equal(md, [
    "# Сұраныс заңы", "", "_Микроэкономика · Лекция · 2026-11-04 09:00_", "",
    "## Негізгі ойлар", "", "- Баға өссе", "  - сұраныс азаяды", "- [x] Кестені қайталау", "",
    "> **Маңызды:** Емтиханда болады", "", "\\# не тақырып", "", "*[Сурет: тақта]*", "",
  ].join("\n"));
  const txt = noteToText(n, { subjectName: "Микроэкономика", lang: "kk" });
  assert.match(txt, /^Сұраныс заңы\n\nМикроэкономика · Лекция/);
  assert.match(txt, /НЕГІЗГІ ОЙЛАР\n\n• Баға өссе\n {4}• сұраныс азаяды\n\[x\] Кестені қайталау/);
  assert.match(txt, /! Маңызды: Емтиханда болады/);
});

test("Cornell export groups the zones under their headings", () => {
  const n = note({ template: "cornell", title: "C", blocks: [block("p", "негізгі", { zone: "main" }), block("p", "не?", { zone: "cue" }), block("p", "итог", { zone: "sum" })] });
  assert.deepEqual(orderedBlocks(n).map(b => b.text), ["не?", "негізгі", "итог"]);
  const md = noteToMarkdown(n, { subjectName: "S", lang: "ru" });
  assert.ok(md.indexOf("## Вопросы и ключевые слова\n\nне?") < md.indexOf("## Записи\n\nнегізгі"));
  assert.ok(md.includes("## Итог\n\nитог"));
  assert.equal(plainText(n), "C\nне?\nнегізгі\nитог");
});

test("file names are safe and keep Cyrillic", () => {
  assert.equal(fileName({ title: "Сұраныс / заңы: 1?", template: "lecture", date: "" }, "kk", "md"), "сұраныс-заңы-1.md");
  assert.equal(fileName({ title: "///", template: "lecture", date: "" }, "kk", "txt"), "note.txt");
});

test("search: every word, any case, newest first, with a snippet", () => {
  const a = note({ id: "a", title: "Икемділік", updated: 1, blocks: [block("p", "Сұраныстың баға бойынша икемділігі")] });
  const b = note({ id: "b", title: "Статистика", updated: 2, blocks: [block("p", "Медиана және мода")] });
  const c = note({ id: "c", title: "Ещё", updated: 3, blocks: [block("p", "ёлка и сұраныс")] });
  assert.deepEqual(searchNotes([a, b, c], "").map(h => h.note.id), ["c", "b", "a"]);
  assert.deepEqual(searchNotes([a, b, c], "СҰРАНЫС").map(h => h.note.id), ["c", "a"]);
  assert.deepEqual(searchNotes([a, b, c], "сұраныс баға").map(h => h.note.id), ["a"]);
  assert.deepEqual(searchNotes([a, b, c], "елка").map(h => h.note.id), ["c"]);
  assert.match(searchNotes([a], "баға")[0].snippet, /баға бойынша/);
  assert.equal(searchNotes([a, b], "химия").length, 0);
});

test("stored notes are validated", () => {
  assert.equal(cleanNote(null), null);
  assert.equal(cleanNote({ id: "x" }), null);
  const n = cleanNote({ id: "x", subject: "s", template: "weird", blocks: [{ id: "1", t: "li", text: "a", indent: 1 }, { id: "2", t: "p", text: "b", indent: 1 }, { id: "3", t: "img" }, { id: "4", t: "bad" }], date: "x" })!;
  assert.equal(n.template, "lecture");
  assert.deepEqual(n.blocks, [{ id: "1", t: "li", text: "a", indent: 1 }, { id: "2", t: "p", text: "b" }]);
  assert.equal(n.date, "");
});

test("AI prompt fits the chat limit and says when the note was cut", () => {
  const short = aiPrompt("Медиана — орта мән", "Статистика", "kk");
  assert.equal(short.cut, false);
  assert.match(short.prompt, /ҚОРЫТЫНДЫ:/);
  const long = aiPrompt("а".repeat(5000), "Статистика", "ru");
  assert.equal(long.cut, true);
  assert.ok(long.prompt.length <= 2000, String(long.prompt.length));
  assert.ok(long.prompt.endsWith("а".repeat(AI_NOTE_MAX)));
});

test("AI answer parsing: requested format, markdown noise, dash lines, two-line pairs", () => {
  const r = parseAiResult([
    "**ҚОРЫТЫНДЫ:**", "Сұраныс баға өскенде азаяды.", "Икемділік — өзгеріске жауап.", "",
    "**КАРТОЧКАЛАР:**", "1. С: Сұраныс заңы | Ж: Баға өссе, сұраныс азаяды", "- С: Икемділік | Ж: пайыздық жауап",
    "Тепе-теңдік — сұраныс = ұсыныс", "С: Шекті шығын", "Ж: тағы бір бірліктің құны", "С: Сұраныс заңы | Ж: қайталау",
  ].join("\n"));
  assert.equal(r.summary, "Сұраныс баға өскенде азаяды. Икемділік — өзгеріске жауап.");
  assert.deepEqual(r.cards, [
    { q: "Сұраныс заңы", a: "Баға өссе, сұраныс азаяды" },
    { q: "Икемділік", a: "пайыздық жауап" },
    { q: "Тепе-теңдік", a: "сұраныс = ұсыныс" },
    { q: "Шекті шығын", a: "тағы бір бірліктің құны" },
  ]);
  const ru = parseAiResult("ИТОГ: Коротко.\nКАРТОЧКИ:\nВ: Мода | О: самое частое значение");
  assert.equal(ru.summary, "Коротко.");
  assert.deepEqual(ru.cards, [{ q: "Мода", a: "самое частое значение" }]);
  assert.deepEqual(parseAiResult(""), { summary: "", cards: [] });
});
