# Mentors API (prompt 07)

Server: `pkg/mentors`, wired in `pkg/server/routes_mentors.go`. Schema: `supabase/migrations/20261028000008_mentors.sql`.
Spec: `docs/prompts/07-mentors.md`.

## Conventions

- **Feature switch** `FEATURE_MENTORS` (on/off). While off, *every* route below answers `404 not_found`, exactly like an unknown route. The frontend reads `GET /api/config` → `features.mentors` and hides the section.
- **Auth**: session cookie `sh_session` (or `Authorization: Bearer <token>` in the Mini App). "signed in" = 401 `unauthorized` otherwise.
- **Writes** (POST/PATCH/DELETE) need an `Origin`/`Referer` from `ALLOWED_HOSTS` (403 `bad_origin`) and are rate limited (429 `rate_limited`, `Retry-After`).
- **Errors** are always `{"error": {"code": "...", "message": "..."}}`; the message is in kk or ru (`?lang=ru` or `Accept-Language`). Branch on `code`, show `message`.
- `400 bad_request` = malformed JSON / invalid enum / text too long.
- Times are RFC 3339 UTC strings.

### Enums

| Name | Values |
|---|---|
| `need` ("не керек?") | `adaptation` (бейімделу: жатақхана, құжаттар), `study` (оқу), `career` (мансап), `life` (тұрмыс) |
| `expect` | `one_on_one`, `group`, `any` (default). Minors are forced to `group`. |
| mentor `format` | `one_on_one`, `group`, `both` (default) |
| mentor `status` | `pending` (in the queue), `approved`, `paused` (own break), `rejected` (may re-apply), `removed` (by admin) |
| request `status` | `waiting`, `matched`, `closed` |
| report `reason` | `disrespect`, `money`, `unsafe_meeting`, `no_show`, `other` |

## Endpoints

| Method & path | Who | Purpose |
|---|---|---|
| `GET /api/mentors/summary?university=` | anyone | Honest numbers: "X mentors, Y waiting, usually N days" + own status |
| `POST /api/mentors/requests` | signed in, consented, university set | Ask for a mentor |
| `POST /api/mentors/requests/close` | signed in | Withdraw own waiting request |
| `POST /api/mentors/apply` | signed in, adult, 2–4 course or alumni | Become a mentor (goes to the moderation queue) |
| `GET /api/mentors/me` | signed in | Own mentor profile (or `null`) |
| `PATCH /api/mentors/me` | approved/paused mentor | Pause/resume, edit profile |
| `GET /api/mentors/pairs` | signed in | My pairs (as mentor and as mentee) |
| `POST /api/mentors/pairs/{id}/checkin` | pair participant | Week-3 check-in / week-4 "continue?" |
| `POST /api/mentors/pairs/{id}/end` | pair participant | Close the pair (reason optional) |
| `POST /api/mentors/pairs/{id}/score` | pair participant | Rate an ended pair |
| `POST /api/mentors/pairs/{id}/report` | pair participant | Report the other side |
| `GET /api/admin/mentors?university=` | admin | Matching screen, left column |
| `GET /api/admin/mentors/requests/{id}/candidates` | admin | Matching screen, right column |
| `POST /api/admin/mentors/match` | admin | Create a pair |
| `GET /api/admin/mentors/pairs?status=active\|ended&university=` | admin | All pairs |
| `POST /api/admin/mentors/pairs/{id}/end` | admin | End a pair (reason required, audited) |
| `POST /api/admin/mentors/{userId}/status` | admin | Approve / pause / remove a mentor (reason required, audited) |
| `GET /api/ambassador/mentors` | ambassador | Same as admin board, pinned to own university |
| `GET /api/ambassador/mentors/requests/{id}/candidates` | ambassador | Same, own university only |
| `POST /api/ambassador/mentors/match` | ambassador | Same, own university only |
| `GET /api/cron/mentors` | `Authorization: Bearer $CRON_SECRET` | Daily job (systemd timer) |

Mentor applications and reports are decided in the **shared moderation queue** (`#/admin/queue`, prompt 06) — see "Queue kinds" below.

---

### GET /api/mentors/summary

Query: `university` (optional; defaults to the signed-in user's university).

```json
{
  "university": "ENU",
  "stats": {"university": "ENU", "mentors": 4, "freeMentors": 1, "waiting": 7, "typicalDays": 5.5},
  "me": {"isAdult": true, "request": null, "mentorStatus": null},
  "groupMeetings": []
}
```

- `stats` is absent when no university is known. `typicalDays` is the median wait (request → match) over the last 180 days, `null` with fewer than 3 matches → show "not enough data yet", never invent a number.
- `me` only when signed in. `me.request` = the open request (`{id, need, goal, expect, status, createdAt, matchedAt}`) or `null`. `me.mentorStatus` = own mentor status or `null`.
- `groupMeetings` = nearest `cat = mentoring` events (`[{id, title, startsAt, place, online, url}]`), filled once the events package (05) sets `mentors.GroupMeetings`; until then always `[]`.

### POST /api/mentors/requests

```json
{"need": "adaptation", "goal": "Жатақхана құжаттарын қалай тапсырамын?", "expect": "any"}
```

`goal` ≤ 300 chars (may be empty). 201:

```json
{
  "request": {"id": 12, "need": "adaptation", "goal": "...", "expect": "any", "status": "waiting", "createdAt": "...", "matchedAt": null},
  "stats": {"university": "ENU", "mentors": 4, "freeMentors": 0, "waiting": 8, "typicalDays": 5.5},
  "groupOnly": false,
  "offerGroup": true,
  "groupMeetings": []
}
```

- `groupOnly: true` for minors (`isAdult` not true): no personal mentor, only group mentoring and open office hours. Their `expect` is stored as `group`.
- `offerGroup: true` when no mentor is free, the student is a minor, or chose `group` → show the nearest group meeting immediately.

Errors: 403 `consent_required`, 400 `university_required`, 409 `request_exists` (one open request per student), 400 `invalid_goal`.

### POST /api/mentors/requests/close

No body. 200 `{"ok": true}`; 404 `no_open_request` (nothing waiting — a matched request is closed by ending the pair).

### POST /api/mentors/apply

```json
{"areas": ["adaptation", "study"], "format": "both", "about": "≤300 chars", "alumni": false, "capacity": 2, "groupCapacity": 8}
```

`areas` 1–4 distinct needs; `capacity` 1–5 (default 2); `groupCapacity` 5–10 (default 8). Eligible: `isAdult = true` **and** (`alumni = true` or profile `course` 2–4). 201 `{"status": "pending", "submissionId": 41}`.

Errors: 403 `consent_required`, 400 `university_required`, 403 `not_eligible`, 409 `already_applied` / `already_mentor`, 409 `mentor_removed` (role taken away by an admin; no re-apply). A `rejected` applicant may apply again.

### GET /api/mentors/me · PATCH /api/mentors/me

```json
{"mentor": {"userId": 5, "university": "ENU", "areas": ["study"], "format": "both", "capacity": 2, "groupCapacity": 8,
  "about": "...", "alumni": false, "status": "approved", "approvedAt": "...", "activePairs": 1, "badge": false}}
```

`mentor` is `null` for non-mentors. `badge` = has the verified "Ментор 🎓" badge.

PATCH body (all optional, at least one): `{"status": "paused" | "approved", "areas": [...], "format": "...", "about": "...", "capacity": 1-5, "groupCapacity": 5-10}`. `"paused"` = session break: the mentor disappears from candidate lists; `"approved"` resumes. Same response shape. 409 `not_editable` when the mentor is `pending`, `rejected` or `removed` (enforced by RLS — a pending mentor cannot approve themselves).

### GET /api/mentors/pairs

```json
{"pairs": [{
  "id": 3, "role": "mentee", "need": "adaptation", "university": "ENU",
  "startedAt": "...", "continuedAt": null, "endedAt": null, "endReason": null, "myScore": null,
  "partner": {"id": 5, "name": "Айгерім", "university": "ENU", "faculty": "IT", "username": "aigerim_k"},
  "prompts": [{"kind": "check3", "sentAt": "...", "answered": false}],
  "due": ["check3"]
}]}
```

- Active pairs first. `partner.username` is present **only while the pair is active and both sides are confirmed adults**; otherwise `null`.
- `due` lists the check-ins the user can answer now: `check3` from day 21, `month` from day 28 (until continued).

### POST /api/mentors/pairs/{id}/checkin

- Week 3: `{"kind": "check3", "met": true, "score": 4}` (`score` 1–5 optional). → `{"ok": true, "offerRematch": false}`. `offerRematch: true` when `met` is false or score ≤ 2: show the blame-free "Басқа ментор іздейік пе?" offer (→ `/end` with `rematch: true`).
- Week 4: `{"kind": "month", "continue": true}` → `{"ok": true, "continued": true}` once both said yes. `continue: false` ends the pair at once → `{"ok": true, "ended": true}`. If no two "yes" answers arrive within 7 days of the question, the cron ends the pair (`end_reason = first_month_done`).

Errors: 404 `pair_not_found` (not a participant), 409 `pair_ended`, 409 `too_early`.

### POST /api/mentors/pairs/{id}/end

`{"reason": "optional ≤300", "score": 1-5 optional, "rematch": false}`. Either side may end; the reason is never required. `rematch: true` (mentee only) opens a new waiting request with the same need. The other side gets a neutral "pair closed" message. 200 `{"ok": true}`; 404 `pair_not_found`; 409 `pair_ended`.

A mentee score of 4–5 awards the mentor the verified `mentor` badge (`user_badges`, shown as "Расталған ✓" in the portfolio, prompt 09).

### POST /api/mentors/pairs/{id}/score

`{"score": 1-5}` after the pair ended. 409 `pair_active` before that.

### POST /api/mentors/pairs/{id}/report

`{"reason": "money", "text": "≤500 chars", "endPair": true}` → 201 `{"submissionId": 77}`. Creates a `mentor_report` submission, direct-messages every admin, and with `endPair` closes the pair immediately. 404 `pair_not_found` for non-participants.

### GET /api/{admin|ambassador}/mentors

Admin: optional `?university=` filter. Ambassador: always their own `users.university` (query ignored); 403 `no_university` if the profile has none; 403 `not_lead` once prompt 06 restricts matching to team leads.

```json
{"university": "ENU",
 "requests": [{"id": 12, "userId": 9, "name": "Ержан", "university": "ENU", "faculty": "IT", "course": 1, "isAdult": true,
   "need": "adaptation", "goal": "...", "expect": "any", "createdAt": "...", "waitingDays": 3, "groupOnly": false}],
 "stats": {"university": "ENU", "mentors": 4, "freeMentors": 1, "waiting": 7, "typicalDays": 5.5}}
```

Waiting requests, **oldest first**, max 200. No Telegram usernames anywhere on this screen. `groupOnly` rows (minors / group-only) cannot be paired — offer group mentoring. `stats` is `null` for an admin without a filter.

### GET /api/{admin|ambassador}/mentors/requests/{id}/candidates

```json
{"requestId": 12, "need": "adaptation", "groupOnly": false, "candidates": [{
  "userId": 5, "name": "Айгерім", "university": "ENU", "faculty": "IT", "course": 3, "alumni": false,
  "areas": ["adaptation", "study"], "format": "both", "about": "...", "capacity": 2, "activePairs": 1, "freeSlots": 1,
  "pastPairs": 2, "avgScore": 4.5, "score": 6.25,
  "reasons": {"areaMatch": true, "sameFaculty": true, "freeSlots": 1, "avgScore": 4.5}}]}
```

Approved one-on-one mentors of the request's university **with a free slot**, sorted by the helper `score` (area match +3, each free slot +1, same faculty +1.5, feedback −1…+1). It is only a suggestion: show `reasons`, let the human pick. Empty list + `groupOnly: true` for minors. 404 `request_not_found` (also for another university's request when called by an ambassador).

### POST /api/{admin|ambassador}/mentors/match

`{"requestId": 12, "mentorId": 5}` → 201 `{"pairId": 3, "mentorId": 5, "menteeId": 9}`. Audited (`audit_log.action = mentor_match`). Both sides receive a direct Telegram message: names, university, need, the partner's @username (or a "we sent yours, they will write" / "set a username and message the bot" fallback), "first step: agree on a 15-minute intro", and the etiquette rules.

All checks run inside `mentor_match()` under row locks:

| Code | Status | Meaning |
|---|---|---|
| `request_not_waiting` | 409 | already matched/closed or unknown |
| `wrong_university` | 403 | ambassador outside their university (request or mentor) |
| `mentee_minor` | 409 | mentee is not a confirmed adult — never paired |
| `group_only` | 409 | the student chose group mentoring |
| `self_match` | 409 | |
| `mentor_not_approved` | 409 | pending, paused, rejected, removed or not a mentor |
| `mentor_group_only` | 409 | mentor runs only group meetings |
| `mentor_full` | 409 | capacity reached |
| `mentee_has_mentor` | 409 | the student already has an active pair |

### Admin-only

- `GET /api/admin/mentors/pairs?status=active|ended&university=` → `{"pairs": [{id, university, need, mentorId, mentorName, menteeId, menteeName, startedAt, continuedAt, endedAt, endReason, menteeScore, mentorScore}]}` (max 300).
- `POST /api/admin/mentors/pairs/{id}/end` `{"reason": "3–500 chars"}` → `{"ok": true}`; 400 `reason_required`, 404 `pair_not_found`, 409 `pair_ended`.
- `POST /api/admin/mentors/{userId}/status` `{"status": "approved"|"paused"|"removed", "reason": "3–500"}` → `{"ok": true, "endedPairs": 1}`. `removed` takes the mentor role away and ends the mentor's active pairs (mentees are told). Note: a mentor can resume their own `paused` status — use `removed` to enforce. 404 `not_a_mentor`.

### GET /api/cron/mentors

Bearer `CRON_SECRET`; 401 otherwise. Runs after the bot's `/api/cron` (see `deploy/studenthub-cron.service`) so deadline reminders keep priority. Response `{"check3Sent", "monthSent", "postponed", "monthExpired", "meetingNotices", "meetingPostponed"}`.
Group meetings (07-017): every student with a `waiting` request hears once about each published, not yet started `cat = mentoring` event of their university that has free seats and that they have not registered for — a digest-type message in the `mentors` category (one bot message a day: one meeting per student per run, the rest on later days), with a link to `/event/{id}`. Delivered (or undeliverable by the bot: category off / no Telegram — the inbox has it) notices are recorded in `mentor_meeting_notices (event_id, user_id)`. Week-3 and week-4 questions are digest messages: they use the bot's one-message-per-day claim (`bot_daily_sends`), so a student who already got a bot message today is asked on a later day.

## Queue kinds (shared `submissions`, decided by `moderation.Review`)

| kind | payload | approve | reject (reason required) |
|---|---|---|---|
| `mentor_application` | `{userId, name, university, faculty, course, alumni, areas, format, about, capacity, groupCapacity}` | mentor → `approved`, DM | mentor → `rejected`, DM with reason |
| `mentor_report` | `{pairId, reporterId, againstId, reporterRole, reason, text, pairEnded}` | pair ended (`report_upheld`), reporter thanked | reporter told, with reason |

### Interview before approving a mentor (admin checklist)

1. Why do you want to be a mentor? What did you wish you had known in your first month?
2. Which of the four needs can you really help with (adaptation / study / career / life)?
3. How much time per week, and when is your exam session (you can pause yourself)?
4. What will you do if a mentee asks for money, or you feel uncomfortable? (Answer: refuse, report through the site.)
5. Do you agree to meet only on campus, in a crowded place or online, and to never ask for personal financial data?
6. Can you answer within 2–3 days? If not, will you pause instead of going silent?

## For other packages

- `mentors.IsApprovedMentor(ctx, tx, userID) bool` — for the events package (05): an approved mentor may create `cat = mentoring` group meetings (5–10 seats; campus or online only). Works in an app_user transaction for that user.
- `mentors.GroupMeetings = func(ctx, university string, limit int) ([]mentors.GroupMeeting, error)` — set by events to fill `groupMeetings`.
- `mentors.AmbassadorCanMatch = func(ctx, userID int64, university string) (bool, error)` — set by the ambassador package (06) to allow only team leads.

## Frontend checklist (screens the spec asks for)

- [ ] `#/mentors` — "не керек?" (4 options) + "нақты не сұрағың келеді?" (≤300) + expectation choice (personal / group / any). After submit, the honest answer: "Қазір X ментор, Y сұраныс күтіп тұр. Әдетте жұптау N күн алады" (`stats`; hide N when `typicalDays` is null). If `offerGroup` → show the nearest group meeting right away. Minors see only group mentoring and office hours, never a "personal mentor" promise. State for an open request (waiting / matched) with "withdraw".
- [ ] `#/mentors/become` — form (areas, format, about ≤300, alumni checkbox, capacity); show eligibility (adult, 2–4 course or alumni) before the form; after submit "тексерілуде" + note that an admin will have a short talk first.
- [ ] `#/mentors/me` — mentor profile + pause/resume (session break) + my pairs; for each pair: partner card (username only when present), due check-ins (week-3: met? 1–5; week-4: continue?), "Жұпты жабу" (optional reason, optional score), "Шағымдану" (reason + text + "also end the pair"), rematch offer after a negative check-in, rating after the end. Mentees see the same pair card.
- [ ] Safety text on the pair card and in the intro: respect; never ask for money or personal financial data; meetings only on campus, in a crowded place or online — for adult pairs too.
- [ ] `#/admin/mentors` (admin) — left: waiting requests oldest first (group-only rows marked); right: candidates for the selected request with `reasons`; "Жұптау" button; error toasts by `code`. Pairs list with end-pair and mentor status (remove / pause / approve) with a reason field.
- [ ] `#/ambassador/mentors` (ambassador) — the same left/right screen, own university only; not under `#/admin`.
- [ ] `#/admin/queue` (06) — show `mentor_application` and `mentor_report` payloads; reject requires a reason.
- [ ] Portfolio (09): `user_badges` row `mentor` (verified) → "Ментор 🎓 Расталған ✓".
