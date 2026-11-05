import { test } from 'node:test';
import assert from 'node:assert/strict';
import { PDFDocument } from 'pdf-lib';
import { buildSegment } from './print-pdf';
import { segments, type Job, type Upload } from './print';

const tt = (kk: string) => kk;

async function pdfDataUrl(n: number, size: [number, number] = [595, 842]): Promise<string> {
  const d = await PDFDocument.create();
  for (let i = 0; i < n; i++) d.addPage(size).drawText(`page ${i + 1}`, { x: 40, y: 40 });
  return 'data:application/pdf;base64,' + Buffer.from(await d.save()).toString('base64');
}

const PNG_1x1 = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';

function jobWith(lines: Job['lines'], files: Upload[]): Job {
  return { id: 1, pickupCode: '4821', customer: 'Аружан Т.', lines, files, note: '', total: 1000, paymentStatus: 'unpaid' } as unknown as Job;
}
const file = (id: number, kind: Upload['kind'], url: string): Upload =>
  ({ id, name: `f${id}.${kind}`, kind, bytes: 1, pages: 3, pagesHint: null, jobId: 1, createdAt: '', deleted: false, url });
const line = (fileId: number, o: Partial<Job['lines'][number]>) => ({ kind: 'file' as const, fileId, nameKk: `f${fileId}`, nameRu: `f${fileId}`, size: 'A4', total: 1, ...o });

async function pagesOf(bytes?: Uint8Array) {
  assert.ok(bytes);
  return (await PDFDocument.load(bytes)).getPageCount();
}

test('a print-ready PDF applies the range and copies; a duplex copy with an odd count gets a blank back', async () => {
  const pdf = await pdfDataUrl(3);
  // pages 1 and 3, two copies, one-sided: 2 × 2
  let j = jobWith([line(1, { range: '1,3', copies: 2, pages: 2 })], [file(1, 'pdf', pdf)]);
  let b = await buildSegment(j, segments(j)[0], false, tt);
  assert.equal(b.pages, 4);
  assert.equal(await pagesOf(b.bytes), 4);
  // all three pages, two copies, duplex: (3 + 1 blank) × 2
  j = jobWith([line(1, { copies: 2, duplex: true, pages: 3 })], [file(1, 'pdf', pdf)]);
  b = await buildSegment(j, segments(j)[0], false, tt);
  assert.equal(b.pages, 8);
  // the student's page is copied untouched: same size, no stamp
  const out = await PDFDocument.load(b.bytes!);
  assert.deepEqual(out.getPage(0).getSize(), { width: 595, height: 842 });
});

test('images go on their paper size; Office files and unreadable PDFs are left to the operator', async () => {
  const j = jobWith([
    line(2, { copies: 3, pages: 1 }),
    line(3, { copies: 1, pages: 7 }),
    line(4, { copies: 1, pages: 2 }),
  ], [file(2, 'png', PNG_1x1), file(3, 'docx', 'data:application/octet-stream;base64,AA=='), file(4, 'pdf', 'data:application/pdf;base64,bm90IGEgcGRm')]);
  const b = await buildSegment(j, segments(j)[0], false, tt);
  assert.equal(b.pages, 3);
  assert.equal(b.manual.length, 1);
  assert.match(b.manual[0], /f3\.docx — 7×1/);
  assert.deepEqual(b.failed, ['f4.pdf']);
  const out = await PDFDocument.load(b.bytes!);
  const { width, height } = out.getPage(0).getSize();
  assert.ok(Math.abs(width - 595.28) < 0.5 && Math.abs(height - 841.89) < 0.5, `A4: ${width}×${height}`);
});

test('nothing printable: no file at all', async () => {
  const j = jobWith([line(5, { copies: 1, pages: 2 })], [file(5, 'docx', 'data:,')]);
  const b = await buildSegment(j, segments(j)[0], false, tt);
  assert.equal(b.pages, 0);
  assert.equal(b.url, '');
});
