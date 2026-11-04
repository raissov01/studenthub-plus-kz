// Package jsx reproduces the few JavaScript string and number semantics that the ported
// Node handlers relied on (String.prototype.trim, Number(), Number#toString, UTF-16
// lengths), so the Go handlers accept and print exactly what the JS ones did.
package jsx

import (
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// IsSpace matches the JS \s class and the characters String.prototype.trim removes.
func IsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// Trim is String.prototype.trim.
func Trim(s string) string { return strings.TrimFunc(s, IsSpace) }

// Lower is String.prototype.toLowerCase for the cases where Go differs in a way that can
// turn a non-ASCII string into an ASCII one (U+0130 lowers to "i̇" in JS, "i" in Go).
func Lower(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "İ", "i̇"))
}

// UTF16Len is the JS string length of s.
func UTF16Len(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// UTF16Prefix is s.slice(0, n). A surrogate pair that would be split is dropped whole
// (JS would keep a lone high surrogate, which Go strings cannot carry).
func UTF16Prefix(s string, n int) string {
	units := 0
	for i, r := range s {
		w := 1
		if r > 0xFFFF {
			w = 2
		}
		if units+w > n {
			return s[:i]
		}
		units += w
	}
	return s
}

// ValidUTF8 replaces invalid bytes the way a UTF-8 TextDecoder does (one U+FFFD per bad byte).
func ValidUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteRune(utf8.RuneError)
		} else {
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

var decimal = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// ToNumber is Number(string): StringToNumber from the ECMAScript spec.
func ToNumber(s string) float64 {
	s = Trim(s)
	switch s {
	case "":
		return 0
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	if len(s) > 2 && s[0] == '0' {
		base := 0
		switch s[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			n, ok := new(big.Int).SetString(s[2:], base)
			if !ok || strings.ContainsAny(s[2:], "+-_") {
				return math.NaN()
			}
			f, _ := new(big.Float).SetInt(n).Float64()
			return f
		}
	}
	if !decimal.MatchString(s) {
		return math.NaN()
	}
	f, _ := strconv.ParseFloat(s, 64) // on overflow ParseFloat returns ±Inf, like JS
	return f
}

// NumberString is Number.prototype.toString() (radix 10).
func NumberString(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		return "0"
	}
	sign := ""
	if f < 0 {
		sign, f = "-", -f
	}
	e := strconv.FormatFloat(f, 'e', -1, 64) // d.ddddde±XX, shortest round-trip digits
	mant, exp, _ := strings.Cut(e, "e")
	digits := strings.Replace(mant, ".", "", 1)
	x, _ := strconv.Atoi(exp)
	k, n := len(digits), x+1
	switch {
	case k <= n && n <= 21:
		return sign + digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return sign + digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return sign + "0." + strings.Repeat("0", -n) + digits
	}
	out := digits[:1]
	if k > 1 {
		out += "." + digits[1:]
	}
	if n-1 >= 0 {
		return sign + out + "e+" + strconv.Itoa(n-1)
	}
	return sign + out + "e-" + strconv.Itoa(1-n)
}

// ToFixed2 is x.toFixed(2) for 0 <= x < 1e21 (ties round up, on the exact binary value).
func ToFixed2(x float64) string {
	if x == 0 {
		return "0.00"
	}
	exact := new(big.Float).SetPrec(0).SetFloat64(x) // exact value
	scaled := new(big.Float).SetPrec(2000).Mul(exact, big.NewFloat(100))
	scaled.Add(scaled, big.NewFloat(0.5))
	n, _ := scaled.Int(nil) // floor for non-negative values
	s := n.String()
	for len(s) < 3 {
		s = "0" + s
	}
	return s[:len(s)-2] + "." + s[len(s)-2:]
}
