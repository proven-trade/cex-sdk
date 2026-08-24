package coinone

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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

type errorSender struct{}

func (errorSender) Do(context.Context, transport.EgressRouteID, *http.Request) (*http.Response, error) {
	return nil, errors.New("network disconnected")
}

type recordingProvider struct {
	mu         sync.Mutex
	calls      int
	lastKey    []byte
	lastSecret []byte
}

func (provider *recordingProvider) Resolve(context.Context, string) (credential.Material, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.calls++
	material := credential.Material{APIKey: []byte("access-key"), SecretKey: []byte("secret-key")}
	provider.lastKey = material.APIKey
	provider.lastSecret = material.SecretKey
	return material, nil
}

func (provider *recordingProvider) snapshot() (int, []byte, []byte) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.calls, slices.Clone(provider.lastKey), slices.Clone(provider.lastSecret)
}

func TestClientPublicAndPrivateLifecycle(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(request.URL.Path, "/public/") {
			writer.Header().Set("Public-Ratelimit-Remaining", "1195")
		} else {
			verifySignedRequest(t, request, []byte("secret-key"), "123e4567-e89b-42d3-a456-426614174000")
			if request.URL.Path == "/v2.1/account/balance/all" {
				writer.Header().Set("Private-Ratelimit-Remaining", "79")
			} else {
				writer.Header().Set("Private-Order-Ratelimit-Remaining", "35")
			}
		}
		switch request.URL.Path {
		case "/public/v2/markets/KRW":
			_, _ = io.WriteString(writer, `{"result":"success","error_code":"0","server_time":1700000000000,"markets":[{"quote_currency":"KRW","target_currency":"BTC","price_unit":"1000","qty_unit":"0.00000001","min_order_amount":"5000","order_book_units":["1000","5000"],"maintenance_status":0,"trade_status":1,"order_types":["LIMIT","MARKET"]}]}`)
		case "/public/v2/orderbook/KRW/BTC":
			if request.URL.Query().Get("size") != "16" || request.URL.Query().Get("order_book_unit") != "1000" {
				t.Errorf("order book query = %q", request.URL.RawQuery)
			}
			_, _ = io.WriteString(writer, `{"result":"success","error_code":"0","timestamp":1700000000000,"id":"book-1","quote_currency":"KRW","target_currency":"BTC","order_book_unit":"1000","bids":[{"price":"64000000","qty":"0.1"}],"asks":[{"price":"64001000","qty":"0.2"}]}`)
		case "/public/v2/trades/KRW/BTC":
			_, _ = io.WriteString(writer, `{"result":"success","error_code":"0","server_time":1700000000000,"quote_currency":"KRW","target_currency":"BTC","transactions":[{"id":"trade-1","timestamp":1700000000000,"price":"64000000","qty":"0.01","is_seller_maker":false}]}`)
		case "/public/v2/ticker_new/KRW/BTC":
			_, _ = io.WriteString(writer, `{"result":"success","error_code":"0","server_time":1700000000000,"tickers":[{"quote_currency":"KRW","target_currency":"BTC","timestamp":1700000000000,"high":"65000000","low":"62000000","first":"63000000","last":"64000000","quote_volume":"1000000000","target_volume":"15.5","best_asks":[{"price":"64001000","qty":"0.2"}],"best_bids":[{"price":"64000000","qty":"0.1"}],"id":"ticker-1","yesterday_last":"63000000"}]}`)
		case "/public/v2/chart/KRW/BTC":
			_, _ = io.WriteString(writer, `{"result":"success","error_code":"0","is_last":true,"chart":[{"timestamp":1700000000000,"open":"63000000","high":"65000000","low":"62000000","close":"64000000","target_volume":"10.5","quote_volume":"670000000"}]}`)
		case "/v2.1/account/balance/all":
			_, _ = io.WriteString(writer, `{"result":"success","error_code":"0","balances":[{"available":"1000000","limit":"0","average_price":"0","currency":"KRW"}]}`)
		case "/v2.1/order":
			body := readRequestBody(t, request)
			want := `{"access_token":"access-key","nonce":"123e4567-e89b-42d3-a456-426614174000","side":"BUY","quote_currency":"KRW","target_currency":"BTC","type":"LIMIT","price":"64000000","qty":"0.01","post_only":false,"user_order_id":"strategy-1"}`
			if string(body) != want {
				t.Errorf("order body = %s, want %s", body, want)
			}
			_, _ = io.WriteString(writer, `{"result":"success","error_code":"0","order_id":"order-1"}`)
		case "/v2.1/order/detail":
			_, _ = io.WriteString(writer, `{"result":"success","error_code":"0","order_id":"order-1","user_order_id":"strategy-1","type":"LIMIT","quote_currency":"KRW","target_currency":"BTC","status":"LIVE","side":"BUY","fee":"0","fee_rate":"0.002","average_executed_price":"0","price":"64000000","original_qty":"0.01","executed_qty":"0","remain_qty":"0.01","is_triggered":null}`)
		case "/v2.1/order/cancel":
			_, _ = io.WriteString(writer, `{"result":"success","error_code":"0","order_id":"order-1","price":"64000000","qty":"0.01","remain_qty":"0.01","side":"BUY","original_qty":"0.01","traded_qty":"0","canceled_qty":"0.01","fee":"0","fee_rate":"0.002","avg_price":"0","canceled_at":1700000001000,"ordered_at":1700000000000}`)
		case "/v2.1/order/active_orders":
			_, _ = io.WriteString(writer, `{"result":"success","error_code":"0","active_orders":[{"order_id":"order-2","type":"STOP_LIMIT","side":"SELL","quote_currency":"KRW","target_currency":"BTC","price":"65000000","original_qty":"0.02","remain_qty":"0.02","is_triggered":false,"trigger_price":"64500000"}]}`)
		case "/v2.1/order/completed_orders/all":
			_, _ = io.WriteString(writer, `{"result":"success","error_code":"0","completed_orders":[{"trade_id":"trade-2","order_id":"order-0","quote_currency":"KRW","target_currency":"BTC","order_type":"LIMIT","is_ask":false,"is_maker":true,"price":"63000000","qty":"0.01","timestamp":1700000000000,"fee_rate":"0.002","fee":"126","fee_currency":"KRW"}]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, limiter := newTestClient(t, server.URL, sender, provider, []transport.EgressRouteID{"route-a", "route-b"})

	markets, err := client.Markets(context.Background(), MarketsRequest{QuoteCurrency: "KRW"}, trade.WithEgressRoute("route-b"))
	if err != nil || len(markets) != 1 || markets[0].MinimumAmount != "5000" || len(markets[0].Raw) == 0 {
		t.Fatalf("Markets() = %+v, error = %v", markets, err)
	}
	book, err := client.OrderBook(context.Background(), OrderBookRequest{QuoteCurrency: "KRW", TargetCurrency: "BTC", Size: 16, OrderBookUnit: "1000"}, trade.WithEgressRoute("route-b"))
	if err != nil || book.Bids[0].Price != "64000000" || len(book.Raw) == 0 {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	trades, err := client.RecentTrades(context.Background(), RecentTradesRequest{QuoteCurrency: "KRW", TargetCurrency: "BTC", Size: 10})
	if err != nil || len(trades) != 1 || trades[0].ID != "trade-1" || len(trades[0].Raw) == 0 {
		t.Fatalf("RecentTrades() = %+v, error = %v", trades, err)
	}
	ticker, err := client.Ticker(context.Background(), TickerRequest{QuoteCurrency: "KRW", TargetCurrency: "BTC", AdditionalData: true})
	if err != nil || ticker.Last != "64000000" || ticker.YesterdayLast != "63000000" || len(ticker.Raw) == 0 {
		t.Fatalf("Ticker() = %+v, error = %v", ticker, err)
	}
	candles, err := client.Candles(context.Background(), CandlesRequest{QuoteCurrency: "KRW", TargetCurrency: "BTC", Interval: Candle10Minutes, Size: 1})
	if err != nil || len(candles.Chart) != 1 || candles.Chart[0].Close != "64000000" || !candles.IsLast || len(candles.Raw) == 0 {
		t.Fatalf("Candles() = %+v, error = %v", candles, err)
	}
	balances, err := client.Accounts(context.Background())
	if err != nil || len(balances) != 1 || balances[0].Available != "1000000" || len(balances[0].Raw) == 0 {
		t.Fatalf("Accounts() = %+v, error = %v", balances, err)
	}
	placed, err := client.PlaceOrder(context.Background(), PlaceOrderRequest{
		Side: SideBuy, QuoteCurrency: "KRW", TargetCurrency: "BTC", Type: OrderTypeLimit,
		Price: "64000000", Quantity: "0.01", UserOrderID: "strategy-1",
	}, trade.WithEgressRoute("route-b"))
	if err != nil || placed.OrderID != "order-1" || len(placed.Raw) == 0 {
		t.Fatalf("PlaceOrder() = %+v, error = %v", placed, err)
	}
	detail, err := client.OrderInfo(context.Background(), OrderInfoRequest{OrderID: "order-1", QuoteCurrency: "KRW", TargetCurrency: "BTC"})
	if err != nil || detail.Status != "LIVE" || detail.RemainingQuantity != "0.01" || detail.IsTriggered != nil {
		t.Fatalf("OrderInfo() = %+v, error = %v", detail, err)
	}
	canceled, err := client.CancelOrder(context.Background(), CancelOrderRequest{UserOrderID: "strategy-1", QuoteCurrency: "KRW", TargetCurrency: "BTC"})
	if err != nil || canceled.CanceledQuantity != "0.01" || len(canceled.Raw) == 0 {
		t.Fatalf("CancelOrder() = %+v, error = %v", canceled, err)
	}
	active, err := client.ActiveOrders(context.Background(), ActiveOrdersRequest{QuoteCurrency: "KRW", TargetCurrency: "BTC", OrderTypes: []OrderType{OrderTypeLimit, OrderTypeStopLimit}})
	if err != nil || len(active) != 1 || active[0].TriggerPrice != "64500000" || active[0].IsTriggered == nil {
		t.Fatalf("ActiveOrders() = %+v, error = %v", active, err)
	}
	completed, err := client.CompletedOrders(context.Background(), CompletedOrdersRequest{
		AllMarkets: true, Size: 100, From: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		To: time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC),
	})
	if err != nil || len(completed) != 1 || completed[0].Fee != "126" || len(completed[0].Raw) == 0 {
		t.Fatalf("CompletedOrders() = %+v, error = %v", completed, err)
	}

	routes := sender.snapshot()
	if len(routes) != 11 || routes[0] != "route-b" || routes[1] != "route-b" || routes[6] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
	providerCalls, key, secret := provider.snapshot()
	if providerCalls != 6 || !allZero(key) || !allZero(secret) {
		t.Fatalf("provider calls = %d, key zero = %v, secret zero = %v", providerCalls, allZero(key), allZero(secret))
	}
	publicSnapshot, err := limiter.Snapshot("coinone:route:route-b:public:1minute")
	if err != nil || publicSnapshot.Used != 6 || publicSnapshot.Rule.Limit != 1200 {
		t.Fatalf("public limiter snapshot = %+v, error = %v", publicSnapshot, err)
	}
	privateSnapshot, err := limiter.Snapshot("coinone:account:coinone-portfolio:private:1second")
	if err != nil || privateSnapshot.Used != 1 || privateSnapshot.Rule.Limit != 80 {
		t.Fatalf("private limiter snapshot = %+v, error = %v", privateSnapshot, err)
	}
	orderSnapshot, err := limiter.Snapshot("coinone:account:coinone-portfolio:order:1second")
	if err != nil || orderSnapshot.Used != 9 || orderSnapshot.Rule.Limit != 40 {
		t.Fatalf("order limiter snapshot = %+v, error = %v", orderSnapshot, err)
	}
}

func TestClientRejectsCredentialRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, _ := newTestClient(t, "http://127.0.0.1", sender, provider, []transport.EgressRouteID{"route-a"})
	_, err := client.Accounts(context.Background(), trade.WithEgressRoute("route-b"))
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("Accounts() error = %v, want ErrAuthorization", err)
	}
	calls, _, _ := provider.snapshot()
	if calls != 0 || len(sender.snapshot()) != 0 {
		t.Fatalf("provider calls = %d, routes = %v", calls, sender.snapshot())
	}
}

func TestMutationNetworkAndDecodeFailuresAreUnknown(t *testing.T) {
	t.Parallel()

	request := PlaceOrderRequest{
		Side: SideBuy, QuoteCurrency: "KRW", TargetCurrency: "BTC", Type: OrderTypeLimit,
		Price: "64000000", Quantity: "0.01",
	}
	networkClient, _ := newTestClient(
		t, "http://127.0.0.1", errorSender{}, &recordingProvider{}, []transport.EgressRouteID{"route-a"},
	)
	_, err := networkClient.PlaceOrder(context.Background(), request)
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceOrder() network error = %v, want ErrUnknownExecutionState", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"result":"success","error_code":"0"}`)
	}))
	defer server.Close()
	decodeClient, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{}, []transport.EgressRouteID{"route-a"},
	)
	_, err = decodeClient.PlaceOrder(context.Background(), request)
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceOrder() decode error = %v, want ErrUnknownExecutionState", err)
	}
}

func TestLogicalRateLimitErrorOnSuccessStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"result":"error","error_code":"4","error_msg":"Blocked by rate limit"}`)
	}))
	defer server.Close()
	client, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{}, []transport.EgressRouteID{"route-a"},
	)
	_, err := client.Markets(context.Background(), MarketsRequest{QuoteCurrency: "KRW"})
	if !errors.Is(err, trade.ErrRateLimited) {
		t.Fatalf("Markets() error = %v, want ErrRateLimited", err)
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
		{"rate", 200, "4", commonexchange.OperationRead, trade.ErrorRateLimited, true},
		{"authentication", 200, "120", commonexchange.OperationRead, trade.ErrorAuthentication, false},
		{"IP authorization", 200, "10", commonexchange.OperationRead, trade.ErrorAuthorization, false},
		{"balance", 200, "103", commonexchange.OperationMutation, trade.ErrorInsufficientBalance, false},
		{"missing", 200, "104", commonexchange.OperationRead, trade.ErrorOrderNotFound, false},
		{"read unavailable", 200, "405", commonexchange.OperationRead, trade.ErrorExchangeUnavailable, true},
		{"mutation unknown", 200, "405", commonexchange.OperationMutation, trade.ErrorUnknownExecutionState, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			category, retryable := classifyError(test.status, test.code, test.operation)
			if category != test.category || retryable != test.retryable {
				t.Fatalf("classifyError() = (%s, %v), want (%s, %v)", category, retryable, test.category, test.retryable)
			}
		})
	}
}

func TestRequestValidation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	invalid := []error{
		(OrderBookRequest{QuoteCurrency: "KRW", TargetCurrency: "BTC", Size: 20}).validate(),
		(RecentTradesRequest{QuoteCurrency: "KRW", TargetCurrency: "BTC", Size: 20}).validate(),
		(CandlesRequest{QuoteCurrency: "KRW", TargetCurrency: "BTC", Interval: "7m"}).validate(),
		(PlaceOrderRequest{Side: SideBuy, QuoteCurrency: "KRW", TargetCurrency: "BTC", Type: OrderTypeMarket, Quantity: "1"}).validate(),
		(PlaceOrderRequest{Side: SideSell, QuoteCurrency: "KRW", TargetCurrency: "BTC", Type: OrderTypeMarket, Amount: "5000"}).validate(),
		(PlaceOrderRequest{Side: SideBuy, QuoteCurrency: "KRW", TargetCurrency: "BTC", Type: OrderTypeLimit, Price: "1", Quantity: "1", UserOrderID: "UPPER"}).validate(),
		(OrderInfoRequest{OrderID: "one", UserOrderID: "two", QuoteCurrency: "KRW", TargetCurrency: "BTC"}).validate(),
		(ActiveOrdersRequest{}).validate(),
		(CompletedOrdersRequest{AllMarkets: true, Size: 100, From: now.Add(-91 * 24 * time.Hour), To: now}).validate(now),
		(CompletedOrdersRequest{AllMarkets: true, Size: 100, From: now.Add(-time.Hour), To: now.Add(time.Hour)}).validate(now),
	}
	for index, err := range invalid {
		if !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("validation error %d = %v", index, err)
		}
	}
}

func newTestClient(
	t *testing.T,
	baseURL string,
	sender commonexchange.Sender,
	provider *recordingProvider,
	allowedRoutes []transport.EgressRouteID,
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
			AccountID: "coinone-portfolio", Exchange: model.ExchangeCoinone, SecretRef: "secret/coinone",
			Permissions:           []credential.Permission{credential.PermissionRead, credential.PermissionTrade},
			AllowedEgressRouteIDs: allowedRoutes,
		},
		CredentialProvider: provider, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true,
		NonceSource: func() (string, error) { return "123e4567-e89b-42d3-a456-426614174000", nil },
		Now:         func() time.Time { return time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, limiter
}

func verifySignedRequest(t *testing.T, request *http.Request, secret []byte, nonce string) {
	t.Helper()
	body := readRequestBody(t, request)
	payload := request.Header.Get("X-COINONE-PAYLOAD")
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || !slices.Equal(decoded, body) {
		t.Errorf("decoded payload = %s, body = %s, error = %v", decoded, body, err)
	}
	mac := hmac.New(sha512.New, secret)
	_, _ = mac.Write([]byte(payload))
	wantSignature := hex.EncodeToString(mac.Sum(nil))
	if request.Header.Get("X-COINONE-SIGNATURE") != wantSignature {
		t.Errorf("signature = %q, want %q", request.Header.Get("X-COINONE-SIGNATURE"), wantSignature)
	}
	var object struct {
		AccessToken string `json:"access_token"`
		Nonce       string `json:"nonce"`
	}
	if err := json.Unmarshal(body, &object); err != nil {
		t.Errorf("decode private request: %v", err)
	}
	if object.AccessToken != "access-key" || object.Nonce != nonce {
		t.Errorf("private credentials = %+v", object)
	}
}

func readRequestBody(t *testing.T, request *http.Request) []byte {
	t.Helper()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	request.Body = io.NopCloser(strings.NewReader(string(body)))
	return body
}

func allZero(value []byte) bool {
	for _, item := range value {
		if item != 0 {
			return false
		}
	}
	return true
}
