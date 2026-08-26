package mexc

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

	trade "github.com/proven-trade/cex-sdk"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
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
	start := time.UnixMilli(1_700_000_000_000)
	end := time.UnixMilli(1_700_003_600_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method != http.MethodGet || request.Header.Get("Accept") != "application/json" ||
			request.Header.Get("User-Agent") != "cex-sdk-go/0" {
			http.Error(writer, `{"code":400,"msg":"invalid request"}`, http.StatusBadRequest)
			return
		}
		switch request.URL.Path {
		case "/api/v3/ping":
			_, _ = io.WriteString(writer, `{}`)
		case "/api/v3/time":
			_, _ = io.WriteString(writer, `{"serverTime":1700000000000}`)
		case "/api/v3/defaultSymbols":
			_, _ = io.WriteString(writer, `{"code":0,"data":["BTCUSDT","ETHUSDT"],"msg":"success"}`)
		case "/api/v3/exchangeInfo":
			if request.URL.Query().Get("symbol") != "BTCUSDT" {
				http.Error(writer, `{"code":10007,"msg":"bad symbol"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"timezone":"CST","serverTime":1700000000000,"rateLimits":[],"exchangeFilters":[],"symbols":[{"symbol":"BTCUSDT","status":"1","baseAsset":"BTC","baseAssetPrecision":8,"quoteAsset":"USDT","quotePrecision":2,"quoteAssetPrecision":2,"baseCommissionPrecision":8,"quoteCommissionPrecision":2,"orderTypes":["LIMIT","MARKET","LIMIT_MAKER"],"quoteOrderQtyMarketAllowed":true,"isSpotTradingAllowed":true,"isMarginTradingAllowed":false,"quoteAmountPrecision":"1","baseSizePrecision":"0.000001","permissions":["SPOT"],"filters":[{"filterType":"PERCENT_PRICE_BY_SIDE"}],"maxQuoteAmount":"4000000","makerCommission":"0","takerCommission":"0.0005","quoteAmountPrecisionMarket":"1","maxQuoteAmountMarket":"4000000","fullName":"Bitcoin","tradeSideType":1,"contractAddress":"","conceptPlateIds":[50],"firstOpenTime":1506787200000,"conceptPlates":["pow"],"st":false}]}`)
		case "/api/v3/depth":
			if request.URL.Query().Get("symbol") != "BTCUSDT" || request.URL.Query().Get("limit") != "5" {
				http.Error(writer, `{"code":33333,"msg":"invalid depth query"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"lastUpdateId":1112416,"bids":[["63999","0.3"]],"asks":[[64001,"0.2"]]}`)
		case "/api/v3/trades":
			_, _ = io.WriteString(writer, `[{"id":null,"price":"64000","qty":"0.1","quoteQty":"6400","time":1700000000000,"isBuyerMaker":true,"isBestMatch":true}]`)
		case "/api/v3/aggTrades":
			query := request.URL.Query()
			if query.Get("startTime") != "1700000000000" || query.Get("endTime") != "1700003600000" || query.Get("limit") != "2" {
				http.Error(writer, `{"code":33333,"msg":"invalid aggregate query"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `[{"a":1,"f":null,"l":3,"p":"64000","q":"0.3","T":1700000000000,"m":false,"M":true}]`)
		case "/api/v3/klines":
			query := request.URL.Query()
			if query.Get("interval") != "1m" || query.Get("startTime") != "1700000000000" ||
				query.Get("endTime") != "1700003600000" || query.Get("limit") != "2" {
				http.Error(writer, `{"code":33333,"msg":"invalid candle query"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `[[1700000000000,"63000","65000","62000","64000","10",1700000059999,"640000"]]`)
		case "/api/v3/avgPrice":
			_, _ = io.WriteString(writer, `{"mins":5,"price":"64000"}`)
		case "/api/v3/ticker/24hr":
			_, _ = io.WriteString(writer, `{"symbol":"BTCUSDT","priceChange":"1000","priceChangePercent":"0.0158","prevClosePrice":"63000","lastPrice":"64000","lastQty":"0.1","bidPrice":"63999","bidQty":"0.3","askPrice":"64001","askQty":"0.2","openPrice":"63000","highPrice":"65000","lowPrice":"62000","volume":"10","quoteVolume":null,"openTime":1699913600000,"closeTime":1700000000000,"count":null}`)
		case "/api/v3/ticker/price":
			_, _ = io.WriteString(writer, `{"symbol":"BTCUSDT","price":"64000"}`)
		case "/api/v3/ticker/bookTicker":
			_, _ = io.WriteString(writer, `{"symbol":"BTCUSDT","bidPrice":"63999","bidQty":"0.3","askPrice":"64001","askQty":"0.2"}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	client, limiter := newTestClient(t, server.URL, sender)
	ctx := context.Background()
	if err := client.Ping(ctx, trade.WithEgressRoute("route-b")); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	serverTime, err := client.ServerTime(ctx)
	if err != nil || serverTime.Time != 1_700_000_000_000 || len(serverTime.Raw) == 0 {
		t.Fatalf("ServerTime() = %+v, error = %v", serverTime, err)
	}
	defaultSymbols, err := client.DefaultSymbols(ctx)
	if err != nil || !slices.Equal(defaultSymbols, []string{"BTCUSDT", "ETHUSDT"}) {
		t.Fatalf("DefaultSymbols() = %v, error = %v", defaultSymbols, err)
	}
	exchangeInfo, err := client.ExchangeInfo(ctx, ExchangeInfoRequest{Symbol: "BTCUSDT"})
	if err != nil || exchangeInfo.Timezone != "CST" || len(exchangeInfo.Symbols) != 1 ||
		exchangeInfo.Symbols[0].TradeSideType != "1" || len(exchangeInfo.Symbols[0].Raw) == 0 ||
		len(exchangeInfo.Raw) == 0 {
		t.Fatalf("ExchangeInfo() = %+v, error = %v", exchangeInfo, err)
	}
	book, err := client.OrderBook(ctx, OrderBookRequest{Symbol: "BTCUSDT", Limit: 5})
	if err != nil || book.LastUpdateID != 1_112_416 || book.Bids[0].Quantity != "0.3" ||
		book.Asks[0].Price != "64001" || len(book.Raw) == 0 {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	trades, err := client.RecentTrades(ctx, TradesRequest{Symbol: "BTCUSDT", Limit: 2})
	if err != nil || len(trades) != 1 || trades[0].ID != "" || trades[0].QuoteQuantity != "6400" ||
		len(trades[0].Raw) == 0 {
		t.Fatalf("RecentTrades() = %+v, error = %v", trades, err)
	}
	aggregates, err := client.AggregateTrades(ctx, AggregateTradesRequest{
		Symbol: "BTCUSDT", Start: &start, End: &end, Limit: 2,
	})
	if err != nil || len(aggregates) != 1 || aggregates[0].AggregateID != "1" ||
		aggregates[0].FirstTradeID != "" || aggregates[0].LastTradeID != "3" ||
		len(aggregates[0].Raw) == 0 {
		t.Fatalf("AggregateTrades() = %+v, error = %v", aggregates, err)
	}
	candles, err := client.Candles(ctx, CandlesRequest{
		Symbol: "BTCUSDT", Interval: Candle1Minute, Start: &start, End: &end, Limit: 2,
	})
	if err != nil || len(candles) != 1 || candles[0].Open != "63000" ||
		candles[0].Close != "64000" || candles[0].QuoteVolume != "640000" ||
		len(candles[0].Raw) == 0 {
		t.Fatalf("Candles() = %+v, error = %v", candles, err)
	}
	average, err := client.AveragePrice(ctx, "BTCUSDT")
	if err != nil || average.Minutes != 5 || average.Price != "64000" || len(average.Raw) == 0 {
		t.Fatalf("AveragePrice() = %+v, error = %v", average, err)
	}
	ticker, err := client.Ticker24H(ctx, "BTCUSDT")
	if err != nil || ticker.LastPrice != "64000" || ticker.QuoteVolume != "" || ticker.Count != "" ||
		len(ticker.Raw) == 0 {
		t.Fatalf("Ticker24H() = %+v, error = %v", ticker, err)
	}
	price, err := client.PriceTicker(ctx, "BTCUSDT")
	if err != nil || price.Price != "64000" || len(price.Raw) == 0 {
		t.Fatalf("PriceTicker() = %+v, error = %v", price, err)
	}
	best, err := client.BookTicker(ctx, "BTCUSDT")
	if err != nil || best.BidQuantity != "0.3" || best.AskPrice != "64001" || len(best.Raw) == 0 {
		t.Fatalf("BookTicker() = %+v, error = %v", best, err)
	}

	routes := sender.snapshot()
	if len(routes) != 12 || routes[0] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
	for _, route := range routes[1:] {
		if route != "route-a" {
			t.Fatalf("default sender route = %q, want route-a", route)
		}
	}
	assertLimiterUsed(t, limiter, "mexc:route:route-b:public:ping:10seconds", 1)
	assertLimiterUsed(t, limiter, "mexc:route:route-a:public:exchange-info:10seconds", 10)
	assertLimiterUsed(t, limiter, "mexc:route:route-a:public:trades:10seconds", 5)
}

func TestClientClassifiesErrorsAndHonorsRetryAfter(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Query().Get("symbol") {
		case "BADUSDT":
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(writer, `{"code":10007,"msg":"bad symbol"}`)
		case "DOWNUSDT":
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(writer, `{"code":503,"msg":"service unavailable"}`)
		case "BROKENUSDT":
			_, _ = io.WriteString(writer, `{`)
		case "RATEUSDT":
			writer.Header().Set("Retry-After", "3")
			writer.Header().Set("X-MEXC-Request-Id", "request-rate")
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(writer, `{"code":429,"msg":"too many requests"}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, limiter := newTestClient(t, server.URL, &directSender{})

	if _, err := client.PriceTicker(context.Background(), "BADUSDT"); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("bad symbol error = %v, want validation", err)
	}
	if _, err := client.PriceTicker(context.Background(), "DOWNUSDT"); !errors.Is(err, trade.ErrExchangeUnavailable) {
		t.Fatalf("service error = %v, want exchange unavailable", err)
	}
	if _, err := client.PriceTicker(context.Background(), "BROKENUSDT"); !errors.Is(err, trade.ErrExchangeUnavailable) {
		t.Fatalf("malformed body error = %v, want exchange unavailable", err)
	}
	_, err := client.PriceTicker(
		context.Background(), "RATEUSDT", trade.WithEgressRoute("route-rate"),
	)
	if !errors.Is(err, trade.ErrRateLimited) {
		t.Fatalf("rate limit error = %v, want rate limited", err)
	}
	var apiError *trade.APIError
	if !errors.As(err, &apiError) || apiError.Exchange != model.ExchangeMEXC ||
		apiError.ExchangeCode != "429" || apiError.RequestID != "request-rate" || !apiError.Retryable {
		t.Fatalf("rate limit API error = %+v", apiError)
	}
	snapshot, snapshotErr := limiter.Snapshot("mexc:route:route-rate:public:price-ticker:10seconds")
	if snapshotErr != nil || !snapshot.BlockedUntil.After(time.Now().Add(2*time.Second)) {
		t.Fatalf("Retry-After snapshot = %+v, error = %v", snapshot, snapshotErr)
	}
}

func TestDecodeResponseAcceptsDocumentedSuccessCode(t *testing.T) {
	t.Parallel()
	client := &Client{}
	var envelope struct {
		Code Scalar   `json:"code"`
		Data []string `json:"data"`
	}
	raw, err := client.decodeResponse(commonexchange.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       []byte(`{"code":200,"data":["BTCUSDT"],"msg":null}`),
	}, &envelope)
	if err != nil || envelope.Code != "200" || !slices.Equal(envelope.Data, []string{"BTCUSDT"}) ||
		len(raw) == 0 {
		t.Fatalf("decodeResponse() = %+v, raw=%q, error=%v", envelope, raw, err)
	}
}

func TestRequestValidation(t *testing.T) {
	t.Parallel()
	beforeEpoch := time.UnixMilli(0)
	start := time.UnixMilli(2_000)
	end := time.UnixMilli(1_000)
	tests := []struct {
		name string
		err  error
	}{
		{name: "symbol", err: validateSymbol("btc-usdt")},
		{name: "exchange info conflict", err: (ExchangeInfoRequest{Symbol: "BTCUSDT", Symbols: []string{"ETHUSDT"}}).validate()},
		{name: "exchange info duplicate", err: (ExchangeInfoRequest{Symbols: []string{"BTCUSDT", "BTCUSDT"}}).validate()},
		{name: "book limit", err: (OrderBookRequest{Symbol: "BTCUSDT", Limit: 5001}).validate()},
		{name: "trade limit", err: (TradesRequest{Symbol: "BTCUSDT", Limit: -1}).validate()},
		{name: "aggregate half range", err: (AggregateTradesRequest{Symbol: "BTCUSDT", Start: &start}).validate()},
		{name: "aggregate reversed", err: (AggregateTradesRequest{Symbol: "BTCUSDT", Start: &start, End: &end}).validate()},
		{name: "aggregate epoch", err: (AggregateTradesRequest{Symbol: "BTCUSDT", Start: &beforeEpoch, End: &start}).validate()},
		{name: "candle interval", err: (CandlesRequest{Symbol: "BTCUSDT", Interval: "2m"}).validate()},
		{name: "candle reversed", err: (CandlesRequest{Symbol: "BTCUSDT", Interval: Candle1Minute, Start: &start, End: &end}).validate()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !errors.Is(test.err, trade.ErrValidation) {
				t.Fatalf("validation error = %v", test.err)
			}
		})
	}
}

func TestPositionalTypesRejectMalformedPayloads(t *testing.T) {
	t.Parallel()
	var level BookLevel
	if err := level.UnmarshalJSON([]byte(`["1"]`)); err == nil {
		t.Fatal("BookLevel.UnmarshalJSON() error = nil")
	}
	if err := level.UnmarshalJSON([]byte(`[null,"1"]`)); err == nil {
		t.Fatal("BookLevel.UnmarshalJSON() null error = nil")
	}
	var candle Candle
	if err := candle.UnmarshalJSON([]byte(`[1,"1"]`)); err == nil {
		t.Fatal("Candle.UnmarshalJSON() error = nil")
	}
	var scalar Scalar
	if err := scalar.UnmarshalJSON([]byte(`{"id":1}`)); err == nil {
		t.Fatal("Scalar.UnmarshalJSON() object error = nil")
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
		{name: "insecure base URL", config: Config{Executor: executor, DefaultEgressRouteID: "route-a", BaseURL: "http://api.mexc.com"}},
		{name: "base URL path", config: Config{Executor: executor, DefaultEgressRouteID: "route-a", BaseURL: "https://api.mexc.com/v3"}},
		{name: "negative timeout", config: Config{Executor: executor, DefaultEgressRouteID: "route-a", RequestTimeout: -time.Second}},
		{name: "negative quota", config: Config{Executor: executor, DefaultEgressRouteID: "route-a", EndpointQuota: -1}},
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
		client.endpointQuota != DefaultEndpointQuota || client.defaultEgressRouteID != "route-a" ||
		client.baseURL.String() != DefaultBaseURL {
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

func assertLimiterUsed(t *testing.T, limiter *ratelimit.Limiter, key string, want int) {
	t.Helper()
	snapshot, err := limiter.Snapshot(key)
	if err != nil || snapshot.Used != want || snapshot.Rule.Limit != DefaultEndpointQuota ||
		snapshot.Rule.Window != 10*time.Second {
		t.Fatalf("limiter snapshot %q = %+v, error = %v", key, snapshot, err)
	}
}
