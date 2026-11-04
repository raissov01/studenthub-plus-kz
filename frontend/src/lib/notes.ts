/* «Лекция конспектісі»: the note model, the three templates, Markdown / plain-text export, search, and the
   «Қорытып, карточка жаса» prompt and its parser. Pure: no DOM, no storage, no React (storage is lib/studydb.ts).

   A note is a flat list of blocks. The only nesting is one indent level on list and checklist items — no pages
   inside pages, no databases. A Cornell note splits its blocks into three zones: cues | notes | summary. */

export type BlockType = 'p' | 'h' | 'li' | 'todo' | 'callout' | 'img';
export const BLOCK_TYPES: BlockType[] = ['p', 'h', 'li', 'todo', 'callout', 'img'];
/** Cornell zones; the other templates use only 'main' */
export type Zone = 'cue' | 'main' | 'sum';
export const ZONES: Zone[] = ['cue', 'main', 'sum'];
export type TemplateId = 'lecture' | 'cornell' | 'seminar';
export const TEMPLATES: TemplateId[] = ['lecture', 'cornell', 'seminar'];
type L2 = 'kk' | 'ru';
type Tr = { kk: string; ru: string };

export interface Block {
  id: string;
  t: BlockType;
  text: string;
  /** checklist item done */
  checked?: boolean;
  /** list / checklist item one level in (the only nesting) */
  indent?: 1;
  zone?: Zone;
  /** image block: the id of the compressed WebP in the images store */
  img?: string;
}

export interface Note {
  id: string;
  /** subject id (lib/subjects.ts) */
  subject: string;
  template: TemplateId;
  title: string;
  blocks: Block[];
  /** optional class day 'YYYY-MM-DD' and start time 'HH:MM' the note belongs to */
  date: string;
  classT: string;
  created: number;
  updated: number;
}

export const TEMPLATE_LABEL: Record<TemplateId, Tr> = {
  lecture: { kk: 'Лекция', ru: 'Лекция' },
  cornell: { kk: 'Cornell', ru: 'Корнелл' },
  seminar: { kk: 'Семинар / СӨЖ', ru: 'Семинар / СРС' },
};
export const TEMPLATE_HINT: Record<TemplateId, Tr> = {
  lecture: { kk: 'Негізгі ойлар, терминдер, сұрақтар', ru: 'Главные мысли, термины, вопросы' },
  cornell: { kk: 'Сұрақтар | жазба | қорытынды', ru: 'Вопросы | записи | итог' },
  seminar: { kk: 'Тапсырма, жоспар, дереккөздер, қорытынды', ru: 'Задание, план, источники, вывод' },
};
export const ZONE_LABEL: Record<Zone, Tr> = {
  cue: { kk: 'Сұрақтар мен кілт сөздер', ru: 'Вопросы и ключевые слова' },
  main: { kk: 'Жазба', ru: 'Записи' },
  sum: { kk: 'Қорытынды', ru: 'Итог' },
};
export const BLOCK_LABEL: Record<BlockType, Tr> = {
  p: { kk: 'Мәтін', ru: 'Текст' },
  h: { kk: 'Тақырыпша', ru: 'Подзаголовок' },
  li: { kk: 'Тізім', ru: 'Список' },
  todo: { kk: 'Чекбокс', ru: 'Чекбокс' },
  callout: { kk: 'Маңызды', ru: 'Важно' },
  img: { kk: 'Сурет', ru: 'Фото' },
};

const tr = (x: Tr, lang: L2) => x[lang];
let seq = 0;
/** a short unique id (time + counter + random), good enough for one device */
export function newId(prefix = 'b'): string {
  seq = (seq + 1) % 1296;
  return prefix + Date.now().toString(36) + seq.toString(36).padStart(2, '0') + Math.floor(Math.random() * 1296).toString(36).padStart(2, '0');
}

export const block = (t: BlockType, text = '', extra: Partial<Block> = {}): Block => ({ id: newId(), t, text, ...extra });

/** the starting blocks of a template, headings in the note's language */
export function templateBlocks(tpl: TemplateId, lang: L2): Block[] {
  const h = (kk: string, ru: string) => block('h', tr({ kk, ru }, lang));
  if (tpl === 'cornell') {
    return [block('p', '', { zone: 'cue' }), block('p', '', { zone: 'main' }), block('p', '', { zone: 'sum' })];
  }
  if (tpl === 'seminar') {
    return [
      h('Тапсырма', 'Задание'), block('p'),
      h('Жоспар', 'План'), block('todo'), block('todo'),
      h('Дереккөздер', 'Источники'), block('li'),
      h('Қорытынды', 'Вывод'), block('callout'),
    ];
  }
  return [
    h('Негізгі ойлар', 'Главные мысли'), block('li'),
    h('Терминдер', 'Термины'), block('li'),
    h('Сұрақтар', 'Вопросы'), block('todo'),
  ];
}

export function newNote(tpl: TemplateId, subject: string, lang: L2, now = Date.now(), date = '', classT = ''): Note {
  return { id: newId('n'), subject, template: tpl, title: '', blocks: templateBlocks(tpl, lang), date, classT, created: now, updated: now };
}

/** the shown title: the student's, else template + date */
export function noteTitle(n: Pick<Note, 'title' | 'template' | 'date'>, lang: L2): string {
  return n.title.trim() || `${tr(TEMPLATE_LABEL[n.template], lang)}${n.date ? ' ' + n.date.split('-').reverse().join('.') : ''}`;
}

/** blocks in display order: Cornell zone by zone, others as stored */
export function orderedBlocks(n: Pick<Note, 'template' | 'blocks'>): Block[] {
  if (n.template !== 'cornell') return n.blocks;
  return ZONES.flatMap(z => n.blocks.filter(b => (b.zone ?? 'main') === z));
}

/* ---------- export ---------- */
const mdEscape = (s: string) => s.replace(/^(#{1,6}\s|>|\s*[-*+]\s|\d+\.\s)/, '\\$1');

function blockMd(b: Block, lang: L2): string {
  const pad = b.indent ? '  ' : '';
  const text = b.text.replace(/\r\n?/g, '\n');
  switch (b.t) {
    case 'h': return '## ' + text.replace(/\n+/g, ' ');
    case 'li': return pad + '- ' + text.replace(/\n/g, '\n' + pad + '  ');
    case 'todo': return pad + (b.checked ? '- [x] ' : '- [ ] ') + text.replace(/\n/g, '\n' + pad + '  ');
    case 'callout': return '> **' + tr(BLOCK_LABEL.callout, lang) + ':** ' + text.replace(/\n/g, '\n> ');
    case 'img': return `*[${tr(BLOCK_LABEL.img, lang)}${text.trim() ? ': ' + text.trim() : ''}]*`;
    default: return text.split('\n').map(mdEscape).join('\n');
  }
}

function blockTxt(b: Block, lang: L2): string {
  const pad = b.indent ? '    ' : '';
  switch (b.t) {
    case 'h': return b.text.toUpperCase();
    case 'li': return pad + '• ' + b.text;
    case 'todo': return pad + (b.checked ? '[x] ' : '[ ] ') + b.text;
    case 'callout': return '! ' + tr(BLOCK_LABEL.callout, lang) + ': ' + b.text;
    case 'img': return `[${tr(BLOCK_LABEL.img, lang)}${b.text.trim() ? ': ' + b.text.trim() : ''}]`;
    default: return b.text;
  }
}

/** consecutive list lines stay together, other blocks are separated by a blank line */
function joinBlocks(blocks: Block[], one: (b: Block) => string): string {
  const out: string[] = [];
  let prev: Block | null = null;
  for (const b of blocks) {
    if (!b.text.trim() && b.t !== 'img') continue; // empty template slots are not exported
    const listy = (x: Block | null) => !!x && (x.t === 'li' || x.t === 'todo');
    if (out.length) out.push(listy(prev) && listy(b) ? '\n' : '\n\n');
    out.push(one(b));
    prev = b;
  }
  return out.join('');
}

export interface ExportMeta { subjectName: string; lang: L2 }

function metaLine(n: Note, m: ExportMeta): string {
  return [m.subjectName, tr(TEMPLATE_LABEL[n.template], m.lang), n.date ? n.date + (n.classT ? ' ' + n.classT : '') : ''].filter(Boolean).join(' · ');
}

export function noteToMarkdown(n: Note, m: ExportMeta): string {
  const parts = ['# ' + noteTitle(n, m.lang).replace(/\n+/g, ' '), '_' + metaLine(n, m) + '_'];
  if (n.template === 'cornell') {
    for (const z of ZONES) {
      const body = joinBlocks(n.blocks.filter(b => (b.zone ?? 'main') === z), b => blockMd(b, m.lang));
      if (body) parts.push('## ' + tr(ZONE_LABEL[z], m.lang) + '\n\n' + body);
    }
  } else {
    const body = joinBlocks(n.blocks, b => blockMd(b, m.lang));
    if (body) parts.push(body);
  }
  return parts.join('\n\n') + '\n';
}

export function noteToText(n: Note, m: ExportMeta): string {
  const parts = [noteTitle(n, m.lang), metaLine(n, m)];
  if (n.template === 'cornell') {
    for (const z of ZONES) {
      const body = joinBlocks(n.blocks.filter(b => (b.zone ?? 'main') === z), b => blockTxt(b, m.lang));
      if (body) parts.push(tr(ZONE_LABEL[z], m.lang).toUpperCase() + '\n' + body);
    }
  } else {
    const body = joinBlocks(n.blocks, b => blockTxt(b, m.lang));
    if (body) parts.push(body);
  }
  return parts.join('\n\n') + '\n';
}

/** a safe file name from the title: letters (any script), digits, dashes */
export function fileName(n: Pick<Note, 'title' | 'template' | 'date'>, lang: L2, ext: 'md' | 'txt'): string {
  const base = noteTitle(n, lang).toLowerCase().replace(/[^\p{L}\p{N}]+/gu, '-').replace(/^-+|-+$/g, '').slice(0, 60) || 'note';
  return `${base}.${ext}`;
}

/* ---------- search ---------- */
/** the note's text for search and AI: title and every block, in display order */
export function plainText(n: Pick<Note, 'title' | 'template' | 'blocks'>): string {
  return [n.title, ...orderedBlocks(n).map(b => b.text)].filter(s => s.trim()).join('\n');
}

const fold = (s: string) => s.toLocaleLowerCase('ru').replace(/ё/g, 'е');

export interface NoteHit { note: Note; snippet: string }
/** notes containing every word of the query (case-insensitive), newest first, with a short snippet around the first word */
export function searchNotes(notes: Note[], q: string): NoteHit[] {
  const words = fold(q).split(/\s+/).filter(Boolean);
  const sorted = notes.slice().sort((a, b) => b.updated - a.updated);
  if (!words.length) return sorted.map(note => ({ note, snippet: '' }));
  const out: NoteHit[] = [];
  for (const note of sorted) {
    const text = plainText(note);
    const f = fold(text);
    if (!words.every(w => f.includes(w))) continue;
    const at = f.indexOf(words[0]);
    const from = Math.max(0, at - 30);
    const snippet = (from ? '…' : '') + text.slice(from, at + words[0].length + 50).replace(/\s+/g, ' ').trim() + (at + words[0].length + 50 < text.length ? '…' : '');
    out.push({ note, snippet });
  }
  return out;
}

/* ---------- validation of stored data ---------- */
const isZone = (z: unknown): z is Zone => z === 'cue' || z === 'main' || z === 'sum';
export function cleanNote(raw: unknown): Note | null {
  if (!raw || typeof raw !== 'object') return null;
  const x = raw as Partial<Note>;
  if (typeof x.id !== 'string' || !x.id || typeof x.subject !== 'string') return null;
  const template: TemplateId = TEMPLATES.includes(x.template as TemplateId) ? x.template as TemplateId : 'lecture';
  const blocks: Block[] = [];
  for (const r of Array.isArray(x.blocks) ? x.blocks : []) {
    if (!r || typeof r !== 'object') continue;
    const b = r as Partial<Block>;
    if (typeof b.id !== 'string' || !BLOCK_TYPES.includes(b.t as BlockType)) continue;
    const out: Block = { id: b.id, t: b.t as BlockType, text: typeof b.text === 'string' ? b.text.slice(0, 20000) : '' };
    if (b.checked) out.checked = true;
    if (b.indent === 1 && (b.t === 'li' || b.t === 'todo')) out.indent = 1;
    if (isZone(b.zone)) out.zone = b.zone;
    if (b.t === 'img') { if (typeof b.img !== 'string' || !b.img) continue; out.img = b.img; }
    blocks.push(out);
  }
  const num = (v: unknown) => (typeof v === 'number' && Number.isFinite(v) ? v : 0);
  return {
    id: x.id, subject: x.subject, template, title: typeof x.title === 'string' ? x.title.slice(0, 300) : '', blocks,
    date: typeof x.date === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(x.date) ? x.date : '',
    classT: typeof x.classT === 'string' && /^\d{2}:\d{2}$/.test(x.classT) ? x.classT : '',
    created: num(x.created), updated: num(x.updated),
  };
}

/* ---------- «Қорытып, карточка жаса» ---------- */
/** /api/chat accepts up to 2000 characters per message (backend/internal/chat CHAT_MAX_MSG_LEN); the instructions take the rest */
export const AI_NOTE_MAX = 1500;
export const AI_NOTE_MIN = 40;

/** the one user message sent to the existing chat backend; `cut` says the note was longer than AI_NOTE_MAX */
export function aiPrompt(text: string, subjectName: string, lang: L2): { prompt: string; cut: boolean } {
  const clean = text.replace(/[ \t]+/g, ' ').replace(/\n{3,}/g, '\n\n').trim();
  const cut = clean.length > AI_NOTE_MAX;
  const body = cut ? clean.slice(0, AI_NOTE_MAX) : clean;
  const prompt = lang === 'ru'
    ? `Конспект по предмету «${subjectName}». Ответь строго в формате:\nИТОГ:\n(3–5 коротких предложений)\nКАРТОЧКИ:\nВ: вопрос или термин | О: короткий ответ\n(5–8 карточек, только по тексту конспекта, ничего не выдумывай)\n\nКонспект:\n${body}`
    : `«${subjectName}» пәні бойынша конспект. Жауапты тек осы форматта бер:\nҚОРЫТЫНДЫ:\n(3–5 қысқа сөйлем)\nКАРТОЧКАЛАР:\nС: сұрақ не термин | Ж: қысқа жауап\n(5–8 карточка, тек конспект мәтіні бойынша, ойдан қоспа)\n\nКонспект:\n${body}`;
  return { prompt, cut };
}

export interface AiResult { summary: string; cards: { q: string; a: string }[] }

const strip = (s: string) => s.replace(/\*\*/g, '').replace(/^[\s>*•\-–—\d.)]+/, '').trim();

/** the model's answer -> summary + cards. Accepts the requested «С: … | Ж: …» / «В: … | О: …» / «Q: … | A: …» lines,
    «термин — анықтама» lines and two-line «С: …» / «Ж: …» pairs; anything unreadable is dropped, never invented. */
export function parseAiResult(text: string): AiResult {
  const lines = (text || '').replace(/\r\n?/g, '\n').split('\n');
  const summary: string[] = [];
  const cards: { q: string; a: string }[] = [];
  let mode: 'none' | 'sum' | 'cards' = 'none';
  let pendingQ = '';
  const qRe = /^(?:С|В|Q|Сұрақ|Вопрос|Термин)\s*[:.)]\s*/i;
  const aRe = /^(?:Ж|О|A|Жауап|Ответ|Анықтама|Определение)\s*[:.)]\s*/i;
  for (const raw of lines) {
    const line = raw.trim();
    if (!line) continue;
    const head = strip(line).replace(/[:：]\s*$/, '').toUpperCase();
    if (/^(ҚОРЫТЫНДЫ|ИТОГ|РЕЗЮМЕ|SUMMARY|ҚЫСҚАША)(?!\p{L})/u.test(head) && head.length < 30) {
      mode = 'sum';
      const rest = line.replace(/^.*?(ҚОРЫТЫНДЫ|ИТОГ|РЕЗЮМЕ|SUMMARY|ҚЫСҚАША)\**\s*[:：]?\s*\**/i, '').trim();
      if (rest) summary.push(rest);
      continue;
    }
    if (/^(КАРТОЧКАЛАР|КАРТОЧКИ|CARDS|FLASHCARDS)(?!\p{L})/u.test(head) && head.length < 30) { mode = 'cards'; continue; }
    const body = strip(line);
    const pair = body.split(/\s*\|\s*/);
    if (pair.length >= 2 && qRe.test(pair[0]) && aRe.test(pair[1])) {
      const q = pair[0].replace(qRe, '').trim(), a = pair.slice(1).join(' | ').replace(aRe, '').trim();
      if (q && a) cards.push({ q, a });
      mode = 'cards';
      continue;
    }
    if (qRe.test(body)) { pendingQ = body.replace(qRe, '').trim(); mode = 'cards'; continue; }
    if (aRe.test(body) && pendingQ) { cards.push({ q: pendingQ, a: body.replace(aRe, '').trim() }); pendingQ = ''; continue; }
    if (mode === 'cards') {
      const m = body.match(/^(.{2,100}?)\s+(?:—|–|-|:)\s+(.{2,})$/);
      if (m) cards.push({ q: m[1].trim(), a: m[2].trim() });
      continue;
    }
    if (mode === 'sum' || mode === 'none') summary.push(body);
  }
  const seen = new Set<string>();
  return {
    summary: summary.join(' ').replace(/\s+/g, ' ').trim().slice(0, 1200),
    cards: cards.filter(c => {
      const k = c.q.toLowerCase();
      if (seen.has(k) || c.q.length > 200 || c.a.length > 500) return false;
      seen.add(k);
      return true;
    }).slice(0, 15),
  };
}
