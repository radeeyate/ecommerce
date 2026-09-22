package domain

import "errors"

// ErrInvalidParcel is returned when a parcel's measurements are
// nonsensical (negative values).
var ErrInvalidParcel = errors.New("invalid parcel dimensions")

// Parcel is the physical shape of something shippable: mass plus a
// bounding box. It lives in the productcatalog domain because the
// catalogue is what knows how big and heavy each item is; the checkout
// and shipping contexts consume it as an opaque measurement.
//
// Units are deliberately integral to avoid floating-point drift in
// carrier rate requests: grams for weight, millimetres for each
// dimension. Carriers want ounces and inches (US) or kilos and
// centimetres (metric); the conversion belongs in the carrier adapter,
// not here.
//
// A zero Parcel means "not measured". Callers that need real
// measurements (the Shippo rate adapter) substitute a configured
// default rather than sending zeros to a carrier, which would be
// rejected.
type Parcel struct {
	weightGrams int
	lengthMM    int
	widthMM     int
	heightMM    int
}

// NewParcel validates and constructs a Parcel. All values must be
// non-negative; zero is allowed and means "unmeasured".
func NewParcel(weightGrams, lengthMM, widthMM, heightMM int) (Parcel, error) {
	if weightGrams < 0 || lengthMM < 0 || widthMM < 0 || heightMM < 0 {
		return Parcel{}, ErrInvalidParcel
	}
	return Parcel{
		weightGrams: weightGrams,
		lengthMM:    lengthMM,
		widthMM:     widthMM,
		heightMM:    heightMM,
	}, nil
}

// RebuildParcel reconstructs a Parcel from storage without validating.
// Storage rows were validated on write.
func RebuildParcel(weightGrams, lengthMM, widthMM, heightMM int) Parcel {
	return Parcel{
		weightGrams: weightGrams,
		lengthMM:    lengthMM,
		widthMM:     widthMM,
		heightMM:    heightMM,
	}
}

func (p Parcel) WeightGrams() int { return p.weightGrams }
func (p Parcel) LengthMM() int    { return p.lengthMM }
func (p Parcel) WidthMM() int     { return p.widthMM }
func (p Parcel) HeightMM() int    { return p.heightMM }

// IsZero reports whether nothing has been measured.
func (p Parcel) IsZero() bool { return p == Parcel{} }

// HasDimensions reports whether all three box dimensions are set. A
// carrier can rate on weight alone for some services, but dimensional
// pricing needs the full box.
func (p Parcel) HasDimensions() bool {
	return p.lengthMM > 0 && p.widthMM > 0 && p.heightMM > 0
}
