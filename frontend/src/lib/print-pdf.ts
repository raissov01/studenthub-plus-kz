/* The operator's print-ready PDFs (docs/print.md «Басу ретi»), built in the operator's own browser: pdf-lib is
   loaded only when the operator asks, and nothing of it runs on the server (the API container has a 150 MB cap and
   no converter). The student's pages are copied untouched — never stamped, never rescaled; page ranges and copies
   are applied, and a duplex copy with an odd page count gets a blank back so the next copy starts on a fresh sheet.
   Office files cannot be merged here: they are listed for the operator to print from their own app.
   Slips and covers are drawn on a canvas with the site's own fonts (every Kazakh letter) and placed as images. */
import {
  manualKind, paddedPages, rangePages, segmentLabel, stackSummary, type Job, type Segment,
} from '@/lib/print';
import { money } from '@/lib/cafe';

type TT = (kk: string, ru: string) => string;

const MM = 72 / 25.4;
/** paper sizes in PDF points (portrait) */
export const PAPER: Record<string, [number, number]> = {
  A5: [148 * MM, 210 * MM], A4: [210 * MM, 297 * MM], A3: [297 * MM, 420 * MM],
  A2: [420 * MM, 594 * MM], A1: [594 * MM, 841 * MM], A0: [841 * MM, 1189 * MM],
};

export interface Built { url: string; pages: number; manual: string[]; failed: string[]; bytes?: Uint8Array }

const loadLib = () => import('pdf-lib');

async function bytesOf(url: string): Promise<ArrayBuffer> {
  const r = await fetch(url, { credentials: 'same-origin', cache: 'no-store' });
  if (!r.ok) throw new Error(String(r.status));
  return r.arrayBuffer();
}

/* ---------- the slip / cover drawing ---------- */

export interface TicketText { code: string; name: string; lines: string[]; foot: string }

/** what a slip or cover says: no file contents, only what the counter needs to match the stack */
export function ticketText(j: Job, tt: TT): TicketText {
  const s = stackSummary(j);
  const lines: string[] = [];
  lines.push(tt(`${s.files} файл · ${s.pages} бет барлығы`, `${s.files} файл. · ${s.pages} стр. всего`));
  for (const l of j.lines) {
    if (l.kind === 'file') {
      const f = j.files.find(x => x.id === l.fileId);
      const extra = (l.parts ?? []).slice(1).map(p => p.nameKk).join(', ');
      lines.push(`${f?.name ?? l.nameKk} — ${l.pages ?? 0}×${l.copies ?? 1}${extra ? ' · ' + extra : ''}`);
    } else if (l.kind !== 'minimum') {
      lines.push(`${l.qty ?? 1} × ${l.nameKk}`);
    }
  }
  if (j.note) lines.push(tt('Ескертпе: ', 'Комментарий: ') + j.note);
  const paid = j.paymentStatus === 'paid';
  const foot = (paid ? tt('Төленді', 'Оплачено') : tt('Кассада төлейді', 'Оплата на кассе')) + ` · ${money(j.total)} · #${j.id}`;
  return { code: j.pickupCode, name: j.customer ?? '', lines: lines.slice(0, 9), foot };
}

async function fontsReady() {
  try {
    await Promise.all([document.fonts.load('700 40px Geologica', 'ӘҒҚҢӨҰҮҺІ'), document.fonts.load('400 20px "Golos Text"', 'әғқңөұүһі')]);
  } catch { /* the fallback font draws it */ }
}

function fit(ctx: CanvasRenderingContext2D, text: string, max: number): string {
  if (ctx.measureText(text).width <= max) return text;
  let t = text;
  while (t.length > 1 && ctx.measureText(t + '…').width > max) t = t.slice(0, -1);
  return t + '…';
}

/** one ticket on a canvas of w×h px: a thick bar on top (easy to spot in a pile), the code, the name, the lines */
function drawTicket(ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, t: TicketText) {
  const pad = w * 0.07;
  ctx.fillStyle = '#ffffff';
  ctx.fillRect(x, y, w, h);
  ctx.fillStyle = '#111111';
  ctx.fillRect(x, y, w, h * 0.06);
  ctx.textBaseline = 'top';
  let cy = y + h * 0.1;
  ctx.font = `700 ${Math.round(h * 0.2)}px Geologica, sans-serif`;
  ctx.fillText('№' + t.code, x + pad, cy);
  cy += h * 0.24;
  ctx.font = `700 ${Math.round(h * 0.065)}px Geologica, sans-serif`;
  ctx.fillText(fit(ctx, t.name, w - 2 * pad), x + pad, cy);
  cy += h * 0.1;
  ctx.font = `400 ${Math.round(h * 0.036)}px "Golos Text", sans-serif`;
  for (const l of t.lines) {
    if (cy > y + h * 0.86) break;
    ctx.fillText(fit(ctx, l, w - 2 * pad), x + pad, cy);
    cy += h * 0.05;
  }
  ctx.font = `700 ${Math.round(h * 0.036)}px "Golos Text", sans-serif`;
  ctx.fillText(fit(ctx, t.foot, w - 2 * pad), x + pad, y + h * 0.92);
}

async function canvasPng(w: number, h: number, paint: (ctx: CanvasRenderingContext2D) => void): Promise<Uint8Array> {
  const c = document.createElement('canvas');
  c.width = w; c.height = h;
  const ctx = c.getContext('2d');
  if (!ctx) throw new Error('canvas');
  paint(ctx);
  const blob = await new Promise<Blob | null>(res => c.toBlob(res, 'image/png'));
  if (!blob) throw new Error('canvas');
  return new Uint8Array(await blob.arrayBuffer());
}

function toUrl(bytes: Uint8Array): string {
  const copy = new Uint8Array(bytes.byteLength);
  copy.set(bytes);
  return URL.createObjectURL(new Blob([copy.buffer], { type: 'application/pdf' }));
}

/* ---------- PDFs ---------- */

/** slips for a batch, four to an A4 sheet (A6 each, thin cut marks), in the batch's order */
export async function buildSlips(jobs: Job[], tt: TT): Promise<Built> {
  const { PDFDocument, rgb } = await loadLib();
  await fontsReady();
  const doc = await PDFDocument.create();
  const [W, H] = PAPER.A4;
  const px = 2; // canvas px per point
  for (let i = 0; i < jobs.length; i += 4) {
    const group = jobs.slice(i, i + 4);
    const png = await canvasPng(Math.round(W * px), Math.round(H * px), ctx => {
      ctx.fillStyle = '#ffffff';
      ctx.fillRect(0, 0, W * px, H * px);
      group.forEach((j, k) => {
        const cw = (W * px) / 2, ch = (H * px) / 2;
        const m = 14 * MM * px * 0.5;
        drawTicket(ctx, (k % 2) * cw + m, Math.floor(k / 2) * ch + m, cw - 2 * m, ch - 2 * m, ticketText(j, tt));
      });
    });
    const page = doc.addPage([W, H]);
    page.drawImage(await doc.embedPng(png), { x: 0, y: 0, width: W, height: H });
    page.drawLine({ start: { x: W / 2, y: 0 }, end: { x: W / 2, y: H }, thickness: 0.4, color: rgb(0.6, 0.6, 0.6), dashArray: [3, 3] });
    page.drawLine({ start: { x: 0, y: H / 2 }, end: { x: W, y: H / 2 }, thickness: 0.4, color: rgb(0.6, 0.6, 0.6), dashArray: [3, 3] });
  }
  const bytes = await doc.save();
  return { url: toUrl(bytes), pages: doc.getPageCount(), manual: [], failed: [], bytes };
}

/** a hard cap on one print-ready file: beyond it the copies are left to the print dialog */
const MAX_PAGES = 3000;

/**
 * One segment of a job (same size, colour and duplex) as one print-ready PDF: each PDF file's chosen pages, each
 * image on its paper size, copy after copy. cover: a full cover page first (the shop's «full» setting), with a
 * blank back when duplex. Office files and PDFs the browser cannot open are returned in manual / failed.
 */
export async function buildSegment(j: Job, seg: Segment, cover: boolean, tt: TT): Promise<Built> {
  const { PDFDocument } = await loadLib();
  const doc = await PDFDocument.create();
  const paper = PAPER[seg.size] ?? PAPER.A4;
  const manual: string[] = [], failed: string[] = [];
  if (cover) {
    await fontsReady();
    const [W, H] = PAPER.A4;
    const png = await canvasPng(Math.round(W * 2), Math.round(H * 2), ctx => drawTicket(ctx, 0, 0, W * 2, H * 2 * 0.5, ticketText(j, tt)));
    const p = doc.addPage([W, H]);
    p.drawImage(await doc.embedPng(png), { x: 0, y: 0, width: W, height: H });
    if (seg.duplex) doc.addPage([W, H]);
  }
  for (const { line, file } of seg.lines) {
    const copies = Math.max(1, line.copies ?? 1);
    if (!file || file.deleted || !file.url) { failed.push(line.nameKk); continue; }
    if (manualKind(file.kind)) { manual.push(`${file.name} — ${line.pages ?? 0}×${copies}${line.range ? ` (${line.range})` : ''}`); continue; }
    try {
      const raw = await bytesOf(file.url);
      if (file.kind === 'pdf') {
        const src = await PDFDocument.load(raw, { updateMetadata: false });
        const idx = rangePages(line.range ?? '', src.getPageCount());
        if (!idx || !idx.length) { failed.push(file.name); continue; }
        let n = copies;
        if (doc.getPageCount() + idx.length * copies > MAX_PAGES) { n = 1; manual.push(tt(`${file.name}: даналарды (${copies}) басып шығару терезесінде қойыңыз`, `${file.name}: копии (${copies}) задайте в окне печати`)); }
        for (let c = 0; c < n; c++) {
          const pages = await doc.copyPages(src, idx);
          pages.forEach(p => doc.addPage(p));
          if (paddedPages(idx.length, seg.duplex) > idx.length) {
            const last = pages[pages.length - 1].getSize();
            doc.addPage([last.width, last.height]);
          }
        }
      } else {
        const img = file.kind === 'png' ? await doc.embedPng(raw) : await doc.embedJpg(raw);
        const landscape = img.width > img.height;
        const [W, H] = landscape ? [paper[1], paper[0]] : paper;
        const s = Math.min(W / img.width, H / img.height);
        for (let c = 0; c < copies; c++) {
          const p = doc.addPage([W, H]);
          p.drawImage(img, { x: (W - img.width * s) / 2, y: (H - img.height * s) / 2, width: img.width * s, height: img.height * s });
        }
      }
    } catch {
      failed.push(file.name); // encrypted or damaged: the operator opens the original
    }
  }
  const pages = doc.getPageCount();
  if (!pages) return { url: '', pages: 0, manual, failed };
  const bytes = await doc.save();
  return { url: toUrl(bytes), pages, manual, failed, bytes };
}

export { segmentLabel };
