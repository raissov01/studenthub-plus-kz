package admin

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/auth"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/db"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/features"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/httpx"
	"github.com/raissov01/studenthub-plus-kz/backend/internal/moderation"
)

// dashboard: GET /api/admin/dashboard — the super admin's overview. Every number is a count over real rows:
// accounts and sign-ups (users), active accounts per day (user_active_days, collected since the admin console
// shipped: "since" says from when), anonymous devices (analytics_events), live content, what waits for a
// decision, and today's handed-over café / print orders (Asia/Almaty day).
func dashboard(w http.ResponseWriter, r *http.Request, _ *auth.User) {
	ctx := r.Context()
	var out json.RawMessage
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, dashboardSQL, moderation.AdminOnlyKinds).Scan(&out)
	})
	if err != nil {
		fail(w, r, "dashboard", err)
		return
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		fail(w, r, "dashboard json", err)
		return
	}
	doc["features"] = features.All()
	httpx.JSON(w, http.StatusOK, doc)
}

const dashboardSQL = `
with today as (select (now() at time zone 'Asia/Almaty')::date as d),
t0 as (select (d::timestamp at time zone 'Asia/Almaty') as ts from today)
select jsonb_build_object(
	'users', jsonb_build_object(
		'total', (select count(*) from users where deleted_at is null),
		'accounts', (select count(*) from users where deleted_at is null and consent_at is not null),
		'new7', (select count(*) from users where deleted_at is null and consent_at > now() - interval '7 days'),
		'new30', (select count(*) from users where deleted_at is null and consent_at > now() - interval '30 days'),
		'blocked', (select count(*) from user_blocks),
		'moderators', (select count(*) from admin_grants where scope = 'moderator'),
		'byRole', coalesce((select jsonb_object_agg(role, n) from (select role::text, count(*) n from users where deleted_at is null group by role) x), '{}')
	),
	'signups', (select jsonb_agg(jsonb_build_object('day', to_char(g.day, 'YYYY-MM-DD'), 'n',
			(select count(*) from users u where u.deleted_at is null and (u.consent_at at time zone 'Asia/Almaty')::date = g.day)) order by g.day)
		from (select generate_series((select d from today) - 29, (select d from today), interval '1 day')::date as day) g),
	'active', jsonb_build_object(
		'dau', (select count(*) from user_active_days where day = (select d from today)),
		'wau', (select count(distinct user_id) from user_active_days where day > (select d from today) - 7),
		'mau', (select count(distinct user_id) from user_active_days where day > (select d from today) - 30),
		'since', (select to_char(min(day), 'YYYY-MM-DD') from user_active_days),
		'days', (select jsonb_agg(jsonb_build_object('day', to_char(g.day, 'YYYY-MM-DD'), 'n',
				(select count(*) from user_active_days a where a.day = g.day)) order by g.day)
			from (select generate_series((select d from today) - 13, (select d from today), interval '1 day')::date as day) g)
	),
	'devices', jsonb_build_object(
		'dau', (select count(distinct anon_id) from analytics_events where day = (select d from today)),
		'wau', (select count(distinct anon_id) from analytics_events where day > (select d from today) - 7)
	),
	'content', jsonb_build_object(
		'adminContent', (select count(*) from admin_content where status = 'published'),
		'community', (select count(*) from community_content where not hidden),
		'jobs', (select count(*) from jobs where status = 'published'),
		'events', (select count(*) from events where status = 'published' and starts_at > now()),
		'clubs', (select count(*) from clubs where status = 'active'),
		'market', (select count(*) from market_items where status in ('active', 'reserved') and expires_at > now()),
		'lostfound', (select count(*) from lostfound_posts where status = 'active'),
		'qa', (select count(*) from qa_questions where status = 'active'),
		'roommates', (select count(*) from roommate_listings where status = 'active'),
		'surveys', (select count(*) from surveys where status = 'published')
	),
	'pending', jsonb_build_object(
		'queue', (select count(*) from submissions where status = 'pending'),
		'queueModerator', (select count(*) from submissions where status = 'pending' and kind <> all($1)),
		'reports', (select count(distinct (target_kind, target_id)) from reports where not dismissed),
		'feedbackHeld', (select count(*) from feedback_tickets where status = 'held'),
		'feedbackOverdue', (select count(*) from feedback_tickets where status in ('new', 'in_progress') and sla_due_at < now()),
		'redemptions', (select count(*) from reward_redemptions where status = 'pending'),
		'employers', (select count(*) from employers where status = 'pending'),
		'cafeRefunds', (select count(*) from cafe_orders where refund_manual and payment_status = 'paid'),
		'printRefunds', (select count(*) from print_jobs where refund_manual and payment_status = 'paid')
	),
	'shops', jsonb_build_object(
		'cafe', (select jsonb_build_object('orders', count(*) filter (where created_at >= (select ts from t0)),
				'done', count(*) filter (where status = 'picked_up' and closed_at >= (select ts from t0)),
				'revenue', coalesce(sum(coalesce(paid_amount, total)) filter (where status = 'picked_up' and payment_status <> 'refunded'
					and closed_at >= (select ts from t0)), 0))
			from cafe_orders where created_at >= (select ts from t0) or closed_at >= (select ts from t0)),
		'print', (select jsonb_build_object('orders', count(*) filter (where created_at >= (select ts from t0)),
				'done', count(*) filter (where status = 'picked_up' and closed_at >= (select ts from t0)),
				'revenue', coalesce(sum(coalesce(paid_amount, total)) filter (where status = 'picked_up' and payment_status <> 'refunded'
					and closed_at >= (select ts from t0)), 0))
			from print_jobs where created_at >= (select ts from t0) or closed_at >= (select ts from t0))
	)
)`
