-- «University voice» (docs/UNIVOICE.md): surveys (FEATURE_SURVEYS), anonymous feedback tickets
-- (FEATURE_FEEDBACK) and the wellbeing psychologist contacts (FEATURE_WELLBEING).
--
-- Every table has RLS on. All reads and writes go through the API as the connection owner, which
-- enforces staff binding, k-anonymity and anonymity; app_user may only read its own signed tickets
-- and identified answers (for the data export), see the policies at the end.
--
-- Anonymity invariants (tested in internal/surveys and internal/feedback):
--   * survey_responses has no user column and no timestamp; the only per-person trace of an
--     anonymous survey is survey_markers.marker = HMAC-SHA256(K_s, user_id) where
--     K_s = HMAC-SHA256(server key derived from SESSION_SECRET, survey_id || marker_salt).
--     It stops double voting; without the server secret it cannot be turned back into a user id,
--     and it has no column that joins it to a response.
--   * identified surveys keep the person ↔ response link in a separate table
--     (survey_response_owners), so an anonymous survey simply has no rows there.
--   * an anonymous feedback ticket stores no author: author_id is null (CHECK) and the student's
--     access is a random ticket token whose SHA-256 is stored; the token itself lives on the device.

create table surveys (
  id            bigint generated always as identity primary key,
  university    text not null check (char_length(university) between 1 and 160),
  title         text not null check (char_length(title) between 3 and 160),
  intro         text not null default '' check (char_length(intro) <= 1000),
  questions     jsonb not null check (jsonb_typeof(questions) = 'array'),
  -- {"faculties":[...],"courses":[...],"groups":[...]}; every empty list = everyone in the university
  audience      jsonb not null default '{}' check (jsonb_typeof(audience) = 'object'),
  anonymous     boolean not null default true,
  status        text not null default 'draft' check (status in ('draft', 'published', 'closed')),
  opens_at      timestamptz,
  closes_at     timestamptz,
  marker_salt   bytea not null check (octet_length(marker_salt) = 32),
  created_by    bigint references users(id) on delete set null,
  created_at    timestamptz not null default now(),
  updated_at    timestamptz not null default now(),
  published_at  timestamptz,
  check (closes_at is null or opens_at is null or closes_at > opens_at)
);
create index surveys_university on surveys (university, status);

-- one row per (survey, person who answered); no user id, only the keyed marker
create table survey_markers (
  survey_id bigint not null references surveys(id) on delete cascade,
  marker    bytea not null check (octet_length(marker) = 32),
  primary key (survey_id, marker)
);

-- answers: deliberately no user_id, no created_at (no timing join with the points ledger), random id
create table survey_responses (
  id        uuid primary key default gen_random_uuid(),
  survey_id bigint not null references surveys(id) on delete cascade,
  answers   jsonb not null check (jsonb_typeof(answers) = 'object'),
  -- coarse segment for k-anonymous breakdowns (groups under 5 are never shown)
  faculty   text check (char_length(faculty) <= 40),
  course    smallint check (course between 1 and 6)
);
create index survey_responses_survey on survey_responses (survey_id);

-- identified (non-anonymous) surveys only: who wrote which response
create table survey_response_owners (
  response_id uuid primary key references survey_responses(id) on delete cascade,
  user_id     bigint not null references users(id) on delete cascade
);
create index survey_response_owners_user on survey_response_owners (user_id);

-- ---------- feedback ----------

create table feedback_recipients (
  id         bigint generated always as identity primary key,
  university text not null check (char_length(university) between 1 and 160),
  key        text not null check (key ~ '^[a-z][a-z0-9_]{1,29}$'),
  name_kk    text not null check (char_length(name_kk) between 2 and 80),
  name_ru    text not null check (char_length(name_ru) between 2 and 80),
  sla_hours  integer not null default 72 check (sla_hours between 1 and 720),
  active     boolean not null default true,
  created_at timestamptz not null default now(),
  unique (university, key)
);

create table feedback_recipient_staff (
  recipient_id bigint not null references feedback_recipients(id) on delete cascade,
  user_id      bigint not null references users(id) on delete cascade,
  primary key (recipient_id, user_id)
);
create index feedback_recipient_staff_user on feedback_recipient_staff (user_id);

create table feedback_tickets (
  id              bigint generated always as identity primary key,
  recipient_id    bigint not null references feedback_recipients(id),
  category        text not null check (category in ('suggestion', 'complaint', 'question', 'gratitude')),
  body            text not null check (char_length(body) between 10 and 3000),
  photo           text check (photo ~ '^[0-9a-f]{32}\.jpg$'),
  anonymous       boolean not null,
  -- signed tickets only; an anonymous ticket can never carry an author
  author_id       bigint references users(id) on delete cascade,
  -- anonymous tickets only: SHA-256 of the 32-byte random ticket token kept on the student's device
  token_hash      bytea unique check (token_hash is null or octet_length(token_hash) = 32),
  -- held = flagged (profanity/threat), waiting for an admin in the moderation queue
  status          text not null default 'new' check (status in ('held', 'new', 'in_progress', 'answered', 'closed')),
  flag            text check (flag in ('profanity', 'threat')),
  student_unread  boolean not null default false,
  sla_due_at      timestamptz not null,
  sla_reminded_at timestamptz,
  -- anonymous tickets: truncated to the hour, so times do not single out a sender
  created_at      timestamptz not null,
  updated_at      timestamptz not null,
  check (not anonymous or author_id is null),
  check (anonymous = (token_hash is not null))
);
create index feedback_tickets_inbox on feedback_tickets (recipient_id, status, created_at desc);
create index feedback_tickets_author on feedback_tickets (author_id) where author_id is not null;
create index feedback_tickets_sla on feedback_tickets (sla_due_at) where status in ('new', 'in_progress');

create table feedback_messages (
  id         bigint generated always as identity primary key,
  ticket_id  bigint not null references feedback_tickets(id) on delete cascade,
  from_staff boolean not null,
  staff_id   bigint references users(id) on delete set null,   -- audit only, never shown to the student
  body       text not null check (char_length(body) between 1 and 3000),
  created_at timestamptz not null
);
create index feedback_messages_ticket on feedback_messages (ticket_id, id);

-- ---------- wellbeing ----------

-- psychologist / support-service contacts per university, entered by admin or the university's
-- staff. Empty until filled: the app never shows made-up contacts.
create table wellbeing_contacts (
  id         bigint generated always as identity primary key,
  university text not null check (char_length(university) between 1 and 160),
  name       text not null check (char_length(name) between 2 and 120),
  role       text not null default '' check (char_length(role) <= 120),
  phone      text check (phone ~ '^\+?[0-9 ()-]{3,24}$'),
  email      text check (char_length(email) <= 120 and email like '%_@_%'),
  place      text not null default '' check (char_length(place) <= 160),
  hours      text not null default '' check (char_length(hours) <= 160),
  sort       smallint not null default 0,
  updated_by bigint references users(id) on delete set null,
  updated_at timestamptz not null default now()
);
create index wellbeing_contacts_university on wellbeing_contacts (university, sort);

alter table surveys enable row level security;
alter table survey_markers enable row level security;
alter table survey_responses enable row level security;
alter table survey_response_owners enable row level security;
alter table feedback_recipients enable row level security;
alter table feedback_recipient_staff enable row level security;
alter table feedback_tickets enable row level security;
alter table feedback_messages enable row level security;
alter table wellbeing_contacts enable row level security;
-- app_user (the student's own session, used by GET /api/me/export) may read only its OWN signed
-- tickets, their messages and its OWN identified-survey answers. Anonymous tickets have no author and
-- anonymous responses have no owner row, so no policy can ever match them. Markers, surveys,
-- staff lists and contacts: no app_user access at all.
grant select on survey_response_owners, survey_responses, feedback_tickets, feedback_messages, feedback_recipients to app_user;
create policy survey_response_owners_self on survey_response_owners for select to app_user using (user_id = app_user_id());
create policy survey_responses_owned on survey_responses for select to app_user
  using (exists (select 1 from survey_response_owners o where o.response_id = survey_responses.id and o.user_id = app_user_id()));
create policy feedback_tickets_author on feedback_tickets for select to app_user using (author_id = app_user_id());
create policy feedback_messages_author on feedback_messages for select to app_user
  using (exists (select 1 from feedback_tickets t where t.id = feedback_messages.ticket_id and t.author_id = app_user_id()));
create policy feedback_recipients_read on feedback_recipients for select to app_user using (true);

-- the SLA reminder cron (/api/cron/feedback) records its runs too; any /api/cron/<name> is allowed
-- from now on so new jobs do not need to rewrite this list
alter table cron_runs drop constraint if exists cron_runs_endpoint_check;
alter table cron_runs add constraint cron_runs_endpoint_check check (endpoint ~ '^/api/cron(/[a-z0-9_-]{1,40})?$');
