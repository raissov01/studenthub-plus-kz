# Email + password sign-in API

Second way in, next to Telegram. Code: `pkg/auth/email.go`, `pkg/auth/password.go`,
`pkg/auth/codes.go`, `pkg/mail`. Schema: `supabase/migrations/20261028000018_email_auth.sql`.
Background and Resend setup (Kazakh): [docs/ACCOUNTS.md](../ACCOUNTS.md).

All endpoints are `POST` with a JSON body, go through `auth.Write` (Origin/Referer must be in
`ALLOWED_HOSTS`, `RATE_WRITE_PER_MIN` per IP and per user) and answer errors as
`{"error":{"code","message"}}` with `message` in kk or ru (`?lang=` / `Accept-Language`).
A successful sign-in sets the same `sh_session` cookie as Telegram (HttpOnly, Secure,
SameSite=Lax, 30 days) and returns `{user, needsConsent}`. No bearer token is returned for
email sign-in.

`GET /api/config` has `"emailAuth": true|false`: true when Resend is configured
(`RESEND_API_KEY` + `MAIL_FROM`) or `MAIL_DEV_LOG=true`. Hide the email tab when false.

`user` (also from `GET /api/me`) now has:

```json
{ "id": 42, "telegramId": null, "email": "aigerim@example.kz", "emailVerified": true, "...": "..." }
```

`telegramId` is `null` for an email-only account; `email` is `null` until email sign-in is set
up. A signed-in user with `email != null` always has `emailVerified: true` and a password.

## Endpoints

| Path | Body | Success | Extra throttle |
|---|---|---|---|
| `/api/auth/email/register` | `{email, password, lang?, ref?}` | `200 {ok, resendAfter}` | send: 20/h per IP, 5/h per address |
| `/api/auth/email/verify` | `{email, code, password}` | `200 {user, needsConsent}` + cookie | check: 30/15 min per IP, 10/15 min per address |
| `/api/auth/email/login` | `{email, password}` | `200 {user, needsConsent}` + cookie | login: 30/15 min per IP, 10/15 min per address |
| `/api/auth/email/resend` | `{email, purpose?}` (`verify_email` default, or `reset_password`) | `200 {ok, resendAfter}` | send |
| `/api/auth/email/reset/request` | `{email}` | `200 {ok, resendAfter}` | send |
| `/api/auth/email/reset/confirm` | `{email, code, newPassword}` | `200 {user, needsConsent}` + new cookie | check |
| `/api/me/password` | `{currentPassword, newPassword}` | `200 {user, needsConsent}` + new cookie | 10/15 min per user |
| `/api/me/email` | `{email, password}` (account without a password) or `{email, currentPassword}` | `200 {ok, resendAfter}` | send, also 5/h per user |
| `/api/me/email/confirm` | `{code}` | `200 {user, needsConsent}` + cookie | 10/15 min per user |

`resendAfter` is 60 (seconds): a new code for the same address and purpose is not sent sooner.
Codes are 6 digits, valid 15 minutes, 5 wrong tries, then dead (request a new one).
`ref` on register accepts the same values as Telegram sign-in (`share`, `amb_<code>`).

### Behaviour that the frontend must not try to "fix"

- `register`, `resend`, `reset/request` and `me/email` answer `200 {ok:true}` whether or not
  the address has an account. When the address belongs to an existing account, its owner gets a
  notice email instead of a code, and the following `verify`/`confirm` just fails with
  `invalid_code`. Do not show "account exists" — show "check your inbox".
- `verify` requires the password typed at registration (keep it in memory between the two
  screens; ask for it again if the page was reloaded).
- `login` with the right password on an unconfirmed address answers `403 email_not_verified`
  and sends a fresh code: switch to the code screen.
- `reset/confirm` and `me/password` sign out every other device (`session_version + 1`); the
  response carries the new cookie for this device. Changing an existing address via
  `me/email/confirm` does the same; adding the first address does not.
- Linking Telegram: while signed in by email, run the normal Telegram sign-in
  (`POST /api/auth/telegram/widget` or `/webapp`). The Telegram id is attached to the current
  account (no second account). If it already belongs to another account: `409 telegram_taken`
  and the session is unchanged; nothing is merged.

## Errors

| Status | Code | When |
|---|---|---|
| 400 | `bad_request` | malformed JSON / unknown `purpose` |
| 400 | `invalid_email` | not a bare `local@domain.tld`, max 254 bytes |
| 400 | `password_too_short` / `password_too_long` | under 8 / over 128 characters |
| 400 | `password_common` | on the common-password list, or one repeated character |
| 400 | `password_like_email` | equal to the email or its part before `@` |
| 400 | `invalid_code` | wrong, expired, used, locked or missing code (also: wrong password on `verify`, unknown address) |
| 400 | `wrong_password` | `me/password`, `me/email`: current password wrong |
| 400 | `no_password` | `me/password` on an account without email sign-in |
| 400 | `same_email` | `me/email` with the address already on the account |
| 401 | `invalid_credentials` | `login`: unknown address or wrong password (same answer) |
| 401 | `unauthorized` | `me/*` without a session |
| 403 | `bad_origin` | Origin/Referer not allowed |
| 403 | `email_not_verified` | `login`: right password, address not confirmed (a code was sent) |
| 403 | `account_blocked` | account blocked (`deleted_at`) |
| 409 | `email_taken` | `me/email/confirm`: address was taken by another account meanwhile |
| 409 | `telegram_taken` | Telegram sign-in while signed in by email, Telegram id on another account |
| 429 | `rate_limited` | throttled; see `Retry-After` |
| 503 | `email_auth_off` | no Resend and no `MAIL_DEV_LOG` (except `login`, which still works) |
| 503 | `auth_off` | `SESSION_SECRET` missing |

## Frontend checklist

- [ ] Sign-in sheet with two tabs: **Telegram** (existing widget / Mini App) and **Email**
      (only when `config.emailAuth`). Inside the Mini App, keep Telegram as the default.
- [ ] Email tab: sign in (email + password, "Forgot password?" link, "Create account" link).
- [ ] Register: email, password (show the rules: 8+ characters, not a common one, not your
      email), language from the UI; then the code screen. Map `password_*` codes to inline errors.
- [ ] Code screen: 6 digit inputs (`inputmode="numeric"`, `autocomplete="one-time-code"`),
      "Resend" disabled with a countdown from `resendAfter`, then `POST /resend`;
      on `invalid_code` clear the inputs and say the code is wrong or expired.
- [ ] After `verify` / `login`: same as Telegram — if `needsConsent`, show the consent screen.
- [ ] Reset password: email → code + new password → signed in.
- [ ] Settings → Security: change password (current + new) when `user.email` is set;
      "Add email" (email + new password → code) when `user.email` is null;
      "Change email" (new email + current password → code) when it is set.
- [ ] Settings → Telegram: when `user.telegramId` is null, show "Link Telegram" (runs the
      Telegram sign-in while signed in); handle `409 telegram_taken` with the server message.
- [ ] Anything that needs the bot (reminders, DMs) should tell email-only users to link
      Telegram first: the bot cannot message an account without `telegramId`.
- [ ] Never store the password, the code or the email in `localStorage`.
