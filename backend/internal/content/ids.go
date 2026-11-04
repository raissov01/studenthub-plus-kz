package content

import "encoding/json"

// RecordID returns the id of a loaded record, whether it came from a sheet row or from a
// data/*.json (or extra source) entry served as is.
func RecordID(r Record) string {
	if id := recordID(r); id != "" {
		return id
	}
	if raw, ok := r.(rawRecord); ok {
		var v struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw.raw, &v) == nil {
			return v.ID
		}
	}
	return ""
}
