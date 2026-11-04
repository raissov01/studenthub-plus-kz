-- Web Push (internal/webpush, docs/PUSH.md): one row per browser that turned on «Браузер
-- хабарламалары». The endpoint is a capability URL chosen by the browser's push service; it is
-- unique, so a browser that signs in to another account moves to that account.
--
-- Written by the server as the connection owner (subscribe has to move a row the student cannot
-- see under RLS; delivery updates last_ok/failures and deletes rows answered 404/410).
-- app_user may read and delete only its own rows (data export, unsubscribe).

create table push_subscriptions (
  id         bigint generated always as identity primary key,
  user_id    bigint not null references users(id) on delete cascade,
  endpoint   text not null unique check (char_length(endpoint) between 12 and 1024 and endpoint like 'https://%'),
  p256dh     text not null check (char_length(p256dh) = 87),   -- base64url, 65-byte P-256 point
  auth       text not null check (char_length(auth) = 22),     -- base64url, 16 bytes
  ua         text check (char_length(ua) <= 200),              -- "Chrome 131", shown in the export
  created_at timestamptz not null default now(),
  last_ok    timestamptz,
  failures   int not null default 0 check (failures >= 0)
);
create index push_subscriptions_user on push_subscriptions (user_id, created_at desc);

alter table push_subscriptions enable row level security;
grant select, delete on push_subscriptions to app_user;
create policy push_subscriptions_self on push_subscriptions for all to app_user
  using (user_id = app_user_id()) with check (user_id = app_user_id());

-- Which channel delivered an outbox item: 'push' (Web Push) or 'telegram' (bot). Null for rows
-- older than this migration and for items that were never delivered.
alter table notify_outbox add column channel text check (channel in ('push', 'telegram'));
