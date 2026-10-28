package events

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/content"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
	"github.com/raissov01/studenthub-plus-kz/pkg/features"
)

func init() { content.RegisterExtra("events", contentEvents) }

// Only public event fields enter the content feed: never check-in secrets or registrations.
func contentEvents(ctx context.Context) (map[string][]content.Record, error) {
	if !features.Enabled("events") {
		return nil, nil
	}
	out := []content.Record{}
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select jsonb_build_object('id', id, 'title_kk', title_kk, 'title_ru', title_ru,
    'desc_kk', desc_kk, 'desc_ru', desc_ru, 'cat', cat, 'starts_at', starts_at, 'ends_at', ends_at,
    'university', university, 'place_text', place_text, 'building_id', building_id, 'is_demo', is_demo)
   from events where status='published' and ends_at > now() order by starts_at, id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw json.RawMessage
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			out = append(out, content.RawRecord(raw, ""))
		}
		return rows.Err()
	})
	return map[string][]content.Record{"events": out}, err
}
