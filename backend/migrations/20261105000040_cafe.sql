-- Campus café pre-ordering («Кофе / Кофейня», internal/cafe, docs/cafe.md), FEATURE_CAFE.
--
-- Money: every price and total is an integer in tiyn (1 ₸ = 100 tiyn) and a whole number of tenge
-- (Kaspi takes tenge). Student Hub never holds money: a Kaspi payment goes straight to the café's own
-- Kaspi Pay business account through a cashier session of that café (services/kaspi-pos); we only read
-- the payment status. Pay-on-pickup needs no money flow at all.
--
-- Every write goes through the server as the connection owner with explicit checks in Go; app_user only
-- reads: a student their own orders, café staff the orders of their own café. The order total is always
-- recomputed by the server from cafe_menu_items; the client never sends a price.

create table cafes (
  id                bigint generated always as identity primary key,
  name_kk           text not null check (char_length(name_kk) between 2 and 80),
  name_ru           text not null check (char_length(name_ru) between 2 and 80),
  building          text not null default '' check (char_length(building) <= 120),  -- «Бас ғимарат», «3-корпус»
  place_kk          text not null default '' check (char_length(place_kk) <= 200),  -- «1-қабат, фойе»
  place_ru          text not null default '' check (char_length(place_ru) <= 200),
  open_time         time not null default '08:00',                                   -- Asia/Almaty wall clock
  close_time        time not null default '18:00',
  open_days         smallint[] not null default '{1,2,3,4,5}'                         -- ISO weekdays, 1 = Monday
                      check (open_days <@ '{1,2,3,4,5,6,7}'::smallint[] and cardinality(open_days) between 1 and 7),
  slot_minutes      smallint not null default 10 check (slot_minutes between 5 and 60),
  slot_capacity     smallint not null default 6 check (slot_capacity between 1 and 100),  -- orders per pickup slot
  lead_minutes      smallint not null default 10 check (lead_minutes between 0 and 120),  -- earliest pickup = now + lead
  active            boolean not null default false,
  payment_mode      text not null default 'pickup' check (payment_mode in ('pickup', 'kaspi', 'both')),
  -- how the café is known in Kaspi Pay (business name / BIN), for people; never a secret
  kaspi_merchant_ref text not null default '' check (char_length(kaspi_merchant_ref) <= 120),
  -- optional OrganizationId when one cashier number serves several businesses (docs/cafe.md)
  kaspi_org_id      bigint check (kaspi_org_id >= 0),
  is_demo           boolean not null default false,
  created_by        bigint references users(id) on delete set null,
  created_at        timestamptz not null default now(),
  updated_at        timestamptz not null default now(),
  check (close_time > open_time)
);

create table cafe_staff (
  cafe_id    bigint not null references cafes(id) on delete cascade,
  user_id    bigint not null references users(id) on delete cascade,
  role       text not null default 'barista' check (role in ('barista', 'manager')),
  added_by   bigint references users(id) on delete set null,
  created_at timestamptz not null default now(),
  primary key (cafe_id, user_id)
);
create index cafe_staff_user on cafe_staff (user_id);

create table cafe_menu_categories (
  id         bigint generated always as identity primary key,
  cafe_id    bigint not null references cafes(id) on delete cascade,
  name_kk    text not null check (char_length(name_kk) between 1 and 60),
  name_ru    text not null check (char_length(name_ru) between 1 and 60),
  sort       smallint not null default 0,
  active     boolean not null default true,
  created_at timestamptz not null default now()
);
create index cafe_menu_categories_cafe on cafe_menu_categories (cafe_id, sort, id);

-- options: [{"id":"size","nameKk":"Көлемі","nameRu":"Объём","required":true,"multi":false,
--            "choices":[{"id":"s","nameKk":"250 мл","nameRu":"250 мл","delta":0},{"id":"l",...,"delta":20000}]}]
-- delta in tiyn, validated by the server (internal/cafe ValidateOptions).
create table cafe_menu_items (
  id          bigint generated always as identity primary key,
  cafe_id     bigint not null references cafes(id) on delete cascade,
  category_id bigint references cafe_menu_categories(id) on delete set null,
  name_kk     text not null check (char_length(name_kk) between 1 and 80),
  name_ru     text not null check (char_length(name_ru) between 1 and 80),
  desc_kk     text not null default '' check (char_length(desc_kk) <= 300),
  desc_ru     text not null default '' check (char_length(desc_ru) <= 300),
  price       integer not null check (price between 100 and 10000000 and price % 100 = 0),
  available   boolean not null default true,
  archived    boolean not null default false,
  options     jsonb not null default '[]' check (jsonb_typeof(options) = 'array'),
  sort        smallint not null default 0,
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now()
);
create index cafe_menu_items_cafe on cafe_menu_items (cafe_id, sort, id) where not archived;

create table cafe_orders (
  id                 bigint generated always as identity primary key,
  cafe_id            bigint not null references cafes(id),          -- a café with orders is deactivated, not deleted
  user_id            bigint references users(id) on delete set null, -- account deletion anonymizes the café's record
  items              jsonb not null check (jsonb_typeof(items) = 'array' and jsonb_array_length(items) between 1 and 20),
  total              integer not null check (total > 0 and total % 100 = 0),
  pickup_at          timestamptz not null,
  pickup_code        text not null check (pickup_code ~ '^[0-9]{3}$'),
  note               text not null default '' check (char_length(note) <= 140),
  status             text not null default 'new'
                       check (status in ('new', 'accepted', 'ready', 'picked_up', 'cancelled', 'no_show')),
  payment_method     text not null check (payment_method in ('pickup', 'kaspi')),
  payment_status     text not null default 'unpaid'
                       check (payment_status in ('unpaid', 'pending', 'paid', 'refunded', 'failed')),
  refund_manual      boolean not null default false,                -- paid, cancelled, the café must return the money by hand
  kaspi_operation_id text unique check (kaspi_operation_id ~ '^[0-9A-Za-z_-]{1,64}$'),
  kaspi_pay_url      text check (char_length(kaspi_pay_url) <= 500),
  kaspi_qr           text check (char_length(kaspi_qr) <= 500),
  kaspi_expires_at   timestamptz,
  pay_started_at     timestamptz,
  pay_claimed_at     timestamptz,                                   -- an invoice request is in flight
  paid_amount        integer check (paid_amount >= 0),
  paid_at            timestamptz,
  idem_key           text not null check (idem_key ~ '^[A-Za-z0-9_-]{8,64}$'),
  cancel_reason      text check (char_length(cancel_reason) <= 300),
  created_at         timestamptz not null default now(),
  updated_at         timestamptz not null default now(),
  accepted_at        timestamptz,
  ready_at           timestamptz,
  closed_at          timestamptz,
  unique (user_id, idem_key)
);
create index cafe_orders_board on cafe_orders (cafe_id, status, pickup_at);
create index cafe_orders_user on cafe_orders (user_id, id desc);
create index cafe_orders_pending on cafe_orders (pay_started_at) where payment_status = 'pending';
create index cafe_orders_day on cafe_orders (user_id, created_at);

-- Audit trail of every order change: who (student/staff/admin/system/kaspi), what, when. Append-only.
create table cafe_order_events (
  id         bigint generated always as identity primary key,
  order_id   bigint not null references cafe_orders(id),
  actor_id   bigint references users(id) on delete set null,
  actor_kind text not null check (actor_kind in ('student', 'staff', 'admin', 'system', 'kaspi')),
  kind       text not null check (kind ~ '^[a-z_]{2,40}$'),
  detail     jsonb not null default '{}' check (jsonb_typeof(detail) = 'object'),
  created_at timestamptz not null default now()
);
create index cafe_order_events_order on cafe_order_events (order_id, id);

create function cafe_events_append_only() returns trigger language plpgsql as $$
begin
  -- the only change allowed: account deletion anonymizing the actor
  if tg_op = 'UPDATE' and new.actor_id is null and old.actor_id is not null
     and (new.id, new.order_id, new.actor_kind, new.kind, new.detail, new.created_at)
         is not distinct from (old.id, old.order_id, old.actor_kind, old.kind, old.detail, old.created_at) then
    return new;
  end if;
  raise exception 'cafe_order_events is append-only';
end $$;
create trigger cafe_order_events_append_only before update or delete on cafe_order_events
  for each row execute function cafe_events_append_only();

-- The café's Kaspi cashier session, AES-256-GCM encrypted with CAFE_KASPI_SESSION_KEY (the vtokenSecret inside
-- is already encrypted again by the sidecar's TOKEN_SECRET_KEY). Only the server reads it; app_user has no access.
create table cafe_kaspi_sessions (
  cafe_id      bigint primary key references cafes(id) on delete cascade,
  ciphertext   text not null check (char_length(ciphertext) <= 8000),
  phone_hash   text not null check (phone_hash ~ '^[0-9a-f]{64}$'),  -- HMAC, to refresh cafés that share one number
  phone_masked text not null default '' check (char_length(phone_masked) <= 20),
  org_name     text not null default '' check (char_length(org_name) <= 200),
  broken       boolean not null default false,                        -- Kaspi rejected it: log in again
  last_error   text check (char_length(last_error) <= 300),
  created_by   bigint references users(id) on delete set null,
  updated_at   timestamptz not null default now()
);

-- Every sidecar webhook delivery, for audit and to make a redelivery a no-op.
create table cafe_kaspi_webhooks (
  id           bigint generated always as identity primary key,
  operation_id text not null check (char_length(operation_id) <= 64),
  event        text not null check (char_length(event) <= 40),
  status       text check (char_length(status) <= 60),
  amount       integer,
  outcome      text not null check (char_length(outcome) <= 40),
  order_id     bigint references cafe_orders(id),
  received_at  timestamptz not null default now(),
  unique (operation_id, event)
);

-- Whether Kaspi can be offered for a café, without exposing the session table to app_user.
create function cafe_kaspi_ready(cafe bigint) returns boolean language sql stable security definer set search_path = public as $$
  select coalesce((select not k.broken from cafe_kaspi_sessions k where k.cafe_id = cafe), false)
$$;
revoke all on function cafe_kaspi_ready(bigint) from public;
grant execute on function cafe_kaspi_ready(bigint) to app_user;

alter table cafes enable row level security;
alter table cafe_staff enable row level security;
alter table cafe_menu_categories enable row level security;
alter table cafe_menu_items enable row level security;
alter table cafe_orders enable row level security;
alter table cafe_order_events enable row level security;
alter table cafe_kaspi_sessions enable row level security;
alter table cafe_kaspi_webhooks enable row level security;

grant select on cafes, cafe_staff, cafe_menu_categories, cafe_menu_items, cafe_orders, cafe_order_events to app_user;
create policy cafes_read on cafes for select to app_user
  using (active or exists (select 1 from cafe_staff s where s.cafe_id = cafes.id and s.user_id = app_user_id()));
create policy cafe_staff_self on cafe_staff for select to app_user using (user_id = app_user_id());
create policy cafe_menu_categories_read on cafe_menu_categories for select to app_user
  using (exists (select 1 from cafes c where c.id = cafe_id));
create policy cafe_menu_items_read on cafe_menu_items for select to app_user
  using (exists (select 1 from cafes c where c.id = cafe_id));
-- a student sees their own orders; staff see the orders of the cafés they work at, and nothing else
create policy cafe_orders_read on cafe_orders for select to app_user
  using (user_id = app_user_id()
         or exists (select 1 from cafe_staff s where s.cafe_id = cafe_orders.cafe_id and s.user_id = app_user_id()));
create policy cafe_order_events_read on cafe_order_events for select to app_user
  using (exists (select 1 from cafe_orders o where o.id = order_id));

-- Transactional order updates (internal/notify Order): the student or staff member caused them, so they skip
-- the daily cap and quiet hours, and are never deferred (sent now or kept in the inbox only).
alter table notify_outbox drop constraint notify_outbox_kind_check;
alter table notify_outbox add constraint notify_outbox_kind_check check (kind in ('direct', 'digest', 'order'));
alter table notify_sends drop constraint notify_sends_kind_check;
alter table notify_sends add constraint notify_sends_kind_check check (kind in ('direct', 'digest', 'order'));
