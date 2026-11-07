-- Admin workspace (docs/research/admin-from-kurs.md): internal notes on an account, saved filters, platform
-- support contacts. All three are read and written only by the server as the owner (no app_user privileges);
-- every write is audited (note_add / note_delete, view_save / view_delete, settings_update).

-- admin_user_notes: what the team should know about an account («asked to delete by phone», «returned 900 ₸ in
-- cash»). Never shown to the student. Deleting the account deletes the notes about it; an author's deletion keeps
-- their notes with no author.
create table admin_user_notes (
  id         bigint generated always as identity primary key,
  user_id    bigint not null references users(id) on delete cascade,
  author_id  bigint references users(id) on delete set null,
  body       text not null check (char_length(body) between 1 and 2000),
  created_at timestamptz not null default now()
);
create index admin_user_notes_user on admin_user_notes (user_id, id desc);
create index admin_user_notes_author on admin_user_notes (author_id) where author_id is not null;

-- admin_saved_views: a person's named filter of a list screen, stored as the screen's query string.
create table admin_saved_views (
  id         bigint generated always as identity primary key,
  owner_id   bigint not null references users(id) on delete cascade,
  screen     text not null check (screen in ('users', 'payments')),
  name       text not null check (char_length(name) between 1 and 60),
  query      text not null check (char_length(query) <= 1000),
  created_at timestamptz not null default now(),
  unique (owner_id, screen, name)
);

-- admin_settings: one row per settings group. 'support' = the contacts and hours students see in the app.
create table admin_settings (
  key        text primary key check (key in ('support')),
  value      jsonb not null check (jsonb_typeof(value) = 'object'),
  updated_by bigint references users(id) on delete set null,
  updated_at timestamptz not null default now()
);
create index admin_settings_updated_by on admin_settings (updated_by) where updated_by is not null;

alter table admin_user_notes enable row level security;
alter table admin_saved_views enable row level security;
alter table admin_settings enable row level security;
