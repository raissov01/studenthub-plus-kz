# Admin panels: research for the Student Hub role-scoped console

Date: 2026-11-05. Scope: super admin, moderators, café/print managers, club admins,
university staff, employers. Target: 390px phone + laptop, flat style (white, pastel
accents, 1px borders, no gradients/blur/emoji).

## 0. Method and caveats

- **Reddit access.** `old.reddit.com/.../search.json` returned `302` to a login page
  (`reason=lor2`) for anonymous requests, and Reddit is blocked for our web-search tool.
  All Reddit material below was therefore fetched through the **Arctic Shift** archive
  API (`/api/posts/search` by subreddit + title, `/api/comments/tree?link_id=...`),
  at roughly one request every 5 s. Many title searches on large subreddits timed out
  (`"Timeout. Maybe slow down a bit"`), so coverage is partial and skews to r/django and
  r/UXDesign.
- Quotes are verbatim, cut short with `...` where trimmed. Dates are the post's
  `created_utc` (UTC). Upvote counts are omitted on purpose: archive snapshots are not
  reliable.
- Docs were accessed on 2026-11-05. Two official pages (Discord support, MoodleDocs)
  returned `403` to our fetcher; where that happened it is stated and the claim is kept
  minimal.

## 1. What Reddit says makes internal tools good or bad

| # | Thread | Date | Takeaway |
|---|--------|------|----------|
| R1 | [r/django: What is the purpose or a use-case of a Django admin?](https://www.reddit.com/r/django/comments/1f9wqog/what_is_the_purpose_or_a_usecase_of_a_django_admin/) | 2024-09-05 | Generic CRUD admin is a huge time saver for staff work |
| R2 | [r/django: How you customise & simplify the Django admin so your non tech savvy client can use it like a CMS?](https://www.reddit.com/r/django/comments/1ed2ifj/how_you_customise_simplify_the_django_admin_so/) | 2024-07-26 | Do not bend a dev CRUD admin into a non-technical tool |
| R3 | [r/django: How would you provide an alternative to the default django admin?](https://www.reddit.com/r/django/comments/1f89or7/how_would_you_provide_an_alternative_to_the/) | 2024-09-03 | Build scoped task pages per role instead |
| R4 | [r/UXDesign: How would you design a bulk action in a table?](https://www.reddit.com/r/UXDesign/comments/12mw6lf/how_would_you_design_a_bulk_action_in_a_table/) | 2023-04-15 | Disable ineligible bulk actions with a reason, or filter first |
| R5 | [r/UXDesign: Bulk action request flow](https://www.reddit.com/r/UXDesign/comments/15xgixt/bulk_action_request_flow/) | 2023-08-21 | Keep group selection on one screen + search |
| R6 | [r/UXDesign: Fintech admin dashboard, CTO wants a drop down menu ...](https://www.reddit.com/r/UXDesign/comments/13ydxlt/i_am_working_on_a_fintech_admin_dashboard_and_my/) | 2023-06-02 | Role/task-based views and saved views are expected |
| R7 | [r/django: Opinion On A New Django Admin Interface](https://www.reddit.com/r/django/comments/1ju78w1/opinion_on_a_new_django_admin_interface/) | 2025-04-08 | Charts dashboard vs operational admin are different jobs; mobile asked for |
| R8 | [r/django: django-daisy, a fully mobile-friendly Django admin](https://www.reddit.com/r/django/comments/1gfi47p/ann_djangodaisy_a_fully_mobilefriendly_django/) | 2024-10-30 | Demand for mobile admin; public demo credentials get abused |
| R9 | [r/UXDesign: Dashboard examples](https://www.reddit.com/r/UXDesign/comments/w1qo2v/dashboard_examples/) | 2022-07-18 | Dashboards should say "so what", not just show numbers |
| R10 | [r/sysadmin: Assistance interpretting Audit Log following a breach](https://www.reddit.com/r/sysadmin/comments/1d2ekcb/assistance_interpretting_audit_log_following_a/) | 2024-05-28 | Audit logs full of raw IDs are useless under pressure |

Key quotes (verbatim, short):

- R1: "Instead of developing a ui for them, you can just use the Django admin panel and
  customize only what you need." Another reply: "My company has 40 CSRs and content
  moderators that use the django admin as part of their workflow."
- R2 (on over-customizing a generic admin): "Pretty soon, admin.py will look like a
  patchwork of fugly nested mixins, the Admin will still be ugly, pages will load in 13
  seconds, the client will be unhappy..." Also: "Would never recommend giving the a
  non-tech savvy client access to Django Admin."
- R3: "...just create the views you need as regular Django pages, restrict them so that
  only the intended users can access them..." and the counterpoint: "Django Admin + a few
  plugins + permissions go a long way. ... Building a custom dashboard is significantly
  more expensive."
- R4: "...disable restock if non supported categories are selected and use a tooltip to
  communicate why it's disabled." Alternative: "Allow filtering by one brand, then show a
  restock option upon select all?"
- R5: "...the ability to see everything on one screen (meaning, not having to back out of
  a screen to select a different group) would be much easier..."
- R6: "So he wants a filter and/or saved views? Pretty standard for data displays." and
  "To have different views based on role or task? This is common for financial apps..."
- R7: "I wanna see how it looks on mobile. Dashboard with all these charts and admin have
  2 separate purposes."
- R9: "...what are they doing based on the data? Not display just data, but the data is
  X, and now what?"
- R10 (OP): "...nothing that's immediately useful as it's all message ID's without
  subjects, email addresses etc..." A reply: "Audit logs are better for telling when
  accounts logged in and from where."

**Synthesis.** (a) Staff tools win by being task-shaped, not table-shaped: a café manager
needs "today's orders", not a CRUD grid of `orders`. (b) A generic super-admin CRUD
layer is still worth having for the one technical operator. (c) Bulk actions must explain
why they are disabled. (d) Role/task views and saved filters are table stakes. (e) Audit
entries must store human-readable snapshots (names, titles), not only IDs.

## 2. Reference panels and the pattern each one teaches

All accessed 2026-11-05.

1. **Django admin** — changelist = `list_display` + `list_filter` ("filters in the right
   sidebar") + `search_fields` + `list_editable` + `date_hierarchy`;
   `show_full_result_count` renders "99 results (103 total)".
   [ModelAdmin options](https://docs.djangoproject.com/en/5.2/ref/contrib/admin/).
   Actions receive "A QuerySet containing the set of objects selected by the user"; "the
   built-in delete action asks for confirmation"; actions can be gated with
   `@admin.action(permissions=["change"])`.
   [Admin actions](https://docs.djangoproject.com/en/5.2/ref/contrib/admin/actions/).
   Django itself says the admin's use "is limited to an organization's internal
   management tool".
2. **Shopify admin / Polaris** — define what "all" means: "all selectable resources on the
   loaded page, all loaded resources, or every result matching the current query"; "State
   whether selection covers this page or every matching result"; "Don't represent every
   result as IDs loaded into the browser."
   [Select-all actions](https://shopify.dev/docs/apps/build/app-home/migrate-from-polaris-react/select-all-actions).
   Narrow layout: "The table header row isn't rendered in the narrow list layout, so
   don't put the only sort control in a header"; "After failure, preserve selection so
   that the user can retry"; "Applied filters render as removable ... chip elements."
   [Index table](https://shopify.dev/docs/apps/build/app-home/migrate-from-polaris-react/index-table).
   Saved views as tabs on Orders (filters + columns + sort):
   [Searching and viewing orders](https://help.shopify.com/manual/orders/search-view-print-orders).
3. **Stripe Dashboard** — one global search with field filters (`email:`, `status:`,
   `amount:>149.99`, `is:refunded`, negation with `-`), and "As search terms are included
   in the URL, you can bookmark the search or share it with other team members".
   [Dashboard search](https://docs.stripe.com/dashboard/search).
4. **Strapi** — default roles Author ("create and manage their own content"), Editor
   ("manage and publish any content"), Super Admin; conditions "the administrator must be
   the creator"; field-level permission boxes.
   [RBAC](https://docs.strapi.io/cms/features/rbac). Audit log: action, date, user; filter
   by action type, user, date range; detail modal with JSON payload; "The retention period
   defaults to 90 days." [Audit logs](https://docs.strapi.io/cms/features/audit-logs).
5. **Directus** — policies are additive ("each policy can add to existing permissions, but
   not take them away"); item rules like `user_id = $CURRENT_USER.id`; per-action field
   sets. [Access control](https://directus.com/docs/guides/auth/access-control).
   Direct analog for "club admin sees only their club", "café manager only their café".
6. **Retool** — table with sort/filter/paginate/download, server-side pagination,
   "Hide column", expandable rows.
   [Table guide](https://docs.retool.com/apps/guides/data/table).
7. **Discord** — audit log entry = `user_id`, `action_type`, `target_id`, `changes`,
   `reason` ("Reason for the change (1-512 characters)"), passed via
   `X-Audit-Log-Reason`; "All audit log entries are stored for 45 days."
   [Audit Log resource](https://docs.discord.com/developers/resources/audit-log).
   AutoMod: block, alert, or time out; alerts go to a mods-only channel
   ([AutoMod FAQ](https://support.discord.com/hc/articles/4421269296535), 403 to our
   fetcher; summary via search result).
8. **Telegram** — supergroup/channel "Recent actions" admin log, filterable by event
   type and by a query string.
   [Admin log](https://core.telegram.org/api/recent-actions).
9. **Vercel** — narrow roles (Owner, Member, Developer, Security, Billing, Viewer,
   Contributor; "Contributors have no access to projects unless explicitly assigned");
   recommends at least two owners; role changes are owner-only.
   [Access roles](https://vercel.com/docs/rbac/access-roles). Activity Log lists all team
   events chronologically. [Activity Log](https://vercel.com/docs/observability/activity-log).
10. **Linear** — "Use the command menu to open pages or run actions without leaving your
    keyboard" (Cmd/Ctrl+K); "Use `/` to search across issues, projects, and documents",
    including by identifier.
    [Navigating Linear](https://linear.app/enablement/guides/navigating-linear).
11. **Canvas LMS** — "Act as user" needs the "Users - Act as" permission, a confirmation
    page before proceeding, and "To act as another admin, you must have all the
    permissions enabled in your account that have been enabled for that admin" (no
    privilege escalation via impersonation).
    [How do I act as another user](https://community.canvaslms.com/docs/DOC-3113)
    (direct fetch redirected to a 404; text taken from the search index).
    MoodleDocs "Override permissions" / "Check permissions" (Inherit/Allow/Prevent/
    Prohibit) is the EDU reference for a "why can this user do X?" screen
    ([Override permissions](https://docs.moodle.org/310/en/Override_permissions), 403 to
    our fetcher).
12. **NN/g on confirmations** — "if you warn people too much, they stop paying attention";
    buttons should "summarize what will happen"; "do go to great lengths to provide
    undo"; typed confirmation only for rare, high-stakes cases.
    [Confirmation dialogs](https://www.nngroup.com/articles/confirmation-dialog/).

## 3. Concrete patterns for Student Hub

### 3.1 Navigation per role
- One `/admin` shell; the sidebar (laptop) / bottom tab bar + "More" sheet (phone) is
  **built from the user's grants**, not hidden with CSS. Server checks every route.
- Each role lands on a **task home**, not a generic dashboard: moderator -> queue with
  counts; café/print manager -> today's orders; club admin -> my club; staff -> my
  surveys; employer -> my vacancies/applicants; super admin -> numbers + recent audit.
- Users with several grants get a scope switcher at the top ("Café: Korpus 1", "Club:
  Robotics"), like Stripe's account switcher. Scope is in the URL.

### 3.2 Tables
- Server-side filter/sort/paginate; filter state in the URL (Stripe) so it can be
  bookmarked and shared.
- Applied filters as removable chips (Polaris). Search box + 3-5 facet filters max.
- **Saved views as tabs** (Shopify): ship 2-4 preset views per role ("Pending", "Reported
  today", "AI-found, unreviewed") before building user-defined views.
- Result count always visible, Django-style "99 results (103 total)".
- Bulk actions: bar replaces the filter row when >=1 selected; "Select all 240 matching"
  link after page select-all, stored as **the query, not IDs** (Polaris); action
  disabled with a tooltip reason when ineligible (R4); confirm **with the count**
  ("Publish 37 items"); on partial failure keep the selection and show "34 published, 3
  failed" with retry.
- Column chooser + sticky header + sticky first column on laptop only; low priority.

### 3.3 Detail drawer / side panel
- Row click opens a right drawer (laptop) or full-screen sheet (phone) with the record,
  its actions and its own audit trail; URL updates (`?id=`) so back button closes it.
  Moderation: J/K to move to next item without closing the drawer.

### 3.4 Command palette
- Ctrl/Cmd+K: jump to page, find a user by name/email/ID, run allowed actions. Only
  shows commands the user's grants allow. Laptop-only; on phone the search field is
  enough.

### 3.5 Audit log
- Append-only table: `actor_id`, actor name snapshot, `action`, `target_type`,
  `target_id`, **target label snapshot** (R10), `changes` (before/after JSON), `reason`,
  scope (café/club id), IP/user agent, `created_at`.
- UI: newest first, filter by actor / action / target / date (Strapi, Discord), row ->
  drawer with diff. Per-record "History" tab reuses the same query.
- Retention: keep moderation and role changes indefinitely; Discord keeps 45 days,
  Strapi defaults to 90 — both too short for a university.
- Export CSV of the current filter.

### 3.6 Roles and permissions UX
- Least privilege by default: scoped grants (`moderator`, `cafe_manager:<cafe_id>`,
  `club_admin:<club_id>`, `staff:<dept>`, `employer:<company>`), additive like Directus
  policies; item rules like `$CURRENT_USER`.
- Granting/revoking requires a **reason** (Discord `reason` field) and shows "granted by X
  on date, reason" on the user's page. Optional expiry for temporary moderators.
- Only super admin changes roles (Vercel: role changes are owner-only); keep at least two
  super admins (Vercel recommends two owners); block removing the last one.
- No impersonation in v1. If added later, follow Canvas: separate permission, cannot act
  as someone with more rights, banner while active, logged.
- "Why can this user do X?" read-only explainer (Moodle "Check permissions") —
  cheap to add on the user detail page as a list of grants.

### 3.7 Empty, loading, error states
- Empty states say what will appear and the next step ("No orders yet today. New orders
  arrive here automatically").
- Skeleton rows matching the final layout; never a blank page. Errors inline in the
  table area with retry, keep filters. Polaris: test all three at narrow and wide sizes.
- Dashboard numbers show "as of hh:mm" and a link to the underlying filtered list
  (answers R9's "now what?").

### 3.8 Mobile admin (390px)
- Tables become **cards** (title, status pill, 2-3 key fields, overflow menu). Sort
  control lives in the toolbar, not a header (Polaris).
- Filters open in a **bottom sheet**; applied filters stay as a horizontal chip row.
- Selection via long-press or a "Select" toggle; a **sticky bottom bulk-action bar**
  ("12 selected · Publish · Reject · ...") above the safe area.
- Primary per-row action reachable with the thumb (e.g. "Ready" on a café order card).

### 3.9 Destructive actions
- Prefer undo/soft delete (toast with "Undo", 10 s) for reversible ops (hide post,
  reject item). Confirm dialogs with specific verbs and counts ("Delete 3 surveys").
- Typed confirmation (type the club name) only for rare, irreversible ops: delete club,
  revoke last admin, purge user data (NN/g).
- Reason field required for moderation removals and bans; reason is shown to the author
  where appropriate and stored in the audit log.

### 3.10 Keyboard shortcuts
- `Ctrl/Cmd+K` palette, `/` focus search, `J/K` next/prev in queue, `A` approve, `R`
  reject (with reason prompt), `X` select row, `?` shortcut sheet. Off when typing in
  inputs. Laptop-only.

## 4. Ranked ideas for our scope

Cost: S = under a day, M = 1-3 days, L = a week or more.

| Rank | Idea | Fit | Cost | Why |
|------|------|-----|------|-----|
| 1 | Grant-driven nav + task home per role | High | M | Core of a role-scoped console; R3, R6 |
| 2 | Append-only audit log with label snapshots + reason | High | M | Required for moderator/role grants; R10, Discord |
| 3 | Role grants with reason, granted-by, scope, last-admin guard | High | M | Least privilege for café/club/staff/employer; Strapi, Directus, Vercel |
| 4 | Server-side tables with URL filters, chips, counts | High | M | Every role lists something; Stripe, Polaris |
| 5 | Cards + bottom-sheet filters + sticky bulk bar at 390px | High | M | Managers work on phones at the counter; R7, R8 |
| 6 | Bulk publish/reject with "select all N matching", count confirm, partial-failure report | High | M | AI-found feed items come in batches; Polaris, R4 |
| 7 | Preset saved views as tabs (2-4 per role) | High | S | Cheap and covers most of "saved views"; Shopify, R6 |
| 8 | Detail drawer with per-record history | High | S | Reuses audit log; fast moderation |
| 9 | Undo toasts + specific confirm labels; typed confirm only for irreversible | High | S | NN/g; avoids confirm fatigue |
| 10 | Empty/loading/error states + "as of" on dashboard numbers | High | S | Real numbers must be trustworthy |
| 11 | CSV export of current filter (super admin, café revenue) | Medium | S | Asked for; reuse table query |
| 12 | Cmd+K command palette (pages + user lookup) | Medium | M | Great for super admin/mods on laptop, no value on phone |
| 13 | Moderation keyboard shortcuts (J/K/A/R) | Medium | S | Big win for mods with long queues only |
| 14 | "Why can this user do X" grant explainer | Medium | S | Support aid; Moodle pattern |
| 15 | User-defined saved views, column chooser, sticky columns | Low | M | Power-user polish; presets cover v1 |
| 16 | Impersonation ("act as") | Low | L | Risky; needs banner, logging, no escalation (Canvas) |
| 17 | Rule-based auto-moderation (AutoMod-style keywords) | Low | L | Useful later; AI feed already filters; tuning cost |
