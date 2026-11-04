import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';
import { logger } from './logger.js';
import { isAllowedWebhookUrl, allowedHttpHostsFromEnv } from './security.js';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const WEBHOOKS_FILE = process.env.WEBHOOKS_FILE || path.join(process.env.DATA_DIR || path.join(__dirname, '..'), 'webhooks.json');

// Warn once per rejected hook, not on every poll cycle
const warned = new Set();
const warnOnce = (key, msg) => {
  if (warned.has(key)) return;
  warned.add(key);
  logger.warn('WEBHOOK STORE', msg);
};

/**
 * Validates raw webhook entries: drops hooks without a secret (they could not be verified
 * by the receiver) and hooks whose URL is not https (plain http only for localhost and
 * WEBHOOK_ALLOWED_HTTP_HOSTS).
 */
export const filterValidWebhooks = (hooks, allowedHttpHosts = allowedHttpHostsFromEnv()) =>
  hooks.filter((hook, i) => {
    if (!hook || typeof hook.url !== 'string' || !Array.isArray(hook.events)) return false;
    if (typeof hook.secret !== 'string' || !hook.secret.trim()) {
      warnOnce(`secret:${i}`, `Webhook #${i} skipped: empty secret`);
      return false;
    }
    if (!isAllowedWebhookUrl(hook.url, allowedHttpHosts)) {
      warnOnce(`url:${i}`, `Webhook #${i} skipped: URL must be https (http only for localhost/allowed hosts)`);
      return false;
    }
    return true;
  });

/**
 * Читает webhooks.json и возвращает массив валидных вебхуков.
 * При ошибке чтения/парсинга возвращает [].
 */
export const ALL_EVENTS = ['payment.success', 'payment.failed', 'payment.expired', 'payment.lost'];

/**
 * Student Hub patch: one hook straight from the environment (WEBHOOK_URL + WEBHOOK_SECRET, optional
 * comma-separated WEBHOOK_EVENTS), so a compose deployment needs no hand-written webhooks.json.
 */
export const envWebhook = (env = process.env) => {
  const url = (env.WEBHOOK_URL || '').trim();
  if (!url) return [];
  const events = (env.WEBHOOK_EVENTS || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);
  return [{ url, secret: env.WEBHOOK_SECRET || '', events: events.length ? events : ALL_EVENTS }];
};

export const loadWebhooks = () => {
  const fromEnv = envWebhook();
  let fromFile = [];
  try {
    const raw = fs.readFileSync(WEBHOOKS_FILE, 'utf8');
    const hooks = JSON.parse(raw);
    if (Array.isArray(hooks)) fromFile = hooks;
  } catch (err) {
    if (err.code !== 'ENOENT') {
      warnOnce('read', `Error reading webhooks.json: ${err.message}`);
    }
  }
  return filterValidWebhooks([...fromEnv, ...fromFile]);
};

/**
 * Возвращает вебхуки, подписанные на указанное событие.
 */
export const getWebhooksByEvent = (event) => loadWebhooks().filter((hook) => hook.events.includes(event));
