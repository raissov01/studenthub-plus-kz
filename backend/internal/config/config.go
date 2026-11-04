// Package config reads all runtime settings from environment variables.
package config

import (
	"os"
	"strconv"
	"strings"
)

func Str(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func Int(key string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key))); err == nil {
		return n
	}
	return def
}

func Bool(key string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func List(key, def string) []string {
	var out []string
	for _, s := range strings.Split(Str(key, def), ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// SiteURL is the public origin of the site, without a trailing slash.
func SiteURL() string {
	return strings.TrimRight(Str("SITE_URL", "https://studenthub-plus-kz.vercel.app"), "/")
}
