package smoke

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/transport"
	"github.com/proven-trade/proven-trade-sdk/unified"
)

type fakeEgressVerifier struct {
	mu       sync.Mutex
	routeID  transport.EgressRouteID
	endpoint string
	check    transport.PublicIPCheck
	err      error
}

func (verifier *fakeEgressVerifier) VerifyPublicIP(
	_ context.Context,
	routeID transport.EgressRouteID,
	endpoint string,
) (transport.PublicIPCheck, error) {
	verifier.mu.Lock()
	defer verifier.mu.Unlock()
	verifier.routeID = routeID
	verifier.endpoint = endpoint
	return verifier.check, verifier.err
}

type fakeSpotReadClient struct {
	mu      sync.Mutex
	options []trade.RequestOptions
	calls   []string
	errors  map[string]error
}

func (client *fakeSpotReadClient) Exchange() model.ExchangeID { return model.ExchangeBinance }

func (client *fakeSpotReadClient) record(name string, options []trade.RequestOption) error {
	resolved, err := trade.ResolveRequestOptions("", options...)
	if err != nil {
		return err
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	client.calls = append(client.calls, name)
	client.options = append(client.options, resolved)
	return client.errors[name]
}

func (client *fakeSpotReadClient) Markets(
	_ context.Context,
	options ...trade.RequestOption,
) ([]unified.MarketInfo, error) {
	if err := client.record("markets", options); err != nil {
		return nil, err
	}
	return []unified.MarketInfo{{
		Exchange: model.ExchangeBinance, Market: unified.Market{Base: "BTC", Quote: "USDT"},
		NativeMarket: "BTCUSDT", Status: "TRADING",
	}}, nil
}

func (client *fakeSpotReadClient) Ticker(
	_ context.Context,
	request unified.TickerRequest,
	options ...trade.RequestOption,
) (unified.Ticker, error) {
	if err := client.record("ticker", options); err != nil {
		return unified.Ticker{}, err
	}
	return unified.Ticker{
		Exchange: model.ExchangeBinance, Market: request.Market,
		NativeMarket: "BTCUSDT", Price: "64000.1",
	}, nil
}

func (client *fakeSpotReadClient) OrderBook(
	_ context.Context,
	request unified.OrderBookRequest,
	options ...trade.RequestOption,
) (unified.OrderBook, error) {
	if err := client.record("order_book", options); err != nil {
		return unified.OrderBook{}, err
	}
	return unified.OrderBook{
		Exchange: model.ExchangeBinance, Market: request.Market, NativeMarket: "BTCUSDT",
		Bids: []unified.BookLevel{{Price: "64000", Quantity: "1"}},
		Asks: []unified.BookLevel{{Price: "64001", Quantity: "2"}},
	}, nil
}

func (client *fakeSpotReadClient) RecentTrades(
	_ context.Context,
	_ unified.RecentTradesRequest,
	options ...trade.RequestOption,
) ([]unified.PublicTrade, error) {
	if err := client.record("recent_trades", options); err != nil {
		return nil, err
	}
	return []unified.PublicTrade{{
		ID: "1", Price: "64000", Quantity: "0.1", Side: unified.SideBuy,
		Timestamp: 1_700_000_000_000,
	}}, nil
}

func (client *fakeSpotReadClient) Candles(
	_ context.Context,
	_ unified.CandlesRequest,
	options ...trade.RequestOption,
) ([]unified.Candle, error) {
	if err := client.record("candles", options); err != nil {
		return nil, err
	}
	return []unified.Candle{{
		StartTime: 1_700_000_000_000, Open: "63000", High: "65000",
		Low: "62000", Close: "64000", Volume: "10.5",
	}}, nil
}

func (client *fakeSpotReadClient) Balances(
	_ context.Context,
	options ...trade.RequestOption,
) ([]unified.Balance, error) {
	if err := client.record("balances", options); err != nil {
		return nil, err
	}
	return []unified.Balance{{Asset: "USDT", Available: "1000", Locked: "0"}}, nil
}

func (*fakeSpotReadClient) PlaceOrder(
	context.Context,
	unified.PlaceOrderRequest,
	...trade.RequestOption,
) (unified.Order, error) {
	return unified.Order{}, trade.ErrUnsupportedCapability
}

func (*fakeSpotReadClient) Order(
	context.Context,
	unified.OrderRequest,
	...trade.RequestOption,
) (unified.Order, error) {
	return unified.Order{}, trade.ErrUnsupportedCapability
}

func (*fakeSpotReadClient) CancelOrder(
	context.Context,
	unified.OrderRequest,
	...trade.RequestOption,
) (unified.Order, error) {
	return unified.Order{}, trade.ErrUnsupportedCapability
}

func (*fakeSpotReadClient) OpenOrders(
	context.Context,
	unified.OpenOrdersRequest,
	...trade.RequestOption,
) ([]unified.Order, error) {
	return nil, trade.ErrUnsupportedCapability
}

func TestSpotReadRunnerPassesAllChecksOnOneRoute(t *testing.T) {
	t.Parallel()

	expectedIP := net.ParseIP("203.0.113.10")
	verifier := &fakeEgressVerifier{check: transport.PublicIPCheck{
		RouteID: "route-b", LocalSourceIP: net.ParseIP("10.0.10.22"),
		ExpectedPublicIP: expectedIP, ObservedPublicIP: append(net.IP(nil), expectedIP...),
		MatchesExpected: true,
	}}
	client := &fakeSpotReadClient{}
	runner, err := NewSpotReadRunner(SpotReadConfig{
		Client: client, EgressVerifier: verifier,
		Market: unified.Market{Base: "BTC", Quote: "USDT"}, EgressRouteID: "route-b",
		PublicIPEndpoint: "https://ip.example.test", CheckTimeout: 2 * time.Second,
		IncludeBalances: true,
	})
	if err != nil {
		t.Fatalf("NewSpotReadRunner() error = %v", err)
	}
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !report.Passed || report.Exchange != model.ExchangeBinance ||
		report.Version != ReadReportVersion || report.EgressRouteID != "route-b" || len(report.Checks) != 7 {
		t.Fatalf("report = %+v", report)
	}
	for _, check := range report.Checks {
		if check.Status != CheckPassed || check.Failure != nil || check.CompletedAt.Before(check.StartedAt) {
			t.Fatalf("check = %+v", check)
		}
	}
	if report.Checks[0].Evidence.ObservedPublicIP != "203.0.113.10" ||
		report.Checks[0].Evidence.LocalSourceIP != "10.0.10.22" ||
		report.Checks[1].Evidence.NativeMarket != "BTCUSDT" {
		t.Fatalf("checks = %+v", report.Checks)
	}
	verifier.mu.Lock()
	gotRoute, gotEndpoint := verifier.routeID, verifier.endpoint
	verifier.mu.Unlock()
	if gotRoute != "route-b" || gotEndpoint != "https://ip.example.test" {
		t.Fatalf("egress verification = %q, %q", gotRoute, gotEndpoint)
	}
	client.mu.Lock()
	gotCalls := slices.Clone(client.calls)
	gotOptions := slices.Clone(client.options)
	client.mu.Unlock()
	wantCalls := []string{"markets", "ticker", "order_book", "recent_trades", "candles", "balances"}
	if !slices.Equal(gotCalls, wantCalls) {
		t.Fatalf("calls = %v", gotCalls)
	}
	for _, options := range gotOptions {
		if options.EgressRouteID != "route-b" || options.Timeout != 2*time.Second {
			t.Fatalf("options = %+v", options)
		}
	}
}

func TestSpotReadRunnerClassifiesFailureWithoutLeakingMessage(t *testing.T) {
	t.Parallel()

	expectedIP := net.ParseIP("203.0.113.10")
	verifier := &fakeEgressVerifier{check: transport.PublicIPCheck{
		RouteID: "route-a", ExpectedPublicIP: expectedIP,
		ObservedPublicIP: append(net.IP(nil), expectedIP...), MatchesExpected: true,
	}}
	client := &fakeSpotReadClient{errors: map[string]error{
		"ticker": &trade.APIError{
			Category: trade.ErrorAuthentication, Exchange: model.ExchangeBinance,
			ExchangeCode: "-2015", HTTPStatus: 401,
			ExchangeMessage: "secret=never-print-this", Retryable: false,
		},
	}}
	runner, err := NewSpotReadRunner(SpotReadConfig{
		Client: client, EgressVerifier: verifier,
		Market: unified.Market{Base: "BTC", Quote: "USDT"}, EgressRouteID: "route-a",
	})
	if err != nil {
		t.Fatalf("NewSpotReadRunner() error = %v", err)
	}
	report, err := runner.Run(context.Background())
	if !errors.Is(err, ErrReadSmokeFailed) || report.Passed || len(report.Checks) != 6 {
		t.Fatalf("report = %+v, error = %v", report, err)
	}
	failure := report.Checks[2].Failure
	if failure == nil || failure.Kind != "api" || failure.Category != trade.ErrorAuthentication ||
		failure.ExchangeCode != "-2015" || failure.HTTPStatus != 401 {
		t.Fatalf("failure = %+v", failure)
	}
	encoded, marshalErr := json.Marshal(report)
	if marshalErr != nil {
		t.Fatalf("json.Marshal() error = %v", marshalErr)
	}
	if strings.Contains(string(encoded), "never-print-this") || strings.Contains(string(encoded), "secret=") {
		t.Fatalf("report leaked exchange message: %s", encoded)
	}
	client.mu.Lock()
	gotCalls := slices.Clone(client.calls)
	client.mu.Unlock()
	if !slices.Equal(gotCalls, []string{"markets", "ticker", "order_book", "recent_trades", "candles"}) {
		t.Fatalf("calls after failure = %v", gotCalls)
	}
}

func TestSpotReadRunnerRequiresExpectedPublicIP(t *testing.T) {
	t.Parallel()

	verifier := &fakeEgressVerifier{check: transport.PublicIPCheck{
		RouteID: "route-a", ObservedPublicIP: net.ParseIP("203.0.113.10"), MatchesExpected: true,
	}}
	runner, err := NewSpotReadRunner(SpotReadConfig{
		Client: &fakeSpotReadClient{}, EgressVerifier: verifier,
		Market: unified.Market{Base: "BTC", Quote: "USDT"}, EgressRouteID: "route-a",
	})
	if err != nil {
		t.Fatalf("NewSpotReadRunner() error = %v", err)
	}
	report, err := runner.Run(context.Background())
	if !errors.Is(err, ErrReadSmokeFailed) || report.Checks[0].Failure == nil ||
		report.Checks[0].Failure.Reason != "expected_public_ip_missing" {
		t.Fatalf("report = %+v, error = %v", report, err)
	}
	client := runner.client.(*fakeSpotReadClient)
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.calls) != 0 {
		t.Fatalf("exchange calls after egress failure = %v", client.calls)
	}
	for _, check := range report.Checks[1:] {
		if check.Status != CheckSkipped || check.Failure == nil ||
			check.Failure.Reason != "egress_verification_failed" {
			t.Fatalf("skipped check = %+v", check)
		}
	}
}

func TestNewSpotReadRunnerValidatesConfiguration(t *testing.T) {
	t.Parallel()

	valid := SpotReadConfig{
		Client: &fakeSpotReadClient{}, EgressVerifier: &fakeEgressVerifier{},
		Market: unified.Market{Base: "BTC", Quote: "USDT"}, EgressRouteID: "route-a",
	}
	tests := []struct {
		name   string
		mutate func(*SpotReadConfig)
	}{
		{name: "클라이언트 없음", mutate: func(config *SpotReadConfig) { config.Client = nil }},
		{name: "검사기 없음", mutate: func(config *SpotReadConfig) { config.EgressVerifier = nil }},
		{name: "마켓 오류", mutate: func(config *SpotReadConfig) { config.Market.Quote = "BTC" }},
		{name: "경로 없음", mutate: func(config *SpotReadConfig) { config.EgressRouteID = " " }},
		{name: "음수 제한 시간", mutate: func(config *SpotReadConfig) { config.CheckTimeout = -time.Second }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := valid
			test.mutate(&config)
			if _, err := NewSpotReadRunner(config); err == nil {
				t.Fatal("NewSpotReadRunner() error = nil")
			}
		})
	}
}
