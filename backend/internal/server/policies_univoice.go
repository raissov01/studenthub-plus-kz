package server

// Reviewed with internal/surveys, internal/feedback and internal/wellbeing (docs/UNIVOICE.md).
// Kept in its own file and merged into routePolicies at init so parallel features do not collide.
var univoicePolicies = map[string]RoutePolicy{
	"GET /api/surveys":                        {"student, ambassador (consented; only surveys whose university/audience match)", "no", "no (no input)", "surveys"},
	"GET /api/surveys/{id}":                   {"student, ambassador (consented; eligible surveys only, else 404)", "no", "yes", "surveys"},
	"POST /api/surveys/{id}/responses":        {"student, ambassador (consented; once per survey via HMAC marker, 409 after)", "yes", "yes", "surveys"},
	"GET /api/staff/surveys":                  {"university_staff (bound university), admin", "no", "no (no input)", "surveys"},
	"POST /api/staff/surveys":                 {"university_staff (bound university), admin", "yes", "yes", "surveys"},
	"GET /api/staff/surveys/{id}":             {"university_staff (bound university), admin", "no", "yes", "surveys"},
	"PUT /api/staff/surveys/{id}":             {"university_staff (bound university), admin; drafts only (published: closing date)", "yes", "yes", "surveys"},
	"POST /api/staff/surveys/{id}/publish":    {"university_staff (bound university), admin", "yes", "yes", "surveys"},
	"POST /api/staff/surveys/{id}/close":      {"university_staff (bound university), admin", "yes", "yes", "surveys"},
	"DELETE /api/staff/surveys/{id}":          {"university_staff (bound university), admin; draft or closed", "yes", "yes", "surveys"},
	"GET /api/staff/surveys/{id}/results":     {"university_staff (bound university), admin; aggregates only, groups under 5 hidden", "no", "yes", "surveys"},
	"GET /api/staff/surveys/{id}/results.csv": {"university_staff (bound university), admin; aggregate CSV only, no texts, under 5 hidden", "no", "yes", "surveys"},

	"GET /api/feedback/recipients":                  {"signed-in (own university's active recipients)", "no", "no (no input)", "feedback"},
	"POST /api/feedback/tickets":                    {"student, ambassador (consented); 5/day per account; threats need ack and are held", "yes", "yes", "feedback"},
	"GET /api/feedback/mine":                        {"signed-in (own signed tickets)", "no", "no (no input)", "feedback"},
	"POST /api/feedback/lookup":                     {"anonymous (ticket token is the credential)", "yes", "yes", "feedback"},
	"POST /api/feedback/tickets/{id}/messages":      {"anonymous (ticket token) or signed author", "yes", "yes", "feedback"},
	"POST /api/feedback/tickets/{id}/read":          {"anonymous (ticket token) or signed author", "yes", "yes", "feedback"},
	"GET /api/feedback/photos/{name}":               {"university_staff (assigned recipient), admin", "no", "yes", "feedback"},
	"GET /api/staff/feedback":                       {"university_staff (assigned recipients), admin; never an anonymous sender", "no", "yes", "feedback"},
	"GET /api/staff/feedback/{id}":                  {"university_staff (assigned recipient), admin", "no", "yes", "feedback"},
	"POST /api/staff/feedback/{id}/reply":           {"university_staff (assigned recipient), admin", "yes", "yes", "feedback"},
	"POST /api/staff/feedback/{id}/status":          {"university_staff (assigned recipient), admin", "yes", "yes", "feedback"},
	"GET /api/admin/feedback/recipients":            {"admin", "no", "yes", "feedback"},
	"POST /api/admin/feedback/recipients":           {"admin", "yes", "yes", "feedback"},
	"PATCH /api/admin/feedback/recipients/{id}":     {"admin", "yes", "yes", "feedback"},
	"PUT /api/admin/feedback/recipients/{id}/staff": {"admin", "yes", "yes", "feedback"},
	"GET /api/cron/feedback":                        {"cron (CRON_SECRET)", "no", "yes", "feedback"},

	"GET /api/wellbeing/contacts":       {"anonymous", "no", "yes", "wellbeing"},
	"PUT /api/staff/wellbeing/contacts": {"university_staff (bound university), admin", "yes", "yes", "wellbeing"},
}

func init() {
	for k, v := range univoicePolicies {
		routePolicies[k] = v
	}
}
