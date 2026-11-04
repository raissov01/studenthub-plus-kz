import assert from 'node:assert/strict';
import test from 'node:test';
import { safeBrowser, safeMessage, safeScreen, startClientErrorReporter } from './client-errors';

test('diagnostics cannot transmit free text, names, tokens, Unicode digits or object properties', () => {
  for (const secret of ['Alice Smith', 'a@example.com', '+7 777 123 45 67', '８７７７１２３４５６７', 'Bearer token', 'https://x/?secret=foo', '<script>alert(1)</script>']) {
    assert.equal(safeMessage(`Uncaught TypeError: ${secret}`), 'TypeError: [details redacted]');
    assert.equal(safeMessage(secret), 'Client error: [details redacted]');
    assert.equal(safeMessage(secret, true), 'UnhandledRejection: [details redacted]');
  }
  assert.equal(safeMessage({ get message() { throw new Error('Do not read me'); }, toString() { throw new Error('Do not stringify me'); } }, true), 'UnhandledRejection: [details redacted]');
  assert.equal(safeMessage('Failed to fetch'), 'Failed to fetch');
  assert.equal(safeMessage('Failed to fetch Alice'), 'Client error: [details redacted]');
});
test('screen excludes query, hash, identifiers and even slugs that look like route names', () => {
  for (const [path, expected] of [['/admin/health?token=foo', '/admin/health'], ['/p/Alice', '/p'], ['/p/profile', '/p'], ['/job/123#private', '/job'], ['#/market?email=alice', '/market'], ['/unknown/Alice', '/'], ['/admin/private', '/admin']]) assert.equal(safeScreen(path), expected);
});
test('only browser family and major version survive', () => {
  for (const [ua, expected] of [['Mozilla/5.0 Windows Chrome/131.1.2 Safari/537.36', 'Chrome/131'], ['Chrome/131.0 Edg/131.0', 'Edge/131'], ['Chrome/131.0 OPR/112.0', 'Opera/112'], ['Version/18.1 Safari/605.1', 'Safari/18'], ['FxiOS/129.4', 'Firefox/129'], ['Alice@example.com', 'Unknown']]) assert.equal(safeBrowser(ua), expected);
});
test('reporter is disabled outside production and safe in server rendering', () => {
  assert.doesNotThrow(() => startClientErrorReporter()());
});
