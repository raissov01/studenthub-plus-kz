// Minimal Telegram Bot API client on fetch.
import { config } from './util.js';

export class TgError extends Error {
  constructor(method, code, description) {
    super(`Telegram ${method} failed: ${code} ${description}`);
    this.code = code;
    this.description = description;
  }
}

export async function tg(method, params = {}, { retry = true } = {}) {
  const { token } = config();
  if (!token) throw new Error('BOT_TOKEN is not set');
  const res = await fetch(`https://api.telegram.org/bot${token}/${method}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(params),
    signal: AbortSignal.timeout(10000)
  });
  const data = await res.json().catch(() => ({ ok: false, error_code: res.status, description: 'bad response' }));
  if (data.ok) return data.result;
  const wait = data.parameters?.retry_after;
  if (data.error_code === 429 && retry && wait && wait <= 5) {
    await new Promise(r => setTimeout(r, wait * 1000));
    return tg(method, params, { retry: false });
  }
  throw new TgError(method, data.error_code, data.description);
}

export const isBlocked = err => err instanceof TgError && err.code === 403;

export const send = (chatId, text, keyboard) => tg('sendMessage', {
  chat_id: chatId, text, parse_mode: 'HTML', link_preview_options: { is_disabled: true },
  ...(keyboard ? { reply_markup: { inline_keyboard: keyboard } } : {})
});

export const edit = (chatId, messageId, text, keyboard) => tg('editMessageText', {
  chat_id: chatId, message_id: messageId, text, parse_mode: 'HTML', link_preview_options: { is_disabled: true },
  reply_markup: { inline_keyboard: keyboard || [] }
});
