package mexc

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
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

type recordingProvider struct {
	mu         sync.Mutex
	calls      int
	lastAPIKey []byte
	lastSecret []byte
}

type providerFunc func(context.Context, string) (credential.Material, error)

func (provider providerFunc) Resolve(
	ctx context.Context,
	secretRef string,
) (credential.Material, error) {
	return provider(ctx, secretRef)
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

func TestClientPrivateSpotLifecycle(t *testing.T) {
	t.Parallel()
	fixedNow := time.UnixMilli(1_700_000_000_000)
	start := time.UnixMilli(1_699_900_000_000)
	end := time.UnixMilli(1_699_986_400_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if !verifyMEXCSignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"code":700002,"msg":"invalid signature"}`, http.StatusUnauthorized)
			return
		}
		switch {
		case request.URL.Path == "/api/v3/selfSymbols" && request.Method == http.MethodGet:
			_, _ = io.WriteString(writer, `{"code":0,"data":["BTCUSDT"],"msg":"success"}`)
		case request.URL.Path == "/api/v3/account" && request.Method == http.MethodGet:
			_, _ = io.WriteString(writer, `{"canTrade":true,"canWithdraw":false,"canDeposit":true,"updateTime":null,"accountType":"SPOT","balances":[{"asset":"USDT","free":"900","locked":"100"}],"permissions":["SPOT"]}`)
		case request.URL.Path == "/api/v3/order" && request.Method == http.MethodPost:
			query := request.URL.Query()
			if query.Get("newClientOrderId") != "strategy-1" || query.Get("symbol") != "BTCUSDT" ||
				query.Get("side") != "BUY" || query.Get("type") != "LIMIT" ||
				query.Get("quantity") != "0.1" || query.Get("price") != "64000" ||
				query.Get("quoteOrderQty") != "" {
				http.Error(writer, `{"code":33333,"msg":"invalid order"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"symbol":"BTCUSDT","orderId":"order-1","orderListId":-1,"price":"64000","origQty":"0.1","type":"LIMIT","side":"BUY","transactTime":1700000000000}`)
		case request.URL.Path == "/api/v3/order" && request.Method == http.MethodGet:
			if request.URL.Query().Get("orderId") != "order-1" {
				http.Error(writer, `{"code":700004,"msg":"missing order identity"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, orderJSON("NEW"))
		case request.URL.Path == "/api/v3/order" && request.Method == http.MethodDelete:
			if request.URL.Query().Get("origClientOrderId") != "strategy-1" {
				http.Error(writer, `{"code":700004,"msg":"missing client order identity"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, orderJSON("CANCELED"))
		case request.URL.Path == "/api/v3/openOrders" && request.Method == http.MethodGet:
			_, _ = io.WriteString(writer, `[`+orderJSON("NEW")+`]`)
		case request.URL.Path == "/api/v3/allOrders" && request.Method == http.MethodGet:
			query := request.URL.Query()
			if query.Get("startTime") != "1699900000000" || query.Get("endTime") != "1699986400000" ||
				query.Get("limit") != "100" {
				http.Error(writer, `{"code":33333,"msg":"invalid history query"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `[`+orderJSON("FILLED")+`]`)
		case request.URL.Path == "/api/v3/myTrades" && request.Method == http.MethodGet:
			_, _ = io.WriteString(writer, `[{"symbol":"BTCUSDT","id":"trade-1","orderId":"order-1","orderListId":-1,"price":"64000","qty":"0.1","quoteQty":"6400","commission":"0.0001","commissionAsset":"BTC","time":1700000000000,"isBuyer":true,"isMaker":false,"isBestMatch":true,"isSelfTrade":false,"clientOrderId":null}]`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, limiter := newPrivateTestClient(
		t, server.URL, sender, provider,
		[]transport.EgressRouteID{"route-a", "route-b"},
		[]credential.Permission{credential.PermissionRead, credential.PermissionTrade}, fixedNow,
	)
	ctx := context.Background()
	selfSymbols, err := client.SelfSymbols(ctx)
	if err != nil || !slices.Equal(selfSymbols, []string{"BTCUSDT"}) {
		t.Fatalf("SelfSymbols() = %v, error = %v", selfSymbols, err)
	}
	account, err := client.Account(ctx, trade.WithEgressRoute("route-b"))
	if err != nil || !account.CanTrade || account.UpdateTime != "" || len(account.Balances) != 1 ||
		account.Balances[0].Free != "900" || len(account.Balances[0].Raw) == 0 || len(account.Raw) == 0 {
		t.Fatalf("Account() = %+v, error = %v", account, err)
	}
	placed, err := client.PlaceOrder(ctx, PlaceOrderRequest{
		ClientOrderID: "strategy-1", Symbol: "BTCUSDT", Side: SideBuy,
		Type: OrderTypeLimit, Quantity: "0.1", Price: "64000",
	}, trade.WithEgressRoute("route-b"))
	if err != nil || placed.OrderID != "order-1" || placed.ClientOrderID != "strategy-1" ||
		len(placed.Raw) == 0 {
		t.Fatalf("PlaceOrder() = %+v, error = %v", placed, err)
	}
	order, err := client.OrderInfo(ctx, OrderInfoRequest{Symbol: "BTCUSDT", OrderID: "order-1"})
	if err != nil || order.Status != OrderStatusNew || order.OriginalQuantity != "0.1" ||
		len(order.Raw) == 0 {
		t.Fatalf("OrderInfo() = %+v, error = %v", order, err)
	}
	canceled, err := client.CancelOrder(ctx, CancelOrderRequest{
		Symbol: "BTCUSDT", ClientOrderID: "strategy-1",
	}, trade.WithEgressRoute("route-b"))
	if err != nil || canceled.Status != OrderStatusCanceled || len(canceled.Raw) == 0 {
		t.Fatalf("CancelOrder() = %+v, error = %v", canceled, err)
	}
	openOrders, err := client.OpenOrders(ctx, OpenOrdersRequest{Symbol: "BTCUSDT"})
	if err != nil || len(openOrders) != 1 || openOrders[0].OrderID != "order-1" ||
		len(openOrders[0].Raw) == 0 {
		t.Fatalf("OpenOrders() = %+v, error = %v", openOrders, err)
	}
	allOrders, err := client.AllOrders(ctx, AllOrdersRequest{
		Symbol: "BTCUSDT", Start: &start, End: &end, Limit: 100,
	})
	if err != nil || len(allOrders) != 1 || allOrders[0].Status != OrderStatusFilled {
		t.Fatalf("AllOrders() = %+v, error = %v", allOrders, err)
	}
	trades, err := client.MyTrades(ctx, MyTradesRequest{
		Symbol: "BTCUSDT", OrderID: "order-1", Limit: 100,
	})
	if err != nil || len(trades) != 1 || trades[0].ID != "trade-1" ||
		trades[0].ClientOrderID != "" || trades[0].Commission != "0.0001" || len(trades[0].Raw) == 0 {
		t.Fatalf("MyTrades() = %+v, error = %v", trades, err)
	}

	routes := sender.snapshot()
	if !slices.Equal(routes, []transport.EgressRouteID{
		"route-a", "route-b", "route-b", "route-a", "route-b", "route-a", "route-a", "route-a",
	}) {
		t.Fatalf("private sender routes = %v", routes)
	}
	calls, apiKey, secret := provider.snapshot()
	if calls != 8 || !allZero(apiKey) || !allZero(secret) {
		t.Fatalf("provider state calls=%d apiKey=%v secret=%v", calls, apiKey, secret)
	}
	assertLimiterUsed(t, limiter, "mexc:route:route-b:private:order:10seconds", 10)
	assertLimiterUsed(t, limiter, "mexc:account:mexc-main:private:order:10seconds", 10)
	assertPrivateFrequency(t, limiter, "mexc:account:mexc-main:order:1second", 1, DefaultOrderQuota)
	assertPrivateFrequency(t, limiter, "mexc:account:mexc-main:account:1second", 1, DefaultAccountQuota)
	assertPrivateFrequency(t, limiter, "mexc:account:mexc-main:private-read:1second", 5, DefaultPrivateReadQuota)
}

func TestClientRejectsPrivateRouteAndPermissionBeforeSecretResolution(t *testing.T) {
	t.Parallel()
	provider := &recordingProvider{}
	client, _ := newPrivateTestClient(
		t, "http://127.0.0.1", &directSender{}, provider,
		[]transport.EgressRouteID{"route-a"}, []credential.Permission{credential.PermissionRead},
		time.UnixMilli(1_700_000_000_000),
	)
	if _, err := client.Account(
		context.Background(), trade.WithEgressRoute("route-b"),
	); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("Account() route error = %v, want authorization", err)
	}
	if _, err := client.PlaceOrder(context.Background(), validLimitOrder()); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("PlaceOrder() permission error = %v, want authorization", err)
	}
	calls, _, _ := provider.snapshot()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestClientPrivateCredentialFailures(t *testing.T) {
	t.Parallel()
	publicClient, _ := newTestClient(t, "http://127.0.0.1", &directSender{})
	if _, err := publicClient.Account(context.Background()); !errors.Is(err, trade.ErrAuthentication) {
		t.Fatalf("Account() without credentials error = %v, want authentication", err)
	}

	apiKey := []byte("discard-api-key")
	secret := []byte("discard-secret")
	failing := providerFunc(func(context.Context, string) (credential.Material, error) {
		return credential.Material{APIKey: apiKey, SecretKey: secret}, errors.New("vault unavailable")
	})
	client, _ := newPrivateTestClient(
		t, "http://127.0.0.1", &directSender{}, failing,
		[]transport.EgressRouteID{"route-a"}, []credential.Permission{credential.PermissionRead},
		time.UnixMilli(1_700_000_000_000),
	)
	if _, err := client.Account(context.Background()); !errors.Is(err, trade.ErrAuthentication) {
		t.Fatalf("Account() provider error = %v, want authentication", err)
	}
	if !allZero(apiKey) || !allZero(secret) {
		t.Fatalf("provider failure material was not destroyed: key=%v secret=%v", apiKey, secret)
	}

	empty := providerFunc(func(context.Context, string) (credential.Material, error) {
		return credential.Material{}, nil
	})
	client, _ = newPrivateTestClient(
		t, "http://127.0.0.1", &directSender{}, empty,
		[]transport.EgressRouteID{"route-a"}, []credential.Permission{credential.PermissionRead},
		time.UnixMilli(1_700_000_000_000),
	)
	if _, err := client.Account(context.Background()); !errors.Is(err, trade.ErrAuthentication) {
		t.Fatalf("Account() empty material error = %v, want authentication", err)
	}
}

func TestClientClassifiesPrivateErrorsAndUnknownMutationState(t *testing.T) {
	t.Parallel()
	fixedNow := time.UnixMilli(1_700_000_000_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.URL.Path == "/api/v3/order" && request.Method == http.MethodPost:
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(writer, `{"code":500,"msg":"internal error"}`)
		case request.URL.Path == "/api/v3/order" && request.Method == http.MethodDelete:
			_, _ = io.WriteString(writer, `{`)
		case request.URL.Path == "/api/v3/account":
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(writer, `{"code":10101,"msg":"insufficient balance"}`)
		default:
			writer.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(writer, `{"code":-2011,"msg":"unknown order"}`)
		}
	}))
	defer server.Close()
	client, _ := newPrivateTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"},
		[]credential.Permission{credential.PermissionRead, credential.PermissionTrade}, fixedNow,
	)
	if _, err := client.PlaceOrder(context.Background(), validLimitOrder()); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceOrder() error = %v, want unknown execution state", err)
	}
	if _, err := client.CancelOrder(context.Background(), CancelOrderRequest{
		Symbol: "BTCUSDT", OrderID: "order-1",
	}); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("CancelOrder() error = %v, want unknown execution state", err)
	}
	if _, err := client.Account(context.Background()); !errors.Is(err, trade.ErrInsufficientBalance) {
		t.Fatalf("Account() error = %v, want insufficient balance", err)
	}
	if _, err := client.OrderInfo(context.Background(), OrderInfoRequest{
		Symbol: "BTCUSDT", OrderID: "order-1",
	}); !errors.Is(err, trade.ErrOrderNotFound) {
		t.Fatalf("OrderInfo() error = %v, want order not found", err)
	}
	category, retryable := classifyError(
		http.StatusInternalServerError, "400", commonexchange.OperationMutation,
	)
	if category != trade.ErrorUnknownExecutionState || retryable {
		t.Fatalf("HTTP 5xx mutation classification = %s, %t", category, retryable)
	}
}

func TestPrivateRequestValidation(t *testing.T) {
	t.Parallel()
	start := time.UnixMilli(1_700_000_000_000)
	weekLater := start.Add(7*24*time.Hour + time.Millisecond)
	monthLater := start.Add(31*24*time.Hour + time.Millisecond)
	tests := []struct {
		name string
		err  error
	}{
		{name: "client order ID", err: (PlaceOrderRequest{Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeMarket, QuoteQuantity: "10"}).validate()},
		{name: "limit quantity", err: (PlaceOrderRequest{ClientOrderID: "strategy", Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeLimit, Price: "1"}).validate()},
		{name: "zero quantity", err: (PlaceOrderRequest{ClientOrderID: "strategy", Symbol: "BTCUSDT", Side: SideSell, Type: OrderTypeMarket, Quantity: "0"}).validate()},
		{name: "market buy base", err: (PlaceOrderRequest{ClientOrderID: "strategy", Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeMarket, Quantity: "1"}).validate()},
		{name: "market sell quote", err: (PlaceOrderRequest{ClientOrderID: "strategy", Symbol: "BTCUSDT", Side: SideSell, Type: OrderTypeMarket, QuoteQuantity: "10"}).validate()},
		{name: "order identity", err: (OrderInfoRequest{Symbol: "BTCUSDT", OrderID: "one", ClientOrderID: "two"}).validate()},
		{name: "cancel identity", err: (CancelOrderRequest{Symbol: "BTCUSDT"}).validate()},
		{name: "open orders conflict", err: (OpenOrdersRequest{Symbol: "BTCUSDT", Symbols: []string{"ETHUSDT"}}).validate()},
		{name: "open orders empty", err: (OpenOrdersRequest{}).validate()},
		{name: "open orders duplicate", err: (OpenOrdersRequest{Symbols: []string{"BTCUSDT", "BTCUSDT"}}).validate()},
		{name: "open orders maximum", err: (OpenOrdersRequest{Symbols: []string{"A1", "A2", "A3", "A4", "A5", "A6"}}).validate()},
		{name: "history range", err: (AllOrdersRequest{Symbol: "BTCUSDT", Start: &start, End: &weekLater}).validate()},
		{name: "trade range", err: (MyTradesRequest{Symbol: "BTCUSDT", Start: &start, End: &monthLater}).validate()},
		{name: "trade limit", err: (MyTradesRequest{Symbol: "BTCUSDT", Limit: 101}).validate()},
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
	payload := []byte("recvWindow=5000&symbol=BTCUSDT&timestamp=1700000000000")
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
	if _, err := SignHMACSHA256(nil, payload); err == nil {
		t.Fatal("SignHMACSHA256() accepted an empty secret")
	}
}

func TestNewClientPrivateValidationAndDefaults(t *testing.T) {
	t.Parallel()
	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{
		Sender: &directSender{}, Limiter: limiter,
	})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	descriptor := &credential.Descriptor{
		AccountID: "mexc-main", Exchange: model.ExchangeMEXC, SecretRef: "secret/mexc-main",
		Permissions:           []credential.Permission{credential.PermissionRead},
		AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"},
	}
	tests := []Config{
		{Executor: executor, DefaultEgressRouteID: "route-a", Credentials: descriptor},
		{Executor: executor, DefaultEgressRouteID: "route-a", CredentialProvider: &recordingProvider{}},
		{Executor: executor, DefaultEgressRouteID: "route-a", ReceiveWindow: 60*time.Second + time.Millisecond},
		{Executor: executor, DefaultEgressRouteID: "route-a", OrderQuota: -1},
	}
	wrong := *descriptor
	wrong.Exchange = model.ExchangeBinance
	tests = append(tests, Config{
		Executor: executor, DefaultEgressRouteID: "route-a", Credentials: &wrong,
		CredentialProvider: &recordingProvider{},
	})
	for _, config := range tests {
		if _, err := New(config); err == nil {
			t.Fatal("New() private validation error = nil")
		}
	}
	client, err := New(Config{
		Executor: executor, DefaultEgressRouteID: "route-a", Credentials: descriptor,
		CredentialProvider: &recordingProvider{},
	})
	if err != nil || client.receiveWindow != DefaultReceiveWindow ||
		client.orderQuota != DefaultOrderQuota || client.cancelQuota != DefaultCancelQuota ||
		client.privateReadQuota != DefaultPrivateReadQuota || client.accountQuota != DefaultAccountQuota {
		t.Fatalf("New() private defaults = %+v, error = %v", client, err)
	}
}

func verifyMEXCSignedRequest(
	t *testing.T,
	request *http.Request,
	secret []byte,
	now time.Time,
) bool {
	t.Helper()
	if request.Header.Get("X-MEXC-APIKEY") != "test-api-key" || request.ContentLength > 0 {
		return false
	}
	values := request.URL.Query()
	signature := values.Get("signature")
	values.Del("signature")
	if values.Get("timestamp") != "1700000000000" || values.Get("recvWindow") != "5000" ||
		now.UnixMilli() != 1_700_000_000_000 {
		return false
	}
	want, err := SignHMACSHA256(secret, []byte(values.Encode()))
	if err != nil {
		t.Fatalf("SignHMACSHA256() error = %v", err)
	}
	return hmac.Equal([]byte(signature), []byte(want))
}

func newPrivateTestClient(
	t *testing.T,
	baseURL string,
	sender commonexchange.Sender,
	provider credential.Provider,
	allowedRoutes []transport.EgressRouteID,
	permissions []credential.Permission,
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
		t.Fatalf("NewExecutor() error = %v", err)
	}
	client, err := New(Config{
		Executor: executor,
		Credentials: &credential.Descriptor{
			AccountID: "mexc-main", Exchange: model.ExchangeMEXC, SecretRef: "secret/mexc-main",
			Permissions: permissions, AllowedEgressRouteIDs: allowedRoutes,
		},
		CredentialProvider: provider, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, limiter
}

func validLimitOrder() PlaceOrderRequest {
	return PlaceOrderRequest{
		ClientOrderID: "strategy-1", Symbol: "BTCUSDT", Side: SideBuy,
		Type: OrderTypeLimit, Quantity: "0.1", Price: "64000",
	}
}

func orderJSON(status string) string {
	return `{"symbol":"BTCUSDT","origClientOrderId":"strategy-1","orderId":"order-1","orderListId":-1,"clientOrderId":"strategy-1","price":"64000","origQty":"0.1","executedQty":"0","cummulativeQuoteQty":"0","status":"` + status + `","timeInForce":"GTC","type":"LIMIT","side":"BUY","stopPrice":"0","icebergQty":"0","time":1700000000000,"updateTime":1700000000000,"isWorking":true,"origQuoteOrderQty":"0"}`
}

func assertPrivateFrequency(
	t *testing.T,
	limiter *ratelimit.Limiter,
	key string,
	wantUsed int,
	wantLimit int,
) {
	t.Helper()
	snapshot, err := limiter.Snapshot(key)
	if err != nil || snapshot.Used != wantUsed || snapshot.Rule.Limit != wantLimit ||
		snapshot.Rule.Window != time.Second {
		t.Fatalf("private limiter snapshot %q = %+v, error = %v", key, snapshot, err)
	}
}

func allZero(value []byte) bool {
	for _, item := range value {
		if item != 0 {
			return false
		}
	}
	return true
}
