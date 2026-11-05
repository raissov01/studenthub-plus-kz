-- Admin console (internal/admin, internal/console): scoped admin roles, account blocks and
-- per-day account activity for DAU/WAU.
--
-- admin_grants: a scoped admin right on top of users.role. A student keeps every student feature and can also
-- be a moderator. Only an admin grants or revokes one, always with a reason, always audited (audit_log
-- scope_grant / scope_revoke). The server re-reads the grants on every request (auth.lookup), so a revoke
-- takes effect at once.
create table admin_grants (
  user_id    bigint not null references users(id) on delete cascade,
  scope      text not null check (scope in ('moderator')),
  granted_by bigint references users(id) on delete set null,
  reason     text not null check (char_length(reason) between 3 and 500),
  created_at timestamptz not null default now(),
  primary key (user_id, scope)
);
create index admin_grants_scope on admin_grants (scope);

-- user_blocks: a blocked account can still sign in, read and delete itself, but every other write is refused
-- (auth.Write). Admins cannot be blocked. Block and unblock are audited (user_block / user_unblock).
create table user_blocks (
  user_id    bigint primary key references users(id) on delete cascade,
  reason     text not null check (char_length(reason) between 3 and 500),
  blocked_by bigint references users(id) on delete set null,
  created_at timestamptz not null default now()
);

-- user_active_days: one row per account per Asia/Almaty day the account made a signed-in request. Written by
-- the server (once per account per day per process), read only by the admin dashboard as counts.
create table user_active_days (
  user_id bigint not null references users(id) on delete cascade,
  day     date not null,
  primary key (user_id, day)
);
create index user_active_days_day on user_active_days (day);

-- the audit log screen filters by action (actor and target already have indexes)
create index audit_log_action on audit_log (action, id desc);

alter table admin_grants enable row level security;
alter table user_blocks enable row level security;
alter table user_active_days enable row level security;
-- an account reads only its own grants and block (the session lookup runs as app_user)
grant select on admin_grants to app_user;
grant select on user_blocks to app_user;
create policy admin_grants_self on admin_grants for select to app_user using (user_id = app_user_id());
create policy user_blocks_self on user_blocks for select to app_user using (user_id = app_user_id());
-- user_active_days: no app_user privileges
