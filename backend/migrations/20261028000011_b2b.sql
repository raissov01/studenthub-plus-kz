-- Employer and university cabinets (prompt 10): verified employers publish jobs, students
-- apply with exactly the fields they choose, anti-ghosting statuses; university staff publish
-- official announcements / opportunities and see aggregate numbers about their own university.
--
-- Access model (see docs/api/b2b.md):
--   * Company membership (employer_members) is the authority, not users.role. One company per
--     account. app_employer_member(e) = caller belongs to company e (any status);
--     app_employer_active(e) = ... and the company is verified.
--   * Employers read and change their own company, jobs and the applications sent to them as
--     app_user, so RLS stops cross-company access even if a Go check were missing.
--   * A student applies through job_apply() (security definer): the job must be published, not
--     expired, the company verified, and a job marked "not suitable for minors" refuses
--     students whose is_adult is not true. The application row holds only the snapshot of the
--     fields the student chose (shared_fields); it cascades with the student's account.
--   * University staff are bound to ONE university by university_staff (granted by admin), not
--     by users.university (which a student may edit). Their posts are written as app_user and
--     limited to app_staff_university(). Statistics are aggregate-only and computed by the
--     server after the same check.

create table employers (
  id                bigint generated always as identity primary key,
  owner_user_id     bigint references users(id) on delete set null,  -- null only after the owner's account is deleted and nobody took over
  name              text not null check (char_length(name) between 2 and 120),
  bin               text not null check (bin ~ '^[0-9]{12}$'),         -- БСН
  website           text not null check (website like 'https://%' and char_length(website) <= 300),  -- site or 2GIS link
  contact_name      text not null check (char_length(contact_name) between 2 and 80),
  contact_phone     text not null default '' check (char_length(contact_phone) <= 30),  -- the company's phone, never a student's
  address           text not null default '' check (char_length(address) <= 200),
  about             text not null default '' check (char_length(about) <= 1000),
  -- pending: in the queue; verified: «Тексерілген»; rejected: may apply again;
  -- suspended: by admin; closed: stopped by its owner (or the owner left with nobody to hand over to)
  status            text not null default 'pending' check (status in ('pending', 'verified', 'rejected', 'suspended', 'closed')),
  status_reason     text check (char_length(status_reason) <= 500),
  submission_id     bigint references submissions(id) on delete set null,
  verified_at       timestamptz,
  verified_by       bigint references users(id) on delete set null,
  low_rate_warned_at timestamptz,                                       -- admin was told the response rate is < 30%
  created_at        timestamptz not null default now(),
  updated_at        timestamptz not null default now()
);
create unique index employers_bin_live on employers (bin) where status in ('pending', 'verified', 'suspended');
create index employers_status on employers (status);
create index employers_owner on employers (owner_user_id) where owner_user_id is not null;

create table employer_members (
  employer_id       bigint not null references employers(id) on delete cascade,
  user_id           bigint not null references users(id) on delete cascade,
  role              text not null default 'member' check (role in ('owner', 'member')),
  rules_accepted_at timestamptz,                                       -- the job-posting rules screen, once
  added_by          bigint references users(id) on delete set null,
  created_at        timestamptz not null default now(),
  primary key (employer_id, user_id)
);
create unique index employer_members_one_company on employer_members (user_id);
create unique index employer_members_one_owner on employer_members (employer_id) where role = 'owner';

-- One-time invite codes the owner hands to a colleague.
create table employer_invites (
  code        text primary key check (code ~ '^[a-z0-9]{12}$'),
  employer_id bigint not null references employers(id) on delete cascade,
  created_by  bigint references users(id) on delete set null,
  expires_at  timestamptz not null,
  used_by     bigint references users(id) on delete set null,
  used_at     timestamptz,
  created_at  timestamptz not null default now()
);
create index employer_invites_employer on employer_invites (employer_id);

create table jobs (
  id            bigint generated always as identity primary key,
  employer_id   bigint not null references employers(id) on delete cascade,
  created_by    bigint references users(id) on delete set null,
  title_kk      text not null check (char_length(title_kk) between 3 and 120),
  title_ru      text not null default '' check (char_length(title_ru) <= 120),
  desc_kk       text not null check (char_length(desc_kk) between 20 and 3000),
  desc_ru       text not null default '' check (char_length(desc_ru) <= 3000),
  city          text not null,
  remote        boolean not null default false,
  address       text not null default '' check (char_length(address) <= 200),
  pay           numeric(12, 0) not null check (pay > 0),
  per           text not null check (per in ('shift', 'hour', 'month')),
  hours         numeric(5, 1) check (hours > 0 and hours <= 168),
  sched_kk      text not null check (char_length(sched_kk) between 3 and 200),
  sched_ru      text not null default '' check (char_length(sched_ru) <= 200),
  tags          text[] not null default '{}' check (tags <@ array['noexp', 'daily', 'remote', 'near']),
  skills        text[] not null default '{}' check (cardinality(skills) <= 10),
  spec          text[] not null default '{all}' check (cardinality(spec) between 1 and 10),
  req           jsonb not null default '[]' check (jsonb_typeof(req) = 'array'),   -- [{kk, ru}]
  apply_kk      text not null check (char_length(apply_kk) between 3 and 500),  -- "қалай өтінім беріледі"
  apply_ru      text not null default '' check (char_length(apply_ru) <= 500),
  minor_ok      boolean not null,                                -- "18-ге толмағандарға сай ма?" (required)
  -- pending: first 3 jobs of a company wait in the queue; hidden: by reports / admin
  status        text not null default 'pending' check (status in ('pending', 'published', 'rejected', 'hidden', 'closed', 'expired')),
  hidden_reason text check (hidden_reason in ('reports', 'admin')),
  status_reason text check (char_length(status_reason) <= 500),
  submission_id bigint references submissions(id) on delete set null,
  approved_at   timestamptz,                                     -- passed the queue or published directly
  published_at  timestamptz,
  expires_at    timestamptz,                                     -- published_at + 30 days, extendable
  closed_at     timestamptz,
  created_at    timestamptz not null default now(),
  updated_at    timestamptz not null default now(),
  check (remote or char_length(address) >= 3),
  check (per <> 'shift' or hours is not null)
);
create index jobs_employer on jobs (employer_id, created_at desc);
create index jobs_live on jobs (published_at desc) where status = 'published';
create index jobs_expiry on jobs (expires_at) where status = 'published';
create index jobs_created_by on jobs (created_by) where created_by is not null;

-- The first 3 jobs of a company go through the moderation queue, later ones publish at once.
-- Enforced here so that no code path can skip the queue.
create function jobs_insert_guard() returns trigger language plpgsql as $$
declare
  approved int;
begin
  select count(*) into approved from jobs where employer_id = new.employer_id and approved_at is not null;
  new.hidden_reason := null;
  new.submission_id := null;
  new.closed_at := null;
  if approved >= 3 then
    new.status := 'published';
    new.approved_at := now();
    new.published_at := now();
    new.expires_at := now() + interval '30 days';
  else
    new.status := 'pending';
    new.approved_at := null;
    new.published_at := null;
    new.expires_at := null;
  end if;
  return new;
end $$;
create trigger jobs_insert_guard before insert on jobs for each row execute function jobs_insert_guard();

-- Employer edits run as app_user: they may close a job, reopen a closed / expired one that was
-- approved before (never one hidden by reports or an admin), and extend it by at most 30 days.
create function jobs_owner_guard() returns trigger language plpgsql as $$
begin
  if current_user = 'app_user' then
    if new.status is distinct from old.status then
      if not (
        (new.status = 'closed' and old.status in ('pending', 'published', 'expired'))
        or (new.status = 'published' and old.status in ('closed', 'expired') and old.approved_at is not null and old.hidden_reason is null)
      ) then
        raise exception 'job_status';
      end if;
      if new.status = 'closed' then
        new.closed_at := now();
      end if;
      if new.status = 'published' then
        new.expires_at := now() + interval '30 days';
        new.closed_at := null;
      end if;
    end if;
    if new.expires_at is distinct from old.expires_at and new.expires_at > now() + interval '30 days' then
      new.expires_at := now() + interval '30 days';
    end if;
  end if;
  new.updated_at := now();
  return new;
end $$;
create trigger jobs_owner_guard before update on jobs for each row execute function jobs_owner_guard();

create table applications (
  id            bigint generated always as identity primary key,
  job_id        bigint not null references jobs(id) on delete cascade,
  employer_id   bigint not null references employers(id) on delete cascade,
  user_id       bigint not null references users(id) on delete cascade,
  shared_fields jsonb not null check (jsonb_typeof(shared_fields) = 'object'),  -- the copy the student chose to send
  status        text not null default 'sent' check (status in ('sent', 'viewed', 'invited', 'rejected', 'no_response', 'withdrawn')),
  invite_message text check (char_length(invite_message) <= 500),
  viewed_at     timestamptz,
  responded_at  timestamptz,                                     -- invited / rejected
  reminded_at   timestamptz,                                     -- employer got the 5-day reminder
  student_notified_at timestamptz,                               -- the no_response message went out
  created_at    timestamptz not null default now(),
  updated_at    timestamptz not null default now(),
  unique (job_id, user_id)
);
create index applications_user on applications (user_id, created_at desc);
create index applications_employer on applications (employer_id, created_at desc);
create index applications_open on applications (created_at) where status in ('sent', 'viewed');

-- University staff: bound to exactly one university by an admin.
create table university_staff (
  user_id    bigint primary key references users(id) on delete cascade,
  university text not null check (char_length(university) between 2 and 120),
  granted_by bigint references users(id) on delete set null,
  created_at timestamptz not null default now()
);
create index university_staff_university on university_staff (university);

-- «Университеттен ресми»: announcements and opportunities, published without the queue.
create table university_posts (
  id           bigint generated always as identity primary key,
  university   text not null,
  author_id    bigint references users(id) on delete set null,
  kind         text not null check (kind in ('announcement', 'opportunity')),
  payload      jsonb not null check (jsonb_typeof(payload) = 'object'),
  deadline     date,
  hidden       boolean not null default false,
  published_at timestamptz not null default now(),
  updated_at   timestamptz not null default now()
);
create index university_posts_live on university_posts (kind, published_at desc) where not hidden;
create index university_posts_university on university_posts (university, published_at desc);
create index university_posts_author on university_posts (author_id) where author_id is not null;

-- ---------- helper functions (security definer: they read membership without RLS recursion) ----------

create function app_employer_member(e bigint) returns boolean
language sql stable security definer set search_path = public, pg_temp as $$
  select exists (select 1 from employer_members m join users u on u.id = m.user_id and u.deleted_at is null
    where m.employer_id = e and m.user_id = app_user_id())
$$;

create function app_employer_active(e bigint) returns boolean
language sql stable security definer set search_path = public, pg_temp as $$
  select exists (select 1 from employer_members m
    join employers c on c.id = m.employer_id and c.status = 'verified'
    join users u on u.id = m.user_id and u.deleted_at is null
    where m.employer_id = e and m.user_id = app_user_id())
$$;

create function app_staff_university() returns text
language sql stable security definer set search_path = public, pg_temp as $$
  select s.university from university_staff s join users u on u.id = s.user_id and u.deleted_at is null
    and u.role = 'university_staff'
  where s.user_id = app_user_id()
$$;

-- Apply to a job as the signed-in student. Result codes:
--   ok | not_found | minor | already | own_company | no_consent
create function job_apply(p_job bigint, p_fields jsonb)
returns table (result text, application_id bigint)
language plpgsql security definer set search_path = public, pg_temp as $$
declare
  uid bigint := app_user_id();
  j   jobs%rowtype;
  u   users%rowtype;
  aid bigint;
begin
  select * into u from users where id = uid and deleted_at is null;
  if not found or u.consent_at is null then
    return query select 'no_consent'::text, null::bigint; return;
  end if;
  select * into j from jobs where id = p_job;
  if not found or j.status <> 'published' or j.expires_at <= now()
     or not exists (select 1 from employers c where c.id = j.employer_id and c.status = 'verified') then
    return query select 'not_found'::text, null::bigint; return;
  end if;
  if not j.minor_ok and u.is_adult is not true then
    return query select 'minor'::text, null::bigint; return;
  end if;
  if exists (select 1 from employer_members m where m.employer_id = j.employer_id and m.user_id = uid) then
    return query select 'own_company'::text, null::bigint; return;
  end if;
  insert into applications (job_id, employer_id, user_id, shared_fields)
    values (j.id, j.employer_id, uid, p_fields)
    on conflict (job_id, user_id) do nothing
    returning id into aid;
  if aid is null then
    return query select 'already'::text, null::bigint; return;
  end if;
  return query select 'ok'::text, aid;
end $$;

revoke all on function app_employer_member(bigint) from public;
revoke all on function app_employer_active(bigint) from public;
revoke all on function app_staff_university() from public;
revoke all on function job_apply(bigint, jsonb) from public;
grant execute on function app_employer_member(bigint) to app_user;
grant execute on function app_employer_active(bigint) to app_user;
grant execute on function app_staff_university() to app_user;
grant execute on function job_apply(bigint, jsonb) to app_user;

-- A bound staff member cannot move to another university by editing the profile: the
-- university field of their users row is kept as is for app_user updates.
create function users_staff_university_guard() returns trigger language plpgsql as $$
begin
  if current_user = 'app_user' and new.university is distinct from old.university
     and exists (select 1 from university_staff s where s.user_id = old.id) then
    new.university := old.university;
  end if;
  return new;
end $$;
create trigger users_staff_university_guard before update on users
  for each row execute function users_staff_university_guard();

-- ---------- RLS ----------

alter table employers enable row level security;
alter table employer_members enable row level security;
alter table employer_invites enable row level security;
alter table jobs enable row level security;
alter table applications enable row level security;
alter table university_staff enable row level security;
alter table university_posts enable row level security;

-- employers: members read their own company; members of a verified company edit the contact
-- block (name and БСН are fixed by verification). Creation, status changes and ownership go
-- through the server after Go checks.
grant select on employers to app_user;
grant update (website, contact_name, contact_phone, address, about, updated_at) on employers to app_user;
create policy employers_member_read on employers for select to app_user using (app_employer_member(id));
create policy employers_member_update on employers for update to app_user
  using (app_employer_active(id)) with check (app_employer_active(id));

grant select on employer_members to app_user;
create policy employer_members_read on employer_members for select to app_user using (app_employer_member(employer_id));
-- employer_invites: server only (no policy).

-- jobs: members of the company read all its jobs; members of a verified company edit them
-- (jobs_owner_guard limits status / expiry changes). Inserts go through the server so the
-- queue item is created in the same transaction; jobs_insert_guard decides the status.
grant select on jobs to app_user;
grant update (title_kk, title_ru, desc_kk, desc_ru, city, remote, address, pay, per, hours, sched_kk, sched_ru,
  tags, skills, spec, req, apply_kk, apply_ru, minor_ok, status, expires_at, updated_at) on jobs to app_user;
create policy jobs_member_read on jobs for select to app_user using (app_employer_member(employer_id));
-- a student reads the jobs they applied to (their own application list / data export)
create policy jobs_applicant_read on jobs for select to app_user
  using (exists (select 1 from applications a where a.job_id = jobs.id and a.user_id = app_user_id()));
create policy jobs_member_update on jobs for update to app_user
  using (app_employer_active(employer_id)) with check (app_employer_active(employer_id));

-- applications: the student reads their own and may only withdraw; a member of the verified
-- company the application was sent to reads it and moves it to viewed / invited / rejected.
grant select on applications to app_user;
grant update (status, invite_message, viewed_at, responded_at, shared_fields, updated_at) on applications to app_user;
create policy applications_own_read on applications for select to app_user using (user_id = app_user_id());
create policy applications_employer_read on applications for select to app_user using (app_employer_active(employer_id));
create policy applications_own_withdraw on applications for update to app_user
  using (user_id = app_user_id() and status in ('sent', 'viewed', 'invited'))
  with check (user_id = app_user_id() and status = 'withdrawn' and shared_fields = '{}'::jsonb);
create policy applications_employer_status on applications for update to app_user
  using (app_employer_active(employer_id) and status in ('sent', 'viewed', 'invited'))
  with check (app_employer_active(employer_id) and status in ('viewed', 'invited', 'rejected'));

-- The employer may change only the status columns; the snapshot itself stays as the student sent it.
create function applications_guard() returns trigger language plpgsql as $$
begin
  if current_user = 'app_user' and old.user_id <> app_user_id() then
    new.shared_fields := old.shared_fields;
  end if;
  new.updated_at := now();
  return new;
end $$;
create trigger applications_guard before update on applications for each row execute function applications_guard();

grant select on university_staff to app_user;
create policy university_staff_self on university_staff for select to app_user using (user_id = app_user_id());

grant select, delete on university_posts to app_user;
grant insert (university, author_id, kind, payload, deadline) on university_posts to app_user;
grant update (payload, deadline, hidden, updated_at) on university_posts to app_user;
create policy university_posts_staff on university_posts for all to app_user
  using (university = app_staff_university())
  with check (university = app_staff_university());
