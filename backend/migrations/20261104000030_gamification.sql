-- Gamification («Ұпайлар / Баллы», pkg/points, docs/api/points.md): XP + daily streak, coins in an
-- append-only ledger, a rewards catalog and an admin-moderated redemption queue.
--
-- Coins are NOT money. There are no cash payouts and no transfers between students; a cash-out
-- would need a licensed payment partner and a lawyer first (see docs/api/points.md, «Ақша»).
--
-- Every write goes through the server as the connection owner (pkg/points); app_user may only
-- read its own rows. The same action never pays twice: unique (user_id, reason, ref_type, ref_id)
-- on both ledgers. The cached balance lives in points_wallets and is updated in the same
-- transaction as the ledger row; coins >= 0 is a CHECK, so two parallel redemptions can never
-- overspend even if the row lock were forgotten.

create table points_wallets (
  user_id             bigint primary key references users(id) on delete cascade,
  coins               integer not null default 0 check (coins >= 0),
  xp                  bigint not null default 0 check (xp >= 0),
  streak_days         integer not null default 0 check (streak_days >= 0),
  streak_best         integer not null default 0 check (streak_best >= 0),
  last_active_day     date,                      -- Asia/Almaty calendar day
  freezes             smallint not null default 0 check (freezes between 0 and 1),
  -- academic group within the university («ИС-21»), normalized upper-case by the server
  study_group         text check (char_length(study_group) between 1 and 24 and study_group = btrim(study_group)),
  leaderboard_opt_out boolean not null default false,
  updated_at          timestamptz not null default now()
);
create index points_wallets_group on points_wallets (study_group) where study_group is not null;

create table xp_events (
  id         bigint generated always as identity primary key,
  user_id    bigint not null references users(id) on delete cascade,
  delta      integer not null check (delta <> 0),
  reason     text not null check (reason ~ '^[a-z_]{2,40}$'),
  ref_type   text not null check (ref_type ~ '^[a-z_]{2,40}$'),
  ref_id     text not null check (char_length(ref_id) between 1 and 80),
  day        date not null,                       -- Asia/Almaty day, for the daily caps
  created_at timestamptz not null default now(),
  unique (user_id, reason, ref_type, ref_id)
);
create index xp_events_user_day on xp_events (user_id, day, reason);
create index xp_events_created on xp_events (created_at, user_id);

create table coin_ledger (
  id         bigint generated always as identity primary key,
  user_id    bigint not null references users(id) on delete cascade,
  delta      integer not null check (delta <> 0),
  reason     text not null check (reason ~ '^[a-z_]{2,40}$'),
  ref_type   text not null check (ref_type ~ '^[a-z_]{2,40}$'),
  ref_id     text not null check (char_length(ref_id) between 1 and 80),
  actor_id   bigint references users(id) on delete set null,  -- admin of a manual adjustment / decision
  note       text check (char_length(note) <= 500),
  day        date not null,
  created_at timestamptz not null default now(),
  unique (user_id, reason, ref_type, ref_id)
);
create index coin_ledger_user on coin_ledger (user_id, id desc);
create index coin_ledger_user_day on coin_ledger (user_id, day);

-- Append-only: no UPDATE/DELETE, except what account deletion itself needs (rows of a user that
-- no longer exists cascade away; an admin actor is anonymized by SET NULL).
create function points_append_only() returns trigger language plpgsql as $$
begin
  if tg_op = 'DELETE' then
    if exists (select 1 from users where id = old.user_id) then
      raise exception '% is append-only', tg_table_name;
    end if;
    return old;
  end if;
  if tg_table_name = 'coin_ledger' and new.actor_id is null and old.actor_id is not null
     and (new.id, new.user_id, new.delta, new.reason, new.ref_type, new.ref_id, new.note, new.day, new.created_at)
         is not distinct from (old.id, old.user_id, old.delta, old.reason, old.ref_type, old.ref_id, old.note, old.day, old.created_at) then
    return new;
  end if;
  raise exception '% is append-only', tg_table_name;
end $$;
create trigger coin_ledger_append_only before update or delete on coin_ledger
  for each row execute function points_append_only();
create trigger xp_events_append_only before update or delete on xp_events
  for each row execute function points_append_only();

-- Rewards catalog (admin panel). kind: discount = a partner promo code (discount_id is the
-- /api/content discounts[].id), merch, print_quota (placeholder), priority_event (placeholder).
create table rewards (
  id             bigint generated always as identity primary key,
  kind           text not null check (kind in ('discount', 'merch', 'print_quota', 'priority_event')),
  title_kk       text not null check (char_length(title_kk) between 2 and 120),
  title_ru       text not null check (char_length(title_ru) between 2 and 120),
  desc_kk        text not null default '' check (char_length(desc_kk) <= 600),
  desc_ru        text not null default '' check (char_length(desc_ru) <= 600),
  cost           integer not null check (cost between 1 and 100000),
  stock          integer check (stock >= 0),          -- null = unlimited
  per_user_limit smallint not null default 1 check (per_user_limit between 1 and 50),
  discount_id    text check (discount_id ~ '^[A-Za-z0-9_-]{1,64}$'),
  active         boolean not null default true,
  created_by     bigint references users(id) on delete set null,
  created_at     timestamptz not null default now(),
  updated_at     timestamptz not null default now()
);

create table reward_redemptions (
  id          bigint generated always as identity primary key,
  user_id     bigint not null references users(id) on delete cascade,
  reward_id   bigint not null references rewards(id),
  cost        integer not null check (cost > 0),
  status      text not null default 'pending' check (status in ('pending', 'approved', 'rejected')),
  code        text check (char_length(code) <= 200),   -- shown to the student once approved
  note        text check (char_length(note) <= 500),   -- admin note / rejection reason
  idem_key    text not null check (idem_key ~ '^[A-Za-z0-9_-]{8,64}$'),
  decided_by  bigint references users(id) on delete set null,
  decided_at  timestamptz,
  created_at  timestamptz not null default now(),
  unique (user_id, idem_key)
);
create index reward_redemptions_queue on reward_redemptions (status, created_at);
create index reward_redemptions_user on reward_redemptions (user_id, id desc);

-- Optional pool of codes per reward; approval hands out the next unused one.
create table reward_codes (
  id            bigint generated always as identity primary key,
  reward_id     bigint not null references rewards(id) on delete cascade,
  code          text not null check (char_length(code) between 1 and 200),
  redemption_id bigint unique references reward_redemptions(id) on delete set null,
  created_at    timestamptz not null default now(),
  unique (reward_id, code)
);
create index reward_codes_free on reward_codes (reward_id, id) where redemption_id is null;

alter table points_wallets enable row level security;
alter table xp_events enable row level security;
alter table coin_ledger enable row level security;
alter table rewards enable row level security;
alter table reward_redemptions enable row level security;
alter table reward_codes enable row level security;

-- Reads only; the server (owner) does every write. reward_codes: no app_user access at all.
grant select on points_wallets, xp_events, coin_ledger, reward_redemptions, rewards to app_user;
create policy points_wallets_self on points_wallets for select to app_user using (user_id = app_user_id());
create policy xp_events_self on xp_events for select to app_user using (user_id = app_user_id());
create policy coin_ledger_self on coin_ledger for select to app_user using (user_id = app_user_id());
create policy reward_redemptions_self on reward_redemptions for select to app_user using (user_id = app_user_id());
create policy rewards_active on rewards for select to app_user using (active);
