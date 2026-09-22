package domain

import (
	"context"
	"errors"
)

// ErrRatesUnavailable signals that live rates could not be fetched —
// the carrier API is down, unconfigured, or returned nothing
// serviceable. Callers fall back to the static shipping catalogue
// rather than blocking checkout.
var ErrRatesUnavailable = errors.New("live shipping rates unavailable")

// Parcel is the physical shape of the basket being shipped, expressed
// in the checkout context's own terms. It is deliberately a local value
// type rather than a reference to productcatalog.Parcel: checkout must
// not depend on the catalogue's types, so the composition root
// translates between them.
//
// Units are integral to avoid float drift: grams and millimetres.
type Parcel struct {
	WeightGrams int
	LengthMM    int
	WidthMM     int
	HeightMM    int
}

// RateQuote is one shippable option returned by a live rate provider.
// It carries everything the checkout UI needs to render a selectable
// shipping choice, plus the opaque provider reference needed later to
// buy the label.
//
// Code is a stable identifier the form posts back; the application
// service resolves it against the quotes it fetched so a customer
// cannot smuggle in an arbitrary price.
type RateQuote struct {
	// Code identifies this quote in the checkout form. It is derived
	// from the carrier and service so it survives a re-fetch of the
	// same basket ("usps-priority", "fedex-2day").
	Code string
	// Label is the customer-facing name ("USPS Priority Mail").
	Label string
	// Carrier is the provider name ("USPS", "FedEx").
	Carrier string
	// CostMinorUnits is the price in minor units, already including any
	// configured handling markup.
	CostMinorUnits int64
	// Currency is the ISO 4217 code the cost is denominated in.
	Currency string
	// EstimatedDays is the carrier's transit estimate; 0 when unknown.
	EstimatedDays int
	// ProviderRateID is the carrier provider's own opaque reference for
	// this quote, retained so a label can be purchased against the
	// exact rate the customer was shown. Treated as a black box.
	ProviderRateID string
}

// AsShippingMethod converts a live quote into the ShippingMethod value
// the Order aggregate already understands. This keeps the aggregate,
// the pricing service and the persistence layer unchanged: a live rate
// is just another shipping method whose cost happens to have come from
// a carrier rather than a hardcoded table.
//
// Live rates always require an address — you cannot rate a parcel
// without a destination — so requiresAddress is unconditionally true.
func (q RateQuote) AsShippingMethod() ShippingMethod {
	return ShippingMethod{
		code:            q.Code,
		label:           q.Label,
		cost:            q.CostMinorUnits,
		requiresAddress: true,
	}
}

// RateProvider is the port the checkout context depends on for live
// carrier rates. The concrete implementation
// (checkout/adapter/shippo_acl.go) is the only thing that imports the
// carrier SDK; this interface keeps the carrier's vocabulary out of the
// domain entirely.
//
// Implementations MUST return ErrRatesUnavailable (not a bare error)
// when they cannot produce quotes, so callers can distinguish
// "fall back to static methods" from a genuine bug.
type RateProvider interface {
	Rates(ctx context.Context, destination Address, parcel Parcel) ([]RateQuote, error)
}
