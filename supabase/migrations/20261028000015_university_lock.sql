-- Anyone whose rights depend on their university (non-student roles, approved mentors, active
-- ambassadors) cannot move to another university by editing their own profile — otherwise
-- they would gain organizer rights over that university's events. Admin changes go through the
-- server role (not app_user) and are unaffected.
create or replace function users_staff_university_guard() returns trigger language plpgsql as $$
begin
  if current_user = 'app_user' and new.university is distinct from old.university
     and (old.role <> 'student'
          or exists (select 1 from university_staff s where s.user_id = old.id)
          or exists (select 1 from mentors m where m.user_id = old.id and m.status in ('approved', 'paused'))
          or exists (select 1 from ambassador_terms t where t.user_id = old.id and t.status = 'active')) then
    new.university := old.university;
  end if;
  return new;
end $$;
