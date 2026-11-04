-- Quiz (Quizizz-style): students create quizzes (manual, Excel import, AI from text or lecture
-- files), practise them with server-side scoring, share them (public / unlisted / private) and
-- host Kahoot-style live games. FEATURE_QUIZ. See docs/api/quiz.md.
--
-- Answer keys live in their own table (quiz_answer_keys) that only the quiz owner can read
-- through RLS, so the public read policy on questions never exposes correct answers.
-- Media files are on the API server's disk (UPLOAD_DIR/quiz/<random>.jpg|.mp3); quiz_media is
-- the registry that ties each file to its uploader (cleanup, export, account deletion).

create table quizzes (
  id              bigint generated always as identity primary key,
  user_id         bigint not null references users(id) on delete cascade,
  title           text not null check (char_length(title) between 1 and 200),
  description     text not null default '' check (char_length(description) <= 1000),
  subject         text not null default '' check (char_length(subject) <= 80),
  -- public: listed and open to everyone; unlisted: only via the share link; private: owner only
  visibility      text not null default 'private' check (visibility in ('public', 'unlisted', 'private')),
  -- AI drafts stay drafts until the student reviews and saves them (never visible to others)
  is_draft        boolean not null default false,
  share_code      text not null unique check (share_code ~ '^[A-Za-z0-9]{10}$'),
  time_per_question int not null default 30 check (time_per_question between 5 and 300),
  shuffle         boolean not null default true,
  source          text not null default 'manual'
                  check (source in ('manual', 'excel', 'ai_text', 'ai_files', 'clone', 'cards')),
  cloned_from     bigint references quizzes(id) on delete set null,
  question_count  int not null default 0,
  plays           int not null default 0,
  version         int not null default 1,
  hidden_at       timestamptz,  -- an admin took a public quiz out of listings
  created_at      timestamptz not null default now(),
  updated_at      timestamptz not null default now()
);
create index quizzes_user on quizzes (user_id, updated_at desc);
create index quizzes_public on quizzes (id desc) where visibility = 'public' and not is_draft and hidden_at is null;

create table quiz_questions (
  id           bigint generated always as identity primary key,
  quiz_id      bigint not null references quizzes(id) on delete cascade,
  position     int not null,
  type         text not null check (type in ('mcq', 'mcq_multi', 'true_false', 'fill_blank', 'reorder',
               'matching', 'categorization', 'dropdown', 'poll', 'hotspot', 'labeling', 'comprehension')),
  prompt       text not null check (char_length(prompt) between 1 and 1000),
  data         jsonb not null default '{}',   -- what a player may see (options, items, zones, passage)
  explanation  text not null default '' check (char_length(explanation) <= 1000),
  source_quote text not null default '' check (char_length(source_quote) <= 600),  -- AI: verbatim support from the notes
  image        text check (image ~ '^[0-9a-f]{32}\.jpg$'),
  audio        text check (audio ~ '^[0-9a-f]{32}\.mp3$'),
  time_limit   int check (time_limit between 5 and 300),
  created_at   timestamptz not null default now()
);
create index quiz_questions_quiz on quiz_questions (quiz_id, position);

-- correct answers, owner-only
create table quiz_answer_keys (
  question_id bigint primary key references quiz_questions(id) on delete cascade,
  quiz_id     bigint not null references quizzes(id) on delete cascade,
  answer      jsonb not null
);
create index quiz_answer_keys_quiz on quiz_answer_keys (quiz_id);

create table quiz_attempts (
  id            bigint generated always as identity primary key,
  quiz_id       bigint not null references quizzes(id) on delete cascade,
  user_id       bigint not null references users(id) on delete cascade,
  mode          text not null default 'practice' check (mode in ('practice', 'retry')),
  retry_of      bigint references quiz_attempts(id) on delete set null,
  score         numeric(8, 2) not null,
  max_score     int not null,
  percentage    int not null check (percentage between 0 and 100),
  correct_count int not null,
  total         int not null,
  time_spent    int not null default 0,
  created_at    timestamptz not null default now()
);
create index quiz_attempts_user on quiz_attempts (user_id, created_at desc);
create index quiz_attempts_quiz on quiz_attempts (quiz_id);

-- graded answers with a snapshot of the question, so results survive later edits
create table quiz_attempt_answers (
  attempt_id   bigint not null references quiz_attempts(id) on delete cascade,
  position     int not null,
  question_id  bigint references quiz_questions(id) on delete set null,
  type         text not null,
  prompt       text not null,
  data         jsonb not null default '{}',
  explanation  text not null default '',
  source_quote text not null default '',
  image        text,
  given        jsonb,              -- null = not answered
  correct_key  jsonb,              -- null for polls
  credit       real not null default 0 check (credit between 0 and 1),
  is_correct   boolean not null default false,
  graded       boolean not null default true,
  time_spent   int not null default 0,
  primary key (attempt_id, position)
);
create index quiz_attempt_answers_question on quiz_attempt_answers (question_id);

create table quiz_media (
  name       text primary key check (name ~ '^[0-9a-f]{32}\.(jpg|mp3)$'),
  user_id    bigint not null references users(id) on delete cascade,
  quiz_id    bigint references quizzes(id) on delete set null,
  kind       text not null check (kind in ('image', 'audio')),
  bytes      int not null,
  created_at timestamptz not null default now()
);
create index quiz_media_user on quiz_media (user_id);
create index quiz_media_orphans on quiz_media (created_at) where quiz_id is null;

-- AI generation budget per Almaty day; a vision page costs more than a text request
create table quiz_ai_usage (
  user_id bigint not null references users(id) on delete cascade,
  day     date not null,
  units   int not null default 0,
  primary key (user_id, day)
);

create table quiz_live_sessions (
  id             bigint generated always as identity primary key,
  code           text not null check (code ~ '^[0-9]{6}$'),
  quiz_id        bigint not null references quizzes(id) on delete cascade,
  host_id        bigint not null references users(id) on delete cascade,
  status         text not null default 'lobby' check (status in ('lobby', 'active', 'finished', 'abandoned')),
  players        int not null default 0,
  question_stats jsonb not null default '[]',  -- [{position, answered, correct}]
  created_at     timestamptz not null default now(),
  started_at     timestamptz,
  finished_at    timestamptz
);
create unique index quiz_live_sessions_code_open on quiz_live_sessions (code) where status in ('lobby', 'active');
create index quiz_live_sessions_host on quiz_live_sessions (host_id, created_at desc);

create table quiz_live_results (
  id         bigint generated always as identity primary key,
  session_id bigint not null references quiz_live_sessions(id) on delete cascade,
  rank       int not null,
  nickname   text not null check (char_length(nickname) between 1 and 24),
  user_id    bigint references users(id) on delete cascade,  -- null for anonymous players
  score      int not null,
  correct    int not null
);
create index quiz_live_results_session on quiz_live_results (session_id, rank);
create index quiz_live_results_user on quiz_live_results (user_id) where user_id is not null;

create function quizzes_touch() returns trigger language plpgsql as $$
begin
  new.updated_at := now();
  return new;
end $$;
create trigger quizzes_touch before update on quizzes for each row execute function quizzes_touch();

alter table quizzes enable row level security;
alter table quiz_questions enable row level security;
alter table quiz_answer_keys enable row level security;
alter table quiz_attempts enable row level security;
alter table quiz_attempt_answers enable row level security;
alter table quiz_media enable row level security;
alter table quiz_ai_usage enable row level security;
alter table quiz_live_sessions enable row level security;
alter table quiz_live_results enable row level security;

grant select, insert, delete on quizzes to app_user;
grant update (title, description, subject, visibility, is_draft, time_per_question, shuffle,
  question_count, version) on quizzes to app_user;
grant select, insert, update, delete on quiz_questions, quiz_answer_keys to app_user;
grant select on quiz_attempts, quiz_attempt_answers, quiz_media, quiz_ai_usage,
  quiz_live_sessions, quiz_live_results to app_user;

-- quizzes: everyone signed in reads published public quizzes; only the owner writes
create policy quizzes_read on quizzes for select to app_user
  using (user_id = app_user_id() or (visibility = 'public' and not is_draft and hidden_at is null));
create policy quizzes_owner_insert on quizzes for insert to app_user with check (user_id = app_user_id());
create policy quizzes_owner_update on quizzes for update to app_user
  using (user_id = app_user_id()) with check (user_id = app_user_id());
create policy quizzes_owner_delete on quizzes for delete to app_user using (user_id = app_user_id());

-- questions follow their quiz (the subquery itself runs under the quizzes policy)
create policy quiz_questions_read on quiz_questions for select to app_user
  using (exists (select 1 from quizzes q where q.id = quiz_id));
create policy quiz_questions_owner_write on quiz_questions for all to app_user
  using (exists (select 1 from quizzes q where q.id = quiz_id and q.user_id = app_user_id()))
  with check (exists (select 1 from quizzes q where q.id = quiz_id and q.user_id = app_user_id()));

-- answer keys: owner only, also for reads
create policy quiz_answer_keys_owner on quiz_answer_keys for all to app_user
  using (exists (select 1 from quizzes q where q.id = quiz_id and q.user_id = app_user_id()))
  with check (exists (select 1 from quizzes q where q.id = quiz_id and q.user_id = app_user_id()));

-- attempts are written by the server after grading; the student reads their own
create policy quiz_attempts_self_read on quiz_attempts for select to app_user using (user_id = app_user_id());
create policy quiz_attempt_answers_self_read on quiz_attempt_answers for select to app_user
  using (exists (select 1 from quiz_attempts a where a.id = attempt_id and a.user_id = app_user_id()));
create policy quiz_media_self_read on quiz_media for select to app_user using (user_id = app_user_id());
-- attaching an upload to one's own quiz, and dropping it again
grant update (quiz_id), delete on quiz_media to app_user;
create policy quiz_media_owner_update on quiz_media for update to app_user
  using (user_id = app_user_id())
  with check (user_id = app_user_id() and (quiz_id is null or exists (select 1 from quizzes q where q.id = quiz_id and q.user_id = app_user_id())));
create policy quiz_media_owner_delete on quiz_media for delete to app_user using (user_id = app_user_id());
create policy quiz_ai_usage_self_read on quiz_ai_usage for select to app_user using (user_id = app_user_id());
create policy quiz_live_sessions_host_read on quiz_live_sessions for select to app_user using (host_id = app_user_id());
create policy quiz_live_results_read on quiz_live_results for select to app_user
  using (user_id = app_user_id()
         or exists (select 1 from quiz_live_sessions s where s.id = session_id and s.host_id = app_user_id()));
