-- Shared moderation queue (06) used by opportunities, places, market reports, mentor and
-- employer applications (07, 08, 10), plus the shared report rule and referral attribution.

alter table users add column referred_by text;   -- amb_<code> / share, set once at sign-up

create type submission_status as enum ('pending', 'approved', 'rejected');

create table submissions (
  id           bigint generated always as identity primary key,
  kind         text not null,               -- opportunity, place, market_report, mentor_application, employer_verification, employer_job, ...
  payload      jsonb not null,
  submitted_by bigint references users(id) on delete cascade,
  university   text,
  status       submission_status not null default 'pending',
  reason       text,                         -- required when rejected
  reviewed_by  bigint references users(id) on delete set null,
  reviewed_at  timestamptz,
  created_at   timestamptz not null default now(),
  check (status <> 'rejected' or coalesce(reason, '') <> '')
);
create index submissions_queue on submissions (status, kind, created_at);
create index submissions_by on submissions (submitted_by);

-- Reports ("шағым") on any public item. Hiding after N reports counts only distinct reporters
-- whose accounts are at least 7 days old (see moderation.CountedReports).
create table reports (
  id          bigint generated always as identity primary key,
  target_kind text not null,                 -- market_item, job, employer, ...
  target_id   text not null,
  reporter_id bigint not null references users(id) on delete cascade,
  reason      text not null,
  dismissed   boolean not null default false, -- admin found it groundless
  created_at  timestamptz not null default now(),
  unique (target_kind, target_id, reporter_id)
);
create index reports_target on reports (target_kind, target_id) where not dismissed;

alter table submissions enable row level security;
alter table reports enable row level security;
grant select, insert on submissions to app_user;
grant select, insert on reports to app_user;
create policy submissions_own_read on submissions for select to app_user using (submitted_by = app_user_id());
create policy submissions_own_insert on submissions for insert to app_user with check (submitted_by = app_user_id() and status = 'pending');
create policy reports_own on reports for select to app_user using (reporter_id = app_user_id());
create policy reports_insert on reports for insert to app_user with check (reporter_id = app_user_id());
