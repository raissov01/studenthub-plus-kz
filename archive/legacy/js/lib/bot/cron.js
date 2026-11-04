// Daily job: deadline reminders (7/3/1 days left) and the optional weekly digest.
// Hard rule: at most ONE message per student per Almaty calendar day (store.claimDay).
import { getStore } from './store.js';
import { send, isBlocked } from './telegram.js';
import { loadOpportunities, oppUrl } from './content.js';
import { T, fmtDate } from './texts.js';
import { config, localDate, daysBetween, isIsoDate, weekday, weekStart, esc, pick, REMIND_DAYS } from './util.js';

const MAX_ITEMS = 8;      // per message, keeps it readable and under Telegram's 4096 chars
const MAX_DOCS = 6;
const PARALLEL = 10;      // well under Telegram's ~30 msg/s

export function reminderMessage(items, lang, today) {
  const L = T[lang];
  const shown = items.slice(0, MAX_ITEMS);
  const many = shown.length > 1;
  const parts = [many ? L.reminderHeadMany : L.reminderHead, ''];
  const kb = [];
  shown.forEach((it, i) => {
    const n = many ? `${i + 1}. ` : '';
    parts.push(`${n}<b>${esc(pick(it.title, lang))}</b>`);
    parts.push(`${L.daysLeft(it.left)} (${fmtDate(it.deadline, lang, today)})`);
    const docs = (it.docs || []).map(d => pick(d, lang)).filter(Boolean);
    if (docs.length) {
      const extra = docs.length > MAX_DOCS ? ` +${docs.length - MAX_DOCS}` : '';
      parts.push(`${L.docs}: ${esc(docs.slice(0, MAX_DOCS).join(', '))}${extra}`);
    }
    parts.push('');
    const p = many ? `${i + 1} · ` : '';
    kb.push([{ text: `${p}${L.btnOpen}`, url: oppUrl(it.oppId) }]);
    kb.push([{ text: `${p}${L.btnDone}`, callback_data: `done:${it.oppId}` }, { text: `${p}${L.btnSkip}`, callback_data: `skip:${it.oppId}` }]);
  });
  if (items.length > shown.length) parts.push(L.moreInMy(items.length - shown.length), '');
  parts.push(L.reminderFoot);
  return { text: parts.join('\n'), keyboard: kb };
}

export function digestMessage(opps, lang, today) {
  const L = T[lang];
  const parts = [L.digestHead, ''];
  const kb = [];
  opps.forEach((o, i) => {
    parts.push(`${i + 1}. <b>${esc(pick(o.title, lang))}</b>${isIsoDate(o.deadline) ? ` — ${L.digestDeadline(fmtDate(o.deadline, lang, today))}` : ''}`);
    kb.push([{ text: `${i + 1} · ${L.btnOpen}`, url: oppUrl(o.id) }, { text: `${i + 1} · ${L.btnRemind}`, callback_data: `rem:${o.id}` }]);
  });
  parts.push('', L.digestFoot);
  return { text: parts.join('\n'), keyboard: kb };
}

export function pickDigest(user, opps, savedIds, today) {
  const seen = new Set(user.digestSeen || []);
  return opps
    .filter(o => o && o.id != null && !seen.has(String(o.id)) && !savedIds.has(String(o.id)))
    .filter(o => isIsoDate(o.posted_at) && daysBetween(o.posted_at, today) >= 0 && daysBetween(o.posted_at, today) <= 7)
    .filter(o => !isIsoDate(o.deadline) || daysBetween(today, o.deadline) > 1)
    .filter(o => !o.city || o.city === 'remote' || user.city === 'other' || o.city === user.city)
    .filter(o => !Array.isArray(o.spec) || !o.spec.length || o.spec.includes('all') || user.dir === 'other' || o.spec.includes(user.dir))
    .sort((a, b) => String(b.posted_at).localeCompare(String(a.posted_at)))
    .slice(0, 3);
}

async function inChunks(list, fn) {
  for (let i = 0; i < list.length; i += PARALLEL) await Promise.all(list.slice(i, i + PARALLEL).map(fn));
}

// Sends one message under the day claim. Returns 'sent' | 'claimed' | 'blocked' | 'failed'.
async function deliver(store, user, today, text, keyboard) {
  if (!(await store.claimDay(user.id, today))) return 'claimed';
  try {
    await send(user.id, text, keyboard);
    return 'sent';
  } catch (err) {
    if (isBlocked(err)) {
      user.active = false;
      await store.saveUser(user);
      return 'blocked';
    }
    await store.releaseDay(user.id, today); // allow a retry run later the same day
    console.error('cron send failed', user.id, err.message);
    return 'failed';
  }
}

export async function runCron({ now = new Date() } = {}) {
  const store = getStore();
  const cfg = config();
  const today = localDate(now);
  const result = { date: today, reminders: 0, digests: 0, alreadySent: 0, blocked: 0, failed: 0, cleaned: 0 };

  let opps = [];
  let contentOk = true;
  try { opps = await loadOpportunities(); } catch (err) { contentOk = false; console.error('cron content', err.message); }
  const byId = new Map(opps.map(o => [String(o.id), o]));

  const users = await store.listUsers();
  const remindersByUser = new Map();
  for (const r of await store.listReminders()) {
    if (!remindersByUser.has(r.userId)) remindersByUser.set(r.userId, []);
    remindersByUser.get(r.userId).push(r);
  }

  // 1. Deadline reminders.
  await inChunks(users, async user => {
    const mine = remindersByUser.get(String(user.id)) || [];
    const due = [];
    for (const r of mine) {
      const opp = byId.get(r.oppId);
      if (opp && isIsoDate(opp.deadline) && opp.deadline !== r.deadline) {
        r.deadline = opp.deadline; // the site moved the deadline: follow it
        await store.putReminder(user.id, r);
      }
      if (contentOk && opp && !isIsoDate(opp.deadline)) continue;
      const left = daysBetween(today, r.deadline);
      if (left < 0) { await store.removeReminder(user.id, r.oppId); result.cleaned++; continue; }
      if (REMIND_DAYS.includes(left)) due.push({ ...r, left, title: opp?.title || r.title, docs: opp?.docs || [] });
    }
    if (!user.active || (user.pausedUntil && user.pausedUntil > today) || !due.length) return;
    due.sort((a, b) => a.deadline.localeCompare(b.deadline));
    const { text, keyboard } = reminderMessage(due, user.lang || 'kk', today);
    const status = await deliver(store, user, today, text, keyboard);
    if (status === 'sent') result.reminders++;
    else if (status === 'claimed') result.alreadySent++;
    else result[status]++;
  });

  // 2. Weekly digest (off unless DIGEST_ENABLED). Thursday, or the next free day that week.
  if (cfg.digest && contentOk && weekday(today) >= 4) {
    const week = weekStart(today);
    const fresh = await store.listUsers(); // re-read: step 1 may have marked users inactive
    await inChunks(fresh, async user => {
      if (!user.active || !user.city || !user.dir || user.digestWeek === week) return;
      if (user.pausedUntil && user.pausedUntil > today) return;
      const saved = new Set((remindersByUser.get(String(user.id)) || []).map(r => r.oppId));
      const list = pickDigest(user, opps, saved, today);
      if (!list.length) return;
      const { text, keyboard } = digestMessage(list, user.lang || 'kk', today);
      const status = await deliver(store, user, today, text, keyboard);
      if (status === 'sent') {
        user.digestWeek = week;
        user.digestSeen = [...(user.digestSeen || []), ...list.map(o => String(o.id))].slice(-200);
        await store.saveUser(user);
        result.digests++;
      } else if (status !== 'claimed') result[status]++;
    });
  }
  return result;
}
