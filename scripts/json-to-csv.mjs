// Regenerates templates/sheets/*.csv from data/*.json (one CSV per Google Sheets tab).
// Usage: node scripts/json-to-csv.mjs
import { readFile, writeFile, mkdir } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { toCsv } from '../lib/content/csv.js';
import { KINDS, COLUMNS, recordToRow } from '../lib/content/schema.js';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const outDir = path.join(root, 'templates', 'sheets');
await mkdir(outDir, { recursive: true });

for (const kind of KINDS) {
  const records = JSON.parse(await readFile(path.join(root, 'data', `${kind}.json`), 'utf8'));
  const rows = [COLUMNS[kind], ...records.map(r => recordToRow(kind, r))];
  const file = path.join(outDir, `${kind}.csv`);
  await writeFile(file, toCsv(rows));
  console.log(`${path.relative(root, file)}: ${records.length} rows`);
}
