package server

// Reviewed with internal/admin (super admin console) and internal/console (role-scoped panels and the shop
// workspace). Merged into routePolicies at init so parallel features do not collide.
var consolePolicies = map[string]RoutePolicy{
	"GET /api/admin/users/{id}":                    {"admin (personal data)", "no", "yes", "—"},
	"GET /api/admin/dashboard":                     {"admin; counts only", "no", "no (no input)", "—"},
	"GET /api/admin/grants":                        {"admin", "no", "no (no input)", "—"},
	"GET /api/admin/audit":                         {"admin", "no", "yes", "—"},
	"GET /api/admin/voice":                         {"admin; counts only, no ticket texts or answers", "no", "no (no input)", "—"},
	"GET /api/admin/export/{file}":                 {"admin; CSV with BOM, formula cells neutralised, feedback without text/author; every export audited", "no", "yes", "—"},
	"POST /api/admin/users/{id}/block":             {"admin (reason required, audited; not self, not an admin)", "yes", "yes", "—"},
	"POST /api/admin/users/{id}/unblock":           {"admin (reason required, audited)", "yes", "yes", "—"},
	"POST /api/admin/users/{id}/delete":            {"admin (reason + DELETE confirmation, audited; the DELETE /api/me code path; not self, not an admin)", "yes", "yes", "—"},
	"POST /api/admin/grants":                       {"admin (reason required, audited)", "yes", "yes", "—"},
	"POST /api/admin/grants/revoke":                {"admin (reason required, audited)", "yes", "yes", "—"},
	"POST /api/admin/feeds/items/bulk":             {"admin, moderator (admin_grants); the single approve/reject path per item, max 500, expired skipped, one audit row", "yes (+ 20/user/10 min)", "yes", "feeds"},
	"GET /api/console/me":                          {"signed-in (own panels and their counts only)", "no", "no (no input)", "—"},
	"GET /api/console/cafe/{id}/stats":             {"staff of the café (today's board numbers); its manager, admin (+ revenue by day, Kaspi status, team)", "no", "yes", "cafe"},
	"POST /api/console/cafe/{id}/team":             {"manager of the café (baristas only), admin (managers too); not self; audited", "yes", "yes", "cafe"},
	"DELETE /api/console/cafe/{id}/team/{userId}":  {"manager of the café (baristas only), admin; not self; audited", "yes", "yes", "cafe"},
	"GET /api/console/print/{id}/stats":            {"staff of the shop (today's board numbers); its manager, admin (+ revenue by day, Kaspi status, team)", "no", "yes", "print"},
	"POST /api/console/print/{id}/team":            {"manager of the shop (operators only), admin (managers too); not self; audited", "yes", "yes", "print"},
	"DELETE /api/console/print/{id}/team/{userId}": {"manager of the shop (operators only), admin; not self; audited", "yes", "yes", "print"},
}

func init() {
	for k, v := range consolePolicies {
		routePolicies[k] = v
	}
}
