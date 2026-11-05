import type { Page } from '@playwright/test';
import { PDFDocument } from 'pdf-lib';
import { test, expect, onboard, fits, label } from './fixtures';
import { config } from './content';
import type { Lang } from '../src/lib/types';

/* «Полиграфия» with the API mocked: upload → the server's quote → pay at the counter → the job screen follows the
   operator to «Дайын»; a new price the student approves; the operator's board. Prices in tiyn. */

const shop = {
  id: 4, nameKk: 'Тест полиграфия', nameRu: 'Тест полиграфия', building: 'Бас ғимарат', placeKk: '1-қабат, 104', placeRu: '1 этаж, 104',
  openTime: '08:00', closeTime: '20:00', openDays: [1, 2, 3, 4, 5, 6, 7], slotMinutes: 15, leadMinutes: 20, minOrder: 0,
  accepts: ['pdf', 'docx', 'xlsx', 'pptx', 'jpg', 'png'], retentionDays: 3, active: true, paymentMode: 'both', noteKk: '', noteRu: '',
  openNow: true, payments: ['pickup', 'kaspi'],
};
const printSvc = { id: 21, shopId: 4, kind: 'print', nameKk: 'Басып шығару', nameRu: 'Печать', descKk: '', descRu: '', online: true, active: true, sort: 0,
  pricing: { sizes: { A4: { bw: 2000, color: 10000, bwDuplex: 1500, colorDuplex: 9000 } } } };
const binding = { ...printSvc, id: 22, kind: 'binding', nameKk: 'Түптеу', nameRu: 'Переплёт', pricing: { tiers: [{ maxSheets: 100, price: 50000 }] } };
const scan = { ...printSvc, id: 23, kind: 'unit', nameKk: 'Сканерлеу', nameRu: 'Сканирование', online: false, pricing: { price: 5000, unitKk: 'бет', unitRu: 'стр.' } };
const folder = { id: 31, shopId: 4, nameKk: 'Папка', nameRu: 'Папка', price: 30000, inStock: true, archived: false, sort: 0 };
const catalog = { shop, services: [printSvc, binding, scan], goods: [folder], slots: [{ at: '2026-11-05T09:00:00Z' }],
  limits: { maxFileBytes: 20971520, maxFiles: 5, dailyFiles: 30, dailyBytes: 209715200 } };
const upload = { id: 91, name: 'Курстық.pdf', kind: 'pdf', bytes: 2048, pages: 10, pagesHint: null, jobId: null, createdAt: '2026-11-05T06:00:00Z', deleted: false, url: '/api/print/files/91?v=77&e=1&s=x' };

function job(over: Record<string, unknown> = {}) {
  return {
    id: 701, shopId: 4, shopNameKk: shop.nameKk, shopNameRu: shop.nameRu, placeKk: shop.placeKk, placeRu: shop.placeRu,
    lines: [{ kind: 'file', fileId: 91, nameKk: 'Курстық.pdf', nameRu: 'Курстық.pdf', serviceId: 21, size: 'A4', duplex: true, copies: 2, filePages: 10, pages: 10, sheets: 5,
      parts: [{ nameKk: 'Басып шығару', nameRu: 'Печать', amount: 30000 }], total: 30000 }],
    total: 30000, priceState: 'final', proposedTotal: null, adjustReason: null, pickupAt: null, pickupCode: '482', note: '', status: 'new',
    paymentMethod: 'pickup', paymentStatus: 'unpaid', refundManual: false, pay: null, cancelReason: null, queuedAt: '2026-11-05T06:01:00Z',
    createdAt: '2026-11-05T06:01:00Z', printingAt: null, readyAt: null, closedAt: null, filesDeleted: false, files: [{ ...upload, jobId: 701 }], ahead: 2, ...over,
  };
}

async function signedIn(page: Page, lang: Lang, on = true) {
  await page.route('**/api/config', route => route.fulfill({ json: { ...config, features: { ...config.features, print: on } } }));
  await page.route('**/api/me', route => route.fulfill({ json: { user: { id: 77, name: 'Аружан Тест', role: 'student', lang, consentAt: '2026-01-01', isAdult: true }, needsConsent: false, inboxUnread: 0 } }));
}

test('print: everything is hidden while FEATURE_PRINT is off', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang, false);
  await onboard(page, lang);
  await page.getByRole('button', { name: t('Мәзір', 'Меню'), exact: true }).click();
  await expect(page.getByRole('dialog', { name: t('Мәзір', 'Меню') }).getByRole('button', { name: 'Полиграфия' })).toHaveCount(0);
  await page.goto('/print');
  await expect(page.getByText(t('Мұндай бет жоқ.', 'Такой страницы нет.'))).toBeVisible();
});

test('print: upload → the server prices it → pay at the counter → the job follows the operator to «Дайын»', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang);
  const quotes: Record<string, unknown>[] = [];
  const posted: Record<string, unknown>[] = [];
  const estimates: Record<string, unknown>[] = [];
  let uploads = 0;
  let reads = 0;
  await page.route('**/api/print/**', async route => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    if (path === '/api/print/shops') return route.fulfill({ json: { shops: [shop] } });
    if (path === '/api/print/shops/4') return route.fulfill({ json: catalog });
    if (path === '/api/print/jobs/mine') return route.fulfill({ json: { jobs: [] } });
    if (path === '/api/print/uploads' && req.method() === 'POST') {
      uploads++;
      expect(req.headers()['content-type']).toMatch(/^multipart\/form-data/);
      return route.fulfill({ status: 201, json: { upload } });
    }
    if (path === '/api/print/quote') {
      const b = req.postDataJSON();
      quotes.push(b);
      const f = b.files[0];
      const sides = 10 * f.copies * (f.duplex ? 1500 : f.color ? 10000 : 2000);
      const bind = f.bindingId ? 50000 * f.copies : 0;
      const goods = (b.goods as { qty: number }[]).reduce((s, g) => s + g.qty * 30000, 0);
      const lines = [{ kind: 'file', fileId: 91, nameKk: 'Курстық.pdf', nameRu: 'Курстық.pdf', size: 'A4', duplex: f.duplex, copies: f.copies, pages: 10, sheets: f.duplex ? 5 : 10,
        parts: [{ nameKk: 'Басып шығару', nameRu: 'Печать', amount: sides }, ...(bind ? [{ nameKk: 'Түптеу', nameRu: 'Переплёт', amount: bind }] : [])], total: sides + bind },
        ...(goods ? [{ kind: 'goods', refId: 31, nameKk: 'Папка', nameRu: 'Папка', qty: goods / 30000, unitPrice: 30000, total: goods }] : [])];
      return route.fulfill({ json: { quote: { lines, total: sides + bind + goods, review: false } } });
    }
    if (path === '/api/print/estimate') {
      const it = req.postDataJSON().items[0];
      estimates.push(it);
      return route.fulfill({ json: { quote: { lines: [], total: it.pages * it.copies * (it.duplex ? 1500 : 2000), review: false } } });
    }
    if (path === '/api/print/jobs' && req.method() === 'POST') { posted.push(req.postDataJSON()); return route.fulfill({ status: 201, json: { job: job({ total: 160000 }), duplicate: false } }); }
    if (path === '/api/print/jobs/701') {
      reads++;
      return route.fulfill({ json: { job: reads > 2 ? job({ total: 160000, status: 'ready', printingAt: '2026-11-05T06:05:00Z', readyAt: '2026-11-05T06:10:00Z' }) : job({ total: 160000, etaMin: 15 }) } });
    }
    return route.fulfill({ status: 501, json: { error: { code: 'unmocked_print' } } });
  });
  await onboard(page, lang);
  await page.getByRole('button', { name: t('Мәзір', 'Меню'), exact: true }).click();
  await page.getByRole('dialog', { name: t('Мәзір', 'Меню') }).getByRole('button', { name: 'Полиграфия' }).click();
  await expect(page).toHaveURL(/\/print$/);
  await page.getByRole('button', { name: /Тест полиграфия/ }).click();
  await expect(page).toHaveURL(/\/print\?id=4$/);
  // the price list shows every service, including the ones done only at the counter
  await expect(page.getByText(t('кассада', 'на кассе')).first()).toBeVisible();
  await fits(page);
  // the price before any upload: pages typed by hand, the server's sum
  await page.locator('#printCalc summary').click();
  await page.locator('#pcPages').fill('12');
  await page.locator('#pcCopies').fill('2');
  await expect(page.locator('#printCalcTotal')).toHaveText(/480\s₸/);
  expect(estimates.at(-1)).toEqual({ serviceId: 21, size: 'A4', color: false, duplex: false, copies: 2, pages: 12, bindingId: 0 });
  await fits(page);
  // a format the shop does not take is refused before uploading
  await page.locator('#printFile').setInputFiles({ name: 'virus.exe', mimeType: 'application/octet-stream', buffer: Buffer.from('MZ') });
  await expect(page.getByText(/virus\.exe/)).toBeVisible();
  expect(uploads).toBe(0);
  const doc = await PDFDocument.create();
  for (let i = 0; i < 10; i++) doc.addPage(i === 1 ? [842, 595] : [595, 842]);
  await page.locator('#printFile').setInputFiles({ name: 'Курстық.pdf', mimeType: 'application/pdf', buffer: Buffer.from(await doc.save()) });
  const card = page.getByRole('article', { name: 'Курстық.pdf' });
  await expect(card).toBeVisible();
  expect(uploads).toBe(1);
  // the student's own proof, drawn on this device: the first eight pages, the landscape one flagged
  await card.getByRole('button', { name: t('Беттерді көру', 'Посмотреть страницы') }).click();
  await expect(card.locator('.print-thumbs img')).toHaveCount(8);
  await expect(card.locator('.print-thumbs li.is-land')).toHaveCount(1);
  await expect(card.locator('.print-preview')).toContainText(t('Тағы 2 бет', 'Ещё страниц: 2'));
  await fits(page);
  // two copies, duplex, bound; a folder: the total is the server's
  await card.getByRole('checkbox', { name: t('Екі жақты', 'Двусторонняя') }).check();
  await card.getByRole('group', { name: t('Дана саны', 'Количество копий') }).getByRole('button', { name: t('Көбейту', 'Больше') }).click();
  await card.getByLabel(t('Түптеу', 'Переплёт')).selectOption('22');
  await page.getByRole('group', { name: 'Папка' }).getByRole('button', { name: t('Көбейту', 'Больше') }).click();
  await expect(page.locator('#printTotal')).toHaveText(/1\s600\s₸/);
  await fits(page);
  const last = quotes.at(-1) as { files: Record<string, unknown>[]; goods: unknown[]; total?: unknown };
  expect(last.total).toBeUndefined();
  expect(last.files).toEqual([{ fileId: 91, serviceId: 21, size: 'A4', color: false, duplex: true, copies: 2, range: '', bindingId: 22, laminationId: 0 }]);
  expect(last.goods).toEqual([{ id: 31, qty: 1 }]);
  await page.getByRole('radio', { name: new RegExp(t('Алған кезде кассада', 'На кассе при получении')) }).check();
  await page.locator('#printSubmit').click();
  await expect(page).toHaveURL(/\/print\/jobs\?id=701$/);
  expect(posted).toHaveLength(1);
  const body = posted[0] as { payment: string; pickupAt: unknown; key: string; total?: unknown };
  expect(body.payment).toBe('pickup');
  expect(body.pickupAt).toBeNull();
  expect(body.total).toBeUndefined();
  expect(body.key).toMatch(/^[a-f0-9]{24}$/);
  // the code and the queue, then the operator marks it ready and the screen follows by itself
  await expect(page.locator('#printCode')).toHaveText('482');
  await expect(page.locator('#printAhead')).toContainText('2');
  await expect(page.locator('#printEta')).toContainText('15');
  // the options are remembered for the next order at this shop (never the file or its pages)
  expect(JSON.parse(await page.evaluate(() => localStorage.getItem('print:preset:4') ?? 'null'))).toEqual(
    { serviceId: 21, size: 'A4', color: false, duplex: true, copies: 2, bindingId: 22, laminationId: 0 });
  await expect(page.getByText(t('Кассада төлейсіз', 'Оплата на кассе'))).toBeVisible();
  await expect(page.getByRole('status').getByText(t('Дайын', 'Готово'), { exact: true })).toBeVisible({ timeout: 15_000 });
  await fits(page);
});

test('print: the operator\'s new price waits for the student, then Kaspi in the shop\'s name', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang);
  let state: 'proposed' | 'final' = 'proposed';
  const answers: unknown[] = [];
  let pays = 0;
  const kaspi = { paymentMethod: 'kaspi', lines: [{ ...job().lines[0], pagesDeclared: true }] };
  await page.route('**/api/print/**', async route => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    if (path === '/api/print/jobs/701/price') {
      answers.push(req.postDataJSON());
      state = 'final';
      return route.fulfill({ json: { job: job({ ...kaspi, total: 42000 }) } });
    }
    if (path === '/api/print/jobs/701/pay') {
      pays++;
      return route.fulfill({ json: { job: job({ ...kaspi, total: 42000, paymentStatus: 'pending', pay: { url: 'https://pay.kaspi.kz/pay/e2e701', qr: 'https://qr.kaspi.kz/e2e701', expiresAt: '2026-11-05T07:03:00Z' } }) } });
    }
    if (path === '/api/print/jobs/701') {
      return route.fulfill({ json: { job: state === 'proposed'
        ? job({ ...kaspi, priceState: 'proposed', proposedTotal: 42000, adjustReason: t('Файлда 14 бет', 'В файле 14 страниц') })
        : job({ ...kaspi, total: 42000, paymentStatus: 'pending', pay: { url: 'https://pay.kaspi.kz/pay/e2e701', qr: 'https://qr.kaspi.kz/e2e701', expiresAt: '2026-11-05T07:03:00Z' } }) } });
    }
    return route.fulfill({ status: 501, json: { error: { code: 'unmocked_print' } } });
  });
  await onboard(page, lang);
  await page.goto('/print/jobs?id=701');
  const proposal = page.locator('#printProposal');
  await expect(proposal).toContainText(/420\s₸/);
  await expect(proposal).toContainText(t('Файлда 14 бет', 'В файле 14 страниц'));
  // no payment while the price is open
  await expect(page.locator('#kaspiLink')).toHaveCount(0);
  expect(pays).toBe(0);
  await fits(page);
  await proposal.getByRole('button', { name: t('Жаңа бағамен келісемін', 'Согласен с новой ценой') }).click();
  expect(answers).toEqual([{ accept: true }]);
  await expect(page.locator('#kaspiLink')).toHaveAttribute('href', 'https://pay.kaspi.kz/pay/e2e701');
  expect(pays).toBe(1);
  await expect(page.locator('#printProposal')).toHaveCount(0);
});

test('print: the operator board confirms or changes a price, prints and hands over', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang);
  const actions: string[] = [];
  const prices: unknown[] = [];
  const handovers: unknown[] = [];
  let cur = job({ priceState: 'review', customer: 'Аружан Т.', lines: [{ ...job().lines[0], pagesDeclared: true }] });
  await page.route('**/api/print/**', async route => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    if (path === '/api/print/staff/jobs') return route.fulfill({ json: { shopId: 4, shops: [{ ...shop, myRole: 'operator' }], jobs: [cur] } });
    if (path === '/api/print/staff/jobs/701/price') {
      prices.push(req.postDataJSON());
      cur = { ...cur, priceState: 'final' };
      return route.fulfill({ json: { job: cur } });
    }
    const m = /^\/api\/print\/staff\/jobs\/701\/(printing|ready|picked-up|no-show|decline)$/.exec(path);
    if (m && req.method() === 'POST') {
      actions.push(m[1]);
      if (m[1] === 'picked-up') handovers.push(req.postDataJSON());
      cur = { ...cur, status: m[1] === 'printing' ? 'printing' : m[1] === 'ready' ? 'ready' : 'picked_up' };
      return route.fulfill({ json: { job: cur } });
    }
    if (path === '/api/print/staff/catalog') return route.fulfill({ json: { shopId: 4, role: 'operator', services: [printSvc], goods: [folder] } });
    return route.fulfill({ status: 501, json: { error: { code: 'unmocked_print' } } });
  });
  await onboard(page, lang);
  await page.goto('/print/staff');
  const ticket = page.locator('.print-ticket');
  await expect(ticket.getByText('№482')).toBeVisible();
  await expect(ticket).toContainText('Аружан Т.');
  // the file opens through its signed link; a declared page count must be checked first
  await expect(ticket.getByRole('link', { name: /Курстық\.pdf/ })).toHaveAttribute('href', /^\/api\/print\/files\/91\?/);
  await expect(ticket.getByRole('button', { name: t('Басып жатырмын', 'Печатаю') })).toHaveCount(0);
  await fits(page);
  // a different price needs a reason (the sheet), the same one confirms
  await ticket.getByRole('button', { name: t('Бағаны өзгерту', 'Изменить цену') }).click();
  const sheet = page.getByRole('dialog');
  await sheet.locator('#ppTotal').fill('420');
  await sheet.locator('#ppReason').fill(t('Файлда 14 бет', 'В файле 14 страниц'));
  await sheet.getByRole('button', { name: t('Жіберу', 'Отправить') }).click();
  await expect(page.getByRole('dialog')).toBeHidden();
  expect(prices).toEqual([{ total: 42000, reason: t('Файлда 14 бет', 'В файле 14 страниц') }]);
  await page.locator('.print-ticket').getByRole('button', { name: t('Басып жатырмын', 'Печатаю') }).click();
  await page.getByRole('tab', { name: new RegExp(t('Басып жатыр', 'Печатаются')) }).click();
  await page.locator('.print-ticket').getByRole('button', { name: t('Дайын', 'Готово'), exact: true }).click();
  await page.getByRole('tab', { name: new RegExp(t('Дайын', 'Готовы') + ' ') }).click();
  // the handover needs the code the student says
  await page.locator('.print-ticket').getByRole('button', { name: t('Берілді', 'Выдан') }).click();
  const hand = page.getByRole('dialog');
  await expect(hand).toContainText('Аружан Т.');
  await hand.locator('#hsCode').fill('№482');
  await hand.getByRole('button', { name: t('Берілді', 'Выдан') }).click();
  await expect(page.getByRole('dialog')).toBeHidden();
  expect(actions).toEqual(['printing', 'ready', 'picked-up']);
  expect(handovers).toEqual([{ code: '482' }]);
  // the price list at the counter
  await page.getByRole('tab', { name: t('Прайс', 'Прайс') }).click();
  await expect(page.locator('.cafe-switch input').first()).toBeChecked();
  await fits(page);
});

test('print: the counter finds a stack by code or QR, prints a batch in queue order, and the student shows a QR', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang);
  const token = 'PJ-702-4821-0123456789abcdef0123';
  const a = job({ id: 701, pickupCode: '1207', customer: 'Аружан Т.', queuedAt: '2026-11-05T06:01:00Z' });
  const b = job({ id: 702, pickupCode: '4821', customer: 'Нұрлан Қ.', queuedAt: '2026-11-05T06:00:00Z', status: 'ready', readyAt: '2026-11-05T06:10:00Z' });
  const c = job({ id: 703, pickupCode: '0450', customer: 'Әлия С.', queuedAt: '2026-11-05T06:02:00Z' });
  const d = job({ id: 704, pickupCode: '0999', customer: 'Ерлан Б.', queuedAt: '2026-11-05T05:59:00Z', status: 'ready' });
  const e = job({ id: 705, pickupCode: '0200', customer: 'Дана М.', queuedAt: '2026-11-05T06:03:00Z', status: 'ready' });
  const lookups: string[] = [];
  const handovers: unknown[] = [];
  const bulks: unknown[] = [];
  let picked = false;
  await page.route('**/api/print/**', async route => {
    const req = route.request();
    const url = new URL(req.url());
    const path = url.pathname;
    if (path === '/api/print/staff/jobs') {
      return route.fulfill({ json: { shopId: 4, shops: [{ ...shop, myRole: 'operator', separator: 'slip' }], jobs: [d, b, a, c, e].filter(j => !(picked && j.id === 702)),
        counts: { new: 2, ready: picked ? 2 : 3 } } });
    }
    if (path === '/api/print/staff/lookup') {
      const q = url.searchParams.get('q') ?? '';
      lookups.push(q);
      return route.fulfill({ json: { jobs: q === '4821' || q === token ? [b] : [], valid: true, byToken: q === token } });
    }
    if (path === '/api/print/staff/jobs/702/picked-up') {
      handovers.push(req.postDataJSON());
      picked = true;
      return route.fulfill({ json: { job: { ...b, status: 'picked_up' } } });
    }
    if (path === '/api/print/staff/jobs/bulk') {
      bulks.push(req.postDataJSON());
      return route.fulfill({ json: { results: [{ id: 701, ok: true }, { id: 703, ok: true }] } });
    }
    if (path === '/api/print/jobs/702') return route.fulfill({ json: { job: { ...b, customer: undefined, pickupToken: token } } });
    if (path === '/api/print/staff/report') {
      return route.fulfill({ json: { report: { day: '2026-11-05', received: 41, pickedUp: 37, noShow: 1, cancelled: 2, open: 3, revenueKaspi: 1250000, revenueCounter: 830000,
        refunded: 0, refundsManual: 0, pagesBw: 1630, pagesColor: 42, sheets: 1100, waitMedianMin: 18 } } });
    }
    if (path === '/api/print/files/91') {
      const d = await PDFDocument.create();
      for (let i = 0; i < 10; i++) d.addPage([595, 842]);
      return route.fulfill({ body: Buffer.from(await d.save()), contentType: 'application/pdf' });
    }
    return route.fulfill({ status: 501, json: { error: { code: 'unmocked_print' } } });
  });
  await onboard(page, lang);
  await page.goto('/print/staff');
  // the zero-paper tip, once
  await expect(page.getByText(t('Стектерді қағазсыз бөлу', 'Разделять стопки без бумаги'))).toBeVisible();
  await page.getByRole('button', { name: t('Түсінікті', 'Понятно') }).click();
  await expect(page.getByText(t('Стектерді қағазсыз бөлу', 'Разделять стопки без бумаги'))).toHaveCount(0);
  // a code nobody has, then the right one: the name and the pages to check against the stack
  const search = page.locator('#pcSearch');
  await search.fill('7777');
  await search.press('Enter');
  await expect(page.getByText(t('№7777 табылмады. Кодты қайта сұраңыз.', '№7777 не найден. Спросите код ещё раз.'))).toBeVisible();
  await search.fill('№4821');
  await search.press('Enter');
  const found = page.locator('.print-handover');
  await expect(found).toContainText('Нұрлан Қ.');
  await expect(found).toContainText(t('20 бет', '20 стр.'));
  await expect(found).toContainText('Курстық.pdf');
  await fits(page);
  await found.getByRole('button', { name: t('Төледі — берілді', 'Оплатил — выдан') }).click();
  await expect(page.locator('.print-handover')).toHaveCount(0);
  expect(handovers).toEqual([{ code: '4821' }]);
  // a USB scanner types the QR into the same box
  await search.fill(token);
  await search.press('Enter');
  await expect(page.locator('.print-handover')).toContainText('Нұрлан Қ.');
  expect(lookups).toEqual(['7777', '4821', token]);
  // the ready pile sorts by code on demand
  await page.getByRole('tab', { name: new RegExp(t('Дайын', 'Готовы') + ' ') }).click();
  const readyCol = page.locator('.cafe-col').nth(2);
  await expect(readyCol.locator('.cafe-ticket-code')).toHaveText(['№0999', '№0200']);
  await readyCol.getByRole('button', { name: t('Кезек бойынша', 'По очереди') }).click();
  await expect(readyCol.locator('.cafe-ticket-code')).toHaveText(['№0200', '№0999']);
  // a batch ticked out of order prints in queue order; slips come four to a sheet; one bulk move
  await page.getByRole('tab', { name: new RegExp(t('Жаңа', 'Новые')) }).click();
  await page.getByRole('checkbox', { name: t('№0450: басуға таңдау', '№0450: выбрать для печати') }).check();
  await page.getByRole('checkbox', { name: t('№1207: басуға таңдау', '№1207: выбрать для печати') }).check();
  await page.locator('#printPlanBtn').click();
  const plan = page.getByRole('dialog');
  await expect(plan.locator('.print-plan-item b.num')).toHaveText(['№1207', '№0450']);
  await expect(plan.getByRole('button', { name: new RegExp(t('Кесетін парақшалар', 'Вкладыши для разрезания')) })).toContainText('1');
  // slips for the batch and one job's print-ready PDF are built in this browser (10 pages, duplex, 2 copies = 20)
  await plan.getByRole('button', { name: new RegExp(t('Кесетін парақшалар', 'Вкладыши для разрезания')) }).click();
  await expect(plan.locator('#printSlipsPdf')).toHaveAttribute('href', /^blob:/);
  await expect(plan.locator('#printSlipsPdf')).toContainText('1');
  await plan.getByRole('button', { name: /PDF · A4/ }).first().click();
  await expect(plan.getByRole('link', { name: new RegExp(t('PDF жүктеу · 20 бет', 'Скачать PDF · 20 стр.')) })).toHaveAttribute('href', /^blob:/);
  await fits(page);
  await plan.getByRole('button', { name: t('Барлығы: басып жатырмын', 'Все: печатаю') }).click();
  await expect(page.getByRole('dialog')).toBeHidden();
  expect(bulks).toEqual([{ action: 'printing', ids: [701, 703] }]);
  // the day in numbers
  await page.getByRole('tab', { name: t('Есеп', 'Отчёт') }).click();
  await expect(page.locator('.print-report')).toContainText(/12\s500\s₸/);
  await expect(page.locator('.print-report')).toContainText('1630');
  await fits(page);
  // the student's screen carries the QR the operator scans
  await page.goto('/print/jobs?id=702');
  await expect(page.locator('#printQr svg')).toBeVisible();
  await expect(page.locator('#printCode')).toHaveText('4821');
});
