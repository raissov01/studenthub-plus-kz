/* Certificate files, browser side.
   - Signed in: the files live on the server (GET/POST/PATCH/DELETE /api/portfolio/certificates, the file itself at
     /api/portfolio/files/<name>, fetched with the session so the Mini App's Bearer token works too).
   - Signed out: the file stays on this device, in IndexedDB "studenthub-certs" (store "certs": {id, ...CertMeta, kind,
     mime, bytes, width, height, blob, created}). Nothing leaves the device until the student signs in and uploads it.
   Photos are shrunk in the browser first: longest side ≤ 1600 px, WebP (JPEG where the browser cannot encode WebP);
   drawing to a canvas also drops EXIF / GPS. */
import { useEffect, useSyncExternalStore } from 'react';
import { api, apiBlob, apiUpload } from '@/lib/account';
import { CERT_LIMITS, checkCertFile, fitSide, type CertKind, type CertList, type CertMeta, type FileProblem, type ServerCert } from '@/lib/certfiles';

/* ---------- picking a file ---------- */

export interface PickedFile { blob: Blob; kind: CertKind; mime: string; bytes: number; width: number | null; height: number | null; name: string }

/** reads the first bytes, checks the real type and size, and shrinks photos; throws {problem} on a bad file */
export async function prepareFile(file: File): Promise<PickedFile> {
  const head = new Uint8Array(await file.slice(0, 16).arrayBuffer());
  const { kind, problem } = checkCertFile(file.size, head);
  if (problem || !kind) throw Object.assign(new Error(problem || 'type'), { problem: (problem || 'type') as FileProblem });
  if (kind === 'pdf') {
    // re-typed: the blob is shown as a PDF only, whatever the name said
    return { blob: new Blob([file], { type: 'application/pdf' }), kind, mime: 'application/pdf', bytes: file.size, width: null, height: null, name: file.name };
  }
  try {
    const out = await shrinkImage(file);
    return { ...out, kind, name: file.name };
  } catch {
    // the browser cannot decode it (rare formats): JPEG / PNG go as they are; the server re-encodes them anyway
    const type = head[0] === 0xff ? 'image/jpeg' : head[0] === 0x89 ? 'image/png' : 'image/webp';
    return { blob: new Blob([file], { type }), kind, mime: type, bytes: file.size, width: null, height: null, name: file.name };
  }
}

async function shrinkImage(file: Blob): Promise<{ blob: Blob; mime: string; bytes: number; width: number; height: number }> {
  const bmp = await createImageBitmap(file, { imageOrientation: 'from-image' } as ImageBitmapOptions);
  const { w, h } = fitSide(bmp.width, bmp.height, CERT_LIMITS.side);
  const c = document.createElement('canvas');
  c.width = w; c.height = h;
  const ctx = c.getContext('2d');
  if (!ctx) throw new Error('canvas');
  ctx.fillStyle = '#FFFFFF';
  ctx.fillRect(0, 0, w, h);
  ctx.drawImage(bmp, 0, 0, w, h);
  bmp.close?.();
  const enc = (type: string, q: number) => new Promise<Blob | null>(r => c.toBlob(r, type, q));
  let q = 0.86;
  let out = await enc('image/webp', q);
  const type = out?.type === 'image/webp' ? 'image/webp' : 'image/jpeg'; // Safari < 16 hands back a PNG for webp
  if (type !== 'image/webp') out = await enc(type, q);
  while (out && out.size > 900_000 && q > 0.5) { q -= 0.12; out = await enc(type, q); }
  if (!out) throw new Error('encode');
  return { blob: out, mime: type, bytes: out.size, width: w, height: h };
}

/* ---------- the server ---------- */

export const certsApi = {
  list: (lang: 'kk' | 'ru') => api<CertList>('/api/portfolio/certificates', { lang }),
  upload(meta: CertMeta, file: Blob, fileName: string, lang: 'kk' | 'ru') {
    const fd = new FormData();
    fd.append('title', meta.title);
    fd.append('issuer', meta.issuer);
    if (meta.issuedOn) fd.append('issuedOn', meta.issuedOn);
    fd.append('credentialId', meta.credentialId);
    fd.append('credentialUrl', meta.credentialUrl);
    fd.append('category', meta.category);
    fd.append('file', file, fileName);
    return apiUpload<{ certificate: ServerCert }>('/api/portfolio/certificates', fd, lang);
  },
  patch: (id: number, body: Partial<CertMeta> & { public?: boolean }, lang: 'kk' | 'ru') =>
    api<{ certificate: ServerCert }>(`/api/portfolio/certificates/${id}`, { method: 'PATCH', body, lang }),
  remove: (id: number, lang: 'kk' | 'ru') => api(`/api/portfolio/certificates/${id}`, { method: 'DELETE', lang }),
  /** the file itself as a typed blob (the session goes along; the type comes from our record, not the response) */
  async file(c: ServerCert, lang: 'kk' | 'ru'): Promise<Blob> {
    const b = await apiBlob(c.url, lang);
    return new Blob([b], { type: c.kind === 'pdf' ? 'application/pdf' : 'image/jpeg' });
  },
};

/* ---------- the device ---------- */

export interface DeviceCert extends CertMeta {
  id: string; kind: CertKind; mime: string; bytes: number; width: number | null; height: number | null; blob: Blob; created: number;
}

const DB_NAME = 'studenthub-certs';
const STORE = 'certs';
let dbp: Promise<IDBDatabase> | null = null;
function db(): Promise<IDBDatabase> {
  if (dbp) return dbp;
  dbp = new Promise<IDBDatabase>((resolve, reject) => {
    if (typeof indexedDB === 'undefined') { reject(new Error('no indexedDB')); return; }
    const req = indexedDB.open(DB_NAME, 1);
    req.onupgradeneeded = () => { if (!req.result.objectStoreNames.contains(STORE)) req.result.createObjectStore(STORE, { keyPath: 'id' }); };
    req.onsuccess = () => { const d = req.result; d.onversionchange = () => { d.close(); dbp = null; }; resolve(d); };
    req.onerror = () => reject(req.error);
    req.onblocked = () => reject(new Error('blocked'));
  });
  dbp.catch(() => { dbp = null; });
  return dbp;
}
async function run<T>(mode: IDBTransactionMode, fn: (s: IDBObjectStore) => IDBRequest<T> | void): Promise<T | undefined> {
  const d = await db();
  return new Promise<T | undefined>((resolve, reject) => {
    const t = d.transaction(STORE, mode);
    let out: T | undefined;
    const req = fn(t.objectStore(STORE));
    if (req) req.onsuccess = () => { out = req.result; };
    t.oncomplete = () => resolve(out);
    t.onerror = () => reject(t.error);
    t.onabort = () => reject(t.error || new Error('aborted'));
  });
}

export interface DeviceSnap { ready: boolean; durable: boolean; list: DeviceCert[] }
let snap: DeviceSnap = { ready: false, durable: true, list: [] };
const SERVER_SNAP = snap;
const subs = new Set<() => void>();
const emit = (next: Partial<DeviceSnap>) => { snap = { ...snap, ...next }; subs.forEach(l => l()); };
let started = false;

async function load() {
  try {
    const all = (await run<DeviceCert[]>('readonly', s => s.getAll() as IDBRequest<DeviceCert[]>)) ?? [];
    emit({ ready: true, durable: true, list: all.sort((a, b) => b.created - a.created) });
  } catch {
    emit({ ready: true, durable: false, list: [] });
  }
}

/** the certificates kept on this device (loads once) */
export function useDeviceCerts(): DeviceSnap {
  useEffect(() => { if (!started) { started = true; void load(); } }, []);
  return useSyncExternalStore(l => { subs.add(l); return () => { subs.delete(l); }; }, () => snap, () => SERVER_SNAP);
}

const newId = () => (typeof crypto !== 'undefined' && 'randomUUID' in crypto ? crypto.randomUUID() : `c${Date.now().toString(36)}${Math.random().toString(36).slice(2)}`);

export const deviceCerts = {
  async add(meta: CertMeta, f: PickedFile): Promise<DeviceCert> {
    const rec: DeviceCert = { ...meta, id: newId(), kind: f.kind, mime: f.mime, bytes: f.bytes, width: f.width, height: f.height, blob: f.blob, created: Date.now() };
    await run('readwrite', s => s.put(rec));
    emit({ list: [rec, ...snap.list] });
    return rec;
  },
  async update(id: string, meta: CertMeta): Promise<void> {
    const cur = snap.list.find(c => c.id === id);
    if (!cur) return;
    const rec = { ...cur, ...meta };
    await run('readwrite', s => s.put(rec));
    emit({ list: snap.list.map(c => (c.id === id ? rec : c)) });
  },
  async remove(id: string): Promise<void> {
    await run('readwrite', s => s.delete(id));
    emit({ list: snap.list.filter(c => c.id !== id) });
  },
};
