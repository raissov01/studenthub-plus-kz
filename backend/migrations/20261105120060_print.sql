-- Campus print / copy shop («Полиграфия», internal/printshop, docs/print.md), FEATURE_PRINT.
--
-- A student uploads files, configures each one (copies, colour, duplex, page range, paper size, binding,
-- lamination), adds goods, sees the price the server computed, joins the shop's queue, pays with Kaspi straight
-- to the shop's own Kaspi Pay account (the café's sidecar and session flow, internal/shopkit) or at the counter,
-- and is told when the job is ready. The operator works the queue on a tablet board (/print/staff).
--
-- Money: every price and total is an integer in tiyn and a whole number of tenge. Student Hub never holds money.
-- Prices are recomputed by the server from print_services / print_goods; a client total is never trusted.
-- Office files have no server-side page count: the student declares it, the job waits for the operator to
-- confirm or adjust the price (price_state), and an adjusted price waits for the student's approval.
--
-- Files are private: random names under UPLOAD_DIR/print, served only through short-lived signed URLs to the
-- student who uploaded them and the shop's staff, and deleted retention_days after the job is closed.
--
-- Every write goes through the server as the connection owner with explicit checks in Go; app_user only reads:
-- a student their own uploads and jobs, the shop's staff the jobs of their own shop.

create table print_shops (
  id                 bigint generated always as identity primary key,
  name_kk            text not null check (char_length(name_kk) between 2 and 80),
  name_ru            text not null check (char_length(name_ru) between 2 and 80),
  building           text not null default '' check (char_length(building) <= 120),
  place_kk           text not null default '' check (char_length(place_kk) <= 200),
  place_ru           text not null default '' check (char_length(place_ru) <= 200),
  open_time          time not null default '08:30',                                -- Asia/Almaty wall clock
  close_time         time not null default '18:00',
  open_days          smallint[] not null default '{1,2,3,4,5}'
                       check (open_days <@ '{1,2,3,4,5,6,7}'::smallint[] and cardinality(open_days) between 1 and 7),
  slot_minutes       smallint not null default 15 check (slot_minutes between 5 and 60),
  lead_minutes       smallint not null default 20 check (lead_minutes between 0 and 240),
  min_order          integer not null default 0 check (min_order between 0 and 1000000 and min_order % 100 = 0),
  accepts            text[] not null default '{pdf,docx,xlsx,pptx,jpg,png}'
                       check (accepts <@ '{pdf,docx,xlsx,pptx,jpg,png}'::text[] and cardinality(accepts) between 1 and 6),
  retention_days     smallint not null default 3 check (retention_days between 1 and 30),
  active             boolean not null default false,
  payment_mode       text not null default 'pickup' check (payment_mode in ('pickup', 'kaspi', 'both')),
  kaspi_merchant_ref text not null default '' check (char_length(kaspi_merchant_ref) <= 120),
  kaspi_org_id       bigint check (kaspi_org_id >= 0),
  note_kk            text not null default '' check (char_length(note_kk) <= 300),  -- «Түрлі түсті тек A4», …
  note_ru            text not null default '' check (char_length(note_ru) <= 300),
  created_by         bigint references users(id) on delete set null,
  created_at         timestamptz not null default now(),
  updated_at         timestamptz not null default now(),
  check (close_time > open_time)
);

create table print_staff (
  shop_id    bigint not null references print_shops(id) on delete cascade,
  user_id    bigint not null references users(id) on delete cascade,
  role       text not null default 'operator' check (role in ('operator', 'manager')),
  added_by   bigint references users(id) on delete set null,
  created_at timestamptz not null default now(),
  primary key (shop_id, user_id)
);
create index print_staff_user on print_staff (user_id);

-- kind and pricing (tiyn, validated by internal/printshop ValidPricing):
--   print      {"sizes":{"A4":{"bw":2000,"color":10000,"bwDuplex":1500,"colorDuplex":9000},"A3":{...}}}  per printed side
--              (a colour price of 0 = no colour at that size; a duplex price of 0 = no duplex); posters are a print
--              service with sizes A2…A0
--   binding    {"tiers":[{"maxSheets":50,"price":50000},{"maxSheets":150,"price":80000}]}  per bound copy, by sheets
--   lamination {"sheet":{"A4":15000,"A3":25000}}                                      per sheet
--   unit       {"price":10000,"unitKk":"бет","unitRu":"стр."}                         per unit: copy, scan, cards, …
create table print_services (
  id         bigint generated always as identity primary key,
  shop_id    bigint not null references print_shops(id) on delete cascade,
  kind       text not null check (kind in ('print', 'binding', 'lamination', 'unit')),
  name_kk    text not null check (char_length(name_kk) between 1 and 80),
  name_ru    text not null check (char_length(name_ru) between 1 and 80),
  desc_kk    text not null default '' check (char_length(desc_kk) <= 300),
  desc_ru    text not null default '' check (char_length(desc_ru) <= 300),
  pricing    jsonb not null check (jsonb_typeof(pricing) = 'object'),
  online     boolean not null default true,   -- false: listed in the price list only («кассада»)
  active     boolean not null default true,
  sort       smallint not null default 0,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
create index print_services_shop on print_services (shop_id, sort, id);

create table print_goods (
  id         bigint generated always as identity primary key,
  shop_id    bigint not null references print_shops(id) on delete cascade,
  name_kk    text not null check (char_length(name_kk) between 1 and 80),
  name_ru    text not null check (char_length(name_ru) between 1 and 80),
  price      integer not null check (price between 100 and 10000000 and price % 100 = 0),
  in_stock   boolean not null default true,
  archived   boolean not null default false,
  sort       smallint not null default 0,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
create index print_goods_shop on print_goods (shop_id, sort, id) where not archived;

create table print_jobs (
  id                 bigint generated always as identity primary key,
  shop_id            bigint not null references print_shops(id),           -- a shop with jobs is deactivated, not deleted
  user_id            bigint references users(id) on delete set null,
  lines              jsonb not null check (jsonb_typeof(lines) = 'array' and jsonb_array_length(lines) between 1 and 40),
  total              integer not null check (total > 0 and total % 100 = 0),
  -- final: the server's own count; review: some page counts were declared by the student, the operator confirms or
  -- adjusts; proposed: the operator set proposed_total, the student approves it (or cancels) before paying/printing
  price_state        text not null default 'final' check (price_state in ('final', 'review', 'proposed')),
  proposed_total     integer check (proposed_total > 0 and proposed_total % 100 = 0),
  adjust_reason      text check (char_length(adjust_reason) <= 300),
  pickup_at          timestamptz,                                          -- null = as soon as possible
  pickup_code        text not null check (pickup_code ~ '^[0-9]{3}$'),
  note               text not null default '' check (char_length(note) <= 300),
  status             text not null default 'new'
                       check (status in ('new', 'printing', 'ready', 'picked_up', 'cancelled', 'no_show')),
  payment_method     text not null check (payment_method in ('pickup', 'kaspi')),
  payment_status     text not null default 'unpaid'
                       check (payment_status in ('unpaid', 'pending', 'paid', 'refunded', 'failed')),
  refund_manual      boolean not null default false,
  kaspi_operation_id text unique check (kaspi_operation_id ~ '^[0-9A-Za-z_-]{1,64}$'),
  kaspi_pay_url      text check (char_length(kaspi_pay_url) <= 500),
  kaspi_qr           text check (char_length(kaspi_qr) <= 500),
  kaspi_expires_at   timestamptz,
  pay_started_at     timestamptz,
  pay_claimed_at     timestamptz,
  paid_amount        integer check (paid_amount >= 0),
  paid_at            timestamptz,
  idem_key           text not null check (idem_key ~ '^[A-Za-z0-9_-]{8,64}$'),
  cancel_reason      text check (char_length(cancel_reason) <= 300),
  queued_at          timestamptz not null default now(),                   -- the queue order (a Kaspi job: when paid)
  created_at         timestamptz not null default now(),
  updated_at         timestamptz not null default now(),
  printing_at        timestamptz,
  ready_at           timestamptz,
  closed_at          timestamptz,
  files_deleted_at   timestamptz,
  unique (user_id, idem_key),
  check ((price_state = 'proposed') = (proposed_total is not null))
);
create index print_jobs_board on print_jobs (shop_id, status, queued_at);
create index print_jobs_user on print_jobs (user_id, id desc);
create index print_jobs_pending on print_jobs (pay_started_at) where payment_status = 'pending';
create index print_jobs_retention on print_jobs (closed_at) where files_deleted_at is null;

-- One uploaded file. A job references its files; an upload never attached to a job is removed after a day.
create table print_files (
  id           bigint generated always as identity primary key,
  user_id      bigint references users(id) on delete set null,
  job_id       bigint references print_jobs(id),
  file_name    text not null unique check (file_name ~ '^[0-9a-f]{32}\.(pdf|docx|xlsx|pptx|jpg|png)$'),
  display_name text not null check (char_length(display_name) between 1 and 120),
  kind         text not null check (kind in ('pdf', 'docx', 'xlsx', 'pptx', 'jpg', 'png')),
  bytes        integer not null check (bytes between 1 and 20971520),
  pages        integer check (pages between 1 and 5000),       -- counted by the server (PDF, image); null = unknown
  pages_hint   integer check (pages_hint between 1 and 5000),  -- the Office file's own metadata, not trusted
  sha256       text not null check (sha256 ~ '^[0-9a-f]{64}$'),
  created_at   timestamptz not null default now(),
  deleted_at   timestamptz                                     -- the bytes are gone (retention, cancel, account)
);
create index print_files_user_day on print_files (user_id, created_at);
create index print_files_job on print_files (job_id);
create index print_files_orphans on print_files (created_at) where job_id is null and deleted_at is null;

create table print_job_events (
  id         bigint generated always as identity primary key,
  job_id     bigint not null references print_jobs(id),
  actor_id   bigint references users(id) on delete set null,
  actor_kind text not null check (actor_kind in ('student', 'staff', 'admin', 'system', 'kaspi')),
  kind       text not null check (kind ~ '^[a-z_]{2,40}$'),
  detail     jsonb not null default '{}' check (jsonb_typeof(detail) = 'object'),
  created_at timestamptz not null default now()
);
create index print_job_events_job on print_job_events (job_id, id);

create function print_events_append_only() returns trigger language plpgsql as $$
begin
  if tg_op = 'UPDATE' and new.actor_id is null and old.actor_id is not null
     and (new.id, new.job_id, new.actor_kind, new.kind, new.detail, new.created_at)
         is not distinct from (old.id, old.job_id, old.actor_kind, old.kind, old.detail, old.created_at) then
    return new;
  end if;
  raise exception 'print_job_events is append-only';
end $$;
create trigger print_job_events_append_only before update or delete on print_job_events
  for each row execute function print_events_append_only();

-- The shop's own Kaspi cashier session: the same shape and encryption as cafe_kaspi_sessions (internal/shopkit).
create table print_kaspi_sessions (
  shop_id      bigint primary key references print_shops(id) on delete cascade,
  ciphertext   text not null check (char_length(ciphertext) <= 8000),
  phone_hash   text not null check (phone_hash ~ '^[0-9a-f]{64}$'),
  phone_masked text not null default '' check (char_length(phone_masked) <= 20),
  org_name     text not null default '' check (char_length(org_name) <= 200),
  broken       boolean not null default false,
  last_error   text check (char_length(last_error) <= 300),
  created_by   bigint references users(id) on delete set null,
  updated_at   timestamptz not null default now()
);

create function print_kaspi_ready(shop bigint) returns boolean language sql stable security definer set search_path = public as $$
  select coalesce((select not k.broken from print_kaspi_sessions k where k.shop_id = shop), false)
$$;
revoke all on function print_kaspi_ready(bigint) from public;
grant execute on function print_kaspi_ready(bigint) to app_user;

alter table print_shops enable row level security;
alter table print_staff enable row level security;
alter table print_services enable row level security;
alter table print_goods enable row level security;
alter table print_jobs enable row level security;
alter table print_files enable row level security;
alter table print_job_events enable row level security;
alter table print_kaspi_sessions enable row level security;

grant select on print_shops, print_staff, print_services, print_goods, print_jobs, print_files, print_job_events to app_user;
create policy print_shops_read on print_shops for select to app_user
  using (active or exists (select 1 from print_staff s where s.shop_id = print_shops.id and s.user_id = app_user_id()));
create policy print_staff_self on print_staff for select to app_user using (user_id = app_user_id());
create policy print_services_read on print_services for select to app_user
  using (exists (select 1 from print_shops p where p.id = shop_id));
create policy print_goods_read on print_goods for select to app_user
  using (exists (select 1 from print_shops p where p.id = shop_id));
-- a student sees their own jobs; staff see the jobs of the shops they work at, and nothing else
create policy print_jobs_read on print_jobs for select to app_user
  using (user_id = app_user_id()
         or exists (select 1 from print_staff s where s.shop_id = print_jobs.shop_id and s.user_id = app_user_id()));
create policy print_files_read on print_files for select to app_user
  using (user_id = app_user_id() or exists (select 1 from print_jobs j where j.id = job_id));
create policy print_job_events_read on print_job_events for select to app_user
  using (exists (select 1 from print_jobs j where j.id = job_id));
