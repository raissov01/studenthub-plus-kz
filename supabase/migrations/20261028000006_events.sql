-- Events (05): real events published by ambassadors / university staff / admin, registrations
-- with capacity + waitlist, QR check-in, small teams, feedback and certificates.
--
-- Access model:
--   * app_user (a signed-in student, see pkg/db.WithUser) reads published / finished / cancelled
--     events and the ones it may manage; it never sees checkin_key.
--   * event writes run as app_user too and are limited by app_can_manage_university().
--   * registration and cancellation go through the security definer functions below, which
--     lock the event row so concurrent registrations never exceed capacity.
--   * check-in, finishing and team assignment run as the connection owner after Go checks.
--   * checked_in_at is the ONLY source of "verified attendance" (portfolio, prompt 09).

create type event_status as enum ('draft', 'published', 'finished', 'cancelled');

create table events (
  id              bigint generated always as identity primary key,
  title_kk        text not null check (length(title_kk) between 3 and 140),
  title_ru        text check (length(title_ru) <= 140),
  desc_kk         text check (length(desc_kk) <= 3000),
  desc_ru         text check (length(desc_ru) <= 3000),
  cat             text not null,                 -- career, youth, volunteer, sport, training, conf, holiday, mentoring, other
  starts_at       timestamptz not null,
  ends_at         timestamptz not null,
  university      text not null,
  building_id     text,                          -- id in the frontend BUILDINGS map (b1, b2, ...)
  place_text      text check (length(place_text) <= 200),
  capacity        int check (capacity > 0),      -- null = unlimited
  walk_in_allowed boolean not null default false,
  gives_cert      boolean not null default false,
  teams_enabled   boolean not null default false,  -- "split into teams" (stage 4)
  status          event_status not null default 'draft',
  created_by      bigint references users(id) on delete set null,
  checkin_key     bytea,                         -- per-event QR signing nonce; rotated by "start check-in"; never readable by app_user
  checkin_started_at timestamptz,
  finished_at     timestamptz,
  created_at      timestamptz not null default now(),
  updated_at      timestamptz not null default now(),
  check (ends_at > starts_at)
);
create index events_upcoming on events (status, starts_at);
create index events_university on events (university, starts_at);
create index events_created_by on events (created_by) where created_by is not null;

create table event_regs (
  event_id        bigint not null references events(id) on delete cascade,
  user_id         bigint not null references users(id) on delete cascade,
  status          text not null default 'registered' check (status in ('registered', 'waitlist')),
  ticket          text not null unique,          -- random per-registration e-ticket code (manual check-in fallback)
  registered_at   timestamptz not null default now(),
  checked_in_at   timestamptz,                   -- the ONLY "verified attendance" source
  checkin_method  text check (checkin_method in ('qr', 'manual', 'walk_in')),
  checkin_revoked_at timestamptz,                -- organizer pressed "not confirmed"; QR self check-in is refused afterwards
  team_no         smallint,
  prefer_prev_team boolean not null default false,
  feedback_score  smallint check (feedback_score between 1 and 5),
  feedback_text   text check (length(feedback_text) <= 500),
  feedback_at     timestamptz,
  reminded_at     timestamptz,
  feedback_asked_at timestamptz,
  primary key (event_id, user_id)
);
create index event_regs_user on event_regs (user_id);
create index event_regs_waitlist on event_regs (event_id, registered_at) where status = 'waitlist';

-- Certificates issued when a gives_cert event is finished (portfolio reads these).
create table event_certificates (
  id        bigint generated always as identity primary key,
  event_id  bigint not null references events(id) on delete cascade,
  user_id   bigint not null references users(id) on delete cascade,
  issued_at timestamptz not null default now(),
  unique (event_id, user_id)
);
create index event_certificates_user on event_certificates (user_id);

-- A QR scanned by someone who is not signed in yet: the token is accepted at scan time and
-- held for 10 minutes; signing in completes the check-in. No user reference by design.
create table event_checkin_claims (
  id         text primary key,                   -- random, handed to the browser
  event_id   bigint not null references events(id) on delete cascade,
  created_at timestamptz not null default now(),
  expires_at timestamptz not null,
  used_at    timestamptz
);
create index event_checkin_claims_expires on event_checkin_claims (expires_at);

-- Verified attendance for the portfolio (09): one row per checked-in registration.
create view verified_attendance with (security_invoker = true) as
  select r.user_id, r.event_id, e.title_kk, e.title_ru, e.cat, e.university, e.starts_at,
         r.checked_in_at, (c.id is not null) as has_certificate
  from event_regs r
  join events e on e.id = r.event_id
  left join event_certificates c on c.event_id = r.event_id and c.user_id = r.user_id
  where r.checked_in_at is not null;

-- True when the signed-in user may create / edit events of university u.
-- Mentors (07) get a narrower right (cat = 'mentoring' only) — added in 07's migration.
create or replace function app_can_manage_university(u text) returns boolean
language sql stable as $$
  select exists (select 1 from users
    where id = app_user_id() and deleted_at is null
      and (role = 'admin' or (role in ('ambassador', 'university_staff') and university is not null and university = u)))
$$;

-- Register the signed-in user. Locks the event row, so concurrent calls are serialized and
-- the count of 'registered' rows never exceeds capacity. Result codes:
--   registered | waitlist | already | not_found | closed
create or replace function event_register(p_event bigint, p_ticket text, p_prev_team boolean)
returns table (result text, reg_status text, ticket text)
language plpgsql security definer set search_path = public, pg_temp as $$
declare
  ev  events%rowtype;
  uid bigint := app_user_id();
  n   int;
  st  text;
begin
  if uid is null then
    return query select 'not_found'::text, null::text, null::text; return;
  end if;
  select * into ev from events where id = p_event for update;
  if not found or ev.status = 'draft' then
    return query select 'not_found'::text, null::text, null::text; return;
  end if;
  if ev.status <> 'published' or ev.ends_at <= now() then
    return query select 'closed'::text, null::text, null::text; return;
  end if;
  if exists (select 1 from event_regs r where r.event_id = p_event and r.user_id = uid) then
    return query select 'already'::text, r.status, r.ticket from event_regs r where r.event_id = p_event and r.user_id = uid;
    return;
  end if;
  select count(*) into n from event_regs r where r.event_id = p_event and r.status = 'registered';
  st := case when ev.capacity is null or n < ev.capacity then 'registered' else 'waitlist' end;
  insert into event_regs (event_id, user_id, status, ticket, prefer_prev_team)
    values (p_event, uid, st, p_ticket, coalesce(p_prev_team, false));
  return query select st, st, p_ticket;
end $$;

-- Cancel the signed-in user's registration. When a confirmed seat frees up, the first person
-- on the waitlist is promoted; their user id is returned so the API can send a direct message.
create or replace function event_unregister(p_event bigint)
returns table (result text, promoted_user bigint)
language plpgsql security definer set search_path = public, pg_temp as $$
declare
  uid  bigint := app_user_id();
  old  text;
  ev   events%rowtype;
  nxt  bigint;
begin
  select * into ev from events where id = p_event for update;
  if not found then
    return query select 'not_found'::text, null::bigint; return;
  end if;
  if ev.status not in ('published', 'draft') or ev.starts_at <= now() then
    return query select 'closed'::text, null::bigint; return;
  end if;
  delete from event_regs r where r.event_id = p_event and r.user_id = uid and r.checked_in_at is null
    returning r.status into old;
  if old is null then
    return query select 'not_registered'::text, null::bigint; return;
  end if;
  if old = 'registered' then
    update event_regs r set status = 'registered'
      where (r.event_id, r.user_id) = (select w.event_id, w.user_id from event_regs w
        where w.event_id = p_event and w.status = 'waitlist' order by w.registered_at, w.user_id limit 1)
      returning r.user_id into nxt;
  end if;
  return query select 'ok'::text, nxt;
end $$;

revoke all on function event_register(bigint, text, boolean) from public;
revoke all on function event_unregister(bigint) from public;
grant execute on function event_register(bigint, text, boolean) to app_user;
grant execute on function event_unregister(bigint) to app_user;
grant execute on function app_can_manage_university(text) to app_user;

alter table events enable row level security;
alter table event_regs enable row level security;
alter table event_certificates enable row level security;
alter table event_checkin_claims enable row level security;

-- events: every column except checkin_key
grant select (id, title_kk, title_ru, desc_kk, desc_ru, cat, starts_at, ends_at, university, building_id,
  place_text, capacity, walk_in_allowed, gives_cert, teams_enabled, status, created_by, checkin_started_at,
  finished_at, created_at, updated_at) on events to app_user;
grant insert (title_kk, title_ru, desc_kk, desc_ru, cat, starts_at, ends_at, university, building_id,
  place_text, capacity, walk_in_allowed, gives_cert, teams_enabled, status, created_by) on events to app_user;
grant update (title_kk, title_ru, desc_kk, desc_ru, cat, starts_at, ends_at, building_id,
  place_text, capacity, walk_in_allowed, gives_cert, teams_enabled, status, updated_at) on events to app_user;
create policy events_public_read on events for select to app_user
  using (status in ('published', 'finished', 'cancelled') or app_can_manage_university(university));
create policy events_manage_insert on events for insert to app_user
  with check (created_by = app_user_id() and app_can_manage_university(university));
create policy events_manage_update on events for update to app_user
  using (app_can_manage_university(university)) with check (app_can_manage_university(university));

-- event_regs: own rows; organizers see the registrations of events they manage
grant select on event_regs to app_user;
grant update (feedback_score, feedback_text, feedback_at) on event_regs to app_user;
create policy event_regs_own_read on event_regs for select to app_user using (user_id = app_user_id());
create policy event_regs_manager_read on event_regs for select to app_user
  using (exists (select 1 from events e where e.id = event_id and app_can_manage_university(e.university)));
create policy event_regs_own_feedback on event_regs for update to app_user
  using (user_id = app_user_id() and checked_in_at is not null) with check (user_id = app_user_id());

grant select on event_certificates to app_user;
create policy event_certificates_own on event_certificates for select to app_user using (user_id = app_user_id());

grant select on verified_attendance to app_user;
-- event_checkin_claims: server only (no policy).
