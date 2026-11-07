// Package payments is the only place that knows how a café order is paid. The café code talks to a
// Provider; today that is the unofficial kaspi-pos sidecar (kaspi.go, services/kaspi-pos) or pay-at-pickup
// (pickup.go). Moving to an official Kaspi API later means writing one more Provider, nothing else.
//
// Money never passes through SIVA: a Kaspi invoice is issued in the café's own name with the
// café's cashier session, the student pays the café, and we only read the payment status.
package payments

import (
	"context"
	"errors"
	"time"
)

// Session is a café's Kaspi cashier session as the sidecar issued it. VtokenSecret is already encrypted by
// the sidecar (TOKEN_SECRET_KEY); the whole triple is stored encrypted again (session.go).
type Session struct {
	TokenSN      string `json:"tokenSN"`
	VtokenSecret string `json:"vtokenSecret"`
	ProfileID    string `json:"profileId"`
}

// Merchant is the café a call acts for. Session is nil for pay-at-pickup.
type Merchant struct {
	CafeID  int64
	Session *Session
}

// Invoice is a payable request in the café's name. Amounts are in tiyn.
type Invoice struct {
	OperationID string
	// PayURL opens the Kaspi app on a phone (pay.kaspi.kz/...).
	PayURL string
	// QR is what to encode in a QR for a desktop screen (qr.kaspi.kz/..., also scannable by other banks' apps).
	QR        string
	Amount    int64
	ExpiresAt *time.Time
}

// State is a provider status reduced to what an order needs.
type State string

const (
	StatePending State = "pending"
	StatePaid    State = "paid"
	StateFailed  State = "failed"  // declined, cancelled by the payer
	StateExpired State = "expired" // QR timed out unpaid
)

type Status struct {
	OperationID string
	State       State
	Raw         string // provider status, for the audit trail
	Amount      int64  // tiyn, 0 when not reported
}

type Provider interface {
	Name() string
	CreateInvoice(ctx context.Context, m Merchant, orderID int64, amount int64) (*Invoice, error)
	Status(ctx context.Context, m Merchant, operationID string) (*Status, error)
	// Refund returns money from the café's account to the payer. ErrRefundNotAllowed means the cashier role
	// may not do it: the café returns the money by hand.
	Refund(ctx context.Context, m Merchant, operationID string, amount int64) error
}

var (
	ErrNotConfigured    = errors.New("payments: provider not configured")
	ErrNoSession        = errors.New("payments: café cashier is not logged in")
	ErrSessionExpired   = errors.New("payments: Kaspi rejected the cashier session")
	ErrInvalid          = errors.New("payments: invalid request")
	ErrUpstream         = errors.New("payments: provider error")
	ErrTimeout          = errors.New("payments: provider timeout")
	ErrRefundNotAllowed = errors.New("payments: refund not allowed for this cashier")
	ErrNotSupported     = errors.New("payments: not supported by this provider")
)

// Tenge converts a tiyn amount to whole tenge for Kaspi. Menu prices are whole tenge (a CHECK in the
// migration), so a fractional amount here is a programming error.
func Tenge(tiyn int64) (int64, error) {
	if tiyn <= 0 || tiyn%100 != 0 {
		return 0, ErrInvalid
	}
	return tiyn / 100, nil
}
