package kucoin

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
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

type recordingProvider struct {
	mu             sync.Mutex
	calls          int
	lastAPIKey     []byte
	lastSecret     []byte
	lastPassphrase []byte
}

func (provider *recordingProvider) Resolve(context.Context, string) (credential.Material, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.calls++
	material := credential.Material{
		APIKey: []byte("test-api-key"), SecretKey: []byte("test-secret"),
		Passphrase: []byte("test-passphrase"),
	}
	provider.lastAPIKey = material.APIKey
	provider.lastSecret = material.SecretKey
	provider.lastPassphrase = material.Passphrase
	return material, nil
}

func (provider *recordingProvider) snapshot() (int, []byte, []byte, []byte) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.calls, slices.Clone(provider.lastAPIKey), slices.Clone(provider.lastSecret),
		slices.Clone(provider.lastPassphrase)
}

func TestClientSpotLifecycle(t *testing.T) {
	t.Parallel()

	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/v2/symbols" {
			writer.Header().Set("gw-ratelimit-limit", "2000")
			writer.Header().Set("gw-ratelimit-remaining", "1990")
		}
		if isPrivatePath(request.URL.Path) && !verifySignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"code":"400005","msg":"Invalid signature"}`, http.StatusUnauthorized)
			return
		}
		switch {
		case request.URL.Path == "/api/v2/symbols":
			_, _ = io.WriteString(writer, `{"code":"200000","data":[{"symbol":"BTC-USDT","name":"BTC-USDT","baseCurrency":"BTC","quoteCurrency":"USDT","feeCurrency":"USDT","market":"USDS","baseMinSize":"0.00001","quoteMinSize":"0.1","baseMaxSize":"10000","quoteMaxSize":"99999999","baseIncrement":"0.00001","quoteIncrement":"0.000001","priceIncrement":"0.1","priceLimitRate":"0.1","minFunds":"0.1","isMarginEnabled":true,"enableTrading":true}]}`)
		case request.URL.Path == "/api/v1/market/orderbook/level1":
			_, _ = io.WriteString(writer, `{"code":"200000","data":{"sequence":"1","bestAsk":"64001","bestAskSize":"2","bestBid":"64000","bestBidSize":"1","price":"64000","size":"0.01","time":1700000000000000000}}`)
		case request.URL.Path == "/api/v1/market/orderbook/level2_20":
			_, _ = io.WriteString(writer, `{"code":"200000","data":{"sequence":"2","time":1700000000000,"bids":[["64000","1"]],"asks":[["64001","2"]]}}`)
		case request.URL.Path == "/api/v1/market/histories":
			_, _ = io.WriteString(writer, `{"code":"200000","data":[{"sequence":"3","price":"64000","size":"0.01","side":"buy","time":1700000000000000000}]}`)
		case request.URL.Path == "/api/v1/market/candles":
			_, _ = io.WriteString(writer, `{"code":"200000","data":[["1700000000","63000","64000","65000","62000","10","640000"]]}`)
		case request.URL.Path == "/api/v1/accounts":
			_, _ = io.WriteString(writer, `{"code":"200000","data":[{"id":"account-1","currency":"USDT","type":"trade","balance":"1000","available":"900","holds":"100"}]}`)
		case request.URL.Path == "/api/v1/hf/orders" && request.Method == http.MethodPost:
			body, _ := io.ReadAll(request.Body)
			if string(body) != `{"clientOid":"strategy-1","symbol":"BTC-USDT","type":"limit","side":"buy","price":"64000","size":"0.01","timeInForce":"GTC"}` {
				http.Error(writer, `{"code":"400100","msg":"unexpected body"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"code":"200000","data":{"orderId":"order-1","clientOid":"strategy-1"}}`)
		case request.URL.Path == "/api/v1/hf/orders/order-1" && request.Method == http.MethodGet:
			_, _ = io.WriteString(writer, `{"code":"200000","data":{"id":"order-1","symbol":"BTC-USDT","opType":"DEAL","type":"limit","side":"buy","price":"64000","size":"0.01","funds":"640","dealFunds":"320","dealSize":"0.005","fee":"0.32","feeCurrency":"USDT","timeInForce":"GTC","postOnly":false,"clientOid":"strategy-1","isActive":true,"cancelExist":false,"createdAt":1700000000000,"tradeType":"TRADE"}}`)
		case request.URL.Path == "/api/v1/hf/orders/order-1" && request.Method == http.MethodDelete:
			_, _ = io.WriteString(writer, `{"code":"200000","data":{"orderId":"order-1","clientOid":"strategy-1"}}`)
		case request.URL.Path == "/api/v1/hf/orders/active/page":
			_, _ = io.WriteString(writer, `{"code":"200000","data":{"currentPage":1,"pageSize":50,"totalNum":1,"totalPage":1,"items":[{"id":"order-1","symbol":"BTC-USDT","type":"limit","side":"buy","price":"64000","size":"0.01","clientOid":"strategy-1","isActive":true,"createdAt":1700000000000}]}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, limiter := newTestClient(
		t, server.URL, sender, provider, []transport.EgressRouteID{"route-a", "route-b"},
		func() time.Time { return fixedNow },
	)

	symbols, err := client.Symbols(context.Background(), trade.WithEgressRoute("route-b"))
	if err != nil || len(symbols) != 1 || symbols[0].PriceIncrement != "0.1" || len(symbols[0].Raw) == 0 {
		t.Fatalf("Symbols() = %+v, error = %v", symbols, err)
	}
	ticker, err := client.Ticker(context.Background(), "BTC-USDT")
	if err != nil || ticker.Price != "64000" || len(ticker.Raw) == 0 {
		t.Fatalf("Ticker() = %+v, error = %v", ticker, err)
	}
	book, err := client.OrderBook(
		context.Background(), OrderBookRequest{Symbol: "BTC-USDT", Size: OrderBook20},
	)
	if err != nil || book.Bids[0].Size != "1" || len(book.Raw) == 0 {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	trades, err := client.RecentTrades(context.Background(), RecentTradesRequest{Symbol: "BTC-USDT"})
	if err != nil || len(trades) != 1 || trades[0].Sequence != "3" || len(trades[0].Raw) == 0 {
		t.Fatalf("RecentTrades() = %+v, error = %v", trades, err)
	}
	start, end := time.Unix(1_699_999_000, 0), time.Unix(1_700_000_000, 0)
	candles, err := client.Candles(
		context.Background(), CandlesRequest{
			Symbol: "BTC-USDT", Interval: Candle1Minute, Start: &start, End: &end,
		},
	)
	if err != nil || len(candles) != 1 || candles[0].Close != "64000" || len(candles[0].Raw) == 0 {
		t.Fatalf("Candles() = %+v, error = %v", candles, err)
	}
	accounts, err := client.Accounts(
		context.Background(), AccountsRequest{Currency: "USDT", Type: AccountTypeTrade},
	)
	if err != nil || len(accounts) != 1 || accounts[0].Holds != "100" || len(accounts[0].Raw) == 0 {
		t.Fatalf("Accounts() = %+v, error = %v", accounts, err)
	}
	reference, err := client.PlaceOrder(
		context.Background(),
		PlaceOrderRequest{
			ClientOrderID: "strategy-1", Symbol: "BTC-USDT", Type: OrderTypeLimit,
			Side: SideBuy, Price: "64000", Size: "0.01", TimeInForce: TimeInForceGTC,
		},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || reference.OrderID != "order-1" || len(reference.Raw) == 0 {
		t.Fatalf("PlaceOrder() = %+v, error = %v", reference, err)
	}
	order, err := client.OrderInfo(
		context.Background(), OrderInfoRequest{OrderID: "order-1", Symbol: "BTC-USDT"},
	)
	if err != nil || !order.Active || order.DealSize != "0.005" || len(order.Raw) == 0 {
		t.Fatalf("OrderInfo() = %+v, error = %v", order, err)
	}
	canceled, err := client.CancelOrder(
		context.Background(), CancelOrderRequest{OrderID: "order-1", Symbol: "BTC-USDT"},
	)
	if err != nil || canceled.OrderID != "order-1" {
		t.Fatalf("CancelOrder() = %+v, error = %v", canceled, err)
	}
	page, err := client.OpenOrders(
		context.Background(), OpenOrdersRequest{Symbol: "BTC-USDT", PageNumber: 1, PageSize: 50},
	)
	if err != nil || len(page.Orders) != 1 || page.TotalPages != 1 || len(page.Raw) == 0 || len(page.Orders[0].Raw) == 0 {
		t.Fatalf("OpenOrders() = %+v, error = %v", page, err)
	}

	routes := sender.snapshot()
	if len(routes) != 10 || routes[0] != "route-b" || routes[6] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
	calls, apiKey, secret, passphrase := provider.snapshot()
	if calls != 5 {
		t.Fatalf("provider calls = %d, want 5", calls)
	}
	if !allZero(apiKey) || !allZero(secret) || !allZero(passphrase) {
		t.Fatal("resolved credential byte slices were not overwritten")
	}
	publicSnapshot, err := limiter.Snapshot("kucoin:route:route-b:public:30seconds")
	if err != nil || publicSnapshot.Used != 10 {
		t.Fatalf("public limiter snapshot = %+v, error = %v", publicSnapshot, err)
	}
	spotSnapshot, err := limiter.Snapshot("kucoin:account:kucoin-main:spot:30seconds")
	if err != nil || spotSnapshot.Used != 6 {
		t.Fatalf("spot limiter snapshot = %+v, error = %v", spotSnapshot, err)
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
	calls, _, _, _ := provider.snapshot()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestClientClassifiesLogicalErrorsAndUnknownMutationState(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1/market/orderbook/level1":
			_, _ = io.WriteString(writer, `{"code":"429000","msg":"Too Many Requests"}`)
		case "/api/v1/accounts":
			_, _ = io.WriteString(writer, `{"code":"200004","msg":"Balance insufficient"}`)
		default:
			_, _ = io.WriteString(writer, `{"code":"500000","msg":"Internal Server Error"}`)
		}
	}))
	defer server.Close()

	client, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, func() time.Time { return time.UnixMilli(1_700_000_000_000) },
	)
	_, err := client.Ticker(context.Background(), "BTC-USDT")
	if !errors.Is(err, trade.ErrRateLimited) {
		t.Fatalf("Ticker() error = %v, want rate limited", err)
	}
	_, err = client.Accounts(context.Background(), AccountsRequest{})
	if !errors.Is(err, trade.ErrInsufficientBalance) {
		t.Fatalf("Accounts() error = %v, want insufficient balance", err)
	}
	_, err = client.PlaceOrder(
		context.Background(),
		PlaceOrderRequest{
			ClientOrderID: "strategy-1", Symbol: "BTC-USDT", Type: OrderTypeMarket,
			Side: SideBuy, Funds: "100",
		},
	)
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
		{name: "symbol", err: validateSymbol("btc-usdt")},
		{name: "book size", err: (OrderBookRequest{Symbol: "BTC-USDT", Size: 50}).validate()},
		{name: "candle range", err: (CandlesRequest{Symbol: "BTC-USDT", Interval: Candle1Minute, Start: &start, End: &end}).validate()},
		{name: "account type", err: (AccountsRequest{Type: "contract"}).validate()},
		{name: "market buy size", err: (PlaceOrderRequest{ClientOrderID: "one", Symbol: "BTC-USDT", Type: OrderTypeMarket, Side: SideBuy, Size: "1", Funds: "1"}).validate()},
		{name: "post only IOC", err: (PlaceOrderRequest{ClientOrderID: "one", Symbol: "BTC-USDT", Type: OrderTypeLimit, Side: SideBuy, Price: "1", Size: "1", TimeInForce: TimeInForceIOC, PostOnly: true}).validate()},
		{name: "page size", err: (OpenOrdersRequest{Symbol: "BTC-USDT", PageSize: 51}).validate()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !errors.Is(test.err, trade.ErrValidation) {
				t.Fatalf("validation error = %v", test.err)
			}
		})
	}
}

func TestSignHMACSHA256(t *testing.T) {
	t.Parallel()

	payload := []byte("1547015186532POST/api/v1/hf/orders{\"symbol\":\"BTC-USDT\"}")
	got, err := SignHMACSHA256([]byte("test-secret"), payload)
	if err != nil {
		t.Fatalf("SignHMACSHA256() error = %v", err)
	}
	mac := hmac.New(sha256.New, []byte("test-secret"))
	_, _ = mac.Write(payload)
	want := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
}

func isPrivatePath(path string) bool {
	return path == "/api/v1/accounts" || path == "/api/v1/hf/orders" ||
		path == "/api/v1/hf/orders/order-1" || path == "/api/v1/hf/orders/active/page"
}

func verifySignedRequest(t *testing.T, request *http.Request, secret []byte, now time.Time) bool {
	t.Helper()
	timestamp := request.Header.Get("KC-API-TIMESTAMP")
	if timestamp != "1700000000000" || request.Header.Get("KC-API-KEY") != "test-api-key" ||
		request.Header.Get("KC-API-KEY-VERSION") != "2" {
		return false
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	endpoint := request.URL.EscapedPath()
	if request.URL.RawQuery != "" {
		endpoint += "?" + request.URL.RawQuery
	}
	wantSignature, err := SignHMACSHA256(
		secret, signaturePayload(timestamp, request.Method, endpoint, body),
	)
	if err != nil {
		t.Fatalf("sign expected request: %v", err)
	}
	wantPassphrase, err := SignHMACSHA256(secret, []byte("test-passphrase"))
	if err != nil {
		t.Fatalf("sign expected passphrase: %v", err)
	}
	return hmac.Equal([]byte(request.Header.Get("KC-API-SIGN")), []byte(wantSignature)) &&
		hmac.Equal([]byte(request.Header.Get("KC-API-PASSPHRASE")), []byte(wantPassphrase)) &&
		now.UnixMilli() == 1_700_000_000_000
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
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{Sender: sender, Limiter: limiter})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	client, err := New(Config{
		Executor: executor,
		Credentials: &credential.Descriptor{
			AccountID: "kucoin-main", Exchange: model.ExchangeKuCoin, SecretRef: "secret/kucoin-main",
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
