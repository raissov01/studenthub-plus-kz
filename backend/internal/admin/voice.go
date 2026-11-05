package admin

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
)

// voice: GET /api/admin/voice — University voice across universities for the super admin (/admin/voice):
// per university the surveys by status and respondents, the feedback inbox by status (held = flagged, waiting
// for an admin), overdue tickets, recipients without staff, and wellbeing contacts; plus the latest surveys.
// Counts only: ticket texts and survey answers stay behind /voice and the aggregate results.
func voice(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	ctx := r.Context()
	var out json.RawMessage
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `with univ as (
				select university from surveys union select university from feedback_recipients
				union select university from wellbeing_contacts union select university from university_staff)
			select jsonb_build_object(
				'universities', coalesce((select jsonb_agg(jsonb_build_object(
					'university', u.university,
					'surveys', jsonb_build_object(
						'draft', (select count(*) from surveys s where s.university = u.university and s.status = 'draft'),
						'published', (select count(*) from surveys s where s.university = u.university and s.status = 'published'),
						'closed', (select count(*) from surveys s where s.university = u.university and s.status = 'closed'),
						'respondents', (select count(*) from survey_markers m join surveys s on s.id = m.survey_id where s.university = u.university)),
					'feedback', jsonb_build_object(
						'held', (select count(*) from feedback_tickets t join feedback_recipients fr on fr.id = t.recipient_id where fr.university = u.university and t.status = 'held'),
						'open', (select count(*) from feedback_tickets t join feedback_recipients fr on fr.id = t.recipient_id where fr.university = u.university and t.status in ('new', 'in_progress')),
						'overdue', (select count(*) from feedback_tickets t join feedback_recipients fr on fr.id = t.recipient_id where fr.university = u.university and t.status in ('new', 'in_progress') and t.sla_due_at < now()),
						'answered', (select count(*) from feedback_tickets t join feedback_recipients fr on fr.id = t.recipient_id where fr.university = u.university and t.status in ('answered', 'closed')),
						'recipients', (select count(*) from feedback_recipients fr where fr.university = u.university and fr.active),
						'unstaffed', (select count(*) from feedback_recipients fr where fr.university = u.university and fr.active
							and not exists (select 1 from feedback_recipient_staff s where s.recipient_id = fr.id))),
					'wellbeing', (select count(*) from wellbeing_contacts c where c.university = u.university),
					'staff', (select count(*) from university_staff s join users x on x.id = s.user_id where s.university = u.university and x.deleted_at is null)
				) order by u.university) from univ u), '[]'),
				'surveys', coalesce((select jsonb_agg(x order by (x->>'id')::bigint desc) from (
					select jsonb_build_object('id', s.id, 'university', s.university, 'title', s.title, 'status', s.status, 'anonymous', s.anonymous,
						'respondents', (select count(*) from survey_markers m where m.survey_id = s.id), 'createdAt', s.created_at,
						'closesAt', s.closes_at) as x
					from surveys s order by s.id desc limit 50) q), '[]')
			)`).Scan(&out)
	})
	if err != nil {
		fail(w, r, "voice", err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}
