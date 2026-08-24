package unified

import (
	"errors"
	"testing"

	trade "github.com/proven-trade/proven-trade-sdk"
)

func TestPlaceOrderRequestValidation(t *testing.T) {
	t.Parallel()

	market := Market{Base: "BTC", Quote: "USDT"}
	tests := []struct {
		name    string
		request PlaceOrderRequest
		wantErr bool
	}{
		{"limit", PlaceOrderRequest{Market: market, Side: SideBuy, Type: OrderTypeLimit, Quantity: "0.01", Price: "64000", TimeInForce: TimeInForceGTC}, false},
		{"market buy", PlaceOrderRequest{Market: market, Side: SideBuy, Type: OrderTypeMarket, QuoteAmount: "100"}, false},
		{"market sell", PlaceOrderRequest{Market: market, Side: SideSell, Type: OrderTypeMarket, Quantity: "0.01"}, false},
		{"market buy quantity", PlaceOrderRequest{Market: market, Side: SideBuy, Type: OrderTypeMarket, Quantity: "0.01", QuoteAmount: "100"}, true},
		{"market sell quote", PlaceOrderRequest{Market: market, Side: SideSell, Type: OrderTypeMarket, QuoteAmount: "100"}, true},
		{"float exponent", PlaceOrderRequest{Market: market, Side: SideSell, Type: OrderTypeMarket, Quantity: "1e-3"}, true},
		{"lowercase asset", PlaceOrderRequest{Market: Market{Base: "btc", Quote: "USDT"}, Side: SideSell, Type: OrderTypeMarket, Quantity: "1"}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := test.request.Validate()
			if test.wantErr && !errors.Is(err, trade.ErrValidation) {
				t.Fatalf("Validate() error = %v, want validation error", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestScopeAndIdentityValidation(t *testing.T) {
	t.Parallel()

	market := Market{Base: "BTC", Quote: "KRW"}
	if err := (OpenOrdersRequest{}).Validate(); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("OpenOrdersRequest.Validate() error = %v", err)
	}
	if err := (OpenOrdersRequest{AllMarkets: true}).Validate(); err != nil {
		t.Fatalf("OpenOrdersRequest.Validate() error = %v", err)
	}
	if err := (OrderRequest{Market: market, OrderID: "1", ClientOrderID: "client-1"}).Validate(); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("OrderRequest.Validate() error = %v", err)
	}
}
