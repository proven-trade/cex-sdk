package htx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/ratelimit"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

type directSender struct {
	mu     sync.Mutex
	routes []transport.EgressRouteID
}

func (sender *directSender) Do(
	ctx context.Context,
	routeID transport.EgressRouteID,
	request *http.Request,
) (*http.Response, error) {
	sender.mu.Lock()
	sender.routes = append(sender.routes, routeID)
	sender.mu.Unlock()
	return http.DefaultClient.Do(request.Clone(ctx))
}

func (sender *directSender) snapshot() []transport.EgressRouteID {
	sender.mu.Lock()
	defer sender.mu.Unlock()
	return slices.Clone(sender.routes)
}

func TestClientPublicMarketDataLifecycle(t *testing.T) {
	t.Parallel()
	updatedSince := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method != http.MethodGet || request.Header.Get("Accept") != "application/json" ||
			request.Header.Get("User-Agent") != "proven-trade-sdk-go/0" {
			http.Error(writer, `{"status":"error","err-code":"invalid-request"}`, http.StatusBadRequest)
			return
		}
		switch request.URL.Path {
		case "/v1/common/timestamp":
			writer.Header().Set("X-HB-RateLimit-Requests-Remain", "7")
			_, _ = io.WriteString(writer, `{"status":"ok","data":1700000000123}`)
		case "/v1/settings/common/market-symbols":
			query := request.URL.Query()
			if query.Get("symbols") != "btcusdt,ethusdt" || query.Get("ts") != "1700000000000" {
				http.Error(writer, `{"status":"error","err-code":"invalid-parameter"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":[{"symbol":"btcusdt","state":"online","bc":"btc","qc":"usdt","pp":2,"ap":6,"sp":"main","vp":8,"minoa":0.00001,"maxoa":1000,"minov":1,"lominoa":0.00001,"lomaxoa":1000,"lomaxba":1000,"lomaxsa":1000,"smminoa":0.00001,"smmaxoa":19,"bmmaxov":1500000,"blmlt":1.1,"slmgt":0.9,"msormlt":0.01,"mbormlt":0.01,"at":"enabled","tags":"activities"}],"ts":"1700000000123","full":1}`)
		case "/market/detail/merged":
			if request.URL.Query().Get("symbol") != "btcusdt" {
				http.Error(writer, `{"status":"error","err-code":"invalid-parameter"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","ts":1700000000123,"tick":{"id":272156789143,"version":"272156789143","open":63000,"close":64000.5,"low":62000,"high":65000,"amount":1.2E-4,"vol":640000,"count":52,"bid":[63999,0.3],"ask":["64001","0.2"]}}`)
		case "/market/tickers":
			_, _ = io.WriteString(writer, `{"status":"ok","ts":1700000000123,"data":[{"symbol":"btcusdt","open":63000,"close":64000,"low":62000,"high":65000,"amount":10.5,"vol":670000,"count":100,"bid":63999,"bidSize":0.3,"ask":64001,"askSize":0.2}]}`)
		case "/market/depth":
			query := request.URL.Query()
			if query.Get("symbol") != "btcusdt" || query.Get("type") != "step0" || query.Get("depth") != "5" {
				http.Error(writer, `{"status":"error","err-code":"invalid-parameter"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","ts":1700000000123,"tick":{"ts":1700000000000,"version":136107114472,"bids":[[63999,0.3]],"asks":[[64001,"0.2"]]}}`)
		case "/market/trade":
			_, _ = io.WriteString(writer, `{"status":"ok","ts":1700000000123,"tick":{"id":136107843051,"ts":1700000000000,"data":[{"id":136107843051348400221001656,"trade-id":"102517374388","amount":0.028416,"price":64000,"ts":1700000000000,"direction":"buy"}]}}`)
		case "/market/history/trade":
			if request.URL.Query().Get("size") != "2" {
				http.Error(writer, `{"status":"error","err-code":"invalid-size"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","ts":1700000000123,"data":[{"id":136108764379,"ts":1700000000000,"data":[{"id":"136108764379348400430265987","trade-id":102517381182,"amount":1.24E-4,"price":63998,"ts":1700000000000,"direction":"sell"}]}]}`)
		case "/market/history/kline":
			query := request.URL.Query()
			if query.Get("symbol") != "btcusdt" || query.Get("period") != "1min" || query.Get("size") != "2" {
				http.Error(writer, `{"status":"error","err-code":"invalid-parameter"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","ts":1700000000123,"data":[{"id":1700000000,"open":63000,"close":64000,"low":62000,"high":65000,"amount":10.5,"vol":670000,"count":100}]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	client, limiter := newTestClient(t, server.URL, sender)
	ctx := context.Background()
	serverTime, err := client.ServerTime(ctx, trade.WithEgressRoute("route-b"))
	if err != nil || serverTime.Time != 1_700_000_000_123 || len(serverTime.Raw) == 0 {
		t.Fatalf("ServerTime() = %+v, error = %v", serverTime, err)
	}
	symbols, err := client.MarketSymbols(ctx, MarketSymbolsRequest{
		Symbols: []string{"btcusdt", "ethusdt"}, UpdatedSince: &updatedSince,
	})
	if err != nil || len(symbols.Symbols) != 1 || symbols.Symbols[0].MinimumOrderAmount != "0.00001" ||
		symbols.UpdatedAt != "1700000000123" || symbols.Full != 1 || len(symbols.Symbols[0].Raw) == 0 ||
		len(symbols.Raw) == 0 {
		t.Fatalf("MarketSymbols() = %+v, error = %v", symbols, err)
	}
	ticker, err := client.Ticker(ctx, "btcusdt")
	if err != nil || ticker.Close != "64000.5" || ticker.Amount != "1.2E-4" ||
		ticker.Bid.Quantity != "0.3" || ticker.Ask.Price != "64001" || len(ticker.Raw) == 0 {
		t.Fatalf("Ticker() = %+v, error = %v", ticker, err)
	}
	tickers, err := client.Tickers(ctx)
	if err != nil || len(tickers) != 1 || tickers[0].BidQuantity != "0.3" || len(tickers[0].Raw) == 0 {
		t.Fatalf("Tickers() = %+v, error = %v", tickers, err)
	}
	book, err := client.OrderBook(ctx, OrderBookRequest{Symbol: "btcusdt", Depth: 5})
	if err != nil || book.Version != "136107114472" || book.Bids[0].Quantity != "0.3" ||
		book.Asks[0].Price != "64001" || len(book.Raw) == 0 {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	latest, err := client.LatestTrade(ctx, "btcusdt")
	if err != nil || len(latest.Trades) != 1 || latest.Trades[0].TradeID != "102517374388" ||
		latest.Trades[0].Amount != "0.028416" || len(latest.Trades[0].Raw) == 0 || len(latest.Raw) == 0 {
		t.Fatalf("LatestTrade() = %+v, error = %v", latest, err)
	}
	trades, err := client.RecentTrades(ctx, TradesRequest{Symbol: "btcusdt", Size: 2})
	if err != nil || len(trades) != 1 || trades[0].Trades[0].Amount != "1.24E-4" ||
		trades[0].Trades[0].Direction != TradeDirectionSell || len(trades[0].Raw) == 0 {
		t.Fatalf("RecentTrades() = %+v, error = %v", trades, err)
	}
	candles, err := client.Candles(ctx, CandlesRequest{
		Symbol: "btcusdt", Interval: Candle1Minute, Size: 2,
	})
	if err != nil || len(candles) != 1 || candles[0].Close != "64000" ||
		candles[0].QuoteVolume != "670000" || len(candles[0].Raw) == 0 {
		t.Fatalf("Candles() = %+v, error = %v", candles, err)
	}

	routes := sender.snapshot()
	if len(routes) != 8 || routes[0] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
	for _, route := range routes[1:] {
		if route != "route-a" {
			t.Fatalf("default sender route = %q, want route-a", route)
		}
	}
	timeSnapshot, err := limiter.Snapshot("htx:route:route-b:public:timestamp:1second")
	if err != nil || timeSnapshot.Rule.Limit != 10 || timeSnapshot.Rule.Window != time.Second {
		t.Fatalf("time limiter snapshot = %+v, error = %v", timeSnapshot, err)
	}
	tickerSnapshot, err := limiter.Snapshot("htx:route:route-a:public:ticker:1second")
	if err != nil || tickerSnapshot.Rule.Limit != 10 || tickerSnapshot.Rule.Window != time.Second {
		t.Fatalf("ticker limiter snapshot = %+v, error = %v", tickerSnapshot, err)
	}
}

func TestObserveRateLimitRecordsServerUsage(t *testing.T) {
	t.Parallel()

	const key = "htx:test:public:100years"
	window := 100 * 365 * 24 * time.Hour
	limiter, err := ratelimit.New(ratelimit.Rule{Key: key, Limit: 10, Window: window})
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	header := http.Header{"X-Hb-Ratelimit-Requests-Remain": {"7"}}
	observeRateLimit(limiter, rateLimit{key: key, limit: 10, window: window}, header, time.Now())
	snapshot, err := limiter.Snapshot(key)
	if err != nil || snapshot.Used != 3 {
		t.Fatalf("관측한 요청 제한 상태 = %+v, 오류 = %v", snapshot, err)
	}
}

func TestClientClassifiesErrorsAndHonorsRetryAfter(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Query().Get("symbol") {
		case "badusdt":
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(writer, `{"status":"error","err-code":"invalid-parameter","err-msg":"invalid symbol"}`)
		case "downusdt":
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(writer, `{"status":"error","err-code":"gateway-internal-error","err-msg":"unavailable"}`)
		case "brokenusdt":
			_, _ = io.WriteString(writer, `{`)
		case "rateusdt":
			writer.Header().Set("Retry-After", "3")
			writer.Header().Set("request-id", "request-rate")
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(writer, `{"status":"error","err-code":"too-many-requests","err-msg":"slow down"}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, limiter := newTestClient(t, server.URL, &directSender{})

	if _, err := client.Ticker(context.Background(), "badusdt"); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("bad symbol error = %v, want validation", err)
	}
	if _, err := client.Ticker(context.Background(), "downusdt"); !errors.Is(err, trade.ErrExchangeUnavailable) {
		t.Fatalf("service error = %v, want exchange unavailable", err)
	}
	if _, err := client.Ticker(context.Background(), "brokenusdt"); !errors.Is(err, trade.ErrExchangeUnavailable) {
		t.Fatalf("malformed body error = %v, want exchange unavailable", err)
	}
	_, err := client.Ticker(
		context.Background(), "rateusdt", trade.WithEgressRoute("route-rate"),
	)
	if !errors.Is(err, trade.ErrRateLimited) {
		t.Fatalf("rate limit error = %v, want rate limited", err)
	}
	var apiError *trade.APIError
	if !errors.As(err, &apiError) || apiError.Exchange != model.ExchangeHTX ||
		apiError.ExchangeCode != "too-many-requests" || apiError.RequestID != "request-rate" ||
		!apiError.Retryable {
		t.Fatalf("rate limit API error = %+v", apiError)
	}
	snapshot, snapshotErr := limiter.Snapshot("htx:route:route-rate:public:ticker:1second")
	if snapshotErr != nil || !snapshot.BlockedUntil.After(time.Now().Add(2*time.Second)) {
		t.Fatalf("Retry-After snapshot = %+v, error = %v", snapshot, snapshotErr)
	}
}

func TestRequestValidation(t *testing.T) {
	t.Parallel()
	beforeEpoch := time.UnixMilli(0)
	tests := []struct {
		name string
		err  error
	}{
		{name: "symbol", err: validateSymbol("BTC-USDT")},
		{name: "symbol duplicate", err: (MarketSymbolsRequest{Symbols: []string{"btcusdt", "btcusdt"}}).validate()},
		{name: "symbol time", err: (MarketSymbolsRequest{UpdatedSince: &beforeEpoch}).validate()},
		{name: "book depth", err: (OrderBookRequest{Symbol: "btcusdt", Depth: 6}).validate()},
		{name: "book type", err: (OrderBookRequest{Symbol: "btcusdt", Type: "step6"}).validate()},
		{name: "trade size", err: (TradesRequest{Symbol: "btcusdt", Size: 2001}).validate()},
		{name: "candle interval", err: (CandlesRequest{Symbol: "btcusdt", Interval: "2min"}).validate()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !errors.Is(test.err, trade.ErrValidation) {
				t.Fatalf("validation error = %v", test.err)
			}
		})
	}
}

func TestWireTypesRejectMalformedPayloads(t *testing.T) {
	t.Parallel()
	var level BookLevel
	if err := level.UnmarshalJSON([]byte(`["1"]`)); err == nil {
		t.Fatal("BookLevel.UnmarshalJSON() error = nil")
	}
	if err := level.UnmarshalJSON([]byte(`[null,"1"]`)); err == nil {
		t.Fatal("BookLevel.UnmarshalJSON() null error = nil")
	}
	var decimal Decimal
	if err := decimal.UnmarshalJSON([]byte(`true`)); err == nil {
		t.Fatal("Decimal.UnmarshalJSON() boolean error = nil")
	}
	var scalar Scalar
	if err := scalar.UnmarshalJSON([]byte(`{"id":1}`)); err == nil {
		t.Fatal("Scalar.UnmarshalJSON() object error = nil")
	}
}

func TestDecodeResponseRejectsMissingStatus(t *testing.T) {
	t.Parallel()
	client := &Client{}
	_, err := client.decodeResponse(commonexchange.Response{
		StatusCode: http.StatusOK, Header: make(http.Header), Body: []byte(`{"data":1}`),
	}, &struct{}{})
	if !errors.Is(err, trade.ErrExchangeUnavailable) {
		t.Fatalf("decodeResponse() error = %v, want exchange unavailable", err)
	}
}

func TestRateLimitExpireDuration(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	tests := []struct {
		value string
		want  time.Duration
		ok    bool
	}{
		{value: "250", want: 250 * time.Millisecond, ok: true},
		{value: "1700000002", want: 2 * time.Second, ok: true},
		{value: "1700000003000", want: 3 * time.Second, ok: true},
		{value: "1699999999", ok: false},
		{value: "bad", ok: false},
	}
	for _, test := range tests {
		got, ok := rateLimitExpireDuration(test.value, now)
		if got != test.want || ok != test.ok {
			t.Fatalf("rateLimitExpireDuration(%q) = (%v, %v), want (%v, %v)", test.value, got, ok, test.want, test.ok)
		}
	}
}

func TestNewClientValidationAndDefaults(t *testing.T) {
	t.Parallel()
	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{
		Sender: &directSender{}, Limiter: limiter,
	})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	tests := []struct {
		name   string
		config Config
	}{
		{name: "nil executor", config: Config{DefaultEgressRouteID: "route-a"}},
		{name: "missing route", config: Config{Executor: executor}},
		{name: "insecure base URL", config: Config{Executor: executor, DefaultEgressRouteID: "route-a", BaseURL: "http://api.huobi.pro"}},
		{name: "base URL path", config: Config{Executor: executor, DefaultEgressRouteID: "route-a", BaseURL: "https://api.huobi.pro/v1"}},
		{name: "negative timeout", config: Config{Executor: executor, DefaultEgressRouteID: "route-a", RequestTimeout: -time.Second}},
		{name: "negative quota", config: Config{Executor: executor, DefaultEgressRouteID: "route-a", PublicRequestsPerSecond: -1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := New(test.config); err == nil {
				t.Fatal("New() error = nil")
			}
		})
	}
	client, err := New(Config{Executor: executor, DefaultEgressRouteID: " route-a "})
	if err != nil || client.requestTimeout != DefaultRequestTimeout ||
		client.publicRequestsPerSecond != DefaultPublicRequestsPerSecond ||
		client.defaultEgressRouteID != "route-a" || client.baseURL.String() != DefaultBaseURL {
		t.Fatalf("New() defaults = %+v, error = %v", client, err)
	}
}

func newTestClient(
	t *testing.T,
	baseURL string,
	sender commonexchange.Sender,
) (*Client, *ratelimit.Limiter) {
	t.Helper()
	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{
		Sender: sender, Limiter: limiter,
	})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	client, err := New(Config{
		Executor: executor, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, limiter
}
