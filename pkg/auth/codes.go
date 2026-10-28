package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raissov01/studenthub-plus-kz/pkg/db"
)

// One-time email codes (auth_codes): 6 digits, 15 minutes, 5 wrong attempts, one live code
// per user and purpose. Only an HMAC of the code is stored, keyed with a key derived from
// SESSION_SECRET, so a database leak alone does not reveal live codes.
const (
	CodeTTL         = 15 * time.Minute
	CodeMaxAttempts = 5
	CodeResendAfter = 60 * time.Second // a new code for the same purpose no sooner than this
)

// Code purposes, as stored in auth_codes.purpose.
const (
	PurposeVerify = "verify_email"
	PurposeReset  = "reset_password"
	PurposeChange = "change_email"
)

func derivedKey(label string) ([]byte, error) {
	key, err := secret()
	if err != nil {
		return nil, err
	}
	return hmacSHA256(key, []byte("studenthub/"+label+"/v1")), nil
}

func codeMAC(purpose string, userID int64, email, code string) ([]byte, error) {
	key, err := derivedKey("auth-code")
	if err != nil {
		return nil, err
	}
	return hmacSHA256(key, []byte(purpose+"|"+strconv.FormatInt(userID, 10)+"|"+email+"|"+code)), nil
}

// emailKey is a stable pseudonym for an address in rate_limits keys, so the throttle
// table never holds addresses.
func emailKey(email string) string {
	key, err := derivedKey("email-key")
	if err != nil {
		sum := sha256.Sum256([]byte(email))
		return hex.EncodeToString(sum[:12])
	}
	return hex.EncodeToString(hmacSHA256(key, []byte(email))[:12])
}

func randomCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// validCode is true for exactly six ASCII digits.
func validCode(s string) bool {
	if len(s) != 6 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// issueCode stores a new code for (user, purpose), replacing the previous one, and returns
// it. It returns "" without a new code while the previous one is younger than
// CodeResendAfter. pwHash is kept with change_email codes only.
func issueCode(ctx context.Context, tx pgx.Tx, userID int64, purpose, email string, pwHash *string) (string, error) {
	var fresh bool
	err := tx.QueryRow(ctx, `select exists (select 1 from auth_codes where user_id = $1 and purpose = $2
		and email = $3 and created_at > now() - make_interval(secs => $4))`,
		userID, purpose, email, CodeResendAfter.Seconds()).Scan(&fresh)
	if err != nil || fresh {
		return "", err
	}
	code, err := randomCode()
	if err != nil {
		return "", err
	}
	mac, err := codeMAC(purpose, userID, email, code)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `insert into auth_codes (user_id, purpose, email, code_hash, password_hash, max_attempts, expires_at)
		values ($1, $2, $3, $4, $5, $6, now() + make_interval(secs => $7))
		on conflict (user_id, purpose) do update set email = excluded.email, code_hash = excluded.code_hash,
			password_hash = excluded.password_hash, attempts = 0, max_attempts = excluded.max_attempts,
			expires_at = excluded.expires_at, created_at = now()`,
		userID, purpose, email, mac, pwHash, CodeMaxAttempts, CodeTTL.Seconds())
	if err != nil {
		return "", err
	}
	return code, nil
}

// errBadCode covers a wrong, expired, used-up or missing code alike: the API answers all of
// them the same way, so it never tells whether a code (and so an account) exists.
var errBadCode = errors.New("auth: invalid code")

// usedCode is what a matching code carried.
type usedCode struct {
	Email        string
	PasswordHash *string
}

// checkCode consumes the (user, purpose) code if code matches and extraOK is true (the
// caller's other check, e.g. the password, passed). Every failed try counts against
// max_attempts; the last allowed failure deletes the code. The attempt counter is
// committed in its own transaction, whatever the caller does next.
func checkCode(ctx context.Context, userID int64, purpose, code string, extraOK bool) (*usedCode, error) {
	var out *usedCode
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		var (
			id            int64
			email         string
			mac           []byte
			pw            *string
			attempts, max int
			expires       time.Time
		)
		err := tx.QueryRow(ctx, `select id, email, code_hash, password_hash, attempts, max_attempts, expires_at
			from auth_codes where user_id = $1 and purpose = $2 for update`, userID, purpose).
			Scan(&id, &email, &mac, &pw, &attempts, &max, &expires)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if !time.Now().Before(expires) || attempts >= max {
			_, err := tx.Exec(ctx, `delete from auth_codes where id = $1`, id)
			return err
		}
		want, err := codeMAC(purpose, userID, email, code)
		if err != nil {
			return err
		}
		if hmac.Equal(want, mac) && extraOK {
			out = &usedCode{Email: email, PasswordHash: pw}
			_, err := tx.Exec(ctx, `delete from auth_codes where id = $1`, id)
			return err
		}
		if attempts+1 >= max {
			_, err = tx.Exec(ctx, `delete from auth_codes where id = $1`, id)
		} else {
			_, err = tx.Exec(ctx, `update auth_codes set attempts = attempts + 1 where id = $1`, id)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, errBadCode
	}
	return out, nil
}
