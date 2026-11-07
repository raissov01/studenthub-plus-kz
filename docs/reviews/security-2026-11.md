# Security review and robustness pass — November 2026

Scope: the Go API at `platform` 6950c17 (`backend/`), its RLS migrations and the nginx/compose setup, with a
focus on what landed in the last two days: café and print shop with Kaspi, uploads, admin console and scopes,
feeds, web push, community, university voice, points. The Next.js frontend was checked for XSS sinks and CSP only.
Branch `agent-sec`; nothing was deployed and production was not touched.

Method: code review of every area (authz in handlers **and** RLS policies, money paths, uploads, outbound HTTP),
then a fuzzer against the local real-API stack (`cmd/e2etest`, fresh database, every feature switch on): every
route of `docs/SECURITY.md` × anonymous / student / admin × path ids `1 0 -1 2^64 abc 1.5 %00 ../` × bodies
(malformed JSON, wrong JSON types, every package's JSON field set to string / -1 / 2^63 / 1e308 / array / object /
bool / null / NUL / 300 000 Cyrillic chars / emoji+RTL / bad date), plus junk query strings. Hostile files were
built by hand and uploaded to the running API.

Severity: **S1** one request takes the service down or moves money/data across accounts; **S2** serious, needs a
precondition; **S3** real but limited (needs a role, timing or a trusted party); **S4** low / defence in depth.
Every finding below was reproduced (test or live request) or proven line by line from the code; guesses are in
the last section. File:line refer to the commit after the fixes.

## Findings

| # | Sev | Finding | Status |
| --- | --- | --- | --- |
| 1 | S1 | 150-byte PDF kills the whole API process (print upload, quiz from-files) | fixed a5202ff |
| 2 | S2 | PDF with a looping xref `/Prev` pins a CPU core forever per upload | fixed a5202ff |
| 3 | S2 | Survey results can be differenced over time to read one respondent's answers | open — owner decision |
| 4 | S3 | Web push: blind SSRF into the internal network through redirects | fixed ff40703 |
| 5 | S3 | Old Kaspi QR paid after switching to pay-at-pickup: paid «pickup» order, no refund on cancel | fixed 6c984ca |
| 6 | S3 | Print: old QR paid at the old price while a new price is proposed | fixed 6c984ca |
| 7 | S3 | Moderator decides own submission / lifts own ban / bans an admin / undoes an admin's final hide | fixed c12be26 |
| 8 | S3 | Anonymous feedback: exact send time recoverable from `slaDueAt` | fixed 4a12f3a (partly, see text) |
| 9 | S3 | Anonymous survey answers linkable to the student at database level (`xmin`, insert order) | open |
| 10 | S3 | Self-declared university/faculty/course opens other universities' surveys and complaint boxes | open — owner decision |
| 11 | S3 | Any player can clone a public/unlisted quiz and read its answer keys | open — owner decision |
| 12 | S4 | NUL in a query/path/JSON string → 500 on 7 routes | fixed a501d0c |
| 13 | S4 | Wellbeing contact email 121–186 chars → 500 | fixed 4a12f3a |
| 14 | S4 | Moderator can hide/restore their own market item / quiz | open |
| 15 | S4 | Roommates: «same gender only» rests on a self-set field; strict listing visible through requests | open |
| 16 | S4 | Webhook amount check skipped when the amount is absent/0; a huge float overflows | open |
| 17 | S4 | Café/print «refunded by hand» after a double payment marks the whole order refunded | open |
| 18 | S4 | Points: an action cut to 0 XP by the daily cap can earn XP again later | open |
| 19 | S4 | Shop managers can probe whether an email has an account (`FindUser`) | open |
| 20 | S4 | Feeds fetcher follows redirects anywhere (sources come only from git) | open |
| 21 | S4 | Frontend CSP has `'unsafe-inline' 'unsafe-eval'` in script-src | open (documented trade-off) |

### 1. S1 — a tiny PDF crashes the API (fixed)

`github.com/ledongthuc/pdf` resolved an object that lives in an object stream by first resolving that stream
(`read.go` `resolve`, upstream line 730). An xref stream that says «object 1 is in object stream 1» recursed until
the goroutine stack hit 1 GB: `fatal error: stack overflow`, which `recover()` cannot catch, so the whole process
exits. `PDFPages` (`printshop/files.go:186`) parses every uploaded PDF; `pdfText` (`quiz/extract.go:109`) parses
quiz notes.

Exploit: any signed-in, consented account (a Telegram account is enough) sends one `POST /api/print/uploads`
with a 120-byte file starting `%PDF-1.5` (passes `Sniff`). Reproduced on the local stack: the request got no
response and the API process died. Docker restarts it, the attacker repeats (40 uploads/hour per account, more
with more accounts): every site, bot and cron endpoint is down.

Fix: `backend/internal/pdf` is the library forked in-tree (BSD licence kept, every change marked
`studenthub:`): nesting depth limit in `resolve`, bounded `/Extends` chains and page-tree walks, visited-offset
sets on `/Prev`, and `pdf.Deadline` — a reader that panics on any read after a deadline, so any other loop that
keeps reading ends too. Tests: `internal/pdf/hostile_test.go` (5 hostile files; against upstream they crash or
hang), `TestInspectHostilePDF`, `TestPDFTextHostile`. Re-probed live: both files now upload in < 0.4 s as
«pages unknown» (the operator confirms) and the API stays up.

### 2. S2 — looping xref pins a core (fixed)

Upstream `readXrefTable`/`readXrefStream` follow `/Prev` with no cycle check; a trailer whose `/Prev` points at
its own `xref` loops forever. `PDFPages` stopped *waiting* after 5 s and released its semaphore, but the parse
goroutine kept spinning, so the 2-slot limit bounded nothing: each upload took one core for good (the quota check
runs after `Inspect`). Same fix and tests as #1; additionally the print semaphore slot is now released only when
the parse has really ended (`printshop/files.go:186-210`).

### 3. S2 — survey results by differencing (open)

`staffResults` (`surveys/results.go:125`) rebuilds the aggregates from all current rows on every request, and
`staffList`/`staffGet` return the exact response count. K = 5 suppression applies to each snapshot, not across
snapshots. Exploit: university staff polls `GET /api/staff/surveys/{id}/results` (and `?by=faculty|course`);
each time the count goes up by one, the change in counts is exactly that one respondent's answers plus their
faculty/course and their new text answer. «I just asked student X to fill it in» → an anonymous survey is
deanonymised. Batching snapshots in steps of K does not fully help (a breakdown group can still grow by one).
Options for the owner: results only after the survey closes; or snapshots frozen in batches with no breakdowns
until close. Both change what staff see today, so this was not changed here.

### 4. S3 — web push SSRF (fixed)

`CheckEndpoint` (`webpush/send.go:146`) only required an https URL; the sender used a plain `http.Client` that
follows redirects. Exploit: a signed-in user registers `https://attacker.example/x` with valid keys, calls
`POST /api/me/push/test`; the attacker answers `307 Location: http://kaspi-pos:3000/...` (or the API, other
containers on the host, 169.254.169.254) and the API makes that request from inside the Docker network; the
`delivered/gone/failed` answer and timing make it a port/status scanner. Fix: the push client dials only global
unicast, non-private, non-CGNAT addresses (checked after DNS in the dialer's `Control`), no proxy, and returns
any 30x as the answer instead of following it (`webpush/send.go` `pushClient`). Test:
`TestDefaultClientStaysOnPublicHosts`.

### 5. S3 — old QR paid on a pay-at-pickup order (fixed)

The poller marks a pending QR `failed` after the timeout although Kaspi may still accept it. `switchToPickup`
(`cafe/pay.go:174`, `printshop/pay.go`) then allows the switch, and when the old QR is paid `applyTx` finds the
order through its `pay_started` event and set `payment_status='paid'` while `payment_method` stayed `pickup`.
Result: the counter treats it as pay-at-pickup and can charge again; `refund()` only refunds `kaspi` orders, so a
cancel never returned the money. Fix: a Kaspi payment always sets `payment_method='kaspi'`
(`cafe/pay.go:289`, `printshop/pay.go:253`). Tests: `TestOldQRPaidAfterSwitchToPickup` (café),
`TestOldQRPaidAfterPickupSwitchOrPriceChange` (print).

### 6. S3 — print: old QR paid during a price change (fixed)

`adjustPrice` (`printshop/staff.go:385`) allows a proposal when the payment is `failed`. If the old QR is then
paid at the old total, the job became `paid`; accepting the proposal (`printshop/jobs.go:539`) left a workable job
paid below its new total, rejecting cancelled it with the money stranded (no refund, no `refund_manual`). Fix:
a payment that arrives while `price_state='proposed'` is recorded (`paid_during_price_change`) and refunded,
the job stays unpaid (`printshop/pay.go:246`). Test: same as #5.

### 7. S3 — moderator separation of duties (fixed)

* `moderation/http.go` `kindAllowed` did not compare `submitted_by` with the reviewer. A post hidden by reports
  files a `community_hidden` item in the author's name (`community/community.go:484`); rejecting it dismisses the
  reports and republishes (`community.go:423`). A moderator could republish their own reported post, or approve
  their own opportunity/market item. Now 403 `own_submission` for a moderator (admins unchanged).
* `market/admin.go` `adminUnban` let a moderator delete their own ban; `adminBan` let a moderator ban an admin.
  Both now 403.
* `community/community.go` `adminModerate` let a moderator restore a post an admin had upheld as hidden
  (`hidden_reason='admin'`, «an admin decision»). Now 403 for a moderator.

Test: `TestModeratorCannotJudgeOwnCaseOrUndoAdmins` (`server/console_test.go`).

### 8. S3 — anonymous feedback send time (partly fixed)

`created_at` of an anonymous ticket is truncated to the hour, but `sla_due_at` was `now + SLA` to the second and
is sent to staff as `slaDueAt` (`feedback/feedback.go:290`), so `slaDueAt − SLA hours` = exact send time; the same
for every anonymous follow-up. Fixed: both are computed from the truncated time (test:
`TestAnonymousTicketReplyLoop`). Still open, by design: staff get a real-time «new ticket» notification
(`feedback.go:544`), and the per-account rate-limit row `fb:t:<user id>` (`feedback.go:476`) holds the exact time
next to the user id for 24 h — a database reader can link it to the ticket. Consider a delayed/batched
notification and a rate-limit key that is an HMAC of the user id.

### 9. S3 — survey anonymity at database level (open)

`submit` (`surveys/surveys.go:274-321`) inserts the anonymous `survey_responses` row and, through `OnComplete`
(`surveys.go:320`, `server/glue_univoice.go`), the points `xp_events` row with `user_id` **in the same
transaction**: both rows carry the same `xmin`, so
`select r.answers, e.user_id from survey_responses r join xp_events e on e.xmin = r.xmin where e.reason='survey'`
links answers to students (also physical insert order). This breaks the migration's promise that the database
alone cannot tell who answered. A database operator or a leaked dump is needed. Fix direction: grant survey
points later, in a separate, batched transaction.

### 10. S3 — self-declared university (open)

`PATCH /api/me` lets a student set `university`/`faculty` freely (`me/me.go:170-171`; the lock trigger of
`20261028000015_university_lock.sql` covers non-students only). Survey eligibility (`surveys.go:162`) and the
feedback recipient scope trust these values, so a student can answer another university's surveys (and collect
the survey points of each) or write to its complaint recipients. Needs a product decision (lock the university
after the first choice, or verify it).

### 11. S3 — quiz answer keys through clone (open)

`cloneFrom` (`quiz/handlers.go:481`) copies the full questions with keys (`loadFull`) for anyone who `canPlay`
the quiz; the clone is the caller's own, so `GET /api/quiz/quizzes/{clone}` shows every key. During a live game on
a public/unlisted quiz a player can read all answers. It contradicts «answer keys … owner-only» in
`docs/SECURITY.md`; if cloning other people's quizzes must stay, allow it only outside live games or without keys.

### 12. S4 — NUL → 500 (fixed)

The fuzzer's only 500s on the original build: `%00` in a query/path (`GET /api/market/items`, `/api/qa/questions`,
`/api/quiz/quizzes`, `/api/quiz/s/{code}`, `/meta`, `/clone`, `/api/events/manage`) and `\u0000` in a JSON string
(`POST /api/events/checkin/claim`): Postgres refuses NUL in text (SQLSTATE 08P01). No body leaked internals (all
were the generic `internal` message). Fix: the router answers 400 to NUL in path/query (`httpx.HasNUL`, wired in
`server/server.go`) and `httpx.Decode` refuses `\u0000`. Tests: `TestDecodeRefusesNUL`, `TestHasNUL`,
`TestNULIsBadRequest`.

### 13. S4 — wellbeing email 500 (fixed)

`reEmail` (`wellbeing/wellbeing.go:41`) allowed up to 186 characters against `char_length(email) <= 120`
(univoice migration): staff saving such a contact got a 500. Now 400. Test in `TestContactsEmptyUntilFilledAndScoped`.

### 14–21. S4, open

14. `market/admin.go:115` `adminDecide` and `quiz/handlers.go:580` `setHidden`: a moderator can hide/restore their
    own item (same class as #7, audited).
15. `community/roommates.go:150` `rmGender` is self-set and changeable, so «same gender only» listings are advisory;
    `rmRequests` loads the requester's listing without `rmVisible` (`roommates.go:79`), so a strict listing can be
    shown to the other gender through a request.
16. `cafe/pay.go:275`, `printshop/pay.go:234`: the amount is compared only when `> 0`; a signed webhook without an
    amount counts as full payment, and `int64(f*100+0.5)` overflows for a huge float. Only the HMAC-signed sidecar
    can send it.
17. `cafe/staff.go:317`, `printshop/staff.go:434` `markRefunded`: after a failed refund of a *second* payment
    (`refund_manual`), «refunded by hand» marks the whole order refunded although the main payment stands; the
    order can then no longer be handed out (`not_paid`).
18. `points/points.go:263`: the duplicate check looks only at `xp_events`; when the daily cap cut XP to 0 no row is
    written, so the same ref earns XP on a later day (coins are safe: unique ledger key).
19. `console/shop.go:250` `FindUser` matches email/username/id: a café/print manager learns whether an email has an
    account (and silently adds that person as staff until removed; audited).
20. `feeds/fetch.go:36`: plain client, redirects followed anywhere. Sources come only from git-tracked
    `feeds/sources.yaml`, so exploiting it needs a hostile source site. 4 MB / 25 s caps exist.
21. `frontend/next.config.ts:26`: `script-src 'self' 'unsafe-inline' 'unsafe-eval' https://telegram.org` (for the
    Telegram widget). No injection sink was found: every `dangerouslySetInnerHTML` takes static SVG/QR/boot code,
    user URLs are https-only on the server, React blocks `javascript:` hrefs.

## Checked and holding

* **Sessions**: HMAC-signed, constant-time compare, new session id per sign-in; role, deletion, session version,
  revocation, blocks and grants re-read on every request; staff sessions capped at 14 days. Cookie `HttpOnly`,
  `SameSite=Lax`, `Secure` except on localhost.
* **CSRF**: `auth.Write` requires an allowed Origin, else Referer; a cross-site text/plain form POST and a
  request with neither header are refused.
* **Telegram**: widget and Mini App HMAC with constant-time compare, `auth_date` checked both ways, duplicate
  initData keys refused; webhook secret constant-time. Replay window is 24 h (`auth/telegram.go:16`) — acceptable
  per Telegram, shorter would be safer.
* **Email codes**: only an HMAC stored, 5 attempts, 15 min, per-address and per-IP send/check limits.
* **Client IP**: `X-Real-Ip` first; both nginx configs overwrite it with `$remote_addr`, and the API port is bound
  to 127.0.0.1, so rate-limit keys cannot be spoofed in the documented deploy.
* **Admin/moderator scope**: every admin-only route answers 403 to a moderator (existing router-wide test);
  role-granting queue kinds are admin-only; grant/block refuse self and admin targets.
* **Money**: totals recomputed from the shop's rows, quantities and option deltas bounded, client totals ignored;
  webhook HMAC over the raw body (`hmac.Equal`, empty secret refused), `unique (operation_id, event)` dedupe,
  «paid» set once under `FOR UPDATE`; staff actions check membership of the order's own shop in SQL; points use a
  locked wallet, unique ledger keys and `CHECK coins >= 0`.
* **RLS**: order/job/inbox reads go through `db.WithUser`; every UPDATE policy has `WITH CHECK`; `SECURITY DEFINER`
  functions set `search_path`; new tables have RLS on. Note: `db.Tx` runs as the owner and bypasses RLS, so for
  writes the handler SQL is the real access control — checked handler by handler in the areas above.
* **Uploads**: `MaxBytesReader` before any read, magic-byte sniffing, `DecodeConfig` + pixel caps before any image
  decode, zip entry caps and macro detection, random stored names, strict name regexes, attachments with
  `sandbox` CSP and `nosniff`; print links are HMAC-bound to file, viewer and expiry and re-checked at serve time.
* **CSV exports**: formula prefixes `= + - @ \t \r` escaped; survey CSV is aggregates only.
* **Errors**: no handler returns `err.Error()`; the fuzzer saw no SQL, path or stack text in any response, and the
  process never died except on #1.

## To check (suspected, not proven)

* Held (threat/profanity) feedback tickets go to the shared queue as kind `feedback` with full text, visible to
  moderators, while the student is told it goes «to the platform administrator first».
* Production env: `MAIL_DEV_LOG` must be false (it logs live codes) and `ALLOWED_HOSTS` must be set explicitly (the
  default also accepts `localhost`/`127.0.0.1` origins, hostname only).
* `webpush.Save` upserts `on conflict (endpoint) do update set user_id` (`webpush/store.go:31`): whoever re-registers
  a known endpoint takes its notifications (endpoints are capability URLs).
* PDF xref streams allocate `make([]xref, Size)` from file data; a tiny file may ask for a large allocation under
  the 120 MiB `GOMEMLIMIT` (it panics on absurd sizes, is recovered, but GC pressure is untested).
* Quiz media upload path (`quiz/media.go`) was not reviewed in depth.
* `POST /api/quiz/live/join` 6-digit code brute force: rate limit not confirmed.
* Kaspi QR real lifetime vs the 10-minute poller timeout (both #5 and #6 depend on a QR outliving it).
* Staff can read unpaid Kaspi orders of their café with the student's QR via `GET /api/cafe/orders/{id}`.
* The rate-limit in-memory fallback resets all counters past 10 000 keys while Postgres is down.

## Robustness pass

| Entry point | Probe | Before | After |
| --- | --- | --- | --- |
| `POST /api/print/uploads` | self-nested object-stream PDF | process died (no response) | 201, pages unknown, 0.36 s |
| `POST /api/print/uploads` | `/Prev` loop PDF | 201 after 5 s, core spinning forever | 201, 0.22 s, no goroutine left |
| 6 GET search/meta routes | `%00` in query/path | 500 | 400 |
| `POST /api/events/checkin/claim` | `"\u0000"` | 500 | 400 |
| `PUT /api/staff/wellbeing/contacts` | 157-char email | 500 | 400 |
| every route × 3 roles | malformed JSON, wrong types, overflow, huge strings, unicode | 4xx | 4xx |

Large bodies to routes that answer before reading (401/403/404) end in a connection reset after the early
answer; that is Go's normal behaviour for an unread oversized body, not a crash.
