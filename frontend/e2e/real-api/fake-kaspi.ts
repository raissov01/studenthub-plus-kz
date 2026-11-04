// A stand-in for services/kaspi-pos in the real-API suite: the same HTTP contract (X-Internal-Api-Key, session
// headers, Kaspi-shaped replies), no Kaspi behind it. POST /__pay/<operation> marks a QR as paid, like a student
// paying in the Kaspi app. Never part of a deployment.
import { createServer } from 'node:http';
import { kaspiApiKey, kaspiPort } from './settings';

const paid = new Set<string>();
let seq = 0;
const json = (res: import('node:http').ServerResponse, status: number, body: unknown) => {
  res.writeHead(status, { 'content-type': 'application/json' });
  res.end(JSON.stringify(body));
};
createServer((req, res) => {
  let raw = '';
  req.on('data', c => { raw += c; });
  req.on('end', () => {
    const url = new URL(req.url || '/', 'http://x');
    if (req.method === 'GET' && url.pathname === '/health') return json(res, 200, { status: 'ok' });
    const pay = /^\/__pay\/(\d+)$/.exec(url.pathname);
    if (req.method === 'POST' && pay) { paid.add(pay[1]); return json(res, 200, { ok: true }); }
    if (req.headers['x-internal-api-key'] !== kaspiApiKey) return json(res, 401, { error: 'unauthorized' });
    const body = raw ? JSON.parse(raw) : {};
    const session = req.headers['x-token-sn'] === 'E2E-TSN' && req.headers['x-vtoken-secret'] === 'E2E-VT';
    switch (`${req.method} ${url.pathname}`) {
      case 'POST /api/auth/init': return json(res, 200, { success: true, processId: 'e2e-process' });
      case 'POST /api/auth/send-phone': return json(res, 200, { success: /^7\d{9}$/.test(body.phoneNumber) });
      case 'POST /api/auth/verify-otp':
        return json(res, 200, body.otp === '1234'
          ? { success: true, tokenSN: 'E2E-TSN', vtokenSecret: 'E2E-VT', profileId: 4242, phone: '7011234567', orgName: 'E2E Coffee LLP', organizationId: 1 }
          : { success: false });
      case 'GET /api/session/check': return json(res, session ? 200 : 401, { active: session });
      case 'POST /api/qr/create': {
        if (!session) return json(res, 401, { error: 'Invalid or expired vtokenSecret. Re-authenticate.' });
        const op = String(880000000 + ++seq);
        return json(res, 200, { StatusCode: 0, Data: { QrOperationId: Number(op), QrToken: `https://pay.kaspi.kz/pay/${op}`, QrOriginalToken: `https://qr.kaspi.kz/${op}`,
          ExpireDate: new Date(Date.now() + 3 * 60_000).toISOString(), Amount: 0 } });
      }
      case 'GET /api/qr/status': {
        if (!session) return json(res, 401, { error: 'Invalid or expired vtokenSecret. Re-authenticate.' });
        const op = url.searchParams.get('qrOperationId') || '';
        return json(res, 200, { StatusCode: 0, Data: { Status: paid.has(op) ? 'Processed' : 'Wait' } });
      }
      case 'POST /api/refund/create': return json(res, 200, { StatusCode: 0, Data: {} });
    }
    return json(res, 404, { error: 'not found' });
  });
}).listen(kaspiPort, '127.0.0.1');
