// Package shippo is a client for the Shippo shipping API, used to fetch
// live carrier rates (USPS, UPS, FedEx, DHL) for a basket and
// destination.
//
// IMPORTANT: the vocabulary in this package is Shippo's, not ours. A
// "Shipment" here is Shippo's rating request object, an "address_from"
// is their field name, a "Rate" is their per-service quote with a
// decimal-string amount. Mirroring their model faithfully is the point:
// the translation into our domain vocabulary happens in the
// anti-corruption layer at checkout/adapter/shippo_acl.go, and nothing
// outside that adapter may import this package.
//
// # Why a hand-rolled client
//
// Shippo publishes no official Go SDK. Rather than take a community
// dependency for the two endpoints we need, this file speaks their REST
// API directly over net/http. The surface is deliberately tiny:
// create-shipment-and-get-rates, which is a single POST.
//
// # Units
//
// Shippo wants decimal strings with an explicit unit ("weight": "1.5",
// "mass_unit": "lb"). Our domain works in integral grams and
// millimetres to avoid float drift. The conversion happens here, at the
// boundary, using grams -> ounces and millimetres -> inches so US
// domestic rates come back in the units the carriers actually price on.
package shippo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// apiBase is Shippo's REST endpoint. Versioning is carried by the
// Shippo-API-Version header rather than the path.
const apiBase = "https://api.goshippo.com"

// apiVersion pins the Shippo API version so their future breaking
// changes cannot silently alter our rate quotes.
const apiVersion = "2018-02-08"

// ErrNoRates is returned when Shippo accepted the request but no
// carrier could service the shipment (an unserviceable destination, or
// no carrier accounts enabled in the Shippo dashboard).
var ErrNoRates = errors.New("shippo: no rates returned")

// ErrNotConfigured is returned by NewClient when no API key is
// supplied. Callers treat it as "live rates unavailable" and fall back
// to a static catalogue rather than failing checkout.
var ErrNotConfigured = errors.New("shippo: no API key configured")

// Address is Shippo's address shape. Only the fields needed for rating
// are modelled; Shippo tolerates the rest being absent.
type Address struct {
	Name    string `json:"name,omitempty"`
	Street1 string `json:"street1"`
	Street2 string `json:"street2,omitempty"`
	City    string `json:"city"`
	State   string `json:"state,omitempty"`
	Zip     string `json:"zip"`
	Country string `json:"country"`
}

// Parcel is Shippo's parcel shape. Dimensions and weight are decimal
// strings paired with explicit units, exactly as their API expects.
type Parcel struct {
	Length       string `json:"length"`
	Width        string `json:"width"`
	Height       string `json:"height"`
	DistanceUnit string `json:"distance_unit"`
	Weight       string `json:"weight"`
	MassUnit     string `json:"mass_unit"`
}

// Rate is one carrier service quote. Amount is a decimal string in
// Currency (e.g. "7.35" / "USD"); ServiceLevel carries the
// human-readable name ("Priority Mail") and a stable token.
type Rate struct {
	ObjectID     string `json:"object_id"`
	Amount       string `json:"amount"`
	Currency     string `json:"currency"`
	Provider     string `json:"provider"`
	ServiceLevel struct {
		Name  string `json:"name"`
		Token string `json:"token"`
	} `json:"servicelevel"`
	EstimatedDays int    `json:"estimated_days"`
	DurationTerms string `json:"duration_terms"`
}

// shipmentRequest is the POST body for Shippo's shipment endpoint.
// async=false makes Shippo compute rates inline instead of returning a
// pending object we would have to poll.
type shipmentRequest struct {
	AddressFrom Address  `json:"address_from"`
	AddressTo   Address  `json:"address_to"`
	Parcels     []Parcel `json:"parcels"`
	Async       bool     `json:"async"`
}

// shipmentResponse is the subset of Shippo's shipment object we read.
// Messages carries per-carrier warnings (an unsupported service, a
// missing account) that are useful in logs but not fatal.
type shipmentResponse struct {
	ObjectID string `json:"object_id"`
	Status   string `json:"status"`
	Rates    []Rate `json:"rates"`
	Messages []struct {
		Source string `json:"source"`
		Code   string `json:"code"`
		Text   string `json:"text"`
	} `json:"messages"`
}

// Client is a Shippo API client. It is safe for concurrent use.
type Client struct {
	apiKey string
	http   *http.Client
}

// NewClient builds a Shippo client. An empty apiKey yields
// ErrNotConfigured so the composition root can decide to fall back to
// static shipping rather than booting a half-configured integration.
func NewClient(apiKey string, timeout time.Duration) (*Client, error) {
	if apiKey == "" {
		return nil, ErrNotConfigured
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Client{
		apiKey: apiKey,
		http:   &http.Client{Timeout: timeout},
	}, nil
}

// Rates creates a Shippo shipment and returns every carrier rate for
// it. The call is synchronous (async=false), so a single round trip
// yields the quotes.
func (c *Client) Rates(ctx context.Context, from, to Address, parcels []Parcel) ([]Rate, error) {
	if len(parcels) == 0 {
		return nil, errors.New("shippo: at least one parcel is required")
	}

	body, err := json.Marshal(shipmentRequest{
		AddressFrom: from,
		AddressTo:   to,
		Parcels:     parcels,
		Async:       false,
	})
	if err != nil {
		return nil, fmt.Errorf("shippo: marshal shipment: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/shipments/", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("shippo: build request: %w", err)
	}
	req.Header.Set("Authorization", "ShippoToken "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Shippo-API-Version", apiVersion)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("shippo: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Cap the read so a malformed or hostile response cannot exhaust
	// memory. Rate responses are a few kilobytes at most.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("shippo: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("shippo: unexpected status %d: %s", resp.StatusCode, string(raw))
	}

	var out shipmentResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("shippo: decode response: %w", err)
	}
	if len(out.Rates) == 0 {
		return nil, ErrNoRates
	}
	return out.Rates, nil
}

// AmountMinorUnits parses a Shippo decimal amount string ("7.35") into
// minor units (735). Shippo always sends two decimal places for the
// currencies we support; a missing or single-digit fraction is padded.
func (r Rate) AmountMinorUnits() (int64, error) {
	f, err := strconv.ParseFloat(r.Amount, 64)
	if err != nil {
		return 0, fmt.Errorf("shippo: parse amount %q: %w", r.Amount, err)
	}
	// Add a half-cent before truncating so binary float representation
	// cannot round 7.35 down to 734.
	return int64(f*100 + 0.5), nil
}

// GramsToOunces converts integral grams into the decimal-string ounces
// Shippo expects. Two decimal places is enough precision for carrier
// rating and keeps the payload stable.
func GramsToOunces(grams int) string {
	const ouncesPerGram = 0.0352739619
	return strconv.FormatFloat(float64(grams)*ouncesPerGram, 'f', 2, 64)
}

// MillimetresToInches converts integral millimetres into the
// decimal-string inches Shippo expects.
func MillimetresToInches(mm int) string {
	const inchesPerMM = 0.0393700787
	return strconv.FormatFloat(float64(mm)*inchesPerMM, 'f', 2, 64)
}
