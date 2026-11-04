/* Device storage for the study core: notes, note images, subject settings and spaced-repetition states.
   IndexedDB database "studenthub-study" (not localStorage: notes with photos outgrow its ~5 MB, and losing a note is
   the worst thing a notes app can do). Stores:
     notes    {id, subject, template, title, blocks[], date, classT, created, updated}   (lib/notes.ts Note)
     images   {id, blob: WebP, created}                                                  (a note's image block points here)
     subjects {id, name, custom, exam, hidden?}                                          (lib/subjects.ts SubjectMeta)
     srs      {key: '<deckId>:<hash>', reps, interval, ease, due, lapses, last}          (lib/srs.ts SrState)
   Write-ahead: every edit of a note is first written synchronously to localStorage "studenthub.notes.wal"
   (text only, small), then to IndexedDB after a short debounce; the WAL entry is dropped once IndexedDB has it.
   On start, a WAL entry newer than the IndexedDB copy wins and is written through — so closing the tab mid-edit,
   a crash or a failed transaction loses nothing. Where IndexedDB is unavailable the WAL itself keeps the notes.
   Everything loads into memory once; screens read it with useStudy(). */
import { useEffect, useSyncExternalStore } from 'react';
import { cleanNote, type Note } from '@/lib/notes';
import { cleanState, type SrState } from '@/lib/srs';
import { cleanMeta, type SubjectMeta } from '@/lib/subjects';

const DB_NAME = 'studenthub-study';
const DB_VERSION = 1;
export const WAL_KEY = 'studenthub.notes.wal';
type StoreName = 'notes' | 'images' | 'subjects' | 'srs';

export interface StudySnap {
  /** the first load finished */
  ready: boolean;
  /** IndexedDB works on this device (false: notes kept only in the small localStorage copy, no photos) */
  durable: boolean;
  notes: Note[];
  subjects: SubjectMeta[];
  srs: Record<string, SrState>;
}

let snap: StudySnap = { ready: false, durable: true, notes: [], subjects: [], srs: {} };
const SERVER: StudySnap = snap;
const listeners = new Set<() => void>();
const set = (patch: Partial<StudySnap>) => { snap = { ...snap, ...patch }; listeners.forEach(l => l()); };
const subscribe = (l: () => void) => { listeners.add(l); return () => { listeners.delete(l); }; };

/* ---------- IndexedDB, promisified ---------- */
let dbp: Promise<IDBDatabase> | null = null;
function db(): Promise<IDBDatabase> {
  if (dbp) return dbp;
  dbp = new Promise<IDBDatabase>((resolve, reject) => {
    if (typeof indexedDB === 'undefined') { reject(new Error('no indexedDB')); return; }
    const req = indexedDB.open(DB_NAME, DB_VERSION);
    req.onupgradeneeded = () => {
      const d = req.result;
      if (!d.objectStoreNames.contains('notes')) d.createObjectStore('notes', { keyPath: 'id' });
      if (!d.objectStoreNames.contains('images')) d.createObjectStore('images', { keyPath: 'id' });
      if (!d.objectStoreNames.contains('subjects')) d.createObjectStore('subjects', { keyPath: 'id' });
      if (!d.objectStoreNames.contains('srs')) d.createObjectStore('srs', { keyPath: 'key' });
    };
    req.onsuccess = () => {
      const d = req.result;
      d.onversionchange = () => { d.close(); dbp = null; }; // another tab deletes or upgrades the database
      resolve(d);
    };
    req.onerror = () => reject(req.error);
    req.onblocked = () => reject(new Error('blocked'));
  });
  dbp.catch(() => { dbp = null; });
  return dbp;
}

async function tx<T>(store: StoreName, mode: IDBTransactionMode, run: (s: IDBObjectStore) => IDBRequest<T> | void): Promise<T | undefined> {
  const d = await db();
  return new Promise<T | undefined>((resolve, reject) => {
    const t = d.transaction(store, mode);
    let out: T | undefined;
    const req = run(t.objectStore(store));
    if (req) req.onsuccess = () => { out = req.result; };
    t.oncomplete = () => resolve(out); // resolved only once the data is committed
    t.onerror = () => reject(t.error);
    t.onabort = () => reject(t.error || new Error('aborted'));
  });
}
const getAll = <T,>(store: StoreName) => tx<T[]>(store, 'readonly', s => s.getAll() as IDBRequest<T[]>).then(v => v ?? []);

/* ---------- write-ahead copy ---------- */
function readWal(): Record<string, unknown> {
  try { const v = JSON.parse(window.localStorage.getItem(WAL_KEY) || '{}'); return v && typeof v === 'object' ? v : {}; } catch { return {}; }
}
function writeWal(m: Record<string, unknown>) {
  try {
    if (Object.keys(m).length) window.localStorage.setItem(WAL_KEY, JSON.stringify(m));
    else window.localStorage.removeItem(WAL_KEY);
  } catch { /* full or unavailable: IndexedDB still gets the write */ }
}
/** synchronous, on every edit */
export function walPut(n: Note) { const m = readWal(); m[n.id] = n; writeWal(m); }
function walDrop(id: string, updated: number) {
  const m = readWal();
  const cur = cleanNote(m[id]);
  if (cur && cur.updated <= updated) { delete m[id]; writeWal(m); }
}

/* ---------- load ---------- */
let loading: Promise<void> | null = null;
export function loadStudy(): Promise<void> {
  if (loading) return loading;
  loading = (async () => {
    let notes: Note[] = [], subjects: SubjectMeta[] = [], durable = true;
    const srs: Record<string, SrState> = {};
    try {
      const [n, s, r] = await Promise.all([getAll<unknown>('notes'), getAll<unknown>('subjects'), getAll<{ key?: unknown }>('srs')]);
      notes = n.map(cleanNote).filter((x): x is Note => !!x);
      subjects = cleanMeta(s);
      for (const x of r) { const st = cleanState(x); if (st && typeof x.key === 'string') srs[x.key] = st; }
    } catch {
      durable = false;
    }
    // replay the write-ahead copy
    const wal = readWal();
    const byId = new Map(notes.map(n => [n.id, n]));
    for (const raw of Object.values(wal)) {
      const w = cleanNote(raw);
      if (!w) continue;
      const cur = byId.get(w.id);
      if (cur && cur.updated >= w.updated) { if (durable) walDrop(w.id, cur.updated); continue; }
      byId.set(w.id, w);
      if (durable) void tx('notes', 'readwrite', s => { s.put(w); }).then(() => walDrop(w.id, w.updated), () => {});
    }
    set({ ready: true, durable, notes: [...byId.values()], subjects, srs });
  })();
  return loading;
}

/** the study data; starts loading on first use */
export function useStudy(): StudySnap {
  useEffect(() => { void loadStudy(); }, []);
  return useSyncExternalStore(subscribe, () => snap, () => SERVER);
}
export const getStudy = () => snap;

/* ---------- notes ---------- */
/** keep a note: memory now, IndexedDB now; resolves true once committed (or kept in the WAL where there is no IndexedDB) */
export async function saveNote(n: Note): Promise<boolean> {
  walPut(n);
  const i = snap.notes.findIndex(x => x.id === n.id);
  const notes = snap.notes.slice();
  if (i < 0) notes.push(n); else notes[i] = n;
  set({ notes });
  if (!snap.durable) return true;
  try {
    await tx('notes', 'readwrite', s => { s.put(n); });
    walDrop(n.id, n.updated);
    return true;
  } catch {
    return false; // stays in the WAL, replayed on the next start
  }
}

export async function deleteNote(id: string): Promise<void> {
  const n = snap.notes.find(x => x.id === id);
  set({ notes: snap.notes.filter(x => x.id !== id) });
  const m = readWal(); delete m[id]; writeWal(m);
  try {
    await tx('notes', 'readwrite', s => { s.delete(id); });
    for (const b of n?.blocks ?? []) if (b.img) await deleteImage(b.img);
  } catch { /* nothing more to do */ }
}

/* ---------- images ---------- */
const urls = new Map<string, string>();
export async function putImage(id: string, blob: Blob): Promise<boolean> {
  try { await tx('images', 'readwrite', s => { s.put({ id, blob, created: Date.now() }); }); return true; } catch { return false; }
}
/** an object URL for a stored image (cached for the session), or '' */
export async function imageUrl(id: string): Promise<string> {
  const hit = urls.get(id);
  if (hit) return hit;
  try {
    const rec = await tx<{ blob?: Blob }>('images', 'readonly', s => s.get(id) as IDBRequest<{ blob?: Blob }>);
    if (!rec?.blob) return '';
    const u = URL.createObjectURL(rec.blob);
    urls.set(id, u);
    return u;
  } catch { return ''; }
}
export async function deleteImage(id: string) {
  const u = urls.get(id);
  if (u) { URL.revokeObjectURL(u); urls.delete(id); }
  try { await tx('images', 'readwrite', s => { s.delete(id); }); } catch { /* ignore */ }
}

/* ---------- subjects ---------- */
export async function saveSubject(m: SubjectMeta): Promise<void> {
  const list = snap.subjects.filter(x => x.id !== m.id).concat(m);
  set({ subjects: list });
  try { await tx('subjects', 'readwrite', s => { s.put(m); }); } catch { /* memory only this session */ }
}

/* ---------- spaced repetition ---------- */
export async function saveReview(key: string, st: SrState): Promise<void> {
  set({ srs: { ...snap.srs, [key]: st } });
  try { await tx('srs', 'readwrite', s => { s.put({ key, ...st }); }); } catch { /* memory only this session */ }
}

/* ---------- «delete all data» ---------- */
export async function clearStudyData(): Promise<void> {
  try { window.localStorage.removeItem(WAL_KEY); } catch { /* ignore */ }
  urls.forEach(u => URL.revokeObjectURL(u));
  urls.clear();
  set({ notes: [], subjects: [], srs: {} });
  try {
    const d = await dbp?.catch(() => null);
    d?.close();
    dbp = null;
    await new Promise<void>(resolve => {
      if (typeof indexedDB === 'undefined') { resolve(); return; }
      const r = indexedDB.deleteDatabase(DB_NAME);
      r.onsuccess = r.onerror = r.onblocked = () => resolve();
    });
  } catch { /* ignore */ }
}
