-- Ambassadors (06): applications (reviewed through the shared `submissions` queue), terms,
-- referral codes, workload pause, certificates; plus `community_content`, the published copy
-- of approved opportunity / place submissions that /api/content merges in (source = ambassador).

create table ambassador_applications (
  id             bigint generated always as identity primary key,
  user_id        bigint not null references users(id) on delete cascade,
  university     text not null check (length(university) between 2 and 120),
  motivation     text not null check (length(motivation) between 10 and 300),
  hours_per_week text not null check (hours_per_week in ('1-2', '2-3', '3-5')),
  status         submission_status not null default 'pending',
  reason         text,
  submission_id  bigint references submissions(id) on delete set null,
  reviewed_by    bigint references users(id) on delete set null,
  reviewed_at    timestamptz,
  created_at     timestamptz not null default now()
);
create index ambassador_applications_user on ambassador_applications (user_id, created_at desc);
create unique index ambassador_applications_one_pending on ambassador_applications (user_id) where status = 'pending';

create table ambassador_terms (
  id             bigint generated always as identity primary key,
  user_id        bigint not null references users(id) on delete cascade,
  university     text not null,
  role_in_team   text not null default 'member' check (role_in_team in ('lead', 'member')),
  term_start     date not null,
  term_end       date not null check (term_end >= term_start),
  hours_per_week smallint not null check (hours_per_week between 1 and 10),  -- promised upper bound (2, 3 or 5)
  ref_code       text not null check (ref_code ~ '^[a-z0-9]{4,30}$'),        -- link: ?ref=amb_<ref_code>
  status         text not null default 'active' check (status in ('active', 'ended')),
  paused_until   date,                          -- "rest week": no tasks / nudges until this date (inclusive)
  renew_answer   boolean,                       -- answer to "continue next semester?" (null = not answered)
  renew_asked_at timestamptz,
  ended_at       timestamptz,
  cert_code      text unique,                   -- set when admin issues the certificate (#/certificate/<cert_code>)
  cert_issued_at timestamptz,
  cert_issued_by bigint references users(id) on delete set null,
  application_id bigint references ambassador_applications(id) on delete set null,
  created_at     timestamptz not null default now()
);
-- one active term per person, one active lead per university; a code belongs to one person
-- (it is reused when the same person comes back for another term)
create unique index ambassador_terms_one_active on ambassador_terms (user_id) where status = 'active';
create unique index ambassador_terms_one_lead on ambassador_terms (university) where status = 'active' and role_in_team = 'lead';
create unique index ambassador_terms_code_active on ambassador_terms (ref_code) where status = 'active';
create index ambassador_terms_code on ambassador_terms (ref_code);
create index ambassador_terms_university on ambassador_terms (university, status);
create index users_referred_by on users (referred_by) where referred_by is not null;

-- Approved opportunity / place submissions as they are published. Survives the author's
-- account deletion (author_id -> null) and the end of an ambassador term.
create table community_content (
  id            bigint generated always as identity primary key,
  kind          text not null check (kind in ('opportunity', 'place')),
  payload       jsonb not null,
  university    text,
  source        text not null default 'ambassador',  -- ambassador | student
  author_id     bigint references users(id) on delete set null,
  submission_id bigint unique references submissions(id) on delete set null,
  deadline      date,
  hidden        boolean not null default false,       -- hidden by the shared report rule
  published_at  timestamptz not null default now()
);
create index community_content_visible on community_content (kind, published_at) where not hidden;
create index community_content_author on community_content (author_id) where author_id is not null;

alter table ambassador_applications enable row level security;
alter table ambassador_terms enable row level security;
alter table community_content enable row level security;

grant select on ambassador_applications, ambassador_terms to app_user;
grant insert (user_id, university, motivation, hours_per_week) on ambassador_applications to app_user;
create policy ambassador_applications_own_read on ambassador_applications for select to app_user using (user_id = app_user_id());
create policy ambassador_applications_own_insert on ambassador_applications for insert to app_user
  with check (user_id = app_user_id() and status = 'pending');
create policy ambassador_terms_own_read on ambassador_terms for select to app_user using (user_id = app_user_id());
-- community_content: read by the server for /api/content only (no app_user access).
