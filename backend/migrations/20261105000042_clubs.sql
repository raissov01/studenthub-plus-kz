-- University clubs (FEATURE_CLUBS): catalogue, per-club admins (many-to-many, not a user role),
-- join requests, moderated club news and club events (events.club_id). API: docs/clubs.md.
--
-- Access model:
--   * the public catalogue and club pages are read by the API as the connection owner
--     (only active clubs, only published posts, aggregate member counts);
--   * everything personal runs as app_user under RLS: a student sees only their own membership
--     row; the member list and pending requests of a club are visible to that club's admins only;
--   * a club admin edits only the page of a club they administer (column grants + policy);
--   * creating, archiving clubs and assigning admins is site-admin work done as the owner after
--     the Go role check, written to audit_log.

create table clubs (
  id               bigint generated always as identity primary key,
  name_kk          text not null check (length(name_kk) between 2 and 120),
  name_ru          text check (length(name_ru) <= 120),
  direction        text not null default 'other' check (direction in
                     ('it', 'science', 'debate', 'sport', 'art', 'volunteer', 'media', 'business', 'languages', 'kvn', 'culture', 'other')),
  summary_kk       text check (length(summary_kk) <= 300),   -- short card text
  summary_ru       text check (length(summary_ru) <= 300),
  about_kk         text check (length(about_kk) <= 4000),    -- club page «Біз туралы»
  about_ru         text check (length(about_ru) <= 4000),
  university       text not null,
  building_id      text check (building_id ~ '^[A-Za-z0-9_-]{1,32}$'),  -- frontend BUILDINGS id
  room             text check (length(room) <= 60),
  schedule_kk      text check (length(schedule_kk) <= 200),
  schedule_ru      text check (length(schedule_ru) <= 200),
  leader_title_kk  text check (length(leader_title_kk) <= 80), -- public role title only («Төраға»), never a name
  leader_title_ru  text check (length(leader_title_ru) <= 80),
  leader_contact   text check (length(leader_contact) <= 120), -- shown only when leader_contact_public
  leader_contact_public boolean not null default false,
  telegram_url     text check (telegram_url ~ '^https://t\.me/[A-Za-z0-9_+/-]{2,100}$'),
  instagram_url    text check (instagram_url ~ '^https://(www\.)?instagram\.com/[A-Za-z0-9_.]{1,60}/?$'),
  join_mode        text not null default 'request' check (join_mode in ('request', 'link')),
  join_link        text check (join_link ~ '^https://t\.me/[A-Za-z0-9_+/-]{2,100}$'),
  recruiting       boolean not null default false,
  status           text not null default 'active' check (status in ('active', 'archived')),
  source_url       text check (source_url ~ '^https://'),      -- where the record was verified (seed / admin)
  verified_at      date,
  seed_key         text unique,                                -- CLUBS_SEED import key; null for clubs made on the site
  created_by       bigint references users(id) on delete set null,
  created_at       timestamptz not null default now(),
  updated_at       timestamptz not null default now(),
  check (join_mode <> 'link' or join_link is not null)
);
create index clubs_active on clubs (status, direction);

create table club_admins (
  club_id    bigint not null references clubs(id) on delete cascade,
  user_id    bigint not null references users(id) on delete cascade,
  granted_by bigint references users(id) on delete set null,
  granted_at timestamptz not null default now(),
  primary key (club_id, user_id)
);
create index club_admins_user on club_admins (user_id);

-- One row per student per club: a join request ('pending') or a member ('member').
-- Declining or leaving deletes the row (nothing about a refused request is kept).
create table club_members (
  club_id     bigint not null references clubs(id) on delete cascade,
  user_id     bigint not null references users(id) on delete cascade,
  status      text not null default 'pending' check (status in ('pending', 'member')),
  message     text check (length(message) <= 300),
  created_at  timestamptz not null default now(),
  decided_at  timestamptz,
  decided_by  bigint references users(id) on delete set null,
  primary key (club_id, user_id)
);
create index club_members_user on club_members (user_id);
create index club_members_pending on club_members (club_id, created_at) where status = 'pending';

-- Club news. Written by club admins and published only after moderation (submissions kind
-- 'club_post'), or attached from the opportunity feed (source = 'feed', see clubs.AttachFeedItem).
create table club_posts (
  id            bigint generated always as identity primary key,
  club_id       bigint not null references clubs(id) on delete cascade,
  author_id     bigint references users(id) on delete set null,
  body_kk       text not null check (length(body_kk) between 3 and 2000),
  body_ru       text check (length(body_ru) <= 2000),
  link_url      text check (link_url ~ '^https://'),
  status        text not null default 'pending' check (status in ('pending', 'published', 'rejected', 'hidden')),
  submission_id bigint references submissions(id) on delete set null,
  source        text not null default 'club' check (source in ('club', 'feed', 'admin')),
  source_ref    text check (length(source_ref) <= 200),        -- the feed item id, unique per club
  created_at    timestamptz not null default now(),
  published_at  timestamptz,
  unique (club_id, source_ref)
);
create index club_posts_club on club_posts (club_id, status, published_at desc);

-- Events made by a club go through the normal events flow (QR check-in, calendar, points).
alter table events add column club_id bigint references clubs(id) on delete set null;
create index events_club on events (club_id, starts_at) where club_id is not null;

-- ---------- helpers ----------

create or replace function app_is_club_admin(p_club bigint) returns boolean
language sql stable security definer set search_path = public, pg_temp as $$
  select exists (select 1 from club_admins a join users u on u.id = a.user_id
    where a.club_id = p_club and a.user_id = app_user_id() and u.deleted_at is null)
$$;

create or replace function app_club_university(p_club bigint) returns text
language sql stable security definer set search_path = public, pg_temp as $$
  select university from clubs where id = p_club and status = 'active'
$$;

create or replace function app_club_accepts_requests(p_club bigint) returns boolean
language sql stable security definer set search_path = public, pg_temp as $$
  select exists (select 1 from clubs where id = p_club and status = 'active' and join_mode = 'request')
$$;

revoke all on function app_is_club_admin(bigint) from public;
revoke all on function app_club_university(bigint) from public;
revoke all on function app_club_accepts_requests(bigint) from public;
grant execute on function app_is_club_admin(bigint) to app_user;
grant execute on function app_club_university(bigint) to app_user;
grant execute on function app_club_accepts_requests(bigint) to app_user;

-- ---------- RLS ----------

alter table clubs enable row level security;
alter table club_admins enable row level security;
alter table club_members enable row level security;
alter table club_posts enable row level security;

grant select on clubs to app_user;
-- a club admin edits the page; name, direction, status, university, sources and seed key stay with the site admin
grant update (summary_kk, summary_ru, about_kk, about_ru, building_id, room, schedule_kk, schedule_ru,
  leader_title_kk, leader_title_ru, leader_contact, leader_contact_public, telegram_url, instagram_url,
  join_mode, join_link, recruiting, updated_at) on clubs to app_user;
create policy clubs_read on clubs for select to app_user using (status = 'active' or app_is_club_admin(id));
create policy clubs_admin_update on clubs for update to app_user
  using (app_is_club_admin(id)) with check (app_is_club_admin(id));

grant select on club_admins to app_user;
create policy club_admins_read on club_admins for select to app_user
  using (user_id = app_user_id() or app_is_club_admin(club_id));

grant select, insert, delete on club_members to app_user;
grant update (status, decided_at, decided_by) on club_members to app_user;
create policy club_members_own_read on club_members for select to app_user using (user_id = app_user_id());
create policy club_members_admin_read on club_members for select to app_user using (app_is_club_admin(club_id));
create policy club_members_request on club_members for insert to app_user
  with check (user_id = app_user_id() and status = 'pending' and decided_at is null and app_club_accepts_requests(club_id));
create policy club_members_own_delete on club_members for delete to app_user using (user_id = app_user_id());
create policy club_members_admin_delete on club_members for delete to app_user using (app_is_club_admin(club_id));
create policy club_members_admin_decide on club_members for update to app_user
  using (app_is_club_admin(club_id)) with check (app_is_club_admin(club_id) and status = 'member');

grant select, insert on club_posts to app_user;
create policy club_posts_read on club_posts for select to app_user
  using (status = 'published' or app_is_club_admin(club_id));
create policy club_posts_insert on club_posts for insert to app_user
  with check (author_id = app_user_id() and status = 'pending' and source = 'club' and app_is_club_admin(club_id));

-- events: club admins create and edit their club's events and see who registered
grant select (club_id) on events to app_user;
grant insert (club_id) on events to app_user;
create policy events_club_insert on events for insert to app_user
  with check (club_id is not null and created_by = app_user_id() and app_is_club_admin(club_id)
              and university = app_club_university(club_id));
create policy events_club_update on events for update to app_user
  using (club_id is not null and app_is_club_admin(club_id))
  with check (club_id is not null and app_is_club_admin(club_id));
create policy events_club_read on events for select to app_user
  using (club_id is not null and app_is_club_admin(club_id));
create policy event_regs_club_read on event_regs for select to app_user
  using (exists (select 1 from events e where e.id = event_id and e.club_id is not null and app_is_club_admin(e.club_id)));

-- whoever inserts through another policy (an ambassador, staff) cannot tag someone else's club
create or replace function app_is_site_admin() returns boolean
language sql stable security definer set search_path = public, pg_temp as $$
  select exists (select 1 from users where id = app_user_id() and role = 'admin' and deleted_at is null)
$$;
revoke all on function app_is_site_admin() from public;
grant execute on function app_is_site_admin() to app_user;
create policy events_club_tag on events as restrictive for insert to app_user
  with check (club_id is null or app_is_club_admin(club_id) or app_is_site_admin());
