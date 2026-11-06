import { test } from 'node:test';
import assert from 'node:assert/strict';
import { isOlderThanController } from './pwa';

test('a stale page (older build than the running worker) does not register its worker', () => {
  assert.equal(isOlderThanController('100', 'https://x.test/sw.js?v=new&t=200'), true);
});
test('a newer or the same build registers', () => {
  assert.equal(isOlderThanController('300', 'https://x.test/sw.js?v=new&t=200'), false);
  assert.equal(isOlderThanController('200', 'https://x.test/sw.js?v=new&t=200'), false);
});
test('no worker yet, or a worker from before build times existed, never blocks', () => {
  assert.equal(isOlderThanController('100', undefined), false);
  assert.equal(isOlderThanController('100', 'https://x.test/sw.js?v=old'), false);
  assert.equal(isOlderThanController('0', 'https://x.test/sw.js?v=new&t=200'), false);
});
