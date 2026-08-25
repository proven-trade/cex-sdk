package cryptocom

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
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
	start := time.UnixMilli(1_700_000_000_000)
	end := time.UnixMilli(1_700_003_600_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method != http.MethodGet || request.Header.Get("Accept") != "application/json" ||
			request.Header.Get("Content-Type") != "application/json" ||
			request.Header.Get("User-Agent") != "proven-trade-sdk-go/0" {
			http.Error(writer, `{"id":-1,"method":"","code":40001,"message":"invalid request"}`, http.StatusBadRequest)
			return
		}
		switch request.URL.Path {
		case "/exchange/v1/public/get-instruments":
			_, _ = io.WriteString(writer, `{"id":"11","method":"public/get-instruments","code":"0","result":{"data":[{"symbol":"BTC_USDT","inst_type":"CCY_PAIR","display_name":"BTC/USDT","base_ccy":"BTC","quote_ccy":"USDT","quote_decimals":"2","quantity_decimals":6,"price_tick_size":"0.01","qty_tick_size":0.000001,"max_leverage":"10","tradable":true,"expiry_timestamp_ms":0,"beta_product":false,"underlying_symbol":"BTC","product_type":"DIGITAL_CURRENCIES","contract_size":"1","margin_buy_enabled":true,"margin_sell_enabled":false}]}}`)
		case "/exchange/v1/public/get-tickers":
			instrumentName := request.URL.Query().Get("instrument_name")
			if instrumentName != "" && instrumentName != "BTC_USDT" {
				http.Error(writer, `{"id":12,"method":"public/get-tickers","code":40001,"message":"bad instrument"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"id":12,"method":"public/get-tickers","code":0,"result":{"data":[{"i":"BTC_USDT","h":"65000","l":null,"a":64000.5,"v":"10.25","vv":"656000","oi":"0","c":"1000","b":"63999","k":64001,"bs":"0.3","ks":null,"t":"1700000000123"}]}}`)
		case "/exchange/v1/public/get-book":
			query := request.URL.Query()
			if query.Get("instrument_name") != "BTC_USDT" || query.Get("depth") != "2" {
				http.Error(writer, `{"id":13,"method":"public/get-book","code":40001}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"id":13,"method":"public/get-book","code":0,"result":{"instrument_name":"BTC_USDT","depth":"2","data":[{"bids":[["63999","0.3",2]],"asks":[[64001,"0.2","1"]],"t":"1700000000123"}]}}`)
		case "/exchange/v1/public/get-trades":
			query := request.URL.Query()
			if query.Get("instrument_name") != "BTC_USDT" || query.Get("count") != "2" ||
				query.Get("start_ts") != "1700000000000" || query.Get("end_ts") != "1700003600000" {
				http.Error(writer, `{"id":14,"method":"public/get-trades","code":40001}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"id":14,"method":"public/get-trades","code":0,"result":{"data":[{"s":"BUY","p":"64000.1","q":0.001,"t":1700000000123,"tn":"1700000000123456789","d":"9876543210123456789","i":"BTC_USDT"}]}}`)
		case "/exchange/v1/public/get-candlestick":
			query := request.URL.Query()
			if query.Get("instrument_name") != "BTC_USDT" || query.Get("timeframe") != "1m" ||
				query.Get("count") != "2" || query.Get("start_ts") != "1700000000000" ||
				query.Get("end_ts") != "1700003600000" {
				http.Error(writer, `{"id":15,"method":"public/get-candlestick","code":40001}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"id":15,"method":"public/get-candlestick","code":0,"result":{"interval":"1m","instrument_name":"BTC_USDT","data":[{"o":"63000","h":65000,"l":"62000","c":"64000.1","v":"10.25","t":"1700000000000"}]}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	client, limiter := newTestClient(t, server.URL+"/exchange/v1", sender)
	ctx := context.Background()
	instruments, err := client.Instruments(ctx, trade.WithEgressRoute("route-b"))
	if err != nil || len(instruments.Items) != 1 || instruments.Items[0].Symbol != "BTC_USDT" ||
		instruments.Items[0].QuoteDecimals != 2 || instruments.Items[0].QuantityTickSize != "0.000001" ||
		instruments.Items[0].ProductType != "DIGITAL_CURRENCIES" ||
		instruments.Items[0].ContractSize != "1" || !instruments.Items[0].MarginBuyEnabled ||
		instruments.Items[0].MarginSellEnabled || len(instruments.Items[0].Raw) == 0 ||
		len(instruments.Raw) == 0 {
		t.Fatalf("Instruments() = %+v, error = %v", instruments, err)
	}
	ticker, err := client.Ticker(ctx, "BTC_USDT")
	if err != nil || ticker.LatestPrice != "64000.5" || ticker.Low != "" ||
		ticker.BestAsk != "64001" || ticker.BestAskSize != "" || len(ticker.Raw) == 0 {
		t.Fatalf("Ticker() = %+v, error = %v", ticker, err)
	}
	tickers, err := client.Tickers(ctx)
	if err != nil || len(tickers) != 1 || tickers[0].Timestamp != "1700000000123" {
		t.Fatalf("Tickers() = %+v, error = %v", tickers, err)
	}
	book, err := client.OrderBook(ctx, OrderBookRequest{InstrumentName: "BTC_USDT", Depth: 2})
	if err != nil || book.Depth != 2 || book.Timestamp != "1700000000123" || book.Bids[0].OrderCount != 2 ||
		book.Asks[0].Price != "64001" || book.Asks[0].OrderCount != 1 || len(book.Raw) == 0 {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	trades, err := client.RecentTrades(ctx, TradesRequest{
		InstrumentName: "BTC_USDT", Count: 2, Start: &start, End: &end,
	})
	if err != nil || len(trades) != 1 || trades[0].Side != TradeSideBuy ||
		trades[0].Price != "64000.1" || trades[0].NanosecondTimestamp != "1700000000123456789" ||
		trades[0].TradeID != "9876543210123456789" || len(trades[0].Raw) == 0 {
		t.Fatalf("RecentTrades() = %+v, error = %v", trades, err)
	}
	candles, err := client.Candles(ctx, CandlesRequest{
		InstrumentName: "BTC_USDT", Timeframe: Candle1Minute,
		Count: 2, Start: &start, End: &end,
	})
	if err != nil || len(candles) != 1 || candles[0].Close != "64000.1" ||
		candles[0].Volume != "10.25" || len(candles[0].Raw) == 0 {
		t.Fatalf("Candles() = %+v, error = %v", candles, err)
	}

	routes := sender.snapshot()
	if len(routes) != 6 || routes[0] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
	for _, route := range routes[1:] {
		if route != "route-a" {
			t.Fatalf("default sender route = %q, want route-a", route)
		}
	}
	instrumentLimit, err := limiter.Snapshot("cryptocom:route:route-b:public:get-instruments:1second")
	if err != nil || instrumentLimit.Used != 1 || instrumentLimit.Rule.Limit != 100 {
		t.Fatalf("instrument limiter snapshot = %+v, error = %v", instrumentLimit, err)
	}
	tickerLimit, err := limiter.Snapshot("cryptocom:route:route-a:public:get-tickers:1second")
	if err != nil || tickerLimit.Used != 2 || tickerLimit.Rule.Limit != 100 {
		t.Fatalf("ticker limiter snapshot = %+v, error = %v", tickerLimit, err)
	}
}

func TestClientClassifiesErrorsAndHonorsRetryAfter(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Query().Get("instrument_name") {
		case "BAD_USDT":
			_, _ = io.WriteString(writer, `{"id":"request-bad","method":"public/get-tickers","code":40001,"message":"bad request","original":"secret-request"}`)
		case "DOWN_USDT":
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(writer, `{"id":21,"method":"public/get-tickers","code":"50001","message":"unavailable"}`)
		case "BROKEN_USDT":
			_, _ = io.WriteString(writer, `{`)
		case "RATE_USDT":
			writer.Header().Set("Retry-After", "3")
			writer.Header().Set("X-Request-ID", "request-rate")
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(writer, `{"id":"ignored-id","method":"public/get-tickers","code":42901,"message":"slow down"}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, limiter := newTestClient(t, server.URL+"/exchange/v1", &directSender{})

	_, err := client.Ticker(context.Background(), "BAD_USDT")
	if !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("bad request error = %v, want validation", err)
	}
	var apiError *trade.APIError
	if !errors.As(err, &apiError) || apiError.RequestID != "request-bad" ||
		apiError.ExchangeMessage != "bad request" || strings.Contains(apiError.Error(), "secret-request") {
		t.Fatalf("bad request API error = %+v", apiError)
	}
	if _, err := client.Ticker(context.Background(), "DOWN_USDT"); !errors.Is(err, trade.ErrExchangeUnavailable) {
		t.Fatalf("service error = %v, want exchange unavailable", err)
	}
	if _, err := client.Ticker(context.Background(), "BROKEN_USDT"); !errors.Is(err, trade.ErrExchangeUnavailable) {
		t.Fatalf("malformed body error = %v, want exchange unavailable", err)
	}
	_, err = client.Ticker(
		context.Background(), "RATE_USDT", trade.WithEgressRoute("route-rate"),
	)
	if !errors.Is(err, trade.ErrRateLimited) {
		t.Fatalf("rate limit error = %v, want rate limited", err)
	}
	if !errors.As(err, &apiError) || apiError.Exchange != model.ExchangeCryptoCom ||
		apiError.ExchangeCode != "42901" || apiError.RequestID != "request-rate" ||
		!apiError.Retryable {
		t.Fatalf("rate limit API error = %+v", apiError)
	}
	snapshot, snapshotErr := limiter.Snapshot("cryptocom:route:route-rate:public:get-tickers:1second")
	if snapshotErr != nil || !snapshot.BlockedUntil.After(time.Now().Add(2*time.Second)) {
		t.Fatalf("Retry-After snapshot = %+v, error = %v", snapshot, snapshotErr)
	}
}

func TestErrorClassification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		status    int
		code      string
		category  trade.ErrorCategory
		retryable bool
	}{
		{name: "bad request", status: http.StatusOK, code: "40001", category: trade.ErrorValidation},
		{name: "unauthorized", status: http.StatusOK, code: "40101", category: trade.ErrorAuthentication},
		{name: "timeout", status: http.StatusOK, code: "40801", category: trade.ErrorExchangeUnavailable, retryable: true},
		{name: "rate limit", status: http.StatusOK, code: "42901", category: trade.ErrorRateLimited, retryable: true},
		{name: "server error", status: http.StatusOK, code: "50001", category: trade.ErrorExchangeUnavailable, retryable: true},
		{name: "forbidden", status: http.StatusForbidden, category: trade.ErrorAuthorization},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			category, retryable := classifyError(test.status, test.code)
			if category != test.category || retryable != test.retryable {
				t.Fatalf("classifyError() = (%q, %t), want (%q, %t)", category, retryable, test.category, test.retryable)
			}
		})
	}
}

func TestRequestValidation(t *testing.T) {
	t.Parallel()
	beforeEpoch := time.UnixMilli(0)
	now := time.UnixMilli(1_700_000_000_000)
	tests := []struct {
		name string
		err  error
	}{
		{name: "instrument", err: validateInstrumentName("btc_usdt")},
		{name: "book zero depth", err: (OrderBookRequest{InstrumentName: "BTC_USDT"}).validate()},
		{name: "book excessive depth", err: (OrderBookRequest{InstrumentName: "BTC_USDT", Depth: 51}).validate()},
		{name: "trade count", err: (TradesRequest{InstrumentName: "BTC_USDT", Count: -1}).validate()},
		{name: "trade timestamp", err: (TradesRequest{InstrumentName: "BTC_USDT", Start: &beforeEpoch}).validate()},
		{name: "trade range", err: (TradesRequest{InstrumentName: "BTC_USDT", Start: &now, End: &now}).validate()},
		{name: "candle timeframe", err: (CandlesRequest{InstrumentName: "BTC_USDT", Timeframe: "3m"}).validate()},
		{name: "candle count", err: (CandlesRequest{InstrumentName: "BTC_USDT", Timeframe: Candle1Minute, Count: -1}).validate()},
		{name: "limit price", err: (PlaceOrderRequest{InstrumentName: "BTC_USDT", Side: OrderSideBuy, Type: OrderTypeLimit, Quantity: "1", ClientOrderID: "id"}).validate()},
		{name: "market buy quantity", err: (PlaceOrderRequest{InstrumentName: "BTC_USDT", Side: OrderSideBuy, Type: OrderTypeMarket, Quantity: "1", ClientOrderID: "id"}).validate()},
		{name: "market sell notional", err: (PlaceOrderRequest{InstrumentName: "BTC_USDT", Side: OrderSideSell, Type: OrderTypeMarket, Notional: "1", ClientOrderID: "id"}).validate()},
		{name: "post only IOC", err: (PlaceOrderRequest{InstrumentName: "BTC_USDT", Side: OrderSideBuy, Type: OrderTypeLimit, Price: "1", Quantity: "1", ClientOrderID: "id", TimeInForce: TimeInForceImmediateOrCancel, PostOnly: true}).validate()},
		{name: "client order ID", err: (PlaceOrderRequest{InstrumentName: "BTC_USDT", Side: OrderSideBuy, Type: OrderTypeLimit, Price: "1", Quantity: "1", ClientOrderID: strings.Repeat("a", 37)}).validate()},
		{name: "order identity", err: (OrderInfoRequest{OrderID: "1", ClientOrderID: "id"}).validate()},
		{name: "history limit", err: (OrderHistoryRequest{Limit: 101}).validate()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !errors.Is(test.err, trade.ErrValidation) {
				t.Fatalf("validation error = %v", test.err)
			}
		})
	}
}

func TestPrivateOrderParameterSemantics(t *testing.T) {
	t.Parallel()
	marketBuy := PlaceOrderRequest{
		InstrumentName: "BTC_USDT", Side: OrderSideBuy, Type: OrderTypeMarket,
		Notional: "100", ClientOrderID: "market-buy",
	}
	if err := marketBuy.validate(); err != nil {
		t.Fatalf("market buy validation error = %v", err)
	}
	buyParams := marketBuy.params()
	if buyParams["notional"] != "100" || buyParams["quantity"] != nil ||
		buyParams["price"] != nil || buyParams["time_in_force"] != nil {
		t.Fatalf("market buy params = %v", buyParams)
	}
	marketSell := PlaceOrderRequest{
		InstrumentName: "BTC_USDT", Side: OrderSideSell, Type: OrderTypeMarket,
		Quantity: "0.1", ClientOrderID: "market-sell",
	}
	if err := marketSell.validate(); err != nil {
		t.Fatalf("market sell validation error = %v", err)
	}
	sellParams := marketSell.params()
	if sellParams["quantity"] != "0.1" || sellParams["notional"] != nil {
		t.Fatalf("market sell params = %v", sellParams)
	}
}

func TestWireTypesRejectMalformedPayloads(t *testing.T) {
	t.Parallel()
	var level BookLevel
	for _, raw := range []string{`["1","2"]`, `[null,"2",1]`, `["1","2",-1]`} {
		if err := level.UnmarshalJSON([]byte(raw)); err == nil {
			t.Fatalf("BookLevel.UnmarshalJSON(%s) error = nil", raw)
		}
	}
	var decimal Decimal
	for _, raw := range []string{`true`, `""`, `"not-a-number"`} {
		if err := decimal.UnmarshalJSON([]byte(raw)); err == nil {
			t.Fatalf("Decimal.UnmarshalJSON(%s) error = nil", raw)
		}
	}
	var integer Integer
	for _, raw := range []string{`1.2`, `""`, `"9223372036854775808"`} {
		if err := integer.UnmarshalJSON([]byte(raw)); err == nil {
			t.Fatalf("Integer.UnmarshalJSON(%s) error = nil", raw)
		}
	}
	var scalar Scalar
	if err := scalar.UnmarshalJSON([]byte(`{"id":1}`)); err == nil {
		t.Fatal("Scalar.UnmarshalJSON() object error = nil")
	}
}

func TestClientRejectsMalformedSuccessEnvelope(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{name: "missing code", body: `{"id":1,"method":"public/get-tickers","result":{"data":[]}}`},
		{name: "wrong method", body: `{"id":1,"method":"public/get-book","code":0,"result":{"data":[]}}`},
		{name: "missing result", body: `{"id":1,"method":"public/get-tickers","code":0}`},
		{name: "missing data", body: `{"id":1,"method":"public/get-tickers","code":0,"result":{}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()
			client, _ := newTestClient(t, server.URL+"/exchange/v1", &directSender{})
			if _, err := client.Tickers(context.Background()); !errors.Is(err, trade.ErrExchangeUnavailable) {
				t.Fatalf("Tickers() error = %v, want exchange unavailable", err)
			}
		})
	}
}

func TestClientConfigurationValidation(t *testing.T) {
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
		{name: "missing executor", config: Config{DefaultEgressRouteID: "route-a"}},
		{name: "missing route", config: Config{Executor: executor}},
		{name: "insecure URL", config: Config{Executor: executor, DefaultEgressRouteID: "route-a", BaseURL: "http://example.com/exchange/v1"}},
		{name: "invalid path", config: Config{Executor: executor, DefaultEgressRouteID: "route-a", BaseURL: "https://example.com/wrong"}},
		{name: "negative timeout", config: Config{Executor: executor, DefaultEgressRouteID: "route-a", RequestTimeout: -time.Second}},
		{name: "excessive quota", config: Config{Executor: executor, DefaultEgressRouteID: "route-a", PublicRequestsPerSecond: 101}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := New(test.config); err == nil {
				t.Fatal("New() error = nil")
			}
		})
	}
}

func newTestClient(
	t *testing.T,
	baseURL string,
	sender *directSender,
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
		Executor: executor, DefaultEgressRouteID: "route-a", BaseURL: baseURL,
		AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, limiter
}
