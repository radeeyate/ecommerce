// shippo_acl.go is the Anti-Corruption Layer between the checkout
// bounded context and the Shippo carrier-rates API.
//
// WHAT THE ACL TRANSLATES.
//
//  1. SHAPE. checkout expresses a rating request as
//     (domain.Address, domain.Parcel). Shippo expresses it as
//     (address_from, address_to, []parcel) where every measurement is a
//     decimal string with an explicit unit. The ACL builds the origin
//     address from operator configuration, converts our integral
//     grams/millimetres into Shippo's ounces/inches strings, and maps
//     the destination field-by-field.
//
//  2. VOCABULARY. Shippo returns a flat list of per-service Rates, each
//     with a provider ("USPS"), a servicelevel name ("Priority Mail")
//     and a decimal-string amount ("7.35"). checkout wants
//     domain.RateQuote with a stable form code, a display label and an
//     integral minor-unit cost. The ACL derives the code by slugging
//     carrier + service token, so re-rating the same basket produces
//     the same code and a customer's selection survives a page refresh.
//
//  3. FAILURE. Any Shippo problem — unconfigured key, HTTP error,
//     timeout, no serviceable carrier — collapses into
//     domain.ErrRatesUnavailable. checkout reacts by falling back to
//     the static shipping catalogue, so a carrier outage degrades the
//     storefront instead of breaking it. The underlying cause is
//     wrapped for logs but never leaks into control flow.
//
//  4. POLICY. The configured handling markup is applied here, at the
//     boundary, so every quote the domain sees is already the price the
//     customer pays. Keeping it in the adapter means the domain never
//     has to know a markup exists.
//
// This file is the ONLY thing in the repository that imports
// internal/shippo. If a future change adds a second importer, the
// import graph stops enforcing the ACL.
package adapter

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/bkielbasa/go-ecommerce/backend/checkout/domain"
	"github.com/bkielbasa/go-ecommerce/backend/internal/shippo"
)

// ShippoConfig is the operator-supplied configuration the rate adapter
// needs: where parcels ship from, what to assume for unmeasured
// products, and how much handling markup to add.
type ShippoConfig struct {
	// Origin is the warehouse address parcels ship from. Carriers
	// cannot rate without it.
	Origin shippo.Address
	// DefaultParcel is used when the basket's own measurements are
	// missing or zero, so an unmeasured product cannot make checkout
	// unrateable.
	DefaultParcel domain.Parcel
	// MarkupPercent is added to every carrier rate to cover packaging
	// and handling. 0 passes the carrier price through unchanged.
	MarkupPercent float64
}

// ShippoRateProvider satisfies domain.RateProvider by calling the
// Shippo API. It holds no mutable state and is safe for concurrent use.
type ShippoRateProvider struct {
	client *shippo.Client
	cfg    ShippoConfig
}

// NewShippoRateProvider builds the ACL around a Shippo client.
func NewShippoRateProvider(client *shippo.Client, cfg ShippoConfig) *ShippoRateProvider {
	return &ShippoRateProvider{client: client, cfg: cfg}
}

// Rates translates the checkout-domain request into a Shippo shipment,
// calls the API, and translates every returned carrier rate back into
// domain vocabulary. Every failure mode becomes
// domain.ErrRatesUnavailable so the caller's fallback path is
// unambiguous.
func (p *ShippoRateProvider) Rates(ctx context.Context, destination domain.Address, parcel domain.Parcel) ([]domain.RateQuote, error) {
	if p == nil || p.client == nil {
		return nil, domain.ErrRatesUnavailable
	}

	// Substitute the configured default for any measurement the
	// catalogue is missing. Sending zeros to a carrier is rejected
	// outright, so this is what keeps an unmeasured product from
	// breaking checkout.
	effective := p.withDefaults(parcel)

	rates, err := p.client.Rates(ctx, p.cfg.Origin, toShippoAddress(destination), []shippo.Parcel{{
		Length:       shippo.MillimetresToInches(effective.LengthMM),
		Width:        shippo.MillimetresToInches(effective.WidthMM),
		Height:       shippo.MillimetresToInches(effective.HeightMM),
		DistanceUnit: "in",
		Weight:       shippo.GramsToOunces(effective.WeightGrams),
		MassUnit:     "oz",
	}})
	if err != nil {
		// Collapse every cause into the domain sentinel; the original
		// error is wrapped so logs keep the detail.
		return nil, fmt.Errorf("%w: %v", domain.ErrRatesUnavailable, err)
	}

	quotes := make([]domain.RateQuote, 0, len(rates))
	for _, r := range rates {
		minor, convErr := r.AmountMinorUnits()
		if convErr != nil {
			// One unparseable amount should not discard every other
			// carrier's valid quote.
			continue
		}
		quotes = append(quotes, domain.RateQuote{
			Code:           rateCode(r),
			Label:          rateLabel(r),
			Carrier:        r.Provider,
			CostMinorUnits: p.applyMarkup(minor),
			Currency:       r.Currency,
			EstimatedDays:  r.EstimatedDays,
			ProviderRateID: r.ObjectID,
		})
	}
	if len(quotes) == 0 {
		return nil, domain.ErrRatesUnavailable
	}
	return quotes, nil
}

// withDefaults fills in any zero measurement from the configured
// default parcel. A basket with a known weight but no box still gets
// the default box, which is the common case for operators who record
// weight but not dimensions.
func (p *ShippoRateProvider) withDefaults(parcel domain.Parcel) domain.Parcel {
	d := p.cfg.DefaultParcel
	if parcel.WeightGrams <= 0 {
		parcel.WeightGrams = d.WeightGrams
	}
	if parcel.LengthMM <= 0 {
		parcel.LengthMM = d.LengthMM
	}
	if parcel.WidthMM <= 0 {
		parcel.WidthMM = d.WidthMM
	}
	if parcel.HeightMM <= 0 {
		parcel.HeightMM = d.HeightMM
	}
	return parcel
}

// applyMarkup adds the configured handling percentage, rounding to the
// nearest minor unit. A non-positive markup is a no-op.
func (p *ShippoRateProvider) applyMarkup(minor int64) int64 {
	if p.cfg.MarkupPercent <= 0 {
		return minor
	}
	return int64(math.Round(float64(minor) * (1 + p.cfg.MarkupPercent/100.0)))
}

// toShippoAddress maps the checkout domain's Address onto Shippo's.
//
// The domain Address has no dedicated state field — it was modelled for
// a storefront that only needed street/city/zip/country. US carriers
// can rate on zip alone, so leaving State empty is safe for domestic
// shipments; Shippo derives it from the zip.
func toShippoAddress(a domain.Address) shippo.Address {
	return shippo.Address{
		Name:    a.Name(),
		Street1: a.Street1(),
		Street2: a.Street2(),
		City:    a.City(),
		Zip:     a.Zip(),
		Country: normaliseCountry(a.Country()),
	}
}

// normaliseCountry maps the free-text country the checkout form
// collects onto the ISO 3166-1 alpha-2 code carriers require. The
// storefront asks for "United States" in a plain text input, so a bare
// pass-through would be rejected by Shippo.
//
// Unrecognised values are upper-cased and truncated to two characters,
// which correctly handles a customer who already typed "US" or "CA" and
// degrades predictably otherwise.
func normaliseCountry(in string) string {
	trimmed := strings.TrimSpace(in)
	if trimmed == "" {
		return "US"
	}
	switch strings.ToLower(trimmed) {
	case "us", "usa", "united states", "united states of america":
		return "US"
	case "ca", "canada":
		return "CA"
	case "gb", "uk", "united kingdom", "great britain":
		return "GB"
	case "au", "australia":
		return "AU"
	case "de", "germany", "deutschland":
		return "DE"
	case "fr", "france":
		return "FR"
	case "mx", "mexico":
		return "MX"
	}
	upper := strings.ToUpper(trimmed)
	if len(upper) > 2 {
		upper = upper[:2]
	}
	return upper
}

// rateCode derives a stable form code from a Shippo rate. Using the
// carrier plus the service token (rather than Shippo's per-request
// object_id) means the same basket re-rated later produces the same
// code, so a customer's radio selection survives a page re-render.
func rateCode(r shippo.Rate) string {
	carrier := slug(r.Provider)
	service := slug(r.ServiceLevel.Token)
	if service == "" {
		service = slug(r.ServiceLevel.Name)
	}
	if carrier == "" && service == "" {
		return "carrier-rate"
	}
	if service == "" {
		return carrier
	}
	return carrier + "-" + service
}

// rateLabel builds the customer-facing name, preferring
// "Carrier Service" and degrading gracefully when either is missing.
func rateLabel(r shippo.Rate) string {
	switch {
	case r.Provider != "" && r.ServiceLevel.Name != "":
		return r.Provider + " " + r.ServiceLevel.Name
	case r.ServiceLevel.Name != "":
		return r.ServiceLevel.Name
	case r.Provider != "":
		return r.Provider
	default:
		return "Carrier rate"
	}
}

// slug lowercases and hyphenates, keeping only alphanumerics. Used to
// build form-safe rate codes out of carrier and service names.
func slug(in string) string {
	var b strings.Builder
	lastHyphen := true
	for _, r := range strings.ToLower(in) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteRune('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// compile-time assertion that the adapter satisfies the domain port.
var _ domain.RateProvider = (*ShippoRateProvider)(nil)
