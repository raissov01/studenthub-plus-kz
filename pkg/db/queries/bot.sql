-- Telegram reminder bot (pkg/bot). Bot users are rows in users keyed by telegram_id.

-- name: BotUserByTelegram :one
select * from users where telegram_id = $1;

-- name: BotTouchUser :one
-- Every private message or button press: create the bot-only user on first contact,
-- mark them active again (they may have unblocked the bot).
insert into users (telegram_id, lang, bot_started_at) values ($1, $2, now())
on conflict (telegram_id) do update
  set bot_inactive = false, bot_started_at = coalesce(users.bot_started_at, now())
returning *;

-- name: BotSetInactive :exec
update users set bot_inactive = $2 where id = $1;

-- name: BotSetLang :exec
update users set lang = $2 where id = $1;

-- name: BotSetPausedUntil :exec
update users set paused_until = sqlc.narg(paused_until) where id = $1;

-- name: BotMarkSafetyNote :exec
update users set safety_note_sent = true where id = $1;

-- name: BotSetDigestAsked :exec
update users set digest_asked = true where id = $1;

-- name: BotSetDigestCity :exec
update users set digest_city = $2 where id = $1;

-- name: BotSetDigestDirection :exec
update users set digest_direction = $2 where id = $1;

-- name: BotSetDigestSent :exec
update users set digest_week = $2, digest_seen = $3 where id = $1;

-- name: BotListUsers :many
select * from users
where bot_started_at is not null and deleted_at is null
order by id;

-- name: BotActiveReminders :many
select * from reminders where status = 'active'
order by user_id, deadline, opportunity_id;

-- name: BotUserReminders :many
select * from reminders where user_id = $1 and status = 'active'
order by deadline, opportunity_id;

-- name: BotUpsertReminder :exec
-- A finished (submitted/dismissed) reminder becomes active again when the student asks anew.
insert into reminders (user_id, opportunity_id, title, title_ru, deadline)
values ($1, $2, $3, $4, $5)
on conflict (user_id, opportunity_id) do update
  set title = excluded.title, title_ru = excluded.title_ru, deadline = excluded.deadline,
      status = 'active', updated_at = now();

-- name: BotSetReminderDeadline :exec
update reminders set deadline = $3, updated_at = now() where user_id = $1 and opportunity_id = $2;

-- name: BotFinishReminder :execrows
update reminders set status = $3, updated_at = now()
where user_id = $1 and opportunity_id = $2 and status = 'active';

-- name: BotDeleteReminder :execrows
delete from reminders where user_id = $1 and opportunity_id = $2;

-- name: BotClaimDay :one
-- One message per student per day: whoever inserts the row owns the day.
insert into bot_daily_sends (user_id, day) values ($1, $2)
on conflict do nothing
returning user_id;

-- name: BotReleaseDay :exec
delete from bot_daily_sends where user_id = $1 and day = $2;

-- name: BotPruneDays :exec
delete from bot_daily_sends where day < $1;

-- name: BotLogEvent :exec
insert into bot_events (kind, user_id) values ($1, $2);

-- name: BotCountEvents :one
select count(*) from bot_events where kind = $1 and at >= $2;

-- name: BotCountUsers :one
select count(*) as users, count(*) filter (where not bot_inactive) as active
from users where bot_started_at is not null and deleted_at is null;

-- name: BotCountLiveReminders :one
select count(*) from reminders r join users u on u.id = r.user_id
where r.status = 'active' and r.deadline >= $1
  and not u.bot_inactive and u.bot_started_at is not null and u.deleted_at is null;

-- name: BotDeleteUserReminders :exec
delete from reminders where user_id = $1;

-- name: BotDeleteUserSends :exec
delete from bot_daily_sends where user_id = $1;

-- name: BotAnonymizeEvents :exec
update bot_events set user_id = null where user_id = $1;

-- name: BotDeleteBotOnlyUser :execrows
-- Only a user known solely to the bot; site accounts are deleted by the account flow.
delete from users where id = $1 and consent_at is null;

-- name: BotClearUser :exec
update users set bot_started_at = null, bot_inactive = false, paused_until = null,
  safety_note_sent = false, digest_city = null, digest_direction = null,
  digest_asked = false, digest_week = null, digest_seen = '{}'
where id = $1;
