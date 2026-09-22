// stripe_acl.go is the Anti-Corruption Layer between the payments
// bounded context and the real Stripe provider.
//
// WHAT THE ACL TRANSLATES.
//
//  1. SHAPE. payments/app expresses a charge attempt as a
//     ChargeRequest (amount, currency, source). Stripe expresses
//     the same thing as a PaymentIntentParams — same conceptual
//     operation, different field set and SDK types. The ACL maps
//     one to the other.
//
//  2. VOCABULARY. Stripe PaymentIntent statuses are
//     "requires_action" / "succeeded" / "failed"; payments-domain
//     statuses are pending / succeeded / failed. The mapping is:
//
//     stripe.StatusRequiresAction -> domain.StatusPending
//     stripe.StatusSucceeded      -> domain.StatusSucceeded
//     stripe.StatusFailed         -> domain.StatusFailed
//
//     "requires_action" -> "pending" is the load-bearing
//     translation: the provider's "SCA challenge in flight" state is,
//     from our point of view, simply "we don't know yet". A
//     payment_intent.succeeded webhook later flips the same Charge
//     into `succeeded` via payments.Service.MarkSucceeded.
//
//  3. IDENTITY. Stripe gives us back a PaymentIntent.ID ("pi_xxx");
//     we copy it into ChargeResult.ProviderRef. That opaque token is
//     the only piece of provider state that ever reaches the payments
//     domain, and it is treated as a black box (never parsed, never
//     inspected — only used for join lookups when webhooks arrive).
//
// This file is the ONLY thing in the repository that imports the
// stripe-go SDK (other than stripe-go's own tests). If a future
// change adds a second importer, the import graph stops enforcing
// the ACL — and the demonstration breaks.
package adapter

import (
	"context"
	"fmt"

	"github.com/bkielbasa/go-ecommerce/backend/internal/stripe"
	"github.com/bkielbasa/go-ecommerce/backend/payments/app"
	"github.com/bkielbasa/go-ecommerce/backend/payments/domain"
)

// Provider is the ACL onto the real Stripe provider. It satisfies
// app.Provider so the application service has zero compile-time
// knowledge of the Stripe SDK types.
type Provider struct {
	client *stripe.Client
}

// NewProvider builds the ACL around a real Stripe client.
func NewProvider(client *stripe.Client) *Provider {
	return &Provider{client: client}
}

// Charge translates the payments-domain ChargeRequest into a
// Stripe PaymentIntentParams, invokes the API, then translates the
// response status back into the payments-domain vocabulary. Errors
// from the provider propagate verbatim — the service wraps them
// with payments-domain context.
func (p *Provider) Charge(ctx context.Context, req app.ChargeRequest) (app.ChargeResult, error) {
	intent, err := p.client.CreatePaymentIntent(ctx, stripe.PaymentIntentRequest{
		Amount:         req.Amount,
		Currency:       req.Currency,
		Source:         req.Source,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return app.ChargeResult{}, fmt.Errorf("stripe acl: %w", err)
	}
	status, err := TranslateStatus(intent.Status)
	if err != nil {
		return app.ChargeResult{}, err
	}
	return app.ChargeResult{Status: status, ProviderRef: intent.ID}, nil
}

// TranslateStatus is the documented mapping from provider vocabulary
// to domain vocabulary. Any value the provider may legitimately
// return MUST appear here; an unknown value is an integration bug
// (the provider added a new state the ACL hasn't been taught about)
// and surfaces as an error so we never silently treat it as
// "succeeded".
func TranslateStatus(providerStatus string) (domain.Status, error) {
	switch providerStatus {
	case stripe.StatusRequiresAction:
		// Provider says "SCA challenge required"; we say "not
		// terminal yet". A later webhook will move it on.
		return domain.StatusPending, nil
	case stripe.StatusSucceeded:
		return domain.StatusSucceeded, nil
	case stripe.StatusFailed:
		return domain.StatusFailed, nil
	default:
		return "", fmt.Errorf("stripe acl: unknown provider status %q", providerStatus)
	}
}
