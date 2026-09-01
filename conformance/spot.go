// Package conformance는 거래소 어댑터의 공통 계약을 검증하는 재사용 테스트 스위트를 제공한다.
package conformance

import (
	"context"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/unified"
)

// TestingT는 표준 testing.T와 호환되는 최소 테스트 출력 계약이다.
type TestingT interface {
	Helper()
	Fatalf(string, ...any)
}

// SpotReadScenario는 공통 현재가·잔고 적합성 검증에 필요한 fixture다.
type SpotReadScenario struct {
	Client           unified.SpotClient
	Exchange         model.ExchangeID
	Market           unified.Market
	Price            string
	NativeMarket     string
	BalanceAsset     string
	BalanceAvailable string
	Options          []trade.RequestOption
}

// SpotOrderScenario는 공통 주문 생성 적합성 검증에 필요한 fixture다.
type SpotOrderScenario struct {
	Client        unified.SpotClient
	Exchange      model.ExchangeID
	Request       unified.PlaceOrderRequest
	OrderID       string
	ClientOrderID string
	NativeMarket  string
	Status        unified.OrderStatus
	Options       []trade.RequestOption
}

// SpotMarketDataScenario는 Markets·OrderBook·RecentTrades·Candles 공통 계약 fixture다.
type SpotMarketDataScenario struct {
	Client      unified.SpotClient
	Exchange    model.ExchangeID
	Market      unified.Market
	MarketInfo  unified.MarketInfo
	OrderBook   unified.OrderBook
	Trades      []unified.PublicTrade
	Candles     []unified.Candle
	BookLimit   int
	TradeLimit  int
	CandleLimit int
	Interval    unified.CandleInterval
	Options     []trade.RequestOption
}

// SpotLifecycleScenario는 주문 조회·취소·미체결 목록 공통 계약 fixture다.
type SpotLifecycleScenario struct {
	Client            unified.SpotClient
	OrderRequest      unified.OrderRequest
	CancelRequest     unified.OrderRequest
	OpenOrdersRequest unified.OpenOrdersRequest
	Order             unified.Order
	CanceledOrder     unified.Order
	OpenOrders        []unified.Order
	Options           []trade.RequestOption
}

// RunSpotReadSuite는 어댑터의 거래소 식별자, 현재가, 원본 응답, 잔고 매핑을 검증한다.
func RunSpotReadSuite(t TestingT, scenario SpotReadScenario) {
	t.Helper()
	if scenario.Client == nil {
		t.Fatalf("공통 Spot 클라이언트가 nil이다")
	}
	if got := scenario.Client.Exchange(); got != scenario.Exchange {
		t.Fatalf("Exchange() = %q, want %q", got, scenario.Exchange)
	}
	ticker, err := scenario.Client.Ticker(
		context.Background(), unified.TickerRequest{Market: scenario.Market}, scenario.Options...,
	)
	if err != nil {
		t.Fatalf("Ticker() error = %v", err)
	}
	if ticker.Exchange != scenario.Exchange || ticker.Market != scenario.Market ||
		ticker.NativeMarket != scenario.NativeMarket || ticker.Price != scenario.Price || len(ticker.Raw) == 0 {
		t.Fatalf("Ticker() = %+v", ticker)
	}
	balances, err := scenario.Client.Balances(context.Background(), scenario.Options...)
	if err != nil {
		t.Fatalf("Balances() error = %v", err)
	}
	for _, balance := range balances {
		if balance.Asset == scenario.BalanceAsset {
			if balance.Available != scenario.BalanceAvailable {
				t.Fatalf("Balance(%s).Available = %q, want %q", balance.Asset, balance.Available, scenario.BalanceAvailable)
			}
			return
		}
	}
	t.Fatalf("Balances()에 %q 자산이 없다: %+v", scenario.BalanceAsset, balances)
}

// RunSpotOrderSuite는 공통 주문 입력과 거래소 주문 응답의 핵심 매핑을 검증한다.
func RunSpotOrderSuite(t TestingT, scenario SpotOrderScenario) {
	t.Helper()
	if scenario.Client == nil {
		t.Fatalf("공통 Spot 클라이언트가 nil이다")
	}
	order, err := scenario.Client.PlaceOrder(context.Background(), scenario.Request, scenario.Options...)
	if err != nil {
		t.Fatalf("PlaceOrder() error = %v", err)
	}
	if order.Exchange != scenario.Exchange || order.ID != scenario.OrderID ||
		order.ClientOrderID != scenario.ClientOrderID || order.Market != scenario.Request.Market ||
		order.NativeMarket != scenario.NativeMarket || len(order.Raw) == 0 {
		t.Fatalf("PlaceOrder() = %+v", order)
	}
	if scenario.Status != "" && order.Status != scenario.Status {
		t.Fatalf("PlaceOrder().Status = %q, want %q", order.Status, scenario.Status)
	}
	if scenario.Request.Type == unified.OrderTypeMarket && scenario.Request.Side == unified.SideBuy {
		if order.Quantity != "" || order.QuoteAmount != scenario.Request.QuoteAmount {
			t.Fatalf("PlaceOrder() amount mapping = quantity %q, quote amount %q", order.Quantity, order.QuoteAmount)
		}
	} else if order.Quantity != scenario.Request.Quantity {
		t.Fatalf("PlaceOrder().Quantity = %q, want %q", order.Quantity, scenario.Request.Quantity)
	}
}

// RunSpotMarketDataSuite는 공통 마켓 규칙·호가·체결·캔들 매핑을 검증한다.
func RunSpotMarketDataSuite(t TestingT, scenario SpotMarketDataScenario) {
	t.Helper()
	if scenario.Client == nil {
		t.Fatalf("공통 Spot 클라이언트가 nil이다")
	}
	markets, err := scenario.Client.Markets(context.Background(), scenario.Options...)
	if err != nil {
		t.Fatalf("Markets() error = %v", err)
	}
	var found *unified.MarketInfo
	for index := range markets {
		if markets[index].Market == scenario.Market {
			found = &markets[index]
			break
		}
	}
	if found == nil || !sameMarketInfo(*found, scenario.MarketInfo) {
		t.Fatalf("Markets() did not contain expected market: got %+v, want %+v", markets, scenario.MarketInfo)
	}
	book, err := scenario.Client.OrderBook(context.Background(), unified.OrderBookRequest{
		Market: scenario.Market, Limit: scenario.BookLimit,
	}, scenario.Options...)
	if err != nil || !sameOrderBook(book, scenario.OrderBook) {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	trades, err := scenario.Client.RecentTrades(context.Background(), unified.RecentTradesRequest{
		Market: scenario.Market, Limit: scenario.TradeLimit,
	}, scenario.Options...)
	if err != nil || !sameTrades(trades, scenario.Trades) {
		t.Fatalf("RecentTrades() = %+v, error = %v", trades, err)
	}
	candles, err := scenario.Client.Candles(context.Background(), unified.CandlesRequest{
		Market: scenario.Market, Interval: scenario.Interval, Limit: scenario.CandleLimit,
	}, scenario.Options...)
	if err != nil || !sameCandles(candles, scenario.Candles) {
		t.Fatalf("Candles() = %+v, error = %v", candles, err)
	}
}

// RunSpotLifecycleSuite는 주문 조회·취소·미체결 목록 매핑을 검증한다.
func RunSpotLifecycleSuite(t TestingT, scenario SpotLifecycleScenario) {
	t.Helper()
	if scenario.Client == nil {
		t.Fatalf("공통 Spot 클라이언트가 nil이다")
	}
	order, err := scenario.Client.Order(context.Background(), scenario.OrderRequest, scenario.Options...)
	if err != nil || !sameOrder(order, scenario.Order) {
		t.Fatalf("Order() = %+v, error = %v", order, err)
	}
	canceled, err := scenario.Client.CancelOrder(context.Background(), scenario.CancelRequest, scenario.Options...)
	if err != nil || !sameOrder(canceled, scenario.CanceledOrder) {
		t.Fatalf("CancelOrder() = %+v, error = %v", canceled, err)
	}
	open, err := scenario.Client.OpenOrders(context.Background(), scenario.OpenOrdersRequest, scenario.Options...)
	if err != nil || len(open) != len(scenario.OpenOrders) {
		t.Fatalf("OpenOrders() = %+v, error = %v", open, err)
	}
	for index := range open {
		if !sameOrder(open[index], scenario.OpenOrders[index]) {
			t.Fatalf("OpenOrders()[%d] = %+v, want %+v", index, open[index], scenario.OpenOrders[index])
		}
	}
}

func sameMarketInfo(got, want unified.MarketInfo) bool {
	return got.Exchange == want.Exchange && got.Market == want.Market &&
		got.NativeMarket == want.NativeMarket && got.Status == want.Status &&
		got.PriceIncrement == want.PriceIncrement && got.QuantityIncrement == want.QuantityIncrement &&
		got.QuoteAmountIncrement == want.QuoteAmountIncrement &&
		got.MinimumBaseQuantity == want.MinimumBaseQuantity && got.MinimumQuoteAmount == want.MinimumQuoteAmount &&
		len(got.Raw) > 0
}

func sameOrderBook(got, want unified.OrderBook) bool {
	if got.Exchange != want.Exchange || got.Market != want.Market || got.NativeMarket != want.NativeMarket ||
		got.Timestamp != want.Timestamp || len(got.Raw) == 0 || len(got.Bids) != len(want.Bids) || len(got.Asks) != len(want.Asks) {
		return false
	}
	for index := range got.Bids {
		if got.Bids[index] != want.Bids[index] {
			return false
		}
	}
	for index := range got.Asks {
		if got.Asks[index] != want.Asks[index] {
			return false
		}
	}
	return true
}

func sameTrades(got, want []unified.PublicTrade) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func sameCandles(got, want []unified.Candle) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func sameOrder(got, want unified.Order) bool {
	return got.Exchange == want.Exchange && got.ID == want.ID && got.ClientOrderID == want.ClientOrderID &&
		got.Market == want.Market && got.NativeMarket == want.NativeMarket && got.Side == want.Side &&
		got.Type == want.Type && got.Status == want.Status && got.Price == want.Price &&
		got.Quantity == want.Quantity && got.QuoteAmount == want.QuoteAmount &&
		got.ExecutedQuantity == want.ExecutedQuantity && len(got.Raw) > 0
}
