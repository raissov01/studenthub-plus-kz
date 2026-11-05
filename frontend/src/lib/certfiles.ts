/* Certificate files in the portfolio (PDF, JPG, PNG): API shapes (docs/api/portfolio.md «Certificate files»; the Go
   code in backend/internal/portfolio/certfiles.go wins), categories, limits and the checks the browser runs before an
   upload or before saving a file on the device. Free of React and of browser-only APIs, so it is unit-tested in Node. */
import type { LText } from '@/lib/types';

export type CertCategory = 'course' | 'olympiad' | 'competition' | 'volunteering' | 'language' | 'other';
export type CertKind = 'pdf' | 'image';

/** the descriptive part, the same on the server and on the device */
export interface CertMeta {
  title: string;
  issuer: string;
  issuedOn: string | null; // YYYY-MM-DD
  credentialId: string;
  credentialUrl: string;
  category: CertCategory;
}
/** GET /api/portfolio/certificates → certificates[] */
export interface ServerCert extends CertMeta {
  id: number; kind: CertKind; mime: string; bytes: number; width: number | null; height: number | null;
  public: boolean; url: string; createdAt: string;
}
export interface CertUsage { files: number; bytes: number; maxFiles: number; maxBytes: number }
export interface CertList { certificates: ServerCert[]; usage: CertUsage; pageVisible: boolean }
/** a public file on /p/<slug> (selfReported.files) */
export interface PublicCertFile {
  title: string; issuer: string; issuedOn: string | null; category: CertCategory; credentialUrl: string;
  kind: CertKind; bytes: number; width: number | null; height: number | null; url: string; verified: false;
}

export const CERT_LIMITS = {
  pdf: 10 * 1024 * 1024,
  image: 8 * 1024 * 1024,
  files: 30,
  bytes: 100 * 1024 * 1024,
  side: 1600,
  title: 120,
  issuer: 120,
  credentialId: 100,
  credentialUrl: 500,
};

export const CERT_CATEGORIES: [CertCategory, LText][] = [
  ['course', { kk: 'Курс', ru: 'Курс' }],
  ['olympiad', { kk: 'Олимпиада', ru: 'Олимпиада' }],
  ['competition', { kk: 'Байқау, жарыс', ru: 'Конкурс, соревнование' }],
  ['volunteering', { kk: 'Волонтерлік', ru: 'Волонтёрство' }],
  ['language', { kk: 'Тіл сынағы (IELTS т.б.)', ru: 'Языковой тест (IELTS и др.)' }],
  ['other', { kk: 'Басқа', ru: 'Другое' }],
];
export const categoryLabel = (c: string): LText => (CERT_CATEGORIES.find(([k]) => k === c) ?? CERT_CATEGORIES[5])[1];
export const isCategory = (c: unknown): c is CertCategory => CERT_CATEGORIES.some(([k]) => k === c);

/** the accept attribute of the file input: PDF and photos (the camera is offered on phones for image/*) */
export const CERT_ACCEPT = 'application/pdf,.pdf,image/jpeg,image/png,image/webp,image/*';

/** the real type of a file from its first bytes; the name and the declared type are not trusted */
export function sniffCert(head: Uint8Array): 'pdf' | 'jpeg' | 'png' | 'webp' | '' {
  const s = (a: number, b: number) => String.fromCharCode(...head.subarray(a, b));
  if (head.length >= 8 && s(0, 5) === '%PDF-' && (head[5] === 0x31 || head[5] === 0x32) && head[6] === 0x2e) return 'pdf';
  if (head.length >= 3 && head[0] === 0xff && head[1] === 0xd8 && head[2] === 0xff) return 'jpeg';
  if (head.length >= 8 && head[0] === 0x89 && s(1, 4) === 'PNG' && head[4] === 0x0d && head[5] === 0x0a && head[6] === 0x1a && head[7] === 0x0a) return 'png';
  if (head.length >= 12 && s(0, 4) === 'RIFF' && s(8, 12) === 'WEBP') return 'webp';
  return '';
}

export type FileProblem = '' | 'type' | 'too_large' | 'empty';
/** what is wrong with a picked file (head = its first 16 bytes) */
export function checkCertFile(size: number, head: Uint8Array): { kind: CertKind | null; problem: FileProblem } {
  if (size <= 0) return { kind: null, problem: 'empty' };
  const t = sniffCert(head);
  if (!t) return { kind: null, problem: 'type' };
  const kind: CertKind = t === 'pdf' ? 'pdf' : 'image';
  if (size > (kind === 'pdf' ? CERT_LIMITS.pdf : CERT_LIMITS.image)) return { kind, problem: 'too_large' };
  return { kind, problem: '' };
}

export function problemText(p: FileProblem, tt: (kk: string, ru: string) => string): string {
  switch (p) {
    case 'type': return tt('Тек PDF, JPG немесе PNG файл қосуға болады.', 'Можно добавить только PDF, JPG или PNG.');
    case 'too_large': return tt('Файл тым үлкен: PDF — 10 МБ-қа дейін, сурет — 8 МБ-қа дейін.', 'Файл слишком большой: PDF — до 10 МБ, фото — до 8 МБ.');
    case 'empty': return tt('Файл бос.', 'Файл пустой.');
    default: return '';
  }
}

/** 1 536 000 → «1,5 МБ» */
export function fmtBytes(n: number): string {
  if (n < 1024) return `${Math.max(1, Math.round(n))} Б`;
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} КБ`;
  const v = n / (1024 * 1024);
  return `${(v >= 10 ? Math.round(v).toString() : v.toFixed(1)).replace('.', ',')} МБ`;
}

/** the long side scaled to at most `max` */
export function fitSide(w: number, h: number, max = CERT_LIMITS.side): { w: number; h: number } {
  const k = Math.min(1, max / Math.max(w, h));
  return { w: Math.max(1, Math.round(w * k)), h: Math.max(1, Math.round(h * k)) };
}

export type MetaProblem = '' | 'title' | 'issuer' | 'credentialId' | 'credentialUrl' | 'issuedOn';
/** trims the fields and checks them the way the server does */
export function cleanCertMeta(m: CertMeta, today = new Date()): { meta: CertMeta; problem: MetaProblem } {
  const meta: CertMeta = {
    title: m.title.trim(), issuer: m.issuer.trim(), issuedOn: m.issuedOn?.trim() || null,
    credentialId: m.credentialId.trim(), credentialUrl: m.credentialUrl.trim(), category: isCategory(m.category) ? m.category : 'other',
  };
  if (!meta.title || meta.title.length > CERT_LIMITS.title) return { meta, problem: 'title' };
  if (meta.issuer.length > CERT_LIMITS.issuer) return { meta, problem: 'issuer' };
  if (meta.credentialId.length > CERT_LIMITS.credentialId) return { meta, problem: 'credentialId' };
  if (meta.credentialUrl) {
    if (!/^https:\/\//i.test(meta.credentialUrl) && !/^[a-z]+:/i.test(meta.credentialUrl) && /^[\w-]+(\.[\w-]+)+/.test(meta.credentialUrl)) meta.credentialUrl = 'https://' + meta.credentialUrl;
    if (!/^https:\/\/[^\s<>"'\\]+$/i.test(meta.credentialUrl) || meta.credentialUrl.length > CERT_LIMITS.credentialUrl) return { meta, problem: 'credentialUrl' };
  }
  if (meta.issuedOn) {
    const ok = /^\d{4}-\d{2}-\d{2}$/.test(meta.issuedOn) && !Number.isNaN(Date.parse(meta.issuedOn + 'T00:00:00Z'));
    const year = Number(meta.issuedOn.slice(0, 4));
    if (!ok || year < 1950 || Date.parse(meta.issuedOn + 'T00:00:00Z') > today.getTime() + 2 * 86400_000) return { meta, problem: 'issuedOn' };
  }
  return { meta, problem: '' };
}

/** a safe file name for «Жүктеп алу» */
export function downloadName(title: string, kind: CertKind, mime = ''): string {
  const base = title.replace(/[^\p{L}\p{N} _.-]+/gu, ' ').replace(/\s+/g, ' ').trim().replace(/^[. ]+|[. ]+$/g, '').slice(0, 80) || 'certificate';
  const ext = kind === 'pdf' ? 'pdf' : mime === 'image/webp' ? 'webp' : mime === 'image/png' ? 'png' : 'jpg';
  return `${base}.${ext}`;
}
