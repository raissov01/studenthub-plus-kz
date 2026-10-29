-- 04-032..037: one durable inbox/outbox and one global budget per user.
-- Moderation has no exemption in 04-foundation; it is switchable and capped too.
alter table notify_prefs add column community boolean not null default true;
alter table notify_prefs add column moderation boolean not null default true;
alter table notify_prefs alter column quiet_end set default '08:00';
update notify_prefs set daily_cap = least(daily_cap, 3);
alter table notify_prefs drop constraint notify_prefs_daily_cap_check;
alter table notify_prefs add constraint notify_prefs_daily_cap_check check (daily_cap between 1 and 3);

alter table notify_outbox add column kind text not null default 'direct' check (kind in ('direct', 'digest'));
alter table notify_outbox add column delivery_key text;
alter table notify_outbox add column notification_id bigint references notifications(id) on delete set null;
alter table notify_outbox add column reply_markup jsonb;
alter table notify_outbox drop constraint notify_outbox_state_check;
alter table notify_outbox add constraint notify_outbox_state_check
  check (state in ('pending', 'sending', 'sent', 'dropped', 'uncertain'));
create unique index notify_outbox_key on notify_outbox (user_id, delivery_key);

-- A committed reservation counts before the network call, including an ambiguous outcome.
alter table notify_sends add column kind text not null default 'direct' check (kind in ('direct', 'digest'));
alter table notify_sends add column outbox_id bigint references notify_outbox(id) on delete set null;
create unique index notify_sends_outbox on notify_sends(outbox_id);

-- Previously deferred rows already have inbox rows in most cases. Link those, otherwise
-- backfill before allowing the new dispatcher to deliver them. Text is escaped by the UI.
update notify_outbox o set notification_id = (
  select n.id from notifications n where n.user_id = o.user_id and n.category = o.category
    and n.text = o.text order by n.id desc limit 1
);
-- HTML/plain-text matches and absent inbox rows are repaired transactionally by the
-- dispatcher using the same PlainText conversion as new messages before any delivery.
-- Old in-flight sends have unknown outcomes; never blindly retry them after upgrading.
-- They might already have reached Telegram, so reserve their original day's budget too.
insert into notify_sends(user_id, category, sent_at, kind, outbox_id)
  select user_id, category, coalesce(claimed_at, created_at), kind, id
  from notify_outbox where state = 'sending'
  on conflict (outbox_id) do nothing;
update notify_outbox set state = 'uncertain', done_at = coalesce(claimed_at, created_at)
  where state = 'sending';
