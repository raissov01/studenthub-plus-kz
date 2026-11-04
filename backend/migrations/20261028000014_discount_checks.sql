-- F7 "codes don't work": students confirm whether a discount worked for them.
-- One vote per student per discount per 30 days (enforced in pkg/discounts under an advisory
-- lock); public counts cover the last 60 days. discount_id is the string id from
-- /api/content discounts (e.g. "d1"), validated against the loaded content in Go.

create table discount_checks (
  id          bigint generated always as identity primary key,
  discount_id text not null check (discount_id ~ '^[A-Za-z0-9_-]{1,64}$'),
  user_id     bigint not null references users(id) on delete cascade,
  worked      boolean not null,
  created_at  timestamptz not null default now()
);
create index discount_checks_recent on discount_checks (discount_id, created_at desc);
create index discount_checks_user on discount_checks (user_id, discount_id, created_at desc);

alter table discount_checks enable row level security;

-- A student inserts and reads only their own votes; the public counts are aggregated by the
-- server. No update/delete: a vote stands until it ages out of the window.
grant select, insert on discount_checks to app_user;
create policy discount_checks_self_read on discount_checks for select to app_user using (user_id = app_user_id());
create policy discount_checks_self_insert on discount_checks for insert to app_user with check (user_id = app_user_id());
