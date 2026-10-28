// Storage for the reminder bot. Two implementations share one narrow interface, so a
// Postgres/Supabase store can be dropped in later without touching the bot logic.
//
// Store interface (every method is async):
//   getUser(id)                 -> User | null
//   saveUser(user)              -> void            upsert by user.id
//   listUsers()                 -> User[]
//   deleteUser(id)              -> void            user + all reminders + day marker, for real
//   listReminders(userId?)      -> Reminder[]      one user's, or everyone's when userId is omitted
//   putReminder(userId, rem)    -> void            upsert by rem.oppId
//   removeReminder(userId, oppId) -> boolean       true if something was removed
//   claimDay(userId, date)      -> boolean         atomically mark "this user got a message on
//                                                  <date>"; false if already marked that date
//   releaseDay(userId, date)    -> void            undo a claim when the send failed
//   logEvent(kind, ts)          -> void            anonymous counter event (no user id kept)
//   countEvents(kind, sinceTs)  -> number
//
// User:     { id, lang, active, warned, pausedUntil, city, dir, digestAsked, digestWeek,
//             digestSeen: [oppId], createdAt }
// Reminder: { userId, oppId, deadline: 'YYYY-MM-DD', title: { kk, ru }, addedAt }

const DAY_MARKER_TTL = 3 * 24 * 3600;     // a day marker only needs to outlive its day
const EVENT_KEEP_MS = 60 * 24 * 3600e3;   // stats events older than 60 days are dropped

export function memoryStore() {
  const users = new Map();
  const reminders = new Map(); // userId -> Map(oppId -> reminder)
  const days = new Map();      // userId -> date
  const events = [];           // { kind, ts }
  const clone = v => (v == null ? v : JSON.parse(JSON.stringify(v)));
  const key = id => String(id);

  return {
    kind: 'memory',
    async getUser(id) { return clone(users.get(key(id))) || null; },
    async saveUser(user) { users.set(key(user.id), clone(user)); },
    async listUsers() { return [...users.values()].map(clone); },
    async deleteUser(id) {
      users.delete(key(id)); reminders.delete(key(id)); days.delete(key(id));
    },
    async listReminders(userId) {
      if (userId != null) return [...(reminders.get(key(userId))?.values() || [])].map(clone);
      return [...reminders.values()].flatMap(m => [...m.values()]).map(clone);
    },
    async putReminder(userId, rem) {
      if (!reminders.has(key(userId))) reminders.set(key(userId), new Map());
      reminders.get(key(userId)).set(rem.oppId, clone({ ...rem, userId: key(userId) }));
    },
    async removeReminder(userId, oppId) {
      const m = reminders.get(key(userId));
      const had = !!m?.delete(oppId);
      if (m && !m.size) reminders.delete(key(userId));
      return had;
    },
    async claimDay(userId, date) {
      if (days.get(key(userId)) === date) return false;
      days.set(key(userId), date);
      return true;
    },
    async releaseDay(userId, date) {
      if (days.get(key(userId)) === date) days.delete(key(userId));
    },
    async logEvent(kind, ts = Date.now()) { events.push({ kind, ts }); },
    async countEvents(kind, sinceTs) { return events.filter(e => e.kind === kind && e.ts >= sinceTs).length; },
    // test helper, not part of the interface
    _dump() { return { users: [...users.keys()], reminders: [...reminders.keys()], days: [...days.keys()] }; }
  };
}

// Upstash Redis over its REST API (no SDK). Key layout:
//   users            SET of user ids
//   u:<id>           JSON user
//   r:<id>           HASH oppId -> JSON reminder
//   d:<id>           STRING date of the last day this user got a message (TTL 3 days)
//   ev:<kind>        ZSET score=ts, member=random id (anonymous)
export function upstashStore({ url, token, fetchImpl } = {}) {
  if (!url || !token) throw new Error('Upstash URL/token missing');
  const base = url.replace(/\/+$/, '');
  const doFetch = (...a) => (fetchImpl || globalThis.fetch)(...a);

  async function call(path, body) {
    const res = await doFetch(base + path, {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
      signal: AbortSignal.timeout(8000)
    });
    const data = await res.json().catch(() => null);
    if (!res.ok || !data) throw new Error(`Upstash HTTP ${res.status}: ${data?.error || 'bad response'}`);
    return data;
  }
  async function cmd(...args) {
    const data = await call('', args);
    if (data.error) throw new Error(`Upstash: ${data.error}`);
    return data.result;
  }
  async function pipe(cmds) {
    if (!cmds.length) return [];
    const data = await call('/pipeline', cmds);
    return data.map(r => { if (r.error) throw new Error(`Upstash: ${r.error}`); return r.result; });
  }
  const parse = s => { try { return s == null ? null : JSON.parse(s); } catch { return null; } };
  const hashValues = flat => { const out = []; for (let i = 1; i < (flat || []).length; i += 2) out.push(parse(flat[i])); return out.filter(Boolean); };

  return {
    kind: 'upstash',
    async getUser(id) { return parse(await cmd('GET', `u:${id}`)); },
    async saveUser(user) { await pipe([['SET', `u:${user.id}`, JSON.stringify(user)], ['SADD', 'users', String(user.id)]]); },
    async listUsers() {
      const ids = await cmd('SMEMBERS', 'users') || [];
      const vals = await pipe(ids.map(id => ['GET', `u:${id}`]));
      return vals.map(parse).filter(Boolean);
    },
    async deleteUser(id) { await pipe([['DEL', `u:${id}`, `r:${id}`, `d:${id}`], ['SREM', 'users', String(id)]]); },
    async listReminders(userId) {
      if (userId != null) return hashValues(await cmd('HGETALL', `r:${userId}`));
      const ids = await cmd('SMEMBERS', 'users') || [];
      const all = await pipe(ids.map(id => ['HGETALL', `r:${id}`]));
      return all.flatMap(hashValues);
    },
    async putReminder(userId, rem) {
      await cmd('HSET', `r:${userId}`, rem.oppId, JSON.stringify({ ...rem, userId: String(userId) }));
    },
    async removeReminder(userId, oppId) { return (await cmd('HDEL', `r:${userId}`, oppId)) > 0; },
    async claimDay(userId, date) {
      // SET ... GET returns the previous value atomically: if it already was today's date,
      // another run got there first.
      const prev = await cmd('SET', `d:${userId}`, date, 'EX', String(DAY_MARKER_TTL), 'GET');
      return prev !== date;
    },
    async releaseDay(userId, date) {
      if ((await cmd('GET', `d:${userId}`)) === date) await cmd('DEL', `d:${userId}`);
    },
    async logEvent(kind, ts = Date.now()) {
      await pipe([
        ['ZADD', `ev:${kind}`, String(ts), `${ts}-${Math.random().toString(36).slice(2, 10)}`],
        ['ZREMRANGEBYSCORE', `ev:${kind}`, '-inf', String(ts - EVENT_KEEP_MS)]
      ]);
    },
    async countEvents(kind, sinceTs) { return Number(await cmd('ZCOUNT', `ev:${kind}`, String(sinceTs), '+inf')) || 0; }
  };
}

let override = null;
let cached = null;

// Tests (and a future Postgres store) inject an implementation here.
export function setStore(store) { override = store; cached = null; }

export function getStore(env = process.env) {
  if (override) return override;
  if (cached) return cached;
  const url = env.UPSTASH_REDIS_REST_URL || env.KV_REST_API_URL;
  const token = env.UPSTASH_REDIS_REST_TOKEN || env.KV_REST_API_TOKEN;
  if (url && token) return (cached = upstashStore({ url, token }));
  if (env.BOT_STORE === 'memory') return (cached = memoryStore()); // local experiments only
  // Fail loudly: a silent in-memory fallback on Vercel would lose every reminder.
  throw new Error('Bot storage is not configured: set UPSTASH_REDIS_REST_URL/TOKEN (or KV_REST_API_URL/TOKEN)');
}
