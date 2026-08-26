package gateio

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
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

type recordingProvider struct {
	mu         sync.Mutex
	calls      int
	lastAPIKey []byte
	lastSecret []byte
}

func (provider *recordingProvider) Resolve(context.Context, string) (credential.Material, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.calls++
	material := credential.Material{
		APIKey: []byte("test-api-key"), SecretKey: []byte("test-secret"),
	}
	provider.lastAPIKey = material.APIKey
	provider.lastSecret = material.SecretKey
	return material, nil
}

func (provider *recordingProvider) snapshot() (int, []byte, []byte) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.calls, slices.Clone(provider.lastAPIKey), slices.Clone(provider.lastSecret)
}

func TestClientSpotLifecycle(t *testing.T) {
	t.Parallel()

	fixedNow := time.Unix(1_700_000_000, 0)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/v4/spot/currency_pairs" {
			writer.Header().Set("X-Gate-RateLimit-Limit", "200")
			writer.Header().Set("X-Gate-RateLimit-Requests-Remain", "198")
		}
		if isPrivatePath(request.URL.Path) && !verifySignedRequest(t, request, []byte("test-secret")) {
			http.Error(writer, `{"label":"INVALID_SIGNATURE","message":"invalid signature"}`, http.StatusUnauthorized)
			return
		}
		switch {
		case request.URL.Path == "/api/v4/spot/currency_pairs":
			_, _ = io.WriteString(writer, `[{"id":"BTC_USDT","base":"BTC","base_name":"Bitcoin","quote":"USDT","quote_name":"Tether","min_base_amount":"0.0001","min_quote_amount":"1","max_base_amount":null,"max_quote_amount":null,"amount_precision":6,"precision":2,"trade_status":"tradable","sell_start":0,"buy_start":0,"delisting_time":0,"type":"normal","st_tag":false}]`)
		case request.URL.Path == "/api/v4/spot/tickers":
			_, _ = io.WriteString(writer, `[{"currency_pair":"BTC_USDT","last":"64000","lowest_ask":"64001","lowest_size":"0.2","highest_bid":"63999","highest_size":"0.3","change_percentage":"1.2","base_volume":"10","quote_volume":"640000","high_24h":"65000","low_24h":"62000"}]`)
		case request.URL.Path == "/api/v4/spot/order_book":
			_, _ = io.WriteString(writer, `{"id":10,"current":1700000000000,"update":1700000000100,"asks":[["64001","0.2"]],"bids":[["63999","0.3"]]}`)
		case request.URL.Path == "/api/v4/spot/trades" && !isPrivatePath(request.URL.Path):
			_, _ = io.WriteString(writer, `[{"id":"trade-public","create_time":"1700000000","create_time_ms":"1700000000000.0","currency_pair":"BTC_USDT","side":"buy","amount":"0.1","price":"64000","sequence_id":"1"}]`)
		case request.URL.Path == "/api/v4/spot/candlesticks":
			_, _ = io.WriteString(writer, `[["1700000000","640000","64000","65000","62000","63000","10","true"]]`)
		case request.URL.Path == "/api/v4/spot/accounts":
			_, _ = io.WriteString(writer, `[{"currency":"USDT","available":"900","locked":"100","update_id":1}]`)
		case request.URL.Path == "/api/v4/spot/orders" && request.Method == http.MethodPost:
			body, _ := io.ReadAll(request.Body)
			if string(body) != `{"text":"t-strategy-1","currency_pair":"BTC_USDT","type":"limit","account":"spot","side":"buy","amount":"0.1","price":"64000","time_in_force":"gtc"}` {
				http.Error(writer, `{"label":"INVALID_PARAM_VALUE","message":"unexpected body"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, orderJSON("open"))
		case request.URL.Path == "/api/v4/spot/orders/order-1" && request.Method == http.MethodGet:
			_, _ = io.WriteString(writer, orderJSON("open"))
		case request.URL.Path == "/api/v4/spot/orders/order-1" && request.Method == http.MethodDelete:
			_, _ = io.WriteString(writer, orderJSON("cancelled"))
		case request.URL.Path == "/api/v4/spot/open_orders":
			_, _ = io.WriteString(writer, `[{"currency_pair":"BTC_USDT","total":1,"orders":[`+orderJSON("open")+`]}]`)
		case request.URL.Path == "/api/v4/spot/my_trades":
			_, _ = io.WriteString(writer, `[{"id":"trade-private","create_time":"1700000000","create_time_ms":"1700000000000.0","currency_pair":"BTC_USDT","order_id":"order-1","side":"buy","role":"taker","amount":"0.1","price":"64000","fee":"0.064","fee_currency":"USDT","text":"t-strategy-1"}]`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, limiter := newTestClient(
		t, server.URL, sender, provider,
		[]transport.EgressRouteID{"route-a", "route-b"}, func() time.Time { return fixedNow },
	)

	pairs, err := client.CurrencyPairs(context.Background(), trade.WithEgressRoute("route-b"))
	if err != nil || len(pairs) != 1 || pairs[0].AmountPrecision != 6 || len(pairs[0].Raw) == 0 {
		t.Fatalf("CurrencyPairs() = %+v, error = %v", pairs, err)
	}
	ticker, err := client.Ticker(context.Background(), "BTC_USDT")
	if err != nil || ticker.Last != "64000" || ticker.HighestBidSize != "0.3" || len(ticker.Raw) == 0 {
		t.Fatalf("Ticker() = %+v, error = %v", ticker, err)
	}
	book, err := client.OrderBook(
		context.Background(), OrderBookRequest{CurrencyPair: "BTC_USDT", Limit: 20},
	)
	if err != nil || len(book.Bids) != 1 || book.Bids[0].Amount != "0.3" || len(book.Raw) == 0 {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	trades, err := client.RecentTrades(
		context.Background(), TradesRequest{CurrencyPair: "BTC_USDT", Limit: 100},
	)
	if err != nil || len(trades) != 1 || trades[0].ID != "trade-public" || len(trades[0].Raw) == 0 {
		t.Fatalf("RecentTrades() = %+v, error = %v", trades, err)
	}
	candles, err := client.Candles(
		context.Background(), CandlesRequest{
			CurrencyPair: "BTC_USDT", Interval: Candle1Minute, Limit: 100,
		},
	)
	if err != nil || len(candles) != 1 || candles[0].Close != "64000" ||
		!candles[0].Closed || len(candles[0].Raw) == 0 {
		t.Fatalf("Candles() = %+v, error = %v", candles, err)
	}
	accounts, err := client.Accounts(context.Background(), AccountsRequest{Currency: "USDT"})
	if err != nil || len(accounts) != 1 || accounts[0].Available != "900" || len(accounts[0].Raw) == 0 {
		t.Fatalf("Accounts() = %+v, error = %v", accounts, err)
	}
	placed, err := client.PlaceOrder(
		context.Background(), PlaceOrderRequest{
			ClientOrderID: "t-strategy-1", CurrencyPair: "BTC_USDT", Type: OrderTypeLimit,
			Account: "spot", Side: SideBuy, Amount: "0.1", Price: "64000",
			TimeInForce: TimeInForceGTC,
		}, trade.WithEgressRoute("route-b"),
	)
	if err != nil || placed.ID != "order-1" || len(placed.Raw) == 0 {
		t.Fatalf("PlaceOrder() = %+v, error = %v", placed, err)
	}
	order, err := client.OrderInfo(
		context.Background(), OrderInfoRequest{OrderID: "order-1", CurrencyPair: "BTC_USDT"},
	)
	if err != nil || order.Status != "open" || len(order.Raw) == 0 {
		t.Fatalf("OrderInfo() = %+v, error = %v", order, err)
	}
	canceled, err := client.CancelOrder(
		context.Background(), CancelOrderRequest{OrderID: "order-1", CurrencyPair: "BTC_USDT"},
	)
	if err != nil || canceled.Status != "cancelled" || len(canceled.Raw) == 0 {
		t.Fatalf("CancelOrder() = %+v, error = %v", canceled, err)
	}
	groups, err := client.OpenOrders(
		context.Background(), OpenOrdersRequest{Page: 1, Limit: 100},
	)
	if err != nil || len(groups) != 1 || len(groups[0].Orders) != 1 ||
		len(groups[0].Raw) == 0 || len(groups[0].Orders[0].Raw) == 0 {
		t.Fatalf("OpenOrders() = %+v, error = %v", groups, err)
	}
	myTrades, err := client.MyTrades(
		context.Background(), MyTradesRequest{
			CurrencyPair: "BTC_USDT", OrderID: "order-1", Page: 1, Limit: 100,
		},
	)
	if err != nil || len(myTrades) != 1 || myTrades[0].Fee != "0.064" || len(myTrades[0].Raw) == 0 {
		t.Fatalf("MyTrades() = %+v, error = %v", myTrades, err)
	}

	routes := sender.snapshot()
	if len(routes) != 11 || routes[0] != "route-b" || routes[6] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
	calls, apiKey, secret := provider.snapshot()
	if calls != 6 {
		t.Fatalf("provider calls = %d, want 6", calls)
	}
	if !allZero(apiKey) || !allZero(secret) {
		t.Fatal("resolved credential byte slices were not overwritten")
	}
	publicSnapshot, err := limiter.Snapshot("gateio:route:route-b:public:currency-pairs:10seconds")
	if err != nil || publicSnapshot.Used != 2 {
		t.Fatalf("currency pair limiter snapshot = %+v, error = %v", publicSnapshot, err)
	}
	privateSnapshot, err := limiter.Snapshot("gateio:account:gateio-main:private:accounts:10seconds")
	if err != nil || privateSnapshot.Used != 1 {
		t.Fatalf("accounts limiter snapshot = %+v, error = %v", privateSnapshot, err)
	}
	orderSnapshot, err := limiter.Snapshot("gateio:account:gateio-main:spot-order:BTC_USDT:1second")
	if err != nil || orderSnapshot.Used != 1 {
		t.Fatalf("order limiter snapshot = %+v, error = %v", orderSnapshot, err)
	}
	cancelSnapshot, err := limiter.Snapshot("gateio:account:gateio-main:spot-cancel:1second")
	if err != nil || cancelSnapshot.Used != 1 {
		t.Fatalf("cancel limiter snapshot = %+v, error = %v", cancelSnapshot, err)
	}
}

func TestClientRejectsUnauthorizedRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{}
	client, _ := newTestClient(
		t, "http://127.0.0.1", &directSender{}, provider,
		[]transport.EgressRouteID{"route-a"}, nil,
	)
	_, err := client.Accounts(
		context.Background(), AccountsRequest{}, trade.WithEgressRoute("route-b"),
	)
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("Accounts() error = %v, want authorization", err)
	}
	calls, _, _ := provider.snapshot()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestClientClassifiesErrorsAndUnknownMutationState(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v4/spot/tickers":
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(writer, `{"label":"TOO_FAST","message":"rate limit exceeded"}`)
		case "/api/v4/spot/accounts":
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(writer, `{"label":"BALANCE_NOT_ENOUGH","message":"balance not enough"}`)
		case "/api/v4/spot/orders/order-missing":
			writer.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(writer, `{"label":"ORDER_NOT_FOUND","message":"order not found"}`)
		default:
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(writer, `{"label":"INTERNAL","message":"server error"}`)
		}
	}))
	defer server.Close()

	client, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, func() time.Time { return time.Unix(1_700_000_000, 0) },
	)
	_, err := client.Ticker(context.Background(), "BTC_USDT")
	if !errors.Is(err, trade.ErrRateLimited) {
		t.Fatalf("Ticker() error = %v, want rate limited", err)
	}
	_, err = client.Accounts(context.Background(), AccountsRequest{})
	if !errors.Is(err, trade.ErrInsufficientBalance) {
		t.Fatalf("Accounts() error = %v, want insufficient balance", err)
	}
	_, err = client.OrderInfo(
		context.Background(), OrderInfoRequest{
			OrderID: "order-missing", CurrencyPair: "BTC_USDT",
		},
	)
	if !errors.Is(err, trade.ErrOrderNotFound) {
		t.Fatalf("OrderInfo() error = %v, want order not found", err)
	}
	_, err = client.PlaceOrder(context.Background(), validMarketOrder())
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceOrder() error = %v, want unknown execution state", err)
	}
}

func TestClientTreatsMalformedMutationResponseAsUnknown(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{}`)
	}))
	defer server.Close()
	client, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, func() time.Time { return time.Unix(1_700_000_000, 0) },
	)
	_, err := client.PlaceOrder(context.Background(), validMarketOrder())
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceOrder() error = %v, want unknown execution state", err)
	}
}

func TestRequestValidation(t *testing.T) {
	t.Parallel()

	end, start := time.Unix(1_700_000_000, 0), time.Unix(1_700_001_000, 0)
	tests := []struct {
		name string
		err  error
	}{
		{name: "currency pair", err: validateCurrencyPair("btc_usdt")},
		{name: "book limit", err: (OrderBookRequest{CurrencyPair: "BTC_USDT", Limit: 101}).validate()},
		{name: "candle range", err: (CandlesRequest{CurrencyPair: "BTC_USDT", Interval: Candle1Minute, From: &start, To: &end}).validate()},
		{name: "candle conflict", err: (CandlesRequest{CurrencyPair: "BTC_USDT", Interval: Candle1Minute, Limit: 10, From: &end}).validate()},
		{name: "account currency", err: (AccountsRequest{Currency: "usdt"}).validate()},
		{name: "client order ID", err: (PlaceOrderRequest{ClientOrderID: "strategy", CurrencyPair: "BTC_USDT", Type: OrderTypeMarket, Account: "spot", Side: SideBuy, Amount: "10", TimeInForce: TimeInForceIOC}).validate()},
		{name: "market price", err: (PlaceOrderRequest{ClientOrderID: "t-strategy", CurrencyPair: "BTC_USDT", Type: OrderTypeMarket, Account: "spot", Side: SideBuy, Amount: "10", Price: "1", TimeInForce: TimeInForceIOC}).validate()},
		{name: "limit policy", err: (PlaceOrderRequest{ClientOrderID: "t-strategy", CurrencyPair: "BTC_USDT", Type: OrderTypeLimit, Account: "spot", Side: SideBuy, Amount: "1", Price: "1", TimeInForce: "bad"}).validate()},
		{name: "trade identity", err: (MyTradesRequest{OrderID: "order-1"}).validate()},
		{name: "page offset", err: (OpenOrdersRequest{Page: 102, Limit: 1000}).validate()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !errors.Is(test.err, trade.ErrValidation) {
				t.Fatalf("validation error = %v", test.err)
			}
		})
	}
}

func TestSignHMACSHA512(t *testing.T) {
	t.Parallel()

	body := []byte(`{"currency_pair":"BTC_USDT"}`)
	payload := signaturePayload(
		http.MethodPost, "/api/v4/spot/orders", "", PayloadHash(body), "1700000000",
	)
	got, err := SignHMACSHA512([]byte("test-secret"), payload)
	if err != nil {
		t.Fatalf("SignHMACSHA512() error = %v", err)
	}
	mac := hmac.New(sha512.New, []byte("test-secret"))
	_, _ = mac.Write(payload)
	want := hex.EncodeToString(mac.Sum(nil))
	if got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
	if _, err := SignHMACSHA512(nil, payload); err == nil {
		t.Fatal("SignHMACSHA512() accepted an empty secret")
	}
}

func TestCandleAcceptsLegacySevenFieldResponse(t *testing.T) {
	t.Parallel()

	var candle Candle
	if err := candle.UnmarshalJSON(
		[]byte(`["1700000000","640000","64000","65000","62000","63000","true"]`),
	); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}
	if candle.Timestamp != 1_700_000_000 || candle.Open != "63000" ||
		candle.BaseVolume != "" || !candle.Closed || len(candle.Raw) == 0 {
		t.Fatalf("candle = %+v", candle)
	}
}

func validMarketOrder() PlaceOrderRequest {
	return PlaceOrderRequest{
		ClientOrderID: "t-strategy", CurrencyPair: "BTC_USDT", Type: OrderTypeMarket,
		Account: "spot", Side: SideBuy, Amount: "10", TimeInForce: TimeInForceIOC,
	}
}

func orderJSON(status string) string {
	return `{"id":"order-1","text":"t-strategy-1","create_time":"1700000000","update_time":"1700000001","create_time_ms":1700000000000,"update_time_ms":1700000001000,"status":"` + status + `","currency_pair":"BTC_USDT","type":"limit","account":"spot","side":"buy","amount":"0.1","price":"64000","time_in_force":"gtc","left":"0.1","filled_amount":"0","filled_total":"0","avg_deal_price":"0","fee":"0","fee_currency":"USDT","finish_as":""}`
}

func isPrivatePath(path string) bool {
	return path == "/api/v4/spot/accounts" || path == "/api/v4/spot/orders" ||
		path == "/api/v4/spot/orders/order-1" || path == "/api/v4/spot/open_orders" ||
		path == "/api/v4/spot/my_trades"
}

func verifySignedRequest(t *testing.T, request *http.Request, secret []byte) bool {
	t.Helper()
	if request.Header.Get("Timestamp") != "1700000000" ||
		request.Header.Get("KEY") != "test-api-key" {
		return false
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	wantSignature, err := SignHMACSHA512(secret, signaturePayload(
		request.Method, request.URL.EscapedPath(), request.URL.RawQuery,
		PayloadHash(body), request.Header.Get("Timestamp"),
	))
	if err != nil {
		t.Fatalf("sign expected request: %v", err)
	}
	return hmac.Equal([]byte(request.Header.Get("SIGN")), []byte(wantSignature))
}

func newTestClient(
	t *testing.T,
	baseURL string,
	sender commonexchange.Sender,
	provider credential.Provider,
	allowedRoutes []transport.EgressRouteID,
	now func() time.Time,
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
		Executor: executor,
		Credentials: &credential.Descriptor{
			AccountID: "gateio-main", Exchange: model.ExchangeGateIO,
			SecretRef: "secret/gateio-main",
			Permissions: []credential.Permission{
				credential.PermissionRead, credential.PermissionTrade,
			},
			AllowedEgressRouteIDs: allowedRoutes,
		},
		CredentialProvider: provider, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true, Now: now,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, limiter
}

func allZero(value []byte) bool {
	for _, item := range value {
		if item != 0 {
			return false
		}
	}
	return true
}
