package bybit

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
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
		APIKey:    []byte("test-api-key"),
		SecretKey: []byte("test-secret"),
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

func TestClientSpotAndLinearLifecycle(t *testing.T) {
	t.Parallel()

	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("X-Bapi-Limit", "20")
		writer.Header().Set("X-Bapi-Limit-Status", "19")
		if isPrivatePath(request.URL.Path) && !verifySignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"retCode":10004,"retMsg":"error sign","result":{},"time":1700000000000}`, http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/v5/market/time":
			_, _ = io.WriteString(writer, `{"retCode":0,"retMsg":"OK","result":{"timeSecond":"1700000000","timeNano":"1700000000000000000"},"time":1700000000000}`)
		case "/v5/market/instruments-info":
			_, _ = io.WriteString(writer, `{"retCode":0,"retMsg":"OK","result":{"category":"spot","list":[{"symbol":"BTCUSDT","status":"Trading","baseCoin":"BTC","quoteCoin":"USDT","priceFilter":{"tickSize":"0.01"},"lotSizeFilter":{"minOrderQty":"0.00001","minOrderAmt":"1"}}],"nextPageCursor":"next-market"},"time":1700000000000}`)
		case "/v5/market/tickers":
			_, _ = io.WriteString(writer, `{"retCode":0,"retMsg":"OK","result":{"category":"linear","list":[{"symbol":"BTCUSDT","lastPrice":"64000","markPrice":"64001","fundingRate":"0.0001"}]},"time":1700000000000}`)
		case "/v5/market/orderbook":
			_, _ = io.WriteString(writer, `{"retCode":0,"retMsg":"OK","result":{"s":"BTCUSDT","b":[["64000","1.5"]],"a":[["64001","2.5"]],"ts":1700000000000,"u":12,"seq":13,"cts":1699999999999},"time":1700000000000}`)
		case "/v5/market/recent-trade":
			_, _ = io.WriteString(writer, `{"retCode":0,"retMsg":"OK","result":{"category":"spot","list":[{"execId":"9","symbol":"BTCUSDT","price":"64000","size":"0.01","side":"Buy","time":"1700000000000"}]},"time":1700000000000}`)
		case "/v5/market/kline":
			_, _ = io.WriteString(writer, `{"retCode":0,"retMsg":"OK","result":{"category":"linear","symbol":"BTCUSDT","list":[["1700000000000","1","2","0.5","1.5","10","15"]]},"time":1700000000000}`)
		case "/v5/account/wallet-balance":
			_, _ = io.WriteString(writer, `{"retCode":0,"retMsg":"OK","result":{"list":[{"accountType":"UNIFIED","totalEquity":"1000","totalAvailableBalance":"900","coin":[{"coin":"USDT","walletBalance":"1000","locked":"100"}]}]},"time":1700000000000}`)
		case "/v5/position/list":
			_, _ = io.WriteString(writer, `{"retCode":0,"retMsg":"OK","result":{"category":"linear","list":[{"symbol":"BTCUSDT","side":"Buy","size":"0.01","avgPrice":"64000","markPrice":"64100","positionIdx":1}],"nextPageCursor":"next-position"},"time":1700000000000}`)
		case "/v5/order/create":
			body, _ := io.ReadAll(request.Body)
			if string(body) != `{"category":"linear","symbol":"BTCUSDT","side":"Buy","orderType":"Limit","qty":"0.01","price":"64000","timeInForce":"GTC","positionIdx":1,"orderLinkId":"strategy-1"}` {
				http.Error(writer, `{"retCode":10001,"retMsg":"unexpected body","result":{},"time":1700000000000}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"retCode":0,"retMsg":"OK","result":{"orderId":"42","orderLinkId":"strategy-1"},"time":1700000000000}`)
		case "/v5/order/realtime":
			if request.URL.Query().Get("orderId") == "42" {
				_, _ = io.WriteString(writer, `{"retCode":0,"retMsg":"OK","result":{"category":"linear","list":[{"orderId":"42","orderLinkId":"strategy-1","symbol":"BTCUSDT","side":"Buy","orderType":"Limit","price":"64000","qty":"0.01","orderStatus":"New"}],"nextPageCursor":""},"time":1700000000000}`)
				return
			}
			_, _ = io.WriteString(writer, `{"retCode":0,"retMsg":"OK","result":{"category":"linear","list":[{"orderId":"42","symbol":"BTCUSDT","orderStatus":"New"}],"nextPageCursor":"next-order"},"time":1700000000000}`)
		case "/v5/order/cancel":
			_, _ = io.WriteString(writer, `{"retCode":0,"retMsg":"OK","result":{"orderId":"42","orderLinkId":"strategy-1"},"time":1700000000000}`)
		case "/v5/order/history":
			_, _ = io.WriteString(writer, `{"retCode":0,"retMsg":"OK","result":{"category":"spot","list":[{"orderId":"41","symbol":"BTCUSDT","orderStatus":"Filled"}],"nextPageCursor":""},"time":1700000000000}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, limiter := newTestClient(
		t, server.URL, sender, provider,
		[]transport.EgressRouteID{"route-a", "route-b"},
		func() time.Time { return fixedNow },
	)

	serverTime, err := client.ServerTime(context.Background())
	if err != nil || serverTime.UnixMilli() != fixedNow.UnixMilli() {
		t.Fatalf("ServerTime() = %v, error = %v", serverTime, err)
	}
	instruments, err := client.Instruments(
		context.Background(),
		InstrumentsRequest{Category: CategorySpot, Symbol: "BTCUSDT", Limit: 10},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || len(instruments.Instruments) != 1 ||
		instruments.Instruments[0].LotSizeFilter.MinimumOrderAmount != "1" || len(instruments.Raw) == 0 {
		t.Fatalf("Instruments() = %+v, error = %v", instruments, err)
	}
	tickers, err := client.Tickers(context.Background(), TickersRequest{Category: CategoryLinear})
	if err != nil || len(tickers) != 1 || tickers[0].MarkPrice != "64001" || len(tickers[0].Raw) == 0 {
		t.Fatalf("Tickers() = %+v, error = %v", tickers, err)
	}
	book, err := client.OrderBook(
		context.Background(), OrderBookRequest{Category: CategorySpot, Symbol: "BTCUSDT", Limit: 50},
	)
	if err != nil || book.Bids[0][1] != "1.5" || book.Asks[0][0] != "64001" ||
		book.MatchingTime != 1699999999999 || len(book.Raw) == 0 {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	trades, err := client.RecentTrades(
		context.Background(), RecentTradesRequest{Category: CategorySpot, Symbol: "BTCUSDT", Limit: 1},
	)
	if err != nil || len(trades) != 1 || trades[0].ExecutionID != "9" || len(trades[0].Raw) == 0 {
		t.Fatalf("RecentTrades() = %+v, error = %v", trades, err)
	}
	candles, err := client.Candles(
		context.Background(),
		CandlesRequest{Category: CategoryLinear, Symbol: "BTCUSDT", Interval: Candle1Hour, Limit: 1},
	)
	if err != nil || len(candles) != 1 || candles[0].Close != "1.5" || candles[0].Turnover != "15" {
		t.Fatalf("Candles() = %+v, error = %v", candles, err)
	}
	accounts, err := client.WalletBalance(
		context.Background(), WalletBalanceRequest{Coins: []string{"USDT"}},
	)
	if err != nil || len(accounts) != 1 || accounts[0].Coins[0].WalletBalance != "1000" || len(accounts[0].Raw) == 0 {
		t.Fatalf("WalletBalance() = %+v, error = %v", accounts, err)
	}
	positions, err := client.Positions(
		context.Background(), PositionsRequest{Category: CategoryLinear, SettleCoin: "USDT", Limit: 50},
	)
	if err != nil || len(positions.Positions) != 1 || positions.Positions[0].Size != "0.01" ||
		positions.NextPageCursor != "next-position" || len(positions.Raw) == 0 {
		t.Fatalf("Positions() = %+v, error = %v", positions, err)
	}
	reference, err := client.PlaceOrder(
		context.Background(),
		PlaceOrderRequest{
			Category: CategoryLinear, Symbol: "BTCUSDT", Side: SideBuy,
			OrderType: OrderTypeLimit, Quantity: "0.01", Price: "64000",
			TimeInForce: TimeInForceGTC, PositionIndex: 1, OrderLinkID: "strategy-1",
		},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || reference.OrderID != "42" || len(reference.Raw) == 0 {
		t.Fatalf("PlaceOrder() = %+v, error = %v", reference, err)
	}
	order, err := client.OrderInfo(
		context.Background(), OrderInfoRequest{Category: CategoryLinear, OrderID: "42"},
	)
	if err != nil || order.OrderStatus != "New" || len(order.Raw) == 0 {
		t.Fatalf("OrderInfo() = %+v, error = %v", order, err)
	}
	if _, err := client.CancelOrder(
		context.Background(),
		CancelOrderRequest{Category: CategoryLinear, Symbol: "BTCUSDT", OrderID: "42"},
	); err != nil {
		t.Fatalf("CancelOrder() error = %v", err)
	}
	openOrders, err := client.OpenOrders(
		context.Background(), OpenOrdersRequest{Category: CategoryLinear, Symbol: "BTCUSDT"},
	)
	if err != nil || len(openOrders.Orders) != 1 || openOrders.NextPageCursor != "next-order" {
		t.Fatalf("OpenOrders() = %+v, error = %v", openOrders, err)
	}
	history, err := client.OrderHistory(
		context.Background(), OrderHistoryRequest{Category: CategorySpot, Symbol: "BTCUSDT"},
	)
	if err != nil || len(history.Orders) != 1 || history.Orders[0].OrderStatus != "Filled" {
		t.Fatalf("OrderHistory() = %+v, error = %v", history, err)
	}

	routes := sender.snapshot()
	if len(routes) != 13 || routes[1] != "route-b" || routes[8] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
	providerCalls, apiKey, secret := provider.snapshot()
	if providerCalls != 7 {
		t.Fatalf("provider calls = %d, want 7", providerCalls)
	}
	if !allZero(apiKey) || !allZero(secret) {
		t.Fatal("resolved credential byte slices were not overwritten")
	}
	snapshot, err := limiter.Snapshot("bybit:account:bybit-main:endpoint:v5/order/create:1second")
	if err != nil || snapshot.Used != 1 {
		t.Fatalf("order create limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestClientRejectsUnauthorizedRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{}
	client, _ := newTestClient(
		t, "http://127.0.0.1", &directSender{}, provider,
		[]transport.EgressRouteID{"route-a"}, nil,
	)
	_, err := client.WalletBalance(
		context.Background(), WalletBalanceRequest{}, trade.WithEgressRoute("route-b"),
	)
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("WalletBalance() error = %v, want authorization", err)
	}
	calls, _, _ := provider.snapshot()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestClientClassifiesRateLimitAndUnknownMutationState(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/v5/market/tickers" {
			_, _ = io.WriteString(writer, `{"retCode":10006,"retMsg":"Too many visits","result":{},"time":1700000000000}`)
			return
		}
		_, _ = io.WriteString(writer, `{"retCode":10016,"retMsg":"Server error","result":{},"time":1700000000000}`)
	}))
	defer server.Close()

	client, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, func() time.Time { return time.UnixMilli(1_700_000_000_000) },
	)
	_, err := client.Tickers(context.Background(), TickersRequest{Category: CategorySpot})
	if !errors.Is(err, trade.ErrRateLimited) {
		t.Fatalf("Tickers() error = %v, want rate limited", err)
	}
	_, err = client.PlaceOrder(
		context.Background(),
		PlaceOrderRequest{
			Category: CategorySpot, Symbol: "BTCUSDT", Side: SideBuy,
			OrderType: OrderTypeMarket, Quantity: "10", MarketUnit: MarketUnitQuoteCoin,
		},
	)
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceOrder() error = %v, want unknown execution state", err)
	}
}

func TestRequestValidation(t *testing.T) {
	t.Parallel()
	if err := (OrderBookRequest{
		Category: CategorySpot, Symbol: "BTCUSDT", Limit: 1000,
	}).validate(); err != nil {
		t.Fatalf("1,000-level Spot order book validation error = %v", err)
	}

	tests := []struct {
		name string
		err  error
	}{
		{name: "category", err: (TickersRequest{}).validate()},
		{name: "book limit", err: (OrderBookRequest{Category: CategorySpot, Symbol: "BTCUSDT", Limit: 1001}).validate()},
		{name: "position scope", err: (PositionsRequest{Category: CategoryLinear}).validate()},
		{name: "duplicate coin", err: (WalletBalanceRequest{Coins: []string{"USDT", "USDT"}}).validate()},
		{name: "missing identity", err: (CancelOrderRequest{Category: CategorySpot, Symbol: "BTCUSDT"}).validate()},
		{name: "linear open scope", err: (OpenOrdersRequest{Category: CategoryLinear}).validate()},
		{name: "Spot reduce only", err: (PlaceOrderRequest{Category: CategorySpot, Symbol: "BTCUSDT", Side: SideBuy, OrderType: OrderTypeMarket, Quantity: "1", ReduceOnly: true}).validate()},
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

	payload := []byte("1700000000000test-api-key5000category=spot&symbol=BTCUSDT")
	got, err := SignHMACSHA256([]byte("test-secret"), payload)
	if err != nil {
		t.Fatalf("SignHMACSHA256() error = %v", err)
	}
	mac := hmac.New(sha256.New, []byte("test-secret"))
	_, _ = mac.Write(payload)
	want := hex.EncodeToString(mac.Sum(nil))
	if got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
}

func isPrivatePath(path string) bool {
	return path == "/v5/account/wallet-balance" || path == "/v5/position/list" ||
		path == "/v5/order/create" || path == "/v5/order/realtime" ||
		path == "/v5/order/cancel" || path == "/v5/order/history"
}

func verifySignedRequest(t *testing.T, request *http.Request, secret []byte, now time.Time) bool {
	t.Helper()
	timestamp := request.Header.Get("X-BAPI-TIMESTAMP")
	if timestamp != strconv.FormatInt(now.UnixMilli(), 10) ||
		request.Header.Get("X-BAPI-API-KEY") != "test-api-key" ||
		request.Header.Get("X-BAPI-RECV-WINDOW") != "5000" {
		return false
	}
	content := []byte(request.URL.RawQuery)
	if request.Method == http.MethodPost {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
		content = body
	}
	payload := signaturePayload(timestamp, []byte("test-api-key"), 5000, content)
	want, err := SignHMACSHA256(secret, payload)
	if err != nil {
		t.Fatalf("sign expected request: %v", err)
	}
	return hmac.Equal([]byte(request.Header.Get("X-BAPI-SIGN")), []byte(want))
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
			AccountID: "bybit-main", Exchange: model.ExchangeBybit, SecretRef: "secret/bybit-main",
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
