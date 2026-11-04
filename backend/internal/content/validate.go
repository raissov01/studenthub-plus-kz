package content

import (
	"maps"
	"regexp"
	"strings"
)

// Validation of one record in the data/*.json shape (the admin content editor, docs/api/admin-content.md).
// It runs the very same rules as a Sheets row: the record is turned into a template row with
// RecordToRow and built by the list's builder, so the two paths cannot drift apart.

// AdminIDPrefix marks records managed in the admin panel ("adm-<id>"). Sheets rows may not use it.
const AdminIDPrefix = "adm-"

// reservedIDPrefixes are taken by database-backed sources; a sheet row with such an id is rejected.
var reservedIDPrefixes = []string{AdminIDPrefix}

// FieldError is the first broken field of a record. Field is the sheet column name
// (title_kk, contact_email, deadline, ...); Message is the Sheets message (kk).
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *FieldError) Error() string { return e.Message }

var firstQuoted = regexp.MustCompile(`"([a-z_]+)"`)

func fieldOf(msg string) string {
	if m := firstQuoted.FindStringSubmatch(msg); m != nil {
		return m[1]
	}
	return ""
}

// ValidateRecord validates one record of kind (opportunities, jobs, discounts) given as a
// decoded data/*.json entry. The record's own id and is_demo are ignored (the caller assigns
// the id; DB records are never demo). noDeadline is the explicit "мерзімсіз" switch: a record
// needs exactly one of deadline / noDeadline, as a sheet row does. The returned Record is one of
// *Opportunity, *Job, *Discount with ID "".
func ValidateRecord(kind string, rec map[string]any, noDeadline bool) (Record, *FieldError) {
	build, ok := builders[kind]
	if !ok {
		return nil, &FieldError{Field: "kind", Message: `"kind" мәні белгісіз (opportunities / jobs / discounts)`}
	}
	r := maps.Clone(rec)
	if r == nil {
		r = map[string]any{}
	}
	r["id"] = "new"
	r["is_demo"] = false
	row, err := RecordToRow(kind, r)
	if err != nil {
		// the only RecordToRow error: "|" inside a list item (it is the sheet list separator)
		return nil, &FieldError{Field: "list", Message: `тізім элементінде "|" таңбасы болмауы керек (` + strings.TrimPrefix(err.Error(), `"|" is the list separator and cannot appear inside an item: `) + `)`}
	}
	cells := map[string]string{}
	for i, col := range Columns[kind] {
		cells[col] = row[i]
	}
	cells["no_deadline"] = ""
	if noDeadline {
		cells["no_deadline"] = "TRUE"
	}
	built, msg := buildOne(build, Item{Row: 1, Cells: cells}, map[string]bool{})
	if built == nil {
		return nil, &FieldError{Field: fieldOf(msg), Message: msg}
	}
	setRecordID(built, "")
	return built, nil
}

// SetRecordID sets the id of a validated record.
func SetRecordID(r Record, id string) { setRecordID(r, id) }

func setRecordID(r Record, id string) {
	switch x := r.(type) {
	case *Opportunity:
		x.ID = id
	case *Job:
		x.ID = id
	case *Discount:
		x.ID = id
	}
}

// NewRecord returns an empty typed record of kind (for decoding stored records).
func NewRecord(kind string) Record {
	switch kind {
	case "opportunities":
		return &Opportunity{}
	case "jobs":
		return &Job{}
	case "discounts":
		return &Discount{}
	}
	return nil
}

func reservedID(id string) bool {
	for _, p := range reservedIDPrefixes {
		if strings.HasPrefix(strings.ToLower(id), p) {
			return true
		}
	}
	return false
}

// dedupe keeps the first record of every id (sheet / data/*.json records come first, so a
// database record can never replace or shadow them).
func (l *Loader) dedupe(kind string, recs []Record) []Record {
	seen := make(map[string]bool, len(recs))
	out := make([]Record, 0, len(recs))
	for _, r := range recs {
		id := RecordID(r)
		if id != "" && seen[id] {
			l.warn(`[content] ` + kind + `: id "` + id + `" қайталанады — екінші жазба алынып тасталды`)
			continue
		}
		seen[id] = true
		out = append(out, r)
	}
	return out
}
