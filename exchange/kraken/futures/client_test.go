package futures

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
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

type errorSender struct {
	err error
}

func (sender errorSender) Do(
	context.Context,
	transport.EgressRouteID,
	*http.Request,
) (*http.Response, error) {
	return nil, sender.err
}

type recordingProvider struct {
	mu     sync.Mutex
	calls  int
	issued [][]byte
	secret []byte
}

func (provider *recordingProvider) Resolve(context.Context, string) (credential.Material, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.calls++
	material := credential.Material{
		APIKey: []byte("test-api-key"), SecretKey: cloneBytes(provider.secret),
	}
	provider.issued = append(provider.issued, material.APIKey, material.SecretKey)
	return material, nil
}

func (provider *recordingProvider) snapshot() (int, [][]byte) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.calls, slices.Clone(provider.issued)
}

func TestClientFuturesLifecycleAndPerRequestRoute(t *testing.T) {
	t.Parallel()

	fixedNow := time.UnixMilli(1_700_000_000_000)
	secret := []byte(base64.StdEncoding.EncodeToString([]byte("test-secret")))
	var nonceMu sync.Mutex
	var nonces []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(request.URL.Path, derivativesPrefix) && request.Header.Get("APIKey") != "" {
			nonce := request.Header.Get("Nonce")
			expected, signErr := SignAuthent(
				request.URL.RawQuery, nonce, strings.TrimPrefix(request.URL.Path, "/derivatives"), secret,
			)
			body, _ := io.ReadAll(request.Body)
			if signErr != nil || request.Header.Get("APIKey") != "test-api-key" ||
				request.Header.Get("Authent") != expected || len(body) != 0 {
				http.Error(writer, `{"result":"error","error":"authenticationError"}`, http.StatusUnauthorized)
				return
			}
			nonceMu.Lock()
			nonces = append(nonces, nonce)
			nonceMu.Unlock()
		}
		switch request.URL.Path {
		case derivativesPrefix + "instruments":
			if request.URL.Query().Get("contractType") != string(ContractTypeVanilla) ||
				request.URL.Query().Get("expired") != "true" {
				http.Error(writer, `{"result":"error","error":"invalidArgument"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"result":"success","serverTime":"2026-08-25T00:00:00Z","instruments":[{"symbol":"PI_XBTUSD","pair":"XBT:USD","base":"XBT","quote":"USD","type":"futures_vanilla","tickSize":0.5,"contractSize":"1","tradeable":true,"isExpired":false}]}`)
		case derivativesPrefix + "tickers":
			if request.URL.Query().Get("symbol") != "PI_XBTUSD" {
				http.Error(writer, `{"result":"error","error":"invalidArgument"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"result":"success","serverTime":"2026-08-25T00:00:00Z","tickers":[{"symbol":"PI_XBTUSD","markPrice":64000.25,"bid":"64000","ask":64000.5,"last":"64000.25","vol24h":123}]}`)
		case derivativesPrefix + "orderbook":
			_, _ = io.WriteString(writer, `{"result":"success","serverTime":"2026-08-25T00:00:00Z","orderBook":{"bids":[[64000,2.5]],"asks":[[64000.5,"1.25"]]}}`)
		case derivativesPrefix + "history":
			_, _ = io.WriteString(writer, `{"result":"success","serverTime":"2026-08-25T00:00:00Z","history":[{"trade_id":42,"price":64000.25,"side":"buy","size":"0.5","time":"2026-08-25T00:00:00Z","type":"fill"}]}`)
		case "/api/charts/v1/trade/PI_XBTUSD/1m":
			if request.URL.Query().Get("from") != "1700000000" || request.URL.Query().Get("count") != "10" {
				http.Error(writer, `{"error":"invalid candle query"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"candles":[{"time":1700000000000,"open":"63900","high":"64100","low":"63800","close":"64000","volume":12}],"more_candles":false}`)
		case derivativesPrefix + "accounts":
			_, _ = io.WriteString(writer, `{"result":"success","serverTime":"2026-08-25T00:00:00Z","accounts":{"cash":{"type":"cashAccount","currency":"USD","balances":{"XBT":"1.25"}},"multiCollateralMargin":{"type":"multiCollateralMarginAccount","portfolioValue":1000.5,"availableMargin":"800"}}}`)
		case derivativesPrefix + "openpositions":
			_, _ = io.WriteString(writer, `{"result":"success","openPositions":[{"side":"long","symbol":"PI_XBTUSD","price":63000,"size":"2","unrealizedPnl":2000}]}`)
		case derivativesPrefix + "openorders":
			_, _ = io.WriteString(writer, `{"result":"success","openOrders":[{"order_id":"ORDER-1","cliOrdId":"strategy-1","symbol":"PI_XBTUSD","side":"buy","orderType":"lmt","limitPrice":64000,"unfilledSize":1,"filledSize":0,"status":"untouched"}]}`)
		case derivativesPrefix + "fills":
			_, _ = io.WriteString(writer, `{"result":"success","fills":[{"fill_id":"FILL-1","symbol":"PI_XBTUSD","side":"buy","order_id":"ORDER-1","size":1,"price":"64000","fillTime":"2026-08-25T00:00:00Z","fillType":"maker"}]}`)
		case derivativesPrefix + "sendorder":
			if request.Method != http.MethodPost || request.URL.Query().Get("cliOrdId") != "strategy-2" {
				http.Error(writer, `{"result":"error","error":"invalidArgument"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"result":"success","sendStatus":{"order_id":"ORDER-2","status":"placed","receivedTime":"2026-08-25T00:00:00Z"}}`)
		case derivativesPrefix + "cancelorder":
			_, _ = io.WriteString(writer, `{"result":"success","cancelStatus":{"order_id":"ORDER-2","status":"cancelled","receivedTime":"2026-08-25T00:00:01Z"}}`)
		case derivativesPrefix + "orders/status":
			_, _ = io.WriteString(writer, `{"result":"success","orders":[{"order":{"orderId":"ORDER-2"},"status":"CANCELLED","updateReason":"USER"}]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{secret: secret}
	client, limiter := newTestClient(
		t, server.URL, sender, provider, []transport.EgressRouteID{"route-a", "route-b"}, fixedNow,
	)
	instruments, err := client.Instruments(
		context.Background(), InstrumentsRequest{
			ContractTypes: []ContractType{ContractTypeVanilla}, Expired: true,
		},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || len(instruments) != 1 || instruments[0].TickSize != "0.5" || len(instruments[0].Raw) == 0 {
		t.Fatalf("Instruments() = %+v, error = %v", instruments, err)
	}
	tickers, err := client.Tickers(
		context.Background(), TickersRequest{Symbols: []string{"PI_XBTUSD"}},
	)
	if err != nil || len(tickers) != 1 || tickers[0].MarkPrice != "64000.25" {
		t.Fatalf("Tickers() = %+v, error = %v", tickers, err)
	}
	book, err := client.OrderBook(context.Background(), OrderBookRequest{Symbol: "PI_XBTUSD"})
	if err != nil || len(book.Bids) != 1 || book.Bids[0].Size != "2.5" || book.ServerTime == "" {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	history, err := client.PublicHistory(
		context.Background(), PublicHistoryRequest{Symbol: "PI_XBTUSD"},
	)
	if err != nil || len(history.Trades) != 1 || history.Trades[0].TradeID != 42 {
		t.Fatalf("PublicHistory() = %+v, error = %v", history, err)
	}
	from := fixedNow
	candles, err := client.Candles(context.Background(), CandlesRequest{
		TickType: CandleTickTrade, Symbol: "PI_XBTUSD", Resolution: Candle1Minute,
		From: &from, Count: 10,
	})
	if err != nil || len(candles.Candles) != 1 || candles.Candles[0].Close != "64000" {
		t.Fatalf("Candles() = %+v, error = %v", candles, err)
	}
	accounts, err := client.Accounts(context.Background())
	if err != nil || len(accounts) != 2 || accounts[0].Name != "cash" || accounts[1].PortfolioValue != "1000.5" {
		t.Fatalf("Accounts() = %+v, error = %v", accounts, err)
	}
	positions, err := client.OpenPositions(context.Background())
	if err != nil || len(positions) != 1 || positions[0].UnrealizedPNL != "2000" {
		t.Fatalf("OpenPositions() = %+v, error = %v", positions, err)
	}
	orders, err := client.OpenOrders(context.Background())
	if err != nil || len(orders) != 1 || orders[0].OrderID != "ORDER-1" {
		t.Fatalf("OpenOrders() = %+v, error = %v", orders, err)
	}
	fills, err := client.Fills(context.Background(), FillsRequest{})
	if err != nil || len(fills) != 1 || fills[0].FillID != "FILL-1" {
		t.Fatalf("Fills() = %+v, error = %v", fills, err)
	}
	reference, err := client.PlaceOrder(
		context.Background(), PlaceOrderRequest{
			OrderType: OrderTypeLimit, Symbol: "PI_XBTUSD", Side: SideBuy,
			Size: "1", LimitPrice: "64000", ClientOrderID: "strategy-2",
		},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || reference.OrderID != "ORDER-2" || len(reference.Raw) == 0 {
		t.Fatalf("PlaceOrder() = %+v, error = %v", reference, err)
	}
	canceled, err := client.CancelOrder(
		context.Background(), CancelOrderRequest{OrderID: "ORDER-2"},
	)
	if err != nil || canceled.Status != "cancelled" || len(canceled.Raw) == 0 {
		t.Fatalf("CancelOrder() = %+v, error = %v", canceled, err)
	}
	statuses, err := client.OrderStatus(
		context.Background(), OrderStatusRequest{OrderIDs: []string{"ORDER-2"}},
	)
	if err != nil || len(statuses) != 1 || statuses[0].Status != "CANCELLED" || len(statuses[0].Order) == 0 {
		t.Fatalf("OrderStatus() = %+v, error = %v", statuses, err)
	}

	routes := sender.snapshot()
	if len(routes) != 12 || routes[0] != "route-b" || routes[9] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
	calls, issued := provider.snapshot()
	if calls != 7 {
		t.Fatalf("provider calls = %d, want 7", calls)
	}
	for _, secretBytes := range issued {
		if !allZero(secretBytes) {
			t.Fatal("resolved credential byte slices were not overwritten")
		}
	}
	nonceMu.Lock()
	for index := 1; index < len(nonces); index++ {
		if nonces[index] <= nonces[index-1] {
			t.Fatalf("nonces are not increasing: %v", nonces)
		}
	}
	nonceMu.Unlock()
	snapshot, err := limiter.Snapshot("kraken-futures:account:kraken-main:derivatives:10seconds")
	if err != nil || snapshot.Used != 29 {
		t.Fatalf("private limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestClientRejectsUnauthorizedRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{secret: []byte("dGVzdA==")}
	client, _ := newTestClient(
		t, "http://127.0.0.1", &directSender{}, provider,
		[]transport.EgressRouteID{"route-a"}, time.Now(),
	)
	_, err := client.Accounts(context.Background(), trade.WithEgressRoute("route-b"))
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("Accounts() error = %v, want authorization", err)
	}
	calls, _ := provider.snapshot()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestClientClassifiesFuturesEnvelopeError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"result":"error","error":"insufficientFunds"}`)
	}))
	defer server.Close()
	provider := &recordingProvider{secret: []byte("dGVzdA==")}
	client, _ := newTestClient(
		t, server.URL, &directSender{}, provider,
		[]transport.EgressRouteID{"route-a"}, time.Now(),
	)
	_, err := client.PlaceOrder(context.Background(), PlaceOrderRequest{
		OrderType: OrderTypeMarket, Symbol: "PI_XBTUSD", Side: SideBuy, Size: "1",
	})
	if !errors.Is(err, trade.ErrInsufficientBalance) {
		t.Fatalf("PlaceOrder() error = %v, want insufficient balance", err)
	}
}

func TestClientClassifiesOrderTransportFailureAsUnknownState(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{secret: []byte("dGVzdA==")}
	client, _ := newTestClient(
		t, "http://kraken-futures.example.test", errorSender{err: errors.New("connection reset")}, provider,
		[]transport.EgressRouteID{"route-a"}, time.Now(),
	)
	_, err := client.PlaceOrder(context.Background(), PlaceOrderRequest{
		OrderType: OrderTypeMarket, Symbol: "PI_XBTUSD", Side: SideBuy, Size: "1",
	})
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceOrder() error = %v, want unknown execution state", err)
	}
}

func TestClientNonceIsUniqueAcrossConcurrentRequests(t *testing.T) {
	t.Parallel()

	client := &Client{now: func() time.Time { return time.UnixMilli(1_700_000_000_000) }}
	const count = 200
	values := make(chan uint64, count)
	var wait sync.WaitGroup
	for range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			value, err := strconv.ParseUint(client.nextNonce(), 10, 64)
			if err != nil {
				t.Errorf("nextNonce() parse error = %v", err)
				return
			}
			values <- value
		}()
	}
	wait.Wait()
	close(values)
	seen := make(map[uint64]struct{}, count)
	for value := range values {
		seen[value] = struct{}{}
	}
	if len(seen) != count {
		t.Fatalf("unique nonce count = %d, want %d", len(seen), count)
	}
}

func TestRequestsRejectInvalidTradingInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
	}{
		{
			name: "시장가 가격 지정",
			err: PlaceOrderRequest{
				OrderType: OrderTypeMarket, Symbol: "PI_XBTUSD", Side: SideBuy,
				Size: "1", LimitPrice: "64000",
			}.validate(),
		},
		{
			name: "limit 가격 없음",
			err: PlaceOrderRequest{
				OrderType: OrderTypeLimit, Symbol: "PI_XBTUSD", Side: SideBuy, Size: "1",
			}.validate(),
		},
		{
			name: "취소 식별자 두 개",
			err:  CancelOrderRequest{OrderID: "ORDER-1", ClientOrderID: "client-1"}.validate(),
		},
		{
			name: "상태 식별자 없음",
			err:  OrderStatusRequest{}.validate(),
		},
	}
	for _, testCase := range cases {
		if !errors.Is(testCase.err, trade.ErrValidation) {
			t.Errorf("%s error = %v, want validation", testCase.name, testCase.err)
		}
	}
}

func TestDecimalRejectsNonNumericJSON(t *testing.T) {
	t.Parallel()

	var decimal Decimal
	if err := decimal.UnmarshalJSON([]byte("true")); err == nil {
		t.Fatal("Decimal.UnmarshalJSON() error = nil")
	}
}

func newTestClient(
	t *testing.T,
	baseURL string,
	sender commonexchange.Sender,
	provider credential.Provider,
	allowedRoutes []transport.EgressRouteID,
	now time.Time,
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
		t.Fatalf("exchange.NewExecutor() error = %v", err)
	}
	client, err := New(Config{
		Executor: executor,
		Credentials: &credential.Descriptor{
			AccountID: "kraken-main", Exchange: model.ExchangeKraken,
			SecretRef: "secret://kraken-futures", Permissions: []credential.Permission{
				credential.PermissionRead, credential.PermissionTrade,
			},
			AllowedEgressRouteIDs: allowedRoutes,
		},
		CredentialProvider: provider, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true,
		RequestTimeout: 2 * time.Second, PublicRequestsPerSecond: 100,
		DerivativesPointLimit: 500, DerivativesWindow: 10 * time.Second,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("futures.New() error = %v", err)
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
