package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var ErrProductNotFound = errors.New("product not found")

// Currency is an ISO 4217 three-letter currency code.
type Currency string

var currencyReg = regexp.MustCompile(`^[A-Z]{3}$`)

func NewCurrency(code string) (Currency, error) {
	if !currencyReg.MatchString(code) {
		return "", fmt.Errorf("invalid currency code %q: must be three uppercase letters (ISO 4217)", code)
	}
	return Currency(code), nil
}

func MustNewCurrency(code string) Currency {
	c, err := NewCurrency(code)
	if err != nil {
		panic(err)
	}
	return c
}

func (c Currency) String() string { return string(c) }

// Price holds an amount in minor currency units (e.g. cents for USD) and a currency.
type Price struct {
	amount   int64
	currency Currency
}

func NewPrice(amount int64, currency Currency) (Price, error) {
	if amount < 0 {
		return Price{}, fmt.Errorf("price amount cannot be negative: %d", amount)
	}
	if currency == "" {
		return Price{}, errors.New("price currency cannot be empty")
	}
	return Price{amount: amount, currency: currency}, nil
}

func MustNewPrice(amount int64, currency Currency) Price {
	p, err := NewPrice(amount, currency)
	if err != nil {
		panic(err)
	}
	return p
}

// Amount returns the price amount in minor units (e.g. cents).
func (p Price) Amount() int64 {
	return p.amount
}

func (p Price) Currency() Currency {
	return p.currency
}

func (p Price) Equals(other Price) bool {
	return p.amount == other.amount && p.currency == other.currency
}

// Display formats the amount as a decimal string in major units (e.g. "2.34").
func (p Price) Display() string {
	return fmt.Sprintf("%d.%02d", p.amount/100, p.amount%100)
}

// Product is an entity that represents a single product visable in the product catalog.
// Its purchasable units are its variants; price is the product's base/display
// price (the price the catalogue was seeded with).
type Product struct {
	id          ProductID
	name        string
	description string
	price       Price
	thumbnail   string
	optionTypes []OptionType
	variants    []Variant
	categories  []Category
	attributes  []AttributeValue
	// attributeSetID is the id of the attribute set that defines which attribute
	// types this product has and in what order. Empty when the product has no
	// set (it then falls back to all attribute types).
	attributeSetID string
	// parcel is the product's physical shape, used to get real carrier
	// shipping rates. A zero parcel means the operator has not measured
	// the product yet; the shipping adapter substitutes a default.
	parcel Parcel
	// gallery holds additional product images in display order. The
	// thumbnail is NOT part of this slice: it stays the canonical
	// single image used by grids, cart lines and og:image, and the
	// product page shows it first followed by the gallery.
	gallery []string
}

var emptyProduct = Product{}

var productIDReg = regexp.MustCompile(`[\w\d\-]+`)

type ProductID string

func NewProductId(id string) (ProductID, error) {
	if !productIDReg.MatchString(id) {
		return ProductID(""), errors.New("the ID doesn't match")
	}

	return ProductID(id), nil
}

func NewProduct(id ProductID, name, description string, price Price, thumbnail string) (Product, error) {
	if name == "" {
		return emptyProduct, errors.New("the name cannot be empty")
	}

	if description == "" {
		return emptyProduct, errors.New("the description cannot be empty")
	}

	return Product{
		id:          id,
		name:        name,
		price:       price,
		description: description,
		thumbnail:   thumbnail,
	}, nil
}

// WithParcel returns a copy of the product with its physical
// measurements attached (used by the storage layer after loading, or
// when an operator saves dimensions in the admin form).
func (p Product) WithParcel(parcel Parcel) Product {
	p.parcel = parcel
	return p
}

// Parcel returns the product's physical measurements. A zero Parcel
// means the product has not been measured.
func (p Product) Parcel() Parcel { return p.parcel }

// WithGallery returns a copy of the product carrying the given gallery
// images in display order. Blank entries are dropped so a half-filled
// admin form cannot produce empty <img> tags on the storefront.
func (p Product) WithGallery(images []string) Product {
	cleaned := make([]string, 0, len(images))
	for _, img := range images {
		if trimmed := strings.TrimSpace(img); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	p.gallery = cleaned
	return p
}

// Gallery returns the additional product images in display order,
// excluding the thumbnail.
func (p Product) Gallery() []string { return p.gallery }

// Images returns every image for the product in display order: the
// thumbnail first, then the gallery. This is what a product-page
// carousel iterates over, and it means the page does not have to
// special-case the thumbnail being separate from the gallery.
//
// Gallery entries equal to the thumbnail are skipped so an operator who
// also pasted the main image into the gallery does not get it twice.
func (p Product) Images() []string {
	out := make([]string, 0, len(p.gallery)+1)
	if p.thumbnail != "" {
		out = append(out, p.thumbnail)
	}
	for _, img := range p.gallery {
		if img != p.thumbnail {
			out = append(out, img)
		}
	}
	return out
}

// HasGallery reports whether the product has more than one image worth
// showing, i.e. whether a carousel is warranted at all.
func (p Product) HasGallery() bool { return len(p.Images()) > 1 }

func (p Product) ID() ProductID {
	return p.id
}

func (p Product) Name() string {
	return p.name
}

func (p Product) Description() string {
	return p.description
}

func (p Product) Price() Price {
	return p.price
}

func (p Product) Thumbnail() string {
	return p.thumbnail
}

func (p Product) WeightGrams() int {
	return p.parcel.WeightGrams()
}

// WithCatalog returns a copy of the product with its option types and
// variants attached (used by the storage layer after loading them).
func (p Product) WithCatalog(optionTypes []OptionType, variants []Variant) Product {
	p.optionTypes = optionTypes
	p.variants = variants
	return p
}

func (p Product) OptionTypes() []OptionType { return p.optionTypes }
func (p Product) Variants() []Variant       { return p.variants }
func (p Product) HasOptions() bool          { return len(p.optionTypes) > 0 }

// WithClassification returns a copy of the product with its categories and
// attribute values attached (used by the storage layer after loading them).
func (p Product) WithClassification(categories []Category, attributes []AttributeValue) Product {
	p.categories = categories
	p.attributes = attributes
	return p
}

func (p Product) Categories() []Category       { return p.categories }
func (p Product) Attributes() []AttributeValue { return p.attributes }

// AttributeSetID returns the id of the product's attribute set (empty when it
// has none).
func (p Product) AttributeSetID() string { return p.attributeSetID }

// WithAttributeSet returns a copy of the product with its attribute set id set
// (used by the storage layer after loading it, or when assigning a set).
func (p Product) WithAttributeSet(setID string) Product {
	p.attributeSetID = setID
	return p
}

// Variant returns the variant with the given id, if it belongs to this product.
func (p Product) Variant(id string) (Variant, bool) {
	for _, v := range p.variants {
		if v.id == id {
			return v, true
		}
	}
	return Variant{}, false
}

// DefaultVariant is the first variant (used when a product has no options).
func (p Product) DefaultVariant() Variant {
	if len(p.variants) > 0 {
		return p.variants[0]
	}
	return Variant{}
}

// PriceFrom is the lowest variant price, for "from $X" display. Falls back to
// the product base price when there are no variants.
func (p Product) PriceFrom() Price {
	if len(p.variants) == 0 {
		return p.price
	}
	min := p.variants[0].price
	for _, v := range p.variants[1:] {
		if v.price.Amount() < min.Amount() {
			min = v.price
		}
	}
	return min
}

// HasPriceRange reports whether variants span more than one price (so the UI
// can show "from $X" rather than a single price).
func (p Product) HasPriceRange() bool {
	if len(p.variants) < 2 {
		return false
	}
	first := p.variants[0].price.Amount()
	for _, v := range p.variants[1:] {
		if v.price.Amount() != first {
			return true
		}
	}
	return false
}

// ResolveVariant finds the variant matching the selected option values
// (keyed by option-type name).
func (p Product) ResolveVariant(selected map[string]string) (Variant, bool) {
	for _, v := range p.variants {
		match := true
		for _, ot := range p.optionTypes {
			if v.options[ot.name] != selected[ot.name] {
				match = false
				break
			}
		}
		if match {
			return v, true
		}
	}
	return Variant{}, false
}
