package server

// Security review of the community routes (internal/community), merged into routePolicies.
func init() {
	for pattern, p := range map[string]RoutePolicy{
		"GET /api/lostfound/posts":                 {"anonymous (no author identity in any answer)", "no", "yes", "lostfound"},
		"GET /api/lostfound/posts/{id}":            {"anonymous (claims: the two parties; contacts only after confirmation)", "no", "yes", "lostfound"},
		"POST /api/lostfound/posts":                {"signed-in", "yes", "yes", "lostfound"},
		"POST /api/lostfound/posts/{id}/close":     {"signed-in", "yes", "yes", "lostfound"},
		"POST /api/lostfound/posts/{id}/claims":    {"signed-in", "yes", "yes", "lostfound"},
		"POST /api/lostfound/claims/{id}/decide":   {"signed-in", "yes", "yes", "lostfound"},
		"POST /api/lostfound/claims/{id}/returned": {"signed-in", "yes", "yes", "lostfound"},
		"GET /api/lostfound/mine":                  {"signed-in", "no", "no (no input)", "lostfound"},
		"POST /api/lostfound/photos":               {"signed-in", "yes", "yes", "lostfound"},
		"GET /api/lostfound/photos/{name}":         {"anonymous (active post); uploader, admin or moderator otherwise", "no", "yes", "lostfound"},
		"POST /api/admin/lostfound/moderate":       {"admin, moderator (admin_grants)", "yes", "yes", "lostfound"},

		"GET /api/qa/questions":                  {"anonymous (author names: signed-in; anonymous authors: admin only)", "no", "yes", "courseqa"},
		"GET /api/qa/questions/{id}":             {"anonymous (author names: signed-in; anonymous authors: admin only)", "no", "yes", "courseqa"},
		"POST /api/qa/questions":                 {"signed-in", "yes", "yes", "courseqa"},
		"DELETE /api/qa/questions/{id}":          {"signed-in", "yes", "yes", "courseqa"},
		"POST /api/qa/questions/{id}/answers":    {"signed-in", "yes", "yes", "courseqa"},
		"DELETE /api/qa/answers/{id}":            {"signed-in", "yes", "yes", "courseqa"},
		"POST /api/qa/questions/{id}/accept":     {"signed-in", "yes", "yes", "courseqa"},
		"POST /api/qa/vote":                      {"signed-in", "yes", "yes", "courseqa"},
		"POST /api/admin/qa/moderate":            {"admin, moderator (admin_grants)", "yes", "yes", "courseqa"},
		"POST /api/admin/qa/questions/{id}/lock": {"admin, moderator (admin_grants)", "yes", "yes", "courseqa"},

		"GET /api/roommates/me":                      {"signed-in", "no", "no (no input)", "roommates"},
		"POST /api/roommates/gender":                 {"signed-in", "yes", "yes", "roommates"},
		"GET /api/roommates/listings":                {"signed-in (strict gender rule applied)", "no", "yes", "roommates"},
		"GET /api/roommates/listings/{id}":           {"signed-in (strict gender rule applied)", "no", "yes", "roommates"},
		"POST /api/roommates/listings":               {"signed-in", "yes", "yes", "roommates"},
		"POST /api/roommates/listings/{id}/close":    {"signed-in", "yes", "yes", "roommates"},
		"POST /api/roommates/listings/{id}/requests": {"signed-in", "yes", "yes", "roommates"},
		"POST /api/roommates/requests/{id}/decide":   {"signed-in", "yes", "yes", "roommates"},
		"GET /api/roommates/requests":                {"signed-in", "no", "no (no input)", "roommates"},
		"POST /api/admin/roommates/moderate":         {"admin, moderator (admin_grants)", "yes", "yes", "roommates"},
	} {
		routePolicies[pattern] = p
	}
}
