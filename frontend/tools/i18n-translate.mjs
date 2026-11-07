#!/usr/bin/env node
/* Drafts English for the pairs that have none yet, with the OpenAI API, and writes src/lib/i18n/en.ts.
   Review the result like any other code change: the draft is a starting point, en.ts is the source of truth.

     node tools/i18n-translate.mjs [--limit N]

   The key comes from OPENAI_API_KEY, else from the env file OPENAI_ENV_FILE (default ~/studenthub-platform/.secrets/prod.env);
   it is never printed. Model: OPENAI_MODEL (default gpt-4.1-mini, as the backend). Existing entries are never changed. */
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { extractAll, readEnglish } from './i18n-extract.mjs';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const EN_FILE = path.join(ROOT, 'src', 'lib', 'i18n', 'en.ts');

function apiKey() {
  if (process.env.OPENAI_API_KEY) return process.env.OPENAI_API_KEY;
  const file = process.env.OPENAI_ENV_FILE || path.join(os.homedir(), 'studenthub-platform', '.secrets', 'prod.env');
  const m = /^OPENAI_API_KEY=(.*)$/m.exec(fs.readFileSync(file, 'utf8'));
  if (!m) throw new Error(`OPENAI_API_KEY not found in ${file}`);
  return m[1].trim().replace(/^["']|["']$/g, '');
}

const SYSTEM = `You translate the user interface of "SIVA", a mobile web app for students of Seifullin University
(Astana, Kazakhstan): jobs, internships, events, discounts, calendar, notes, quizzes, an AI helper, a Telegram bot.
For each item you get the Kazakh text (kk) and the Russian text (ru) of the same UI string. Write the English UI text.
Rules:
- Concise, natural app English (buttons: short imperative, e.g. "Save", "Sign in"). Sentence case. No added words.
- Keep product and proper names: SIVA, Telegram, Kaspi, Google, Seifullin, AI, CV, QR, XP, ENT, GPA, city names.
- Keep emoji, symbols (₸, %, →, ·, —), numbers, line breaks, and leading/trailing spaces exactly.
- Guillemets «…» become “…”.
- Placeholders {0}, {1}, … are values filled in at run time and refer to the Kazakh text's holes. Every placeholder
  of kk must appear in the English text. When a placeholder is a count and a noun depends on it, write the noun as
  {N|singular|plural}, e.g. kk "{0} күн қалды" -> "{0} {0|day|days} left". Do not invent placeholders that kk lacks.
- If kk and ru disagree, follow the meaning both share; prefer ru for details.
Answer with a JSON object mapping each item id to its English text, nothing else.`;

function holes(s) { return [...s.matchAll(/\{(\d+)(?:\|[^}]*)?\}/g)].map(m => m[1]); }
function valid(kk, en) {
  if (typeof en !== 'string' || !en.trim()) return false;
  const need = new Set(holes(kk)), have = new Set(holes(en));
  for (const h of need) if (!have.has(h)) return false;
  for (const h of have) if (!need.has(h)) return false;
  return true;
}

async function translateBatch(key, model, items) {
  const input = Object.fromEntries(items.map((p, i) => [String(i), { kk: p.kk, ru: p.ru }]));
  const res = await fetch('https://api.openai.com/v1/chat/completions', {
    method: 'POST',
    headers: { authorization: `Bearer ${key}`, 'content-type': 'application/json' },
    body: JSON.stringify({ model, temperature: 0.2, response_format: { type: 'json_object' },
      messages: [{ role: 'system', content: SYSTEM }, { role: 'user', content: JSON.stringify(input) }] }),
  });
  if (!res.ok) throw new Error(`OpenAI ${res.status}`);
  const j = await res.json();
  const out = JSON.parse(j.choices[0].message.content);
  const got = {};
  items.forEach((p, i) => { if (valid(p.kk, out[String(i)])) got[p.kk] = out[String(i)]; });
  return got;
}

/** en.ts: one entry per line, sorted, so a review diff shows only what changed */
export function writeEnglish(dict) {
  const keys = Object.keys(dict).sort();
  const lines = keys.map(k => `  ${JSON.stringify(k)}: ${JSON.stringify(dict[k])},`);
  fs.writeFileSync(EN_FILE, `/* English UI, keyed by the Kazakh text of tt(kk, ru) / { kk, ru } (see src/lib/i18n/index.ts).
   Drafted by tools/i18n-translate.mjs, reviewed by hand; the test src/lib/i18n/en.test.ts fails on a missing key.
   {0} {1} … are the holes of a template string; {0|day|days} picks the word form for the count in hole 0. */
export const EN: Record<string, string> = {
${lines.join('\n')}
};
`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const limitArg = process.argv.indexOf('--limit');
  const limit = limitArg > 0 ? Number(process.argv[limitArg + 1]) : Infinity;
  const en = readEnglish();
  const todo = extractAll().filter(p => !en[p.kk]).slice(0, limit);
  const key = apiKey();
  const model = process.env.OPENAI_MODEL || 'gpt-4.1-mini';
  console.error(`${todo.length} to translate with ${model}`);
  const BATCH = 60, PARALLEL = 4;
  const batches = [];
  for (let i = 0; i < todo.length; i += BATCH) batches.push(todo.slice(i, i + BATCH));
  let done = 0;
  for (let i = 0; i < batches.length; i += PARALLEL) {
    const results = await Promise.all(batches.slice(i, i + PARALLEL).map(async b => {
      for (let attempt = 0; attempt < 3; attempt++) {
        try { return await translateBatch(key, model, b); } catch (e) { console.error(`batch failed (${e.message}), retrying`); }
      }
      return {};
    }));
    for (const r of results) { Object.assign(en, r); done += Object.keys(r).length; }
    writeEnglish(en);
    console.error(`${done}/${todo.length}`);
  }
  const left = extractAll().filter(p => !en[p.kk]).length;
  console.error(left ? `${left} still missing (run again, or write them by hand)` : 'all pairs have English');
}
