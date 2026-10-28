-- Student Hub: base schema (accounts, audit, reminders, rate limits).
-- Row Level Security works without Supabase Auth: the Go API opens a transaction,
-- sets app.user_id and switches to the restricted role app_user (see pkg/db).

do $$ begin
  if not exists (select 1 from pg_roles where rolname = 'app_user') then
    create role app_user nologin;
  end if;
end $$;
grant app_user to current_user;
grant usage on schema public to app_user;

create or replace function app_user_id() returns bigint
language sql stable as $$ select nullif(current_setting('app.user_id', true), '')::bigint $$;

create type user_role as enum ('student', 'ambassador', 'admin', 'university_staff', 'employer');

create table users (
  id           bigint generated always as identity primary key,
  telegram_id  bigint not null unique,
  tg_username  text,
  name         text,
  lang         text not null default 'kk' check (lang in ('kk', 'ru')),
  university   text,
  faculty      text,
  course       smallint check (course between 1 and 6),
  city         text,
  role         user_role not null default 'student',
  consent_at   timestamptz,          -- null = only known to the bot, no site account yet
  session_version int not null default 1,  -- bump to log out every device
  -- bot state
  bot_started_at  timestamptz,
  bot_inactive    boolean not null default false,  -- blocked the bot (Telegram 403)
  paused_until    timestamptz,
  safety_note_sent boolean not null default false,
  digest_city     text,
  digest_direction text,
  created_at   timestamptz not null default now(),
  deleted_at   timestamptz
);

create table audit_log (
  id         bigint generated always as identity primary key,
  actor_id   bigint references users(id) on delete set null,  -- null after the actor deletes the account
  action     text not null,              -- role_grant, role_revoke, moderation, account_delete, ...
  target_id  bigint references users(id) on delete set null,
  reason     text,
  meta       jsonb not null default '{}',
  created_at timestamptz not null default now()
);
create index audit_log_created on audit_log (created_at desc);

create table reminders (
  user_id        bigint not null references users(id) on delete cascade,
  opportunity_id text not null,
  title          text not null,
  deadline       date not null,
  status         text not null default 'active' check (status in ('active', 'submitted', 'dismissed')),
  created_at     timestamptz not null default now(),
  updated_at     timestamptz not null default now(),
  primary key (user_id, opportunity_id)
);
create index reminders_due on reminders (deadline) where status = 'active';

-- one bot message per student per day: inserting the row claims the day
create table bot_daily_sends (
  user_id bigint not null references users(id) on delete cascade,
  day     date not null,
  primary key (user_id, day)
);

create table bot_events (
  id      bigint generated always as identity primary key,
  kind    text not null,            -- submitted, dismissed, registered
  user_id bigint references users(id) on delete set null,
  at      timestamptz not null default now()
);
create index bot_events_kind_at on bot_events (kind, at);

create table rate_limits (
  key          text not null,
  window_start timestamptz not null,
  hits         int not null default 0,
  primary key (key, window_start)
);

-- RLS: a student reads and edits only their own rows; everything else is server-only.
alter table users enable row level security;
alter table audit_log enable row level security;
alter table reminders enable row level security;
alter table bot_daily_sends enable row level security;
alter table bot_events enable row level security;
alter table rate_limits enable row level security;

grant select, update on users to app_user;
grant select, insert, update, delete on reminders to app_user;

create policy users_self_read on users for select to app_user using (id = app_user_id() and deleted_at is null);
create policy users_self_update on users for update to app_user using (id = app_user_id()) with check (id = app_user_id());
create policy reminders_self on reminders for all to app_user using (user_id = app_user_id()) with check (user_id = app_user_id());
-- audit_log, bot_*, rate_limits: no policy for app_user = no access.
