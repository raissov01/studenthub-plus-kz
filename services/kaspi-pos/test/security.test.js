import { describe, it, before, after } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'fs';
import os from 'os';
import path from 'path';

process.env.TOKEN_SECRET_KEY = 'a'.repeat(64);
process.env.MAX_PAYMENT_AMOUNT = '50000';
process.env.MAX_REFUND_AMOUNT = '20000';

const { parseAmount, maxPaymentAmount, maxRefundAmount, isAllowedWebhookUrl, writePrivateFile } = await import(
  '../src/security.js'
);
const { filterValidWebhooks } = await import('../src/webhookStore.js');
const { generateECDH, completeECDH, purgeStaleECDH, pendingECDHCount, ECDH_TTL_MS } = await import('../src/crypto.js');
const { stripQuery } = await import('../src/helpers.js');
const { createApp } = await import('../server.js');

describe('parseAmount', () => {
  it('accepts positive finite numbers within the limit', () => {
    assert.equal(parseAmount(6990, maxPaymentAmount()), 6990);
    assert.equal(parseAmount('6990', maxPaymentAmount()), 6990);
    assert.equal(parseAmount(50000, maxPaymentAmount()), 50000);
  });
  it('rejects zero, negatives, NaN, Infinity, junk and over-limit', () => {
    for (const v of [0, -1, '0', 'abc', NaN, Infinity, '1e999', null, undefined, '', true, {}, [], 50001]) {
      assert.equal(parseAmount(v, maxPaymentAmount()), null, `value ${String(v)}`);
    }
  });
  it('uses the separate refund limit', () => {
    assert.equal(parseAmount(20001, maxRefundAmount()), null);
    assert.equal(parseAmount(20000, maxRefundAmount()), 20000);
  });
});

describe('webhook policy', () => {
  it('allows https, localhost http and listed internal hosts only', () => {
    assert.ok(isAllowedWebhookUrl('https://example.com/hook'));
    assert.ok(isAllowedWebhookUrl('http://localhost:3001/hook'));
    assert.ok(isAllowedWebhookUrl('http://127.0.0.1/hook'));
    assert.ok(isAllowedWebhookUrl('http://api:3001/hook', ['api']));
    assert.ok(!isAllowedWebhookUrl('http://api:3001/hook'));
    assert.ok(!isAllowedWebhookUrl('http://evil.com/hook'));
    assert.ok(!isAllowedWebhookUrl('ftp://example.com'));
    assert.ok(!isAllowedWebhookUrl('not a url'));
  });
  it('drops hooks with empty secret or http URL', () => {
    const hooks = filterValidWebhooks(
      [
        { url: 'https://a.kz/h', events: ['payment.success'], secret: 's1' },
        { url: 'https://b.kz/h', events: ['payment.success'], secret: '' },
        { url: 'https://c.kz/h', events: ['payment.success'] },
        { url: 'http://d.kz/h', events: ['payment.success'], secret: 's2' },
        { url: 'http://api:3001/h', events: ['payment.success'], secret: 's3' },
      ],
      ['api'],
    );
    assert.deepEqual(
      hooks.map((h) => h.url),
      ['https://a.kz/h', 'http://api:3001/h'],
    );
  });
});

describe('ECDH per processId', () => {
  it('keeps concurrent logins apart', async () => {
    const crypto = await import('crypto');
    const server = crypto.generateKeyPairSync('ec', { namedCurve: 'prime256v1' });
    const serverPub = server.publicKey.export({ type: 'spki', format: 'der' }).toString('base64');

    const pubA = generateECDH('A');
    const pubB = generateECDH('B');
    const secretA = completeECDH('A', serverPub);
    const secretB = completeECDH('B', serverPub);

    const expect = (pub) =>
      crypto.diffieHellman({
        privateKey: server.privateKey,
        publicKey: crypto.createPublicKey({ key: Buffer.from(pub, 'base64'), format: 'der', type: 'spki' }),
      });
    assert.deepEqual(secretA, expect(pubA));
    assert.deepEqual(secretB, expect(pubB));
    assert.throws(() => completeECDH('A', serverPub), /No ECDH keypair/);
  });
  it('purges unfinished logins after 10 minutes', () => {
    generateECDH('stale');
    assert.equal(purgeStaleECDH(Date.now() + ECDH_TTL_MS + 1), 1);
    assert.equal(pendingECDHCount(), 0);
  });
});

describe('logging', () => {
  it('strips query strings from logged URLs', () => {
    assert.equal(stripQuery('https://x.kz/a?otp=123456&phone=7700'), 'https://x.kz/a');
  });
});

describe('private files', () => {
  it('writes files with mode 0600 even if they existed before', () => {
    const f = path.join(fs.mkdtempSync(path.join(os.tmpdir(), 'kpos-')), 'k.json');
    fs.writeFileSync(f, '{}', { mode: 0o644 });
    writePrivateFile(f, '{"a":1}');
    assert.equal(fs.statSync(f).mode & 0o777, 0o600);
  });
});

describe('HTTP API', () => {
  const KEY = 'k'.repeat(64);
  let server;
  let base;
  before(async () => {
    server = createApp({ apiKey: KEY }).listen(0, '127.0.0.1');
    await new Promise((r) => server.once('listening', r));
    base = `http://127.0.0.1:${server.address().port}`;
  });
  after(() => server.close());

  const post = (p, body, headers = {}) =>
    fetch(base + p, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...headers },
      body: JSON.stringify(body),
    });

  it('returns 401 for /api/* without or with a wrong key', async () => {
    assert.equal((await post('/api/qr/create', { amount: 100 })).status, 401);
    assert.equal((await post('/api/qr/create', { amount: 100 }, { 'X-Internal-Api-Key': 'wrong' })).status, 401);
    assert.equal((await fetch(base + '/api/session/check')).status, 401);
  });
  it('keeps /health open and the demo UI off by default', async () => {
    assert.equal((await fetch(base + '/health')).status, 200);
    assert.equal((await fetch(base + '/')).status, 404);
    assert.equal((await fetch(base + '/demo/qr.svg?text=x')).status, 404);
  });
  it('rejects invalid amounts with 400 before calling Kaspi', async () => {
    const { encryptSecret } = await import('../src/crypto.js');
    const h = {
      'X-Internal-Api-Key': KEY,
      'X-Token-SN': 'T',
      'X-Vtoken-Secret': encryptSecret(Buffer.from('s')),
    };
    for (const amount of [0, -5, 'abc', 50001, null]) {
      assert.equal((await post('/api/qr/create', { amount }, h)).status, 400, `qr ${amount}`);
      assert.equal((await post('/api/invoice/create', { phoneNumber: '7700', amount }, h)).status, 400);
    }
    assert.equal((await post('/api/refund/create', { qrOperationId: '1', returnAmount: 20001 }, h)).status, 400);
    assert.equal((await post('/api/refund/create', { qrOperationId: '1', returnAmount: -1 }, h)).status, 400);
  });
});
