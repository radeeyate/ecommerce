// Package stripe wraps the real stripe-go SDK with the narrow shape
// the payments ACL needs. The alternative would be to import stripe-go
// directly in the ACL — this thin wrapper exists so the ACL's tests
// stay deterministic (they can substitute their own implementation of
// the small interface here) and the composition root is the only place
// that constructs the real *stripe.Client.
package stripe

import (
	"context"
	"fmt"

	stripeSDK "github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/paymentintent"
)

// PaymentIntentRequest is the input translated by the ACL.
// Source is a Stripe PaymentMethod ID (pm_xxx) collected via
// Stripe Elements on the frontend.
type PaymentIntentRequest struct {
	Amount         int64
	Currency       string
	Source         string
	IdempotencyKey string
}

// Status strings mirroring Stripe's PaymentIntent status vocabulary.
const (
	StatusRequiresAction = "requires_action"
	StatusSucceeded      = "succeeded"
	StatusFailed         = "failed"
)

// PaymentIntent is the subset of stripe.PaymentIntent fields the ACL
// reads after creating an intent.
type PaymentIntent struct {
	ID           string
	ClientSecret string
	Status       string
}

// Client is a thin wrapper around *stripeSDK.Client that exposes only
// the methods the payments ACL needs.
type Client struct {
	key string
}

// NewClient builds a real Stripe client authenticated with the
// provided secret key (sk_test_xxx or sk_live_xxx).
func NewClient(secretKey string) *Client {
	return &Client{key: secretKey}
}

// CreatePaymentIntent creates a PaymentIntent with the real Stripe
// API. It uses the configured secret key, confirms immediately, and
// maps the response status into the vocabulary the ACL expects.
func (c *Client) CreatePaymentIntent(ctx context.Context, req PaymentIntentRequest) (PaymentIntent, error) {
	if err := ctx.Err(); err != nil {
		return PaymentIntent{}, err
	}

	params := &stripeSDK.PaymentIntentParams{
		Amount:             stripeSDK.Int64(req.Amount),
		Currency:           stripeSDK.String(req.Currency),
		PaymentMethod:      stripeSDK.String(req.Source),
		ConfirmationMethod: stripeSDK.String("manual"),
		Confirm:            stripeSDK.Bool(true),
		ReturnURL:          stripeSDK.String(""), // not used in API calls; required for some redirect flows
	}
	if req.IdempotencyKey != "" {
		params.IdempotencyKey = stripeSDK.String(req.IdempotencyKey)
	}

	client := paymentintent.Client{}
	intent, err := client.New(params)
	if err != nil {
		return PaymentIntent{}, fmt.Errorf("stripe: create payment intent: %w", err)
	}

	status := translateStripeStatus(intent.Status)
	return PaymentIntent{
		ID:           intent.ID,
		ClientSecret: intent.ClientSecret,
		Status:       status,
	}, nil
}

// translateStripeStatus maps the real Stripe PaymentIntent status to
// the small vocabulary the ACL passes to the payments domain.
func translateStripeStatus(s stripeSDK.PaymentIntentStatus) string {
	switch s {
	case stripeSDK.PaymentIntentStatusSucceeded:
		return StatusSucceeded
	case stripeSDK.PaymentIntentStatusRequiresAction,
		stripeSDK.PaymentIntentStatusProcessing:
		return StatusRequiresAction
	default:
		return StatusFailed
	}
}
