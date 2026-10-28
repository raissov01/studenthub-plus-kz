-- Product analytics without personal data: a random per-device id, an event name from a fixed
-- list and a few categorical properties. No names, GPA, phones or chat text ever land here.
create table analytics_events (
  id       bigint generated always as identity primary key,
  anon_id  uuid not null,
  name     text not null,
  props    jsonb not null default '{}',
  day      date not null default (now() at time zone 'Asia/Almaty')::date,
  at       timestamptz not null default now()
);
create index analytics_events_anon_day on analytics_events (anon_id, day);
create index analytics_events_name_day on analytics_events (name, day);

-- first visit per device and where it came from (?ref=share, ?ref=amb_<code>)
create table analytics_devices (
  anon_id   uuid primary key,
  first_day date not null,
  ref       text,
  created_at timestamptz not null default now()
);
create index analytics_devices_first_day on analytics_devices (first_day);
create index analytics_devices_ref on analytics_devices (ref) where ref is not null;

alter table analytics_events enable row level security;
alter table analytics_devices enable row level security;
-- no policies for app_user: written and read only by the server
