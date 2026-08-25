package smoke

import (
	"context"
	"errors"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/transport"
	"github.com/proven-trade/proven-trade-sdk/unified"
)

type fakeSpotTradeClient struct {
	fakeSpotReadClient

	tradeMu        sync.Mutex
	placeRequest   unified.PlaceOrderRequest
	placedClientID string
	placeError     error
	queryErrors    []error
	queryIndex     int
	cancelError    error
	finalExecuted  string
	afterPlace     func()
	canceled       bool
	cancelCtxErr   error
	cancelRequest  unified.OrderRequest
}

func (client *fakeSpotTradeClient) PlaceOrder(
	_ context.Context,
	request unified.PlaceOrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := client.record("place_order", options); err != nil {
		return unified.Order{}, err
	}
	client.tradeMu.Lock()
	client.placeRequest = request
	err := client.placeError
	afterPlace := client.afterPlace
	placedClientID := client.placedClientID
	client.tradeMu.Unlock()
	if afterPlace != nil {
		afterPlace()
	}
	if err != nil {
		return unified.Order{}, err
	}
	if placedClientID == "" {
		placedClientID = request.ClientOrderID
	}
	return unified.Order{
		Exchange: model.ExchangeBinance, ID: "order-1", ClientOrderID: placedClientID,
		Market: request.Market, NativeMarket: "BTCUSDT", Status: unified.OrderStatusNew,
		ExecutedQuantity: "0",
	}, nil
}

func (client *fakeSpotTradeClient) Order(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := client.record("query_order", options); err != nil {
		return unified.Order{}, err
	}
	if err := ctx.Err(); err != nil {
		return unified.Order{}, err
	}
	client.tradeMu.Lock()
	index := client.queryIndex
	client.queryIndex++
	var queryErr error
	if index < len(client.queryErrors) {
		queryErr = client.queryErrors[index]
	}
	finalExecuted := client.finalExecuted
	canceled := client.canceled
	client.tradeMu.Unlock()
	if queryErr != nil {
		return unified.Order{}, queryErr
	}
	status := unified.OrderStatusNew
	executed := "0"
	if canceled {
		status = unified.OrderStatusCanceled
		if finalExecuted != "" {
			executed = finalExecuted
		}
	}
	return unified.Order{
		Exchange: model.ExchangeBinance, ID: "order-1", ClientOrderID: "smoke-order-1",
		Market: request.Market, NativeMarket: "BTCUSDT", Status: status,
		ExecutedQuantity: executed,
	}, nil
}

func (client *fakeSpotTradeClient) CancelOrder(
	ctx context.Context,
	request unified.OrderRequest,
	options ...trade.RequestOption,
) (unified.Order, error) {
	if err := client.record("cancel_order", options); err != nil {
		return unified.Order{}, err
	}
	client.tradeMu.Lock()
	client.cancelCtxErr = ctx.Err()
	client.cancelRequest = request
	err := client.cancelError
	if err == nil && client.cancelCtxErr == nil {
		client.canceled = true
	}
	client.tradeMu.Unlock()
	if ctx.Err() != nil {
		return unified.Order{}, ctx.Err()
	}
	if err != nil {
		return unified.Order{}, err
	}
	return unified.Order{
		Exchange: model.ExchangeBinance, ID: "order-1", ClientOrderID: "smoke-order-1",
		Market: request.Market, NativeMarket: "BTCUSDT", Status: unified.OrderStatusCanceled,
		ExecutedQuantity: "0",
	}, nil
}

func validTradeConfig(client unified.SpotClient) SpotTradeConfig {
	expectedIP := net.ParseIP("203.0.113.10")
	return SpotTradeConfig{
		Client: client,
		EgressVerifier: &fakeEgressVerifier{check: transport.PublicIPCheck{
			RouteID: "route-b", LocalSourceIP: net.ParseIP("10.0.10.22"),
			ExpectedPublicIP: expectedIP, ObservedPublicIP: append(net.IP(nil), expectedIP...),
			MatchesExpected: true,
		}},
		Market: unified.Market{Base: "BTC", Quote: "USDT"}, EgressRouteID: "route-b",
		CheckTimeout: 2 * time.Second, Side: unified.SideBuy,
		Price: "63000", Quantity: "0.001", MaxNotional: "100",
		ClientOrderID: "smoke-order-1", Confirmation: RealOrderConfirmation,
	}
}

func TestSpotTradeRunnerCompletesPostOnlyLifecycle(t *testing.T) {
	t.Parallel()

	client := &fakeSpotTradeClient{}
	runner, err := NewSpotTradeRunner(validTradeConfig(client))
	if err != nil {
		t.Fatalf("NewSpotTradeRunner() error = %v", err)
	}
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !report.Passed || !report.CleanupAttempted || !report.CancellationConfirmed ||
		len(report.Checks) != 6 {
		t.Fatalf("report = %+v", report)
	}
	for _, check := range report.Checks {
		if check.Status != CheckPassed || check.Failure != nil {
			t.Fatalf("check = %+v", check)
		}
	}
	client.tradeMu.Lock()
	placed := client.placeRequest
	client.tradeMu.Unlock()
	if placed.Type != unified.OrderTypeLimit || placed.TimeInForce != unified.TimeInForcePostOnly ||
		placed.Side != unified.SideBuy || placed.Price != "63000" ||
		placed.Quantity != "0.001" || placed.ClientOrderID != "smoke-order-1" {
		t.Fatalf("place request = %+v", placed)
	}
	client.mu.Lock()
	calls := slices.Clone(client.calls)
	options := slices.Clone(client.options)
	client.mu.Unlock()
	if !slices.Equal(calls, []string{
		"order_book", "place_order", "query_order", "cancel_order", "query_order",
	}) {
		t.Fatalf("calls = %v", calls)
	}
	for _, option := range options {
		if option.EgressRouteID != "route-b" || option.Timeout != 2*time.Second {
			t.Fatalf("request options = %+v", option)
		}
	}
}

func TestSpotTradeRunnerRejectsCrossingPriceBeforeMutation(t *testing.T) {
	t.Parallel()

	client := &fakeSpotTradeClient{}
	config := validTradeConfig(client)
	config.Price = "65000"
	config.MaxNotional = "1000"
	runner, err := NewSpotTradeRunner(config)
	if err != nil {
		t.Fatalf("NewSpotTradeRunner() error = %v", err)
	}
	report, err := runner.Run(context.Background())
	if !errors.Is(err, ErrTradeSmokeFailed) || report.Passed || report.CleanupAttempted ||
		report.Checks[1].Failure == nil ||
		report.Checks[1].Failure.Reason != "buy_price_crosses_best_ask" {
		t.Fatalf("report = %+v, error = %v", report, err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if !slices.Equal(client.calls, []string{"order_book"}) {
		t.Fatalf("calls = %v", client.calls)
	}
	for _, check := range report.Checks[2:] {
		if check.Status != CheckSkipped {
			t.Fatalf("mutation check = %+v", check)
		}
	}
}

func TestSpotTradeRunnerReconcilesUnknownPlaceWithoutRetry(t *testing.T) {
	t.Parallel()

	client := &fakeSpotTradeClient{placeError: &trade.APIError{
		Category: trade.ErrorUnknownExecutionState, Exchange: model.ExchangeBinance,
	}}
	runner, err := NewSpotTradeRunner(validTradeConfig(client))
	if err != nil {
		t.Fatalf("NewSpotTradeRunner() error = %v", err)
	}
	report, err := runner.Run(context.Background())
	if !errors.Is(err, ErrTradeSmokeFailed) || report.Passed ||
		!report.CleanupAttempted || !report.CancellationConfirmed {
		t.Fatalf("report = %+v, error = %v", report, err)
	}
	if report.Checks[2].Failure == nil ||
		report.Checks[2].Failure.Category != trade.ErrorUnknownExecutionState {
		t.Fatalf("place check = %+v", report.Checks[2])
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if !slices.Equal(client.calls, []string{
		"order_book", "place_order", "query_order", "cancel_order", "query_order",
	}) {
		t.Fatalf("calls = %v", client.calls)
	}
}

func TestSpotTradeRunnerCancelsAfterQueryFailure(t *testing.T) {
	t.Parallel()

	client := &fakeSpotTradeClient{queryErrors: []error{trade.ErrNetwork}}
	runner, err := NewSpotTradeRunner(validTradeConfig(client))
	if err != nil {
		t.Fatalf("NewSpotTradeRunner() error = %v", err)
	}
	report, err := runner.Run(context.Background())
	if !errors.Is(err, ErrTradeSmokeFailed) || report.Passed ||
		!report.CleanupAttempted || !report.CancellationConfirmed {
		t.Fatalf("report = %+v, error = %v", report, err)
	}
	if report.Checks[3].Failure == nil ||
		report.Checks[3].Failure.Category != trade.ErrorNetwork {
		t.Fatalf("query check = %+v", report.Checks[3])
	}
}

func TestSpotTradeRunnerKeepsClientIdentityWhenPlaceResponseIsUnexpected(t *testing.T) {
	t.Parallel()

	client := &fakeSpotTradeClient{
		placedClientID: "unexpected-order", queryErrors: []error{trade.ErrNetwork},
	}
	runner, err := NewSpotTradeRunner(validTradeConfig(client))
	if err != nil {
		t.Fatalf("NewSpotTradeRunner() error = %v", err)
	}
	report, err := runner.Run(context.Background())
	if !errors.Is(err, ErrTradeSmokeFailed) || report.Passed || !report.CleanupAttempted {
		t.Fatalf("report = %+v, error = %v", report, err)
	}
	client.tradeMu.Lock()
	cancelRequest := client.cancelRequest
	client.tradeMu.Unlock()
	if cancelRequest.OrderID != "" || cancelRequest.ClientOrderID != "smoke-order-1" {
		t.Fatalf("cancel request = %+v", cancelRequest)
	}
}

func TestSpotTradeRunnerUsesIndependentCleanupAfterContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeSpotTradeClient{afterPlace: cancel}
	runner, err := NewSpotTradeRunner(validTradeConfig(client))
	if err != nil {
		t.Fatalf("NewSpotTradeRunner() error = %v", err)
	}
	report, err := runner.Run(ctx)
	if !errors.Is(err, ErrTradeSmokeFailed) || report.Passed ||
		!report.CleanupAttempted || !report.CancellationConfirmed {
		t.Fatalf("report = %+v, error = %v", report, err)
	}
	client.tradeMu.Lock()
	cancelCtxErr := client.cancelCtxErr
	client.tradeMu.Unlock()
	if cancelCtxErr != nil {
		t.Fatalf("cancel context error = %v", cancelCtxErr)
	}
}

func TestSpotTradeRunnerDetectsExecutedOrder(t *testing.T) {
	t.Parallel()

	client := &fakeSpotTradeClient{finalExecuted: "0.0001"}
	runner, err := NewSpotTradeRunner(validTradeConfig(client))
	if err != nil {
		t.Fatalf("NewSpotTradeRunner() error = %v", err)
	}
	report, err := runner.Run(context.Background())
	if !errors.Is(err, ErrTradeSmokeFailed) || report.Passed || report.CancellationConfirmed ||
		report.Checks[5].Failure == nil || report.Checks[5].Failure.Reason != "order_executed" {
		t.Fatalf("report = %+v, error = %v", report, err)
	}
}

func TestNewSpotTradeRunnerEnforcesSafetyConfiguration(t *testing.T) {
	t.Parallel()

	base := validTradeConfig(&fakeSpotTradeClient{})
	tests := []struct {
		name   string
		mutate func(*SpotTradeConfig)
	}{
		{name: "동의 없음", mutate: func(config *SpotTradeConfig) { config.Confirmation = "" }},
		{name: "금액 상한 초과", mutate: func(config *SpotTradeConfig) { config.MaxNotional = "1" }},
		{name: "시장가 방향 누락", mutate: func(config *SpotTradeConfig) { config.Side = "" }},
		{name: "수량 오류", mutate: func(config *SpotTradeConfig) { config.Quantity = "0" }},
		{name: "가격 오류", mutate: func(config *SpotTradeConfig) { config.Price = "-1" }},
		{name: "주문 ID 누락", mutate: func(config *SpotTradeConfig) { config.ClientOrderID = "" }},
		{name: "경로 누락", mutate: func(config *SpotTradeConfig) { config.EgressRouteID = " " }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := base
			test.mutate(&config)
			if _, err := NewSpotTradeRunner(config); err == nil {
				t.Fatal("NewSpotTradeRunner() error = nil")
			}
		})
	}
}
