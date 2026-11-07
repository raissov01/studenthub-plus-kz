-- «Sign in with Google» (internal/auth/google.go, docs/GOOGLE_LOGIN.md).
--
-- * users.google_sub is the OpenID Connect "sub" of the Google account linked to the row: Google's stable,
--   never reassigned account id (the email can change, the sub cannot). One Google account per users row and
--   one row per Google account (unique). NULL while no Google account is linked.
-- * Only the server writes it: app_user keeps its column grants from 20261028000002 (no update on google_sub),
--   and it reads the column only on its own row through the existing users_self_read policy, like telegram_id.
-- * The row goes with the account (DELETE /api/me deletes the users row), so nothing else to clean up.
-- * users_has_identity: a row must still be reachable by some sign-in method; Google counts now.

alter table users add column google_sub text;

alter table users add constraint users_google_sub_shape
  check (google_sub is null or (length(google_sub) between 1 and 255 and google_sub !~ '[[:space:][:cntrl:]]'));
create unique index users_google_sub_key on users (google_sub) where google_sub is not null;

alter table users drop constraint users_has_identity;
alter table users add constraint users_has_identity
  check (telegram_id is not null or email is not null or google_sub is not null);
