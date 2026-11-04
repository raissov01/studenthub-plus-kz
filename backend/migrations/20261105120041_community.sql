-- Community features (internal/community, docs/api/community.md), each behind its own switch:
--   FEATURE_LOSTFOUND  «Табылған заттар»: lost / found posts, private claims, return confirmed by both
--   FEATURE_COURSEQA   «Пән бойынша сұрақ-жауап»: questions per subject, answers, upvotes, accepted answer
--   FEATURE_ROOMMATES  «Пәтерлес»: roommate listings, mutual-consent contact reveal (rules from Bagdar)
--
-- Privacy model: no table here stores a phone, @username, e-mail or link. Contacts live only in
-- users (app_user reads its own row only) and the server reveals them to the two parties of a
-- confirmed claim / accepted request, nothing else. Every write runs on the server as the
-- connection owner after explicit checks; app_user gets SELECT under RLS for exports and the
-- «mine» screens, so a bug in a handler still cannot read another student's private rows.
-- Every users(id) reference cascades: deleting the account removes everything.

-- ---------- lost & found ----------

create table lostfound_posts (
  id            bigint generated always as identity primary key,
  user_id       bigint not null references users(id) on delete cascade,
  kind          text not null check (kind in ('lost', 'found')),
  -- documents / cards are «sensitive»: never a photo, and a found one is handed to the dean's
  -- office or security (handed_to) instead of being kept by the finder
  category      text not null check (category in ('documents', 'cards', 'keys', 'phone', 'electronics', 'bag', 'clothes', 'books', 'other')),
  title         text not null check (char_length(title) between 3 and 80),
  descr         text not null default '' check (char_length(descr) <= 500),
  building      text not null default '' check (building ~ '^[a-z0-9_-]{0,20}$'),
  room          text not null default '' check (char_length(room) <= 20),
  happened_on   date not null,
  photo         text check (photo ~ '^[0-9a-f]{32}\.jpg$'),
  handed_to     text not null default '' check (handed_to in ('', 'dean', 'security')),
  university    text not null,
  status        text not null default 'active' check (status in ('active', 'closed', 'resolved', 'hidden', 'archived')),
  hidden_reason text check (hidden_reason in ('reports', 'admin')),
  created_at    timestamptz not null default now(),
  updated_at    timestamptz not null default now(),
  expires_at    timestamptz not null default now() + interval '30 days',
  check (category not in ('documents', 'cards') or photo is null),
  check (kind = 'lost' or category not in ('documents', 'cards') or handed_to <> '')
);
create index lostfound_posts_browse on lostfound_posts (university, id desc) where status = 'active';
create index lostfound_posts_user on lostfound_posts (user_id, created_at desc);
create index lostfound_posts_expiry on lostfound_posts (expires_at) where status = 'active';

create table lostfound_photos (
  name       text primary key check (name ~ '^[0-9a-f]{32}\.jpg$'),
  user_id    bigint not null references users(id) on delete cascade,
  post_id    bigint references lostfound_posts(id) on delete cascade,
  bytes      int not null,
  created_at timestamptz not null default now()
);
create index lostfound_photos_user on lostfound_photos (user_id);

-- A claim is the private message to the poster: on a «found» post the owner names a detail only
-- the owner knows; on a «lost» post the finder says what was found. Only the poster and the
-- claimant ever read it. confirmed = both see each other's contact; returned needs both marks.
create table lostfound_claims (
  id                   bigint generated always as identity primary key,
  post_id              bigint not null references lostfound_posts(id) on delete cascade,
  claimant_id          bigint not null references users(id) on delete cascade,
  message              text not null check (char_length(message) between 10 and 300),
  status               text not null default 'pending' check (status in ('pending', 'confirmed', 'declined', 'returned', 'withdrawn')),
  poster_returned_at   timestamptz,
  claimant_returned_at timestamptz,
  created_at           timestamptz not null default now(),
  decided_at           timestamptz,
  unique (post_id, claimant_id)
);
create index lostfound_claims_claimant on lostfound_claims (claimant_id, created_at desc);

-- ---------- course Q&A ----------
-- Subjects are device-side (frontend lib/subjects.ts), so a board is keyed by
-- (university, faculty_key, subject_key): subject_key is the normalised subject name (lower case,
-- ё→е, letters and digits only, single spaces), faculty_key the normalised users.faculty for a
-- faculty-only question or '' for the whole university.

create table qa_questions (
  id               bigint generated always as identity primary key,
  user_id          bigint not null references users(id) on delete cascade,
  university       text not null,
  faculty_key      text not null default '' check (char_length(faculty_key) <= 80),
  subject_key      text not null check (char_length(subject_key) between 2 and 80),
  subject_name     text not null check (char_length(subject_name) between 2 and 80),
  title            text not null check (char_length(title) between 8 and 150),
  body             text not null default '' check (char_length(body) <= 2000),
  tags             text[] not null default '{}' check (cardinality(tags) <= 5),
  anonymous        boolean not null default false,  -- hidden from classmates, not from moderators
  status           text not null default 'active' check (status in ('active', 'hidden', 'deleted')),
  hidden_reason    text check (hidden_reason in ('reports', 'admin')),
  locked           boolean not null default false,  -- e.g. during an exam: no new answers
  locked_reason    text check (char_length(locked_reason) <= 300),
  accepted_answer  bigint,
  answers          int not null default 0,
  score            int not null default 0,
  created_at       timestamptz not null default now(),
  updated_at       timestamptz not null default now()
);
create index qa_questions_board on qa_questions (university, subject_key, id desc) where status = 'active';
create index qa_questions_recent on qa_questions (university, id desc) where status = 'active';
create index qa_questions_user on qa_questions (user_id, created_at desc);
create index qa_questions_tags on qa_questions using gin (tags);

create table qa_answers (
  id            bigint generated always as identity primary key,
  question_id   bigint not null references qa_questions(id) on delete cascade,
  user_id       bigint not null references users(id) on delete cascade,
  body          text not null check (char_length(body) between 2 and 2000),
  anonymous     boolean not null default false,
  status        text not null default 'active' check (status in ('active', 'hidden', 'deleted')),
  hidden_reason text check (hidden_reason in ('reports', 'admin')),
  score         int not null default 0,
  created_at    timestamptz not null default now()
);
create index qa_answers_question on qa_answers (question_id, id);
create index qa_answers_user on qa_answers (user_id, created_at desc);
alter table qa_questions add constraint qa_questions_accepted_fk
  foreign key (accepted_answer) references qa_answers(id) on delete set null;

-- upvotes only (no downvotes: a new student's honest question must not sink)
create table qa_votes (
  user_id     bigint not null references users(id) on delete cascade,
  target_kind text not null check (target_kind in ('question', 'answer')),
  target_id   bigint not null,
  created_at  timestamptz not null default now(),
  primary key (user_id, target_kind, target_id)
);
create index qa_votes_target on qa_votes (target_kind, target_id);

-- ---------- roommates («Пәтерлес») ----------

-- Gender exists ONLY for the strict «only my gender» rule; it is not part of the profile and is
-- readable by its owner alone (and by the server).
create table roommate_genders (
  user_id    bigint primary key references users(id) on delete cascade,
  gender     text not null check (gender in ('f', 'm')),
  updated_at timestamptz not null default now()
);

create table roommate_listings (
  id            bigint generated always as identity primary key,
  user_id       bigint not null references users(id) on delete cascade,
  kind          text not null check (kind in ('have_room', 'looking')),
  housing       text not null check (housing in ('dorm', 'flat')),
  district      text not null check (char_length(district) between 2 and 60),
  budget_min    int not null check (budget_min between 0 and 1000000),
  budget_max    int not null check (budget_max between 5000 and 1000000 and budget_max >= budget_min),
  move_in       date not null,
  habits        text[] not null default '{}' check (cardinality(habits) <= 8),
  about         text not null default '' check (char_length(about) <= 400),
  same_gender   boolean not null default false,
  university    text not null,
  status        text not null default 'active' check (status in ('active', 'closed', 'hidden')),
  hidden_reason text check (hidden_reason in ('reports', 'admin')),
  created_at    timestamptz not null default now(),
  expires_at    timestamptz not null default now() + interval '30 days'
);
-- one person, one active listing
create unique index roommate_listings_one_active on roommate_listings (user_id) where status = 'active';
create index roommate_listings_browse on roommate_listings (university, id desc) where status = 'active';

create table roommate_requests (
  id         bigint generated always as identity primary key,
  listing_id bigint not null references roommate_listings(id) on delete cascade,
  from_user  bigint not null references users(id) on delete cascade,
  status     text not null default 'pending' check (status in ('pending', 'accepted', 'declined')),
  created_at timestamptz not null default now(),
  decided_at timestamptz,
  unique (listing_id, from_user)
);
create index roommate_requests_from on roommate_requests (from_user, created_at desc);

-- ---------- row level security ----------

alter table lostfound_posts enable row level security;
alter table lostfound_photos enable row level security;
alter table lostfound_claims enable row level security;
alter table qa_questions enable row level security;
alter table qa_answers enable row level security;
alter table qa_votes enable row level security;
alter table roommate_genders enable row level security;
alter table roommate_listings enable row level security;
alter table roommate_requests enable row level security;

grant select on lostfound_posts, lostfound_photos, lostfound_claims, qa_questions, qa_answers, qa_votes,
  roommate_genders, roommate_listings, roommate_requests to app_user;

-- posts: active ones are public anyway; anything else only to its author
create policy lostfound_posts_read on lostfound_posts for select to app_user
  using (status = 'active' or user_id = app_user_id());
create policy lostfound_photos_self on lostfound_photos for select to app_user using (user_id = app_user_id());
-- a claim (with its secret detail) is readable by the claimant and the post's author only
create policy lostfound_claims_parties on lostfound_claims for select to app_user
  using (claimant_id = app_user_id()
    or exists (select 1 from lostfound_posts p where p.id = post_id and p.user_id = app_user_id()));

create policy qa_questions_read on qa_questions for select to app_user
  using (status = 'active' or user_id = app_user_id());
create policy qa_answers_read on qa_answers for select to app_user
  using (status = 'active' or user_id = app_user_id());
create policy qa_votes_self on qa_votes for select to app_user using (user_id = app_user_id());

create policy roommate_genders_self on roommate_genders for select to app_user using (user_id = app_user_id());
-- listings are matched by the server (gender rule); app_user sees only its own
create policy roommate_listings_self on roommate_listings for select to app_user using (user_id = app_user_id());
create policy roommate_requests_parties on roommate_requests for select to app_user
  using (from_user = app_user_id()
    or exists (select 1 from roommate_listings l where l.id = listing_id and l.user_id = app_user_id()));
