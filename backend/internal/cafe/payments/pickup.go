package payments

import "context"

// Pickup is pay-at-the-counter (cash or the café's own Kaspi QR at the till). No money moves through us,
// so there is nothing to create, check or refund.
type Pickup struct{}

func (Pickup) Name() string { return "pickup" }

func (Pickup) CreateInvoice(context.Context, Merchant, int64, int64) (*Invoice, error) {
	return nil, ErrNotSupported
}

func (Pickup) Status(_ context.Context, _ Merchant, operationID string) (*Status, error) {
	return nil, ErrNotSupported
}

// Refund of an unpaid pickup order is a no-op.
func (Pickup) Refund(context.Context, Merchant, string, int64) error { return nil }
