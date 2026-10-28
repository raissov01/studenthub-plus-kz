-- Accounts: consent details, tighter column grants for app_user, self-read policies needed
-- for the data export, and a guard that every table pointing at users(id) says what
-- happens on account deletion.
--
-- Deletion rule for future tables (see docs/ACCOUNTS.md):
--   * a student's own rows            -> user_id ... references users(id) on delete cascade
--   * authorship on shared content    -> created_by ... references users(id) on delete set null
--   * anything outside Postgres (Storage, PostHog) -> me.RegisterDeleteHook in Go

alter table users add column is_adult boolean;          -- null = not asked yet
alter table users add column last_seen_at timestamptz;  -- updated on login

-- app_user may change only profile fields, consent and session_version (logout everywhere).
-- role, telegram_id, deleted_at and bot state stay server-only even for the row owner.
revoke update on users from app_user;
grant update (name, lang, university, faculty, course, city, consent_at, is_adult, session_version)
  on users to app_user;
grant delete on users to app_user;
create policy users_self_delete on users for delete to app_user using (id = app_user_id());

-- Export reads the student's own bot rows and audit entries about them.
grant select on bot_daily_sends, bot_events, audit_log to app_user;
create policy bot_daily_sends_self_read on bot_daily_sends for select to app_user using (user_id = app_user_id());
create policy bot_events_self_read on bot_events for select to app_user using (user_id = app_user_id());
create policy audit_log_self_read on audit_log for select to app_user
  using (actor_id = app_user_id() or target_id = app_user_id());

create index audit_log_target on audit_log (target_id) where target_id is not null;
create index audit_log_actor on audit_log (actor_id) where actor_id is not null;
create index bot_events_user on bot_events (user_id) where user_id is not null;
