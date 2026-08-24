// Package conformance는 거래소 어댑터의 공통 계약을 검증하는 재사용 테스트 스위트를 제공한다.
package conformance

import (
	"context"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/unified"
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
	Options       []trade.RequestOption
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
}
