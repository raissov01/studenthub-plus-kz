package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (OWASP 2024 minimum: 19 MiB, 2 passes, 1 lane). Hashes made with
// other parameters still verify and are re-hashed with these on the next sign-in.
const (
	argonMemory  = 19 * 1024 // KiB
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16

	PasswordMin = 8
	PasswordMax = 128 // runes; longer input is refused before hashing
)

// hashSlots caps concurrent hashing so a burst of logins cannot take more than
// cap*19 MiB of memory.
var hashSlots = make(chan struct{}, 4)

// argonKey runs argon2id inside a hashing slot.
func argonKey(pw, salt []byte, t, m uint32, p uint8, n uint32) []byte {
	hashSlots <- struct{}{}
	defer func() { <-hashSlots }()
	return argon2.IDKey(pw, salt, t, m, p, n)
}

// HashPassword returns a PHC-format argon2id hash: $argon2id$v=19$m=..,t=..,p=..$salt$key.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argonKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

var errBadHash = errors.New("auth: malformed password hash")

type phc struct {
	m, t uint32
	p    uint8
	salt []byte
	key  []byte
}

func parsePHC(s string) (phc, error) {
	parts := strings.Split(s, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != fmt.Sprintf("v=%d", argon2.Version) {
		return phc{}, errBadHash
	}
	var h phc
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &h.m, &h.t, &h.p); err != nil {
		return phc{}, errBadHash
	}
	if h.m < 8 || h.m > 1<<20 || h.t < 1 || h.t > 10 || h.p < 1 {
		return phc{}, errBadHash
	}
	var err error
	if h.salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil || len(h.salt) < 8 {
		return phc{}, errBadHash
	}
	if h.key, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil || len(h.key) < 16 {
		return phc{}, errBadHash
	}
	return h, nil
}

// VerifyPassword compares pw with hash in constant time. rehash is true when the hash was
// made with other parameters than the current ones.
func VerifyPassword(pw, hash string) (ok, rehash bool) {
	h, err := parsePHC(hash)
	if err != nil {
		return false, false
	}
	key := argonKey([]byte(pw), h.salt, h.t, h.m, h.p, uint32(len(h.key)))
	ok = subtle.ConstantTimeCompare(key, h.key) == 1
	return ok, ok && (h.m != argonMemory || h.t != argonTime || h.p != argonThreads || len(h.key) != argonKeyLen)
}

// dummyHash is verified against when the account does not exist, so a wrong email costs
// the same time as a wrong password.
var dummyHash = sync.OnceValue(func() string {
	h, err := HashPassword("studenthub-dummy-password-never-matches")
	if err != nil {
		panic(err)
	}
	return h
})

// Password rule violations; the code doubles as the API error code.
var (
	ErrPwShort  = errors.New("password_too_short")
	ErrPwLong   = errors.New("password_too_long")
	ErrPwCommon = errors.New("password_common")
	ErrPwEmail  = errors.New("password_like_email")
)

// CheckPassword applies the password rules: 8–128 characters, not one repeated character,
// not on the common-password list, not the email address (or its part before @).
func CheckPassword(pw, email string) error {
	n := utf8.RuneCountInString(pw)
	switch {
	case n < PasswordMin:
		return ErrPwShort
	case n > PasswordMax:
		return ErrPwLong
	}
	low := strings.ToLower(pw)
	if commonPasswords[low] || strings.Count(low, string([]rune(low)[0])) == n {
		return ErrPwCommon
	}
	if email != "" {
		local, _, _ := strings.Cut(email, "@")
		if low == strings.ToLower(email) || (len(local) >= 4 && low == strings.ToLower(local)) {
			return ErrPwEmail
		}
	}
	return nil
}

// commonPasswords: the most common leaked passwords of 8+ characters plus local ones.
// Shorter ones are already refused by the length rule.
var commonPasswords = func() map[string]bool {
	m := map[string]bool{}
	for _, p := range strings.Fields(`
		password password1 password12 password123 passw0rd p@ssw0rd p@ssword 12345678 123456789
		1234567890 0123456789 87654321 987654321 11111111 00000000 12121212 11223344 12341234
		123123123 123321123 1234512345 112233445566 qwertyui qwertyuiop qwerty12 qwerty123
		qwerty1234 1qaz2wsx 1qazxsw2 zaq12wsx qazwsxedc 1q2w3e4r 1q2w3e4r5t q1w2e3r4 q1w2e3r4t5
		qweasdzxc 123qweasd 123456qwe qwe12345 1234qwer asdf1234 zxcv1234 asdfghjk asdfghjkl
		zxcvbnm1 zxcvbnmm a1234567 aa123456 abc12345 abcd1234 abcdefgh 123456789a 1234567q
		iloveyou iloveyou1 sunshine princess football baseball welcome1 welcome123 superman
		letmein1 trustno1 dragon12 monkey12 michael1 computer internet whatever starwars
		pokemon1 master12 shadow12 jennifer jordan23 basketball 123abc123 admin123 administrator
		student student1 student123 studenthub studenthub1 university kazakhstan qazaqstan
		almaty123 astana123 nursultan karaganda 12345qwert parol123 privet123 пароль123
		йцукенгш йцукенгшщз qwerty7777 77777777 88888888 99999999 66666666 55555555 44444444
		33333333 22222222 10203040 147258369 159753456 789456123 741852963 qwertyqwerty
	`) {
		m[p] = true
	}
	return m
}()
