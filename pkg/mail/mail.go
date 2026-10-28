// Package mail sends transactional email (sign-in codes) through the Resend HTTPS API.
//
//	RESEND_API_KEY   Resend API key (re_...); without it nothing is sent
//	MAIL_FROM        sender, e.g. "Student Hub <noreply@mail.example.kz>" (a domain verified in Resend)
//	MAIL_DEV_LOG     true = with no key, print messages (and so the codes) to the server log;
//	                 for local development only, default false
//	RESEND_API_BASE  API origin, default https://api.resend.com (tests point it at a fake)
//
// Messages go out in the background (Go), so a request that sends a code takes as long as
// one that does not: response timing does not tell whether an address has an account.
package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/raissov01/studenthub-plus-kz/pkg/config"
)

// Message is one email. Text is required; HTML is optional.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
	Tag     string // short kind for logs and Resend tags: verify_email, reset_password, ...
}

// ErrOff means neither Resend nor MAIL_DEV_LOG is configured.
var ErrOff = errors.New("mail: RESEND_API_KEY / MAIL_FROM not set")

var client = &http.Client{Timeout: 15 * time.Second}

// Configured is true when Resend can be used.
func Configured() bool {
	return config.Str("RESEND_API_KEY", "") != "" && config.Str("MAIL_FROM", "") != ""
}

// DevLog is true when messages may be written to the server log instead of being sent.
func DevLog() bool { return config.Bool("MAIL_DEV_LOG", false) }

// Enabled is true when a message would reach someone: Resend is configured, or the
// developer explicitly asked for the log fallback.
func Enabled() bool { return Configured() || DevLog() }

// Send delivers m now. Without Resend it logs m when MAIL_DEV_LOG=true and otherwise
// returns ErrOff.
func Send(ctx context.Context, m Message) error {
	if !Configured() {
		if DevLog() {
			log.Printf("mail (MAIL_DEV_LOG, not sent) to=%s subject=%q\n%s", m.To, m.Subject, m.Text)
			return nil
		}
		return ErrOff
	}
	payload := map[string]any{
		"from":    config.Str("MAIL_FROM", ""),
		"to":      []string{m.To},
		"subject": m.Subject,
		"text":    m.Text,
	}
	if m.HTML != "" {
		payload["html"] = m.HTML
	}
	if m.Tag != "" {
		payload["tags"] = []map[string]string{{"name": "kind", "value": m.Tag}}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	base := strings.TrimRight(config.Str("RESEND_API_BASE", "https://api.resend.com"), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/emails", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+config.Str("RESEND_API_KEY", ""))
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("resend: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return fmt.Errorf("resend %d: %s", res.StatusCode, strings.TrimSpace(string(msg)))
	}
	_, _ = io.Copy(io.Discard, res.Body)
	return nil
}

var pending sync.WaitGroup

// Go sends m in the background; a failure is logged with the address masked.
func Go(m Message) {
	pending.Add(1)
	go func() {
		defer pending.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := Send(ctx, m); err != nil {
			log.Printf("mail: %s to %s: %v", m.Tag, Mask(m.To), err)
		}
	}()
}

// Wait blocks until every message started with Go has finished; tests and shutdown use it.
func Wait() { pending.Wait() }

// Mask hides most of an address for logs: "ai***@example.kz".
func Mask(addr string) string {
	local, domain, ok := strings.Cut(addr, "@")
	if !ok {
		return "***"
	}
	if len(local) > 2 {
		local = local[:2]
	}
	return local + "***@" + domain
}
