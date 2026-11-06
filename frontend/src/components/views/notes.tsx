'use client';
/* «Конспектілер»: the list with search (/notes), the block editor (/notes?id=<note id>), the «new note» sheet with the
   three templates, export (.md / .txt / share) and «Қорытып, карточка жаса» (one AI button, result edited before saving).
   Storage and the write-ahead save are in lib/studydb.ts; the note model, templates and export in lib/notes.ts. */
import { DeskList } from '@/components/desk';
import { useDesktop } from '@/lib/desktop';
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type TextareaHTMLAttributes } from 'react';
import { copyText, useApp } from '@/components/app';
import { ConfirmSheet, Empty, Icon, Row } from '@/components/ui';
import { saveFile } from '@/components/views/calendar';
import { todayYmd, useQueryId, useSubjects } from '@/components/views/subjects';
import { cardsFromText } from '@/lib/domain';
import { useOnline } from '@/lib/pwa';
import {
  AI_NOTE_MIN, aiPrompt, block, BLOCK_LABEL, fileName, newId, newNote, noteTitle, noteToMarkdown, noteToText, parseAiResult, plainText,
  searchNotes, TEMPLATE_HINT, TEMPLATE_LABEL, TEMPLATES, ZONE_LABEL, type Block, type BlockType, type Note, type TemplateId, type Zone,
} from '@/lib/notes';
import { deleteImage, deleteNote, imageUrl, putImage, saveNote, useStudy, walPut } from '@/lib/studydb';
import type { Subject } from '@/lib/subjects';
import { getState } from '@/lib/store';
import { apiLang } from '@/lib/i18n';

type Tt = (kk: string, ru: string) => string;
/** the clock, read in event handlers only */
const nowMs = () => Date.now();

export function NotesScreen() {
  const id = useQueryId();
  return id ? <NoteEditor id={id} /> : <NotesList />;
}

/* ---------- list + search ---------- */
function NotesList() {
  const { S, tt, L, openSheet } = useApp();
  const desktop = useDesktop();
  const { list } = useSubjects();
  const study = useStudy();
  const [q, setQ] = useState('');
  const lang = apiLang(S.lang);
  const hits = searchNotes(study.notes, q);
  const subjName = (id: string) => list.find(s => s.id === id)?.name ?? '';
  const tools = (
    <>
      <div className="search" role="search">
        <Icon n="search" s={18} />
        <input className="input" id="notesQ" type="search" value={q} onChange={e => setQ(e.target.value)} autoComplete="off"
          aria-label={tt('Конспектілерден іздеу', 'Поиск по конспектам')} placeholder={tt('Конспектілерден іздеу', 'Поиск по конспектам')} />
      </div>
      <button className="btn block" type="button" onClick={() => openSheet(<NewNoteSheet />)}><Icon n="plus" s={18} /> {tt('Жаңа конспект', 'Новый конспект')}</button>
    </>
  );
  const results = (
    <>
      {!study.ready ? <p className="small muted" role="status">{tt('Жүктелуде…', 'Загрузка…')}</p>
        : hits.length ? (
          <div className={`group${desktop ? ' desk-cards' : ''}`} id="noteList">{hits.map(({ note, snippet }) => (
            <Row key={note.id} go={'notes:' + note.id} icon="doc" title={noteTitle(note, lang)}
              sub={snippet || [subjName(note.subject), L(TEMPLATE_LABEL[note.template]), note.date].filter(Boolean).join(' · ')} />
          ))}</div>
        ) : <Empty icon="doc">{q.trim() ? tt('Ештеңе табылмады.', 'Ничего не найдено.') : tt('Әзірге конспект жоқ. Үлгіні таңдап, бастаңыз.', 'Пока нет конспектов. Выберите шаблон и начните.')}</Empty>}
    </>
  );
  const notes = (
    <>
      {!study.durable && study.ready ? <p className="note">{tt('Бұл браузер IndexedDB-ге рұқсат бермейді: конспектілер шағын көшірмеде ғана сақталады, суреттер сақталмайды.', 'Этот браузер не даёт доступ к IndexedDB: конспекты хранятся только в небольшой копии, фото не сохраняются.')}</p> : null}
      <p className="tiny muted">{tt('Конспектілер тек осы құрылғыда сақталады және интернетсіз ашылады. Маңыздыларын .md файлына экспорттап қойыңыз.', 'Конспекты хранятся только на этом устройстве и открываются без интернета. Важные экспортируйте в файл .md.')}</p>
    </>
  );
  // a laptop: search and «new note» in a left panel, the notes as tiles beside it
  if (desktop) return <DeskList label={tt('Конспектілерден іздеу', 'Поиск по конспектам')} filters={<>{tools}{notes}</>}>{results}</DeskList>;
  return <>{tools}{results}{notes}</>;
}

/* ---------- new note ---------- */
/** the class of this subject running now or started last today, for linking a new note to it */
function classNow(s: Subject | undefined, now: Date): string {
  if (!s || now.getDay() === 0 || now.getDay() === 6) return '';
  const mins = now.getHours() * 60 + now.getMinutes() + 15;
  const started = s.slots.filter(x => { const [h, m] = x.t.split(':').map(Number); return h * 60 + m <= mins; });
  return started.length ? started[started.length - 1].t : '';
}

export function NewNoteSheet({ subject }: { subject?: string }) {
  const { S, tt, L, go, toast } = useApp();
  const { list } = useSubjects();
  const [subj, setSubj] = useState(subject ?? list[0]?.id ?? '');
  const create = async (tpl: TemplateId) => {
    const s = list.find(x => x.id === subj);
    if (!s) return;
    const n = newNote(tpl, s.id, apiLang(S.lang), nowMs(), todayYmd(), classNow(s, new Date()));
    const ok = await saveNote(n);
    if (!ok) toast(tt('IndexedDB-ге жазылмады, көшірме сақталды', 'Не записалось в IndexedDB, копия сохранена'));
    go('notes:' + n.id);
  };
  return (
    <>
      <h2>{tt('Жаңа конспект', 'Новый конспект')}</h2>
      <div className="field">
        <label htmlFor="nnSubj">{tt('Пән', 'Предмет')}</label>
        <select className="input" id="nnSubj" value={subj} onChange={e => setSubj(e.target.value)}>
          {list.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
        </select>
      </div>
      <div className="group" role="group" aria-label={tt('Үлгі', 'Шаблон')}>
        {TEMPLATES.map(t => <Row key={t} onClick={() => void create(t)} icon="doc" title={L(TEMPLATE_LABEL[t])} sub={L(TEMPLATE_HINT[t])} />)}
      </div>
    </>
  );
}

/* ---------- editor ---------- */
function NoteEditor({ id }: { id: string }) {
  const { tt, go } = useApp();
  const study = useStudy();
  if (!study.ready) return <p className="small muted" role="status">{tt('Жүктелуде…', 'Загрузка…')}</p>;
  const note = study.notes.find(n => n.id === id);
  if (!note) {
    return <Empty icon="doc" action={<button className="btn secondary" type="button" onClick={() => go('notes', { replace: true })}>{tt('Конспектілер', 'Конспекты')}</button>}>{tt('Конспект табылмады.', 'Конспект не найден.')}</Empty>;
  }
  return <Editor key={note.id} initial={note} durable={study.durable} />;
}

/** a textarea that grows with its text */
function AutoText(props: TextareaHTMLAttributes<HTMLTextAreaElement> & { id: string; value: string }) {
  const ref = useRef<HTMLTextAreaElement>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = el.scrollHeight + 2 + 'px';
  }, [props.value]);
  return <textarea ref={ref} rows={1} {...props} />;
}

function NoteImage({ b, label, onCaption }: { b: Block; label: string; onCaption: (v: string) => void }) {
  const { tt } = useApp();
  const [url, setUrl] = useState('');
  useEffect(() => {
    let live = true;
    if (b.img) void imageUrl(b.img).then(u => { if (live) setUrl(u); });
    return () => { live = false; };
  }, [b.img]);
  return (
    <figure className="nb-fig">
      {/* eslint-disable-next-line @next/next/no-img-element -- a blob: URL from IndexedDB, nothing for next/image to optimise */}
      {url ? <img src={url} alt={b.text || tt('Конспекттегі сурет', 'Фото в конспекте')} /> : <span className="tiny muted">{tt('Сурет жүктелмеді', 'Фото не загрузилось')}</span>}
      <AutoText id={'blk-' + b.id} className="input nb-text" value={b.text} aria-label={label} placeholder={tt('Сурет сипаттамасы', 'Подпись к фото')}
        onChange={e => onCaption(e.target.value)} />
    </figure>
  );
}

/** camera / gallery photo -> WebP, longest side ≤ 1600 px, ≤ ~350 KB */
async function toWebp(file: File): Promise<Blob> {
  const bmp = await createImageBitmap(file);
  const k = Math.min(1, 1600 / Math.max(bmp.width, bmp.height));
  const w = Math.max(1, Math.round(bmp.width * k)), h = Math.max(1, Math.round(bmp.height * k));
  const c = document.createElement('canvas');
  c.width = w; c.height = h;
  c.getContext('2d')!.drawImage(bmp, 0, 0, w, h);
  bmp.close?.();
  const enc = (q: number) => new Promise<Blob | null>(r => c.toBlob(r, 'image/webp', q));
  let q = 0.8, out = await enc(q);
  while (out && out.size > 350_000 && q > 0.45) { q -= 0.15; out = await enc(q); }
  if (!out) throw new Error('encode');
  return out;
}

const fmtTime = (t: number) => { const d = new Date(t); return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`; };
const EDITABLE: BlockType[] = ['p', 'h', 'li', 'todo', 'callout'];

function Editor({ initial, durable }: { initial: Note; durable: boolean }) {
  const app = useApp();
  const { S, tt, L, go, toast, openSheet } = app;
  const lang = apiLang(S.lang);
  const { list } = useSubjects();
  const [note, setNote] = useState(initial);
  const noteRef = useRef(initial);
  const [status, setStatus] = useState<{ k: 'saved' | 'saving' | 'error'; at: number }>({ k: 'saved', at: initial.updated });
  const [cur, setCur] = useState<string>('');
  const pending = useRef<Note | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const focusNext = useRef<string>('');
  const subject = list.find(s => s.id === note.subject);

  const flush = useCallback(async () => {
    clearTimeout(timer.current);
    const n = pending.current;
    if (!n) return;
    pending.current = null;
    const ok = await saveNote(n);
    if (pending.current) return; // a newer edit is on its way
    setStatus({ k: ok ? 'saved' : 'error', at: n.updated });
  }, []);

  // write-ahead on every edit, IndexedDB after a short pause (and at once when the page hides or the screen closes)
  const change = (fn: (n: Note) => Note) => {
    const next = { ...fn(noteRef.current), updated: nowMs() };
    noteRef.current = next;
    setNote(next);
    walPut(next);
    pending.current = next;
    setStatus(s => ({ k: 'saving', at: s.at }));
    clearTimeout(timer.current);
    timer.current = setTimeout(() => { void flush(); }, 500);
  };
  useEffect(() => {
    const hide = () => { void flush(); };
    const vis = () => { if (document.visibilityState === 'hidden') void flush(); };
    window.addEventListener('pagehide', hide);
    document.addEventListener('visibilitychange', vis);
    return () => { window.removeEventListener('pagehide', hide); document.removeEventListener('visibilitychange', vis); void flush(); };
  }, [flush]);
  useEffect(() => {
    if (!focusNext.current) return;
    const el = document.getElementById('blk-' + focusNext.current);
    focusNext.current = '';
    el?.focus();
  }, [note]);

  const blocks = note.blocks;
  const upd = (bid: string, patch: Partial<Block>) => change(n => ({ ...n, blocks: n.blocks.map(b => (b.id === bid ? { ...b, ...patch } : b)) }));
  const insertAfter = (afterId: string | null, b: Block) => {
    focusNext.current = b.id;
    change(n => {
      const i = afterId ? n.blocks.findIndex(x => x.id === afterId) : -1;
      if (i < 0) return { ...n, blocks: [...n.blocks, b] };
      return { ...n, blocks: [...n.blocks.slice(0, i + 1), b, ...n.blocks.slice(i + 1)] };
    });
  };
  const zoneOf = (b: Block): Zone => (note.template === 'cornell' ? b.zone ?? 'main' : 'main');
  const add = (t: BlockType, zone: Zone, extra: Partial<Block> = {}) => {
    const n = noteRef.current;
    const inZone = n.blocks.filter(b => (n.template === 'cornell' ? b.zone ?? 'main' : 'main') === zone);
    insertAfter(inZone.length ? inZone[inZone.length - 1].id : null, block(t, '', { ...(n.template === 'cornell' ? { zone } : {}), ...extra }));
  };
  const remove = (bid: string) => {
    const i = blocks.findIndex(b => b.id === bid);
    const b = blocks[i];
    if (!b) return;
    const prev = blocks.slice(0, i).reverse().find(x => zoneOf(x) === zoneOf(b));
    if (prev) focusNext.current = prev.id;
    if (b.img) void deleteImage(b.img);
    setCur(prev?.id ?? '');
    change(n => ({ ...n, blocks: n.blocks.filter(x => x.id !== bid) }));
  };
  const move = (bid: string, dir: -1 | 1) => change(n => {
    const i = n.blocks.findIndex(b => b.id === bid);
    const z = (b: Block) => (n.template === 'cornell' ? b.zone ?? 'main' : 'main');
    let j = i + dir;
    while (j >= 0 && j < n.blocks.length && z(n.blocks[j]) !== z(n.blocks[i])) j += dir;
    if (i < 0 || j < 0 || j >= n.blocks.length) return n;
    const out = n.blocks.slice();
    [out[i], out[j]] = [out[j], out[i]];
    focusNext.current = bid;
    return { ...n, blocks: out };
  });

  const onKey = (e: KeyboardEvent<HTMLTextAreaElement>, b: Block) => {
    const listy = b.t === 'li' || b.t === 'todo';
    if (e.key === 'Enter' && !e.shiftKey && (listy || b.t === 'h')) {
      e.preventDefault();
      if (listy && !b.text.trim()) {
        if (b.indent) upd(b.id, { indent: undefined }); else upd(b.id, { t: 'p' }); // an empty item ends the list
        return;
      }
      insertAfter(b.id, block(b.t === 'h' ? 'p' : b.t, '', { ...(b.indent ? { indent: 1 as const } : {}), ...(b.zone ? { zone: b.zone } : {}) }));
      return;
    }
    if (e.key === 'Backspace' && !b.text && e.currentTarget.selectionStart === 0) {
      const sameZone = blocks.filter(x => zoneOf(x) === zoneOf(b));
      if (sameZone.length > 1) { e.preventDefault(); remove(b.id); }
    }
  };

  // camera / gallery
  const camRef = useRef<HTMLInputElement>(null), galRef = useRef<HTMLInputElement>(null);
  const imgZone = useRef<Zone>('main');
  const pickImage = async (f: File | undefined) => {
    if (!f) return;
    if (!durable) { toast(tt('Бұл браузерде суретті сақтау мүмкін емес', 'В этом браузере фото сохранить нельзя')); return; }
    try {
      const blob = await toWebp(f);
      const imgId = newId('i');
      if (!(await putImage(imgId, blob))) throw new Error('store');
      add('img', imgZone.current, { img: imgId });
    } catch {
      toast(tt('Суретті қосу мүмкін болмады', 'Не удалось добавить фото'));
    }
  };

  const label = (b: Block, n: number) => (note.template === 'cornell' ? L(ZONE_LABEL[zoneOf(b)]) + ': ' : '') + L(BLOCK_LABEL[b.t]) + ' ' + n;
  const renderBlock = (b: Block, n: number) => {
    const common = {
      id: 'blk-' + b.id, value: b.text, 'aria-label': label(b, n),
      onFocus: () => setCur(b.id), onChange: (e: { target: { value: string } }) => upd(b.id, { text: e.target.value }),
      onKeyDown: (e: KeyboardEvent<HTMLTextAreaElement>) => onKey(e, b),
    };
    return (
      <div key={b.id} className={`nb nb-${b.t}${b.indent ? ' nb-in' : ''}${cur === b.id ? ' nb-cur' : ''}`}>
        {b.t === 'todo' ? <input type="checkbox" className="nb-check" checked={!!b.checked} aria-label={tt('Орындалды', 'Выполнено') + ': ' + (b.text || label(b, n))}
          onChange={e => upd(b.id, { checked: e.target.checked || undefined })} onFocus={() => setCur(b.id)} /> : null}
        {b.t === 'li' ? <span className="nb-dot" aria-hidden="true">•</span> : null}
        {b.t === 'callout' ? <span className="nb-flag" aria-hidden="true"><Icon n="bolt" s={16} /></span> : null}
        {b.t === 'img'
          ? <NoteImage b={b} label={label(b, n)} onCaption={v => upd(b.id, { text: v })} />
          : <AutoText {...common} className={`input nb-text${b.t === 'h' ? ' nb-h' : ''}`} placeholder={b.t === 'h' ? tt('Тақырыпша', 'Подзаголовок') : b.t === 'callout' ? tt('Маңызды ой', 'Важная мысль') : ''} />}
      </div>
    );
  };
  const zones: Zone[] = note.template === 'cornell' ? ['cue', 'main', 'sum'] : ['main'];
  const curBlock = blocks.find(b => b.id === cur);
  const counters: Partial<Record<string, number>> = {};
  const nth = (b: Block) => { const k = zoneOf(b) + b.t; counters[k] = (counters[k] ?? 0) + 1; return counters[k]!; };

  const meta = { subjectName: subject?.name ?? '', lang } as const;
  const share = async () => {
    const txt = noteToText(note, meta);
    const title = noteTitle(note, lang);
    try {
      const file = new File([noteToMarkdown(note, meta)], fileName(note, lang, 'md'), { type: 'text/markdown' });
      if (navigator.canShare?.({ files: [file] })) { await navigator.share({ files: [file], title }); return; }
      if (navigator.share) { await navigator.share({ title, text: txt }); return; }
    } catch (e) {
      if ((e as Error)?.name === 'AbortError') return;
    }
    copyText(txt, toast, tt);
  };

  return (
    <div className="note-ed">
      <div className="note-head">
        <span className={`tiny note-status${status.k === 'error' ? ' bad' : ''}`} role="status" aria-live="polite" id="noteStatus">
          {status.k === 'saving' ? tt('Сақталуда…', 'Сохраняется…') : status.k === 'error' ? tt('IndexedDB-ге жазылмады — көшірме құрылғыда, қайта ашқанда қалпына келеді', 'Не записалось в IndexedDB — копия на устройстве, восстановится при открытии')
            : tt(`Сақталды ${fmtTime(status.at)}`, `Сохранено ${fmtTime(status.at)}`)}
        </span>
      </div>
      <div className="field">
        <label htmlFor="noteTitle">{tt('Конспект тақырыбы', 'Тема конспекта')}</label>
        <input className="input note-title" id="noteTitle" value={note.title} maxLength={300} placeholder={noteTitle({ ...note, title: '' }, lang)}
          onChange={e => change(n => ({ ...n, title: e.target.value }))} />
      </div>
      <div className="cal-form-row">
        <div className="field">
          <label htmlFor="noteSubj">{tt('Пән', 'Предмет')}</label>
          <select className="input" id="noteSubj" value={note.subject} onChange={e => change(n => ({ ...n, subject: e.target.value, classT: '' }))}>
            {list.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
            {subject ? null : <option value={note.subject}>—</option>}
          </select>
        </div>
        <div className="field">
          <label htmlFor="noteDate">{tt('Сабақ күні', 'Дата занятия')}</label>
          <input className="input" id="noteDate" type="date" value={note.date} onChange={e => change(n => ({ ...n, date: e.target.value }))} />
        </div>
        {subject?.slots.length ? (
          <div className="field">
            <label htmlFor="noteClass">{tt('Сабақ', 'Пара')}</label>
            <select className="input" id="noteClass" value={note.classT} onChange={e => change(n => ({ ...n, classT: e.target.value }))}>
              <option value="">—</option>
              {subject.slots.map(x => <option key={x.t} value={x.t}>{x.t} · {L(x.kind)} · {x.room}</option>)}
            </select>
          </div>
        ) : null}
      </div>

      <div className="note-tools" role="toolbar" aria-label={tt('Блок құралдары', 'Инструменты блока')}>
        <select className="input" aria-label={tt('Блок түрі', 'Тип блока')} value={curBlock && curBlock.t !== 'img' ? curBlock.t : ''} disabled={!curBlock || curBlock.t === 'img'}
          onChange={e => curBlock && upd(curBlock.id, { t: e.target.value as BlockType, ...(e.target.value === 'li' || e.target.value === 'todo' ? {} : { indent: undefined }) })}>
          {curBlock && curBlock.t !== 'img' ? null : <option value="">{tt('Блок түрі', 'Тип блока')}</option>}
          {EDITABLE.map(t => <option key={t} value={t}>{L(BLOCK_LABEL[t])}</option>)}
        </select>
        <button className="icon-btn" type="button" aria-label={tt('Ішке жылжыту', 'Сдвинуть вправо')} aria-pressed={!!curBlock?.indent}
          disabled={!curBlock || (curBlock.t !== 'li' && curBlock.t !== 'todo')} onClick={() => curBlock && upd(curBlock.id, { indent: curBlock.indent ? undefined : 1 })}><Icon n="chev" s={18} /></button>
        <button className="icon-btn" type="button" aria-label={tt('Жоғары', 'Вверх')} disabled={!curBlock} onClick={() => curBlock && move(curBlock.id, -1)}><Icon n="chev" s={18} className="rot-up" /></button>
        <button className="icon-btn" type="button" aria-label={tt('Төмен', 'Вниз')} disabled={!curBlock} onClick={() => curBlock && move(curBlock.id, 1)}><Icon n="chev" s={18} className="rot-down" /></button>
        <button className="icon-btn" type="button" aria-label={tt('Блокты жою', 'Удалить блок')} disabled={!curBlock} onClick={() => curBlock && remove(curBlock.id)}><Icon n="trash" s={18} /></button>
      </div>

      <div className={note.template === 'cornell' ? 'cornell' : ''}>
        {zones.map(z => (
          <section key={z} className={`note-zone nz-${z}`} aria-labelledby={note.template === 'cornell' ? 'nz-' + z : undefined} aria-label={note.template === 'cornell' ? undefined : tt('Конспект мәтіні', 'Текст конспекта')}>
            {note.template === 'cornell' ? <h2 className="nz-title" id={'nz-' + z}>{L(ZONE_LABEL[z])}</h2> : null}
            <div className="note-blocks">{blocks.filter(b => zoneOf(b) === z).map(b => renderBlock(b, nth(b)))}</div>
            <div className="note-add" role="group" aria-label={tt('Блок қосу', 'Добавить блок') + (note.template === 'cornell' ? ': ' + L(ZONE_LABEL[z]) : '')}>
              {EDITABLE.map(t => <button key={t} className="chip" type="button" onClick={() => add(t, z)}><Icon n="plus" s={14} /> {L(BLOCK_LABEL[t])}</button>)}
              <button className="chip" type="button" onClick={() => { imgZone.current = z; camRef.current?.click(); }}><Icon n="plus" s={14} /> {tt('Камера', 'Камера')}</button>
              <button className="chip" type="button" onClick={() => { imgZone.current = z; galRef.current?.click(); }}><Icon n="plus" s={14} /> {tt('Галерея', 'Галерея')}</button>
            </div>
          </section>
        ))}
      </div>
      <input ref={camRef} type="file" accept="image/*" capture="environment" hidden aria-hidden="true" tabIndex={-1} onChange={e => { void pickImage(e.target.files?.[0]); e.target.value = ''; }} />
      <input ref={galRef} type="file" accept="image/*" hidden aria-hidden="true" tabIndex={-1} onChange={e => { void pickImage(e.target.files?.[0]); e.target.value = ''; }} />

      <AiCards note={note} subjectName={subject?.name ?? ''} onSummary={text => change(n => ({
        ...n, blocks: [...n.blocks, block('callout', text, n.template === 'cornell' ? { zone: 'sum' } : {})],
      }))} />

      <section className="section" aria-labelledby="noteExport">
        <h2 id="noteExport">{tt('Экспорт', 'Экспорт')}</h2>
        <div className="btn-row">
          <button className="btn ghost" type="button" onClick={() => saveFile(noteToMarkdown(note, meta), fileName(note, lang, 'md'), 'text/markdown;charset=utf-8')}><Icon n="file" s={18} /> .md</button>
          <button className="btn ghost" type="button" onClick={() => saveFile(noteToText(note, meta), fileName(note, lang, 'txt'), 'text/plain;charset=utf-8')}><Icon n="file" s={18} /> .txt</button>
          <button className="btn ghost" type="button" onClick={() => void share()}><Icon n="share" s={18} /> {tt('Бөлісу', 'Поделиться')}</button>
        </div>
      </section>
      <div className="btn-row">
        {subject ? <button className="btn secondary" type="button" onClick={() => go('subjects:' + subject.id)}><Icon n="book" s={18} /> {subject.name}</button> : null}
        <button className="btn danger" type="button" onClick={() => openSheet(<ConfirmSheet msg={tt('Конспектіні жою керек пе?', 'Удалить конспект?')} yes={() => {
          clearTimeout(timer.current);
          pending.current = null;
          void deleteNote(note.id);
          go(subject ? 'subjects:' + subject.id : 'notes', { replace: true });
        }} />)}><Icon n="trash" s={18} /> {tt('Жою', 'Удалить')}</button>
      </div>
    </div>
  );
}

/* ---------- «Қорытып, карточка жаса» ---------- */
type Draft = { summary: string; addSummary: boolean; name: string; cards: { k: string; q: string; a: string }[]; cut: boolean; ai: boolean };

async function askAi(prompt: string, lang: 'kk' | 'ru', tt: Tt): Promise<{ text: string } | { error: string }> {
  const p = getState().profile;
  try {
    const r = await fetch('/api/chat', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ lang, messages: [{ role: 'user', content: prompt }], profile: { spec: p.spec, course: p.course, gpa: p.gpa, en: p.en, skills: p.skills, exp: p.exp } }),
      signal: AbortSignal.timeout(45_000),
    });
    const data: { text?: string; error?: { message?: string } } = await r.json().catch(() => ({}));
    if (!r.ok) {
      return { error: data?.error?.message || (r.status === 503 ? tt('AI уақытша қолжетімсіз.', 'AI временно недоступен.') : tt('AI жауап бермеді (HTTP ', 'AI не ответил (HTTP ') + r.status + ').') };
    }
    return { text: String(data.text || '') };
  } catch {
    return { error: tt('AI-мен байланыс болмады. Интернетті тексеріп, қайталаңыз.', 'Нет связи с AI. Проверьте интернет и повторите.') };
  }
}

function AiCards({ note, subjectName, onSummary }: { note: Note; subjectName: string; onSummary: (text: string) => void }) {
  const { S, tt, go, update, toast } = useApp();
  const online = useOnline();
  const lang = apiLang(S.lang);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [draft, setDraft] = useState<Draft | null>(null);
  const [savedDeck, setSavedDeck] = useState<{ id: string; n: number } | null>(null);
  const text = plainText(note);
  const tooShort = text.replace(/\s+/g, '').length < AI_NOTE_MIN;
  const reason = tooShort ? tt(`AI-ға жіберу үшін конспектіге кемінде ${AI_NOTE_MIN} таңба жазыңыз.`, `Чтобы отправить AI, напишите в конспекте хотя бы ${AI_NOTE_MIN} символов.`)
    : !online ? tt('AI-ға интернет керек. Конспект құрылғыда сақталды — интернет қосылғанда қайталаңыз.', 'AI нужен интернет. Конспект сохранён на устройстве — повторите, когда появится сеть.') : '';
  const deckName = `${subjectName ? subjectName + ': ' : ''}${noteTitle(note, lang)}`.slice(0, 120);
  const fill = (summary: string, cards: { q: string; a: string }[], cut: boolean, ai: boolean) =>
    setDraft({ summary, addSummary: !!summary, name: deckName, cards: cards.map(c => ({ k: newId('c'), ...c })), cut, ai });

  const run = async () => {
    if (reason || busy) return;
    setBusy(true); setErr(''); setSavedDeck(null);
    const { prompt, cut } = aiPrompt(text, subjectName, lang);
    const res = await askAi(prompt, lang, tt);
    setBusy(false);
    if ('error' in res) { setErr(res.error); return; }
    const parsed = parseAiResult(res.text);
    if (!parsed.summary && !parsed.cards.length) { setErr(tt('AI түсінікті жауап бермеді. Қайталаңыз немесе карточканы қолмен жасаңыз.', 'AI ответил непонятно. Повторите или создайте карточки вручную.')); return; }
    fill(parsed.summary, parsed.cards, cut, true);
  };
  const fromTerms = () => {
    const cards = cardsFromText(text);
    setErr(''); setSavedDeck(null);
    if (!cards.length) { setErr(tt('«Термин — анықтама» түріндегі жол табылмады.', 'Не найдено строк вида «Термин — определение».')); return; }
    fill('', cards, false, false);
  };
  const save = () => {
    if (!draft) return;
    const cards = draft.cards.map(c => ({ q: c.q.trim(), a: c.a.trim() })).filter(c => c.q && c.a);
    if (cards.length) {
      const deck = { id: 'dk' + nowMs(), name: draft.name.trim() || deckName, cards, subject: note.subject };
      update(s => { s.decks.push(deck); });
      setSavedDeck({ id: deck.id, n: cards.length });
    }
    if (draft.addSummary && draft.summary.trim()) onSummary(draft.summary.trim());
    toast(cards.length ? tt(`${cards.length} карточка сақталды`, `Сохранено карточек: ${cards.length}`) : tt('Қорытынды қосылды', 'Итог добавлен'));
    setDraft(null);
  };
  const setCard = (k: string, patch: Partial<{ q: string; a: string }>) => setDraft(d => d && ({ ...d, cards: d.cards.map(c => (c.k === k ? { ...c, ...patch } : c)) }));

  return (
    <section className="card ai-note" aria-labelledby="aiNoteTitle">
      <h2 id="aiNoteTitle">{tt('Қорытып, карточка жаса', 'Итог и карточки')}</h2>
      <p className="small muted" id="aiNoteWhy">{reason || tt('Конспект мәтіні (суретсіз) AI-ға жіберіледі. Нәтижені сақтамас бұрын түзете аласыз.', 'Текст конспекта (без фото) отправится AI. Результат можно поправить перед сохранением.')}</p>
      <button className="btn" type="button" id="aiNoteBtn" disabled={!!reason || busy} aria-describedby="aiNoteWhy" onClick={() => void run()}>
        <Icon n="spark" s={18} /> {busy ? tt('AI ойлануда…', 'AI думает…') : tt('Қорытып, карточка жаса', 'Итог и карточки')}
      </button>
      <button className="link-btn" type="button" onClick={fromTerms}>{tt('AI-сыз: «термин — анықтама» жолдарынан карточка', 'Без AI: карточки из строк «термин — определение»')}</button>
      {err ? <p className="small ai-err" role="alert">{err}</p> : null}
      {savedDeck ? <Row go={'deck:' + savedDeck.id} icon="cards" title={tt('Жиынды ашу', 'Открыть набор')} sub={tt(`${savedDeck.n} карточка`, `карточек: ${savedDeck.n}`)} /> : null}
      {draft ? (
        <div className="form ai-draft" aria-label={tt('AI нәтижесі', 'Результат AI')} role="group">
          {draft.cut ? <p className="tiny muted">{tt('Конспект ұзын болғандықтан, оның басы ғана жіберілді.', 'Конспект длинный, поэтому отправлено только начало.')}</p> : null}
          {draft.ai ? (
            <>
              <div className="field">
                <label htmlFor="aiSum">{tt('Қорытынды', 'Итог')}</label>
                <AutoText id="aiSum" className="input" value={draft.summary} onChange={e => setDraft({ ...draft, summary: e.target.value })} />
              </div>
              <label className="check small"><input type="checkbox" checked={draft.addSummary} onChange={e => setDraft({ ...draft, addSummary: e.target.checked })} /> {tt('Қорытындыны конспектіге қосу', 'Добавить итог в конспект')}</label>
            </>
          ) : null}
          <div className="field">
            <label htmlFor="aiDeck">{tt('Жиын атауы', 'Название набора')}</label>
            <input className="input" id="aiDeck" value={draft.name} maxLength={120} onChange={e => setDraft({ ...draft, name: e.target.value })} />
          </div>
          <ol className="ai-cards">
            {draft.cards.map((c, i) => (
              <li key={c.k} className="ai-card">
                <input className="input" value={c.q} aria-label={tt(`Сұрақ ${i + 1}`, `Вопрос ${i + 1}`)} onChange={e => setCard(c.k, { q: e.target.value })} />
                <AutoText id={'aiA-' + c.k} className="input" value={c.a} aria-label={tt(`Жауап ${i + 1}`, `Ответ ${i + 1}`)} onChange={e => setCard(c.k, { a: e.target.value })} />
                <button className="link-btn" type="button" onClick={() => setDraft({ ...draft, cards: draft.cards.filter(x => x.k !== c.k) })}>{tt(`${i + 1}-карточканы алып тастау`, `Убрать карточку ${i + 1}`)}</button>
              </li>
            ))}
          </ol>
          <button className="chip" type="button" onClick={() => setDraft({ ...draft, cards: [...draft.cards, { k: newId('c'), q: '', a: '' }] })}><Icon n="plus" s={14} /> {tt('Карточка', 'Карточка')}</button>
          <div className="btn-row">
            <button className="btn" type="button" onClick={save}><Icon n="check" s={18} /> {tt('Жиынға сақтау', 'Сохранить в набор')}</button>
            <button className="btn ghost" type="button" onClick={() => setDraft(null)}>{tt('Бас тарту', 'Отмена')}</button>
          </div>
          {draft.cards.length ? null : <p className="tiny muted">{tt('Карточка жоқ: тек қорытынды сақталады.', 'Карточек нет: сохранится только итог.')}</p>}
        </div>
      ) : null}
      {savedDeck ? <button className="link-btn" type="button" onClick={() => go('review:' + note.subject)}>{tt('Бүгін қайталау', 'Повторить сегодня')}</button> : null}
    </section>
  );
}
