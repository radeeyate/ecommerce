package adapter_test

import (
	"testing"

	"github.com/bkielbasa/go-ecommerce/backend/internal/stripe"
	"github.com/bkielbasa/go-ecommerce/backend/payments/adapter"
	"github.com/bkielbasa/go-ecommerce/backend/payments/domain"
	"github.com/matryer/is"
)

func TestTranslateStatus_RequiresAction(t *testing.T) {
	is := is.New(t)
	got, err := adapter.TranslateStatus(stripe.StatusRequiresAction)
	is.NoErr(err)
	is.Equal(got, domain.StatusPending)
}

func TestTranslateStatus_Succeeded(t *testing.T) {
	is := is.New(t)
	got, err := adapter.TranslateStatus(stripe.StatusSucceeded)
	is.NoErr(err)
	is.Equal(got, domain.StatusSucceeded)
}

func TestTranslateStatus_Failed(t *testing.T) {
	is := is.New(t)
	got, err := adapter.TranslateStatus(stripe.StatusFailed)
	is.NoErr(err)
	is.Equal(got, domain.StatusFailed)
}

func TestTranslateStatus_Unknown(t *testing.T) {
	is := is.New(t)
	_, err := adapter.TranslateStatus("some_new_stripe_status")
	is.True(err != nil)
}