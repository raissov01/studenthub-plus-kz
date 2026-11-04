// Reminder bot tests: node --test tests/
// Telegram and the site are mocked through globalThis.fetch; storage is the in-memory store.
import { test, beforeEach } from 'node:test';
import assert from 'node:assert/strict';

process.env.BOT_TOKEN = 'TEST_TOKEN';
process.env.WEBHOOK_SECRET = 'hook-secret';
process.env.CRON_SECRET = 'cron-secret';
process.env.SITE_URL = 'https://site.test';
process.env.ADMIN_IDS = '900, 901';
delete process.env.DIGEST_ENABLED;
delete process.env.UPSTASH_REDIS_REST_URL; delete process.env.KV_REST_API_URL;

const { memoryStore, setStore } = await import('../lib/bot/store.js');
const { localDate, addDays } = await import('../lib/bot/util.js');
const { default: telegramHandler } = await import('../api/telegram.js');
const { default: cronHandler } = await import('../api/cron.js');
const { runCron } = await import('../lib/bot/cron.js');

const today = localDate();
const opp = (id, inDays, extra = {}) => ({
  id, type: 'grant', city: 'astana', spec: ['all'],
  title: { kk: `Мүмкіндік ${id}`, ru: `Возможность ${id}` },
  deadline: inDays == null ? null : addDays(today, inDays),
  docs: [{ kk: 'Транскрипт', ru: 'Транскрипт' }],
  posted_at: addDays(today, -1),
  ...extra
});

let store, sent, opps, tgFail, contentApi;

beforeEach(() => {
  store = memoryStore();
  setStore(store);
  sent = [];
  tgFail = null;
  contentApi = true;
  opps = [opp('o7', 7), opp('o3', 3), opp('o1', 1), opp('o20', 20), opp('old', -2), opp('nodl', null), opp('o10', 10)];
  delete process.env.DIGEST_ENABLED;
  globalThis.fetch = async (url, init = {}) => {
    url = String(url);
    if (url.startsWith('https://api.telegram.org/botTEST_TOKEN/')) {
      const method = url.split('/').pop();
      const body = JSON.parse(init.body || '{}');
      sent.push({ method, ...body });
      const fail = tgFail?.(method, body);
      if (fail) return json(fail, fail.error_code);
      return json({ ok: true, result: { message_id: sent.length } });
    }
    if (url === 'https://site.test/api/content') return contentApi ? json({ opportunities: opps, jobs: [], discounts: [] }) : json({}, 404);
    if (url === 'https://site.test/data/opportunities.json') return json(opps);
    throw new Error('unexpected fetch ' + url);
  };
});

function json(obj, status = 200) {
  return new Response(JSON.stringify(obj), { status, headers: { 'content-type': 'application/json' } });
}

function mockRes() {
  const res = { statusCode: 200, body: null, headers: {} };
  res.status = c => { res.statusCode = c; return res; };
  res.json = b => { res.body = b; return res; };
  res.setHeader = (k, v) => { res.headers[k] = v; };
  return res;
}

let updateId = 1;
async function hook(update, secret = 'hook-secret') {
  const res = mockRes();
  const headers = secret == null ? {} : { 'x-telegram-bot-api-secret-token': secret };
  await telegramHandler({ method: 'POST', headers, body: { update_id: updateId++, ...update } }, res);
  return res;
}
const from = (id, lang = 'kk') => ({ id, is_bot: false, first_name: 'S', language_code: lang });
const say = (id, text, lang) => hook({ message: { message_id: 1, from: from(id, lang), chat: { id, type: 'private' }, date: 0, text } });
const press = (id, data, message) => hook({ callback_query: { id: 'cb' + updateId, from: from(id), data, message: message || { message_id: 5, chat: { id }, reply_markup: { inline_keyboard: [] } } } });
const messages = (chatId) => sent.filter(s => s.method === 'sendMessage' && (chatId == null || s.chat_id === chatId));
const cron = async (auth = 'Bearer cron-secret') => { const res = mockRes(); await cronHandler({ method: 'GET', headers: auth ? { authorization: auth } : {} }, res); return res; };

test('registration: /start remind_<id> saves reminder, confirms in kk, warns about money once', async () => {
  await say(100, '/start remind_o20');
  const msgs = messages(100);
  assert.equal(msgs.length, 2);
  assert.match(msgs[0].text, /«Мүмкіндік o20» мерзімі — \d+ \S+.* 7, 3 және 1 күн қалғанда еске саламын ✅/);
  assert.equal(msgs[0].reply_markup.inline_keyboard[0][0].url, 'https://site.test/#/opportunity/o20');
  assert.match(msgs[1].text, /Бот ешқашан ақша сұрамайды/);
  assert.equal((await store.listReminders('100')).length, 1);

  await say(100, '/start remind_o10');
  assert.equal(messages(100).length, 3, 'money warning is not repeated');
  assert.equal((await store.listReminders('100')).length, 2);
});

test('registration in Russian uses Russian date and text', async () => {
  await say(101, '/start remind_o20', 'ru');
  assert.match(messages(101)[0].text, /Дедлайн «Возможность o20» — \d+ [а-я]+.*Напомню за 7, 3 и 1 день ✅/);
  assert.match(messages(101)[1].text, /Бот никогда не просит денег/);
});

test('plain /start shows greeting with language and site buttons; language switch works', async () => {
  await say(102, '/start');
  const kb = messages(102)[0].reply_markup.inline_keyboard;
  assert.deepEqual(kb[0].map(b => b.callback_data), ['lang:kk', 'lang:ru']);
  assert.equal(kb[1][0].url, 'https://site.test/');
  await press(102, 'lang:ru');
  assert.equal((await store.getUser(102)).lang, 'ru');
});

test('falls back to /data/opportunities.json when /api/content is missing', async () => {
  contentApi = false;
  await say(103, '/start remind_o20');
  assert.equal((await store.listReminders('103')).length, 1);
});

test('not found / expired / no deadline -> polite text, site button, nothing saved', async () => {
  for (const [id, re] of [['nope', /таба алмадым/], ['old', /аяқталып қойыпты/], ['nodl', /нақты мерзім жоқ/], ['o1', /уақыт қалмады/], ['bad$id', /таба алмадым/]]) {
    sent = [];
    await say(104, `/start remind_${id}`);
    const m = messages(104)[0];
    assert.match(m.text, re, id);
    assert.equal(m.reply_markup.inline_keyboard[0][0].url, 'https://site.test/');
  }
  assert.equal((await store.listReminders('104')).length, 0);
});

test('cron: merges all due reminders into ONE message, nearest deadline first, with docs and buttons', async () => {
  const u = { id: '200', lang: 'kk', active: true, warned: true };
  await store.saveUser(u);
  for (const o of opps.filter(x => ['o7', 'o3', 'o1', 'o20'].includes(x.id))) {
    await store.putReminder('200', { oppId: o.id, deadline: o.deadline, title: o.title, addedAt: '' });
  }
  const res = await cron();
  assert.equal(res.statusCode, 200);
  const msgs = messages('200');
  assert.equal(msgs.length, 1);
  const t = msgs[0].text;
  assert.ok(t.indexOf('Мүмкіндік o1') < t.indexOf('Мүмкіндік o3') && t.indexOf('Мүмкіндік o3') < t.indexOf('Мүмкіндік o7'));
  assert.ok(!t.includes('Мүмкіндік o20'));
  assert.match(t, /Ертең — соңғы күн/);
  assert.match(t, /Дайындайтын құжаттар: Транскрипт/);
  const buttons = msgs[0].reply_markup.inline_keyboard.flat();
  assert.ok(buttons.some(b => b.url === 'https://site.test/#/opportunity/o1'));
  assert.ok(buttons.some(b => b.callback_data === 'done:o3' && b.text.includes('Тапсырдым ✅')));
  assert.ok(buttons.some(b => b.callback_data === 'skip:o7' && b.text.includes('Қажет емес')));
});

test('cron: second run the same day sends nothing', async () => {
  await store.saveUser({ id: '201', lang: 'ru', active: true });
  await store.putReminder('201', { oppId: 'o3', deadline: addDays(today, 3), title: opps[1].title, addedAt: '' });
  await runCron();
  assert.equal(messages('201').length, 1);
  const second = await runCron();
  assert.equal(messages('201').length, 1);
  assert.equal(second.alreadySent, 1);
});

test('cron: Telegram 403 marks the user inactive and later runs skip them', async () => {
  await store.saveUser({ id: '202', lang: 'kk', active: true });
  await store.putReminder('202', { oppId: 'o7', deadline: addDays(today, 7), title: opps[0].title, addedAt: '' });
  tgFail = (method, body) => (body.chat_id === '202' ? { ok: false, error_code: 403, description: 'Forbidden: bot was blocked by the user' } : null);
  const r = await runCron();
  assert.equal(r.blocked, 1);
  assert.equal((await store.getUser('202')).active, false);
  sent = [];
  await runCron({ now: new Date(Date.now() + 864e5 * 4) }); // o7 now has 3 days left
  assert.equal(messages('202').length, 0);
});

test('cron: paused users get nothing; /resume brings them back', async () => {
  await say(203, '/start remind_o20');
  await say(203, '/pause');
  assert.ok((await store.getUser(203)).pausedUntil > today);
  await store.putReminder('203', { oppId: 'o3', deadline: addDays(today, 3), title: opps[1].title, addedAt: '' });
  sent = [];
  await runCron();
  assert.equal(messages('203').length, 0);
  await say(203, '/resume');
  sent = [];
  await runCron();
  assert.equal(messages('203').length, 1);
});

test('webhook: missing or wrong secret -> 401, nothing processed', async () => {
  assert.equal((await hook({ message: { from: from(1), chat: { id: 1, type: 'private' }, text: '/start' } }, null)).statusCode, 401);
  assert.equal((await hook({ message: { from: from(1), chat: { id: 1, type: 'private' }, text: '/start' } }, 'wrong')).statusCode, 401);
  assert.equal(sent.length, 0);
  assert.equal(await store.getUser(1), null);
  assert.equal((await hook({ message: { from: from(1), chat: { id: 1, type: 'private' }, text: '/start' } })).statusCode, 200);
});

test('cron: wrong or missing CRON_SECRET -> 401', async () => {
  assert.equal((await cron('Bearer nope')).statusCode, 401);
  assert.equal((await cron(null)).statusCode, 401);
  assert.equal((await cron('cron-secret')).statusCode, 401);
  assert.equal(sent.length, 0);
});

test('"Тапсырдым" stops the reminder and counts as submitted; "Қажет емес" stops without counting', async () => {
  await say(300, '/start remind_o20');
  await say(300, '/start remind_o10');
  const message = { message_id: 9, chat: { id: 300 }, reply_markup: { inline_keyboard: [
    [{ text: '1 · Ашу', url: 'https://site.test/#/opportunity/o20' }],
    [{ text: '1 · Тапсырдым ✅', callback_data: 'done:o20' }, { text: '1 · Қажет емес', callback_data: 'skip:o20' }],
    [{ text: '2 · Ашу', url: 'https://site.test/#/opportunity/o10' }],
    [{ text: '2 · Тапсырдым ✅', callback_data: 'done:o10' }, { text: '2 · Қажет емес', callback_data: 'skip:o10' }]
  ] } };
  await press(300, 'done:o20', message);
  assert.deepEqual((await store.listReminders('300')).map(r => r.oppId), ['o10']);
  assert.equal(await store.countEvents('submitted', 0), 1);
  const kbEdit = sent.find(s => s.method === 'editMessageReplyMarkup');
  assert.ok(!JSON.stringify(kbEdit.reply_markup).includes('o20'));
  assert.ok(JSON.stringify(kbEdit.reply_markup).includes('o10'));
  await press(300, 'skip:o10', message);
  assert.equal((await store.listReminders('300')).length, 0);
  assert.equal(await store.countEvents('submitted', 0), 1);
  // stopped reminders never come back in cron
  sent = [];
  await runCron({ now: new Date(Date.now() + 864e5 * 13) });
  assert.equal(messages('300').length, 0);
});

test('/my lists reminders with a delete button each; delete works', async () => {
  await say(301, '/start remind_o20');
  await say(301, '/start remind_o10');
  sent = [];
  await say(301, '/my');
  const m = messages(301)[0];
  assert.ok(m.text.indexOf('o10') < m.text.indexOf('o20'));
  const dels = m.reply_markup.inline_keyboard.flat().filter(b => b.callback_data?.startsWith('del:'));
  assert.deepEqual(dels.map(b => b.callback_data), ['del:o10', 'del:o20']);
  await press(301, 'del:o10');
  assert.deepEqual((await store.listReminders('301')).map(r => r.oppId), ['o20']);
});

test('/stop asks first, then really deletes everything', async () => {
  await say(400, '/start remind_o20');
  await store.claimDay('400', today);
  await say(400, '/stop');
  assert.ok(messages(400).at(-1).reply_markup.inline_keyboard[0].some(b => b.callback_data === 'stop:yes'));
  assert.ok(await store.getUser(400), 'nothing deleted before confirm');
  await press(400, 'stop:no');
  assert.ok(await store.getUser(400));
  await press(400, 'stop:yes');
  assert.equal(await store.getUser(400), null);
  assert.equal((await store.listReminders('400')).length, 0);
  assert.equal((await store.listUsers()).length, 0);
  assert.deepEqual(store._dump(), { users: [], reminders: [], days: [] });
});

test('/stats only for ADMIN_IDS', async () => {
  await say(500, '/start remind_o20');
  sent = [];
  await say(500, '/stats');
  assert.doesNotMatch(messages(500)[0].text, /Студенттер/);
  await say(900, '/start');
  await say(900, '/stats');
  const t = messages(900).find(m => /Студенттер/.test(m.text)).text;
  assert.match(t, /Студенттер: 2/);
  assert.match(t, /Белсенді еске салулар: 1/);
  assert.match(t, /«Тапсырдым»: 0/);
});

test('digest is off by default: no city question, no digest on Thursday', async () => {
  await say(600, '/start remind_o20');
  assert.ok(!messages(600).some(m => /Қай қаладасың/.test(m.text)));
  const u = await store.getUser(600);
  await store.saveUser({ ...u, city: 'astana', dir: 'it' });
  sent = [];
  const thursday = nextWeekday(4);
  const r = await runCron({ now: thursday });
  assert.equal(r.digests, 0);
  assert.equal(messages('600').length, 0);
});

test('digest when enabled: asks city+direction once, Thursday send, moves to Friday if a reminder went out', async () => {
  process.env.DIGEST_ENABLED = 'true';
  await say(700, '/start remind_o20');
  assert.ok(messages(700).some(m => /Қай қаладасың/.test(m.text)));
  await press(700, 'city:astana');
  await press(700, 'dir:it');
  const u = await store.getUser(700);
  assert.equal(u.city, 'astana'); assert.equal(u.dir, 'it'); assert.equal(u.digestAsked, true);
  sent = [];
  await say(700, '/start remind_o20');
  assert.ok(!messages(700).some(m => /Қай қаладасың/.test(m.text)), 'asked only once');

  // a fresh opportunity posted "today" relative to the simulated Thursday
  const thu = nextWeekday(4);
  const thuIso = localDate(thu);
  opps.push(opp('new1', 30, { posted_at: thuIso, spec: ['it'] }), opp('new2', 30, { posted_at: thuIso, city: 'almaty' }));
  // reminder due on that Thursday -> digest must wait
  await store.putReminder('700', { oppId: 'dueThu', deadline: addDaysIso(thuIso, 3), title: { kk: 'D', ru: 'D' }, addedAt: '' });
  sent = [];
  const r1 = await runCron({ now: thu });
  assert.equal(r1.reminders, 1); assert.equal(r1.digests, 0);
  assert.equal(messages('700').length, 1);
  sent = [];
  const fri = new Date(thu.getTime() + 864e5);
  const r2 = await runCron({ now: fri });
  assert.equal(r2.digests, 1);
  const d = messages('700')[0];
  assert.match(d.text, /new1/); assert.doesNotMatch(d.text, /new2/, 'other city filtered out');
  sent = [];
  const r3 = await runCron({ now: new Date(fri.getTime() + 864e5) });
  assert.equal(r3.digests, 0, 'one digest per week');
  delete process.env.DIGEST_ENABLED;
});

function addDaysIso(iso, n) { return addDays(iso, n); }
function nextWeekday(target) {
  for (let i = 1; i <= 7; i++) {
    const d = new Date(Date.now() + i * 864e5);
    const wd = new Date(localDate(d) + 'T00:00:00Z').getUTCDay() || 7;
    if (wd === target) return d;
  }
}

test('Upstash store speaks the REST protocol (emulated Redis)', async () => {
  const { upstashStore } = await import('../lib/bot/store.js');
  const db = new Map();
  const calls = [];
  const exec = ([c, k, ...a]) => {
    c = c.toUpperCase();
    const h = () => (db.get(k) instanceof Map ? db.get(k) : (db.set(k, new Map()), db.get(k)));
    const set = () => (db.get(k) instanceof Set ? db.get(k) : (db.set(k, new Set()), db.get(k)));
    switch (c) {
      case 'GET': return db.has(k) ? db.get(k) : null;
      case 'SET': { const prev = db.get(k) ?? null; db.set(k, a[0]); return a.includes('GET') ? prev : 'OK'; }
      case 'DEL': return [k, ...a].filter(x => db.delete(x)).length;
      case 'SADD': return a.filter(x => !set().has(x) && set().add(x)).length;
      case 'SREM': return a.filter(x => set().delete(x)).length;
      case 'SMEMBERS': return [...(db.get(k) || [])];
      case 'HSET': h().set(a[0], a[1]); return 1;
      case 'HDEL': return h().delete(a[0]) ? 1 : 0;
      case 'HGETALL': return [...(db.get(k) || new Map())].flat();
      case 'ZADD': h().set(a[1], Number(a[0])); return 1;
      case 'ZREMRANGEBYSCORE': return 0;
      case 'ZCOUNT': return [...(db.get(k) || new Map()).values()].filter(v => v >= Number(k && a[0])).length;
      default: throw new Error('unsupported ' + c);
    }
  };
  const fetchImpl = async (url, init) => {
    assert.equal(init.headers.Authorization, 'Bearer tok');
    const body = JSON.parse(init.body);
    calls.push(body);
    return json(url.endsWith('/pipeline') ? body.map(cmd => ({ result: exec(cmd) })) : { result: exec(body) });
  };
  const s = upstashStore({ url: 'https://redis.test/', token: 'tok', fetchImpl });
  await s.saveUser({ id: '1', lang: 'kk' });
  await s.putReminder('1', { oppId: 'o1', deadline: '2030-01-01', title: { kk: 'a', ru: 'a' } });
  assert.deepEqual((await s.listUsers()).map(u => u.id), ['1']);
  assert.equal((await s.listReminders()).length, 1);
  assert.equal((await s.listReminders('1'))[0].userId, '1');
  assert.equal(await s.claimDay('1', '2030-01-01'), true);
  assert.equal(await s.claimDay('1', '2030-01-01'), false);
  assert.equal(await s.claimDay('1', '2030-01-02'), true);
  await s.logEvent('submitted', 1000);
  assert.equal(await s.countEvents('submitted', 500), 1);
  assert.equal(await s.removeReminder('1', 'o1'), true);
  assert.equal(await s.removeReminder('1', 'o1'), false);
  await s.putReminder('1', { oppId: 'o2', deadline: '2030-01-01', title: {} });
  await s.deleteUser('1');
  assert.deepEqual([...db.keys()].filter(k => !k.startsWith('ev:') && !(db.get(k) instanceof Set && !db.get(k).size)), []);
  assert.equal(await s.getUser('1'), null);
});
