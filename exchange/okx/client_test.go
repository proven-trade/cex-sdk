package okx

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

func TestClientSpotAndSwapLifecycle(t *testing.T) {
	t.Parallel()

	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Header.Get("x-simulated-trading") != "1" {
			http.Error(writer, `{"code":"50101","msg":"missing demo header","data":[]}`, http.StatusBadRequest)
			return
		}
		if isPrivatePath(request.URL.Path) && !verifySignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"code":"50113","msg":"Invalid signature","data":[]}`, http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/api/v5/public/time":
			_, _ = io.WriteString(writer, `{"code":"0","msg":"","data":[{"ts":"1700000000000"}]}`)
		case "/api/v5/public/instruments":
			_, _ = io.WriteString(writer, `{"code":"0","msg":"","data":[{"instType":"SPOT","instId":"BTC-USDT","baseCcy":"BTC","quoteCcy":"USDT","tickSz":"0.1","lotSz":"0.00001","minSz":"0.00001","state":"live"}]}`)
		case "/api/v5/market/tickers":
			_, _ = io.WriteString(writer, `{"code":"0","msg":"","data":[{"instType":"SWAP","instId":"BTC-USDT-SWAP","last":"64000","bidPx":"63999","askPx":"64001","vol24h":"100","ts":"1700000000000"}]}`)
		case "/api/v5/market/books":
			_, _ = io.WriteString(writer, `{"code":"0","msg":"","data":[{"asks":[["64001","2","0","3"]],"bids":[["64000","1","0","2"]],"ts":"1700000000000","checksum":7,"prevSeqId":8,"seqId":9}]}`)
		case "/api/v5/market/trades":
			_, _ = io.WriteString(writer, `{"code":"0","msg":"","data":[{"instId":"BTC-USDT","tradeId":"10","px":"64000","sz":"0.01","side":"buy","ts":"1700000000000"}]}`)
		case "/api/v5/market/candles":
			_, _ = io.WriteString(writer, `{"code":"0","msg":"","data":[["1700000000000","1","2","0.5","1.5","10","15","16","1"]]}`)
		case "/api/v5/account/balance":
			_, _ = io.WriteString(writer, `{"code":"0","msg":"","data":[{"totalEq":"1000","uTime":"1700000000000","details":[{"ccy":"USDT","eq":"1000","availBal":"900","frozenBal":"100"}]}]}`)
		case "/api/v5/account/positions":
			_, _ = io.WriteString(writer, `{"code":"0","msg":"","data":[{"instType":"SWAP","instId":"BTC-USDT-SWAP","mgnMode":"cross","posId":"20","posSide":"long","pos":"1","avgPx":"64000","markPx":"64100"}]}`)
		case "/api/v5/trade/order":
			if request.Method == http.MethodPost {
				body, _ := io.ReadAll(request.Body)
				if string(body) != `{"instId":"BTC-USDT-SWAP","tdMode":"cross","clOrdId":"strategy1","side":"buy","posSide":"long","ordType":"limit","sz":"1","px":"64000"}` {
					http.Error(writer, `{"code":"51000","msg":"unexpected body","data":[]}`, http.StatusBadRequest)
					return
				}
				_, _ = io.WriteString(writer, `{"code":"0","msg":"","data":[{"ordId":"42","clOrdId":"strategy1","sCode":"0","sMsg":"","ts":"1700000000000"}]}`)
				return
			}
			_, _ = io.WriteString(writer, `{"code":"0","msg":"","data":[{"instType":"SWAP","instId":"BTC-USDT-SWAP","ordId":"42","clOrdId":"strategy1","ordType":"limit","side":"buy","px":"64000","sz":"1","state":"live","posSide":"long","tdMode":"cross"}]}`)
		case "/api/v5/trade/cancel-order":
			_, _ = io.WriteString(writer, `{"code":"0","msg":"","data":[{"ordId":"42","clOrdId":"strategy1","sCode":"0","sMsg":"","ts":"1700000000000"}]}`)
		case "/api/v5/trade/orders-pending":
			_, _ = io.WriteString(writer, `{"code":"0","msg":"","data":[{"instType":"SWAP","instId":"BTC-USDT-SWAP","ordId":"42","state":"live"}]}`)
		case "/api/v5/trade/orders-history":
			_, _ = io.WriteString(writer, `{"code":"0","msg":"","data":[{"instType":"SPOT","instId":"BTC-USDT","ordId":"41","state":"filled"}]}`)
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
		InstrumentsRequest{InstrumentType: InstrumentTypeSpot, InstrumentID: "BTC-USDT"},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || len(instruments) != 1 || instruments[0].MinimumSize != "0.00001" || len(instruments[0].Raw) == 0 {
		t.Fatalf("Instruments() = %+v, error = %v", instruments, err)
	}
	tickers, err := client.Tickers(context.Background(), TickersRequest{InstrumentType: InstrumentTypeSwap})
	if err != nil || len(tickers) != 1 || tickers[0].LastPrice != "64000" || len(tickers[0].Raw) == 0 {
		t.Fatalf("Tickers() = %+v, error = %v", tickers, err)
	}
	book, err := client.OrderBook(context.Background(), OrderBookRequest{InstrumentID: "BTC-USDT", Size: 50})
	if err != nil || book.Asks[0].OrderCount != "3" || book.SequenceID != 9 || len(book.Raw) == 0 {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	trades, err := client.RecentTrades(
		context.Background(), RecentTradesRequest{InstrumentID: "BTC-USDT", Limit: 1},
	)
	if err != nil || len(trades) != 1 || trades[0].TradeID != "10" || len(trades[0].Raw) == 0 {
		t.Fatalf("RecentTrades() = %+v, error = %v", trades, err)
	}
	candles, err := client.Candles(
		context.Background(),
		CandlesRequest{InstrumentID: "BTC-USDT-SWAP", Interval: Candle1Hour, Limit: 1},
	)
	if err != nil || len(candles) != 1 || candles[0].Close != "1.5" || !candles[0].Confirmed {
		t.Fatalf("Candles() = %+v, error = %v", candles, err)
	}
	balance, err := client.Balance(context.Background(), BalanceRequest{Currencies: []string{"USDT"}})
	if err != nil || balance.TotalEquity != "1000" || balance.Details[0].AvailableBalance != "900" || len(balance.Raw) == 0 {
		t.Fatalf("Balance() = %+v, error = %v", balance, err)
	}
	positions, err := client.Positions(
		context.Background(), PositionsRequest{InstrumentType: InstrumentTypeSwap, InstrumentID: "BTC-USDT-SWAP"},
	)
	if err != nil || len(positions) != 1 || positions[0].Position != "1" || len(positions[0].Raw) == 0 {
		t.Fatalf("Positions() = %+v, error = %v", positions, err)
	}
	reference, err := client.PlaceOrder(
		context.Background(),
		PlaceOrderRequest{
			InstrumentType: InstrumentTypeSwap, InstrumentID: "BTC-USDT-SWAP",
			TradeMode: TradeModeCross, ClientOrderID: "strategy1", Side: SideBuy,
			PositionSide: PositionSideLong, OrderType: OrderTypeLimit, Quantity: "1", Price: "64000",
		},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || reference.OrderID != "42" || len(reference.Raw) == 0 {
		t.Fatalf("PlaceOrder() = %+v, error = %v", reference, err)
	}
	order, err := client.OrderInfo(
		context.Background(), OrderInfoRequest{InstrumentID: "BTC-USDT-SWAP", OrderID: "42"},
	)
	if err != nil || order.State != "live" || len(order.Raw) == 0 {
		t.Fatalf("OrderInfo() = %+v, error = %v", order, err)
	}
	if _, err := client.CancelOrder(
		context.Background(), CancelOrderRequest{InstrumentID: "BTC-USDT-SWAP", OrderID: "42"},
	); err != nil {
		t.Fatalf("CancelOrder() error = %v", err)
	}
	openOrders, err := client.OpenOrders(
		context.Background(), OpenOrdersRequest{InstrumentType: InstrumentTypeSwap, InstrumentID: "BTC-USDT-SWAP"},
	)
	if err != nil || len(openOrders.Orders) != 1 || len(openOrders.Raw) == 0 {
		t.Fatalf("OpenOrders() = %+v, error = %v", openOrders, err)
	}
	history, err := client.OrderHistory(
		context.Background(), OrderHistoryRequest{InstrumentType: InstrumentTypeSpot, InstrumentID: "BTC-USDT"},
	)
	if err != nil || len(history.Orders) != 1 || history.Orders[0].State != "filled" {
		t.Fatalf("OrderHistory() = %+v, error = %v", history, err)
	}

	routes := sender.snapshot()
	if len(routes) != 13 || routes[1] != "route-b" || routes[8] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
	calls, apiKey, secret, passphrase := provider.snapshot()
	if calls != 7 {
		t.Fatalf("provider calls = %d, want 7", calls)
	}
	if !allZero(apiKey) || !allZero(secret) || !allZero(passphrase) {
		t.Fatal("resolved credential byte slices were not overwritten")
	}
	snapshot, err := limiter.Snapshot(
		"okx:account:okx-main:endpoint:POST:api/v5/trade/order:BTC-USDT-SWAP:2seconds",
	)
	if err != nil || snapshot.Used != 1 {
		t.Fatalf("order limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestClientRejectsUnauthorizedRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{}
	client, _ := newTestClient(
		t, "http://127.0.0.1", &directSender{}, provider,
		[]transport.EgressRouteID{"route-a"}, nil,
	)
	_, err := client.Balance(
		context.Background(), BalanceRequest{}, trade.WithEgressRoute("route-b"),
	)
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("Balance() error = %v, want authorization", err)
	}
	calls, _, _, _ := provider.snapshot()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestClientClassifiesItemRateLimitAndUnknownMutationState(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/v5/market/tickers" {
			_, _ = io.WriteString(writer, `{"code":"50011","msg":"Requests too frequent","data":[]}`)
			return
		}
		_, _ = io.WriteString(writer, `{"code":"0","msg":"","data":[{"ordId":"","clOrdId":"strategy1","sCode":"50004","sMsg":"API endpoint request timeout"}]}`)
	}))
	defer server.Close()

	client, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, func() time.Time { return time.UnixMilli(1_700_000_000_000) },
	)
	_, err := client.Tickers(context.Background(), TickersRequest{InstrumentType: InstrumentTypeSpot})
	if !errors.Is(err, trade.ErrRateLimited) {
		t.Fatalf("Tickers() error = %v, want rate limited", err)
	}
	_, err = client.PlaceOrder(
		context.Background(),
		PlaceOrderRequest{
			InstrumentType: InstrumentTypeSpot, InstrumentID: "BTC-USDT",
			TradeMode: TradeModeCash, Side: SideBuy, OrderType: OrderTypeMarket,
			Quantity: "10", ClientOrderID: "strategy1", TargetCurrency: TargetCurrencyQuote,
		},
	)
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceOrder() error = %v, want unknown execution state", err)
	}
}

func TestRequestValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{name: "instrument type", err: (TickersRequest{}).validate()},
		{name: "book size", err: (OrderBookRequest{InstrumentID: "BTC-USDT", Size: 401}).validate()},
		{name: "position type", err: (PositionsRequest{InstrumentType: InstrumentTypeSpot}).validate()},
		{name: "duplicate currency", err: (BalanceRequest{Currencies: []string{"USDT", "USDT"}}).validate()},
		{name: "missing identity", err: (CancelOrderRequest{InstrumentID: "BTC-USDT"}).validate()},
		{name: "Spot margin mode", err: (PlaceOrderRequest{InstrumentType: InstrumentTypeSpot, InstrumentID: "BTC-USDT", TradeMode: TradeModeCross, Side: SideBuy, OrderType: OrderTypeMarket, Quantity: "1"}).validate()},
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

	payload := []byte("2020-12-08T09:08:57.715ZGET/api/v5/account/balance?ccy=BTC")
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
	return path == "/api/v5/account/balance" || path == "/api/v5/account/positions" ||
		path == "/api/v5/trade/order" || path == "/api/v5/trade/cancel-order" ||
		path == "/api/v5/trade/orders-pending" || path == "/api/v5/trade/orders-history"
}

func verifySignedRequest(t *testing.T, request *http.Request, secret []byte, now time.Time) bool {
	t.Helper()
	timestamp := request.Header.Get("OK-ACCESS-TIMESTAMP")
	if timestamp != now.UTC().Format(okxTimestampLayout) ||
		request.Header.Get("OK-ACCESS-KEY") != "test-api-key" ||
		request.Header.Get("OK-ACCESS-PASSPHRASE") != "test-passphrase" {
		return false
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	requestPath := request.URL.EscapedPath()
	if request.URL.RawQuery != "" {
		requestPath += "?" + request.URL.RawQuery
	}
	want, err := SignHMACSHA256(
		secret, signaturePayload(timestamp, request.Method, requestPath, body),
	)
	if err != nil {
		t.Fatalf("sign expected request: %v", err)
	}
	return hmac.Equal([]byte(request.Header.Get("OK-ACCESS-SIGN")), []byte(want))
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
			AccountID: "okx-main", Exchange: model.ExchangeOKX, SecretRef: "secret/okx-main",
			Permissions: []credential.Permission{
				credential.PermissionRead, credential.PermissionTrade,
			},
			AllowedEgressRouteIDs: allowedRoutes,
		},
		CredentialProvider: provider, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true, DemoTrading: true, Now: now,
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
