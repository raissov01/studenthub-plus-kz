package server

// Reviewed with the simple admin (role invites by email) and the workspace taken from the kurs panel
// (docs/research/admin-from-kurs.md): notes, a person's orders, saved filters, support settings, payments,
// attendance. Merged into routePolicies at init.
var adminWorkspacePolicies = map[string]RoutePolicy{
	"GET /api/admin/invites":                    {"admin", "no", "no (no input)", "—"},
	"POST /api/admin/invites":                   {"admin; grant admin|moderator only, refused for an email with an account, audited; applied once on a verified sign-in", "yes", "yes", "—"},
	"DELETE /api/admin/invites/{id}":            {"admin; audited", "yes", "yes", "—"},
	"GET /api/admin/users/{id}/orders":          {"admin (personal data): the account's 30 latest café orders and print jobs", "no", "yes", "—"},
	"GET /api/admin/users/{id}/notes":           {"admin; internal notes, never shown to the student", "no", "yes", "—"},
	"POST /api/admin/users/{id}/notes":          {"admin; 1–2000 chars, audited without the text", "yes", "yes", "—"},
	"DELETE /api/admin/users/{id}/notes/{note}": {"admin; audited", "yes", "yes", "—"},
	"GET /api/admin/views":                      {"signed-in with the screen's right (users: admin; payments: admin or a shop manager); own filters only", "no", "yes", "—"},
	"POST /api/admin/views":                     {"signed-in with the screen's right; own filters, max 20 per screen, query ≤1000 chars, audited", "yes", "yes", "—"},
	"DELETE /api/admin/views/{id}":              {"the filter's owner only; audited", "yes", "yes", "—"},
	"GET /api/admin/config":                     {"admin; feature switches (read only) and support contacts", "no", "no (no input)", "—"},
	"PUT /api/admin/config/support":             {"admin; plain text fields with length limits, no markup, audited with the changed field names", "yes", "yes", "—"},
	"GET /api/support":                          {"anonymous (the support contacts the admin published)", "no", "no (no input)", "—"},
	"GET /api/admin/payments":                   {"admin (every shop); a café / print manager (own shops only, another shop 403); anyone else 403", "no", "yes", "cafe or print"},
	"GET /api/events/attendance":                {"admin; ambassador / university staff (own university); club admin (own clubs); anyone else 403", "no", "yes", "events"},
}

func init() {
	for k, v := range adminWorkspacePolicies {
		routePolicies[k] = v
	}
}
