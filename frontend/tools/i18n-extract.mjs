#!/usr/bin/env node
/* Collects every Kazakh/Russian UI string pair in frontend/src for the English dictionary (src/lib/i18n/en.ts).

     node tools/i18n-extract.mjs            # writes tools/i18n-strings.json: [{ kk, ru, en?, files }]
     node tools/i18n-extract.mjs --missing  # lists pairs without an English entry, exit 1 if any

   A pair is found with the TypeScript parser, not regexes:
   - a call whose first two arguments are strings and the first has Cyrillic letters: tt('…', '…'), t('…', '…'), s(…);
     tr(lang, '…', '…') (the language comes first)
   - a two-string array whose first string has Cyrillic letters: ['Іс-шаралар', 'Мероприятия'] (the Pair type)
   - an object literal with string kk and ru and no en: { kk: '…', ru: '…' } (LText; L() looks it up too)
   Template literals become keys with numbered holes: tt(`${n} күн қалды`, …) -> "{0} күн қалды"; the English entry
   uses the same holes ("{0} days left"), and tt() fills them from the Kazakh string at run time.
   Content from the API is not in the code, so it is never extracted (English users see its Russian text). */
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import ts from 'typescript';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const SRC = path.join(ROOT, 'src');
const OUT = path.join(ROOT, 'tools', 'i18n-strings.json');
const CYR = /[Ѐ-ӿ]/;

function walk(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) walk(p, out);
    else if (/\.(ts|tsx)$/.test(e.name) && !/\.(test|spec)\.tsx?$/.test(e.name) && !p.includes(`${path.sep}i18n${path.sep}`)) out.push(p);
  }
  return out;
}

/** a string literal, a template without holes, or a template with holes -> "{0}" form; else null */
export function textOf(node) {
  if (!node) return null;
  if (ts.isParenthesizedExpression(node)) return textOf(node.expression);
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return node.text;
  if (ts.isTemplateExpression(node)) {
    let s = node.head.text;
    node.templateSpans.forEach((span, i) => { s += `{${i}}` + span.literal.text; });
    return s;
  }
  return null;
}

function propName(p) {
  if (!ts.isPropertyAssignment(p)) return null;
  const n = p.name;
  return ts.isIdentifier(n) || ts.isStringLiteral(n) ? n.text : null;
}

/** every pair in one source text */
export function extractSource(code, fileName = 'x.tsx') {
  const sf = ts.createSourceFile(fileName, code, ts.ScriptTarget.Latest, true, fileName.endsWith('x') ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  const found = [];
  const add = (kk, ru) => { if (kk && ru !== null && CYR.test(kk.replace(/\{\d+\}/g, ''))) found.push({ kk, ru }); };
  const visit = node => {
    if (ts.isCallExpression(node) && node.arguments.length >= 2) {
      // tr(lang, kk, ru) has the language first
      const at = ts.isIdentifier(node.expression) && node.expression.text === 'tr' && node.arguments.length >= 3 ? 1 : 0;
      const kk = textOf(node.arguments[at]), ru = textOf(node.arguments[at + 1]);
      if (kk !== null && ru !== null) add(kk, ru);
    } else if (ts.isArrayLiteralExpression(node) && node.elements.length === 2) {
      const kk = textOf(node.elements[0]), ru = textOf(node.elements[1]);
      if (kk !== null && ru !== null && CYR.test(ru)) add(kk, ru);
    } else if (ts.isObjectLiteralExpression(node)) {
      const props = new Map();
      for (const p of node.properties) { const n = propName(p); if (n) props.set(n, p.initializer); }
      if (props.has('kk') && props.has('ru') && !props.has('en')) {
        const kk = textOf(props.get('kk')), ru = textOf(props.get('ru'));
        if (kk !== null && ru !== null) add(kk, ru);
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return found;
}

/** all pairs in src/, one per Kazakh key (the first Russian text seen wins), sorted by key */
export function extractAll() {
  const byKey = new Map();
  for (const file of walk(SRC)) {
    const rel = path.relative(ROOT, file);
    for (const { kk, ru } of extractSource(fs.readFileSync(file, 'utf8'), file)) {
      const e = byKey.get(kk) || { kk, ru, files: [] };
      if (!e.files.includes(rel)) e.files.push(rel);
      byKey.set(kk, e);
    }
  }
  return [...byKey.values()].sort((a, b) => (a.kk < b.kk ? -1 : a.kk > b.kk ? 1 : 0));
}

/** the keys of src/lib/i18n/en.ts, read as data (no TS loader needed) */
export function readEnglish() {
  const file = path.join(SRC, 'lib', 'i18n', 'en.ts');
  if (!fs.existsSync(file)) return {};
  const code = fs.readFileSync(file, 'utf8');
  const sf = ts.createSourceFile(file, code, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
  const out = {};
  const visit = node => {
    if (ts.isObjectLiteralExpression(node)) {
      for (const p of node.properties) {
        if (ts.isPropertyAssignment(p) && (ts.isStringLiteral(p.name) || ts.isIdentifier(p.name))) {
          const v = textOf(p.initializer);
          if (v !== null) out[p.name.text] = v;
        }
      }
      return;
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return out;
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const pairs = extractAll();
  const en = readEnglish();
  if (process.argv.includes('--missing')) {
    const missing = pairs.filter(p => !en[p.kk]);
    for (const p of missing) console.log(`${p.files[0]}: ${JSON.stringify(p.kk)}`);
    console.error(`${missing.length} of ${pairs.length} pairs have no English entry`);
    process.exit(missing.length ? 1 : 0);
  }
  fs.writeFileSync(OUT, JSON.stringify(pairs.map(p => ({ kk: p.kk, ru: p.ru, en: en[p.kk] ?? null, files: p.files })), null, 1) + '\n');
  console.error(`${pairs.length} pairs -> ${path.relative(ROOT, OUT)} (${pairs.filter(p => en[p.kk]).length} with English)`);
}
