package auth

import (
	"strings"
	"testing"

	"github.com/raissov01/studenthub-plus-kz/backend/internal/testdb"
)

// ADMIN_EMAILS promotes an owner who signs in by email, but only once the email is verified, case-insensitively,
// audited once like the Telegram bootstrap.
func TestAdminEmailsBootstrap(t *testing.T) {
	testdb.Need(t)
	in := fakeResend(t)
	email := newEmail()
	t.Setenv("ADMIN_EMAILS", " other@example.kz , "+strings.ToUpper(email))
	// a registered but unverified address never becomes admin
	if w := post(t, "/api/auth/email/register", map[string]string{"email": email, "password": goodPw, "lang": "kk"}, nil); w.Code != 200 {
		t.Fatalf("register: %d %s", w.Code, w.Body)
	}
	norm := strings.ToLower(email)
	if n := count(t, `select count(*) from users where email = $1 and role = 'admin'`, norm); n != 0 {
		t.Fatal("unverified email promoted")
	}
	w := post(t, "/api/auth/email/verify", map[string]string{"email": email, "code": in.code(t, norm), "password": goodPw}, nil)
	if w.Code != 200 || body(w)["user"].(map[string]any)["role"] != "admin" {
		t.Fatalf("verify: %d %s", w.Code, w.Body)
	}
	// a later login keeps the single audit row
	if w := post(t, "/api/auth/email/login", map[string]string{"email": email, "password": goodPw}, fromIP(uniqueIP())); w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	if n := count(t, `select count(*) from audit_log l join users u on u.id = l.target_id
		where u.email = $1 and l.action = 'role_grant' and l.reason = 'ADMIN_EMAILS bootstrap'`, norm); n != 1 {
		t.Fatalf("audit rows: %d", n)
	}
	// someone else's verified email stays a student
	t.Setenv("ADMIN_EMAILS", "other@example.kz")
	other := newEmail()
	signUp(t, in, other, goodPw)
	if n := count(t, `select count(*) from users where email = $1 and role = 'student'`, strings.ToLower(other)); n != 1 {
		t.Fatal("unlisted email promoted")
	}
	if adminEmail("") || !adminEmail(" OTHER@example.kz ") {
		t.Fatal("adminEmail matching")
	}
}

// A role reserved for an email (admin_role_invites) is applied on the first verified sign-in, once, audited.
func TestRoleInviteApplied(t *testing.T) {
	testdb.Need(t)
	in := fakeResend(t)
	mod, adm := newEmail(), newEmail()
	for e, kind := range map[string]string{strings.ToLower(mod): "moderator", strings.ToLower(adm): "admin"} {
		exec(t, `insert into admin_role_invites (email, grant_kind, reason) values ($1, $2, 'Командаға қосылды')`, e, kind)
	}
	// registered but not verified: nothing yet
	if w := post(t, "/api/auth/email/register", map[string]string{"email": mod, "password": goodPw, "lang": "kk"}, nil); w.Code != 200 {
		t.Fatalf("register: %d %s", w.Code, w.Body)
	}
	if n := count(t, `select count(*) from admin_role_invites where email = $1`, strings.ToLower(mod)); n != 1 {
		t.Fatal("invite used before verification")
	}
	w := post(t, "/api/auth/email/verify", map[string]string{"email": mod, "code": in.code(t, strings.ToLower(mod)), "password": goodPw}, nil)
	if w.Code != 200 {
		t.Fatalf("verify: %d %s", w.Code, w.Body)
	}
	if n := count(t, `select count(*) from admin_grants g join users u on u.id = g.user_id where u.email = $1 and g.scope = 'moderator'`, strings.ToLower(mod)); n != 1 {
		t.Fatal("moderator invite not applied")
	}
	signUp(t, in, adm, goodPw)
	if n := count(t, `select count(*) from users where email = $1 and role = 'admin'`, strings.ToLower(adm)); n != 1 {
		t.Fatal("admin invite not applied")
	}
	if n := count(t, `select count(*) from admin_role_invites where email in ($1, $2)`, strings.ToLower(mod), strings.ToLower(adm)); n != 0 {
		t.Fatal("invites not consumed")
	}
	if n := count(t, `select count(*) from audit_log l join users u on u.id = l.target_id where u.email in ($1, $2) and l.action = 'invite_applied'`,
		strings.ToLower(mod), strings.ToLower(adm)); n != 2 {
		t.Fatalf("invite_applied rows: %d", n)
	}
}
