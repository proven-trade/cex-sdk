package upbit

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

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
		private := request.URL.Path == "/v1/accounts" || request.URL.Path == "/v1/orders" ||
			request.URL.Path == "/v1/order" || request.URL.Path == "/v1/orders/open" ||
			request.URL.Path == "/v1/orders/closed"
		if private {
			verifySignedRequest(t, request, []byte("secret-key"), "nonce-fixed")
			writer.Header().Set("Remaining-Req", "group=default; min=1800; sec=29")
		}
		switch request.URL.Path {
		case "/v1/market/all":
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
		case "/v1/orders":
			body, _ := io.ReadAll(request.Body)
			if string(body) != `{"market":"KRW-BTC","side":"bid","volume":"0.01","price":"64000","ord_type":"limit","time_in_force":"post_only","identifier":"strategy-1"}` {
				http.Error(writer, `{"error":{"name":"validation_error","message":"unexpected body"}}`, http.StatusBadRequest)
				return
			}
			writer.Header().Set("Remaining-Req", "group=order; min=720; sec=11")
			_, _ = io.WriteString(writer, `{"uuid":"order-1","side":"bid","ord_type":"limit","price":"64000","state":"wait","market":"KRW-BTC","volume":"0.01","remaining_volume":"0.01","identifier":"strategy-1"}`)
		case "/v1/order":
			state := "wait"
			if request.Method == http.MethodDelete {
				state = "cancel"
			}
			_, _ = io.WriteString(writer, `{"uuid":"order-1","side":"bid","ord_type":"limit","state":"`+state+`","market":"KRW-BTC","identifier":"strategy-1"}`)
		case "/v1/orders/open":
			_, _ = io.WriteString(writer, `[{"uuid":"order-1","state":"wait","market":"KRW-BTC"}]`)
		case "/v1/orders/closed":
			_, _ = io.WriteString(writer, `[{"uuid":"order-0","state":"done","market":"KRW-BTC"}]`)
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
	books, err := client.OrderBooks(context.Background(), OrderBooksRequest{Markets: []string{"KRW-BTC"}, Count: 5})
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
	if err != nil || len(balances) != 1 || balances[0].Balance != "1000000.0" {
		t.Fatalf("Accounts() = %+v, error = %v", balances, err)
	}
	order, err := client.PlaceOrder(context.Background(), PlaceOrderRequest{
		Market: "KRW-BTC", Side: SideBid, Volume: "0.01", Price: "64000",
		OrderType: OrderTypeLimit, TimeInForce: TimeInForcePostOnly, Identifier: "strategy-1",
	}, trade.WithEgressRoute("route-b"))
	if err != nil || order.UUID != "order-1" || len(order.Raw) == 0 {
		t.Fatalf("PlaceOrder() = %+v, error = %v", order, err)
	}
	queried, err := client.OrderInfo(context.Background(), OrderInfoRequest{UUID: "order-1"})
	if err != nil || queried.State != OrderStateWait {
		t.Fatalf("OrderInfo() = %+v, error = %v", queried, err)
	}
	canceled, err := client.CancelOrder(context.Background(), CancelOrderRequest{Identifier: "strategy-1"})
	if err != nil || canceled.State != OrderStateCancel {
		t.Fatalf("CancelOrder() = %+v, error = %v", canceled, err)
	}
	open, err := client.OpenOrders(context.Background(), OpenOrdersRequest{Market: "KRW-BTC", States: []OrderState{OrderStateWait, OrderStateWatch}})
	if err != nil || len(open) != 1 || len(open[0].Raw) == 0 {
		t.Fatalf("OpenOrders() = %+v, error = %v", open, err)
	}
	closed, err := client.ClosedOrders(context.Background(), ClosedOrdersRequest{AllMarkets: true, State: OrderStateDone})
	if err != nil || len(closed) != 1 || closed[0].State != OrderStateDone {
		t.Fatalf("ClosedOrders() = %+v, error = %v", closed, err)
	}

	routes := sender.snapshot()
	if len(routes) != 11 || routes[0] != "route-b" || routes[1] != "route-b" || routes[6] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
	providerCalls, key, secret := provider.snapshot()
	if providerCalls != 6 || !allZero(key) || !allZero(secret) {
		t.Fatalf("provider calls = %d, key zero = %v, secret zero = %v", providerCalls, allZero(key), allZero(secret))
	}
	snapshot, err := limiter.Snapshot("upbit:account:upbit-pocket:order:1second")
	if err != nil || snapshot.Used != 1 || snapshot.Rule.Limit != 12 {
		t.Fatalf("order limiter snapshot = %+v, error = %v", snapshot, err)
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

func TestMutationServerErrorIsUnknownExecutionState(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(writer, `{"error":{"name":"internal_error","message":"temporary"}}`)
	}))
	defer server.Close()
	client, _ := newTestClient(t, server.URL, &directSender{}, &recordingProvider{}, []transport.EgressRouteID{"route-a"})
	_, err := client.PlaceOrder(context.Background(), PlaceOrderRequest{
		Market: "KRW-BTC", Side: SideBid, Volume: "0.01", Price: "64000", OrderType: OrderTypeLimit,
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
		{"rate", 429, "", commonexchange.OperationRead, trade.ErrorRateLimited, true},
		{"teapot", 418, "", commonexchange.OperationRead, trade.ErrorRateLimited, true},
		{"JWT", 401, "jwt_verification", commonexchange.OperationRead, trade.ErrorAuthentication, false},
		{"IP", 401, "no_authorization_ip", commonexchange.OperationRead, trade.ErrorAuthorization, false},
		{"scope", 403, "out_of_scope", commonexchange.OperationRead, trade.ErrorAuthorization, false},
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

func TestOrderListRequiresExplicitAllMarkets(t *testing.T) {
	t.Parallel()

	if err := (OpenOrdersRequest{}).validate(); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("OpenOrdersRequest.validate() error = %v", err)
	}
	if err := (ClosedOrdersRequest{}).validate(); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("ClosedOrdersRequest.validate() error = %v", err)
	}
}

func TestParseRemainingRequest(t *testing.T) {
	t.Parallel()

	group, remaining, ok := parseRemainingRequest("group=order; min=720; sec=11")
	if !ok || group != "order" || remaining != 11 {
		t.Fatalf("parseRemainingRequest() = (%q, %d, %v)", group, remaining, ok)
	}
}

func newTestClient(
	t *testing.T,
	baseURL string,
	sender *directSender,
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
			AccountID: "upbit-pocket", Exchange: model.ExchangeUpbit, SecretRef: "secret/upbit",
			Permissions:           []credential.Permission{credential.PermissionRead, credential.PermissionTrade},
			AllowedEgressRouteIDs: allowedRoutes,
		},
		CredentialProvider: provider, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true,
		NonceSource: func() (string, error) { return "nonce-fixed", nil },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, limiter
}

func verifySignedRequest(t *testing.T, request *http.Request, secret []byte, nonce string) {
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
	mac := hmac.New(sha512.New, secret)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if parts[2] != base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) {
		t.Error("JWT signature is invalid")
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Errorf("decode JWT payload: %v", err)
		return
	}
	var payload map[string]string
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Errorf("decode JWT payload JSON: %v", err)
		return
	}
	if payload["access_key"] != "access-key" || payload["nonce"] != nonce {
		t.Errorf("JWT payload = %v", payload)
	}
	hashInput := request.URL.RawQuery
	if request.Method == http.MethodPost {
		body, _ := io.ReadAll(request.Body)
		request.Body = io.NopCloser(strings.NewReader(string(body)))
		var object map[string]string
		if err := json.Unmarshal(body, &object); err != nil {
			t.Errorf("decode request body: %v", err)
			return
		}
		ordered := make(parameters, 0, len(object))
		for _, key := range []string{"market", "side", "volume", "price", "ord_type", "time_in_force", "identifier", "smp_type"} {
			if value := object[key]; value != "" {
				ordered.add(key, value)
			}
		}
		hashInput, _ = ordered.hashString()
	} else {
		hashInput, _ = parametersFromRawQuery(request.URL.RawQuery).hashString()
	}
	if hashInput == "" {
		if payload["query_hash"] != "" || payload["query_hash_alg"] != "" {
			t.Errorf("unexpected query hash payload = %v", payload)
		}
		return
	}
	if payload["query_hash"] != QueryHash(hashInput) || payload["query_hash_alg"] != "SHA512" {
		t.Errorf("JWT query hash payload = %v, input = %q", payload, hashInput)
	}
}

func parametersFromRawQuery(raw string) parameters {
	values := parameters{}
	for _, part := range strings.Split(raw, "&") {
		if part == "" {
			continue
		}
		key, value, _ := strings.Cut(part, "=")
		values = append(values, parameter{key: key, value: value})
	}
	return values
}

func allZero(value []byte) bool {
	for _, item := range value {
		if item != 0 {
			return false
		}
	}
	return true
}
