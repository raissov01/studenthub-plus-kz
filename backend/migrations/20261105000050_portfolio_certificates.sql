-- Certificate files in the portfolio: the student attaches a PDF or a photo of a certificate
-- (course, olympiad, competition, volunteering, language test, other). The file lives on the API
-- server's disk under UPLOAD_DIR/certs/<random>.pdf|.jpg; this table is its registry.
--
-- Self-declared, like portfolio_items: there is no "verified" column. Every row is private until
-- the student turns its own is_public on; a public file is visible only while the public portfolio
-- page itself is enabled (checked by the server when it serves the file and builds /api/p/{slug}).
--
-- Quota: 30 files and 100 MB per student, enforced by the trigger below under a per-user advisory
-- lock, so two parallel uploads cannot both slip past the limit.

create table portfolio_certificates (
  id             bigint generated always as identity primary key,
  user_id        bigint not null references users(id) on delete cascade,
  title          text not null check (char_length(title) between 1 and 120),
  issuer         text not null default '' check (char_length(issuer) <= 120),
  issued_on      date,
  credential_id  text not null default '' check (char_length(credential_id) <= 100),
  credential_url text not null default '' check (credential_url = '' or (credential_url like 'https://%' and char_length(credential_url) <= 500)),
  category       text not null default 'other'
                   check (category in ('course', 'olympiad', 'competition', 'volunteering', 'language', 'other')),
  file_name      text not null unique check (file_name ~ '^[0-9a-f]{32}\.(pdf|jpg)$'),
  mime           text not null check (mime in ('application/pdf', 'image/jpeg')),
  bytes          integer not null check (bytes > 0 and bytes <= 10485760),
  width          integer check (width is null or width between 1 and 1600),
  height         integer check (height is null or height between 1 and 1600),
  is_public      boolean not null default false,
  created_at     timestamptz not null default now(),
  updated_at     timestamptz not null default now()
);
create index portfolio_certificates_user on portfolio_certificates (user_id, created_at desc);

create function portfolio_certificates_quota() returns trigger language plpgsql as $$
declare
  n int;
  total bigint;
begin
  perform pg_advisory_xact_lock(hashtextextended('portfolio_certificates:' || new.user_id, 0));
  select count(*), coalesce(sum(bytes), 0) into n, total from portfolio_certificates where user_id = new.user_id;
  if n >= 30 then
    raise exception 'cert_quota_files';
  end if;
  if total + new.bytes > 104857600 then
    raise exception 'cert_quota_bytes';
  end if;
  return new;
end $$;
create trigger portfolio_certificates_quota before insert on portfolio_certificates
  for each row execute function portfolio_certificates_quota();

alter table portfolio_certificates enable row level security;

-- Owner only. The file name, mime and size are fixed at upload; only the descriptive fields and
-- the public flag can change afterwards.
grant select, insert, delete on portfolio_certificates to app_user;
grant update (title, issuer, issued_on, credential_id, credential_url, category, is_public, updated_at)
  on portfolio_certificates to app_user;
create policy portfolio_certificates_own on portfolio_certificates for all to app_user
  using (user_id = app_user_id()) with check (user_id = app_user_id());
