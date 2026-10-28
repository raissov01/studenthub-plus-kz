-- Launch controls and monitoring are server-only: no app_user privileges or policies.
create table app_settings (
  singleton boolean primary key default true check (singleton),
  show_demo boolean not null default true
);
insert into app_settings (singleton) values (true);

create table client_errors (
  id bigint generated always as identity primary key,
  message text not null check (char_length(message) between 1 and 500),
  screen text not null check (char_length(screen) <= 200 and screen not like '%?%' and screen not like '%#%'),
  browser text not null check (browser ~ '^(Unknown|Chrome|Firefox|Safari|Edge|Opera)(/[0-9]{1,3})?$'),
  created_at timestamptz not null default now()
);
create index client_errors_created_at on client_errors (created_at);

create table cron_runs (
  endpoint text primary key check (endpoint in ('/api/cron', '/api/cron/events', '/api/cron/ambassador',
    '/api/cron/mentors', '/api/cron/market', '/api/cron/b2b')),
  last_success_at timestamptz not null
);

alter table app_settings enable row level security;
alter table client_errors enable row level security;
alter table cron_runs enable row level security;
revoke all on app_settings, client_errors, cron_runs from public, app_user;

-- Events did not previously carry a demo marker. Only server-side imports may set it.
alter table events add column is_demo boolean not null default false;
