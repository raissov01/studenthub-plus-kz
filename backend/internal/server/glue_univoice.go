package server

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/points"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/surveys"
)

// A completed survey pays once (points' unique ref), capped per day. The ref is an opaque
// per-(survey, student) HMAC, so the points ledger shows that a student finished *a* survey,
// never which one; survey responses carry no timestamp to correlate with the ledger row.
func init() {
	surveys.OnComplete = func(ctx context.Context, tx pgx.Tx, userID int64, ref string) any {
		return grant(ctx, tx, points.Earn{UserID: userID, Reason: points.ReasonSurvey, RefType: "survey", RefID: ref})
	}
}
