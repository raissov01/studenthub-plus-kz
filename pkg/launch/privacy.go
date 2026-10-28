package launch

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/raissov01/studenthub-plus-kz/pkg/config"
)

var email = regexp.MustCompile(`[^\s@]+@[^\s@]+\.[^\s@]+`)
var phone = regexp.MustCompile(`\+?\d[\d\s().-]{5,}\d`)
var digits = regexp.MustCompile(`\d{5,}`)
var browserVersion = regexp.MustCompile(`(?i)(Edg|Edge|OPR|Opera|Firefox|FxiOS|Chrome|CriOS|Version|Safari)/([0-9]{1,3})(?:[. /;)]|$)`)

// Free-form error text can contain names, tokens and form values. Redaction alone cannot
// guarantee privacy. Keep only known, constant diagnostics; categorize all other text.
func cleanMessage(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) && (r < '0' || r > '9') {
			return '0'
		}
		return r
	}, s)
	s = email.ReplaceAllString(s, "[redacted]")
	s = phone.ReplaceAllString(s, "[redacted]")
	s = digits.ReplaceAllString(s, "[redacted]")
	for _, known := range []string{"Script error.", "Failed to fetch", "NetworkError when attempting to fetch resource.", "Load failed", "ResizeObserver loop limit exceeded", "ResizeObserver loop completed with undelivered notifications.", "Cannot read properties of undefined", "Cannot read properties of null"} {
		if strings.TrimSpace(s) == known {
			return known
		}
	}
	for _, class := range []string{"TypeError", "ReferenceError", "SyntaxError", "RangeError", "URIError", "EvalError", "Error", "UnhandledRejection"} {
		if strings.HasPrefix(strings.TrimSpace(s), class+":") {
			return class + ": [details redacted]"
		}
	}
	return "Client error: [details redacted]"
}

// Only fixed application screen names survive. IDs, slugs, names and arbitrary path
// components are dropped, as are queries/fragments (which can hold Telegram credentials).
func cleanScreen(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return "/"
	}
	path := u.Path
	if u.IsAbs() {
		path = u.EscapedPath()
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	allowed := map[string]bool{"admin": true, "events": true, "event": true, "market": true, "jobs": true, "job": true, "opportunities": true, "opportunity": true, "discounts": true, "mentors": true, "ambassador": true, "employer": true, "university": true, "profile": true, "portfolio": true, "settings": true, "health": true, "applications": true, "announcements": true, "p": true, "my": true, "mine": true, "new": true, "edit": true, "me": true, "map": true, "schedule": true, "onboarding": true}
	out := []string{}
	for _, p := range parts {
		if !allowed[p] {
			break
		}
		out = append(out, p)
		if len(out) == 4 {
			break
		}
	}
	return "/" + strings.Join(out, "/")
}
func cleanBrowser(s string) string {
	matches := browserVersion.FindAllStringSubmatch(s, -1)
	families := []string{"Edge", "Opera", "Firefox", "Chrome", "Safari"}
	for _, family := range families {
		for _, m := range matches {
			f := ""
			switch strings.ToLower(m[1]) {
			case "edg", "edge":
				f = "Edge"
			case "opr", "opera":
				f = "Opera"
			case "firefox", "fxios":
				f = "Firefox"
			case "chrome", "crios":
				f = "Chrome"
			case "version", "safari":
				f = "Safari"
			}
			if f == family {
				return f + "/" + m[2]
			}
		}
	}
	return "Unknown"
}
func errorRateKey(ip string, now time.Time) string {
	mac := hmac.New(sha256.New, []byte(config.Str("SESSION_SECRET", "client-errors-rate-limit")))
	mac.Write([]byte(now.UTC().Format("2006-01-02") + "|" + ip))
	return "client-errors:" + hex.EncodeToString(mac.Sum(nil))
}
func durationSeconds(d time.Duration) string { return strconv.Itoa(int(d.Seconds()) + 1) }
