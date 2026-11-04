import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  addToCart, cartTotal, defaultOptions, hoursLabel, lineKey, missingOptions, money, newArrivals, paymentLabel, pickChoice,
  preferLink, setQty, shouldPoll, stepIndex, tengeToTiyn, unitPrice, type Item, type Option,
} from './cafe';

const tt = (kk: string) => kk;
const size: Option = { id: 'size', nameKk: 'Көлемі', nameRu: 'Объём', required: true, multi: false, choices: [
  { id: 's', nameKk: 'S', nameRu: 'S', delta: 0 }, { id: 'l', nameKk: 'L', nameRu: 'L', delta: 20000 }] };
const syrup: Option = { id: 'syrup', nameKk: 'Сироп', nameRu: 'Сироп', required: false, multi: true, choices: [
  { id: 'van', nameKk: 'Ваниль', nameRu: 'Ваниль', delta: 15000 }, { id: 'car', nameKk: 'Карамель', nameRu: 'Карамель', delta: 15000 }] };
const latte: Item = { id: 1, cafeId: 1, categoryId: null, nameKk: 'Латте', nameRu: 'Латте', descKk: '', descRu: '', price: 100000, available: true, archived: false, options: [size, syrup], sort: 0 };
const cookie: Item = { ...latte, id: 2, nameKk: 'Печенье', nameRu: 'Печенье', price: 40000, options: [] };

test('money is whole tenge with a non-breaking group separator', () => {
  assert.equal(money(125000), '1 250 ₸');
  assert.equal(money(70000), '700 ₸');
  assert.equal(money(300000000), '3 000 000 ₸');
  assert.equal(tengeToTiyn('1 250'), 125000);
  for (const bad of ['', '0', '12.5', '-5', 'abc', '1234567']) assert.equal(tengeToTiyn(bad), null, bad);
});

test('options: defaults, required, single replaces, multi toggles, price adds deltas', () => {
  const d = defaultOptions(latte);
  assert.deepEqual(d, { size: ['s'] });
  assert.equal(missingOptions(latte, {}).length, 1);
  assert.equal(missingOptions(latte, d).length, 0);
  let o = pickChoice(d, size, 'l');
  assert.deepEqual(o.size, ['l']);
  o = pickChoice(o, syrup, 'van');
  o = pickChoice(o, syrup, 'car');
  assert.deepEqual(o.syrup, ['van', 'car']);
  assert.equal(unitPrice(latte, o), 100000 + 20000 + 30000);
  o = pickChoice(o, syrup, 'van');
  assert.deepEqual(o.syrup, ['car']);
  assert.deepEqual(pickChoice(o, size, 'l').size, ['l'], 'a required single group cannot be emptied');
});

test('cart merges equal lines, caps quantity, previews the total and reports gone items', () => {
  let cart = addToCart([], 1, { size: ['l'], syrup: ['van'] });
  cart = addToCart(cart, 1, { syrup: ['van'], size: ['l'] });
  assert.equal(cart.length, 1);
  assert.equal(cart[0].qty, 2);
  assert.equal(lineKey(1, { size: ['l'], syrup: [] }), lineKey(1, { size: ['l'] }));
  cart = addToCart(cart, 2, {}, 12);
  assert.equal(cart[1].qty, 10);
  assert.deepEqual(cartTotal(cart, [latte, cookie]), { total: 2 * 135000 + 10 * 40000, count: 12, gone: [] });
  const t = cartTotal(cart, [latte, { ...cookie, available: false }]);
  assert.equal(t.total, 270000);
  assert.equal(t.gone.length, 1);
  cart = setQty(cart, cart[1].key, 0);
  assert.equal(cart.length, 1);
});

test('labels and polling', () => {
  assert.equal(hoursLabel({ openDays: [1, 2, 3, 4, 5], openTime: '08:00', closeTime: '18:00' }, 'kk'), 'Дс–Жм 08:00–18:00');
  assert.equal(hoursLabel({ openDays: [1, 3], openTime: '08:00', closeTime: '18:00' }, 'ru'), 'Пн, Ср 08:00–18:00');
  assert.equal(hoursLabel({ openDays: [1, 2, 3, 4, 5, 6, 7], openTime: '09:00', closeTime: '20:00' }, 'ru'), 'Ежедневно 09:00–20:00');
  assert.equal(stepIndex('ready'), 2);
  assert.equal(stepIndex('cancelled'), -1);
  assert.equal(shouldPoll({ status: 'picked_up', paymentStatus: 'paid' }), false);
  assert.equal(shouldPoll({ status: 'cancelled', paymentStatus: 'pending' }), true);
  assert.equal(paymentLabel({ paymentMethod: 'pickup', paymentStatus: 'unpaid', refundManual: false }, tt), 'Кассада төлейсіз');
  assert.equal(paymentLabel({ paymentMethod: 'kaspi', paymentStatus: 'paid', refundManual: true }, tt), 'Ақшаны кофейня қайтарады');
  assert.equal(preferLink({ innerWidth: 390 }), true);
  assert.equal(preferLink({ innerWidth: 1280, matchMedia: () => ({ matches: false }) }), false);
  assert.deepEqual(newArrivals(null, [{ id: 1, status: 'new' }]), []);
  assert.deepEqual(newArrivals(new Set([1]), [{ id: 1, status: 'new' }, { id: 2, status: 'new' }, { id: 3, status: 'ready' }]), [2]);
});
