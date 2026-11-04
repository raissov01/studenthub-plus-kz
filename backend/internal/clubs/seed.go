package clubs

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
)

// seed/kazatu.json: clubs verified on official kazatu.edu.kz pages (docs/clubs-seed.md lists the
// sources and what could not be verified). No leaders' names or phones, no demo records.
//
//go:embed seed/kazatu.json
var seedJSON []byte

type seedClub struct {
	SeedKey       string  `json:"seed_key"`
	NameKK        string  `json:"name_kk"`
	NameRU        *string `json:"name_ru"`
	Direction     string  `json:"direction"`
	SummaryKK     *string `json:"summary_kk"`
	SummaryRU     *string `json:"summary_ru"`
	LeaderTitleKK *string `json:"leader_title_kk"`
	LeaderTitleRU *string `json:"leader_title_ru"`
	InstagramURL  *string `json:"instagram_url"`
	TelegramURL   *string `json:"telegram_url"`
	SourceURL     string  `json:"source_url"`
	VerifiedAt    string  `json:"verified_at"`
}

// SeedRecords parses the embedded seed file.
func SeedRecords() ([]seedClub, error) {
	var out []seedClub
	err := json.Unmarshal(seedJSON, &out)
	return out, err
}

// Seed imports the official KazATU clubs once: rows are keyed by seed_key, so running it again
// adds only new records and never overwrites what admins edited. cmd/server calls it at start
// when CLUBS_SEED=1. Returns how many clubs were added.
func Seed(ctx context.Context) (int, error) {
	recs, err := SeedRecords()
	if err != nil {
		return 0, err
	}
	added := 0
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		for _, c := range recs {
			if !contains(Directions, c.Direction) {
				return fmt.Errorf("seed %s: direction %q", c.SeedKey, c.Direction)
			}
			tag, err := tx.Exec(ctx, `insert into clubs (seed_key, name_kk, name_ru, direction, summary_kk, summary_ru,
					leader_title_kk, leader_title_ru, instagram_url, telegram_url, source_url, verified_at, university, join_mode)
				values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::date, $13, 'request')
				on conflict (seed_key) do nothing`,
				c.SeedKey, c.NameKK, c.NameRU, c.Direction, c.SummaryKK, c.SummaryRU, c.LeaderTitleKK, c.LeaderTitleRU,
				c.InstagramURL, c.TelegramURL, c.SourceURL, c.VerifiedAt, University())
			if err != nil {
				return fmt.Errorf("seed %s: %w", c.SeedKey, err)
			}
			added += int(tag.RowsAffected())
		}
		return nil
	})
	return added, err
}
