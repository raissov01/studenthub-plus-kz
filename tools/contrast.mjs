#!/usr/bin/env node
/* WCAG 2.x contrast check for the palette in app/globals.css: reads the token blocks (:root, the light block, the dark
   block), resolves var() chains and checks every text / icon / UI pair the components use, in both themes.
   Exit code 1 when any pair is below its AA minimum. Run: node tools/contrast.mjs [--md] */
import { readFileSync } from 'node:fs';

const css = readFileSync(new URL('../app/globals.css', import.meta.url), 'utf8');
const blocks = { root: {}, light: {}, dark: {} };
const re = /(^|\n)(:root|:root:not\(\[data-theme="dark"\]\)|:root\[data-theme="dark"\])\s*\{([^}]*)\}/g;
for (const m of css.matchAll(re)) {
  const key = m[2] === ':root' ? 'root' : m[2].includes(':not') ? 'light' : 'dark';
  for (const d of m[3].replace(/\/\*[\s\S]*?\*\//g, '').matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)) blocks[key][d[1]] = d[2].trim();
}
const theme = t => ({ ...blocks.root, ...blocks[t] });
function resolve(vars, v, depth = 0) {
  if (depth > 10) throw new Error('var loop: ' + v);
  const m = /^var\((--[\w-]+)\)$/.exec(v);
  if (m) { if (!(m[1] in vars)) throw new Error('unknown token ' + m[1]); return resolve(vars, vars[m[1]], depth + 1); }
  return v;
}
const hex = s => {
  const m = /^#([0-9a-f]{6})$/i.exec(s);
  if (!m) throw new Error('not a #rrggbb colour: ' + s);
  return [0, 2, 4].map(i => parseInt(m[1].slice(i, i + 2), 16) / 255);
};
const lum = c => { const [r, g, b] = hex(c).map(x => x <= 0.04045 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4); return 0.2126 * r + 0.7152 * g + 0.0722 * b; };
const ratio = (a, b) => { const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p); return (x + 0.05) / (y + 0.05); };

const T = 4.5, L = 3; // body text; large text, icons, UI borders, focus
// [foreground, background, minimum, where it is used]
const pairs = [
  ['--text', '--bg', T, 'body text on the page'],
  ['--text', '--surface', T, 'card text'],
  ['--text', '--surface-2', T, 'text on pressed rows / inputs'],
  ['--muted', '--bg', T, 'secondary text on the page'],
  ['--muted', '--surface', T, 'secondary text in cards'],
  ['--muted', '--surface-2', T, 'secondary text on surface-2'],
  ['--muted', '--p-violet', T, 'hero subtitle'],
  ['--text', '--p-violet', T, 'hero title, tile labels (violet)'],
  ['--text', '--p-rose', T, 'tile labels (rose)'],
  ['--text', '--p-mint', T, 'tile labels (mint)'],
  ['--text', '--p-peach', T, 'tile labels (peach)'],
  ['--text', '--p-sky', T, 'tile labels (sky)'],
  ['--text', '--primary-soft', T, 'geo status, hero stats'],
  ['--text', '--accent', T, '.btn.sun / accent'],
  ['--primary', '--surface', T, 'links, text buttons, active tab'],
  ['--primary', '--bg', T, 'links on the page'],
  ['--primary', '--primary-soft', T, 'selected chip, avatar letter, .ico'],
  ['--on-primary', '--primary', T, 'primary button, today, date badge'],
  ['--on-primary', '--primary-press', T, 'pressed primary button'],
  ['--primary', '--surface', L, 'focus ring, selected day border'],
  ['--danger', '--surface', T, 'error text'],
  ['--danger', '--danger-soft', T, 'error on its tint'],
  ['--on-heart', '--heart', T, 'unread counters'],
  ['--on-deep', '--deep', T, 'header'],
  ['--on-deep-muted', '--deep', T, 'header secondary'],
  ['--p-violet-ink', '--p-violet', T, 'violet tile icon, tag, mark letter'],
  ['--p-rose-ink', '--p-rose', T, 'rose tile icon, tag, mark letter'],
  ['--p-mint-ink', '--p-mint', T, 'mint tile icon, tag, mark letter'],
  ['--p-peach-ink', '--p-peach', T, 'peach tile icon, tag, mark letter'],
  ['--p-sky-ink', '--p-sky', T, 'sky tile icon, tag, mark letter'],
  ['--sw-ink', '--sw-sage', T, 'legacy swatch (violet)'],
  ['--sw-ink', '--sw-mauve', T, 'legacy swatch (rose)'],
  ['--sw-ink', '--sw-taupe', T, 'legacy swatch (mint)'],
  ['--sw-ink', '--sw-mist', T, 'legacy swatch (peach)'],
  ['--sw-ink', '--sw-ice', T, 'legacy swatch (sky)'],
  ['--c0', '--surface', L, 'calendar dot: opportunity'],
  ['--c1', '--surface', L, 'calendar dot: event'],
  ['--c2', '--surface', L, 'calendar dot: job'],
  ['--c3', '--surface', L, 'calendar dot: own deadline'],
  ['--c4', '--surface', L, 'category colour 4'],
  ['--c0', '--surface-2', L, 'calendar icon on surface-2'],
  ['--c2', '--surface-2', L, 'calendar icon on surface-2'],
  ['--c1', '--surface-2', L, 'calendar icon on surface-2'],
  ['--c3', '--surface-2', L, 'calendar icon on surface-2'],
  ['--cal-class', '--surface', T, 'calendar kind text/stripe: lessons'],
  ['--cal-class', '--bg', T, 'calendar kind on the page: lessons'],
  ['--surface', '--cal-class', L, 'calendar legend swatch glyph: lessons'],
  ['--cal-opp', '--surface', T, 'calendar kind text/stripe: opportunity'],
  ['--cal-opp', '--bg', T, 'calendar kind on the page: opportunity'],
  ['--surface', '--cal-opp', L, 'calendar legend swatch glyph: opportunity'],
  ['--cal-job', '--surface', T, 'calendar kind text/stripe: job'],
  ['--cal-job', '--bg', T, 'calendar kind on the page: job'],
  ['--surface', '--cal-job', L, 'calendar legend swatch glyph: job'],
  ['--cal-event', '--surface', T, 'calendar kind text/stripe: event'],
  ['--cal-event', '--bg', T, 'calendar kind on the page: event'],
  ['--surface', '--cal-event', L, 'calendar legend swatch glyph: event'],
  ['--cal-exam', '--surface', T, 'calendar kind text/stripe: exam'],
  ['--cal-exam', '--bg', T, 'calendar kind on the page: exam'],
  ['--surface', '--cal-exam', L, 'calendar legend swatch glyph: exam'],
  ['--cal-own', '--surface', T, 'calendar kind text/stripe: own deadline'],
  ['--cal-own', '--bg', T, 'calendar kind on the page: own deadline'],
  ['--surface', '--cal-own', L, 'calendar legend swatch glyph: own deadline'],
  ['--ev-1-ink', '--ev-1-soft', T, 'calendar own-event chip text: colour 1'],
  ['--ev-1', '--surface', L, 'calendar own-event stripe / swatch: colour 1'],
  ['--ev-2-ink', '--ev-2-soft', T, 'calendar own-event chip text: colour 2'],
  ['--ev-2', '--surface', L, 'calendar own-event stripe / swatch: colour 2'],
  ['--ev-3-ink', '--ev-3-soft', T, 'calendar own-event chip text: colour 3'],
  ['--ev-3', '--surface', L, 'calendar own-event stripe / swatch: colour 3'],
  ['--ev-4-ink', '--ev-4-soft', T, 'calendar own-event chip text: colour 4'],
  ['--ev-4', '--surface', L, 'calendar own-event stripe / swatch: colour 4'],
  ['--ev-5-ink', '--ev-5-soft', T, 'calendar own-event chip text: colour 5'],
  ['--ev-5', '--surface', L, 'calendar own-event stripe / swatch: colour 5'],
  ['--ev-6-ink', '--ev-6-soft', T, 'calendar own-event chip text: colour 6'],
  ['--ev-6', '--surface', L, 'calendar own-event stripe / swatch: colour 6'],
  ['--ev-7-ink', '--ev-7-soft', T, 'calendar own-event chip text: colour 7'],
  ['--ev-7', '--surface', L, 'calendar own-event stripe / swatch: colour 7'],
  ['--ev-8-ink', '--ev-8-soft', T, 'calendar own-event chip text: colour 8'],
  ['--ev-8', '--surface', L, 'calendar own-event stripe / swatch: colour 8'],
  ['#FFFFFF', '--map-study', L, 'map: white glyph / stroke on study'],
  ['#FFFFFF', '--map-dorm', L, 'map: white on dorm'],
  ['#FFFFFF', '--map-other', L, 'map: white on other'],
  ['#FFFFFF', '--map-ent', L, 'map: white on entrance / shop'],
  ['#FFFFFF', '--map-rose', L, 'map: white on food / health'],
  ['--map-ink', '#FFFFFF', T, 'map: pin number'],
  ['#FFFFFF', '--map-street', T, 'map: street label halo'],
];

const md = process.argv.includes('--md');
let failed = 0;
for (const t of ['light', 'dark']) {
  const vars = theme(t);
  const val = x => x.startsWith('--') ? resolve(vars, vars[x] ?? (() => { throw new Error('unknown token ' + x); })()) : x;
  const rows = pairs.map(([f, b, min, where]) => {
    const r = ratio(val(f), val(b));
    if (r < min) failed++;
    return { f, b, fv: val(f), bv: val(b), r, min, ok: r >= min, where };
  });
  if (md) {
    console.log(`\n### ${t}\n\n| foreground | background | ratio | min | | used for |\n| --- | --- | ---: | ---: | --- | --- |`);
    for (const x of rows) console.log(`| \`${x.f}\` ${x.fv} | \`${x.b}\` ${x.bv} | ${x.r.toFixed(2)} | ${x.min} | ${x.ok ? 'pass' : '**FAIL**'} | ${x.where} |`);
  } else {
    console.log(`\n${t}`);
    for (const x of rows) console.log(`${x.ok ? ' ok ' : 'FAIL'} ${x.r.toFixed(2).padStart(5)} >= ${x.min}  ${x.f} ${x.fv} on ${x.b} ${x.bv}  (${x.where})`);
  }
}
console.log(failed ? `\n${failed} pair(s) below WCAG AA` : '\nall pairs pass WCAG AA');
process.exit(failed ? 1 : 0);
