package bitget

import (
	"errors"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
)

func TestPlaceOrderRequestValidation(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		request PlaceOrderRequest
		valid   bool
	}{
		"Spot limit": {
			request: PlaceOrderRequest{
				Category: CategorySpot, Symbol: "BTCUSDT", Side: SideBuy,
				OrderType: OrderTypeLimit, Quantity: "0.001", Price: "64000", TimeInForce: TimeInForceGTC,
			},
			valid: true,
		},
		"Spot market": {
			request: PlaceOrderRequest{
				Category: CategorySpot, Symbol: "BTCUSDT", Side: SideBuy,
				OrderType: OrderTypeMarket, Quantity: "100",
			},
			valid: true,
		},
		"Futures hedge": {
			request: PlaceOrderRequest{
				Category: CategoryUSDTFutures, Symbol: "BTCUSDT", Side: SideSell,
				OrderType: OrderTypeLimit, Quantity: "0.01", Price: "64000",
				PositionSide: PositionSideLong, ReduceOnly: "yes",
			},
			valid: true,
		},
		"limit without price": {
			request: PlaceOrderRequest{
				Category: CategorySpot, Symbol: "BTCUSDT", Side: SideBuy,
				OrderType: OrderTypeLimit, Quantity: "0.001",
			},
		},
		"market with price": {
			request: PlaceOrderRequest{
				Category: CategorySpot, Symbol: "BTCUSDT", Side: SideBuy,
				OrderType: OrderTypeMarket, Quantity: "100", Price: "64000",
			},
		},
		"Spot position side": {
			request: PlaceOrderRequest{
				Category: CategorySpot, Symbol: "BTCUSDT", Side: SideBuy,
				OrderType: OrderTypeMarket, Quantity: "100", PositionSide: PositionSideLong,
			},
		},
		"scientific quantity": {
			request: PlaceOrderRequest{
				Category: CategoryUSDTFutures, Symbol: "BTCUSDT", Side: SideBuy,
				OrderType: OrderTypeMarket, Quantity: "1e-3",
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

func TestOrderQueryRangeValidation(t *testing.T) {
	t.Parallel()

	start := time.UnixMilli(1_700_000_000_000)
	tooLate := start.Add(31 * 24 * time.Hour)
	request := OrderHistoryRequest{
		Category:  CategorySpot,
		StartTime: &start,
		EndTime:   &tooLate,
	}
	if err := request.validate(); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("validate() error = %v, want ErrValidation", err)
	}
	if err := (OpenOrdersRequest{}).validate(); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("OpenOrdersRequest.validate() error = %v, want ErrValidation", err)
	}
}

func TestHistoryCandlesRequestValidation(t *testing.T) {
	t.Parallel()

	start := time.UnixMilli(1_609_459_200_000)
	withinRange := start.Add(90 * 24 * time.Hour)
	beyondRange := withinRange.Add(time.Millisecond)
	base := CandlesRequest{Category: CategorySpot, Symbol: "BTCUSDT", Interval: Candle1Minute}

	valid := base
	valid.StartTime, valid.EndTime, valid.Limit = &start, &withinRange, 100
	if err := valid.validateHistory(); err != nil {
		t.Fatalf("90-day history request error = %v", err)
	}
	tooWide := valid
	tooWide.EndTime = &beyondRange
	if err := tooWide.validateHistory(); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("wide history request error = %v, want ErrValidation", err)
	}
	tooMany := base
	tooMany.Limit = 101
	if err := tooMany.validateHistory(); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("history limit 101 error = %v, want ErrValidation", err)
	}
	if err := tooMany.validate(); err != nil {
		t.Fatalf("recent candle limit 101 error = %v", err)
	}
}
