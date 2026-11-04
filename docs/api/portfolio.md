# Portfolio API — public portfolio page (prompt 09)

Server: `backend/internal/portfolio`, wired in `backend/internal/server/routes_portfolio.go`. Schema: `backend/migrations/20261028000010_portfolio.sql`.
Spec: `docs/prompts/09-portfolio.md`.

## Conventions

Switch `FEATURE_PORTFOLIO` (off → every route below is `404 not_found`, byte-identical to an unknown `/api` route).
Session cookie or `Authorization: Bearer`; writes need an allowed `Origin` and are rate limited (`RATE_WRITE_PER_MIN`).
Errors are `{"error": {"code", "message"}}` with the message in kk/ru (`?lang=` / `Accept-Language`) — branch on `code`.
Keys are camelCase.

Every student's public page is **off by default**; the student turns it on. The slug is 10 random characters
(`[a-z0-9]`, not the username), so pages cannot be enumerated.

### Honesty rule ("Расталған ✓")

`verified: true` is produced **only** from system tables:

| Item | Source | Verify link (`verifyUrl`) |
|---|---|---|
| Event attended | `verified_attendance` view = `event_regs.checked_in_at` (QR / organizer check-in) | event page `/#/event/<id>` |
| Event certificate | `event_certificates` (issued when a `gives_cert` event is finished) | event page |
| Ambassador certificate | `ambassador_terms.cert_code` (issued by admin) | `/#/certificate/<code>` |
| «Ментор 🎓» | `user_badges` with `verified = true` (mentee rated the finished pair 4–5) | — |

Everything else is `verified: false` («өзі көрсеткен»): self-declared certificates, experience, skills
(`portfolio_items`, which has **no** verified column) and unverified community badges (e.g. `generous` from the market).
No endpoint accepts a `verified` field; crafted SQL as the student is refused (no column, no grant on the system tables).

GPA, phone and email are **never** stored or shown — there is no field and no toggle; `gpa`, `phone`, `email`,
`showGpa`, `showPhone`, `showEmail` in a PATCH answer `400 not_public`.

## Endpoints

| Method & path | Who | Purpose |
|---|---|---|
| `GET /api/portfolio/me` | signed in, consented | Settings (created on first call, disabled), synced items, live preview |
| `PATCH /api/portfolio/settings` | signed in, consented | Toggle page / blocks, name, headline, bio, sync consent |
| `POST /api/portfolio/slug` | signed in, consented | «Жаңа сілтеме жасау» — new slug, the old one is 404 at once |
| `PUT /api/portfolio/items` | signed in, consented | Replace synced self-declared items (needs sync consent) |
| `DELETE /api/portfolio/items` | signed in | Withdraw sync consent, delete synced items |
| `GET /api/p/{slug}` | **anyone, no auth** | Public page JSON for the Next.js page `/p/<slug>` |

### `GET /api/portfolio/me`

```json
{
  "settings": {
    "enabled": false, "slug": "k3m9x2p7qa", "url": "https://…/p/k3m9x2p7qa",
    "displayName": null, "headline": null, "bio": null,
    "showUniversity": true, "showCourse": true, "showEvents": true, "showCerts": true,
    "showExperience": true, "showSkills": true, "showBadges": true, "showContact": false,
    "syncConsentAt": null, "slugRotatedAt": null, "updatedAt": "…"
  },
  "items": [{"kind": "skill", "title": "Go", "org": "", "period": "", "desc": "", "url": ""}],
  "preview": { /* same shape as GET /api/p/{slug}, built even while disabled */ }
}
```
`403 consent_required` before the consent screen.

### `PATCH /api/portfolio/settings`

Any subset of:

| Field | Type | Rule |
|---|---|---|
| `enabled` | bool | the page is public only while true |
| `displayName` | string \| null | ≤ 80; null → the account name |
| `headline` | string \| null | ≤ 80 |
| `bio` | string \| null | ≤ 300, newlines allowed |
| `showUniversity`, `showCourse`, `showEvents`, `showCerts`, `showExperience`, `showSkills`, `showBadges`, `showContact` | bool | block toggles (`showContact` default false) |
| `syncConsent` | bool | answer to «тәжірибе, дағдылар, сертификаттарды аккаунтқа сақтаймыз ба?»; `false` deletes the synced items |

→ `200 {"settings": {...}}`. Errors: `400 bad_request` (empty body), `400 unknown_field`, `400 invalid_<field>`, `400 not_public`.

Block → section mapping: `showEvents` → verified events; `showCerts` → event + ambassador certificates and
self-declared certificates; `showBadges` → mentor badge and community badges; `showExperience`, `showSkills` →
self-declared experience / skills.

### `POST /api/portfolio/slug`

No body. → `200 {"settings": {...}}` with the new `slug`/`url`; `slugRotatedAt` set. The old link answers the neutral 404 immediately.

### `PUT /api/portfolio/items`

```json
{"consent": true, "items": [
  {"kind": "cert", "title": "Coursera: Go", "org": "Coursera", "period": "2025", "desc": "", "url": "https://coursera.org/…"},
  {"kind": "experience", "title": "Бариста", "org": "Coffee", "period": "2025–2026", "desc": "…"},
  {"kind": "skill", "title": "Excel"}
]}
```
`kind` ∈ `cert | experience | skill`; `title` 1–120, `org` ≤ 120, `period` ≤ 40, `desc` ≤ 300, `url` empty or `https://` ≤ 500;
at most 60 items; unknown keys (`verified`, `gpa`, …) → `400 unknown_field`. Replaces the whole list (the device is the source).
`consent: true` grants the sync consent in the same call; without it and without an earlier consent → `409 sync_consent_required`.
→ `200 {"items": [...]}`.

### `GET /api/p/{slug}` — the public page

One GET, no auth, server-renderable. Headers: `Cache-Control: no-store`, `X-Robots-Tag: noindex, nofollow`.
Rate limit 60/min per IP (`429 rate_limited`).

**Neutral 404**: disabled page, unknown slug, old (rotated) slug, malformed slug, deleted/blocked account and switch off all
answer exactly `404 {"error":{"code":"not_found","message":"Мұндай бет жоқ."}}`.

```json
{
  "noindex": true,
  "profile": {"name": "Айгерім", "headline": "Backend стажер", "bio": "…", "university": "ENU", "course": 3},
  "verified": {
    "events": [{"title": {"kk": "Хакатон", "ru": ""}, "date": "…", "organizer": "ENU", "verifyUrl": "https://…/#/event/12", "verified": true}],
    "certificates": [
      {"kind": "event", "title": {"kk": "Хакатон", "ru": ""}, "issuer": "ENU", "issuedAt": "…", "verifyUrl": "https://…/#/event/12", "verified": true},
      {"kind": "ambassador", "title": {"kk": "Student Hub амбассадоры", "ru": "Амбассадор Student Hub"}, "issuer": "ENU", "issuedAt": "…", "verifyUrl": "https://…/#/certificate/abcd…", "verified": true}
    ],
    "badges": [{"badge": "mentor", "awardedAt": "…", "verified": true}]
  },
  "selfReported": {
    "certificates": [{"title": "Coursera: Go", "org": "Coursera", "period": "2025", "desc": "", "url": "https://…", "verified": false}],
    "files": [{"title": "IELTS Academic 7.5", "issuer": "British Council", "issuedOn": "2026-05-12", "category": "language",
               "credentialUrl": "https://…", "kind": "pdf", "bytes": 182344, "width": null, "height": null,
               "url": "/api/portfolio/files/3f…a1.pdf", "verified": false}],
    "experience": [], "skills": [],
    "badges": [{"badge": "generous", "awardedAt": "…", "verified": false}]
  },
  "contact": {"telegramUrl": "https://t.me/aigerim"},
  "og": {"title": "Айгерім — Student Hub портфолиосы", "description": "Backend стажер", "image": "https://…/og.png", "url": "https://…/p/k3m9x2p7qa"},
  "site": "https://…"
}
```
- `university` / `course` are `null` when hidden; `contact` is `null` unless `showContact` is on and the account has a Telegram username.
- The body never contains a user id, Telegram id, internal row id, slug field, GPA, phone or email (tested).
- All text is the student's raw input — **render it as text** (React escapes it; never `dangerouslySetInnerHTML`). The JSON
  encoder already escapes `<`, `>`, `&`.

## Certificate files

The student attaches a certificate as a **PDF (≤ 10 MB)** or a **photo (JPG / PNG / WebP ≤ 8 MB)**. Code:
`backend/internal/portfolio/certfiles.go`, table `portfolio_certificates` (migration `20261105000050`, RLS: owner only).

- **Validation by bytes, never by name or Content-Type**: `%PDF-1.`/`%PDF-2.` at byte 0 and `%%EOF` in the last 2 KB, or a
  JPEG / PNG / WebP signature. An `.exe`, `.html` or `.svg` renamed to `.pdf` is `415 cert_file_type`; a truncated PDF is
  `400 cert_invalid`; too big is `413 cert_too_large`.
- **Images are re-encoded** on the server to JPEG, longest side ≤ 1600 px: EXIF (GPS), XMP, ICC and comments are dropped.
  The browser already shrinks photos to a ≤ 1600 px WebP before upload. PDFs are stored as uploaded (never parsed).
- Files live in `$UPLOAD_DIR/certs/<32 hex>.pdf|.jpg` (random names). Delete removes the file; account deletion removes all.
- **Quota**: 30 files and 100 MB per student (`409 cert_quota_files` / `cert_quota_bytes`), enforced by a database trigger
  under a per-user advisory lock. Uploads: 30 per hour per student (`PORTFOLIO_CERTS_PER_HOUR`).
- **Visibility**: every file is private (`public: false`) until the student turns its flag on. A public file is served to
  others only while the public page is enabled with the certificates block (`showCerts`) on; otherwise only the owner gets it.

| Method | Path | Body / answer |
|---|---|---|
| `GET` | `/api/portfolio/certificates` | `{certificates: Cert[], usage: {files, bytes, maxFiles, maxBytes}, pageVisible}` |
| `POST` | `/api/portfolio/certificates` | multipart: `file` + `title` (1–120), `issuer` (≤120), `issuedOn` (YYYY-MM-DD), `credentialId` (≤100), `credentialUrl` (https, ≤500), `category` → `201 {certificate, usage}` |
| `PATCH` | `/api/portfolio/certificates/{id}` | JSON, any subset of the fields above and `public` (bool) → `{certificate}`; someone else's id is 404 |
| `DELETE` | `/api/portfolio/certificates/{id}` | `{deleted: true, usage}` — the file is removed |
| `GET` | `/api/portfolio/files/{name}` | the file: owner always; others only when public on an enabled page, else `403 forbidden`; unknown name 404 |

`category` ∈ `course`, `olympiad`, `competition`, `volunteering`, `language` (IELTS, TOEFL, Kaztest…), `other`.
`Cert` = `{id, title, issuer, issuedOn, credentialId, credentialUrl, category, kind: "pdf"|"image", mime, bytes, width, height, public, url, createdAt}`.

The file response: `Content-Type: application/pdf` or `image/jpeg`, `X-Content-Type-Options: nosniff`,
`Content-Disposition: inline; filename="<title>.pdf"` (RFC 2231 for non-ASCII titles),
`Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'self'; sandbox`,
`Cross-Origin-Resource-Policy: same-origin`, `X-Robots-Tag: noindex`; `Cache-Control: private, no-store` (owner) or `no-cache` (public).
nginx allows an 11 MB body on `= /api/portfolio/certificates` only (`docker/nginx*.conf`).

Signed out, the app keeps the file on the device (IndexedDB `studenthub-certs`); signing in lets the student upload it.

## Account deletion and export

`portfolio_settings` and `portfolio_items` cascade with the account — the page is 404 the moment the account is deleted.
`GET /api/me/export` gains `portfolioSettings`, `portfolioItems` and `portfolioCertificates` (metadata; files are deleted
with the account by a delete hook).

## Frontend checklist (spec 09)

- [ ] `VIEWS.portfolio` → «Жария портфолио» section: on/off switch, a toggle per block (university, course, events,
      certificates, experience, skills, badges, contact), display name / headline (80) / bio (300).
- [ ] When the student first turns the page on: the explicit question «тәжірибе, дағдылар, сертификаттарды аккаунтқа сақтаймыз ба?»
      → yes: `PUT /api/portfolio/items` with `consent: true` and the local list; no: page shows verified items only.
      Re-sync after local edits (PUT replaces the list). «Сақтауды тоқтату» → `DELETE /api/portfolio/items`.
- [ ] Buttons: «Сілтемені көшіру» (`settings.url`), «Бөлісу» (Web Share / Telegram share), «QR-код» (`qrSvg(settings.url)`),
      «Жаңа сілтеме жасау» (`POST /api/portfolio/slug`, with a confirm: the old link stops working).
- [ ] Preview of the page from `preview` (same component as the public page).
- [ ] Next.js page `/p/[slug]`: server-render from `GET /api/p/{slug}` with `cache: "no-store"`; on 404 render the neutral
      «Мұндай бет жоқ» page with HTTP 404; `<meta name="robots" content="noindex">` (`noindex` is always true);
      `og:title` / `og:description` / `og:image` / `og:url` from `og`.
- [ ] Page layout top → bottom: name, headline, university + course, bio; «Расталған жетістіктер ✓» (events with date and
      organizer, certificates, ambassador certificate, «Ментор 🎓») each with its `verifyUrl`; «Өзі көрсеткен» (certificates,
      experience, skills, community badges such as «Жомарт»); legend «✓ — Student Hub жүйесі арқылы расталған. Қалғанын студенттің өзі көрсеткен»;
      «Telegram арқылы жазу» only when `contact` is not null; footer «Student Hub — Сейфуллин университеті студенттеріне арналған» + site link.
- [ ] Print CSS (Ctrl+P → 1–2 clean pages, buttons hidden, links printed as text); mobile + desktop.
- [ ] `VIEWS.resume`: checkbox «Түйіндемеге портфолио сілтемесін қосу» (uses `settings.url`; only offer it while `enabled`).
- [ ] Never send or display GPA / phone / email in this feature.
