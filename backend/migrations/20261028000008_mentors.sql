-- Mentors (prompt 07): mentor profiles, mentoring requests, manually matched pairs and their
-- check-in prompts, plus user_badges shared with the market ("Жомарт") and portfolio (09).
--
-- Rules that must hold under concurrency live in Postgres (see 04): mentor capacity and the
-- "adults only" rule for one-on-one pairs are enforced by mentor_match() under a row lock.
-- The Go API calls the functions as the connection owner; app_user only reads its own rows.

create table mentors (
  user_id        bigint primary key references users(id) on delete cascade,
  university     text not null,
  faculty        text,
  areas          text[] not null check (cardinality(areas) between 1 and 4
                   and areas <@ array['adaptation', 'study', 'career', 'life']),
  format         text not null default 'both' check (format in ('one_on_one', 'group', 'both')),
  capacity       smallint not null default 2 check (capacity between 1 and 5),
  group_capacity smallint not null default 8 check (group_capacity between 5 and 10),
  about          text not null default '' check (char_length(about) <= 300),
  alumni         boolean not null default false,
  -- pending: in the moderation queue; paused: the mentor's own break (exam session);
  -- rejected: application declined (may re-apply); removed: admin took the role away.
  status         text not null default 'pending' check (status in ('pending', 'approved', 'paused', 'rejected', 'removed')),
  submission_id  bigint references submissions(id) on delete set null,
  approved_at    timestamptz,
  created_at     timestamptz not null default now(),
  updated_at     timestamptz not null default now()
);
create index mentors_pick on mentors (university) where status = 'approved';

create table mentor_requests (
  id         bigint generated always as identity primary key,
  user_id    bigint not null references users(id) on delete cascade,
  university text not null,
  need       text not null check (need in ('adaptation', 'study', 'career', 'life')),
  goal_text  text not null default '' check (char_length(goal_text) <= 300),
  -- what the student expects: a personal mentor, a group meeting, or either.
  -- Minors (is_adult is not true) are always 'group'.
  expect     text not null default 'any' check (expect in ('one_on_one', 'group', 'any')),
  status     text not null default 'waiting' check (status in ('waiting', 'matched', 'closed')),
  created_at timestamptz not null default now(),
  matched_at timestamptz,
  closed_at  timestamptz
);
-- one open request per student
create unique index mentor_requests_open on mentor_requests (user_id) where status in ('waiting', 'matched');
create index mentor_requests_queue on mentor_requests (university, created_at) where status = 'waiting';

create table mentor_pairs (
  id           bigint generated always as identity primary key,
  mentor_id    bigint not null references users(id) on delete cascade,
  mentee_id    bigint not null references users(id) on delete cascade,
  request_id   bigint references mentor_requests(id) on delete set null,
  matched_by   bigint references users(id) on delete set null,
  university   text not null,
  need         text not null,
  started_at   timestamptz not null default now(),
  check3_at    timestamptz,        -- first time the week-3 "did you meet?" question went out
  month_at     timestamptz,        -- first time the week-4 "continue?" question went out
  continued_at timestamptz,        -- both said yes after the first month
  ended_at     timestamptz,
  end_reason   text check (char_length(end_reason) <= 300),
  ended_by     bigint references users(id) on delete set null,
  mentee_score smallint check (mentee_score between 1 and 5),  -- the mentee rates the mentoring
  mentor_score smallint check (mentor_score between 1 and 5),  -- the mentor rates the pair
  check (mentor_id <> mentee_id)
);
-- a student has at most one active personal mentor
create unique index mentor_pairs_active_mentee on mentor_pairs (mentee_id) where ended_at is null;
create index mentor_pairs_mentor on mentor_pairs (mentor_id) where ended_at is null;
create index mentor_pairs_mentee on mentor_pairs (mentee_id);
create index mentor_pairs_open on mentor_pairs (started_at) where ended_at is null;

-- The week-3 check-in and the week-4 "continue?" question, one row per side.
create table mentor_pair_prompts (
  pair_id     bigint not null references mentor_pairs(id) on delete cascade,
  user_id     bigint not null references users(id) on delete cascade,
  kind        text not null check (kind in ('check3', 'month')),
  sent_at     timestamptz,
  met         boolean,                                 -- check3
  score       smallint check (score between 1 and 5),  -- check3: usefulness
  continue_   boolean,                                 -- month
  answered_at timestamptz,
  primary key (pair_id, user_id, kind)
);
create index mentor_pair_prompts_user on mentor_pair_prompts (user_id);

-- Badges shown in the portfolio (09). verified = confirmed by the platform ("Расталған ✓",
-- e.g. 'mentor' from mentee feedback); unverified = self-declared ('generous' from the market).
create table user_badges (
  user_id    bigint not null references users(id) on delete cascade,
  badge      text not null,
  verified   boolean not null default false,
  awarded_at timestamptz not null default now(),
  primary key (user_id, badge)
);

-- mentor_match pairs a waiting request with an approved mentor. Raises:
--   request_not_waiting, wrong_university, mentee_minor, group_only, self_match,
--   mentor_not_approved, mentor_group_only, mentor_full, mentee_has_mentor
-- p_university limits both sides to one university (ambassador-leads); null = any (admin).
create function mentor_match(p_request bigint, p_mentor bigint, p_actor bigint, p_university text)
returns bigint language plpgsql as $$
declare
  req mentor_requests%rowtype;
  m mentors%rowtype;
  adult boolean;
  active int;
  pair_id bigint;
begin
  select * into req from mentor_requests where id = p_request for update;
  if not found or req.status <> 'waiting' then
    raise exception 'request_not_waiting';
  end if;
  if p_university is not null and req.university is distinct from p_university then
    raise exception 'wrong_university';
  end if;
  select is_adult into adult from users where id = req.user_id and deleted_at is null;
  if adult is not true then
    raise exception 'mentee_minor';
  end if;
  if req.expect = 'group' then
    raise exception 'group_only';
  end if;
  if req.user_id = p_mentor then
    raise exception 'self_match';
  end if;
  -- the mentor row lock serialises concurrent matches against the same capacity
  select * into m from mentors where user_id = p_mentor for update;
  if not found or m.status <> 'approved' then
    raise exception 'mentor_not_approved';
  end if;
  if p_university is not null and m.university is distinct from p_university then
    raise exception 'wrong_university';
  end if;
  if m.format = 'group' then
    raise exception 'mentor_group_only';
  end if;
  select count(*) into active from mentor_pairs where mentor_id = p_mentor and ended_at is null;
  if active >= m.capacity then
    raise exception 'mentor_full';
  end if;
  begin
    insert into mentor_pairs (mentor_id, mentee_id, request_id, matched_by, university, need)
    values (p_mentor, req.user_id, req.id, p_actor, req.university, req.need)
    returning id into pair_id;
  exception when unique_violation then
    raise exception 'mentee_has_mentor';
  end;
  update mentor_requests set status = 'matched', matched_at = now() where id = req.id;
  return pair_id;
end $$;
revoke all on function mentor_match(bigint, bigint, bigint, text) from public;

-- RLS: a student reads only their own mentor profile, requests, pairs and prompts. Every
-- change goes through the API (as the owner, with explicit participant checks), except the
-- mentor's own profile edits and request close, which run as app_user under these policies.
alter table mentors enable row level security;
alter table mentor_requests enable row level security;
alter table mentor_pairs enable row level security;
alter table mentor_pair_prompts enable row level security;
alter table user_badges enable row level security;

grant select on mentors, mentor_requests, mentor_pairs, mentor_pair_prompts, user_badges to app_user;
grant update (areas, format, capacity, group_capacity, about, status, updated_at) on mentors to app_user;
grant update (status, closed_at) on mentor_requests to app_user;

create policy mentors_self_read on mentors for select to app_user using (user_id = app_user_id());
-- the mentor may only toggle between approved and paused and edit the profile; pending,
-- rejected and removed rows are not editable by the mentor at all
create policy mentors_self_update on mentors for update to app_user
  using (user_id = app_user_id() and status in ('approved', 'paused'))
  with check (user_id = app_user_id() and status in ('approved', 'paused'));
create policy mentor_requests_self_read on mentor_requests for select to app_user using (user_id = app_user_id());
create policy mentor_requests_self_close on mentor_requests for update to app_user
  using (user_id = app_user_id() and status = 'waiting')
  with check (user_id = app_user_id() and status = 'closed');
create policy mentor_pairs_party_read on mentor_pairs for select to app_user
  using (mentor_id = app_user_id() or mentee_id = app_user_id());
create policy mentor_pair_prompts_self_read on mentor_pair_prompts for select to app_user using (user_id = app_user_id());
create policy user_badges_self_read on user_badges for select to app_user using (user_id = app_user_id());
