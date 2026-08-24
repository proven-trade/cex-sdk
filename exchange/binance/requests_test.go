package binance

import (
	"errors"
	"testing"

	trade "github.com/proven-trade/proven-trade-sdk"
)

func TestNewOrderRequestValidation(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		request NewOrderRequest
		valid   bool
	}{
		"limit": {
			request: NewOrderRequest{
				Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeLimit,
				TimeInForce: TimeInForceGTC, Quantity: "0.001", Price: "100000",
			},
			valid: true,
		},
		"market quantity": {
			request: NewOrderRequest{
				Symbol: "BTCUSDT", Side: SideSell, Type: OrderTypeMarket, Quantity: "0.001",
			},
			valid: true,
		},
		"market quote quantity": {
			request: NewOrderRequest{
				Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeMarket, QuoteOrderQuantity: "100.00",
			},
			valid: true,
		},
		"missing limit price": {
			request: NewOrderRequest{
				Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeLimit,
				TimeInForce: TimeInForceGTC, Quantity: "0.001",
			},
		},
		"float notation": {
			request: NewOrderRequest{
				Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeMarket, Quantity: "1e-3",
			},
		},
		"zero quantity": {
			request: NewOrderRequest{
				Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeMarket, Quantity: "0.000",
			},
		},
		"market with price": {
			request: NewOrderRequest{
				Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeMarket,
				Quantity: "0.001", Price: "100000",
			},
		},
		"invalid client ID": {
			request: NewOrderRequest{
				Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeMarket,
				Quantity: "0.001", ClientOrderID: "contains a space",
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := test.request.validate()
			if test.valid && err != nil {
				t.Fatalf("validate() error = %v", err)
			}
			if !test.valid && !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("validate() error = %v, want ErrValidation", err)
			}
		})
	}
}

func TestQueryAndCancelRequireOrderIdentity(t *testing.T) {
	t.Parallel()

	if err := (QueryOrderRequest{Symbol: "BTCUSDT"}).validate(); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("QueryOrderRequest.validate() error = %v, want ErrValidation", err)
	}
	if err := (CancelOrderRequest{Symbol: "BTCUSDT"}).validate(); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("CancelOrderRequest.validate() error = %v, want ErrValidation", err)
	}
}
