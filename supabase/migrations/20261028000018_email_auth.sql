-- Email + password sign-in, next to Telegram (pkg/auth/email*.go, docs/ACCOUNTS.md).
--
-- * users.telegram_id becomes nullable: an email-only account has no Telegram. The unique
--   constraint stays (Postgres allows any number of NULLs in a unique column), so the
--   bot's and the login's "on conflict (telegram_id)" keep working.
-- * users.email is stored normalized (trimmed, lower case) and unique case-insensitively.
--   A row with email set but email_verified_at null is a sign-up waiting for its code: it
--   never gets a session.
-- * Password hashes live in their own server-only table, so "select * from users" (the
--   data export, app_user's own-row reads) can never return one.
-- * auth_codes: one live 6-digit code per user and purpose, stored as an HMAC, 15 minutes,
--   at most 5 wrong attempts. Login/code throttling uses the existing rate_limits table.

alter table users alter column telegram_id drop not null;

alter table users add column email text;
alter table users add column email_verified_at timestamptz;

alter table users add constraint users_email_normalized
  check (email is null or (email = lower(btrim(email)) and length(email) between 3 and 254 and position('@' in email) > 1));
create unique index users_email_key on users (lower(email)) where email is not null;

-- every account can be reached by at least one sign-in method
alter table users add constraint users_has_identity check (telegram_id is not null or email is not null);

create table user_passwords (
  user_id    bigint primary key references users(id) on delete cascade,
  hash       text not null,             -- PHC string: $argon2id$v=19$m=...,t=...,p=...$salt$hash
  updated_at timestamptz not null default now()
);

create table auth_codes (
  id            bigint generated always as identity primary key,
  user_id       bigint not null references users(id) on delete cascade,
  purpose       text not null check (purpose in ('verify_email', 'reset_password', 'change_email')),
  email         text not null,           -- where the code went (the new address for change_email)
  code_hash     bytea not null,          -- HMAC-SHA256(SESSION_SECRET-derived key, purpose|user|email|code)
  password_hash text,                    -- change_email only: password chosen with the new address
  attempts      int not null default 0 check (attempts >= 0),
  max_attempts  int not null default 5 check (max_attempts between 1 and 10),
  expires_at    timestamptz not null,
  created_at    timestamptz not null default now(),
  unique (user_id, purpose)              -- a new code replaces the previous one
);
create index auth_codes_expires on auth_codes (expires_at);

-- Server-only: RLS on and no policy or grant for app_user.
alter table user_passwords enable row level security;
alter table auth_codes enable row level security;
revoke all on user_passwords, auth_codes from app_user;
