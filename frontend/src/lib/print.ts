/* «Полиграфия» client (docs/print.md, backend/internal/printshop). Prices are integers in tiyn. The device never
   computes a price it would show as final: every total on screen comes from POST /api/print/quote or the job itself.
   Pure helpers are tested in lib/print.test.ts. */
import { money } from '@/lib/cafe';

export { money };

export type PaymentMethod = 'pickup' | 'kaspi';
export type JobStatus = 'new' | 'printing' | 'ready' | 'picked_up' | 'cancelled' | 'no_show';
export type PaymentStatus = 'unpaid' | 'pending' | 'paid' | 'refunded' | 'failed';
export type PriceState = 'final' | 'review' | 'proposed';
export type FileKind = 'pdf' | 'docx' | 'xlsx' | 'pptx' | 'jpg' | 'png';
export type ServiceKind = 'print' | 'binding' | 'lamination' | 'unit';

export interface SizePrice { bw: number; color: number; bwDuplex: number; colorDuplex: number }
export interface Pricing {
  sizes?: Record<string, SizePrice>; tiers?: { maxSheets: number; price: number }[]; sheet?: Record<string, number>;
  price?: number; unitKk?: string; unitRu?: string;
}
export interface Service {
  id: number; shopId: number; kind: ServiceKind; nameKk: string; nameRu: string; descKk: string; descRu: string;
  pricing: Pricing; online: boolean; active: boolean; sort: number;
}
export interface Goods { id: number; shopId: number; nameKk: string; nameRu: string; price: number; inStock: boolean; archived: boolean; sort: number }
export interface Shop {
  id: number; nameKk: string; nameRu: string; building: string; placeKk: string; placeRu: string; openTime: string; closeTime: string;
  openDays: number[]; slotMinutes: number; leadMinutes: number; minOrder: number; accepts: FileKind[]; retentionDays: number;
  active: boolean; paymentMode: 'pickup' | 'kaspi' | 'both'; noteKk: string; noteRu: string; openNow: boolean; payments: PaymentMethod[];
  separator?: Separator; kaspiMerchantRef?: string; kaspiOrgId?: number | null; myRole?: string;
  kaspi?: { configured: boolean; loggedIn: boolean; broken: boolean; phoneMasked: string; orgName: string; lastError: string | null; updatedAt: string | null };
  staff?: { userId: number; name: string | null; username: string | null; role: 'operator' | 'manager' }[];
}
export interface Catalog {
  shop: Shop; services: Service[]; goods: Goods[]; slots: { at: string }[];
  limits: { maxFileBytes: number; maxFiles: number; dailyFiles: number; dailyBytes: number };
}
export interface Upload {
  id: number; name: string; kind: FileKind; bytes: number; pages: number | null; pagesHint: number | null; jobId: number | null;
  createdAt: string; deleted: boolean; url?: string; urlExpiresAt?: string;
}
export interface Part { nameKk: string; nameRu: string; amount: number }
export interface Line {
  kind: 'file' | 'goods' | 'service' | 'minimum'; fileId?: number; refId?: number; nameKk: string; nameRu: string; serviceId?: number;
  size?: string; color?: boolean; duplex?: boolean; copies?: number; range?: string; filePages?: number; pages?: number; sheets?: number;
  pagesDeclared?: boolean; bindingId?: number; laminationId?: number; qty?: number; unitPrice?: number; parts?: Part[]; total: number;
}
export interface Quote { lines: Line[]; total: number; review: boolean }
export interface Job {
  id: number; shopId: number; shopNameKk: string; shopNameRu: string; placeKk: string; placeRu: string; lines: Line[]; total: number;
  priceState: PriceState; proposedTotal: number | null; adjustReason: string | null; pickupAt: string | null; pickupCode: string; note: string;
  status: JobStatus; paymentMethod: PaymentMethod; paymentStatus: PaymentStatus; refundManual: boolean;
  pay: { url: string; qr: string; expiresAt: string | null } | null; cancelReason: string | null; queuedAt: string; createdAt: string;
  printingAt: string | null; readyAt: string | null; closedAt: string | null; filesDeleted: boolean; files: Upload[]; ahead?: number; pickupToken?: string; etaMin?: number;
  customer?: string; userId?: number;
  events?: { id: number; actorId: number | null; actorKind: string; kind: string; detail: Record<string, unknown>; createdAt: string }[];
}

/** one file's options on the device (no price) */
export interface FileOpts {
  fileId: number; serviceId: number; size: string; color: boolean; duplex: boolean; copies: number; range: string;
  pages: number; bindingId: number; laminationId: number;
}

type TT = (kk: string, ru: string) => string;

export const SIZES = ['A5', 'A4', 'A3', 'A2', 'A1', 'A0'];
export const KINDS: FileKind[] = ['pdf', 'docx', 'xlsx', 'pptx', 'jpg', 'png'];
export const MAX_FILE_BYTES = 20 * 1024 * 1024;

const EXT: Record<string, FileKind> = { pdf: 'pdf', docx: 'docx', xlsx: 'xlsx', pptx: 'pptx', jpg: 'jpg', jpeg: 'jpg', png: 'png' };

/** the file picker's accept attribute for the shop's formats */
export function acceptAttr(accepts: FileKind[]): string {
  return Object.entries(EXT).filter(([, k]) => accepts.includes(k)).map(([e]) => '.' + e).join(',');
}

/** a quick check before uploading (the server checks the bytes anyway): null when fine, else an error code */
export function checkFile(f: { name: string; size: number }, accepts: FileKind[], have: number, maxFiles = 5): 'too_many' | 'too_large' | 'empty' | 'format' | null {
  if (have >= maxFiles) return 'too_many';
  if (f.size <= 0) return 'empty';
  if (f.size > MAX_FILE_BYTES) return 'too_large';
  const ext = (f.name.split('.').pop() || '').toLowerCase();
  const kind = EXT[ext];
  if (!kind || !accepts.includes(kind)) return 'format';
  return null;
}

export function fileError(code: string, tt: TT): string {
  return ({
    too_many: tt('Бір тапсырысқа ең көбі 5 файл.', 'Не больше 5 файлов в заказе.'),
    too_large: tt('Файл 20 МБ-тан аспауы керек.', 'Файл должен быть не больше 20 МБ.'),
    empty: tt('Файл бос.', 'Файл пустой.'),
    format: tt('Бұл полиграфия мұндай файлды қабылдамайды.', 'Эта полиграфия не принимает такой формат.'),
  } as Record<string, string>)[code] ?? code;
}

export const isImage = (k: FileKind) => k === 'jpg' || k === 'png';

/** sizes a print service prices, in display order */
export function sizesOf(s: Service | undefined): string[] {
  const sz = s?.pricing.sizes ?? {};
  return SIZES.filter(x => x in sz);
}
export const colorAt = (s: Service | undefined, size: string) => !!s?.pricing.sizes?.[size]?.color;
export const duplexAt = (s: Service | undefined, size: string, color: boolean) => {
  const p = s?.pricing.sizes?.[size];
  return !!p && (color ? p.colorDuplex > 0 : p.bwDuplex > 0);
};

/** the starting options of a freshly uploaded file: the first print service, A4 if priced, b/w, one copy */
export function defaultOpts(u: Upload, services: Service[]): FileOpts {
  const svc = services.find(s => s.kind === 'print' && s.online && s.active);
  const sizes = sizesOf(svc);
  const size = sizes.includes('A4') ? 'A4' : sizes[0] ?? 'A4';
  return { fileId: u.id, serviceId: svc?.id ?? 0, size, color: false, duplex: false, copies: 1, range: '', pages: u.pages ?? u.pagesHint ?? 0, bindingId: 0, laminationId: 0 };
}

/** keeps options consistent after a change: a size the service prices, colour/duplex only where priced */
export function fixOpts(o: FileOpts, services: Service[], kind: FileKind): FileOpts {
  const svc = services.find(s => s.id === o.serviceId);
  const sizes = sizesOf(svc);
  const size = sizes.includes(o.size) ? o.size : sizes.includes('A4') ? 'A4' : sizes[0] ?? o.size;
  const color = o.color && colorAt(svc, size);
  const duplex = o.duplex && !isImage(kind) && duplexAt(svc, size, color);
  const lam = services.find(s => s.id === o.laminationId);
  const laminationId = lam && lam.pricing.sheet?.[size] ? o.laminationId : 0;
  return { ...o, size, color, duplex, laminationId, copies: Math.min(100, Math.max(1, Math.round(o.copies) || 1)), range: isImage(kind) ? '' : o.range };
}

/** what the server is asked to price: ids and options only */
export function quoteBody(shopId: number, files: { opts: FileOpts; upload: Upload }[], goods: Record<number, number>, units: Record<number, number>) {
  return {
    shopId,
    files: files.map(({ opts, upload }) => ({
      fileId: opts.fileId, serviceId: opts.serviceId, size: opts.size, color: opts.color, duplex: opts.duplex, copies: opts.copies,
      range: opts.range.trim(), bindingId: opts.bindingId, laminationId: opts.laminationId,
      ...(upload.pages == null && !isImage(upload.kind) ? { pages: opts.pages } : {}),
    })),
    goods: Object.entries(goods).filter(([, q]) => q > 0).map(([id, qty]) => ({ id: Number(id), qty })),
    services: Object.entries(units).filter(([, q]) => q > 0).map(([id, qty]) => ({ id: Number(id), qty })),
  };
}

/** «1-5, 8» is well-formed (the server checks it against the real page count) */
export const rangeOK = (s: string) => s.trim() === '' || /^\s*\d{1,4}(\s*-\s*\d{1,4})?(\s*,\s*\d{1,4}(\s*-\s*\d{1,4})?)*\s*$/.test(s);

/** «A4 · түрлі түсті · 2 жақты · 3 дана · 1-5 бет» */
export function lineDetail(l: Line, tt: TT): string {
  if (l.kind !== 'file') return l.qty ? `${l.qty} × ${money(l.unitPrice ?? 0)}` : '';
  const bits = [l.size ?? '', l.color ? tt('түрлі түсті', 'цветная') : tt('ақ-қара', 'ч/б'), l.duplex ? tt('2 жақты', '2-сторонняя') : '',
    tt(`${l.copies} дана`, `${l.copies} экз.`), l.range ? tt(`беттер ${l.range}`, `стр. ${l.range}`) : '',
    tt(`${l.pages} бет`, `${l.pages} стр.`) + (l.pagesDeclared ? tt(' (студент жазды)', ' (со слов студента)') : '')];
  return bits.filter(Boolean).join(' · ');
}

export const isActive = (s: JobStatus) => s === 'new' || s === 'printing' || s === 'ready';

export function statusLabel(s: JobStatus, tt: TT): string {
  return ({
    new: tt('Жаңа', 'Новый'), printing: tt('Басып жатыр', 'Печатается'), ready: tt('Дайын', 'Готово'),
    picked_up: tt('Алынды', 'Получен'), cancelled: tt('Болдырылмады', 'Отменён'), no_show: tt('Алынбады', 'Не забран'),
  } as Record<JobStatus, string>)[s] ?? s;
}

export function priceStateLabel(s: PriceState, tt: TT): string {
  return ({ final: tt('Баға келісілді', 'Цена согласована'), review: tt('Оператор бағаны тексеріп жатыр', 'Оператор проверяет цену'),
    proposed: tt('Жаңа бағаны растаңыз', 'Подтвердите новую цену') } as Record<PriceState, string>)[s];
}

export function paymentLabel(j: Pick<Job, 'paymentMethod' | 'paymentStatus' | 'refundManual'>, tt: TT): string {
  if (j.refundManual) return tt('Ақшаны полиграфия қайтарады', 'Деньги вернёт полиграфия');
  if (j.paymentMethod === 'pickup') return j.paymentStatus === 'paid' ? tt('Кассада төленді', 'Оплачено на кассе') : tt('Кассада төлейсіз', 'Оплата на кассе');
  return ({
    unpaid: tt('Kaspi: төленбеген', 'Kaspi: не оплачен'), pending: tt('Kaspi: төлем күтілуде', 'Kaspi: ждём оплату'),
    paid: tt('Kaspi арқылы төленді', 'Оплачено через Kaspi'), refunded: tt('Kaspi-ге қайтарылды', 'Возвращено в Kaspi'),
    failed: tt('Kaspi: төлем өтпеді', 'Kaspi: оплата не прошла'),
  } as Record<PaymentStatus, string>)[j.paymentStatus];
}

/** new → printing → ready → picked_up */
export function stepIndex(s: JobStatus): number {
  return s === 'new' ? 0 : s === 'printing' ? 1 : s === 'ready' ? 2 : s === 'picked_up' ? 3 : -1;
}

/** the job screen keeps polling while something can still change */
export const shouldPoll = (j: Pick<Job, 'status' | 'paymentStatus'>) => isActive(j.status) || j.paymentStatus === 'pending';

/** the operator may print: an agreed price, and paid or paid at the counter */
export const workable = (j: Pick<Job, 'priceState' | 'paymentMethod' | 'paymentStatus'>) =>
  j.priceState === 'final' && (j.paymentMethod === 'pickup' || j.paymentStatus === 'paid');

/** what the board shows on a ticket that cannot be printed yet */
export function blocker(j: Job, tt: TT): string {
  if (j.priceState === 'review') return tt('Бетті тексеріп, бағаны растаңыз', 'Проверьте страницы и подтвердите цену');
  if (j.priceState === 'proposed') return tt('Студент жаңа бағаны растауын күтуде', 'Ждём, пока студент примет новую цену');
  if (j.paymentMethod === 'kaspi' && j.paymentStatus !== 'paid') return tt('Kaspi төлемі күтілуде', 'Ждём оплату Kaspi');
  return '';
}

/** jobs that appeared since the last board refresh (for the counter's sound) */
export function newArrivals(prev: Set<number> | null, jobs: Pick<Job, 'id' | 'status'>[]): number[] {
  if (!prev) return [];
  return jobs.filter(j => j.status === 'new' && !prev.has(j.id)).map(j => j.id);
}

/** «1,2 МБ» */
export function bytesLabel(n: number, lang: 'kk' | 'ru' | 'en'): string {
  const mb = n / (1024 * 1024);
  if (mb >= 1) return `${mb.toFixed(1).replace('.', ',')} ${lang === 'en' ? 'MB' : 'МБ'}`;
  return `${Math.max(1, Math.round(n / 1024))} ${lang === 'en' ? 'KB' : 'КБ'}`;
}

/** the price list line of a service: «A4: ақ-қара 20 ₸, түрлі түсті 100 ₸, 2 жақты 15 ₸» */
export function priceList(s: Service, tt: TT, lang: 'kk' | 'ru' | 'en'): string[] {
  const p = s.pricing;
  switch (s.kind) {
    case 'print':
      return sizesOf(s).map(size => {
        const x = p.sizes![size];
        const parts = [x.bw ? `${tt('ақ-қара', 'ч/б')} ${money(x.bw)}` : '', x.color ? `${tt('түрлі түсті', 'цвет')} ${money(x.color)}` : '',
          x.bwDuplex ? `${tt('2 жақты ақ-қара', '2-сторон. ч/б')} ${money(x.bwDuplex)}` : '', x.colorDuplex ? `${tt('2 жақты түсті', '2-сторон. цвет')} ${money(x.colorDuplex)}` : ''];
        return `${size}: ${parts.filter(Boolean).join(', ')} ${tt('/ бет', '/ стр.')}`;
      });
    case 'binding': {
      let from = 1;
      return (p.tiers ?? []).map(t => { const s = tt(`${from}–${t.maxSheets} парақ: ${money(t.price)}`, `${from}–${t.maxSheets} листов: ${money(t.price)}`); from = t.maxSheets + 1; return s; });
    }
    case 'lamination':
      return SIZES.filter(x => p.sheet?.[x]).map(x => `${x}: ${money(p.sheet![x])} ${tt('/ парақ', '/ лист')}`);
    case 'unit':
      return [`${money(p.price ?? 0)} / ${lang !== 'kk' ? p.unitRu : p.unitKk}`];
  }
}

/* ---------- the counter (docs/print.md «Беру»): print order = stack order, lookup by code or QR ---------- */

export type Separator = 'none' | 'slip' | 'full';

/** 0-based page indices of «1-5, 8» inside a file of n pages ('' = all); null when malformed or out of range */
export function rangePages(range: string, n: number): number[] | null {
  if (n < 1) return null;
  const s = range.replace(/\s+/g, '');
  if (!s) return Array.from({ length: n }, (_, i) => i);
  const seen = new Set<number>();
  for (const part of s.split(',')) {
    const m = /^(\d{1,4})(?:-(\d{1,4}))?$/.exec(part);
    if (!m) return null;
    const a = Number(m[1]), b = m[2] ? Number(m[2]) : a;
    if (a < 1 || b < a || b > n) return null;
    for (let i = a; i <= b; i++) seen.add(i - 1);
  }
  return [...seen].sort((x, y) => x - y);
}

/** one print job for the printer: consecutive file lines of a job that share paper size, colour and duplex */
export interface Segment { size: string; color: boolean; duplex: boolean; lines: { line: Line; file?: Upload }[] }

/** a file the browser cannot put into the print-ready PDF (an Office file): the operator prints it from its app */
export const manualKind = (k?: FileKind) => !!k && k !== 'pdf' && !isImage(k);

export function segments(j: Pick<Job, 'lines' | 'files'>): Segment[] {
  const out: Segment[] = [];
  for (const line of j.lines) {
    if (line.kind !== 'file') continue;
    const file = j.files.find(f => f.id === line.fileId);
    const size = line.size ?? 'A4', color = !!line.color, duplex = !!line.duplex;
    const last = out[out.length - 1];
    if (last && last.size === size && last.color === color && last.duplex === duplex) last.lines.push({ line, file });
    else out.push({ size, color, duplex, lines: [{ line, file }] });
  }
  return out;
}

/** «A4 · ч/б · 2 жақты» — what to set in the print dialog */
export function segmentLabel(s: Pick<Segment, 'size' | 'color' | 'duplex'>, tt: TT): string {
  return [s.size, s.color ? tt('түрлі түсті', 'цветная') : tt('ақ-қара', 'ч/б'), s.duplex ? tt('2 жақты', '2-сторонняя') : tt('1 жақты', '1-сторонняя')].join(' · ');
}

/** what the operator checks against the top of the stack: printed pages in all, copies, the first file */
export function stackSummary(j: Pick<Job, 'lines' | 'files'>): { pages: number; sheets: number; copies: number; files: number; first: string } {
  let pages = 0, sheets = 0, copies = 0, files = 0, first = '';
  for (const l of j.lines) {
    if (l.kind !== 'file') continue;
    files++;
    pages += (l.pages ?? 0) * (l.copies ?? 1);
    sheets += (l.sheets ?? l.pages ?? 0) * (l.copies ?? 1);
    copies = Math.max(copies, l.copies ?? 1);
    if (!first) first = j.files.find(f => f.id === l.fileId)?.name ?? l.nameKk;
  }
  return { pages, sheets, copies, files, first };
}

/** what a scan or the search box holds: the job's QR token, or a 3–4 digit code («№4821» and spaces allowed) */
export function parseCounterInput(raw: string): { token: string } | { code: string } | null {
  const s = raw.trim().replace(/^№\s*/, '');
  if (/^PJ-\d{1,18}-\d{3,4}-[0-9a-f]{20}$/.test(s)) return { token: s };
  if (/^\d{3,4}$/.test(s)) return { code: s };
  return null;
}

/** the «Дайын» pile: in queue order (as printed) or by code (as the operator keeps the tray sorted) */
export function sortReady<T extends Pick<Job, 'pickupCode' | 'queuedAt' | 'id'>>(jobs: T[], by: 'queue' | 'code'): T[] {
  const list = [...jobs];
  if (by === 'code') return list.sort((a, b) => Number(a.pickupCode) - Number(b.pickupCode) || a.id - b.id);
  return list.sort((a, b) => a.queuedAt.localeCompare(b.queuedAt) || a.id - b.id);
}

/** the batch in the order it will come out of the printer: the board's queue order, whatever order it was ticked in */
export function batchOrder<T extends Pick<Job, 'id' | 'queuedAt'>>(jobs: T[], picked: Set<number>): T[] {
  return jobs.filter(j => picked.has(j.id)).sort((a, b) => a.queuedAt.localeCompare(b.queuedAt) || a.id - b.id);
}

/** pages of a print-ready copy run: with duplex an odd count gets a blank back, so the next copy starts on a new sheet */
export function paddedPages(pages: number, duplex: boolean): number {
  return duplex && pages % 2 === 1 ? pages + 1 : pages;
}

/** slips are four to an A4 sheet */
export const slipSheets = (n: number) => Math.ceil(n / 4);

/* ---------- remembered options («как в прошлый раз») ---------- */

/** the options worth remembering per shop: never the file, its pages or its page range */
export type Preset = Pick<FileOpts, 'serviceId' | 'size' | 'color' | 'duplex' | 'copies' | 'bindingId' | 'laminationId'>;

export const presetKey = (shopId: number) => `print:preset:${shopId}`;

export function presetOf(o: FileOpts): Preset {
  return { serviceId: o.serviceId, size: o.size, color: o.color, duplex: o.duplex, copies: o.copies, bindingId: o.bindingId, laminationId: o.laminationId };
}

/** a stored preset read back defensively (another device's or an old shape): null when unusable */
export function parsePreset(raw: string | null): Preset | null {
  if (!raw) return null;
  try {
    const v = JSON.parse(raw) as Record<string, unknown>;
    const n = (x: unknown) => (typeof x === 'number' && Number.isFinite(x) ? x : 0);
    if (typeof v !== 'object' || !v || typeof v.size !== 'string') return null;
    return { serviceId: n(v.serviceId), size: v.size, color: v.color === true, duplex: v.duplex === true, copies: n(v.copies) || 1, bindingId: n(v.bindingId), laminationId: n(v.laminationId) };
  } catch { return null; }
}

/** a fresh upload's options with the remembered ones on top, kept consistent with what the shop prices today
    (a service gone, a size no longer priced, binding on an image: fixOpts and the checks below drop them) */
export function withPreset(o: FileOpts, p: Preset | null, services: Service[], kind: FileKind): FileOpts {
  if (!p) return o;
  const svc = services.find(s => s.id === p.serviceId && s.kind === 'print' && s.online && s.active);
  const bind = services.find(s => s.id === p.bindingId && s.kind === 'binding' && s.online && s.active);
  const lam = services.find(s => s.id === p.laminationId && s.kind === 'lamination' && s.online && s.active);
  return fixOpts({
    ...o, serviceId: svc ? svc.id : o.serviceId, size: p.size, color: p.color, duplex: p.duplex, copies: p.copies,
    bindingId: bind && !isImage(kind) ? bind.id : 0, laminationId: lam ? lam.id : 0,
  }, services, kind);
}

/** «≈ 15 мин» on the job screen: rough, from the queue ahead */
export function etaLabel(min: number | undefined, tt: TT): string {
  if (!min) return '';
  if (min >= 120) return tt(`≈ ${Math.round(min / 60)} сағатта дайын`, `Готово через ≈ ${Math.round(min / 60)} ч`);
  return tt(`≈ ${min} минутта дайын`, `Готово через ≈ ${min} мин`);
}
