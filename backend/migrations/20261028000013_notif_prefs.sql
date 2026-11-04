-- F8 "too many notifications / can't turn them off": per-student notification preferences,
-- a deferred-message outbox (quiet hours, daily cap) and a send log the daily cap counts.
-- Go side: pkg/notify (Send, Digest, Flush), pkg/bot (/settings), docs/api/notifications.md.
--
-- No row = defaults (every category on, quiet 22:00–09:00 Asia/Almaty, cap 3).
-- `moderation` (decisions about the student's own submissions) is transactional: it has no
-- column because it cannot be turned off.

create table notify_prefs (
  user_id       bigint primary key references users(id) on delete cascade,
  deadlines     boolean not null default true,   -- bot deadline reminders
  digest        boolean not null default true,   -- weekly digest and other one-per-day digests
  events        boolean not null default true,
  mentors       boolean not null default true,
  market        boolean not null default true,
  jobs          boolean not null default true,   -- b2b
  quiet_enabled boolean not null default true,
  quiet_start   time not null default '22:00',   -- Asia/Almaty wall clock
  quiet_end     time not null default '09:00',
  daily_cap     smallint not null default 3 check (daily_cap between 1 and 20),  -- non-transactional per Almaty day
  updated_at    timestamptz not null default now(),
  check (quiet_start <> quiet_end)
);

-- Messages held back by quiet hours or the daily cap; GET /api/cron/notify sends them.
create table notify_outbox (
  id         bigint generated always as identity primary key,
  user_id    bigint not null references users(id) on delete cascade,
  category   text not null,
  text       text not null,
  not_before timestamptz not null,
  state      text not null default 'pending' check (state in ('pending', 'sending', 'sent', 'dropped')),
  attempts   int not null default 0,
  claimed_at timestamptz,
  done_at    timestamptz,
  created_at timestamptz not null default now()
);
create index notify_outbox_due on notify_outbox (not_before) where state = 'pending';
create index notify_outbox_user on notify_outbox (user_id);

-- One row per non-transactional message actually sent (the daily cap counts these).
create table notify_sends (
  user_id  bigint not null references users(id) on delete cascade,
  category text not null,
  sent_at  timestamptz not null default now()
);
create index notify_sends_user_at on notify_sends (user_id, sent_at);
create index notify_sends_at on notify_sends (sent_at);

alter table notify_prefs enable row level security;
alter table notify_outbox enable row level security;
alter table notify_sends enable row level security;

-- A student reads and writes only their own preferences; outbox and log are read-only
-- (data export) and written by the server.
grant select, insert, update on notify_prefs to app_user;
grant select on notify_outbox, notify_sends to app_user;
create policy notify_prefs_self on notify_prefs for all to app_user
  using (user_id = app_user_id()) with check (user_id = app_user_id());
create policy notify_outbox_self_read on notify_outbox for select to app_user using (user_id = app_user_id());
create policy notify_sends_self_read on notify_sends for select to app_user using (user_id = app_user_id());
