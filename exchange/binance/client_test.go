package binance

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	calls  int
}

func (sender *directSender) Do(
	ctx context.Context,
	routeID transport.EgressRouteID,
	request *http.Request,
) (*http.Response, error) {
	sender.mu.Lock()
	sender.routes = append(sender.routes, routeID)
	sender.calls++
	sender.mu.Unlock()
	return http.DefaultClient.Do(request.Clone(ctx))
}

func (sender *directSender) snapshot() ([]transport.EgressRouteID, int) {
	sender.mu.Lock()
	defer sender.mu.Unlock()
	return slices.Clone(sender.routes), sender.calls
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
		SecretKey: []byte("test-secret-key"),
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

func TestClientPublicAndPrivateLifecycle(t *testing.T) {
	t.Parallel()

	secret := []byte("test-secret-key")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("X-MBX-USED-WEIGHT-1M", "50")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v3/ticker/price":
			if request.URL.Query().Get("symbol") != "BTCUSDT" {
				http.Error(writer, `{"code":-1121,"msg":"Invalid symbol."}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"symbol":"BTCUSDT","price":"64000.10"}`)
		case request.Method == http.MethodGet && request.URL.Path == "/api/v3/account":
			if !verifySignedRequest(t, request, secret) {
				http.Error(writer, `{"code":-1022,"msg":"Signature invalid."}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"canTrade":true,"accountType":"SPOT","balances":[{"asset":"USDT","free":"1000.00","locked":"0.00"}]}`)
		case request.Method == http.MethodPost && request.URL.Path == "/api/v3/order":
			if !verifySignedRequest(t, request, secret) {
				http.Error(writer, `{"code":-1022,"msg":"Signature invalid."}`, http.StatusBadRequest)
				return
			}
			writer.Header().Set("X-MBX-ORDER-COUNT-10S", "1")
			_, _ = io.WriteString(writer, `{"symbol":"BTCUSDT","orderId":42,"clientOrderId":"strategy-1","transactTime":1700000000000,"price":"64000.00","origQty":"0.001","executedQty":"0.000","status":"NEW","timeInForce":"GTC","type":"LIMIT","side":"BUY"}`)
		case request.Method == http.MethodGet && request.URL.Path == "/api/v3/order":
			if !verifySignedRequest(t, request, secret) {
				http.Error(writer, `{"code":-1022,"msg":"Signature invalid."}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"symbol":"BTCUSDT","orderId":42,"clientOrderId":"strategy-1","status":"NEW","type":"LIMIT","side":"BUY"}`)
		case request.Method == http.MethodDelete && request.URL.Path == "/api/v3/order":
			if !verifySignedRequest(t, request, secret) {
				http.Error(writer, `{"code":-1022,"msg":"Signature invalid."}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"symbol":"BTCUSDT","orderId":42,"clientOrderId":"cancel-1","origClientOrderId":"strategy-1","status":"CANCELED","type":"LIMIT","side":"BUY"}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, limiter := newTestClient(t, server.URL, sender, provider, []transport.EgressRouteID{"route-a", "route-b"}, nil)

	ticker, err := client.TickerPrice(
		context.Background(),
		TickerPriceRequest{Symbol: "BTCUSDT"},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("TickerPrice() error = %v", err)
	}
	if ticker.Price != "64000.10" || len(ticker.Raw) == 0 {
		t.Fatalf("TickerPrice() = %+v", ticker)
	}

	account, err := client.Account(context.Background(), AccountRequest{OmitZeroBalances: true})
	if err != nil {
		t.Fatalf("Account() error = %v", err)
	}
	if !account.CanTrade || len(account.Balances) != 1 {
		t.Fatalf("Account() = %+v", account)
	}

	order, err := client.NewOrder(
		context.Background(),
		NewOrderRequest{
			Symbol:        "BTCUSDT",
			Side:          SideBuy,
			Type:          OrderTypeLimit,
			TimeInForce:   TimeInForceGTC,
			Quantity:      "0.001",
			Price:         "64000.00",
			ClientOrderID: "strategy-1",
			ResponseType:  NewOrderResponseResult,
		},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil {
		t.Fatalf("NewOrder() error = %v", err)
	}
	if order.OrderID != 42 || order.Status != OrderStatusNew || len(order.Raw) == 0 {
		t.Fatalf("NewOrder() = %+v", order)
	}

	orderID := int64(42)
	queried, err := client.QueryOrder(context.Background(), QueryOrderRequest{
		Symbol:  "BTCUSDT",
		OrderID: &orderID,
	})
	if err != nil {
		t.Fatalf("QueryOrder() error = %v", err)
	}
	if queried.OrderID != orderID {
		t.Fatalf("QueryOrder().OrderID = %d, want %d", queried.OrderID, orderID)
	}

	canceled, err := client.CancelOrder(context.Background(), CancelOrderRequest{
		Symbol:                "BTCUSDT",
		OriginalClientOrderID: "strategy-1",
		NewClientOrderID:      "cancel-1",
	})
	if err != nil {
		t.Fatalf("CancelOrder() error = %v", err)
	}
	if canceled.Status != OrderStatusCanceled {
		t.Fatalf("CancelOrder().Status = %q, want CANCELED", canceled.Status)
	}

	routes, calls := sender.snapshot()
	wantRoutes := []transport.EgressRouteID{"route-b", "route-a", "route-b", "route-a", "route-a"}
	if calls != len(wantRoutes) || !slices.Equal(routes, wantRoutes) {
		t.Fatalf("sender routes = %v, want %v", routes, wantRoutes)
	}
	providerCalls, apiKeyAfter, secretAfter := provider.snapshot()
	if providerCalls != 4 {
		t.Fatalf("provider calls = %d, want 4", providerCalls)
	}
	if !allZero(apiKeyAfter) || !allZero(secretAfter) {
		t.Fatal("resolved credential byte slices were not overwritten")
	}

	snapshot, err := limiter.Snapshot("binance:route:route-b:request_weight:1minute")
	if err != nil {
		t.Fatalf("limiter.Snapshot() error = %v", err)
	}
	if snapshot.Used < 50 {
		t.Fatalf("observed request weight = %d, want at least 50", snapshot.Used)
	}
}

func TestClientRejectsCredentialRouteBeforeResolvingSecret(t *testing.T) {
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
	_, err := client.Account(
		context.Background(),
		AccountRequest{},
		trade.WithEgressRoute("route-b"),
	)
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("Account() error = %v, want ErrAuthorization", err)
	}
	providerCalls, _, _ := provider.snapshot()
	_, senderCalls := sender.snapshot()
	if providerCalls != 0 || senderCalls != 0 {
		t.Fatalf("provider calls = %d, sender calls = %d, want 0 and 0", providerCalls, senderCalls)
	}
}

func TestServerTimeAdjustsSignedTimestampUsingRoundTripMidpoint(t *testing.T) {
	t.Parallel()

	var capturedTimestamp string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/time":
			_, _ = io.WriteString(writer, `{"serverTime":2050}`)
		case "/api/v3/account":
			capturedTimestamp = request.URL.Query().Get("timestamp")
			_, _ = io.WriteString(writer, `{"accountType":"SPOT","balances":[]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	times := []time.Time{time.UnixMilli(1000), time.UnixMilli(1100), time.UnixMilli(1200)}
	var timeMu sync.Mutex
	now := func() time.Time {
		timeMu.Lock()
		defer timeMu.Unlock()
		value := times[0]
		times = times[1:]
		return value
	}
	client, _ := newTestClient(t, server.URL, &directSender{}, &recordingProvider{}, []transport.EgressRouteID{"route-a"}, now)
	serverTime, err := client.ServerTime(context.Background())
	if err != nil {
		t.Fatalf("ServerTime() error = %v", err)
	}
	if serverTime.UnixMilli() != 2050 {
		t.Fatalf("ServerTime() = %d, want 2050", serverTime.UnixMilli())
	}
	if _, err := client.Account(context.Background(), AccountRequest{}); err != nil {
		t.Fatalf("Account() error = %v", err)
	}
	if capturedTimestamp != "2200" {
		t.Fatalf("signed timestamp = %q, want 2200", capturedTimestamp)
	}
}

func TestClassifyError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    int
		code      int
		message   string
		operation commonexchange.OperationKind
		category  trade.ErrorCategory
		retryable bool
	}{
		{"rate limit", 429, -1003, "too many", commonexchange.OperationRead, trade.ErrorRateLimited, true},
		{"authentication", 401, -2015, "bad key", commonexchange.OperationRead, trade.ErrorAuthentication, false},
		{"signature", 400, -1022, "bad signature", commonexchange.OperationRead, trade.ErrorAuthentication, false},
		{"order missing", 400, -2011, "unknown order", commonexchange.OperationRead, trade.ErrorOrderNotFound, false},
		{"no such order", 400, -2013, "order does not exist", commonexchange.OperationRead, trade.ErrorOrderNotFound, false},
		{"balance", 400, -2010, "Account has insufficient balance", commonexchange.OperationMutation, trade.ErrorInsufficientBalance, false},
		{"read unavailable", 500, 0, "", commonexchange.OperationRead, trade.ErrorExchangeUnavailable, true},
		{"mutation unknown", 500, 0, "", commonexchange.OperationMutation, trade.ErrorUnknownExecutionState, false},
		{"matching timeout", 504, -1007, "timeout", commonexchange.OperationMutation, trade.ErrorUnknownExecutionState, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			category, retryable := classifyError(test.status, test.code, test.message, test.operation)
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
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{
		Sender:  sender,
		Limiter: limiter,
	})
	if err != nil {
		t.Fatalf("exchange.NewExecutor() error = %v", err)
	}
	descriptor := &credential.Descriptor{
		AccountID:             "binance-main",
		Exchange:              model.ExchangeBinance,
		SecretRef:             "secret/binance/main",
		Permissions:           []credential.Permission{credential.PermissionRead, credential.PermissionTrade},
		AllowedEgressRouteIDs: allowedRoutes,
	}
	client, err := New(Config{
		Executor:             executor,
		Credentials:          descriptor,
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

func verifySignedRequest(t *testing.T, request *http.Request, secret []byte) bool {
	t.Helper()
	if request.Header.Get("X-MBX-APIKEY") != "test-api-key" {
		t.Errorf("X-MBX-APIKEY = %q", request.Header.Get("X-MBX-APIKEY"))
		return false
	}
	rawQuery := request.URL.RawQuery
	separator := strings.LastIndex(rawQuery, "&signature=")
	if separator < 0 {
		t.Error("signature parameter is missing or is not last")
		return false
	}
	payload := rawQuery[:separator]
	encodedSignature := rawQuery[separator+len("&signature="):]
	signature, err := url.QueryUnescape(encodedSignature)
	if err != nil {
		t.Errorf("url.QueryUnescape() error = %v", err)
		return false
	}
	want, err := SignHMACSHA256(secret, []byte(payload))
	if err != nil {
		t.Errorf("SignHMACSHA256() error = %v", err)
		return false
	}
	if signature != want {
		t.Errorf("signature = %q, want %q, payload = %q", signature, want, payload)
		return false
	}
	if request.URL.Query().Get("timestamp") == "" || request.URL.Query().Get("recvWindow") != "5000" {
		t.Errorf("timing parameters = %v", request.URL.Query())
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

func TestAPIErrorMappingFromResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(writer, `{"code":-1121,"msg":"Invalid symbol."}`)
	}))
	defer server.Close()
	client, _ := newTestClient(t, server.URL, &directSender{}, &recordingProvider{}, []transport.EgressRouteID{"route-a"}, nil)
	_, err := client.TickerPrice(context.Background(), TickerPriceRequest{Symbol: "NOTREAL"})
	if !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("TickerPrice() error = %v, want ErrValidation", err)
	}
	var apiError *trade.APIError
	if !errors.As(err, &apiError) || apiError.ExchangeCode != "-1121" || apiError.HTTPStatus != 400 {
		t.Fatalf("APIError = %+v", apiError)
	}
}
