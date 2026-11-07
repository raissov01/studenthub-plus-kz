# What the Student Hub admin takes from the kurs admin panel

Date: 2026-11-08. Source: `~/kurs/web/src/pages/admin/*` and `~/kurs/api/internal/*` (the owner's course-centre
platform, read only). Target: the Student Hub console (`/admin`), a university student platform with Кофехана,
print shops, clubs, events, feeds and moderation. Code was re-written for our stack (Next.js 16 static screens,
Go + Postgres RLS, `tt(kk, ru)`, flat tokens); nothing was copied across with its kurs-specific data model
(groups, lessons, tiyn balances, teachers).

Verdicts: **take** (the pattern fits as is), **adapt** (the idea fits, our data is different), **skip**.

| kurs feature | Where in kurs | Verdict | Why |
|---|---|---|---|
| KPI strip: one ruled row of figures with links to the filtered list | `DashboardPage` | take | Same need: «how many, and where do I click». Used on the payments and attendance screens. |
| Flat SVG charts with a chart/table toggle (column + line, hover/keyboard tooltip, direct labels) | `schedule/charts.tsx` | take | Our overview only had tiny bars; these are accessible and dependency-free. Ported to `components/charts.tsx`. |
| Revenue by month chart | `DashboardPage` | adapt | Our money is café / print sales by day (Kaspi or at the counter), not course fees by month. |
| Attendance by week line chart | `DashboardPage` | adapt | We have QR check-ins on events: registered vs checked in per week. |
| Today's sessions list | `DashboardPage` | adapt | Becomes «this week» on the calendar; the overview already lists what waits for a decision. |
| Student list with debt filter, add student | `StudentsPage` | skip | Our users sign themselves up (Telegram / email); the directory already searches and filters. |
| Student drawer: contacts, groups, attendance, balance, payments, notes | `crm/StudentDrawer` | adapt | The user page gets tabs: profile, activity, orders, reports, notes, history. No balance or groups here. |
| Internal notes on a person | `StudentDrawer` notes field | adapt | A list of dated notes with an author (not one free text field), admin only, never shown to the student. |
| Accept a payment by hand | `crm/PaymentModal` | skip | Money goes to the shop's own Kaspi or the counter; the platform never takes cash. |
| Payments list with filters, debtors | `PaymentsPage` | adapt | Café + print orders with filters (shop, status, payment, dates), revenue by day, and «money returned by hand» for refunds Kaspi could not do. No debts. |
| Staff list: role, active toggle, invites by link | `StaffPage` | adapt | Our «Рөл беру» dialog and email invites (pending role on first sign-in) cover it; «Кім нені басқарады» lists everyone. |
| Week grid (drag to move, click a slot to create) + phone agenda | `SchedulePage`, `schedule/WeekGrid`, `Agenda` | take | Events and club meetings on a calendar. Reuses the event endpoints, so the same university / club rights apply. |
| Session dialog with repeat rules and conflict check | `schedule/SessionDialog` | adapt | Our event form (title, category, place, seats) opens prefilled from the slot; no weekly series, no room conflicts (events may overlap). |
| Session details: status, cancel, link to attendance | `schedule/SessionDetails` | adapt | Event details: status, counts, «publish», «edit», «open check-in». |
| Attendance marking per student | `AttendancePage` | adapt | Our attendance is QR self check-in; the new screen is the overview (rate per event and per week) and links to the existing check-in page for manual marks. |
| Leads kanban (CRM sales funnel) | `LeadsPage` | skip | No sales funnel. Partner companies already have the employer application and review (`/admin/employers`). |
| Audit page with actor / entity filters | `AuditPage` | skip | Ours exists (`/admin/audit`, «Өзгерістер тарихы») with actor, action, target and dates. |
| Org settings (name, currency, charge rules) | `SettingsPage` | adapt | Platform settings: feature switches (read only, set by the server), support contacts and hours shown to students. |
| Locale-proof date/time fields | `fields.tsx` | skip | Our forms already use Astana time with `datetime-local`; the calendar shows 24-hour times itself. |
| Saved filters | — (kurs keeps filters in the URL) | adapt | Named saved filters per admin for users and payments (research doc admin-panels.md §3.2). |

## Built from this list

- Overview: sign-ups, active accounts, orders and revenue by day, check-in rate by week, each with a table view.
- User page tabs with orders and internal notes.
- Saved filters on users and payments.
- `/admin/schedule` (week grid / agenda), `/admin/attendance`, `/admin/payments`, `/admin/settings`.

Rights: everything is checked on the server. Users, notes, settings: super admin. Payments: super admin, or a shop's
manager for their own shop. Calendar and attendance: anyone who may manage events (admin, ambassadors and
university staff for their university, club admins for their clubs). Every write is in the audit log.
