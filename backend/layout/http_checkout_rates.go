package layout

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strings"

	cartDomain "github.com/bkielbasa/go-ecommerce/backend/cart/domain"
	checkoutDomain "github.com/bkielbasa/go-ecommerce/backend/checkout/domain"
)

// basketParcel derives the physical shape of a cart: total weight plus a
// single box big enough to hold everything.
//
// # The box estimate
//
// Real multi-item packing is a bin-packing problem, and solving it
// properly needs the operator's actual box inventory. This approximates
// it in two steps:
//
//  1. Sum the VOLUME of every unit in the basket, then take the cube
//     root to get the edge of an equivalent cube. Ten mugs therefore
//     quote for a box roughly 2.15x the edge of one mug, not the same
//     box as a single mug.
//
//  2. Raise each dimension to at least the largest single item's
//     corresponding dimension. A yoga mat in the basket forces a box
//     long enough for the mat, however small the cube-root estimate is.
//
// A packing-efficiency factor pads the volume to account for the fact
// that items do not tessellate perfectly and need void fill. Without it
// every multi-item order would be quoted slightly under.
//
// This errs toward quoting one larger box rather than several small
// ones, which is the safer default for a storefront: carriers price
// dimensional weight generously on single parcels, and an operator who
// genuinely ships bulky multi-item orders wants manual rating anyway.
//
// Cart lines reference variants, so each line's weight comes from its
// variant with the parent product's box as the dimensional fallback — a
// large T-shirt weighs more than a small one but ships in the same
// polybag.
//
// Lookup failures are tolerated: a line whose variant has vanished from
// the catalogue contributes nothing rather than failing the whole quote.
// The configured default parcel in the Shippo adapter covers the case
// where nothing could be measured at all.
func (handler httpHandler) basketParcel(ctx context.Context, cart *cartDomain.Cart) checkoutDomain.Parcel {
	// packingEfficiency is the fraction of a box's interior that usable
	// product actually occupies. 0.75 assumes a quarter of the volume is
	// void fill and packaging, which is a common rule of thumb for
	// mixed-goods parcels.
	const packingEfficiency = 0.75

	var (
		totalWeightGrams int
		totalVolumeMM3   float64
		maxLengthMM      int
		maxWidthMM       int
		maxHeightMM      int
	)

	for _, item := range cart.Items() {
		product, variant, err := handler.catalogSrv.FindVariant(ctx, item.Product().ID())
		if err != nil {
			continue
		}

		// Variant weight wins when set; otherwise inherit the parent
		// product's.
		unitWeight := variant.WeightGrams()
		if unitWeight <= 0 {
			unitWeight = product.Parcel().WeightGrams()
		}
		totalWeightGrams += unitWeight * item.Quantity()

		box := product.Parcel()
		if !box.HasDimensions() {
			// Unmeasured product: it still contributes weight above, and
			// the adapter's default parcel covers the volume.
			continue
		}

		totalVolumeMM3 += float64(box.LengthMM()) * float64(box.WidthMM()) * float64(box.HeightMM()) * float64(item.Quantity())

		if box.LengthMM() > maxLengthMM {
			maxLengthMM = box.LengthMM()
		}
		if box.WidthMM() > maxWidthMM {
			maxWidthMM = box.WidthMM()
		}
		if box.HeightMM() > maxHeightMM {
			maxHeightMM = box.HeightMM()
		}
	}

	parcel := checkoutDomain.Parcel{
		WeightGrams: totalWeightGrams,
		LengthMM:    maxLengthMM,
		WidthMM:     maxWidthMM,
		HeightMM:    maxHeightMM,
	}

	if totalVolumeMM3 <= 0 {
		return parcel
	}

	// Equivalent-cube edge for the padded total volume.
	edge := int(math.Cbrt(totalVolumeMM3/packingEfficiency) + 0.5)

	// The final box is the cube estimate, floored by the largest single
	// item in each axis so a long product always gets a long box.
	if edge > parcel.LengthMM {
		parcel.LengthMM = edge
	}
	if edge > parcel.WidthMM {
		parcel.WidthMM = edge
	}
	if edge > parcel.HeightMM {
		parcel.HeightMM = edge
	}
	return parcel
}

// shippingOptions resolves the shipping choices to offer for a basket
// and destination. It is the single place that decides between live
// carrier rates and the static catalogue, so the checkout page and the
// order-placing handler cannot disagree about what was on offer.
//
// The address is required for live rates — you cannot rate a parcel
// without a destination — so an incomplete address short-circuits to
// the static catalogue. That is also what makes the initial checkout
// page render fast: no carrier call happens until the customer has
// actually entered where they live.
//
// Any rate-provider failure degrades to the static catalogue. A carrier
// outage must not be able to block checkout.
func (handler httpHandler) shippingOptions(ctx context.Context, cart *cartDomain.Cart, dest checkoutDomain.Address) ([]checkoutDomain.ShippingMethod, bool) {
	static := checkoutDomain.ShippingMethods()

	if handler.rateProvider == nil || dest.IsZero() {
		return static, false
	}

	quotes, err := handler.rateProvider.Rates(ctx, dest, handler.basketParcel(ctx, cart))
	if err != nil {
		if !errors.Is(err, checkoutDomain.ErrRatesUnavailable) {
			handler.logger.WithError(err).Warn("unexpected shipping rate error")
		}
		return static, false
	}

	// Live rates all require an address, so pickup — which does not —
	// is preserved from the static catalogue. Customers who want to
	// collect in person should not lose that option just because the
	// carrier API answered.
	methods := make([]checkoutDomain.ShippingMethod, 0, len(quotes)+1)
	for _, q := range quotes {
		methods = append(methods, q.AsShippingMethod())
	}
	for _, m := range static {
		if !m.RequiresAddress() {
			methods = append(methods, m)
		}
	}
	return methods, true
}

// addressFromForm builds a destination address out of whatever the
// checkout form has so far. It does NOT validate: partially-filled
// addresses are expected while the customer is still typing, and the
// caller decides whether the result is complete enough to rate against.
func addressFromForm(r *http.Request) checkoutDomain.Address {
	return checkoutDomain.RebuildAddress(
		strings.TrimSpace(r.FormValue("ship_name")),
		strings.TrimSpace(r.FormValue("ship_street1")),
		strings.TrimSpace(r.FormValue("ship_street2")),
		strings.TrimSpace(r.FormValue("ship_city")),
		strings.TrimSpace(r.FormValue("ship_zip")),
		strings.TrimSpace(r.FormValue("ship_country")),
	)
}

// rateableAddress reports whether an address has enough detail to ask a
// carrier for a quote. Carriers rate on locality, not on who lives
// there, so the name and street-line-2 are irrelevant here.
func rateableAddress(a checkoutDomain.Address) bool {
	return a.Street1() != "" && a.City() != "" && a.Zip() != "" && a.Country() != ""
}

// resolveShippingMethod turns the shipping-method code posted by the
// checkout form into the ShippingMethod the order will be charged for.
//
// A code can name either a static catalogue entry (flat, pickup,
// courier) or a live carrier rate. Static codes resolve locally. Live
// codes are resolved by RE-FETCHING rates for the same basket and
// destination and matching on code — never by trusting a price from the
// form, which a customer could otherwise set to zero.
//
// If a live code cannot be matched on re-fetch (the carrier changed its
// offering mid-checkout, or the API is now down), the caller gets an
// error and the customer is asked to choose again with a fresh list.
// Silently substituting a different price would be worse: they would be
// charged for something they did not pick.
func (handler httpHandler) resolveShippingMethod(ctx context.Context, sessID, code string, dest checkoutDomain.Address) (checkoutDomain.ShippingMethod, error) {
	if code == "" {
		return checkoutDomain.ShippingMethod{}, checkoutDomain.ErrInvalidShippingMethod
	}

	// Static catalogue first — it is the cheap path and covers pickup,
	// which has no address and therefore no carrier quote.
	if m, err := checkoutDomain.ShippingMethodByCode(code); err == nil {
		return m, nil
	}

	if handler.rateProvider == nil || !rateableAddress(dest) {
		return checkoutDomain.ShippingMethod{}, checkoutDomain.ErrInvalidShippingMethod
	}

	cart, err := handler.cartSrv.Get(ctx, sessID)
	if err != nil || cart == nil || len(cart.Items()) == 0 {
		return checkoutDomain.ShippingMethod{}, checkoutDomain.ErrInvalidShippingMethod
	}

	quotes, err := handler.rateProvider.Rates(ctx, dest, handler.basketParcel(ctx, cart))
	if err != nil {
		return checkoutDomain.ShippingMethod{}, checkoutDomain.ErrInvalidShippingMethod
	}
	for _, q := range quotes {
		if q.Code == code {
			return q.AsShippingMethod(), nil
		}
	}
	return checkoutDomain.ShippingMethod{}, checkoutDomain.ErrInvalidShippingMethod
}

// ShippingRates is the HTMX endpoint that re-renders the shipping-method
// radio list whenever the customer changes their address. It returns a
// fragment, not a page: the checkout template swaps it into the
// shipping-method fieldset.
//
// Carrier calls take one to three seconds, which is why this is a
// separate request rather than part of the checkout page render — the
// page paints immediately with static methods and upgrades to live
// rates once an address exists.
func (handler httpHandler) ShippingRates(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	sessID := cartIDFromCookies(w, r)
	cart, err := handler.cartSrv.Get(r.Context(), sessID)
	if err != nil || cart == nil || len(cart.Items()) == 0 {
		// An empty cart has nothing to ship; render the static list so
		// the fieldset is never blank.
		handler.renderShippingMethods(w, r, checkoutDomain.ShippingMethods(), false)
		return
	}

	dest := addressFromForm(r)
	if !rateableAddress(dest) {
		handler.renderShippingMethods(w, r, checkoutDomain.ShippingMethods(), false)
		return
	}

	methods, live := handler.shippingOptions(r.Context(), cart, dest)
	handler.renderShippingMethods(w, r, methods, live)
}

// renderShippingMethods renders the shipping-method fragment shared by
// the full checkout page and the HTMX rate refresh.
func (handler httpHandler) renderShippingMethods(w http.ResponseWriter, r *http.Request, methods []checkoutDomain.ShippingMethod, live bool) {
	handler.renderPartialTemplate(w, r, "checkout/shipping_methods", map[string]any{
		"ShippingMethods": methods,
		"LiveRates":       live,
	})
}
