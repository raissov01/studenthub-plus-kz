import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  acceptAttr, blocker, checkFile, defaultOpts, fixOpts, lineDetail, newArrivals, paymentLabel, priceList, quoteBody, rangeOK, shouldPoll,
  sizesOf, statusLabel, stepIndex, workable, type Job, type Service, type Upload,
} from './print';

const tt = (kk: string) => kk;
const sp = (s: string) => s.replace(/\u00a0/g, ' ');
const print: Service = {
  id: 1, shopId: 1, kind: 'print', nameKk: 'Басып шығару', nameRu: 'Печать', descKk: '', descRu: '', online: true, active: true, sort: 0,
  pricing: { sizes: { A3: { bw: 4000, color: 0, bwDuplex: 0, colorDuplex: 0 }, A4: { bw: 2000, color: 10000, bwDuplex: 1500, colorDuplex: 0 } } },
};
const lam: Service = { ...print, id: 3, kind: 'lamination', pricing: { sheet: { A4: 15000 } } };
const pdf: Upload = { id: 9, name: 'a.pdf', kind: 'pdf', bytes: 1000, pages: 12, pagesHint: null, jobId: null, createdAt: '', deleted: false };
const docx: Upload = { ...pdf, id: 10, name: 'b.docx', kind: 'docx', pages: null, pagesHint: 7 };
const png: Upload = { ...pdf, id: 11, name: 'c.png', kind: 'png', pages: 1 };

test('the picker and the pre-check follow the shop formats; the server still checks the bytes', () => {
  assert.equal(acceptAttr(['pdf', 'jpg']), '.pdf,.jpg,.jpeg');
  assert.equal(checkFile({ name: 'x.PDF', size: 10 }, ['pdf'], 0), null);
  assert.equal(checkFile({ name: 'x.doc', size: 10 }, ['pdf', 'docx'], 0), 'format');
  assert.equal(checkFile({ name: 'x.pdf', size: 21 * 1024 * 1024 }, ['pdf'], 0), 'too_large');
  assert.equal(checkFile({ name: 'x.pdf', size: 10 }, ['pdf'], 5), 'too_many');
  assert.equal(checkFile({ name: 'x.pdf', size: 0 }, ['pdf'], 0), 'empty');
});

test('options start sensible and stay consistent with what the service prices', () => {
  assert.deepEqual(sizesOf(print), ['A4', 'A3']);
  const o = defaultOpts(pdf, [print, lam]);
  assert.equal(o.size, 'A4'); assert.equal(o.copies, 1); assert.equal(o.pages, 12);
  assert.equal(defaultOpts(docx, [print]).pages, 7); // the Office hint pre-fills the declared count
  // A3 has no colour and no duplex; lamination is A4 only
  const f = fixOpts({ ...o, size: 'A3', color: true, duplex: true, laminationId: 3, copies: 500 }, [print, lam], 'pdf');
  assert.equal(f.color, false); assert.equal(f.duplex, false); assert.equal(f.laminationId, 0); assert.equal(f.copies, 100);
  // colour duplex is not priced at A4 either
  assert.equal(fixOpts({ ...o, color: true, duplex: true }, [print], 'pdf').duplex, false);
  assert.equal(fixOpts({ ...o, duplex: true }, [print], 'pdf').duplex, true);
  // an image is one page: no duplex, no range
  assert.equal(fixOpts({ ...o, fileId: 11, duplex: true, range: '1' }, [print], 'png').duplex, false);
});

test('the quote request carries ids and options only, and pages only where the server cannot count', () => {
  const body = quoteBody(4, [{ opts: defaultOpts(pdf, [print]), upload: pdf }, { opts: { ...defaultOpts(docx, [print]), pages: 9 }, upload: docx }, { opts: defaultOpts(png, [print]), upload: png }],
    { 5: 2, 6: 0 }, { 7: 3 });
  assert.equal(body.shopId, 4);
  assert.equal('pages' in body.files[0], false);
  assert.equal(body.files[1].pages, 9);
  assert.equal('pages' in body.files[2], false);
  assert.deepEqual(body.goods, [{ id: 5, qty: 2 }]);
  assert.deepEqual(body.services, [{ id: 7, qty: 3 }]);
  assert.equal(JSON.stringify(body).includes('total'), false);
});

test('page ranges are checked for shape before asking the server', () => {
  for (const ok of ['', '1', '1-5', '1-5, 8', ' 2 - 4 ,9']) assert.equal(rangeOK(ok), true, ok);
  for (const bad of ['a', '1-', '-3', '1,,2', '1-2-3']) assert.equal(rangeOK(bad), false, bad);
});

test('labels, steps, polling and what blocks the operator', () => {
  assert.equal(statusLabel('printing', tt), 'Басып жатыр');
  assert.deepEqual(['new', 'printing', 'ready', 'picked_up', 'cancelled'].map(s => stepIndex(s as Job['status'])), [0, 1, 2, 3, -1]);
  assert.equal(shouldPoll({ status: 'ready', paymentStatus: 'unpaid' }), true);
  assert.equal(shouldPoll({ status: 'picked_up', paymentStatus: 'paid' }), false);
  const base = { priceState: 'final', paymentMethod: 'pickup', paymentStatus: 'unpaid', refundManual: false } as Job;
  assert.equal(workable(base), true);
  assert.equal(workable({ ...base, paymentMethod: 'kaspi' }), false);
  assert.equal(workable({ ...base, priceState: 'review' }), false);
  assert.match(blocker({ ...base, priceState: 'review' } as Job, tt), /растаңыз/);
  assert.match(blocker({ ...base, paymentMethod: 'kaspi' } as Job, tt), /Kaspi/);
  assert.equal(paymentLabel({ ...base, paymentStatus: 'paid' }, tt), 'Кассада төленді');
  assert.deepEqual(newArrivals(new Set([1]), [{ id: 1, status: 'new' }, { id: 2, status: 'new' }, { id: 3, status: 'ready' }]), [2]);
  assert.deepEqual(newArrivals(null, [{ id: 2, status: 'new' }]), []);
});

test('line details and the price list read like the shop board', () => {
  assert.equal(lineDetail({ kind: 'file', nameKk: 'a', nameRu: 'a', size: 'A4', color: false, duplex: true, copies: 2, range: '1-5', pages: 5, total: 1 }, tt),
    'A4 · ақ-қара · 2 жақты · 2 дана · беттер 1-5 · 5 бет');
  assert.equal(sp(priceList(print, tt, 'kk')[0]), 'A4: ақ-қара 20 ₸, түрлі түсті 100 ₸, 2 жақты ақ-қара 15 ₸ / бет');
  assert.deepEqual(priceList({ ...print, kind: 'binding', pricing: { tiers: [{ maxSheets: 50, price: 50000 }, { maxSheets: 150, price: 80000 }] } }, tt, 'kk').map(sp),
    ['1–50 парақ: 500 ₸', '51–150 парақ: 800 ₸']);
});
