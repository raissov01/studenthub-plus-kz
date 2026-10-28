-- Telegram reminder bot: columns the Go port needs on top of the init schema.

-- Russian title of the opportunity (reminders.title holds the Kazakh one), so the bot can
-- answer in the student's language even when the site no longer lists the opportunity.
alter table reminders add column title_ru text;

-- Weekly digest state (only used when DIGEST_ENABLED=true).
alter table users
  add column digest_asked boolean not null default false,  -- city/direction question was asked once
  add column digest_week  date,                             -- Monday of the week the last digest went out
  add column digest_seen  text[] not null default '{}';     -- opportunity ids already sent in digests (last 200)

-- cron prunes old day claims by date
create index bot_daily_sends_day on bot_daily_sends (day);
