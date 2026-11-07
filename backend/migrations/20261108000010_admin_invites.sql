-- Admin console, made simple: a role can be given to an email that has not signed in yet.
--
-- admin_role_invites: «this email becomes a moderator / admin when it first signs in». Only an admin creates or
-- removes one (audited invite_create / invite_delete). On a sign-in with that email verified (auth.bootstrapAdmin),
-- the right is applied once, audited as invite_applied, and the row is deleted. An email that already belongs to
-- an account is refused: the admin gives that account the role directly.
create table admin_role_invites (
  id         bigint generated always as identity primary key,
  email      text not null unique check (email = lower(email) and char_length(email) between 3 and 254),
  grant_kind text not null check (grant_kind in ('admin', 'moderator')),
  reason     text not null check (char_length(reason) between 3 and 500),
  invited_by bigint references users(id) on delete set null,
  created_at timestamptz not null default now()
);
create index admin_role_invites_invited_by on admin_role_invites (invited_by) where invited_by is not null;

alter table admin_role_invites enable row level security;
-- no app_user privileges: read and written only by the server as the owner
