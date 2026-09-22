package adapter_test

import (
	"testing"

	"github.com/bkielbasa/go-ecommerce/backend/checkout/adapter"
	"github.com/bkielbasa/go-ecommerce/backend/checkout/domain"
	"github.com/bkielbasa/go-ecommerce/backend/internal/shippo"
	"github.com/matryer/is"
)

// TestRatesUnavailableWhenUnconfigured locks down the fallback contract:
// a provider with no client must report ErrRatesUnavailable rather than
// panicking, because the composition root builds one whenever Shippo is
// only partly configured.
func TestRatesUnavailableWhenUnconfigured(t *testing.T) {
	is := is.New(t)
	p := adapter.NewShippoRateProvider(nil, adapter.ShippoConfig{})
	_, err := p.Rates(t.Context(), domain.Address{}, domain.Parcel{})
	is.True(err != nil)
	is.True(isRatesUnavailable(err))
}

func isRatesUnavailable(err error) bool {
	return err != nil && err.Error() == domain.ErrRatesUnavailable.Error()
}

// TestAmountMinorUnits covers the decimal-string to minor-unit
// conversion. The 7.35 case is the one that matters: naive
// float64*100 truncation yields 734.
func TestAmountMinorUnits(t *testing.T) {
	is := is.New(t)
	for _, c := range []struct {
		in   string
		want int64
	}{
		{"7.35", 735},
		{"0.00", 0},
		{"12.99", 1299},
		{"100", 10000},
		{"8.10", 810},
	} {
		r := shippo.Rate{Amount: c.in}
		got, err := r.AmountMinorUnits()
		is.NoErr(err)
		is.Equal(got, c.want)
	}
}

func TestAmountMinorUnits_Invalid(t *testing.T) {
	is := is.New(t)
	r := shippo.Rate{Amount: "not-a-number"}
	_, err := r.AmountMinorUnits()
	is.True(err != nil)
}

// TestUnitConversions pins the gram/millimetre to ounce/inch conversions
// the Shippo payload depends on. A regression here would silently
// misquote every shipment.
func TestUnitConversions(t *testing.T) {
	is := is.New(t)
	// 1000 g is 35.27 oz.
	is.Equal(shippo.GramsToOunces(1000), "35.27")
	// 254 mm is exactly 10 inches.
	is.Equal(shippo.MillimetresToInches(254), "10.00")
	is.Equal(shippo.GramsToOunces(0), "0.00")
}

// TestNewClientRequiresKey documents that an unconfigured Shippo yields
// a typed error the composition root can branch on, rather than a client
// that fails later at request time.
func TestNewClientRequiresKey(t *testing.T) {
	is := is.New(t)
	_, err := shippo.NewClient("", 0)
	is.True(err == shippo.ErrNotConfigured)
}

// TestAsShippingMethod verifies a live quote converts into the
// ShippingMethod the Order aggregate already understands, carrying the
// carrier's price through unchanged.
func TestAsShippingMethod(t *testing.T) {
	is := is.New(t)
	q := domain.RateQuote{
		Code:           "usps-priority",
		Label:          "USPS Priority Mail",
		CostMinorUnits: 735,
	}
	m := q.AsShippingMethod()
	is.Equal(m.Code(), "usps-priority")
	is.Equal(m.Label(), "USPS Priority Mail")
	is.Equal(m.Cost(), int64(735))
	// Live rates are always address-based; pickup comes from the static
	// catalogue instead.
	is.True(m.RequiresAddress())
}
