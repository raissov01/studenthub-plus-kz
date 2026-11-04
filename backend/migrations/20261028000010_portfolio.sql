-- Public portfolio (prompt 09): per-student settings for the public page /p/<slug> and the
-- self-declared items the student chose to sync to the account.
--
-- Honesty rule: "Расталған ✓" is never stored. It is derived at read time from system tables
-- only — verified_attendance / event_regs.checked_in_at, event_certificates, ambassador_terms
-- certificates and user_badges.verified (set by the mentors package). portfolio_items has no
-- "verified" column, so neither the API nor a crafted request can mark a self-declared item
-- as verified.
--
-- GPA, phone and email are never stored here (there is no column and no toggle for them).

create table portfolio_settings (
  user_id         bigint primary key references users(id) on delete cascade,
  enabled         boolean not null default false,          -- the public page is OFF until the student turns it on
  slug            text not null unique check (slug ~ '^[a-z0-9]{10}$'),  -- random, not the username
  display_name    text check (char_length(display_name) <= 80),
  headline        text check (char_length(headline) <= 80),
  bio             text check (char_length(bio) <= 300),
  show_university boolean not null default true,
  show_course     boolean not null default true,
  show_events     boolean not null default true,
  show_certs      boolean not null default true,
  show_experience boolean not null default true,
  show_skills     boolean not null default true,
  show_badges     boolean not null default true,
  show_contact    boolean not null default false,          -- "Telegram арқылы жазу" button
  sync_consent_at timestamptz,                              -- "save experience, skills, certificates to the account?" yes
  slug_rotated_at timestamptz,
  created_at      timestamptz not null default now(),
  updated_at      timestamptz not null default now()
);

-- Self-declared ("өзі көрсеткен") items, stored only after the explicit sync consent.
create table portfolio_items (
  id         bigint generated always as identity primary key,
  user_id    bigint not null references users(id) on delete cascade,
  kind       text not null check (kind in ('cert', 'experience', 'skill')),
  title      text not null check (char_length(title) between 1 and 120),
  org        text not null default '' check (char_length(org) <= 120),
  period     text not null default '' check (char_length(period) <= 40),
  descr      text not null default '' check (char_length(descr) <= 300),
  url        text not null default '' check (url = '' or (url like 'https://%' and char_length(url) <= 500)),
  position   smallint not null default 0,
  created_at timestamptz not null default now()
);
create index portfolio_items_user on portfolio_items (user_id, kind, position);

alter table portfolio_settings enable row level security;
alter table portfolio_items enable row level security;

-- The student manages only their own rows. The public page is read by the server (connection
-- owner) by slug, with enabled = true and a live account as hard filters.
grant select, insert, delete on portfolio_settings to app_user;
grant update (enabled, slug, display_name, headline, bio, show_university, show_course, show_events, show_certs,
  show_experience, show_skills, show_badges, show_contact, sync_consent_at, slug_rotated_at, updated_at)
  on portfolio_settings to app_user;
create policy portfolio_settings_own on portfolio_settings for all to app_user
  using (user_id = app_user_id()) with check (user_id = app_user_id());

grant select, insert, update, delete on portfolio_items to app_user;
-- items may be written only while the sync consent is on
create policy portfolio_items_own_read on portfolio_items for select to app_user using (user_id = app_user_id());
create policy portfolio_items_own_delete on portfolio_items for delete to app_user using (user_id = app_user_id());
create policy portfolio_items_own_write on portfolio_items for insert to app_user
  with check (user_id = app_user_id() and exists (select 1 from portfolio_settings s
    where s.user_id = app_user_id() and s.sync_consent_at is not null));
create policy portfolio_items_own_update on portfolio_items for update to app_user
  using (user_id = app_user_id()) with check (user_id = app_user_id());
