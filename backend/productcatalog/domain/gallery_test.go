package domain_test

import (
	"testing"

	"github.com/bkielbasa/go-ecommerce/backend/productcatalog/domain"
	"github.com/matryer/is"
)

func galleryProduct(t *testing.T, thumbnail string, gallery []string) domain.Product {
	t.Helper()
	price := domain.MustNewPrice(1000, domain.MustNewCurrency("USD"))
	p, err := domain.NewProduct("p1", "Product", "desc", price, thumbnail)
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	return p.WithGallery(gallery)
}

// TestGalleryDropsBlanks covers the admin form posting more slots than
// the operator filled in: empty entries must not become empty <img> tags.
func TestGalleryDropsBlanks(t *testing.T) {
	is := is.New(t)
	p := galleryProduct(t, "thumb.jpg", []string{"a.jpg", "", "   ", "b.jpg"})
	is.Equal(p.Gallery(), []string{"a.jpg", "b.jpg"})
}

// TestImagesPutsThumbnailFirst locks down the display contract the
// product page relies on: the thumbnail leads, the gallery follows in
// order.
func TestImagesPutsThumbnailFirst(t *testing.T) {
	is := is.New(t)
	p := galleryProduct(t, "thumb.jpg", []string{"a.jpg", "b.jpg"})
	is.Equal(p.Images(), []string{"thumb.jpg", "a.jpg", "b.jpg"})
}

// TestImagesDeduplicatesThumbnail covers the operator who also pasted
// the main image into the gallery — it should not appear twice.
func TestImagesDeduplicatesThumbnail(t *testing.T) {
	is := is.New(t)
	p := galleryProduct(t, "thumb.jpg", []string{"thumb.jpg", "a.jpg"})
	is.Equal(p.Images(), []string{"thumb.jpg", "a.jpg"})
}

// TestImagesWithoutThumbnail: a product with no thumbnail still renders
// its gallery rather than showing nothing.
func TestImagesWithoutThumbnail(t *testing.T) {
	is := is.New(t)
	p := galleryProduct(t, "", []string{"a.jpg"})
	is.Equal(p.Images(), []string{"a.jpg"})
}

// TestHasGalleryNeedsTwoImages: a carousel is pointless for one image.
func TestHasGalleryNeedsTwoImages(t *testing.T) {
	is := is.New(t)
	is.True(!galleryProduct(t, "thumb.jpg", nil).HasGallery())
	is.True(!galleryProduct(t, "", []string{"a.jpg"}).HasGallery())
	is.True(galleryProduct(t, "thumb.jpg", []string{"a.jpg"}).HasGallery())
}

// TestParcelRejectsNegatives guards the measurement value object against
// nonsensical input from the admin form.
func TestParcelRejectsNegatives(t *testing.T) {
	is := is.New(t)
	_, err := domain.NewParcel(-1, 10, 10, 10)
	is.True(err != nil)
	_, err = domain.NewParcel(10, -1, 10, 10)
	is.True(err != nil)
}

// TestParcelZeroMeansUnmeasured: zero is valid and signals "not
// measured", which the shipping adapter replaces with a default.
func TestParcelZeroMeansUnmeasured(t *testing.T) {
	is := is.New(t)
	p, err := domain.NewParcel(0, 0, 0, 0)
	is.NoErr(err)
	is.True(p.IsZero())
	is.True(!p.HasDimensions())
}

// TestParcelHasDimensionsNeedsAllThree: dimensional pricing needs a full
// box, not one or two edges.
func TestParcelHasDimensionsNeedsAllThree(t *testing.T) {
	is := is.New(t)
	partial, err := domain.NewParcel(500, 200, 150, 0)
	is.NoErr(err)
	is.True(!partial.HasDimensions())

	full, err := domain.NewParcel(500, 200, 150, 100)
	is.NoErr(err)
	is.True(full.HasDimensions())
}
