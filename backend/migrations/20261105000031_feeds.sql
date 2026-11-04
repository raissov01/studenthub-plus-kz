-- Opportunity feed (docs/feeds.md): posts collected from Telegram channels and websites, structured
-- by AI, reviewed in the moderation queue (kind feed_item) and served by /api/content as
-- opportunities "feed-<id>" with source = "feed". Behind FEATURE_FEEDS.
--
-- feed_sources mirrors backend/feeds/sources.yaml (synced on every collector run); `enabled` is
-- taken from the file only when a source is first seen, afterwards the admin switch wins.

create table feed_sources (
  id               text primary key,                     -- registry id, e.g. tg-astanahub
  kind             text not null check (kind in ('tg_public', 'tg_mtproto', 'rss', 'html')),
  handle           text not null default '',             -- Telegram username without @
  url              text not null default '',             -- RSS / HTML list URL
  name             text not null,
  lang             text not null default 'ru',
  category         text not null default 'other',        -- default category hint for the AI
  scope            text not null default 'KZ' check (scope in ('KZ', 'international')),
  trust            text not null default 'medium' check (trust in ('high', 'medium', 'low')),
  enabled          boolean not null default false,
  last_run_at      timestamptz,
  last_ok_at       timestamptz,
  last_error       text,
  last_new         int not null default 0,                -- new posts in the last run
  created_at       timestamptz not null default now(),
  updated_at       timestamptz not null default now()
);

-- Raw posts: one row per (source, external id); text is kept for dedupe and re-runs.
create table feed_posts (
  id           bigint generated always as identity primary key,
  source_id    text not null references feed_sources(id) on delete cascade,
  external_id  text not null,                            -- Telegram message id / RSS guid / link
  url          text not null,                            -- permalink of the original post
  text         text not null,
  links        jsonb not null default '[]' check (jsonb_typeof(links) = 'array'),
  posted_at    timestamptz,
  fetched_at   timestamptz not null default now(),
  simhash      bigint not null default 0,
  status       text not null default 'new' check (status in ('new', 'skipped', 'irrelevant', 'duplicate', 'processed', 'failed')),
  note         text,                                     -- skip / failure reason
  unique (source_id, external_id)
);
create index feed_posts_new on feed_posts (fetched_at) where status = 'new';

-- Structured items. Exactly one live item per dedupe group; duplicates found later are linked in
-- extra_sources instead of becoming items.
create table feed_items (
  id             bigint generated always as identity primary key,
  post_id        bigint not null unique references feed_posts(id) on delete cascade,
  source_id      text not null references feed_sources(id) on delete cascade,
  status         text not null default 'pending' check (status in ('pending', 'published', 'rejected', 'expired')),
  category       text not null,
  data           jsonb not null check (jsonb_typeof(data) = 'object'),  -- validated AI output (feeds.Result)
  title_norm     text not null,
  canonical_url  text not null default '',
  deadline       date,
  event_date     date,
  confidence     real not null default 0,
  simhash        bigint not null default 0,
  extra_sources  jsonb not null default '[]' check (jsonb_typeof(extra_sources) = 'array'),
  submission_id  bigint references submissions(id) on delete set null,
  auto           boolean not null default false,          -- published without review
  ai_model       text not null default '',
  ai_tokens      int not null default 0,
  channel_posted_at timestamptz,                          -- FEEDS_PUBLISH_CHANNEL outbox
  created_at     timestamptz not null default now(),
  published_at   timestamptz,
  decided_by     bigint references users(id) on delete set null
);
create index feed_items_live on feed_items (published_at desc) where status = 'published';
create index feed_items_status on feed_items (status, created_at desc);
create index feed_items_url on feed_items (canonical_url) where canonical_url <> '';
create index feed_items_title on feed_items (title_norm);

-- AI spend per Asia/Almaty day (FEEDS_AI_DAILY_LIMIT is checked against tokens).
create table feed_ai_usage (
  day               date primary key,
  calls             int not null default 0,
  prompt_tokens     int not null default 0,
  completion_tokens int not null default 0,
  failures          int not null default 0
);

-- server only: no app_user grants or policies, so a student session cannot read raw posts,
-- moderation notes or AI usage
alter table feed_sources enable row level security;
alter table feed_posts enable row level security;
alter table feed_items enable row level security;
alter table feed_ai_usage enable row level security;
