-- Spec 10 career-fair booths (pkg/fairs, FEATURE_FAIRS on top of FEATURE_EVENTS): the companies at
-- a career event, a printable per-booth QR and the students who scanned it.
--
-- The booth token is HMAC(SESSION_SECRET, qr_secret | event | employer): static for the fair so it
-- can be printed, and void as soon as the exhibitor row is removed or its secret rotated.
-- booth_checkins keeps only the NAMES of the fields the student agreed to share ('name', 'course',
-- 'faculty', 'telegram'); the employer reads their current values, so a deleted account leaves
-- nothing behind (cascade) and an empty list is shown to the employer as a count only.

create table event_exhibitors (
  event_id    bigint not null references events(id) on delete cascade,
  employer_id bigint not null references employers(id) on delete cascade,
  booth       text not null default '' check (char_length(booth) <= 40),
  qr_secret   bytea not null check (length(qr_secret) = 16),
  added_by    bigint references users(id) on delete set null,
  created_at  timestamptz not null default now(),
  primary key (event_id, employer_id)
);
create index event_exhibitors_employer on event_exhibitors (employer_id);

create table booth_checkins (
  event_id      bigint not null,
  employer_id   bigint not null,
  user_id       bigint not null references users(id) on delete cascade,
  shared_fields jsonb not null default '[]' check (jsonb_typeof(shared_fields) = 'array' and jsonb_array_length(shared_fields) <= 4),
  at            timestamptz not null default now(),
  primary key (event_id, employer_id, user_id),
  foreign key (event_id, employer_id) references event_exhibitors (event_id, employer_id) on delete cascade
);
create index booth_checkins_user on booth_checkins (user_id, event_id);

alter table event_exhibitors enable row level security;
alter table booth_checkins enable row level security;
-- the exhibitor list is public (the fair page); the secret never is
grant select (event_id, employer_id, booth, created_at) on event_exhibitors to app_user;
create policy event_exhibitors_read on event_exhibitors for select to app_user using (true);
grant select on booth_checkins to app_user;
create policy booth_checkins_own on booth_checkins for select to app_user using (user_id = app_user_id());
create policy booth_checkins_employer on booth_checkins for select to app_user using (app_employer_active(employer_id));
