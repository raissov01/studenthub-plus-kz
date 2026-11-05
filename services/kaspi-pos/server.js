import 'dotenv/config';
import express from 'express';
import path from 'path';
import { fileURLToPath } from 'url';
import QRCode from 'qrcode';
import { PORT, HOST, ROOT_DIR, DATA_DIR, ENABLE_DEMO_UI } from './src/config.js';
import authRoutes from './src/routes/auth.js';
import invoiceRoutes from './src/routes/invoice.js';
import qrRoutes from './src/routes/qr.js';
import historyRoutes from './src/routes/history.js';
import refundRoutes from './src/routes/refund.js';
import sessionRoutes from './src/routes/session.js';
import { startPolling } from './src/polling.js';
import { requireApiKey, tightenPermissions } from './src/security.js';
import { logger } from './src/logger.js';

export const createApp = ({ apiKey, demoUi = false }) => {
  const app = express();
  app.disable('x-powered-by');
  app.use(express.json({ limit: '32kb' }));

  app.get('/health', (req, res) => res.json({ status: 'ok' }));

  // Every /api/* route requires the shared internal key
  app.use('/api', requireApiKey(apiKey));
  app.use('/api/auth', authRoutes);
  app.use('/api/invoice', invoiceRoutes);
  app.use('/api/qr', qrRoutes);
  app.use('/api/history', historyRoutes);
  app.use('/api/refund', refundRoutes);
  app.use('/api/session', sessionRoutes);

  if (demoUi) {
    // Demo QR is rendered locally — the token never leaves this host
    app.get('/demo/qr.svg', async (req, res) => {
      const text = String(req.query.text || '').slice(0, 512);
      if (!text) return res.status(400).end();
      res.type('image/svg+xml').send(await QRCode.toString(text, { type: 'svg', margin: 1 }));
    });
    app.use(express.static(path.join(ROOT_DIR, 'public')));
  }

  return app;
};

const isMain = process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);

if (isMain) {
  const apiKey = process.env.INTERNAL_API_KEY;
  if (!apiKey || apiKey.length < 32) {
    console.error('FATAL: INTERNAL_API_KEY is not set (min 32 chars). Generate: openssl rand -hex 32');
    process.exit(1);
  }

  tightenPermissions(
    ['keypair.json', 'device.json', 'ecdh-keypair.json', 'tracked-payments.json', 'webhook-retries.json', 'webhooks.json', '.env'].map(
      (f) => path.join(DATA_DIR, f),
    ),
  );

  createApp({ apiKey, demoUi: ENABLE_DEMO_UI }).listen(PORT, HOST, () => {
    logger.info('SERVER', `kaspi-pos listening on ${HOST}:${PORT} (demo UI: ${ENABLE_DEMO_UI ? 'on' : 'off'})`);
    startPolling();
  });
}
