# B2B API — employer and university cabinets (prompt 10)

Server: `pkg/b2b`, wired in `pkg/server/routes_b2b.go`. Schema: `supabase/migrations/20261028000011_b2b.sql`.
Spec: `docs/prompts/10-b2b.md`.

## Conventions

Switch `FEATURE_B2B` (off → every route below is `404 not_found`, and `/api/content` contains no database jobs or
official opportunities). Session cookie or `Authorization: Bearer`; writes need an allowed `Origin` and are rate limited;
errors are `{"error": {"code", "message"}}` (kk/ru) — branch on `code`. Keys are camelCase, except records merged into
`/api/content`, which keep the `data/*.json` snake_case shape.

Shared pieces: the admin queue (`GET /api/admin/queue`, `POST /api/admin/queue/{id}/approve|reject`, `pkg/moderation`)
decides kinds `employer_verification`, `employer_job` and `job_report`. Messages go through `pkg/notify`
(`Direct` for status changes / new applications / reminders, `Digest` — at most one bot message per student per day — for
the 14-day «no answer» message). Everything is free for students; no payments.

### Who is who

| Actor | How the server knows |
|---|---|
| student | any signed-in, consented account |
| employer member | a row in `employer_members` (one company per account). Role `employer` is a label granted on verification; **membership is the authority**. `owner` = one per company |
| university staff | role `university_staff` **and** a `university_staff` row bound by an admin to one university. `users.university` is not trusted (a staff member cannot change it via `PATCH /api/me`) |
| admin | role `admin` |

### Isolation (Go checks + RLS)

- Company data, jobs and applications are read and changed **as `app_user`** (RLS): `app_employer_member(e)` /
  `app_employer_active(e)` (verified company). A member of company B sees 0 rows of company A even with crafted SQL.
- Employers see an application's `sharedFields` snapshot and its status only — no user id, no profile, no other
  applications of the student. The snapshot cannot be changed by the employer (trigger) and is erased when the student withdraws.
- Students apply only through `job_apply()` (security definer): job published + not expired, company verified,
  minors refused on adult-only jobs, not your own company.
- Staff posts are written as `app_user` limited to `app_staff_university()`; statistics are aggregate-only after the same binding check.

### Enums

| Name | Values |
|---|---|
| employer `status` | `pending` (in the queue), `verified` («Тексерілген»), `rejected` (may apply again), `suspended` (admin), `closed` (owner stopped it / owner left alone) |
| job `status` | `pending` (first 3 jobs, in the queue), `published`, `rejected`, `hidden` (`hiddenReason`: `reports` reversible / `admin`), `closed`, `expired` (30 days) |
| job `per` | `shift`, `hour`, `month` (`hours` required for `shift`) |
| job `tags` | `noexp`, `daily`, `remote`, `near` (`remote: true` adds `remote`) |
| job `city` | `content.Cities` (`remote` allowed) |
| application `status` | `sent`, `viewed` (қаралды), `invited` (шақырылды), `rejected` (бас тартылды), `no_response` (14 days, automatic), `withdrawn` |
| report `reason` | `scam` (алаяқтық), `fee` (ақы сұрады), `fake` (сипаттама өтірік), `other` |

## Endpoints

### Students

| Method & path | Who | Purpose |
|---|---|---|
| `GET /api/jobs` | anyone | Real (database) jobs; signed-in students with `isAdult != true` never get adult-only jobs |
| `GET /api/jobs/{id}` | anyone | One real job + `myApplication`; `404 job_not_found` for minors on adult-only jobs |
| `POST /api/jobs/{id}/apply` | signed in, consented | Send an application with chosen fields |
| `POST /api/jobs/{id}/report` | signed in, consented, not a member of that company | Report a job |
| `GET /api/applications/my` | signed in | «Менің өтінімдерім» with real statuses |
| `POST /api/applications/{id}/withdraw` | the applicant | Withdraw (shared copy erased) |
| `GET /api/announcements?university=` | anyone | «Университеттен ресми» announcements |

**Public job** (`/api/jobs`, `/api/jobs/{id}` and `/api/content` → `jobs`): the exact `data/jobs.json` shape
(`id` = `"job-<id>"`, `org` = company name, `contact` = empty, `is_demo` = false, `deadline` = expiry date,
`verified_by` = "Student Hub") plus:

```json
{
  "source": "employer",
  "employer": {"name": "ТОО Кофе", "website": "https://…", "about": "…", "verified": true, "verified_at": "2026-10-28",
               "verification_meaning": {"kk": "Тексерілген: компания ресми тіркелген … кепілдік ЕМЕС.", "ru": "…"}},
  "adult_only": false, "remote": false, "address": "Мәңгілік Ел 10",
  "apply": {"kk": "Өтінім жіберіңіз …", "ru": ""},
  "response": {"applications": 12, "days": 2, "rate": 75},
  "low_response": false
}
```
`response` is `null` until the company has 5 applications (withdrawn excluded); `days` = median days to invited/rejected;
`rate` = % answered (invited+rejected) among decided (invited+rejected+no_response). Companies with `rate < 30` have
`low_response: true` and are sorted to the end. `/api/content` is public and CDN-cached, so it cannot filter by age —
the frontend must hide `adult_only` jobs for students whose `isAdult` is not `true`; `/api/jobs` filters server-side.

**Apply** — `POST /api/jobs/{id}/apply`:
```json
{"share": {"name": true, "course": true, "telegram": true, "skills": ["Excel"],
           "experience": [{"title": "Бариста", "org": "Coffee", "period": "2025"}], "phone": "+77010000000"},
 "letter": "≤ 300 таңба", "confirm": true}
```
Only ticked fields are copied: `name`, `course` (+ `specialty` = faculty) and `telegram` from the account; `skills` (≤ 15 × 40),
`experience` (≤ 10), `phone` (optional, only into this application) and `letter` from the device. Unknown keys (`gpa`, …) → `400 invalid`.
`confirm: true` is the «осы деректер "<компания>" компаниясына жіберіледі» confirmation (show `job.org` before sending).
→ `201 {"application": {"id", "status": "sent", "sharedFields": {...}}, "company": "ТОО Кофе"}`; company members get a direct message.
Errors: `400 confirm_required | nothing_shared | invalid | own_company`, `403 consent_required | minor`, `404 job_not_found`, `409 already_applied`
(one application per job, also after withdrawal).

**My applications** — `GET /api/applications/my` → `{"applications": [{"id", "jobId": "job-12", "title", "titleRu", "company",
"status", "inviteMessage", "sharedFields", "createdAt", "updatedAt"}]}`.

**Withdraw** → `200 {"status": "withdrawn"}`; `404 application_not_found`, `409 application_closed` (already rejected / no_response / withdrawn).

**Report** — `{"reason": "fee", "text": "≤ 300"}` → `200 {"alreadyReported", "hidden"}`. The first open report creates a
`job_report` queue item and messages admins; with 3 counted reporters (accounts ≥ 7 days, shared rule `REPORT_HIDE_AFTER`)
the job is hidden until an admin decides (approve = hidden for good, reject = reports dismissed, job back).
Errors: `400 bad_request | own_job`, `404 job_not_found`.

**Announcements** → `{"announcements": [{"id": "uni-7", "university", "title": {kk,ru}, "body": {kk,ru}, "url", "until",
"publishedAt", "source": "university", "official": true}]}`.

### Employers

| Method & path | Who | Purpose |
|---|---|---|
| `POST /api/employer/apply` | consented, not in a live company | Company application → queue `employer_verification` |
| `GET /api/employer/me` | anyone signed in | My company (`employer: null` if none), role, rules, job counts, response stats |
| `PATCH /api/employer/me` | member, verified | Edit `website`, `contactName`, `contactPhone`, `address`, `about` (name / БСН fixed) |
| `POST /api/employer/rules/accept` | member | One-time rules screen |
| `GET /api/employer/members` | member | Colleagues (`userId`, `name`, `username`, `role`, `joinedAt`, `me`) |
| `POST /api/employer/invites` | owner, verified | One-time 7-day invite code → `{"code", "expiresAt", "link"}` |
| `POST /api/employer/join` | consented | `{"code"}` → join as member |
| `DELETE /api/employer/members/{userId}` | owner | Remove a member |
| `POST /api/employer/leave` | member (owner only after transfer) | Leave |
| `POST /api/employer/transfer` | owner | `{"userId"}` hand the company to a member |
| `POST /api/employer/close` | owner | `{"confirm": "CLOSE"}` stop the company: jobs closed, members removed |
| `GET /api/employer/jobs` | member | All my company's jobs + `applications` counts by status |
| `POST /api/employer/jobs` | member, verified | Create a job |
| `PATCH /api/employer/jobs/{id}` | member, verified | Replace a job's fields (not while pending / rejected / hidden) |
| `POST /api/employer/jobs/{id}/close` | member, verified | Close |
| `POST /api/employer/jobs/{id}/extend` | member, verified | +30 days from now; reopens a closed / expired approved job |
| `GET /api/employer/jobs/{id}/applications` | member, verified | Applications to one job |
| `POST /api/employer/applications/{id}/status` | member, verified | `viewed` / `invited` / `rejected` + message |

**Apply for verification** — `{"name" 2–120, "bin" 12 digits, "website" https:// site or 2GIS link, "contactName" 2–80,
"contactPhone"?, "address"?, "about"? ≤ 1000}` → `201 {"employer": {...}, "verificationMeaning": {kk, ru}}`.
Errors: `400 invalid`, `403 consent_required`, `409 already_member | bin_taken`. Approval → status `verified`,
members get role `employer`, the owner is told what verification means. Rejection → `rejected` with `statusReason`; may apply again.

**Employer JSON**: `{"id", "name", "bin", "website", "contactName", "contactPhone", "address", "about", "status", "statusReason", "verifiedAt", "createdAt"}`.

**Create job** — body:
```json
{"titleKk": "3–120", "titleRu": "", "descKk": "20–3000", "descRu": "", "city": "astana", "remote": false, "address": "≥ 3 unless remote",
 "pay": 9000, "per": "shift", "hours": 6, "schedKk": "3–200", "schedRu": "", "tags": ["noexp"], "skills": ["comm"], "spec": ["all"],
 "req": [{"kk": "…", "ru": "…"}], "applyKk": "қалай өтінім беріледі, 3–500", "applyRu": "", "minorOk": true, "acceptRules": true}
```
`minorOk` («18-ге толмағандарға сай ма?») is required. `acceptRules: true` accepts the rules screen in the same call.
Texts asking for prepayment / deposit / «курс сатып алу» → `400 forbidden_terms`. → `201 {"job": {...}, "pending": true|false}`:
the company's first 3 jobs wait in the queue (`employer_job`), later ones publish at once (enforced by a DB trigger).
Errors: `400 invalid | forbidden_terms`, `403 not_member | not_verified | rules_required`.

**Job JSON (employer)**: input fields + `id`, `status`, `hiddenReason`, `statusReason`, `publishedAt`, `expiresAt`, `createdAt`, `applications`.
Edit / close / extend errors: `404 job_not_found` (also for another company's job), `409 job_status`.

**Applications to a job** → `{"applications": [{"id", "status", "sharedFields", "inviteMessage", "createdAt", "viewedAt",
"respondedAt", "daysWaiting"}]}` — nothing but what the student sent (withdrawn → `{}`).

**Status** — `{"status": "invited", "message": "≤ 500"}` → `200 {"application": {...}}`. `invited` / `rejected` send the student a
direct message (with the message). `viewed` never downgrades an invited application.
Errors: `400 bad_request | invalid`, `404 application_not_found`, `409 application_closed`.

### University staff

| Method & path | Who | Purpose |
|---|---|---|
| `GET /api/university/me` | bound staff | `{"university"}` |
| `GET /api/university/posts` | bound staff | My university's posts |
| `POST /api/university/posts` | bound staff | Publish at once (no queue) |
| `PATCH /api/university/posts/{id}` | bound staff, same university | `{"payload"?, "hidden"?}` |
| `DELETE /api/university/posts/{id}` | bound staff, same university | Delete |
| `GET /api/university/stats` | bound staff | Aggregate numbers |
| `GET /api/university/stats.csv` | bound staff | Same numbers as CSV (`metric,value`) |

Everything else returns `403 not_staff`. Staff never get any student-level endpoint.

**Post** — `{"kind": "opportunity", "payload": {...}}`: the existing opportunity structure (`type`, `title_kk/ru`, `org_kk/ru`,
`desc_kk/ru`, `source_url` https, `city`, `deadline`, `funded`, `free`) plus `docs` and `steps` (`[{kk, ru}]`, ≤ 10).
It appears in `/api/content` → `opportunities` as `{"id": "uni-<id>", …data/opportunities.json shape…, "source": "university",
"official": true, "university": "ENU"}` and drops out after its deadline. Or `{"kind": "announcement", "payload":
{"title_kk", "title_ru", "body_kk", "body_ru", "url"?, "until"?}}` → `/api/announcements`.
Events of their university: the existing events API (prompt 05) — `university_staff` is already an event manager role.

**Stats** (last 90 days for activity):
```json
{"university": "ENU", "periodDays": 90, "students": 37, "weeklyActive": "< 5", "byFaculty": {"IT": 36, "Physics": "< 5"},
 "byCourse": {"2": 37}, "savedDeadlines": 0, "events": 3, "registrations": 41, "checkIns": 30, "ratings": 12, "avgRating": 4.3,
 "mentors": "< 5", "mentorRequests": 6, "mentorPairs": "< 5"}
```
Any count 1–4 is the string `"< 5"`; `avgRating` is `null` with fewer than 5 ratings. No names, no per-student rows.

### Admin

| Method & path | Purpose |
|---|---|
| `GET /api/admin/queue?kind=employer_verification\|employer_job\|job_report` | Shared queue (pkg/moderation) |
| `POST /api/admin/queue/{id}/approve` / `reject` (`{"reason"}`) | Decide |
| `GET /api/admin/employers?status=` | Companies + `members`, `jobs`, `response`, `lowRate` |
| `POST /api/admin/employers/{id}/suspend` / `restore` | `{"reason"}` 3–500, audited; suspended jobs vanish at once |
| `GET /api/admin/university-staff` | Bound staff |
| `POST /api/admin/university-staff` | `{"userId", "university"}` → role `university_staff` + binding (audited) |
| `DELETE /api/admin/university-staff/{userId}` | Unbind, role back to `student` (audited) |

### Daily job

`GET /api/cron/b2b` with `Authorization: Bearer $CRON_SECRET` (in `deploy/studenthub-cron.service`) →
`{"employerReminders", "noResponse", "studentsTold", "studentsDeferred", "jobsExpired", "lowRateWarnings"}`:
1. applications still `sent` after 5 days → one reminder per company to every member (direct);
2. `sent` / `viewed` after 14 days → `no_response`; the student gets «Компания 14 күнде жауап бермеді. Басқа вакансияларды қара»
   through the one-message-per-day rule (deferred students are retried next run);
3. published jobs past `expiresAt` → `expired`;
4. companies with response rate < 30% → admins told (once per 30 days).

## Account deletion and export

Applications (with the shared copy, phone included) cascade with the student's account. A deleting **owner** hands the
company to the longest-standing member automatically (pending queue items move with it); with no members left the company is
`closed` and its jobs closed. Explicit paths: `POST /api/employer/transfer`, `POST /api/employer/close`.
`GET /api/me/export` gains `jobApplications`, `employerMembership`, `universityStaff`.

## How I verify an employer (admin)

1. `#/admin/queue` → kind `employer_verification`. Check the БСН in the official register (stat.gov.kz / egov.kz), open
   the website or 2GIS link, call the contact phone if in doubt.
2. Approve → company `verified`, «Тексерілген» badge, members get role `employer`. Reject with a reason → they may fix and re-apply.
3. «Тексерілген» means *registered company, real contacts* — **not** a job guarantee; this is shown to students next to the badge.
4. Watch `GET /api/admin/employers` (`lowRate`) and `job_report` items; suspend with a reason when needed.

## How I add university staff (admin)

1. The person signs in once (Telegram) and gives consent.
2. Find the user id in `#/admin/users`, then `POST /api/admin/university-staff {"userId", "university"}` (exact university
   name as used in profiles/events). Revoke with `DELETE /api/admin/university-staff/{userId}`.
3. Setting the role via `POST /api/admin/role` alone is not enough — without the binding the cabinet answers `403 not_staff`.

**Legal note**: the "not suitable for minors" rule (night shifts, alcohol sales, heavy work) should be reviewed by a lawyer
against the Labour Code of Kazakhstan before launch.

## Frontend checklist (spec 10)

- [ ] `#/employer/apply`: company name, БСН, site or 2GIS link (https only), contact person, phone/address/about; text explaining
      what «Тексерілген» means (`verificationMeaning`) and that it is not a job guarantee; pending / rejected (reason) states.
- [ ] `#/employer` cabinet: company card + status, response stats, job list with application counts by status,
      members + invite link (owner), leave / transfer / close (owner, with confirm `CLOSE`).
- [ ] Rules screen once before the first job (prohibited: prepayment, deposit, «курс сатып алу»), then `acceptRules`.
- [ ] `#/employer/jobs/new` (and edit): all `JOBS` fields + pay amount and type (shift / hour / month), schedule, exact address or
      «қашықтан», requirements, «қалай өтінім беріледі», required yes/no «18-ге толмағандарға сай ма?»; show «тексеруде» for pending jobs;
      close / extend (+30 days) buttons; expired badge.
- [ ] Applications per job: only `sharedFields`; buttons «Қаралды», «Шақыру» (with message textarea), «Бас тарту»; days waiting.
- [ ] Job lists: merge `/api/content` jobs (`source: "employer"`) with the `JOBS` demo array; demos keep the «үлгі» label;
      existing filters (`noexp`, `daily`, `remote`, `near`) work on `tags`; hide `adult_only` jobs for students whose `isAdult` is not true.
- [ ] `VIEWS.job` for real jobs: «Тексерілген» badge with explanation, address / remote, «Әдетте N күнде жауап береді» and
      «өтінімдердің X%-ына жауап берді» when `response` is not null, «Шағым» button (reason picker).
- [ ] «Өтінім жіберу» on a real job: sign-in required → checklist of fields (name, course + specialty, skills, experience,
      Telegram username, phone optional, letter ≤ 300) → confirm «осы деректер "<компания>" компаниясына жіберіледі» → send.
      Demo jobs keep the local `S.applied` behaviour.
- [ ] `VIEWS.mine`: real application statuses (sent / viewed / invited + message / rejected / no_response / withdrawn) and «Кері қайтару».
- [ ] `#/university` (staff): publish announcement / opportunity (with `docs`, `steps`, `deadline`), list / hide / delete own posts,
      link to the events organizer screens, statistics with «< 5», CSV download.
- [ ] Show «Университеттен ресми» badge for `official: true` opportunities and on announcements.
- [ ] Admin: `#/admin/queue` kinds `employer_verification`, `employer_job`, `job_report`; employers list with suspend / restore;
      university staff binding screen.
