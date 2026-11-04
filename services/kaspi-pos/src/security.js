import crypto from 'crypto';
import fs from 'fs';

// ─── Internal API key (X-Internal-Api-Key) ───

const digest = (s) => crypto.createHash('sha256').update(String(s)).digest();

/**
 * Express middleware: rejects requests without a valid X-Internal-Api-Key.
 * Both sides are hashed first so timingSafeEqual always compares equal-length buffers.
 */
export const requireApiKey = (expectedKey) => {
  if (!expectedKey) throw new Error('INTERNAL_API_KEY is not set');
  const expected = digest(expectedKey);
  return (req, res, next) => {
    const given = req.headers['x-internal-api-key'];
    if (typeof given !== 'string' || !given || !crypto.timingSafeEqual(digest(given), expected)) {
      return res.status(401).json({ error: 'unauthorized' });
    }
    next();
  };
};

// ─── Amount validation ───

const envLimit = (name, fallback) => {
  const n = Number(process.env[name]);
  return Number.isFinite(n) && n > 0 ? n : fallback;
};

export const maxPaymentAmount = () => envLimit('MAX_PAYMENT_AMOUNT', 100000);
export const maxRefundAmount = () => envLimit('MAX_REFUND_AMOUNT', 100000);

/** Returns the amount as a number, or null if it is not a positive finite number ≤ max. */
export const parseAmount = (value, max) => {
  if (value === null || value === undefined || value === '' || typeof value === 'boolean') return null;
  const n = typeof value === 'number' ? value : Number(String(value).trim());
  if (!Number.isFinite(n) || n <= 0 || n > max) return null;
  return n;
};

// ─── Private files (keys, sessions, logs) ───

export const FILE_MODE = 0o600;
export const DIR_MODE = 0o700;

/** writeFileSync that always leaves the file at 0600 (mode only applies on create, so chmod too). */
export const writePrivateFile = (file, data) => {
  fs.writeFileSync(file, data, { mode: FILE_MODE });
  fs.chmodSync(file, FILE_MODE);
};

export const ensurePrivateDir = (dir) => {
  fs.mkdirSync(dir, { recursive: true, mode: DIR_MODE });
  fs.chmodSync(dir, DIR_MODE);
};

/** Tightens permissions on files that may already exist from older runs. */
export const tightenPermissions = (files) => {
  for (const f of files) {
    try {
      if (fs.existsSync(f)) fs.chmodSync(f, FILE_MODE);
    } catch {
      /* best effort */
    }
  }
};

// ─── Webhook URL policy ───

const LOCAL_HOSTS = new Set(['localhost', '127.0.0.1', '::1', '[::1]']);

/**
 * https everywhere; plain http only for localhost and hosts explicitly listed in
 * WEBHOOK_ALLOWED_HTTP_HOSTS (e.g. the docker-internal "api" service).
 */
export const isAllowedWebhookUrl = (raw, allowedHttpHosts = []) => {
  let u;
  try {
    u = new URL(raw);
  } catch {
    return false;
  }
  if (u.protocol === 'https:') return true;
  if (u.protocol !== 'http:') return false;
  return LOCAL_HOSTS.has(u.hostname) || allowedHttpHosts.includes(u.hostname);
};

export const allowedHttpHostsFromEnv = () =>
  (process.env.WEBHOOK_ALLOWED_HTTP_HOSTS || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);
