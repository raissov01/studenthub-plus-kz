import { test, describe, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseCsv, toCsv, rowsToObjects } from '../lib/content/csv.js';
import { COLUMNS, KINDS, buildRecords, filterActive, almatyToday, recordToRow } from '../lib/content/schema.js';
import { loadContent, _resetMemory } from '../lib/content/load.js';
import handler from '../api/content.js';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const readJson = async kind => JSON.parse(await readFile(path.join(root, 'data', `${kind}.json`), 'utf8'));
const readTemplate = kind => readFile(path.join(root, 'templates', 'sheets', `${kind}.csv`), 'utf8');
const silent = () => {};

// ---------- CSV parser ----------

describe('parseCsv', () => {
  test('plain rows, LF and CRLF, no trailing newline', () => {
    assert.deepEqual(parseCsv('a,b\r\nc,d\ne,f'), [['a', 'b'], ['c', 'd'], ['e', 'f']]);
  });
  test('trailing newline does not add an empty row', () => {
    assert.deepEqual(parseCsv('a,b\r\n'), [['a', 'b']]);
  });
  test('quoted fields with commas, newlines and "" escapes', () => {
    const csv = 'id,text\r\n1,"hello, ""world""\r\nline2"\r\n2,"x\ny"\r\n';
    assert.deepEqual(parseCsv(csv), [['id', 'text'], ['1', 'hello, "world"\r\nline2'], ['2', 'x\ny']]);
  });
  test('BOM is stripped', () => {
    assert.deepEqual(parseCsv('﻿id,x\n1,2'), [['id', 'x'], ['1', '2']]);
  });
  test('empty fields and empty quoted fields', () => {
    assert.deepEqual(parseCsv(',,\n"",a,""'), [['', '', ''], ['', 'a', '']]);
  });
  test('lone CR line endings', () => {
    assert.deepEqual(parseCsv('a\rb'), [['a'], ['b']]);
  });
  test('unterminated quote keeps the rest as one field', () => {
    assert.deepEqual(parseCsv('a,"b,c\nd'), [['a', 'b,c\nd']]);
  });
  test('toCsv -> parseCsv round-trips awkward values', () => {
    const rows = [['a', 'b, c', 'say "hi"', 'x\ny', ' pad ', '', 'қазақ | тілі']];
    assert.deepEqual(parseCsv(toCsv(rows)), rows);
  });
  test('rowsToObjects lower-cases headers, skips blank rows, keeps sheet row numbers', () => {
    const { headers, items } = rowsToObjects(parseCsv(' ID ,Status\n1,published\n,\n2,draft'));
    assert.deepEqual(headers, ['id', 'status']);
    assert.deepEqual(items.map(i => [i._row, i.id]), [[2, '1'], [4, '2']]);
  });
});

// ---------- validation ----------

function row(kind, over = {}) {
  const base = {
    opportunities: {
      id: 'x1', status: 'published', type: 'grant', title_kk: 'Грант', source_url: 'https://example.com',
      city: 'astana', deadline: '2099-01-01'
    },
    jobs: {
      id: 'x1', status: 'published', title_kk: 'Жұмыс', city: 'astana', pay: '8 000', per: 'shift', hours: '4',
      source_url: 'https://example.com', no_deadline: 'TRUE'
    },
    discounts: {
      id: 'x1', status: 'published', cat: 'food', pct: '−10%', name_kk: 'Кафе', city: 'astana',
      source_url: 'https://example.com', no_deadline: 'TRUE'
    }
  }[kind];
  return { _row: 2, ...base, ...over };
}

function check(kind, rows) {
  const warnings = [];
  const out = buildRecords(kind, rows, { warn: m => warnings.push(m) });
  return { ...out, warnings };
}

describe('validation', () => {
  test('minimal valid rows pass for every kind', () => {
    for (const kind of KINDS) {
      const r = check(kind, [row(kind)]);
      assert.equal(r.records.length, 1, kind + ': ' + r.warnings.join('; '));
      assert.equal(r.rejected, 0);
    }
  });
  test('ru falls back to kk, numbers with spaces/commas are parsed', () => {
    const { records } = check('jobs', [row('jobs', { pay: '12 500', hours: '4,5' })]);
    assert.deepEqual(records[0].title, { kk: 'Жұмыс', ru: 'Жұмыс' });
    assert.equal(records[0].pay, 12500);
    assert.equal(records[0].hours, 4.5);
  });
  const bad = [
    ['missing id', 'opportunities', { id: '' }, /"id"/],
    ['missing title_kk', 'opportunities', { title_kk: '' }, /title_kk/],
    ['unknown type', 'opportunities', { type: 'party' }, /type/],
    ['missing source_url', 'opportunities', { source_url: '' }, /source_url/],
    ['http:// link', 'opportunities', { source_url: 'http://example.com' }, /https:\/\//],
    ['http:// contact site', 'discounts', { contact_site: 'http://shop.kz' }, /contact_site/],
    ['bad date', 'opportunities', { deadline: '2026-02-30' }, /deadline/],
    ['bad date format', 'opportunities', { posted_at: '2026/01/05' }, /posted_at/],
    ['no deadline and no no_deadline', 'opportunities', { deadline: '' }, /no_deadline/],
    ['both deadline and no_deadline', 'opportunities', { no_deadline: 'TRUE' }, /біреуін/],
    ['job without pay', 'jobs', { pay: '' }, /pay/],
    ['job with unknown per', 'jobs', { per: 'week' }, /per/],
    ['discount without cat', 'discounts', { cat: '' }, /cat/],
    ['unknown city', 'discounts', { city: 'london' }, /city/],
    ['unknown status', 'discounts', { status: 'publshed' }, /status/]
  ];
  for (const [name, kind, over, re] of bad) {
    test(`rejects: ${name}`, () => {
      const r = check(kind, [row(kind, over), row(kind, { id: 'ok', _row: 3 })]);
      assert.equal(r.rejected, 1);
      assert.deepEqual(r.records.map(x => x.id), ['ok'], 'one bad row must not affect the others');
      assert.equal(r.warnings.length, 1);
      assert.match(r.warnings[0], new RegExp(`${kind}: 2-жол`));
      assert.match(r.warnings[0], re);
    });
  }
  test('duplicate id: first kept, second rejected', () => {
    const r = check('jobs', [row('jobs'), row('jobs', { _row: 3, title_kk: 'Басқа' })]);
    assert.equal(r.records.length, 1);
    assert.equal(r.records[0].title.kk, 'Жұмыс');
    assert.equal(r.rejected, 1);
    assert.match(r.warnings[0], /3-жол.*қайталанады/);
  });
  test('draft, hidden and empty status are skipped silently (not rejected)', () => {
    const r = check('discounts', [
      row('discounts', { id: 'a', status: 'draft', name_kk: '' }),
      row('discounts', { id: 'b', status: 'Hidden' }),
      row('discounts', { id: 'c', status: '' }),
      row('discounts', { id: 'd', status: 'PUBLISHED' })
    ]);
    assert.deepEqual(r.records.map(x => x.id), ['d']);
    assert.equal(r.rejected, 0);
  });
  test('no_deadline gives deadline null; DD.MM.YYYY is normalised', () => {
    assert.equal(check('jobs', [row('jobs')]).records[0].deadline, null);
    assert.equal(check('opportunities', [row('opportunities', { deadline: '6.12.2026' })]).records[0].deadline, '2026-12-06');
  });
  test('expired deadline is dropped, today and no-deadline stay', () => {
    const recs = [{ id: 'old', deadline: '2026-10-27' }, { id: 'today', deadline: '2026-10-28' }, { id: 'none', deadline: null }];
    assert.deepEqual(filterActive(recs, '2026-10-28').map(r => r.id), ['today', 'none']);
  });
  test('Almaty date is UTC+5', () => {
    assert.equal(almatyToday(Date.parse('2026-10-28T18:59:00Z')), '2026-10-28');
    assert.equal(almatyToday(Date.parse('2026-10-28T19:00:00Z')), '2026-10-29');
  });
});

// ---------- templates round-trip ----------

describe('CSV templates', () => {
  for (const kind of KINDS) {
    test(`${kind}.csv round-trips to data/${kind}.json`, async () => {
      const data = await readJson(kind);
      const csv = await readTemplate(kind);
      const { headers, items } = rowsToObjects(parseCsv(csv));
      assert.deepEqual(headers, COLUMNS[kind]);
      const warnings = [];
      const { records, rejected } = buildRecords(kind, items, { warn: m => warnings.push(m) });
      assert.deepEqual(warnings, []);
      assert.equal(rejected, 0);
      assert.deepStrictEqual(records, data);
    });
    test(`${kind}.csv is in sync with scripts/json-to-csv.mjs`, async () => {
      const data = await readJson(kind);
      assert.equal(await readTemplate(kind), toCsv([COLUMNS[kind], ...data.map(r => recordToRow(kind, r))]));
    });
  }
});

// ---------- fallback chain ----------

const URLS = {
  SHEET_OPPORTUNITIES_CSV: 'https://docs.google.com/o.csv',
  SHEET_JOBS_CSV: 'https://docs.google.com/j.csv',
  SHEET_DISCOUNTS_CSV: 'https://docs.google.com/d.csv'
};
const NOW = Date.parse('2026-10-28T06:00:00Z');

function sheetCsv(kind, rows) {
  const cols = COLUMNS[kind];
  return toCsv([cols, ...rows.map(r => cols.map(c => r[c] ?? ''))]);
}

function okFetch(byUrl) {
  return async url => {
    if (!(url in byUrl)) throw new Error('unexpected url ' + url);
    return new Response(byUrl[url], { status: 200, headers: { 'content-type': 'text/csv' } });
  };
}
const downFetch = async () => { throw new TypeError('fetch failed'); };

function sheetsData() {
  return {
    [URLS.SHEET_OPPORTUNITIES_CSV]: sheetCsv('opportunities', [
      row('opportunities', { id: 'so1' }),
      row('opportunities', { id: 'so2', deadline: '2026-10-01' }), // expired
      row('opportunities', { id: 'bad', source_url: 'http://x.kz' }) // rejected
    ]),
    [URLS.SHEET_JOBS_CSV]: sheetCsv('jobs', [row('jobs', { id: 'sj1' })]),
    [URLS.SHEET_DISCOUNTS_CSV]: sheetCsv('discounts', [row('discounts', { id: 'sd1' }), row('discounts', { id: 'sd2', status: 'draft' })])
  };
}

describe('loadContent fallback chain', () => {
  beforeEach(() => _resetMemory());

  test('sheets OK -> source sheets, memory then used when sheets go down', async () => {
    const a = await loadContent({ env: URLS, fetchImpl: okFetch(sheetsData()), now: NOW, warn: silent });
    assert.equal(a.source, 'sheets');
    assert.deepEqual(a.opportunities.map(r => r.id), ['so1']);
    assert.deepEqual(a.jobs.map(r => r.id), ['sj1']);
    assert.deepEqual(a.discounts.map(r => r.id), ['sd1']);
    assert.equal(a.rejected, 1);
    assert.match(a.updated_at, /^\d{4}-\d{2}-\d{2}T/);

    const warnings = [];
    const b = await loadContent({ env: URLS, fetchImpl: downFetch, now: NOW, warn: m => warnings.push(m) });
    assert.equal(b.source, 'memory');
    assert.deepEqual(b.opportunities.map(r => r.id), ['so1']);
    assert.deepEqual(b.jobs.map(r => r.id), ['sj1']);
    assert.equal(b.rejected, 1);
    assert.equal(b.updated_at, a.updated_at);
    assert.equal(warnings.length, 3);
  });

  test('cold start with sheets down -> data/*.json, deadline-filtered', async () => {
    const r = await loadContent({ env: URLS, fetchImpl: downFetch, now: NOW, warn: silent });
    assert.equal(r.source, 'fallback');
    assert.equal(r.updated_at, null);
    assert.equal(r.rejected, 0);
    for (const kind of KINDS) assert.deepEqual(r[kind], filterActive(await readJson(kind), '2026-10-28'));
    assert.equal(r.jobs.length, (await readJson('jobs')).length);
    // Far in the future every dated opportunity has expired; undated jobs/discounts remain.
    const later = await loadContent({ env: URLS, fetchImpl: downFetch, now: Date.parse('2030-01-01'), warn: silent });
    assert.deepEqual(later.opportunities, []);
    assert.equal(later.discounts.length, (await readJson('discounts')).length);
  });

  test('unset env -> fallback without calling fetch', async () => {
    const r = await loadContent({ env: {}, fetchImpl: () => assert.fail('fetch called'), now: NOW, warn: silent });
    assert.equal(r.source, 'fallback');
    assert.equal(r.opportunities.length, (await readJson('opportunities')).length);
  });

  test('mixed: one sheet configured, others fall back', async () => {
    const env = { SHEET_JOBS_CSV: URLS.SHEET_JOBS_CSV };
    const r = await loadContent({ env, fetchImpl: okFetch(sheetsData()), now: NOW, warn: silent });
    assert.equal(r.source, 'fallback');
    assert.deepEqual(r.jobs.map(x => x.id), ['sj1']);
    assert.equal(r.discounts.length, (await readJson('discounts')).length);
  });

  test('HTML page, HTTP error and timeout count as failures', async () => {
    const html = async () => new Response('<!doctype html><html>login</html>', { status: 200, headers: { 'content-type': 'text/html' } });
    const http500 = async () => new Response('err', { status: 500 });
    for (const f of [html, http500]) {
      _resetMemory();
      const r = await loadContent({ env: URLS, fetchImpl: f, now: NOW, warn: silent });
      assert.equal(r.source, 'fallback');
    }
  });

  test('timeout aborts after ~5s and falls back', { timeout: 10000 }, async () => {
    const hang = (url, { signal }) => new Promise((_, reject) =>
      signal.addEventListener('abort', () => reject(Object.assign(new Error('aborted'), { name: 'AbortError' }))));
    const t0 = Date.now();
    const warnings = [];
    const r = await loadContent({ env: { SHEET_JOBS_CSV: URLS.SHEET_JOBS_CSV }, fetchImpl: hang, now: NOW, warn: m => warnings.push(m) });
    assert.ok(Date.now() - t0 >= 4900);
    assert.equal(r.source, 'fallback');
    assert.match(warnings.join('\n'), /5 с ішінде/);
  });
});

// ---------- HTTP handler ----------

describe('api/content handler', () => {
  function mockRes() {
    const res = { headers: {}, statusCode: 0, body: undefined };
    res.setHeader = (k, v) => { res.headers[k.toLowerCase()] = v; };
    res.status = c => { res.statusCode = c; return res; };
    res.json = b => { res.body = b; return res; };
    return res;
  }
  test('GET returns content with cache header', async () => {
    _resetMemory();
    const saved = {};
    for (const k of Object.keys(URLS)) { saved[k] = process.env[k]; delete process.env[k]; }
    const origWarn = console.warn; console.warn = silent;
    try {
      const res = mockRes();
      await handler({ method: 'GET', headers: {} }, res);
      assert.equal(res.statusCode, 200);
      assert.equal(res.headers['cache-control'], 'public, s-maxage=600, stale-while-revalidate=86400');
      assert.deepEqual(Object.keys(res.body).sort(), ['discounts', 'jobs', 'opportunities', 'rejected', 'source', 'updated_at']);
      assert.equal(res.body.source, 'fallback');
    } finally {
      console.warn = origWarn;
      for (const [k, v] of Object.entries(saved)) if (v !== undefined) process.env[k] = v;
    }
  });
  test('POST is rejected with 405', async () => {
    const res = mockRes();
    await handler({ method: 'POST', headers: {} }, res);
    assert.equal(res.statusCode, 405);
  });
});
