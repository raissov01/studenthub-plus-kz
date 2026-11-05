/* Three UI languages without rewriting the tt(kk, ru) calls: English is a dictionary keyed by the Kazakh text
   (./en.ts, drafted by tools/i18n-translate.mjs). tt() in English: the dictionary entry, else Russian (else the Kazakh
   text itself when it has no Cyrillic letters, e.g. "Student Hub"). The dictionary is a separate chunk, loaded only
   when the language is English; until it arrives English falls back to Russian.
   Template strings: tt(`${n} күн қалды`, …) is extracted as the key "{0} күн қалды"; at run time the Kazakh string is
   matched against such keys and the holes are copied into the English text ("{0} {0|day|days} left").
   Content from the API (opportunities, events, …) has only kk/ru: L() shows its Russian text to English users.
   The same Kazakh text with different meanings gets an extra entry keyed "kk‖ru" (pairKey), which wins. */
import type { Lang, LText } from '@/lib/types';

export const LANGS: Lang[] = ['kk', 'ru', 'en'];
export const isLang = (v: unknown): v is Lang => v === 'kk' || v === 'ru' || v === 'en';

const CYR = /[Ѐ-ӿ]/;

let dict: Record<string, string> | null = null;
let version = 0;
let loading: Promise<void> | null = null;
const listeners = new Set<() => void>();
/** for useSyncExternalStore in client code (this module is also used by server components, so no React here) */
export const subscribeDict = (l: () => void) => { listeners.add(l); return () => { listeners.delete(l); }; };

function install(d: Record<string, string>) {
  dict = d;
  templates = null;
  cache.clear();
  version++;
  listeners.forEach(l => l());
}

/** load the English dictionary (once); resolves when tt() can answer in English */
export function loadEnglish(): Promise<void> {
  if (dict) return Promise.resolve();
  loading ??= import('./en').then(m => install(m.EN), () => { loading = null; });
  return loading;
}
/** changes when the dictionary arrives, so screens bound to English re-render */
export const dictVersion = () => version;
/** tests: use this dictionary instead of ./en */
export function setDictionary(d: Record<string, string> | null) {
  if (d) install(d);
  else { dict = null; templates = null; cache.clear(); version++; }
}

/* ---------- template keys ---------- */
type Template = { re: RegExp; holes: number[]; en: string; weight: number };
let templates: Template[] | null = null;
const cache = new Map<string, string | null>();
const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

function compile(d: Record<string, string>): Template[] {
  const out: Template[] = [];
  for (const [k, en] of Object.entries(d)) {
    if (!/\{\d+\}/.test(k)) continue;
    const parts = k.split(/\{(\d+)\}/);
    let src = '^';
    const holes: number[] = [];
    let weight = 0;
    parts.forEach((p, i) => {
      if (i % 2) { holes.push(Number(p)); src += '([\\s\\S]*?)'; }
      else { src += escape(p); weight += p.length; }
    });
    out.push({ re: new RegExp(src + '$'), holes, en, weight });
  }
  // the most literal text first: "{0} күн қалды" before "{0} {1}"
  return out.sort((a, b) => b.weight - a.weight);
}

/** fill {0} and {0|one|other} with the values taken from the Kazakh string */
export function fill(en: string, values: Record<number, string>): string {
  return en.replace(/\{(\d+)(?:\|([^|}]*)\|([^}]*))?\}/g, (_, i: string, one?: string, other?: string) => {
    const v = values[Number(i)] ?? '';
    if (one === undefined) return v;
    return Number(v.replace(/\s/g, '')) === 1 ? one : (other ?? one);
  });
}

/** the English text for a Kazakh UI string, or null */
export function english(kk: string): string | null {
  if (!dict) return null;
  const hit = dict[kk];
  if (hit !== undefined) return hit;
  const c = cache.get(kk);
  if (c !== undefined) return c;
  templates ??= compile(dict);
  let found: string | null = null;
  for (const t of templates) {
    const m = t.re.exec(kk);
    if (!m) continue;
    const values: Record<number, string> = {};
    t.holes.forEach((h, i) => { values[h] = m[i + 1]; });
    found = fill(t.en, values);
    break;
  }
  if (cache.size > 5000) cache.clear();
  cache.set(kk, found);
  return found;
}

/* ---------- missing keys (development only) ---------- */
const missing = new Set<string>();
let warnTimer: ReturnType<typeof setTimeout> | null = null;
function warnMissing(kk: string) {
  if (process.env.NODE_ENV === 'production' || missing.has(kk)) return;
  missing.add(kk);
  warnTimer ??= setTimeout(() => {
    warnTimer = null;
    console.warn(`[i18n] ${missing.size} UI strings have no English entry (src/lib/i18n/en.ts; run node tools/i18n-extract.mjs --missing):\n` + [...missing].join('\n'));
  }, 2000);
}

/** an entry for one kk/ru pair, when the same Kazakh text means different things («Тіркелу»: sign in / register) */
export const pairKey = (kk: string, ru: string) => `${kk}\u2016${ru}`;

/** tt(kk, ru) for any language */
export function tr(lang: Lang, kk: string, ru: string): string {
  if (lang === 'ru') return ru;
  if (lang !== 'en') return kk;
  const exact = dict?.[pairKey(kk, ru)];
  if (exact !== undefined) return exact;
  const en = english(kk);
  if (en !== null) return en;
  if (dict && CYR.test(kk)) warnMissing(kk);
  return CYR.test(kk) ? (ru || kk) : kk;
}

/** L(): bilingual content in the UI language. English: its own en, the dictionary (strings from our code), else Russian */
export function lt(lang: Lang, o?: LText | string | null): string {
  if (!o) return '';
  if (typeof o === 'string') return o;
  if (lang === 'en') return o.en || (o.kk ? english(o.kk) : null) || o.ru || o.kk || '';
  return o[lang] || o.kk || o.ru || '';
}

/** the language the API answers in: it speaks kk and ru, English users get Russian messages */
export const apiLang = (lang: Lang): 'kk' | 'ru' => (lang === 'kk' ? 'kk' : 'ru');

/** Intl locale of a UI language */
export const locale = (lang: Lang) => (lang === 'en' ? 'en' : lang === 'ru' ? 'ru-RU' : 'kk-KZ');

/** each language in its own name (shown with lang={code}, so screen readers and the English check skip it) */
export const LANG_NAMES: Record<Lang, string> = { kk: 'Қазақша', ru: 'Русский', en: 'English' };
export const LANG_SHORT: Record<Lang, string> = { kk: 'Қаз', ru: 'Рус', en: 'Eng' };
