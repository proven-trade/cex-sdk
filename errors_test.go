package trade

import (
	"errors"
	"fmt"
	"testing"

	"github.com/proven-trade/proven-trade-sdk/model"
)

func TestAPIErrorSupportsCommonAndOriginalErrors(t *testing.T) {
	t.Parallel()

	original := errors.New("socket closed")
	err := &APIError{
		Category: ErrorNetwork,
		Exchange: model.ExchangeBinance,
		Cause:    original,
	}
	if !errors.Is(err, ErrNetwork) {
		t.Fatal("errors.Is(err, ErrNetwork) = false")
	}
	if !errors.Is(err, original) {
		t.Fatal("errors.Is(err, original) = false")
	}
	var apiError *APIError
	if !errors.As(fmt.Errorf("wrapped: %w", err), &apiError) {
		t.Fatal("errors.As() = false")
	}
}

func TestAPIErrorStringDoesNotInventDetails(t *testing.T) {
	t.Parallel()

	err := (&APIError{
		Category:        ErrorValidation,
		Exchange:        model.ExchangeBinance,
		ExchangeCode:    "-1121",
		ExchangeMessage: "Invalid symbol.",
	}).Error()
	want := "binance request failed with code -1121: Invalid symbol."
	if err != want {
		t.Fatalf("Error() = %q, want %q", err, want)
	}
}
