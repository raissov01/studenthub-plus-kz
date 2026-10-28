package launch

import (
	"strings"
	"testing"
)

func TestPrivacyAllowsOnlyNonPersonalDiagnostics(t *testing.T) {
	for _, msg := range []string{"Alice Example alice@example.com +7 (777) 123-45-67 123456789012", "TypeError: Alice alice@example.com ８７７７１２３４５６７", strings.Repeat("қ", 1000), "Bearer secret-token", "Error: https://site/path?token=secret"} {
		got := cleanMessage(msg)
		if len(got) > 500 || strings.Contains(got, "Alice") || strings.Contains(got, "example") || strings.Contains(got, "777") || strings.Contains(got, "secret") || strings.Contains(got, "８") {
			t.Fatalf("leak %q", got)
		}
	}
	if got := cleanMessage("Failed to fetch"); got != "Failed to fetch" {
		t.Fatal(got)
	}
	for in, want := range map[string]string{"/jobs/12345?email=private@example.com": "/jobs", "https://site/profile/Alice#token": "/profile", "/p/private-slug": "/p", "/admin/health?token=x": "/admin/health", "//alice@example.com/": "/"} {
		if got := cleanScreen(in); got != want {
			t.Errorf("screen %q = %q want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"Mozilla/5.0 Windows Chrome/131.0.123 Safari/537.36": "Chrome/131", "Chrome/131.0 Edg/131.0": "Edge/131", "Version/18.1 Safari/605.1": "Safari/18", "Firefox/130": "Firefox/130", "Alice <alice@example.com>": "Unknown"} {
		if got := cleanBrowser(in); got != want {
			t.Errorf("browser %q = %q want %q", in, got, want)
		}
	}
}
