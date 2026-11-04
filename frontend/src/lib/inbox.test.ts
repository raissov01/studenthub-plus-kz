import assert from 'node:assert/strict';
import test from 'node:test';
import { badgeText, inboxAgo, inboxCategory, inboxLink, inboxPath } from './inbox';

const kk = (a: string) => a;
const ru = (_: string, b: string) => b;

test('only in-site paths are followed', () => {
  for (const ok of ['/event/12', '/mentors/me', '/jobs', '/']) assert.equal(inboxLink(ok), ok);
  for (const bad of [null, '', '//evil.example', '/\\evil.example', 'https://evil.example/x', 'javascript:alert(1)', 'event/12', '/a b', '/' + 'x'.repeat(300)]) {
    assert.equal(inboxLink(bad), null, String(bad));
  }
});

test('badge hides zero and caps at 9+', () => {
  assert.equal(badgeText(0), '');
  assert.equal(badgeText(-1), '');
  assert.equal(badgeText(3), '3');
  assert.equal(badgeText(9), '9');
  assert.equal(badgeText(10), '9+');
});

test('page path carries limit, before and the unread filter', () => {
  assert.equal(inboxPath(), '/api/me/inbox?limit=20');
  assert.equal(inboxPath({ before: 41, unread: true }), '/api/me/inbox?limit=20&before=41&unread=1');
  assert.equal(inboxPath({ before: null }), '/api/me/inbox?limit=20');
});

test('categories are labelled in both languages; unknown ones fall back', () => {
  assert.equal(inboxCategory('moderation', kk).label, 'Модерация');
  assert.equal(inboxCategory('events', ru).label, 'Мероприятие');
  assert.equal(inboxCategory('something-new', kk).label, 'Хабарлама');
  assert.equal(inboxCategory('something-new', ru).icon, 'bell');
});

test('relative time', () => {
  const now = Date.parse('2026-10-29T12:00:00Z');
  assert.equal(inboxAgo('2026-10-29T11:59:40Z', now, kk, 'kk'), 'Жаңа ғана');
  assert.equal(inboxAgo('2026-10-29T11:55:00Z', now, ru, 'ru'), '5 мин назад');
  assert.equal(inboxAgo('2026-10-29T10:00:00+00:00', now, kk, 'kk'), '2 сағ бұрын');
  assert.equal(inboxAgo('2026-10-26T12:00:00Z', now, kk, 'kk'), '3 күн бұрын');
  assert.equal(inboxAgo('not a date', now, kk, 'kk'), '');
});
