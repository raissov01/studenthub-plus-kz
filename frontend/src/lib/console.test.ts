/* Admin console helpers. Run: npx tsx --test src/lib/console.test.ts */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { bars, panelCount, query, reasonOk, total, who, type Panels } from './console';

const none: Panels = {
  admin: false, moderator: false, blocked: false, moderation: null, cafes: [], printShops: [], clubs: [],
  university: null, feedback: [], employer: null, ambassador: false, mentor: false,
};

test('panelCount: nothing for a plain student, one per workspace otherwise', () => {
  assert.equal(panelCount(null), 0);
  assert.equal(panelCount(none), 0);
  assert.equal(panelCount({ ...none, moderator: true }), 1);
  // an admin's moderation lives inside the admin panel
  assert.equal(panelCount({ ...none, admin: true, moderator: true }), 1);
  const shop = { id: 1, nameKk: 'К', nameRu: 'К', role: 'barista', active: true, open: 0 };
  assert.equal(panelCount({ ...none, cafes: [shop], printShops: [shop, { ...shop, id: 2 }] }), 3);
  // university staff and feedback recipients share one panel
  assert.equal(panelCount({ ...none, university: { name: 'KATU', drafts: 0, published: 1 }, feedback: [{ id: 1, university: 'KATU', nameKk: 'Д', nameRu: 'Д', open: 0, overdue: 0 }] }), 1);
});

test('bars: relative heights, a visible minimum, all zero stays flat', () => {
  assert.deepEqual(bars([{ day: 'a', n: 0 }, { day: 'b', n: 1 }, { day: 'c', n: 100 }]).map(b => b.h), [0, 2, 100]);
  assert.deepEqual(bars([{ day: 'a', n: 0 }, { day: 'b', n: 0 }]).map(b => b.h), [0, 0]);
  assert.deepEqual(bars([]), []);
  assert.equal(total([{ n: 2 }, { n: 3 }]), 5);
});

test('query: drops empty values, stable order, booleans as 1', () => {
  assert.equal(query({}), '');
  assert.equal(query({ role: '', q: 'аружан', page: 2, blocked: true, scope: null, active: false }), '?blocked=1&page=2&q=%D0%B0%D1%80%D1%83%D0%B6%D0%B0%D0%BD');
});

test('who and reasonOk', () => {
  assert.equal(who({ id: 5, name: 'Аружан' }), 'Аружан');
  assert.equal(who({ id: 5, name: null, username: 'aru' }), '@aru');
  assert.equal(who({ userId: 7, name: null }), '#7');
  assert.equal(who(null), '—');
  assert.equal(reasonOk('  ок '), false);
  assert.equal(reasonOk('спам'), true);
  assert.equal(reasonOk('x'.repeat(501)), false);
});
