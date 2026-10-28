# Launch API — server handoff

Implemented in migration `20261028000016_launch.sql`. Apply it with the existing Go
migration command before starting this version of the API. This work does not deploy or
change the frontend. The new routes are always available; their authorization does not
rely on a feature switch.

## Settings

`GET /api/admin/settings` and `PUT /api/admin/settings` require a current admin session.
Anonymous callers receive 401; all other roles receive 403. PUT also requires an allowed
Origin/Referer and passes through the shared write limiter. The only accepted JSON field is
a required boolean `showDemo`; unknown fields, null, trailing JSON and invalid types return 400.

Request and response:

```json
{"showDemo": false}
```

The singleton `app_settings` row defaults to `true`. Reads and writes go directly to the
database, so changes apply across instances without a restart. Database failures return 503.
No role acting as `app_user`, including an administrator, can read or write this table directly.

`GET /api/content` applies the following independently to jobs, opportunities, events and
discounts, after merging sources and filtering expired records:

- `showDemo=false`: remove all records with `is_demo=true`.
- `showDemo=true`: retain demos while that section has at most five real records;
  hide its demos once it has **six or more** real records.
- Missing `is_demo` means real. Hiding demos never removes the underlying records.
- Changing one section's real-record count does not hide another section's demos.

The existing keys and record shapes are preserved. An optional `events` array is now populated
from published, unexpired database events when `FEATURE_EVENTS=true`; it is omitted when empty.
Each event contains `id`, `title_kk`, `title_ru`, `desc_kk`, `desc_ru`, `cat`, `starts_at`,
`ends_at`, `university`, `place_text`, `building_id` and `is_demo`. It contains no registrations,
check-in secrets or private attendee information. `events.is_demo` defaults to false and is
set only by server-side demo imports, not organizer inputs. Other sections still combine
Sheets/bundled records with the existing database sources. Places are unaffected.

The content response uses `Cache-Control: no-store` so a CDN cannot serve demos after the
admin turns them off. Loader source caches remain intact. If settings cannot be read with
a configured database, demos are hidden; the standalone mode without DATABASE_URL retains
the default of showing demos. The setting affects the content feed, not individual resource
URLs or the separate event-management API.

## Client error reporter

`POST /api/client-errors` accepts anonymous calls with an allowed Origin/Referer. Send only:

```json
{
  "message": "TypeError: Cannot read properties of undefined (reading 'map')",
  "screen": "/jobs",
  "browser": "Chrome/131"
}
```

A successful insert returns **204 with no body**. The request body is limited to 8 KiB.
`message` must be nonempty; unknown fields (including userId, IP, stack, email, session or
profile), malformed JSON and multiple JSON values return 400. Omitted screen/browser values
normalize to `/` and `Unknown`. Storage failures return 503. A strict, fixed **5 requests per
IP per minute** budget applies, in addition to `RATE_WRITE_PER_MIN` (default 30) for browser
writes. A 429 response includes `Retry-After`. Do not retry automatically or recursively
report reporter failures.

Privacy is enforced on the server:

- Emails, phone-like sequences, Unicode digits and long digit runs are scrubbed before
  classification. Stored messages are at most 500 characters. Only a small allowlist of
  constant browser diagnostics is retained verbatim (for example `Failed to fetch`).
  Other messages become `TypeError: [details redacted]`, another standard JS error class,
  or `Client error: [details redacted]`. Arbitrary free text is not stored: regex redaction
  alone cannot reliably remove names, tokens or form values.
- Screen is a fixed application path prefix, at most four known segments. Query, fragment,
  host, IDs, portfolio slugs and arbitrary path components are discarded. For hash routing,
  the frontend should send the screen name (e.g. `/market`), not the full location hash.
- Browser is reduced to Chrome/Firefox/Safari/Edge/Opera and its major version, or Unknown.
  Full user agents, device/OS details and arbitrary browser strings are discarded.
- `client_errors` has exactly `id`, `message`, `screen`, `browser`, `created_at`. It has no
  user, session, analytics identifier or IP column and no user foreign key. It is server-only
  under RLS. The dedicated rate-limit key is a daily HMAC of IP; the existing shared write
  guard separately uses the application's normal rate-limit counters.

## Admin health

`GET /api/admin/health` requires a current admin session. It returns:

```json
{
  "dbReachable": true,
  "errors": [
    {"message":"Failed to fetch","count":2,"lastSeen":"2026-10-28T12:00:00Z"}
  ],
  "cronRuns": [
    {"endpoint":"/api/cron","lastSuccessAt":"2026-10-28T03:00:00Z"},
    {"endpoint":"/api/cron/events","lastSuccessAt":null},
    {"endpoint":"/api/cron/ambassador","lastSuccessAt":null},
    {"endpoint":"/api/cron/mentors","lastSuccessAt":null},
    {"endpoint":"/api/cron/market","lastSuccessAt":null},
    {"endpoint":"/api/cron/b2b","lastSuccessAt":null}
  ],
  "moderationQueue": 3,
  "telegram": {
    "configured": true,
    "reachable": true,
    "installed": true,
    "pendingUpdates": 0,
    "lastErrorAt": null
  }
}
```

Errors are grouped by normalized message for the preceding 24 hours, ordered by count
(descending), then message, capped at 100 groups. `moderationQueue` counts pending submissions
in the shared queue, without double-counting their feature records. `cronRuns` includes all
six jobs; null means no successful run has been recorded since this migration, including
jobs whose feature is disabled.

When BOT_TOKEN is set the API calls `getWebhookInfo`; `TELEGRAM_API_BASE` overrides
`https://api.telegram.org` for tests. The request has a three-second timeout and does not
follow redirects. Only configuration/reachability, webhook presence, pending count and
last-error timestamp are returned. Bot tokens, the webhook URL/IP and Telegram's free-form
error description are never exposed. Telegram failures preserve the rest of the health
response and report `reachable=false`. An unset token reports `configured=false`.

The DB probe and monitoring queries have a three-second timeout. A monitoring query failure
returns 503 with the available health fields; unavailable queue is null. `dbReachable` means
the database answered the probe, not that every schema/query succeeded. As with all admin
routes, validating a session itself needs the database: a complete DB outage before the
handler is reached can result in 401. Use an external uptime check as well.

All existing cron endpoints still require `Authorization: Bearer <CRON_SECRET>`. Their
feature gates remain unchanged. Successful work records `cron_runs.last_success_at`; rejected
requests and failed jobs keep the previous timestamp. Failure to record completion returns
500, not a false success. The main `/api/cron` deletes errors older than 30 days in the same
transaction as recording its success. “Success” here means the endpoint completed its job;
existing result fields still report deferred or failed individual notifications where relevant.

## Frontend checklist

- [ ] Add admin-only “Демо деректерді көрсету” in `/admin`; GET the current value, PUT the
  explicit boolean, handle failures, and refresh the content feed after a successful save.
- [ ] Add an admin health view for DB status, error counts/last seen, Telegram status, all
  cron timestamps and moderation queue length. Show null timestamps as “not run”; distinguish
  disabled jobs from overdue jobs using the existing config endpoint. Render text with normal
  escaping and handle 401, 403 and partial 503 responses.
- [ ] Register `error` and `unhandledrejection` listeners. Send only the three whitelisted
  fields, use a static screen name and browser family/major version, deduplicate locally,
  and stop/report nothing further on 429 or reporter failure. Never send stacks, user input,
  profile, location.href, Telegram initData, cookies or tokens. Explain that unknown diagnostic
  details are deliberately discarded by the API.
- [ ] Reuse the optional content `events` field for the public content section. Resource and
  management APIs retain their existing access/visibility rules.

## Verification and operational limits

Run from the repository root with PostgreSQL available to `pkg/testdb` (default
`postgres://postgres:dev@localhost:54329/studenthub`; override with TEST_DATABASE_URL):

```sh
go run ./cmd/routes > docs/SECURITY.md
go vet ./...
go test ./...
go test -race ./pkg/server ./pkg/launch ./pkg/content
```

Tests use fresh disposable databases and all migrations. A missing database causes the
existing test helper to skip integration tests; check output when running elsewhere.
The security inventory test prevents route/document drift and the behavioral guard test
checks every registered browser mutation's origin and rate protection. Router tests check
anonymous access, all non-admin roles on every admin route, and all feature switches.

The deletion integration fixture discovers every user FK through pg_catalog, requires
populated coverage for each, snapshots primary keys, then deletes through DELETE /api/me.
It verifies cascades and surviving anonymized rows, invalidated sessions, company handover
or closure, and attached/unattached/legacy market photos while retaining another user's data.
An undeletable photo aborts the account deletion and a later retry succeeds. SQL and disk
are not one atomic resource: a later SQL failure can leave a surviving row without an
already-removed file. Existing analytics is anonymous device-based, with no linked PostHog
person or sessions/notifications tables in this schema; signed cookies are invalidated by
removing the user. No external analytics erasure API is configured or assumed.

Before release, apply the migration, ensure trusted proxies overwrite client IP headers,
keep allowed origins/secrets configured, schedule all enabled daily jobs, and ensure the API
has persistent writable market storage. Check health and moderation after rollout. The
frontend checklist, browser/PWA tests and deployment remain the responsibility of the other
workstreams; no push or deployment is performed by this task.
