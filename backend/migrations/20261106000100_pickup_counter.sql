-- The counter (docs/print.md «Беру»): a pickup code is unique among a shop's open orders, so the operator
-- can find the one stack by what the student says or shows. Before, shopkit.PickupCode was a bare random
-- 100–999 with no check, and at a few hundred open jobs a day two students could hold the same number.
--
-- Print: four digits (1000–9999). A busy campus copy shop may take ~1000 jobs a day; internal/shopkit
-- UniqueCode avoids every code of the open jobs AND of today's closed ones (a student may still read
-- today's message), which three digits (900 codes) cannot promise. Old three-digit codes stay valid.
-- Café: still three digits (a slot holds a few orders; the board is small), unique among its open orders.
--
-- The server picks the code under the shop's row lock; the partial unique indexes below are the database's
-- own guard. Any duplicates already open are renumbered first (the later order gets a free code).

alter table print_jobs drop constraint print_jobs_pickup_code_check;
alter table print_jobs add constraint print_jobs_pickup_code_check check (pickup_code ~ '^[0-9]{3,4}$');

do $$
declare
  r record;
  c text;
begin
  for r in
    select id, shop_id from (
      select id, shop_id, row_number() over (partition by shop_id, pickup_code order by id) as n
      from print_jobs where status in ('new', 'printing', 'ready')) d
    where n > 1
  loop
    select x::text into c from generate_series(1000, 9999) x
     where x::text not in (select pickup_code from print_jobs where shop_id = r.shop_id and status in ('new', 'printing', 'ready'))
     order by random() limit 1;
    update print_jobs set pickup_code = c, updated_at = now() where id = r.id;
  end loop;
  for r in
    select id, cafe_id from (
      select id, cafe_id, row_number() over (partition by cafe_id, pickup_code order by id) as n
      from cafe_orders where status in ('new', 'accepted', 'ready')) d
    where n > 1
  loop
    select x::text into c from generate_series(100, 999) x
     where x::text not in (select pickup_code from cafe_orders where cafe_id = r.cafe_id and status in ('new', 'accepted', 'ready'))
     order by random() limit 1;
    update cafe_orders set pickup_code = c, updated_at = now() where id = r.id;
  end loop;
end $$;

create unique index print_jobs_open_code on print_jobs (shop_id, pickup_code) where status in ('new', 'printing', 'ready');
create unique index cafe_orders_open_code on cafe_orders (cafe_id, pickup_code) where status in ('new', 'accepted', 'ready');
-- the counter's lookup by code, today's closed jobs included
create index print_jobs_code on print_jobs (shop_id, pickup_code, created_at desc);

-- How the operator keeps one student's pages apart from the next in the output tray:
--   none (default) — no paper: the board's print order is the stack order, and the printer driver's
--                    «Offset / Сдвиг / Job separation» shifts each print job sideways
--   slip           — small slips, four to an A4 sheet (one sheet per four jobs), cut and laid on each stack
--   full           — a full cover page in front of each job (the shop chose to spend the sheet)
-- The student's own document is never changed.
alter table print_shops add column separator text not null default 'none' check (separator in ('none', 'slip', 'full'));
