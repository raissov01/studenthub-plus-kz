-- 05 × 07: an approved mentor may create and edit `cat = mentoring` group meetings of their own
-- university, and see who registered for the meetings they created. Go checks mirror this
-- (pkg/server/glue.go: events.ExtraManager).

create or replace function app_is_approved_mentor() returns boolean
language sql stable security definer set search_path = public, pg_temp as $$
  select exists (select 1 from mentors where user_id = app_user_id() and status = 'approved')
$$;
grant execute on function app_is_approved_mentor() to app_user;

create or replace function app_own_university() returns text
language sql stable security definer set search_path = public, pg_temp as $$
  select university from users where id = app_user_id() and deleted_at is null
$$;
grant execute on function app_own_university() to app_user;

create policy events_mentor_insert on events for insert to app_user
  with check (cat = 'mentoring' and created_by = app_user_id() and app_is_approved_mentor()
              and university = app_own_university());
create policy events_mentor_update on events for update to app_user
  using (cat = 'mentoring' and created_by = app_user_id() and app_is_approved_mentor())
  with check (cat = 'mentoring' and created_by = app_user_id() and university = app_own_university());
create policy events_mentor_read on events for select to app_user
  using (cat = 'mentoring' and created_by = app_user_id());
create policy event_regs_mentor_read on event_regs for select to app_user
  using (exists (select 1 from events e where e.id = event_id and e.cat = 'mentoring'
                 and e.created_by = app_user_id() and app_is_approved_mentor()));
