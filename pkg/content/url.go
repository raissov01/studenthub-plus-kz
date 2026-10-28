package content

import (
	"net/netip"
	"strconv"
	"strings"
)

// parsesAsURL approximates `new URL(v)` (WHATWG URL parser) for an input that already
// starts with "https://": the host must be non-empty, free of forbidden code points and,
// when it looks numeric, a valid IPv4 address; the port must be a number up to 65535.
// IDNA (punycode) validation of non-ASCII hosts is not reproduced.
func parsesAsURL(v string) bool {
	v = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, v)
	rest := strings.TrimLeft(v[len("https:"):], `/\`)
	if i := strings.IndexAny(rest, `/\?#`); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.LastIndexByte(rest, '@'); i >= 0 {
		rest = rest[i+1:]
	}
	host, port := rest, ""
	if strings.HasPrefix(rest, "[") {
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			return false
		}
		host, port = rest[:end+1], rest[end+1:]
		if port != "" {
			if port[0] != ':' {
				return false
			}
			port = port[1:]
		}
		a, err := netip.ParseAddr(host[1 : len(host)-1])
		if err != nil || !a.Is6() || a.Zone() != "" {
			return false
		}
	} else {
		if i := strings.IndexByte(rest, ':'); i >= 0 {
			host, port = rest[:i], rest[i+1:]
		}
		if !validDomain(host) {
			return false
		}
	}
	if port != "" {
		for _, c := range port {
			if c < '0' || c > '9' {
				return false
			}
		}
		if n, err := strconv.Atoi(port); err != nil || n > 65535 {
			return false
		}
	}
	return true
}

func validDomain(host string) bool {
	if host == "" {
		return false
	}
	host = percentDecode(host)
	for _, r := range host {
		if r <= 0x20 || r == 0x7F || strings.ContainsRune("#%/:<>?@[\\]^|", r) {
			return false
		}
	}
	labels := strings.Split(host, ".")
	if len(labels) > 1 && labels[len(labels)-1] == "" {
		labels = labels[:len(labels)-1]
	}
	if last := labels[len(labels)-1]; last != "" && (isDigits(last) || isHexNumber(last)) {
		return validIPv4(labels)
	}
	return true
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

func isHexNumber(s string) bool {
	if len(s) < 2 || s[0] != '0' || (s[1] != 'x' && s[1] != 'X') {
		return false
	}
	for _, c := range strings.ToLower(s[2:]) {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func ipv4Part(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	base := 10
	switch {
	case len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X"):
		s, base = s[2:], 16
		if s == "" {
			return 0, true
		}
	case len(s) >= 2 && s[0] == '0':
		s, base = s[1:], 8
	}
	n, err := strconv.ParseUint(s, base, 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return 1 << 40, true // out of range, still a number
		}
		return 0, false
	}
	return n, true
}

func validIPv4(parts []string) bool {
	if len(parts) > 4 {
		return false
	}
	nums := make([]uint64, len(parts))
	for i, p := range parts {
		n, ok := ipv4Part(p)
		if !ok {
			return false
		}
		nums[i] = n
	}
	for _, n := range nums[:len(nums)-1] {
		if n > 255 {
			return false
		}
	}
	return nums[len(nums)-1] < uint64(1)<<(8*(5-len(nums)))
}

func percentDecode(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			n, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
			b.WriteByte(byte(n))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }
