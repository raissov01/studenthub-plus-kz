-- Spec 08 «kind streak» (pkg/points, FEATURE_STREAK): one evening reminder the student can turn
-- off, milestone coins, a free exam-session pause once per semester and a second freeze.
-- All columns have defaults, so existing wallets keep working while the switch is off.
alter table points_wallets
  add column streak_reminder             boolean not null default true,
  add column streak_reminder_time        time not null default '20:00'
    check (streak_reminder_time between '19:00' and '21:00'),
  add column streak_reminded_on          date,     -- Almaty day of the last evening nudge (one a day)
  add column session_pause_from          date,     -- «Сессия режимі»: missed days in [from, until] do not break the streak
  add column session_pause_until         date,
  add column session_pause_used_semester text check (session_pause_used_semester ~ '^[0-9]{4}-(fall|spring)$'),
  add column last_milestone              integer not null default 0 check (last_milestone >= 0),
  add constraint points_wallets_pause_range check (session_pause_until is null or session_pause_from is null
    or session_pause_until >= session_pause_from - 1);

-- Duolingo's tested number: two freezes in hand (the price stays fixed).
alter table points_wallets drop constraint points_wallets_freezes_check;
alter table points_wallets add constraint points_wallets_freezes_check check (freezes between 0 and 2);

-- the evening cron scans only wallets that could be nudged today
create index points_wallets_reminder on points_wallets (last_active_day)
  where streak_reminder and streak_days >= 3;
