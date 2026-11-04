-- 04-016 / 04-024: "log out" on one device ends that session on the server too.
--
-- A session token is signed, not stored, so a plain logout used to clear only the cookie: a
-- copied token kept working until it expired. Every token now carries a session id ("s",
-- kept across sliding refreshes); logout writes it here and pkg/auth rejects it. Tokens signed
-- before "s" existed are keyed as "u<user>.a<sign-in unix>". "Log out everywhere" still bumps
-- users.session_version. expires_at is when no token of that session can still be valid
-- (logout time + the longest lifetime); expired rows are purged on the user's next logout.
create table revoked_sessions (
  sid        text primary key check (char_length(sid) between 8 and 80),
  user_id    bigint not null references users(id) on delete cascade,
  revoked_at timestamptz not null default now(),
  expires_at timestamptz not null
);
create index revoked_sessions_user on revoked_sessions (user_id, expires_at);

alter table revoked_sessions enable row level security;
grant select, insert, delete on revoked_sessions to app_user;
create policy revoked_sessions_self on revoked_sessions for all to app_user
  using (user_id = app_user_id()) with check (user_id = app_user_id());
