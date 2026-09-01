package unified

import (
	"errors"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
)

func TestMarketInfoValidateOrderUsesExactDecimals(t *testing.T) {
	t.Parallel()
	market := Market{Base: "BTC", Quote: "USDT"}
	info := MarketInfo{
		Market: market, PriceIncrement: "0.10", QuantityIncrement: "0.001",
		MinimumBaseQuantity: "0.005", MinimumQuoteAmount: "10",
	}
	valid := PlaceOrderRequest{
		Market: market, Side: SideBuy, Type: OrderTypeLimit, TimeInForce: TimeInForceGTC,
		Price: "1000.10", Quantity: "0.010", ClientOrderID: "order-1",
	}
	if err := info.ValidateOrder(valid); err != nil {
		t.Fatalf("ValidateOrder() error = %v", err)
	}
	invalid := valid
	invalid.Price = "1000.11"
	if err := info.ValidateOrder(invalid); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("ValidateOrder() error = %v, want validation", err)
	}
	invalid = valid
	invalid.Quantity = "0.006"
	if err := info.ValidateOrder(invalid); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("ValidateOrder() error = %v, want minimum notional validation", err)
	}
}

func TestMarketInfoValidateMarketBuyQuoteMinimum(t *testing.T) {
	t.Parallel()
	market := Market{Base: "BTC", Quote: "KRW"}
	info := MarketInfo{Market: market, MinimumQuoteAmount: "5000"}
	request := PlaceOrderRequest{
		Market: market, Side: SideBuy, Type: OrderTypeMarket,
		QuoteAmount: "4999", ClientOrderID: "buy-1",
	}
	if err := info.ValidateOrder(request); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("ValidateOrder() error = %v, want validation", err)
	}
}

func TestDecimalIncrement(t *testing.T) {
	t.Parallel()
	if DecimalIncrement(0) != "1" || DecimalIncrement(3) != "0.001" || DecimalIncrement(-1) != "" {
		t.Fatalf("unexpected decimal increments")
	}
}
