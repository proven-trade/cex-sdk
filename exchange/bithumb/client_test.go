package bithumb

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
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
		private := request.URL.Path == "/v1/accounts" || request.URL.Path == "/v2/orders" ||
			request.URL.Path == "/v1/order" || request.URL.Path == "/v2/order" ||
			request.URL.Path == "/v2/orders/pending" || request.URL.Path == "/v2/orders/history"
		if private {
			verifySignedRequest(t, request, []byte("secret-key"), "nonce-fixed", 1700000000123)
		}
		switch request.URL.Path {
		case "/v1/market/all":
			if request.URL.Query().Get("isDetails") != "true" {
				t.Errorf("market query = %q", request.URL.RawQuery)
			}
			_, _ = io.WriteString(writer, `[{"market":"KRW-BTC","korean_name":"비트코인","english_name":"Bitcoin","market_warning":"NONE"}]`)
		case "/v1/ticker":
			_, _ = io.WriteString(writer, `[{"market":"KRW-BTC","trade_price":64000.10,"acc_trade_volume_24h":12.345,"timestamp":1700000000000}]`)
		case "/v1/orderbook":
			_, _ = io.WriteString(writer, `[{"market":"KRW-BTC","timestamp":1700000000000,"total_ask_size":2.5,"total_bid_size":1.5,"orderbook_units":[{"ask_price":64001.0,"bid_price":64000.0,"ask_size":2.5,"bid_size":1.5}]}]`)
		case "/v1/trades/ticks":
			_, _ = io.WriteString(writer, `[{"market":"KRW-BTC","trade_price":64000.0,"trade_volume":0.01,"ask_bid":"BID","sequential_id":9}]`)
		case "/v1/candles/minutes/1":
			_, _ = io.WriteString(writer, `[{"market":"KRW-BTC","opening_price":63000.0,"high_price":65000.0,"low_price":62000.0,"trade_price":64000.0,"candle_acc_trade_volume":10.5,"unit":1}]`)
		case "/v1/accounts":
			_, _ = io.WriteString(writer, `[{"currency":"KRW","balance":"1000000.0","locked":"0.0","avg_buy_price":"0","avg_buy_price_modified":false,"unit_currency":"KRW"}]`)
		case "/v2/orders":
			body, _ := io.ReadAll(request.Body)
			if got, want := string(body), `{"market":"KRW-BTC","side":"bid","volume":"0.01","price":"64000","order_type":"limit","time_in_force":"post_only","client_order_id":"strategy-1"}`; got != want {
				t.Errorf("order body = %s, want %s", got, want)
			}
			writer.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(writer, `{"order_id":"order-1","market":"KRW-BTC","side":"bid","order_type":"limit","time_in_force":"post_only","client_order_id":"strategy-1","stp_type":"cancel_taker"}`)
		case "/v1/order":
			_, _ = io.WriteString(writer, `{"uuid":"order-1","client_order_id":"strategy-1","side":"bid","ord_type":"limit","state":"wait","market":"KRW-BTC","executed_funds":"0","stp_type":"cancel_taker"}`)
		case "/v2/order":
			_, _ = io.WriteString(writer, `{"order_id":"order-1","client_order_id":"strategy-1","created_at":"2026-08-25T00:00:00+09:00"}`)
		case "/v2/orders/pending":
			_, _ = io.WriteString(writer, `{"data":[{"order_id":"order-1","client_order_id":"strategy-1","state":"wait","market":"KRW-BTC","order_type":"limit"}],"has_next":true,"next_key":"cursor+/="}`)
		case "/v2/orders/history":
			_, _ = io.WriteString(writer, `{"data":[{"order_id":"order-0","state":"done","market":"KRW-BTC","order_type":"limit","executed_funds":"640"}],"has_next":false,"next_key":""}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, limiter := newTestClient(t, server.URL, sender, provider, []transport.EgressRouteID{"route-a", "route-b"})

	markets, err := client.Markets(context.Background(), MarketsRequest{IncludeDetails: true}, trade.WithEgressRoute("route-b"))
	if err != nil || len(markets) != 1 || markets[0].KoreanName != "비트코인" || len(markets[0].Raw) == 0 {
		t.Fatalf("Markets() = %+v, error = %v", markets, err)
	}
	tickers, err := client.Tickers(context.Background(), TickersRequest{Markets: []string{"KRW-BTC"}}, trade.WithEgressRoute("route-b"))
	if err != nil || len(tickers) != 1 || tickers[0].TradePrice != "64000.10" || len(tickers[0].Raw) == 0 {
		t.Fatalf("Tickers() = %+v, error = %v", tickers, err)
	}
	books, err := client.OrderBooks(context.Background(), OrderBooksRequest{Markets: []string{"KRW-BTC"}})
	if err != nil || len(books) != 1 || books[0].OrderBook[0].AskPrice != "64001.0" {
		t.Fatalf("OrderBooks() = %+v, error = %v", books, err)
	}
	trades, err := client.RecentTrades(context.Background(), RecentTradesRequest{Market: "KRW-BTC", Count: 1})
	if err != nil || len(trades) != 1 || trades[0].SequentialID != 9 {
		t.Fatalf("RecentTrades() = %+v, error = %v", trades, err)
	}
	candles, err := client.MinuteCandles(context.Background(), MinuteCandlesRequest{Market: "KRW-BTC", Unit: Minute1, Count: 1})
	if err != nil || len(candles) != 1 || candles[0].TradePrice != "64000.0" {
		t.Fatalf("MinuteCandles() = %+v, error = %v", candles, err)
	}
	balances, err := client.Accounts(context.Background())
	if err != nil || len(balances) != 1 || balances[0].Balance != "1000000.0" || len(balances[0].Raw) == 0 {
		t.Fatalf("Accounts() = %+v, error = %v", balances, err)
	}
	placed, err := client.PlaceOrder(context.Background(), PlaceOrderRequest{
		Market: "KRW-BTC", Side: SideBid, Volume: "0.01", Price: "64000",
		OrderType: OrderTypeLimit, TimeInForce: TimeInForcePostOnly, ClientOrderID: "strategy-1",
	}, trade.WithEgressRoute("route-b"))
	if err != nil || placed.OrderID != "order-1" || placed.STPType != "cancel_taker" || len(placed.Raw) == 0 {
		t.Fatalf("PlaceOrder() = %+v, error = %v", placed, err)
	}
	detail, err := client.OrderInfo(context.Background(), OrderInfoRequest{UUID: "order-1"})
	if err != nil || detail.State != OrderStateWait || detail.ExecutedFunds != "0" {
		t.Fatalf("OrderInfo() = %+v, error = %v", detail, err)
	}
	canceled, err := client.CancelOrder(context.Background(), CancelOrderRequest{ClientOrderID: "strategy-1"})
	if err != nil || canceled.OrderID != "order-1" || len(canceled.Raw) == 0 {
		t.Fatalf("CancelOrder() = %+v, error = %v", canceled, err)
	}
	pending, err := client.PendingOrders(context.Background(), PendingOrdersRequest{Market: "KRW-BTC", State: OrderStateWait, Limit: 50})
	if err != nil || len(pending.Data) != 1 || !pending.HasNext || pending.NextKey != "cursor+/=" || len(pending.Data[0].Raw) == 0 {
		t.Fatalf("PendingOrders() = %+v, error = %v", pending, err)
	}
	start := time.UnixMilli(1700000000000)
	end := start.Add(24 * time.Hour)
	history, err := client.OrderHistory(context.Background(), OrderHistoryRequest{
		AllMarkets: true, States: []OrderState{OrderStateDone, OrderStateCancel},
		StartTime: &start, EndTime: &end, Limit: 1000, NextKey: "cursor+/=",
	})
	if err != nil || len(history.Data) != 1 || history.Data[0].ExecutedFunds != "640" || len(history.Raw) == 0 {
		t.Fatalf("OrderHistory() = %+v, error = %v", history, err)
	}

	routes := sender.snapshot()
	if len(routes) != 11 || routes[0] != "route-b" || routes[1] != "route-b" || routes[6] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
	providerCalls, key, secret := provider.snapshot()
	if providerCalls != 6 || !allZero(key) || !allZero(secret) {
		t.Fatalf("provider calls = %d, key zero = %v, secret zero = %v", providerCalls, allZero(key), allZero(secret))
	}
	privateSnapshot, err := limiter.Snapshot("bithumb:account:bithumb-pocket:private:1second")
	if err != nil || privateSnapshot.Used != 6 || privateSnapshot.Rule.Limit != 140 {
		t.Fatalf("private limiter snapshot = %+v, error = %v", privateSnapshot, err)
	}
	orderSnapshot, err := limiter.Snapshot("bithumb:account:bithumb-pocket:order:1second")
	if err != nil || orderSnapshot.Used != 5 || orderSnapshot.Rule.Limit != 10 {
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

	provider := &recordingProvider{}
	networkClient, _ := newTestClient(
		t, "http://127.0.0.1", errorSender{}, provider, []transport.EgressRouteID{"route-a"},
	)
	request := PlaceOrderRequest{
		Market: "KRW-BTC", Side: SideBid, Volume: "0.01", Price: "64000", OrderType: OrderTypeLimit,
	}
	_, err := networkClient.PlaceOrder(context.Background(), request)
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceOrder() network error = %v, want ErrUnknownExecutionState", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(writer, `{invalid`)
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
		{"rate", 429, "", commonexchange.OperationRead, trade.ErrorRateLimited, true},
		{"JWT", 401, "jwt_verification", commonexchange.OperationRead, trade.ErrorAuthentication, false},
		{"expired", 401, "expired_jwt", commonexchange.OperationRead, trade.ErrorAuthentication, false},
		{"IP", 401, "NotAllowIP", commonexchange.OperationRead, trade.ErrorAuthorization, false},
		{"scope", 401, "out_of_scope", commonexchange.OperationRead, trade.ErrorAuthorization, false},
		{"balance", 400, "insufficient_funds_bid", commonexchange.OperationMutation, trade.ErrorInsufficientBalance, false},
		{"missing", 404, "order_not_found", commonexchange.OperationRead, trade.ErrorOrderNotFound, false},
		{"read unavailable", 500, "internal_error", commonexchange.OperationRead, trade.ErrorExchangeUnavailable, true},
		{"mutation unknown", 500, "internal_error", commonexchange.OperationMutation, trade.ErrorUnknownExecutionState, false},
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

	invalid := []error{
		(PendingOrdersRequest{}).validate(),
		(PendingOrdersRequest{Market: "KRW-BTC", Limit: 101}).validate(),
		(OrderHistoryRequest{}).validate(),
		(OrderInfoRequest{UUID: "order-1", ClientOrderID: "client-1"}).validate(),
		(CancelOrderRequest{}).validate(),
		(PlaceOrderRequest{Market: "BTC-KRW", Side: SideBid, Price: "1000", OrderType: OrderTypeBest, TimeInForce: TimeInForceIOC}).validate(),
		(PlaceOrderRequest{Market: "BTC-ETH", Side: SideBid, Volume: "1", Price: "1000", OrderType: OrderTypeLimit, TimeInForce: TimeInForceIOC}).validate(),
		(PlaceOrderRequest{Market: "KRW-BTC", Side: SideBid, Price: "1000", OrderType: OrderTypePrice, TimeInForce: TimeInForceIOC}).validate(),
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
			AccountID: "bithumb-pocket", Exchange: model.ExchangeBithumb, SecretRef: "secret/bithumb",
			Permissions:           []credential.Permission{credential.PermissionRead, credential.PermissionTrade},
			AllowedEgressRouteIDs: allowedRoutes,
		},
		CredentialProvider: provider, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true,
		NonceSource: func() (string, error) { return "nonce-fixed", nil },
		Now:         func() time.Time { return time.UnixMilli(1700000000123) },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, limiter
}

func verifySignedRequest(t *testing.T, request *http.Request, secret []byte, nonce string, timestamp int64) {
	t.Helper()
	authorization := request.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer ") {
		t.Errorf("Authorization = %q", authorization)
		return
	}
	parts := strings.Split(strings.TrimPrefix(authorization, "Bearer "), ".")
	if len(parts) != 3 {
		t.Errorf("JWT has %d parts", len(parts))
		return
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if parts[2] != base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) {
		t.Error("JWT signature is invalid")
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Errorf("decode JWT payload: %v", err)
		return
	}
	var payload struct {
		AccessKey    string `json:"access_key"`
		Nonce        string `json:"nonce"`
		Timestamp    int64  `json:"timestamp"`
		QueryHash    string `json:"query_hash"`
		QueryHashAlg string `json:"query_hash_alg"`
	}
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Errorf("decode JWT payload JSON: %v", err)
		return
	}
	if payload.AccessKey != "access-key" || payload.Nonce != nonce || payload.Timestamp != timestamp {
		t.Errorf("JWT payload = %+v", payload)
	}
	hashInput := rawQueryHashInput(t, request)
	if hashInput == "" {
		if payload.QueryHash != "" || payload.QueryHashAlg != "" {
			t.Errorf("unexpected query hash payload = %+v", payload)
		}
		return
	}
	if payload.QueryHash != QueryHash(hashInput) || payload.QueryHashAlg != "SHA512" {
		t.Errorf("JWT query hash payload = %+v, input = %q", payload, hashInput)
	}
}

func rawQueryHashInput(t *testing.T, request *http.Request) string {
	t.Helper()
	if request.Method == http.MethodPost {
		body, _ := io.ReadAll(request.Body)
		request.Body = io.NopCloser(strings.NewReader(string(body)))
		var object map[string]string
		if err := json.Unmarshal(body, &object); err != nil {
			t.Errorf("decode request body: %v", err)
			return ""
		}
		ordered := make(parameters, 0, len(object))
		for _, key := range []string{"market", "side", "volume", "price", "order_type", "time_in_force", "client_order_id"} {
			ordered.add(key, object[key])
		}
		return ordered.hashString()
	}
	values := parameters{}
	for _, part := range strings.Split(request.URL.RawQuery, "&") {
		if part == "" {
			continue
		}
		key, value, _ := strings.Cut(part, "=")
		decodedKey, keyErr := url.QueryUnescape(key)
		decodedValue, valueErr := url.QueryUnescape(value)
		if keyErr != nil || valueErr != nil {
			t.Errorf("decode raw query %q", part)
			return ""
		}
		values = append(values, parameter{key: decodedKey, value: decodedValue})
	}
	return values.hashString()
}

func allZero(value []byte) bool {
	for _, item := range value {
		if item != 0 {
			return false
		}
	}
	return true
}
