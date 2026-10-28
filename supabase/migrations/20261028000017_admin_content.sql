-- Admin content editor (docs/api/admin-content.md): opportunities, jobs and discounts managed in
-- our own /admin panel instead of Google Sheets. `record` holds one entry in exactly the
-- data/*.json shape, validated by the same rules as a sheet row (pkg/content.ValidateRecord);
-- /api/content serves it as "adm-<id>" with source = "admin".
--
-- Not community_content: that table is the published copy of approved queue submissions
-- (opportunity / place, flat suggestion payloads, tied to submissions and the report rule).
-- Editorial records have the full record shape, three kinds and an edit / hide lifecycle.

create table admin_content (
  id          bigint generated always as identity primary key,
  kind        text not null check (kind in ('opportunities', 'jobs', 'discounts')),
  record      jsonb not null check (jsonb_typeof(record) = 'object'),
  status      text not null default 'published' check (status in ('published', 'hidden')),
  deadline    date,                                          -- copy of record.deadline (null = мерзімсіз)
  created_by  bigint references users(id) on delete set null,
  updated_by  bigint references users(id) on delete set null,
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now()
);
create index admin_content_live on admin_content (kind, created_at desc) where status = 'published';
create index admin_content_kind on admin_content (kind, updated_at desc);

-- server only (no app_user access); every write is recorded in audit_log by the server
alter table admin_content enable row level security;
