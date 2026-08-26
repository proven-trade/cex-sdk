package bitget

import (
	"bytes"
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
		APIKey:     []byte("test-api-key"),
		SecretKey:  []byte("test-secret"),
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
	return provider.calls,
		slices.Clone(provider.lastAPIKey),
		slices.Clone(provider.lastSecret),
		slices.Clone(provider.lastPassphrase)
}

func TestClientSpotAndFuturesLifecycle(t *testing.T) {
	t.Parallel()

	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		private := request.URL.Path == "/api/v3/account/assets" ||
			request.URL.Path == "/api/v3/trade/place-order" ||
			request.URL.Path == "/api/v3/trade/order-info" ||
			request.URL.Path == "/api/v3/trade/cancel-order" ||
			request.URL.Path == "/api/v3/trade/unfilled-orders" ||
			request.URL.Path == "/api/v3/trade/history-orders" ||
			request.URL.Path == "/api/v3/position/current-position"
		if private && !verifySignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"code":"40009","msg":"sign signature error"}`, http.StatusBadRequest)
			return
		}
		switch request.URL.Path {
		case "/api/v3/market/instruments":
			_, _ = io.WriteString(writer, `{"code":"00000","msg":"success","requestTime":1700000000000,"data":[{"symbol":"BTCUSDT","category":"SPOT","baseCoin":"BTC","quoteCoin":"USDT","pricePrecision":"2","quantityPrecision":"6","minOrderAmount":"1","status":"online"}]}`)
		case "/api/v3/market/orderbook":
			_, _ = io.WriteString(writer, `{"code":"00000","msg":"success","requestTime":1700000000000,"data":{"a":[[64001.0,2.5]],"b":[["64000.0","1.5"]],"ts":"1700000000000"}}`)
		case "/api/v3/account/assets":
			_, _ = io.WriteString(writer, `{"code":"00000","msg":"success","requestTime":1700000000000,"data":{"accountEquity":"1000","assets":[{"coin":"USDT","balance":"1000","available":"900","locked":"100"}]}}`)
		case "/api/v3/trade/place-order":
			body, _ := io.ReadAll(request.Body)
			if string(body) != `{"category":"USDT-FUTURES","symbol":"BTCUSDT","qty":"0.01","price":"64000","side":"buy","orderType":"limit","timeInForce":"gtc","posSide":"long","clientOid":"strategy-1"}` {
				http.Error(writer, `{"code":"40017","msg":"unexpected body"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"code":"00000","msg":"success","requestTime":1700000000000,"data":{"clientOid":"strategy-1","orderId":"42"}}`)
		case "/api/v3/trade/order-info":
			_, _ = io.WriteString(writer, `{"code":"00000","msg":"success","requestTime":1700000000000,"data":{"orderId":"42","clientOid":"strategy-1","category":"USDT-FUTURES","symbol":"BTCUSDT","orderType":"limit","side":"buy","price":"64000","qty":"0.01","orderStatus":"live","posSide":"long"}}`)
		case "/api/v3/trade/cancel-order":
			_, _ = io.WriteString(writer, `{"code":"00000","msg":"success","requestTime":1700000000000,"data":{"clientOid":"strategy-1","orderId":"42"}}`)
		case "/api/v3/trade/unfilled-orders":
			_, _ = io.WriteString(writer, `{"code":"00000","msg":"success","requestTime":1700000000000,"data":{"list":[{"orderId":"42","clientOid":"strategy-1","category":"USDT-FUTURES","symbol":"BTCUSDT","orderStatus":"live"}],"cursor":"next-42"}}`)
		case "/api/v3/trade/history-orders":
			_, _ = io.WriteString(writer, `{"code":"00000","msg":"success","requestTime":1700000000000,"data":{"list":[{"orderId":"41","clientOid":"done-41","category":"SPOT","symbol":"BTCUSDT","orderStatus":"filled"}],"cursor":""}}`)
		case "/api/v3/position/current-position":
			_, _ = io.WriteString(writer, `{"code":"00000","msg":"success","requestTime":1700000000000,"data":{"list":[{"category":"USDT-FUTURES","symbol":"BTCUSDT","posSide":"long","total":"0.01","avgPrice":"64000","markPrice":"64100"}]}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, limiter := newTestClient(
		t,
		server.URL,
		sender,
		provider,
		[]transport.EgressRouteID{"route-a", "route-b"},
		func() time.Time { return fixedNow },
	)

	instruments, err := client.Instruments(
		context.Background(),
		InstrumentsRequest{Category: CategorySpot, Symbol: "BTCUSDT"},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || len(instruments) != 1 || instruments[0].MinimumOrderAmount != "1" || len(instruments[0].Raw) == 0 {
		t.Fatalf("Instruments() = %+v, error = %v", instruments, err)
	}
	book, err := client.OrderBook(
		context.Background(),
		OrderBookRequest{Category: CategorySpot, Symbol: "BTCUSDT", Limit: 5},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || book.Asks[0].Price != "64001.0" || book.Bids[0].Quantity != "1.5" {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	assets, err := client.AccountAssets(context.Background())
	if err != nil || assets.AccountEquity != "1000" || len(assets.Raw) == 0 {
		t.Fatalf("AccountAssets() = %+v, error = %v", assets, err)
	}
	reference, err := client.PlaceOrder(
		context.Background(),
		PlaceOrderRequest{
			Category: CategoryUSDTFutures, Symbol: "BTCUSDT", Quantity: "0.01",
			Price: "64000", Side: SideBuy, OrderType: OrderTypeLimit,
			TimeInForce: TimeInForceGTC, PositionSide: PositionSideLong, ClientOrderID: "strategy-1",
		},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || reference.OrderID != "42" || len(reference.Raw) == 0 {
		t.Fatalf("PlaceOrder() = %+v, error = %v", reference, err)
	}
	order, err := client.OrderInfo(context.Background(), OrderInfoRequest{OrderID: "42"})
	if err != nil || order.Status != OrderStatusLive || len(order.Raw) == 0 {
		t.Fatalf("OrderInfo() = %+v, error = %v", order, err)
	}
	if _, err := client.CancelOrder(context.Background(), CancelOrderRequest{OrderID: "42", Category: CategoryUSDTFutures}); err != nil {
		t.Fatalf("CancelOrder() error = %v", err)
	}
	page, err := client.OpenOrders(context.Background(), OpenOrdersRequest{Category: CategoryUSDTFutures})
	if err != nil || len(page.Orders) != 1 || page.Cursor != "next-42" || len(page.Orders[0].Raw) == 0 {
		t.Fatalf("OpenOrders() = %+v, error = %v", page, err)
	}
	history, err := client.OrderHistory(context.Background(), OrderHistoryRequest{Category: CategorySpot, Symbol: "BTCUSDT"})
	if err != nil || len(history.Orders) != 1 || history.Orders[0].Status != OrderStatusFilled {
		t.Fatalf("OrderHistory() = %+v, error = %v", history, err)
	}
	positions, err := client.Positions(context.Background(), PositionsRequest{Category: CategoryUSDTFutures, Symbol: "BTCUSDT"})
	if err != nil || len(positions) != 1 || positions[0].Total != "0.01" || len(positions[0].Raw) == 0 {
		t.Fatalf("Positions() = %+v, error = %v", positions, err)
	}

	wantRoutes := []transport.EgressRouteID{
		"route-b", "route-b", "route-a", "route-b", "route-a", "route-a", "route-a", "route-a", "route-a",
	}
	if routes := sender.snapshot(); !slices.Equal(routes, wantRoutes) {
		t.Fatalf("sender routes = %v, want %v", routes, wantRoutes)
	}
	providerCalls, apiKey, secret, passphrase := provider.snapshot()
	if providerCalls != 7 {
		t.Fatalf("provider calls = %d, want 7", providerCalls)
	}
	if !allZero(apiKey) || !allZero(secret) || !allZero(passphrase) {
		t.Fatal("resolved credential byte slices were not overwritten")
	}
	snapshot, err := limiter.Snapshot("bitget:account:bitget-main:endpoint:api/v3/trade/place-order:1second")
	if err != nil || snapshot.Used != 1 {
		t.Fatalf("place-order limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestClientAdditionalMarketDataEndpoints(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v3/market/tickers":
			_, _ = io.WriteString(writer, `{"code":"00000","msg":"success","requestTime":1700000000000,"data":[{"category":"USDT-FUTURES","symbol":"BTCUSDT","lastPrice":"64000","markPrice":"64001","fundingRate":"0.0001","ts":"1700000000000"}]}`)
		case "/api/v3/market/fills":
			_, _ = io.WriteString(writer, `{"code":"00000","msg":"success","requestTime":1700000000000,"data":[{"execId":"9","price":"64000","size":"0.01","side":"buy","ts":"1700000000000","isRPI":"no"}]}`)
		case "/api/v3/market/candles":
			if request.URL.Query().Get("interval") != "1H" || request.URL.Query().Get("type") != "mark" {
				http.Error(writer, `{"code":"40017","msg":"bad candle query"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"code":"00000","msg":"success","requestTime":1700000000000,"data":[["1700000000000","1","2","0.5","1.5","10","15"]]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, limiter := newTestClient(
		t,
		server.URL,
		&directSender{},
		&recordingProvider{},
		[]transport.EgressRouteID{"route-a", "route-b"},
		nil,
	)
	tickers, err := client.Tickers(
		context.Background(),
		TickersRequest{Category: CategoryUSDTFutures, Symbol: "BTCUSDT"},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || len(tickers) != 1 || tickers[0].MarkPrice != "64001" || len(tickers[0].Raw) == 0 {
		t.Fatalf("Tickers() = %+v, error = %v", tickers, err)
	}
	fills, err := client.RecentFills(
		context.Background(),
		RecentFillsRequest{Category: CategoryUSDTFutures, Symbol: "BTCUSDT", Limit: 1},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || len(fills) != 1 || fills[0].ExecutionID != "9" {
		t.Fatalf("RecentFills() = %+v, error = %v", fills, err)
	}
	candles, err := client.Candles(
		context.Background(),
		CandlesRequest{
			Category: CategoryUSDTFutures, Symbol: "BTCUSDT",
			Interval: Candle1Hour, Type: CandleTypeMark, Limit: 1,
		},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || len(candles) != 1 || candles[0].Close != "1.5" || candles[0].Turnover != "15" {
		t.Fatalf("Candles() = %+v, error = %v", candles, err)
	}
	snapshot, err := limiter.Snapshot("bitget:route:route-b:global:1minute")
	if err != nil || snapshot.Used != 3 {
		t.Fatalf("global limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestClientRejectsCredentialRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, _ := newTestClient(
		t,
		"http://127.0.0.1",
		sender,
		provider,
		[]transport.EgressRouteID{"route-a"},
		nil,
	)
	_, err := client.AccountAssets(context.Background(), trade.WithEgressRoute("route-b"))
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("AccountAssets() error = %v, want ErrAuthorization", err)
	}
	providerCalls, _, _, _ := provider.snapshot()
	if providerCalls != 0 || len(sender.snapshot()) != 0 {
		t.Fatalf("provider calls = %d, sender routes = %v", providerCalls, sender.snapshot())
	}
}

func TestMutationTimeoutCodeIsUnknownExecutionState(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"code":"40010","msg":"Request timed out","requestTime":1700000000000,"data":null}`)
	}))
	defer server.Close()
	client, _ := newTestClient(
		t,
		server.URL,
		&directSender{},
		&recordingProvider{},
		[]transport.EgressRouteID{"route-a"},
		nil,
	)
	_, err := client.PlaceOrder(context.Background(), PlaceOrderRequest{
		Category: CategorySpot, Symbol: "BTCUSDT", Quantity: "100",
		Side: SideBuy, OrderType: OrderTypeMarket, ClientOrderID: "timeout-1",
	})
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceOrder() error = %v, want ErrUnknownExecutionState", err)
	}
}

func TestClassifyError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    int
		code      string
		operation commonexchange.OperationKind
		category  trade.ErrorCategory
		retryable bool
	}{
		{"rate limit", 429, "429", commonexchange.OperationRead, trade.ErrorRateLimited, true},
		{"signature", 400, "40009", commonexchange.OperationRead, trade.ErrorAuthentication, false},
		{"IP permission", 403, "40018", commonexchange.OperationRead, trade.ErrorAuthorization, false},
		{"order missing", 400, "43001", commonexchange.OperationRead, trade.ErrorOrderNotFound, false},
		{"balance", 400, "43012", commonexchange.OperationMutation, trade.ErrorInsufficientBalance, false},
		{"read unavailable", 500, "25000", commonexchange.OperationRead, trade.ErrorExchangeUnavailable, true},
		{"mutation unknown", 500, "40725", commonexchange.OperationMutation, trade.ErrorUnknownExecutionState, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			category, retryable := classifyError(test.status, test.code, test.operation)
			if category != test.category || retryable != test.retryable {
				t.Fatalf("classifyError() = (%s, %t), want (%s, %t)", category, retryable, test.category, test.retryable)
			}
		})
	}
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
		t.Fatalf("exchange.NewExecutor() error = %v", err)
	}
	client, err := New(Config{
		Executor: executor,
		Credentials: &credential.Descriptor{
			AccountID: "bitget-main", Exchange: model.ExchangeBitget,
			SecretRef:             "secret/bitget/main",
			Permissions:           []credential.Permission{credential.PermissionRead, credential.PermissionTrade},
			AllowedEgressRouteIDs: allowedRoutes,
		},
		CredentialProvider:   provider,
		DefaultEgressRouteID: "route-a",
		BaseURL:              baseURL,
		AllowInsecureHTTP:    true,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, limiter
}

func verifySignedRequest(t *testing.T, request *http.Request, secret []byte, now time.Time) bool {
	t.Helper()
	if request.Header.Get("ACCESS-KEY") != "test-api-key" ||
		request.Header.Get("ACCESS-PASSPHRASE") != "test-passphrase" {
		t.Errorf("Bitget credential headers are invalid")
		return false
	}
	timestamp := request.Header.Get("ACCESS-TIMESTAMP")
	if timestamp != "1700000000000" && !now.IsZero() {
		t.Errorf("ACCESS-TIMESTAMP = %q", timestamp)
		return false
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Errorf("io.ReadAll() error = %v", err)
		return false
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	preHash := signaturePayload(timestamp, request.Method, request.URL.Path, request.URL.RawQuery, body)
	want, err := SignHMACSHA256(secret, preHash)
	if err != nil {
		t.Errorf("SignHMACSHA256() error = %v", err)
		return false
	}
	if request.Header.Get("ACCESS-SIGN") != want {
		t.Errorf("ACCESS-SIGN = %q, want %q", request.Header.Get("ACCESS-SIGN"), want)
		return false
	}
	return true
}

func allZero(value []byte) bool {
	for _, item := range value {
		if item != 0 {
			return false
		}
	}
	return true
}
