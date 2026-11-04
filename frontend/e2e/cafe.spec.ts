import type { Page } from '@playwright/test';
import { test, expect, onboard, fits, label } from './fixtures';
import { config } from './content';
import type { Lang } from '../src/lib/types';

/* «Кофе / Кофейня» with the API mocked: the payment provider never runs here, the order replies stand in for it
   (pickup and Kaspi paths), and the counter's board. Prices in tiyn. */

const cafe = {
  id: 3, nameKk: 'Тест кофейня', nameRu: 'Тест кофейня', building: 'Бас ғимарат', placeKk: '1-қабат', placeRu: '1 этаж',
  openTime: '08:00', closeTime: '20:00', openDays: [1, 2, 3, 4, 5, 6, 7], slotMinutes: 10, slotCapacity: 6, leadMinutes: 10,
  active: true, paymentMode: 'both', isDemo: false, openNow: true, payments: ['pickup', 'kaspi'],
};
const latte = {
  id: 11, cafeId: 3, categoryId: 1, nameKk: 'Латте', nameRu: 'Латте', descKk: 'Сүтті', descRu: 'С молоком', price: 100000, available: true, archived: false, sort: 1,
  options: [{ id: 'size', nameKk: 'Көлемі', nameRu: 'Объём', required: true, multi: false, choices: [
    { id: 's', nameKk: '250 мл', nameRu: '250 мл', delta: 0 }, { id: 'l', nameKk: '450 мл', nameRu: '450 мл', delta: 40000 }] }],
};
const croissant = { id: 12, cafeId: 3, categoryId: 2, nameKk: 'Круассан', nameRu: 'Круассан', descKk: '', descRu: '', price: 60000, available: true, archived: false, sort: 1, options: [] };
const menu = {
  cafe, categories: [{ id: 1, nameKk: 'Кофе', nameRu: 'Кофе', sort: 1, active: true }, { id: 2, nameKk: 'Тағамдар', nameRu: 'Еда', sort: 2, active: true }],
  items: [latte, croissant],
  slots: [{ at: '2026-11-05T07:20:00Z', full: false }, { at: '2026-11-05T07:30:00Z', full: true }, { at: '2026-11-05T07:40:00Z', full: false }],
};

function order(over: Record<string, unknown> = {}) {
  return {
    id: 501, cafeId: 3, cafeNameKk: cafe.nameKk, cafeNameRu: cafe.nameRu, placeKk: '1-қабат', placeRu: '1 этаж', total: 200000,
    items: [
      { itemId: 11, nameKk: 'Латте', nameRu: 'Латте', qty: 1, unitPrice: 140000, total: 140000, options: [{ group: 'size', choice: 'l', nameKk: 'Көлемі: 450 мл', nameRu: 'Объём: 450 мл', delta: 40000 }] },
      { itemId: 12, nameKk: 'Круассан', nameRu: 'Круассан', qty: 1, unitPrice: 60000, total: 60000, options: [] },
    ],
    pickupAt: '2026-11-05T07:20:00Z', pickupCode: '347', note: '', status: 'new', paymentMethod: 'pickup', paymentStatus: 'unpaid',
    refundManual: false, pay: null, cancelReason: null, createdAt: '2026-11-05T06:50:00Z', acceptedAt: null, readyAt: null, closedAt: null, ...over,
  };
}

async function signedIn(page: Page, lang: Lang, cafeOn = true) {
  await page.route('**/api/config', route => route.fulfill({ json: { ...config, features: { ...config.features, cafe: cafeOn } } }));
  await page.route('**/api/me', route => route.fulfill({ json: { user: { id: 77, name: 'Аружан Тест', role: 'student', lang, consentAt: '2026-01-01', isAdult: true }, needsConsent: false, inboxUnread: 0 } }));
}

test('cafe: everything is hidden while FEATURE_CAFE is off', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang, false);
  await onboard(page, lang);
  await expect(page.locator('#cafeShortcut')).toHaveCount(0);
  await page.getByRole('button', { name: t('Мәзір', 'Меню'), exact: true }).click();
  await expect(page.getByRole('dialog', { name: t('Мәзір', 'Меню') }).getByRole('button', { name: 'Кофе / Кофейня' })).toHaveCount(0);
  await page.goto('/cafe');
  await expect(page.getByText(t('Мұндай бет жоқ.', 'Такой страницы нет.'))).toBeVisible();
});

test('cafe: menu with options → cart → slot → pay at pickup → status with a big code', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang);
  const posted: Record<string, unknown>[] = [];
  let reads = 0;
  await page.route('**/api/cafes', route => route.fulfill({ json: { cafes: [cafe] } }));
  await page.route('**/api/cafes/3/menu', route => route.fulfill({ json: menu }));
  await page.route('**/api/cafe/**', async route => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    if (path === '/api/cafe/orders/mine') return route.fulfill({ json: { orders: posted.length ? [order()] : [] } });
    if (req.method() === 'POST' && path === '/api/cafe/orders') { posted.push(req.postDataJSON()); return route.fulfill({ status: 201, json: { order: order(), duplicate: false } }); }
    if (path === '/api/cafe/orders/501') { reads++; return route.fulfill({ json: { order: reads > 1 ? order({ status: 'ready', acceptedAt: '2026-11-05T06:55:00Z', readyAt: '2026-11-05T07:10:00Z' }) : order() } }); }
    return route.fulfill({ status: 501, json: { error: { code: 'unmocked_cafe' } } });
  });
  await onboard(page, lang);
  // the compact home shortcut and the menu entry
  await expect(page.locator('#cafeShortcut')).toBeVisible();
  expect((await page.locator('#cafeShortcut').boundingBox())!.height).toBeLessThanOrEqual(80);
  await page.getByRole('button', { name: t('Мәзір', 'Меню'), exact: true }).click();
  await page.getByRole('dialog', { name: t('Мәзір', 'Меню') }).getByRole('button', { name: 'Кофе / Кофейня' }).click();
  await expect(page).toHaveURL(/\/cafe$/);
  await page.getByRole('button', { name: /Тест кофейня/ }).click();
  await expect(page).toHaveURL(/\/cafe\?id=3$/);
  await fits(page);
  // a drink with a required size: the sheet, the large size, two of them
  await page.getByRole('button', { name: t('Латте: себетке қосу', 'Латте: в корзину') }).click();
  const sheet = page.getByRole('dialog');
  await sheet.getByRole('button', { name: /450 мл/ }).click();
  await expect(sheet.getByRole('button', { name: /450 мл/ })).toHaveAttribute('aria-pressed', 'true');
  await sheet.getByRole('button', { name: t('Себетке қосу', 'В корзину') }).click();
  await page.getByRole('button', { name: t('Круассан: себетке қосу', 'Круассан: в корзину') }).click();
  const cart = page.locator('#cafeCart');
  await expect(cart).toContainText(/2\s000/);
  await fits(page);
  await cart.click();
  const checkout = page.getByRole('dialog');
  // a full slot cannot be chosen; pay at the counter
  await expect(checkout.locator('#cafeSlot option[disabled]')).toHaveCount(1);
  await checkout.locator('#cafeSlot').selectOption('2026-11-05T07:40:00Z');
  await checkout.getByRole('radio', { name: new RegExp(t('Алған кезде кассада', 'На кассе при получении')) }).check();
  await checkout.locator('#cafeNote').fill(t('Қантсыз', 'Без сахара'));
  await checkout.locator('#cafeSubmit').click();
  await expect(page).toHaveURL(/\/cafe\/orders\?id=501$/);
  // the request names items and options only: the server prices them
  expect(posted).toHaveLength(1);
  const body = posted[0] as { items: Record<string, unknown>[]; payment: string; pickupAt: string; key: string; total?: unknown };
  expect(body.total).toBeUndefined();
  expect(body.items).toEqual([{ itemId: 11, qty: 1, options: { size: ['l'] } }, { itemId: 12, qty: 1, options: {} }]);
  expect(body.payment).toBe('pickup');
  expect(body.pickupAt).toBe('2026-11-05T07:40:00Z');
  expect(body.key).toMatch(/^[a-f0-9]{24}$/);
  // the big code, then the board marks it ready and the screen follows by itself
  await expect(page.locator('#cafeCode')).toHaveText('347');
  await expect(page.getByText(t('Кассада төлейсіз', 'Оплата на кассе'))).toBeVisible();
  await expect(page.getByRole('status').getByText(t('Дайын — алып кетіңіз', 'Готов — заберите'))).toBeVisible({ timeout: 15_000 });
  await fits(page);
});

test('cafe: Kaspi path opens the café\'s own Kaspi QR and follows the payment', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang);
  let paid = false;
  let pays = 0;
  const kaspi = order({ paymentMethod: 'kaspi' });
  await page.route('**/api/cafe/**', async route => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    if (path === '/api/cafe/orders/mine') return route.fulfill({ json: { orders: [] } });
    if (path === '/api/cafe/orders/501/pay') {
      pays++;
      return route.fulfill({ json: { order: { ...kaspi, paymentStatus: 'pending', pay: { url: 'https://pay.kaspi.kz/pay/e2e501', qr: 'https://qr.kaspi.kz/e2e501', expiresAt: '2026-11-05T07:03:00Z' } } } });
    }
    if (path === '/api/cafe/orders/501') {
      return route.fulfill({ json: { order: paid ? { ...kaspi, paymentStatus: 'paid', status: 'accepted' }
        : pays ? { ...kaspi, paymentStatus: 'pending', pay: { url: 'https://pay.kaspi.kz/pay/e2e501', qr: 'https://qr.kaspi.kz/e2e501', expiresAt: '2026-11-05T07:03:00Z' } } : kaspi } });
    }
    return route.fulfill({ status: 501, json: { error: { code: 'unmocked_cafe' } } });
  });
  await onboard(page, lang);
  await page.goto('/cafe/orders?id=501');
  // the QR is asked for at once, exactly once
  const link = page.locator('#kaspiLink');
  await expect(link).toHaveAttribute('href', 'https://pay.kaspi.kz/pay/e2e501');
  expect(pays).toBe(1);
  await expect(page.getByText(t('Kaspi: төлем күтілуде', 'Kaspi: ждём оплату'))).toBeVisible();
  await fits(page);
  // Kaspi confirms (webhook/poller on the server): the screen moves on without a reload
  paid = true;
  await expect(page.getByText(t('Kaspi арқылы төленді', 'Оплачено через Kaspi'))).toBeVisible({ timeout: 15_000 });
  await expect(link).toHaveCount(0);
  await expect(page.getByRole('status').getByText(t('Дайындалуда', 'Готовится'))).toBeVisible();
  expect(pays).toBe(1);
});

test('cafe: the counter board accepts, marks ready and switches an item off', async ({ page, lang }) => {
  const t = (kk: string, ru: string) => label(lang, kk, ru);
  await signedIn(page, lang);
  const actions: string[] = [];
  let status = 'new';
  let available = true;
  await page.route('**/api/cafe/**', async route => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    if (path === '/api/cafe/orders/mine') return route.fulfill({ json: { orders: [] } });
    if (path === '/api/cafe/staff/orders') {
      return route.fulfill({ json: { cafeId: 3, cafes: [{ ...cafe, myRole: 'barista' }], orders: [order({ status, customer: 'Аружан Т.', note: 'Қантсыз' })] } });
    }
    const m = /^\/api\/cafe\/staff\/orders\/501\/(accept|ready|picked-up|no-show|decline)$/.exec(path);
    if (m && req.method() === 'POST') {
      actions.push(m[1]);
      status = m[1] === 'accept' ? 'accepted' : m[1] === 'ready' ? 'ready' : 'picked_up';
      return route.fulfill({ json: { order: order({ status }) } });
    }
    if (path === '/api/cafe/staff/menu') return route.fulfill({ json: { cafeId: 3, role: 'barista', categories: menu.categories, items: [{ ...croissant, available }] } });
    if (path === '/api/cafe/staff/menu/items/12/availability') {
      available = req.postDataJSON().available;
      return route.fulfill({ json: { item: { ...croissant, available } } });
    }
    return route.fulfill({ status: 501, json: { error: { code: 'unmocked_cafe' } } });
  });
  await onboard(page, lang);
  await page.goto('/cafe/staff');
  const ticket = page.locator('.cafe-ticket');
  await expect(ticket.getByText('№347')).toBeVisible();
  await expect(ticket).toContainText('Аружан Т.');
  await fits(page);
  await ticket.getByRole('button', { name: t('Қабылдау', 'Принять') }).click();
  // narrow screens show one column at a time
  await page.getByRole('tab', { name: new RegExp(t('Дайындалуда', 'Готовятся')) }).click();
  await page.locator('.cafe-ticket').getByRole('button', { name: t('Дайын', 'Готово'), exact: true }).click();
  await page.getByRole('tab', { name: new RegExp(t('Дайын', 'Готовы') + ' ') }).click();
  await page.locator('.cafe-ticket').getByRole('button', { name: t('Берілді', 'Выдан') }).click();
  expect(actions).toEqual(['accept', 'ready', 'picked-up']);
  // availability at the counter
  await page.getByRole('tab', { name: t('Мәзір', 'Меню') }).click();
  const sw = page.locator('.cafe-switch input').first();
  await expect(sw).toBeChecked();
  await sw.uncheck();
  await expect(sw).not.toBeChecked();
  expect(available).toBe(false);
  await fits(page);
});
