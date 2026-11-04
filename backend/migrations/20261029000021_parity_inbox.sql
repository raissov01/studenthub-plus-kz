-- Parity fixes (docs/PARITY.md rows 04-012, 04-033, 07-017).
--
-- 1. In-site notification inbox. Every notify.Send / Direct / Digest writes one row here
--    first, whatever happens to the Telegram message afterwards (no Telegram linked, bot
--    blocked, users.bot_inactive, category switched off, quiet hours, daily cap), so nothing
--    is lost. The site reads it through GET /api/me/inbox (docs/api/inbox.md).
--    Read rows older than 90 days are deleted by the daily /api/cron run (pkg/cronrun).
create table notifications (
  id         bigint generated always as identity primary key,
  user_id    bigint not null references users(id) on delete cascade,
  category   text not null,                       -- notify.Category: deadlines, digest, events, mentors, market, jobs, moderation
  text       text not null check (char_length(text) <= 4096),  -- plain text in the user's language (bot HTML stripped)
  link       text check (link is null or (link ~ '^/[^/\\]' and char_length(link) <= 300)),  -- site path, e.g. /event/12
  created_at timestamptz not null default now(),
  read_at    timestamptz
);
create index notifications_user on notifications (user_id, id desc);
create index notifications_unread on notifications (user_id) where read_at is null;
create index notifications_read_at on notifications (created_at) where read_at is not null;

alter table notifications enable row level security;
-- A student reads their own inbox and may only mark it read; rows are written by the server.
grant select on notifications to app_user;
grant update (read_at) on notifications to app_user;
create policy notifications_self_read on notifications for select to app_user using (user_id = app_user_id());
create policy notifications_self_mark on notifications for update to app_user
  using (user_id = app_user_id()) with check (user_id = app_user_id());

-- 2. 07-017: waiting mentees hear about a new group mentoring meeting of their university
--    once per meeting (digest type: at most one bot message a day, retried by the daily
--    mentors cron until delivered or the meeting starts).
create table mentor_meeting_notices (
  event_id bigint not null references events(id) on delete cascade,
  user_id  bigint not null references users(id) on delete cascade,
  sent_at  timestamptz not null default now(),
  primary key (event_id, user_id)
);
alter table mentor_meeting_notices enable row level security;
grant select on mentor_meeting_notices to app_user;
create policy mentor_meeting_notices_self_read on mentor_meeting_notices for select to app_user using (user_id = app_user_id());
