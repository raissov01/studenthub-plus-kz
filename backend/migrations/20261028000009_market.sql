-- Market (prompt 08): free give-away items between students. No price column on purpose.
-- Photos live on the API server's disk (UPLOAD_DIR/market/<random>.jpg); market_photos is the
-- registry that ties each file to its uploader, so cleanup and account deletion find them.

create table market_items (
  id            bigint generated always as identity primary key,
  user_id       bigint not null references users(id) on delete cascade,
  title         text not null check (char_length(title) between 1 and 60),
  descr         text not null default '' check (char_length(descr) <= 300),
  category      text not null check (category in ('books', 'dorm', 'tech', 'clothes', 'sport', 'other')),
  condition     text not null check (condition in ('new', 'good', 'used')),
  university    text not null,
  pickup_text   text not null default '' check (char_length(pickup_text) <= 120),
  photos        text[] not null default '{}' check (cardinality(photos) <= 3),
  -- pending: a new account's first item waits in the moderation queue;
  -- hidden: by reports (hidden_reason = 'reports', reversible) or by an admin ('admin');
  -- expired: 30 days passed.
  status        text not null default 'active' check (status in ('pending', 'active', 'reserved', 'given', 'hidden', 'expired')),
  hidden_reason text check (hidden_reason in ('reports', 'admin')),
  submission_id bigint references submissions(id) on delete set null,
  created_at    timestamptz not null default now(),
  updated_at    timestamptz not null default now(),
  expires_at    timestamptz not null default now() + interval '30 days',
  reminded_at   timestamptz,    -- "3 days left" digest sent for the current expires_at
  given_at      timestamptz
);
create index market_items_browse on market_items (university, id desc) where status in ('active', 'reserved');
create index market_items_user on market_items (user_id, created_at desc);
create index market_items_expiry on market_items (expires_at) where status in ('active', 'reserved');
create index market_items_photos on market_items using gin (photos);

create table market_photos (
  name       text primary key check (name ~ '^[0-9a-f]{32}\.jpg$'),
  user_id    bigint not null references users(id) on delete cascade,
  item_id    bigint references market_items(id) on delete set null,
  bytes      int not null,
  created_at timestamptz not null default now()
);
create index market_photos_user on market_photos (user_id);
create index market_photos_orphans on market_photos (created_at) where item_id is null;

-- daily publish counter: market_item_create bumps it atomically (deleting an item does not
-- give the slot back)
create table market_daily (
  user_id bigint not null references users(id) on delete cascade,
  day     date not null,
  n       int not null default 0,
  primary key (user_id, day)
);

create table market_rules (
  user_id     bigint primary key references users(id) on delete cascade,
  accepted_at timestamptz not null default now()
);

-- admin's temporary publishing ban (the reason also goes to audit_log)
create table market_bans (
  user_id    bigint primary key references users(id) on delete cascade,
  until      timestamptz not null,
  reason     text not null,
  created_by bigint references users(id) on delete set null,
  created_at timestamptz not null default now()
);

-- the seasonal "moving out?" digest goes to each student once per season
create table market_season_sends (
  season  text not null,
  user_id bigint not null references users(id) on delete cascade,
  sent_at timestamptz not null default now(),
  primary key (season, user_id)
);

-- market_item_create checks the publisher and the daily limit in one transaction and returns
-- the new item id. Raises: no_account, no_username, minor, no_rules, banned, daily_limit,
-- bad_photo. A young account's (< 7 days) first item starts as 'pending'.
create function market_item_create(p_user bigint, p_title text, p_descr text, p_category text,
  p_condition text, p_pickup text, p_photos text[], p_daily_limit int)
returns market_items language plpgsql as $$
declare
  u users%rowtype;
  n int;
  item market_items%rowtype;
  young boolean;
begin
  select * into u from users where id = p_user and deleted_at is null for update;
  if not found or u.consent_at is null or coalesce(u.university, '') = '' then
    raise exception 'no_account';
  end if;
  if coalesce(u.tg_username, '') = '' then
    raise exception 'no_username';
  end if;
  if u.is_adult is not true then
    raise exception 'minor';
  end if;
  if not exists (select 1 from market_rules where user_id = p_user) then
    raise exception 'no_rules';
  end if;
  if exists (select 1 from market_bans where user_id = p_user and until > now()) then
    raise exception 'banned';
  end if;
  if (select count(*) from market_photos where name = any(p_photos) and user_id = p_user and item_id is null)
     <> cardinality(p_photos) then
    raise exception 'bad_photo';
  end if;
  insert into market_daily (user_id, day, n) values (p_user, (now() at time zone 'Asia/Almaty')::date, 1)
  on conflict (user_id, day) do update set n = market_daily.n + 1
  returning market_daily.n into n;
  if n > p_daily_limit then
    raise exception 'daily_limit';
  end if;
  young := u.created_at > now() - interval '7 days'
    and not exists (select 1 from market_items where user_id = p_user and status <> 'pending');
  insert into market_items (user_id, title, descr, category, condition, university, pickup_text, photos, status)
  values (p_user, p_title, p_descr, p_category, p_condition, u.university, p_pickup, p_photos,
          case when young then 'pending' else 'active' end)
  returning * into item;
  update market_photos set item_id = item.id where name = any(p_photos);
  return item;
end $$;
revoke all on function market_item_create(bigint, text, text, text, text, text, text[], int) from public;

-- Owner edits run as app_user: extending is capped at 30 days from now, and leaving the
-- 'given' state is impossible (the RLS policy below also blocks it).
create function market_items_owner_guard() returns trigger language plpgsql as $$
begin
  if current_user = 'app_user' then
    if new.expires_at > now() + interval '30 days' then
      new.expires_at := now() + interval '30 days';
    end if;
    if new.status = 'given' and old.status <> 'given' then
      new.given_at := now();
    end if;
  end if;
  new.updated_at := now();
  return new;
end $$;
create trigger market_items_owner_guard before update on market_items
  for each row execute function market_items_owner_guard();

alter table market_items enable row level security;
alter table market_photos enable row level security;
alter table market_daily enable row level security;
alter table market_rules enable row level security;
alter table market_bans enable row level security;
alter table market_season_sends enable row level security;

grant select, delete on market_items to app_user;
grant update (title, descr, category, condition, pickup_text, status, expires_at, reminded_at) on market_items to app_user;
grant select on market_photos, market_daily, market_rules, market_bans to app_user;
grant insert on market_rules to app_user;

-- A student sees active and reserved items and their own; edits only their own items that are active or
-- reserved, and may move them to active / reserved / given. Hidden, pending, expired and
-- given items cannot be revived by the owner.
create policy market_items_read on market_items for select to app_user
  using (status in ('active', 'reserved') or user_id = app_user_id());
create policy market_items_owner_update on market_items for update to app_user
  using (user_id = app_user_id() and status in ('active', 'reserved'))
  with check (user_id = app_user_id() and status in ('active', 'reserved', 'given'));
create policy market_items_owner_delete on market_items for delete to app_user
  using (user_id = app_user_id());
create policy market_photos_self_read on market_photos for select to app_user using (user_id = app_user_id());
create policy market_daily_self_read on market_daily for select to app_user using (user_id = app_user_id());
create policy market_rules_self on market_rules for select to app_user using (user_id = app_user_id());
create policy market_rules_self_insert on market_rules for insert to app_user with check (user_id = app_user_id());
create policy market_bans_self_read on market_bans for select to app_user using (user_id = app_user_id());
