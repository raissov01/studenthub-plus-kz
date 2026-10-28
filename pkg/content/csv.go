package content

import (
	"strings"

	"github.com/raissov01/studenthub-plus-kz/pkg/jsx"
)

// ParseCSV is an RFC 4180 parser. It handles a UTF-8 BOM, CRLF / LF / lone CR line
// endings, quoted fields with embedded commas, quotes ("" escape) and line breaks, and a
// missing trailing newline. An unterminated quote keeps the rest of the input as one field.
func ParseCSV(text string) [][]string {
	text = strings.TrimPrefix(text, "\uFEFF")
	var (
		rows         [][]string
		row          []string
		field        strings.Builder
		inQuotes     bool
		atFieldStart = true
		dirty        bool // anything consumed for the current row
	)
	endField := func() { row = append(row, field.String()); field.Reset(); atFieldStart = true }
	endRow := func() { endField(); rows = append(rows, row); row = nil; dirty = false }

	n := len(text)
	for i := 0; i < n; i++ {
		c := text[i]
		if inQuotes {
			if c == '"' {
				if i+1 < n && text[i+1] == '"' {
					field.WriteByte('"')
					i++
				} else {
					inQuotes = false
				}
			} else {
				field.WriteByte(c)
			}
			continue
		}
		switch {
		case c == '"' && atFieldStart:
			inQuotes, atFieldStart, dirty = true, false, true
		case c == ',':
			endField()
			dirty = true
		case c == '\r':
			if i+1 < n && text[i+1] == '\n' {
				i++
			}
			endRow()
		case c == '\n':
			endRow()
		default:
			field.WriteByte(c)
			atFieldStart, dirty = false, true
		}
	}
	if dirty || inQuotes {
		endRow()
	}
	return rows
}

func escapeField(s string) string {
	if strings.ContainsAny(s, "\",\r\n") || s != jsx.Trim(s) {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

// ToCSV writes rows with CRLF line endings and a trailing CRLF.
func ToCSV(rows [][]string) string {
	var b strings.Builder
	for i, r := range rows {
		if i > 0 {
			b.WriteString("\r\n")
		}
		for j, v := range r {
			if j > 0 {
				b.WriteByte(',')
			}
			b.WriteString(escapeField(v))
		}
	}
	b.WriteString("\r\n")
	return b.String()
}

// Item is one sheet row keyed by lower-cased, trimmed header names, with its 1-based
// sheet row number (header = row 1).
type Item struct {
	Row   int
	Cells map[string]string
}

// Header names a JS object already "has" (own _row key, Object.prototype members), which
// rowsToObjects therefore never assigned.
var reservedHeaders = map[string]bool{"_row": true, "constructor": true, "__proto__": true}

// RowsToObjects turns parsed rows into items. Fully blank rows are skipped but still
// counted for numbering; for duplicate headers the first column wins.
func RowsToObjects(rows [][]string) (headers []string, items []Item) {
	if len(rows) == 0 {
		return []string{}, nil
	}
	headers = make([]string, len(rows[0]))
	for i, h := range rows[0] {
		headers[i] = jsx.Lower(jsx.Trim(h))
	}
	for r := 1; r < len(rows); r++ {
		cells := rows[r]
		blank := true
		for _, c := range cells {
			if jsx.Trim(c) != "" {
				blank = false
				break
			}
		}
		if blank {
			continue
		}
		obj := Item{Row: r + 1, Cells: map[string]string{}}
		for i, h := range headers {
			if _, dup := obj.Cells[h]; h == "" || dup || reservedHeaders[h] {
				continue
			}
			if i < len(cells) {
				obj.Cells[h] = cells[i]
			} else {
				obj.Cells[h] = ""
			}
		}
		items = append(items, obj)
	}
	return headers, items
}
