// Webhook update handling: /start remind_<id>, commands, button callbacks.
import { getStore } from './store.js';
import { tg, send, edit } from './telegram.js';
import { loadOpportunities, oppUrl } from './content.js';
import { T, LANG_BUTTONS, CITIES, DIRS, fmtDate } from './texts.js';
import { config, localDate, daysBetween, addDays, isIsoDate, esc, pick, ID_RE, REMIND_DAYS, MAX_REMINDERS } from './util.js';

const siteButton = lang => [[{ text: T[lang].btnSite, url: config().siteUrl + '/' }]];

function newUser(from) {
  return {
    id: String(from.id),
    lang: /^ru|^be|^uk/i.test(from.language_code || '') ? 'ru' : 'kk',
    active: true, warned: false, pausedUntil: null,
    city: null, dir: null, digestAsked: false, digestWeek: null, digestSeen: [],
    createdAt: new Date().toISOString()
  };
}

export async function handleUpdate(update) {
  const store = getStore();
  if (update.my_chat_member) return onChatMember(store, update.my_chat_member);
  const msg = update.message;
  const cq = update.callback_query;
  const from = msg?.from || cq?.from;
  if (!from || from.is_bot) return;
  if (msg && msg.chat?.type !== 'private') return; // the bot only talks in private chats

  let user = await store.getUser(from.id);
  const isNew = !user;
  if (!user) user = newUser(from);
  user.active = true;

  const ctx = { store, user, chatId: from.id, deleted: false };
  if (msg) await onMessage(ctx, msg);
  else if (cq) await onCallback(ctx, cq);

  if (ctx.deleted) return; // /stop confirmed: nothing may be written back
  await store.saveUser(ctx.user);
  if (isNew && !ctx.user.warned) {
    ctx.user.warned = true;
    await store.saveUser(ctx.user);
    await send(ctx.chatId, T[ctx.user.lang].noMoney);
  }
}

async function onChatMember(store, upd) {
  if (upd.chat?.type !== 'private') return;
  const user = await store.getUser(upd.from.id);
  if (!user) return;
  const status = upd.new_chat_member?.status;
  user.active = !(status === 'kicked' || status === 'left');
  await store.saveUser(user);
}

async function onMessage(ctx, msg) {
  const text = String(msg.text || '').trim();
  const m = text.match(/^\/([a-z]+)(?:@\w+)?(?:\s+(.*))?$/i);
  const cmd = m?.[1].toLowerCase();
  const arg = (m?.[2] || '').trim();
  const L = T[ctx.user.lang];

  switch (cmd) {
    case 'start': {
      const rm = arg.match(/^remind_(.+)$/);
      if (rm) return startRemind(ctx, rm[1]);
      return send(ctx.chatId, L.greeting, [...LANG_BUTTONS, ...siteButton(ctx.user.lang)]);
    }
    case 'my': return sendMy(ctx);
    case 'pause': {
      ctx.user.pausedUntil = addDays(localDate(), 7);
      return send(ctx.chatId, L.paused(fmtDate(ctx.user.pausedUntil, ctx.user.lang, localDate())));
    }
    case 'resume': {
      const was = ctx.user.pausedUntil && ctx.user.pausedUntil > localDate();
      ctx.user.pausedUntil = null;
      return send(ctx.chatId, was ? L.resumed : L.notPaused);
    }
    case 'stop':
      return send(ctx.chatId, L.stopAsk, [[{ text: L.stopYes, callback_data: 'stop:yes' }, { text: L.stopNo, callback_data: 'stop:no' }]]);
    case 'stats':
      if (config().admins.has(ctx.user.id)) return send(ctx.chatId, L.stats(await stats(ctx.store)));
      return send(ctx.chatId, L.help); // non-admins see nothing special
    default:
      return send(ctx.chatId, L.help, siteButton(ctx.user.lang));
  }
}

export async function stats(store, now = Date.now()) {
  const users = await store.listUsers();
  const activeIds = new Set(users.filter(u => u.active).map(u => u.id));
  const today = localDate(new Date(now));
  const reminders = (await store.listReminders()).filter(r => activeIds.has(r.userId) && r.deadline >= today);
  return {
    users: users.length,
    active: activeIds.size,
    reminders: reminders.length,
    submitted: await store.countEvents('submitted', now - 7 * 864e5)
  };
}

async function findOpp(id) {
  const list = await loadOpportunities();
  return list.find(o => String(o.id) === id) || null;
}

// Saves a reminder; returns the reply text. Shared by /start remind_<id> and the digest button.
async function saveReminder(ctx, id, { short = false } = {}) {
  const { lang } = ctx.user;
  const L = T[lang];
  const today = localDate();
  if (!ID_RE.test(id)) return { text: L.notFound };
  let opp;
  try { opp = await findOpp(id); } catch { return { text: L.loadFail }; }
  if (!opp) return { text: L.notFound };
  const title = esc(pick(opp.title, lang));
  if (!isIsoDate(opp.deadline)) return { text: L.noDeadline(title) };
  const date = fmtDate(opp.deadline, lang, today);
  const left = daysBetween(today, opp.deadline);
  if (left < 0) return { text: L.expired(title, date) };
  const days = REMIND_DAYS.filter(d => d < left);
  if (!days.length) return { text: L.tooLate(title, date) };

  const existing = await ctx.store.listReminders(ctx.user.id);
  if (existing.some(r => r.oppId === id)) return { text: L.already(title, date), ok: true };
  if (existing.length >= MAX_REMINDERS) return { text: L.tooMany };
  await ctx.store.putReminder(ctx.user.id, {
    oppId: id, deadline: opp.deadline,
    title: { kk: pick(opp.title, 'kk'), ru: pick(opp.title, 'ru') },
    addedAt: new Date().toISOString()
  });
  return { text: short ? L.remindSaved(title, date) : L.confirm(title, date, days), ok: true };
}

async function startRemind(ctx, id) {
  const L = T[ctx.user.lang];
  const r = await saveReminder(ctx, id);
  await send(ctx.chatId, r.text, r.ok
    ? [[{ text: L.btnOpen, url: oppUrl(id) }]]
    : siteButton(ctx.user.lang));
  if (r.ok && config().digest && !ctx.user.digestAsked) await askCity(ctx);
}

async function askCity(ctx) {
  const { lang } = ctx.user;
  const keys = Object.keys(CITIES);
  const rows = [];
  for (let i = 0; i < keys.length; i += 3) rows.push(keys.slice(i, i + 3).map(k => ({ text: CITIES[k][lang], callback_data: `city:${k}` })));
  ctx.user.digestAsked = true;
  await send(ctx.chatId, T[lang].askCity, rows);
}

async function sendMy(ctx) {
  const { lang } = ctx.user;
  const L = T[lang];
  const today = localDate();
  const list = (await ctx.store.listReminders(ctx.user.id)).sort((a, b) => a.deadline.localeCompare(b.deadline));
  if (!list.length) return send(ctx.chatId, L.myEmpty, siteButton(lang));
  const lines = list.map((r, i) => `${i + 1}. <b>${esc(pick(r.title, lang))}</b> — ${fmtDate(r.deadline, lang, today)}`);
  if (ctx.user.pausedUntil && ctx.user.pausedUntil > today) lines.push('', L.myPaused(fmtDate(ctx.user.pausedUntil, lang, today)));
  const kb = list.map((r, i) => [{ text: `${i + 1} · ${L.btnOpen}`, url: oppUrl(r.oppId) }, { text: `${i + 1} · ${L.btnDelete}`, callback_data: `del:${r.oppId}` }]);
  return send(ctx.chatId, `${L.myHead}\n\n${lines.join('\n')}`, kb);
}

// Drops the buttons that belong to one opportunity from a message keyboard.
function withoutOpp(markup, id) {
  const rows = markup?.inline_keyboard || [];
  return rows
    .map(row => row.filter(b => !(b.callback_data && b.callback_data.split(':')[1] === id) && !(b.url && b.url.endsWith(`/opportunity/${encodeURIComponent(id)}`))))
    .filter(row => row.length);
}

async function onCallback(ctx, cq) {
  const { lang } = ctx.user;
  const L = T[lang];
  const [action, value = ''] = String(cq.data || '').split(':');
  const msg = cq.message;
  const answer = (text, alert = false) => tg('answerCallbackQuery', { callback_query_id: cq.id, ...(text ? { text, show_alert: alert } : {}) }).catch(() => {});
  const setKeyboard = kb => (msg ? tg('editMessageReplyMarkup', { chat_id: msg.chat.id, message_id: msg.message_id, reply_markup: { inline_keyboard: kb } }).catch(() => {}) : null);

  switch (action) {
    case 'lang': {
      if (value !== 'kk' && value !== 'ru') return answer();
      ctx.user.lang = value;
      await answer(T[value].langSet);
      return send(ctx.chatId, T[value].langSet + '\n\n' + T[value].help, siteButton(value));
    }
    case 'done':
    case 'skip': {
      const removed = await ctx.store.removeReminder(ctx.user.id, value);
      if (removed && action === 'done') await ctx.store.logEvent('submitted', Date.now());
      await setKeyboard(withoutOpp(msg?.reply_markup, value));
      return answer(removed ? (action === 'done' ? L.doneToast : L.skipToast) : L.gone, removed);
    }
    case 'del': {
      await ctx.store.removeReminder(ctx.user.id, value);
      await setKeyboard(withoutOpp(msg?.reply_markup, value));
      return answer(L.deleted);
    }
    case 'stop': {
      if (value === 'yes') {
        await ctx.store.deleteUser(ctx.user.id);
        ctx.deleted = true;
        await answer();
        return msg ? edit(msg.chat.id, msg.message_id, L.stopDone) : send(ctx.chatId, L.stopDone);
      }
      await answer();
      return msg ? edit(msg.chat.id, msg.message_id, L.stopCancel) : null;
    }
    case 'city': {
      if (!CITIES[value]) return answer();
      ctx.user.city = value;
      await answer();
      const keys = Object.keys(DIRS);
      const rows = [];
      for (let i = 0; i < keys.length; i += 2) rows.push(keys.slice(i, i + 2).map(k => ({ text: DIRS[k][lang], callback_data: `dir:${k}` })));
      return msg ? edit(msg.chat.id, msg.message_id, L.askDir, rows) : send(ctx.chatId, L.askDir, rows);
    }
    case 'dir': {
      if (!DIRS[value]) return answer();
      ctx.user.dir = value;
      await answer();
      return msg ? edit(msg.chat.id, msg.message_id, L.digestSaved) : send(ctx.chatId, L.digestSaved);
    }
    case 'rem': {
      const r = await saveReminder(ctx, value, { short: true });
      if (r.ok) await setKeyboard((msg?.reply_markup?.inline_keyboard || []).map(row => row.filter(b => b.callback_data !== `rem:${value}`)).filter(row => row.length));
      return answer(r.text.replace(/<[^>]+>/g, '').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&').slice(0, 190), true);
    }
    default:
      return answer();
  }
}

