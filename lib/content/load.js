// Content loader: Google Sheets (published CSV) -> last good copy in memory -> bundled data/*.json.
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import { parseCsv, rowsToObjects } from './csv.js';
import { KINDS, buildRecords, filterActive, almatyToday } from './schema.js';

export const ENV_KEYS = {
  opportunities: 'SHEET_OPPORTUNITIES_CSV',
  jobs: 'SHEET_JOBS_CSV',
  discounts: 'SHEET_DISCOUNTS_CSV'
};
const TIMEOUT_MS = 5000;
const MAX_BYTES = 5 * 1024 * 1024;

// Module memory survives between requests while the function instance is warm.
const memory = new Map();   // kind -> { records, rejected, at }
const bundled = new Map();  // kind -> records from data/*.json

export function _resetMemory() { memory.clear(); bundled.clear(); }

async function fetchSheet(kind, url, fetchImpl, warn) {
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), TIMEOUT_MS);
  try {
    const res = await fetchImpl(url, { signal: ctrl.signal, redirect: 'follow', headers: { accept: 'text/csv' } });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const text = await res.text();
    if (text.length > MAX_BYTES) throw new Error('кесте тым үлкен');
    const ctype = String(res.headers?.get?.('content-type') || '');
    if (/text\/html/i.test(ctype) || /^\s*</.test(text)) {
      throw new Error('CSV емес, HTML келді (кесте "Publish to web → CSV" арқылы жарияланбаған болуы мүмкін)');
    }
    const { headers, items } = rowsToObjects(parseCsv(text));
    if (!headers.includes('id') || !headers.includes('status')) {
      throw new Error('тақырып жолында "id" не "status" бағаны жоқ — сілтеме дұрыс парақты көрсете ме?');
    }
    return buildRecords(kind, items, { warn });
  } finally {
    clearTimeout(timer);
  }
}

async function readBundled(kind, warn) {
  if (bundled.has(kind)) return bundled.get(kind);
  try {
    const file = path.join(process.cwd(), 'data', `${kind}.json`);
    const data = JSON.parse(await readFile(file, 'utf8'));
    if (!Array.isArray(data)) throw new Error('массив емес');
    bundled.set(kind, data);
    return data;
  } catch (e) {
    warn(`[content] ${kind}: data/${kind}.json оқылмады: ${e.message}`);
    return [];
  }
}

async function loadKind(kind, { env, fetchImpl, warn }) {
  const url = String(env[ENV_KEYS[kind]] || '').trim();
  if (url) {
    if (!/^https:\/\//i.test(url)) {
      warn(`[content] ${kind}: ${ENV_KEYS[kind]} https:// деп басталмайды — data/${kind}.json қолданылады`);
    } else {
      try {
        const { records, rejected } = await fetchSheet(kind, url, fetchImpl, warn);
        const entry = { records, rejected, at: new Date().toISOString() };
        memory.set(kind, entry);
        return { ...entry, source: 'sheets' };
      } catch (e) {
        const reason = e.name === 'AbortError' ? `${TIMEOUT_MS / 1000} с ішінде жауап бермеді` : e.message;
        const mem = memory.get(kind);
        warn(`[content] ${kind}: Google Sheets оқылмады (${reason}) — ${mem ? 'жадтағы соңғы нұсқа' : `data/${kind}.json`} қолданылады`);
        if (mem) return { ...mem, source: 'memory' };
      }
    }
  }
  return { records: await readBundled(kind, warn), rejected: 0, at: null, source: 'fallback' };
}

const RANK = { sheets: 0, memory: 1, fallback: 2 };

// Returns { opportunities, jobs, discounts, source, updated_at, rejected }.
// source = the "worst" source among the three lists (sheets < memory < fallback).
// updated_at = the oldest Sheets fetch time among lists that came from Sheets/memory, or null.
export async function loadContent({ env = process.env, fetchImpl = globalThis.fetch, now = Date.now(), warn = console.warn } = {}) {
  const today = almatyToday(now);
  const parts = await Promise.all(KINDS.map(kind =>
    loadKind(kind, { env, fetchImpl, warn }).catch(e => {
      warn(`[content] ${kind}: күтпеген қате: ${e.message}`);
      return { records: [], rejected: 0, at: null, source: 'fallback' };
    })
  ));
  const out = {};
  let source = 'sheets';
  let updatedAt = null;
  let rejected = 0;
  KINDS.forEach((kind, i) => {
    const p = parts[i];
    out[kind] = filterActive(p.records, today);
    if (RANK[p.source] > RANK[source]) source = p.source;
    if (p.at && (!updatedAt || p.at < updatedAt)) updatedAt = p.at;
    rejected += p.rejected;
  });
  return { ...out, source, updated_at: updatedAt, rejected };
}
