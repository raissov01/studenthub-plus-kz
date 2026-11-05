package feeds

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

type registryFile struct {
	Sources []Source `yaml:"sources"`
}

var (
	idRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,63}$`)
	handleRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{3,31}$`)
)

// RegistryPath is $FEEDS_SOURCES, else feeds/sources.yaml next to data/ (backend/ locally,
// /app in the image).
func RegistryPath(getenv func(string) string) string {
	if p := strings.TrimSpace(getenv("FEEDS_SOURCES")); p != "" {
		return p
	}
	for _, p := range []string{"feeds/sources.yaml", "/app/feeds/sources.yaml"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "feeds", "sources.yaml")
}

// LoadRegistry reads and validates the registry file.
func LoadRegistry(path string) ([]Source, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseRegistry(b)
}

// ParseRegistry validates every entry; one bad entry fails the whole file (it is in git, so a
// mistake should be loud, not silently skipped).
func ParseRegistry(b []byte) ([]Source, error) {
	var f registryFile
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("sources.yaml: %w", err)
	}
	seen := map[string]bool{}
	for i := range f.Sources {
		s := &f.Sources[i]
		if err := normalizeSource(s); err != nil {
			return nil, fmt.Errorf("sources.yaml: entry %d (%s): %w", i+1, s.ID, err)
		}
		if seen[s.ID] {
			return nil, fmt.Errorf("sources.yaml: duplicate id %q", s.ID)
		}
		seen[s.ID] = true
	}
	return f.Sources, nil
}

func normalizeSource(s *Source) error {
	s.Handle = strings.TrimPrefix(strings.TrimSpace(s.Handle), "@")
	if !idRe.MatchString(s.ID) {
		return fmt.Errorf("bad id")
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("name is required")
	}
	switch s.Kind {
	case KindTGMTProto:
		if s.Handle == "" && s.ChannelID > 0 {
			break
		}
		fallthrough
	case KindTGPublic:
		if !handleRe.MatchString(s.Handle) {
			return fmt.Errorf("bad telegram handle %q", s.Handle)
		}
		if s.URL == "" {
			s.URL = "https://t.me/" + s.Handle
		}
	case KindRSS, KindHTML:
		if !strings.HasPrefix(s.URL, "https://") && !strings.HasPrefix(s.URL, "http://") {
			return fmt.Errorf("url must be http(s)")
		}
		if s.Kind == KindHTML && s.Select.Item == "" {
			return fmt.Errorf("html source needs select.item")
		}
	default:
		return fmt.Errorf("unknown kind %q", s.Kind)
	}
	if s.Lang == "" {
		s.Lang = "ru"
	}
	if s.Category == "" {
		s.Category = "other"
	}
	if !slices.Contains(Categories, s.Category) {
		return fmt.Errorf("unknown category %q", s.Category)
	}
	if s.Scope == "" {
		s.Scope = "KZ"
	}
	if s.Scope != "KZ" && s.Scope != "international" {
		return fmt.Errorf("scope must be KZ or international")
	}
	if s.Trust == "" {
		s.Trust = "medium"
	}
	if !slices.Contains([]string{"high", "medium", "low"}, s.Trust) {
		return fmt.Errorf("trust must be high, medium or low")
	}
	return nil
}
