# Codex task: prompt 11 (launch) — SERVER SIDE ONLY

Repo: this git worktree (branch `agent-launch`, from `platform`). Go 1.25 + pgx v5 API in `pkg/`, Next.js frontend in `app/`,
`components/`, `lib/` (DO NOT touch the frontend, `legacy/`, `data/` — other agents work there). Read `docs/prompts/11-launch.md`
fully. Also read how features are wired: `pkg/server/server.go` + `routes_*.go` (`func init(){ register(...) }`;
`pkg/server/server_test.go` fails on duplicate routes), `pkg/auth` (`Current`, `Need`, `NeedRole`, `Write`), `pkg/db`
(`Tx`, `WithUser` = RLS as role app_user), `pkg/ratelimit`, `pkg/me` (account export/delete hooks; a schema-wide test
requires every FK to users be cascade/set null), `pkg/content` (content loader + `RegisterExtra`), `pkg/testdb`
(fresh DB per test run against postgres://postgres:dev@localhost:54329/studenthub), `supabase/migrations/*`.
No `internal/` packages (Vercel/ours forbid it — use `pkg/`). Plain pgx queries.

Implement (server side):
1. Stage 1 security audit: generate `docs/SECURITY.md` — a table of EVERY API route registered in the router (method,
   path, who may call: anonymous/student/ambassador/university_staff/employer/admin/cron, rate limit yes/no, input
   validation yes/no, feature switch). Build it from the code, not from docs: write a Go test or small cmd
   (`cmd/routes`) that walks the registered patterns so the table can't drift, and a test that fails if a new
   state-changing route (POST/PUT/PATCH/DELETE) is not wrapped in the write guard (origin check + rate limit) unless it
   is explicitly listed as an exception (webhooks/cron with their own secrets). Fix any real gaps you find, minimally.
2. Stage 3 data integrity: a test that enumerates ALL tables referencing users (from pg_catalog) and proves account
   deletion removes/anonymizes every one, including files on disk via delete hooks (market photos). Add what's missing.
   Also confirm `client_errors` (below) never stores personal data.
3. Stage 5 demo → real: admin setting «Демо деректерді көрсету» (on/off, stored in a small `app_settings` table) and the
   automatic rule: in each section (jobs, opportunities, events, discounts) when there are more than 5 real (non-demo)
   records, that section's demo records are hidden from `/api/content` automatically. Expose `GET/PUT
   /api/admin/settings` (admin only). Keep `/api/content` output shape unchanged otherwise.
4. Stage 6 monitoring: `client_errors` table + `POST /api/client-errors` (anonymous allowed, strict rate limit per IP,
   whitelist fields: message ≤500 chars with emails/phones/long digit runs redacted, screen path without query string,
   browser family/version only — no user id, no IP stored). `GET /api/admin/health` (admin only): errors in the last 24h
   grouped by message, Telegram webhook status (call getWebhookInfo with BOT_TOKEN when set; tests use
   TELEGRAM_API_BASE), last cron run time (record each cron endpoint's last successful run in a `cron_runs` table — add a
   small helper and call it from the existing cron handlers: /api/cron, /api/cron/events, /api/cron/ambassador,
   /api/cron/mentors, /api/cron/market, /api/cron/b2b), moderation queue length, DB reachable. Daily retention: delete
   client_errors older than 30 days from one of the existing daily cron runs.
Migration: ONLY `supabase/migrations/20261028000016_launch.sql` (13/14/15 are taken). RLS on new tables (server-only).
Tests with pkg/testdb for all of the above. `go vet ./...` and `go test ./...` (whole repo) must pass.
Write `docs/api/launch.md` (endpoints + JSON + frontend checklist for: admin health page, demo toggle in /admin, client
error reporter). Commit on `agent-launch` with clear messages (no AI attribution). Do not push or deploy.
