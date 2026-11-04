// RFC 4180 CSV parser and writer. No dependencies.
// Handles: UTF-8 BOM, CRLF / LF / lone CR line endings, quoted fields with
// embedded commas, quotes ("" escape) and line breaks, missing trailing newline.

export function parseCsv(input) {
  let text = typeof input === 'string' ? input : String(input ?? '');
  if (text.charCodeAt(0) === 0xfeff) text = text.slice(1);

  const rows = [];
  let row = [];
  let field = '';
  let inQuotes = false;
  let atFieldStart = true;
  let dirty = false; // anything consumed for the current row
  const n = text.length;

  const endField = () => { row.push(field); field = ''; atFieldStart = true; };
  const endRow = () => { endField(); rows.push(row); row = []; dirty = false; };

  for (let i = 0; i < n; i++) {
    const c = text[i];
    if (inQuotes) {
      if (c === '"') {
        if (text[i + 1] === '"') { field += '"'; i++; } else inQuotes = false;
      } else field += c;
      continue;
    }
    if (c === '"' && atFieldStart) { inQuotes = true; atFieldStart = false; dirty = true; continue; }
    if (c === ',') { endField(); dirty = true; continue; }
    if (c === '\r') { if (text[i + 1] === '\n') i++; endRow(); continue; }
    if (c === '\n') { endRow(); continue; }
    field += c; atFieldStart = false; dirty = true;
  }
  if (dirty || inQuotes) endRow();
  return rows;
}

function escapeField(v) {
  const s = v == null ? '' : String(v);
  return /[",\r\n]/.test(s) || s !== s.trim() ? '"' + s.replace(/"/g, '""') + '"' : s;
}

export function toCsv(rows) {
  return rows.map(r => r.map(escapeField).join(',')).join('\r\n') + '\r\n';
}

// Turns parsed rows into objects keyed by lower-cased, trimmed header names.
// Each object carries its 1-based sheet row number in `_row` (header = row 1).
// Fully blank rows are skipped but still counted for numbering.
export function rowsToObjects(rows) {
  if (!rows.length) return { headers: [], items: [] };
  const headers = rows[0].map(h => String(h).trim().toLowerCase());
  const items = [];
  for (let r = 1; r < rows.length; r++) {
    const cells = rows[r];
    if (cells.every(c => String(c).trim() === '')) continue;
    const obj = { _row: r + 1 };
    headers.forEach((h, i) => { if (h && !(h in obj)) obj[h] = cells[i] ?? ''; });
    items.push(obj);
  }
  return { headers, items };
}
