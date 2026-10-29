# Notifications API and shared dispatcher

`pkg/notify` owns every active Go `sendMessage` call. The inbox contract is in
[inbox.md](inbox.md). Schema: migrations `13`, `20`, `21`, and
`20261029000024_notification_dispatch.sql`.

## Rules (04-032–04-037, 11-017)

- Every notification first commits one `notifications` row and an outbox receipt in the
  **same transaction**. An inbox write failure prevents Telegram delivery. Unlinked,
  inactive, or category-disabled accounts still receive the inbox item.
- **Direct: at most 3 per user per Asia/Almaty calendar day.** A student may lower this
  to 1 or 2. The database constraint and dispatcher both enforce the maximum of 3.
- **Digest: at most 1 per user per Asia/Almaty day**, independently of the direct budget.
  Producers queue individual items; `Flush` combines due items in the order
  **deadlines → events → remaining categories**, then source order within each priority.
  Overflow beyond Telegram's message length stays queued for the next day. Each item has
  its own inbox entry. Bot action keyboards survive aggregation and deferral.
- Default quiet hours are **22:00–08:00 Asia/Almaty**. Both delivery types wait until the
  configured quiet period ends. Caps defer to the next local day, after quiet hours.
  Pending items are **not expired after 72 hours**. Paused digest recipients also keep
  their inbox items; `/resume` makes the queue eligible for rechecking.
- All categories are switchable: `deadlines`, `digest`, `events`, `mentors`, `market`,
  `jobs`, `community`, `moderation`. A disabled category is inbox-only. Every queued item
  is rechecked before reservation, so disabling a category also cancels pending delivery.
  Re-enabling a category does not replay previously suppressed receipts.

### Moderation decision

The foundation spec, stage 5, explicitly includes publication decisions among `direct`
messages and says **all functions** use the common budget. It contains no moderation
exemption. Therefore moderation, report outcomes, admin alerts, and ambassador renewal
questions obey the same cap and quiet hours and can be switched off. `Direct`,
`DirectAs`, and `DirectFor` are compatibility adapters, not privileged transports.

New bot command replies also use the direct budget (`community`); this includes `/help`,
`/settings`, and onboarding messages. They may be deferred or muted and remain visible in
inbox. Callback acknowledgements and edits to existing menus are interactive protocol
responses, not new messages, and do not consume notification slots. `/stop` cancels pending
items and marks a retained site account inactive. Shared receipts, settings, and daily
budget records survive `/stop` so `/stop`→`/start` cannot reset the cap. Deleting a bot-only
account or deleting the site account cascades its data normally.

## Persistence, concurrency, and retries

Acceptance and budget reservation take a transaction-scoped PostgreSQL advisory lock per
user. All processes, cron runs, and modules share that lock and the same `notify_sends`
ledger. Direct reservations count immediately. Digest reservations also own a unique
`bot_daily_sends(user_id, day)` claim. No feature producer claims its own day.

The reservation and `sending` state commit **before** the network call. Successful
transmission moves the items to `sent`. An explicit Telegram `ok:false` rejection releases
the reservation and schedules a retry after an hour (maximum five attempts); 403 marks the
bot inactive and drops Telegram delivery. The inbox stays intact.

Telegram has no idempotency key for `sendMessage`. A timeout, broken response, or process
crash after reservation has an ambiguous outcome: the receipt becomes `uncertain` and its
budget remains consumed. It is **not automatically resent**, avoiding duplicate messages
and budget overruns. A stale `sending` claim becomes `uncertain` after 15 minutes. This
provides at-most-once attempts for ambiguous outcomes, not a promise of exactly-once
external delivery. Operators can inspect `uncertain` receipts; the in-site message remains
available regardless of Telegram's outcome.

Use a stable source key for every recurring event. `Opts.Key` or `WithKey(ctx, key)` is
namespaced by user, delivery kind, and category. Keys cover event reminders, pair prompts,
meeting notices, application decisions, market expiry versions/seasons, ambassador terms,
and bot update IDs. Generic moderation and module-specific decision notices reuse the
same key to avoid double notifications. Legacy callers without a key deduplicate identical
text/category/kind/link. Terminal receipts are retained **90 days**; that is the retry
idempotency window. Successful retries and repeated cron runs do not create another inbox
row or Telegram message. Distinct source keys can carry identical text.

## Go API

```go
// Sent | Queued | Off | Uncertain. Inbox/queue persistence errors are returned.
res, err := notify.Send(ctx, userID, notify.CatEvents, text, notify.Opts{
    Key: "event:42:promoted", Link: "/event/42",
})
var Notify = notify.For(notify.CatJobs)
err = Notify(notify.WithKey(ctx, "application:123:invited"), userID, text)

// Queues a digest item; no immediate Telegram send.
st, err := notify.DigestAs(notify.WithKey(ctx, "pair:42:check3"),
    userID, notify.CatMentors, now, text)
if err == nil && notify.DigestAccepted(st) {
    // Mark the source as handed to the dispatcher (sent, queued, or inbox-only/off).
}
// notify.Digest defaults to CatDigest; B2B's adapter explicitly uses CatJobs.
// notify.Direct/DirectAs/DirectFor all delegate to Send.

result, err := notify.Flush(ctx, now)
```

`notify.Dispatcher` uses the same implementation with an explicit pool, Telegram endpoint,
token, and HTTP client; the bot uses this to retain its configured transport. No token means
queue without reserving budget, not a falsely successful delivery.

## HTTP

Existing cookie/Bearer authentication, `auth.Write` Origin/rate-limit checks, and localized
`{"error":{"code","message"}}` errors are unchanged. No routes were added.

`GET /api/me/notifications` (signed in):

```json
{"prefs":{"categories":{"deadlines":true,"digest":true,"events":true,"mentors":true,"market":true,"jobs":true,"community":true,"moderation":true},"quietHours":{"enabled":true,"start":"22:00","end":"08:00","timezone":"Asia/Almaty"},"dailyCap":3,"locked":[]}}
```

`PUT /api/me/notifications` accepts any subset and returns the complete new preferences:

```json
{"categories":{"market":false,"moderation":false},"quietHours":{"start":"23:00","end":"08:00"},"dailyCap":2}
```

| HTTP 400 code | Cause |
|---|---|
| `bad_request` | Invalid JSON or more than 4 KB |
| `unknown_category` | Unknown category |
| `invalid_quiet_hours` | Invalid HH:MM or identical start/end |
| `invalid_daily_cap` | Outside 1–3 |

Existing explicit quiet-hour preferences are preserved on migration; new/default settings
end at 08:00. Existing caps above 3 are lowered to 3. `locked` remains an empty array for
wire compatibility. Both website settings and bot `/settings` expose every category.

`GET /api/cron/notify`, with `Authorization: Bearer <CRON_SECRET>`, dispatches due work:

```json
{"ok":true,"sent":1,"deferred":2,"dropped":0,"failed":0,"uncertain":0}
```

`sent` counts Telegram messages (one aggregate digest can contain multiple inbox items);
`deferred`/`dropped` count items. Invalid credentials return 401. Daily producer counters
such as `check3Sent`/`reminded` retain their historical names but now count items handed to
the dispatcher, not confirmed Telegram delivery.

Run all producers first, then `/api/cron/notify`. `deploy/studenthub-cron.service` does this;
`studenthub-notify.timer` also flushes hourly at :05 for quiet-hour/cap deferrals. No service
was deployed or restarted as part of this change.

## Producer audit

| Producer | Shared entry point | Delivery type |
|---|---|---|
| Events: promotion/cancellation | `notify.For` | direct |
| Events: reminder/feedback | `notify.DigestAs(CatEvents)` | digest |
| Ambassador: term renewal | `notify.For(CatModeration)` | direct |
| Mentors: decisions, introductions, pair closure | `notify.Send` | direct |
| Mentors: week/month prompts, group meetings | `notify.DigestAs(CatMentors)` | digest |
| Market: decisions/reports | `notify.Send` | direct |
| Market: expiry/season | `notify.DigestAs(CatMarket)` | digest |
| B2B: applications/invitations/admin alerts | `notify.For(CatJobs)` | direct |
| B2B: no-response follow-up | `notify.DigestAs(CatJobs)` | digest |
| Moderation: review/report notices | `notify.For(CatModeration)` | direct |
| Bot: new command replies | `Dispatcher.Send(CatCommunity)` | direct |
| Bot: deadlines/weekly opportunities | `Dispatcher.Digest` | digest |
| Quiz | HTTP/WebSocket game responses only; no Telegram sender | none |

`legacy/js/lib/bot/telegram.js` is archived pre-Go code, outside the Next/Go build and route
registration. Its old transport is not an active sender. `pkg/notify/modules_test.go` guards
against new Go `sendMessage` bypasses and exercises the real producers together.

RLS remains self-only for preferences, inbox, and exported outbox rows. New columns and
receipts cascade with account deletion. Tests: `pkg/notify/integrity_test.go` (table-driven
budgets, quiet/off categories, concurrent calls, UTC/Almaty boundary, inbox failure,
retry/ambiguous outcomes, keyboard/length), `modules_test.go` (real producers), and existing
bot/event/mentor/market integration tests.
