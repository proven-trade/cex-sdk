package coinbase

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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

type recordingProvider struct {
	mu      sync.Mutex
	keyName string
	secret  []byte
	calls   int
	issued  [][]byte
}

func (provider *recordingProvider) Resolve(context.Context, string) (credential.Material, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.calls++
	material := credential.Material{
		APIKey: []byte(provider.keyName), SecretKey: cloneBytes(provider.secret),
	}
	provider.issued = append(provider.issued, material.APIKey, material.SecretKey)
	return material, nil
}

func (provider *recordingProvider) snapshot() (int, [][]byte) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.calls, slices.Clone(provider.issued)
}

func TestClientSpotLifecycleAndPerRequestRoute(t *testing.T) {
	t.Parallel()

	keyName := "organizations/test/apiKeys/main"
	privateKey, secret := newTestECKey(t, elliptic.P256())
	fixedNow := time.Unix(1_700_000_000, 0)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if strings.Contains(request.URL.Path, "/market/") || request.URL.Path == publicPrefix+"/time" {
			if request.Header.Get("Authorization") != "" || request.Header.Get("Cache-Control") != "no-cache" {
				http.Error(writer, `{"error":"INVALID_ARGUMENT","message":"invalid public headers"}`, http.StatusBadRequest)
				return
			}
		} else if !verifyRequestJWT(t, request, &privateKey.PublicKey, keyName, fixedNow) {
			http.Error(writer, `{"error":"UNAUTHENTICATED","message":"invalid token"}`, http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case publicPrefix + "/time":
			_, _ = io.WriteString(writer, `{"iso":"2023-11-14T22:13:20Z","epochSeconds":"1700000000","epochMillis":"1700000000000"}`)
		case publicPrefix + "/market/products":
			_, _ = io.WriteString(writer, `{"products":[{"product_id":"BTC-USD","price":"64000","product_type":"SPOT","base_increment":"0.00000001","quote_increment":"0.01"}],"num_products":1}`)
		case publicPrefix + "/market/products/BTC-USD":
			_, _ = io.WriteString(writer, `{"product_id":"BTC-USD","price":"64000","product_type":"SPOT","best_bid_price":"63999","best_ask_price":"64001"}`)
		case publicPrefix + "/market/product_book":
			_, _ = io.WriteString(writer, `{"pricebook":{"product_id":"BTC-USD","bids":[{"price":"63999","size":"1"}],"asks":[{"price":"64001","size":"2"}],"time":"2023-11-14T22:13:20Z"},"last":"64000","mid_market":"64000","spread_bps":"0.3","spread_absolute":"2"}`)
		case publicPrefix + "/market/products/BTC-USD/ticker":
			if request.URL.Query().Get("start") != "1699999940" || request.URL.Query().Get("end") != "1700000000" {
				http.Error(writer, `{"error":"INVALID_ARGUMENT","message":"unexpected trade range"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"trades":[{"trade_id":"trade-1","product_id":"BTC-USD","price":"64000","size":"0.01","time":"2023-11-14T22:13:20Z","side":"BUY","exchange":"CBE"}],"best_bid":"63999","best_ask":"64001"}`)
		case publicPrefix + "/market/products/BTC-USD/candles":
			_, _ = io.WriteString(writer, `{"candles":[{"start":"1699999940","low":"63900","high":"64100","open":"64000","close":"64050","volume":"10"}]}`)
		case publicPrefix + "/accounts":
			_, _ = io.WriteString(writer, `{"accounts":[{"uuid":"account-1","currency":"USD","available_balance":{"value":"900","currency":"USD"},"hold":{"value":"100","currency":"USD"},"active":true,"ready":true}],"has_next":true,"cursor":"account-next","size":1}`)
		case publicPrefix + "/accounts/account-1":
			_, _ = io.WriteString(writer, `{"account":{"uuid":"account-1","currency":"USD","available_balance":{"value":"900","currency":"USD"},"hold":{"value":"100","currency":"USD"}}}`)
		case publicPrefix + "/orders":
			body, _ := io.ReadAll(request.Body)
			if string(body) != `{"client_order_id":"strategy-1","product_id":"BTC-USD","side":"BUY","order_configuration":{"limit_limit_gtc":{"base_size":"0.01","limit_price":"64000"}}}` {
				http.Error(writer, `{"error":"INVALID_ARGUMENT","message":"unexpected body"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"success":true,"success_response":{"order_id":"order-1","product_id":"BTC-USD","side":"BUY","client_order_id":"strategy-1"},"order_configuration":{}}`)
		case publicPrefix + "/orders/batch_cancel":
			_, _ = io.WriteString(writer, `{"results":[{"success":true,"failure_reason":"UNKNOWN_CANCEL_FAILURE_REASON","order_id":"order-1"}]}`)
		case publicPrefix + "/orders/historical/order-1":
			_, _ = io.WriteString(writer, `{"order":{"order_id":"order-1","product_id":"BTC-USD","client_order_id":"strategy-1","side":"BUY","status":"OPEN","filled_size":"0"}}`)
		case publicPrefix + "/orders/historical/batch":
			_, _ = io.WriteString(writer, `{"orders":[{"order_id":"order-1","product_id":"BTC-USD","status":"OPEN"}],"sequence":"10","has_next":true,"cursor":"order-next"}`)
		case publicPrefix + "/orders/historical/fills":
			_, _ = io.WriteString(writer, `{"fills":[{"entry_id":"entry-1","trade_id":"trade-1","order_id":"order-1","product_id":"BTC-USD","price":"64000","size":"0.01","commission":"0.1","side":"BUY"}],"cursor":"fill-next"}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{keyName: keyName, secret: secret}
	client, limiter := newTestClient(
		t, server.URL, sender, provider, []transport.EgressRouteID{"route-a", "route-b"}, fixedNow,
	)
	serverTime, err := client.ServerTime(context.Background())
	if err != nil || serverTime.Unix() != fixedNow.Unix() {
		t.Fatalf("ServerTime() = %v, error = %v", serverTime, err)
	}
	products, err := client.Products(
		context.Background(), ProductsRequest{Limit: 10, ProductIDs: []string{"BTC-USD"}},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || len(products) != 1 || products[0].ProductID != "BTC-USD" || len(products[0].Raw) == 0 {
		t.Fatalf("Products() = %+v, error = %v", products, err)
	}
	product, err := client.Product(context.Background(), "BTC-USD")
	if err != nil || product.BestAskPrice != "64001" || len(product.Raw) == 0 {
		t.Fatalf("Product() = %+v, error = %v", product, err)
	}
	book, err := client.OrderBook(
		context.Background(), OrderBookRequest{ProductID: "BTC-USD", Limit: 50},
	)
	if err != nil || book.PriceBook.Bids[0].Price != "63999" || len(book.Raw) == 0 {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	trades, err := client.MarketTrades(
		context.Background(), MarketTradesRequest{
			ProductID: "BTC-USD", Limit: 10,
			Start: timePointer(fixedNow.Add(-time.Minute)), End: timePointer(fixedNow),
		},
	)
	if err != nil || len(trades.Trades) != 1 || trades.Trades[0].TradeID != "trade-1" ||
		len(trades.Trades[0].Raw) == 0 {
		t.Fatalf("MarketTrades() = %+v, error = %v", trades, err)
	}
	candles, err := client.Candles(context.Background(), CandlesRequest{
		ProductID: "BTC-USD", Start: fixedNow.Add(-time.Hour), End: fixedNow,
		Granularity: Candle1Minute, Limit: 60,
	})
	if err != nil || len(candles) != 1 || candles[0].Close != "64050" {
		t.Fatalf("Candles() = %+v, error = %v", candles, err)
	}
	accounts, err := client.Accounts(
		context.Background(), AccountsRequest{Limit: 50, Cursor: "account-page"},
	)
	if err != nil || len(accounts.Accounts) != 1 || accounts.Accounts[0].AvailableBalance.Value != "900" ||
		!accounts.HasNext || len(accounts.Raw) == 0 {
		t.Fatalf("Accounts() = %+v, error = %v", accounts, err)
	}
	account, err := client.Account(context.Background(), "account-1")
	if err != nil || account.Hold.Value != "100" || len(account.Raw) == 0 {
		t.Fatalf("Account() = %+v, error = %v", account, err)
	}
	reference, err := client.PlaceOrder(
		context.Background(),
		PlaceOrderRequest{
			ClientOrderID: "strategy-1", ProductID: "BTC-USD", Side: SideBuy,
			OrderConfiguration: OrderConfiguration{LimitLimitGTC: &LimitGTCConfiguration{
				BaseSize: "0.01", LimitPrice: "64000",
			}},
		},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || reference.OrderID != "order-1" || len(reference.Raw) == 0 {
		t.Fatalf("PlaceOrder() = %+v, error = %v", reference, err)
	}
	canceled, err := client.CancelOrders(
		context.Background(), CancelOrdersRequest{OrderIDs: []string{"order-1"}},
	)
	if err != nil || len(canceled) != 1 || !canceled[0].Success {
		t.Fatalf("CancelOrders() = %+v, error = %v", canceled, err)
	}
	order, err := client.OrderInfo(context.Background(), "order-1")
	if err != nil || order.Status != "OPEN" || len(order.Raw) == 0 {
		t.Fatalf("OrderInfo() = %+v, error = %v", order, err)
	}
	orders, err := client.Orders(
		context.Background(), OrdersRequest{ProductIDs: []string{"BTC-USD"}, Limit: 100},
	)
	if err != nil || len(orders.Orders) != 1 || orders.Cursor != "order-next" || len(orders.Raw) == 0 {
		t.Fatalf("Orders() = %+v, error = %v", orders, err)
	}
	fills, err := client.Fills(
		context.Background(), FillsRequest{ProductIDs: []string{"BTC-USD"}, Limit: 100},
	)
	if err != nil || len(fills.Fills) != 1 || fills.Fills[0].Commission != "0.1" ||
		len(fills.Fills[0].Raw) == 0 {
		t.Fatalf("Fills() = %+v, error = %v", fills, err)
	}

	routes := sender.snapshot()
	if len(routes) != 13 || routes[1] != "route-b" || routes[8] != "route-b" {
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
	snapshot, err := limiter.Snapshot("coinbase:account:coinbase-main:private:1second")
	if err != nil || snapshot.Used != 7 {
		t.Fatalf("private limiter snapshot = %+v, error = %v", snapshot, err)
	}
}

func TestClientRejectsUnauthorizedRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	_, secret := newTestECKey(t, elliptic.P256())
	provider := &recordingProvider{keyName: "organizations/test/apiKeys/main", secret: secret}
	client, _ := newTestClient(
		t, "http://127.0.0.1", &directSender{}, provider,
		[]transport.EgressRouteID{"route-a"}, time.Now(),
	)
	_, err := client.Accounts(
		context.Background(), AccountsRequest{}, trade.WithEgressRoute("route-b"),
	)
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("Accounts() error = %v, want authorization", err)
	}
	calls, _ := provider.snapshot()
	if calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestClientClassifiesOrderFailureAndUnknownMutationState(t *testing.T) {
	t.Parallel()

	privateKey, secret := newTestECKey(t, elliptic.P256())
	keyName := "organizations/test/apiKeys/main"
	fixedNow := time.Unix(1_700_000_000, 0)
	responses := []struct {
		status int
		body   string
	}{
		{status: http.StatusOK, body: `{"success":false,"error_response":{"error":"INSUFFICIENT_FUND","message":"insufficient balance","new_order_failure_reason":"INSUFFICIENT_FUND"}}`},
		{status: http.StatusInternalServerError, body: `{"error":"INTERNAL","message":"try later"}`},
	}
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !verifyRequestJWT(t, request, &privateKey.PublicKey, keyName, fixedNow) {
			http.Error(writer, "invalid token", http.StatusUnauthorized)
			return
		}
		mu.Lock()
		response := responses[0]
		responses = responses[1:]
		mu.Unlock()
		writer.WriteHeader(response.status)
		_, _ = io.WriteString(writer, response.body)
	}))
	defer server.Close()
	client, _ := newTestClient(
		t, server.URL, &directSender{},
		&recordingProvider{keyName: keyName, secret: secret},
		[]transport.EgressRouteID{"route-a"}, fixedNow,
	)
	request := PlaceOrderRequest{
		ClientOrderID: "strategy-1", ProductID: "BTC-USD", Side: SideBuy,
		OrderConfiguration: OrderConfiguration{MarketMarketIOC: &MarketIOCConfiguration{QuoteSize: "10"}},
	}
	if _, err := client.PlaceOrder(context.Background(), request); !errors.Is(err, trade.ErrInsufficientBalance) {
		t.Fatalf("PlaceOrder(insufficient) error = %v", err)
	}
	if _, err := client.PlaceOrder(context.Background(), request); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceOrder(500) error = %v", err)
	}
}

func TestRequestValidationRejectsAmbiguousOrdersAndRanges(t *testing.T) {
	t.Parallel()

	invalidOrders := []PlaceOrderRequest{
		{ClientOrderID: "id", ProductID: "BTC-USD", Side: SideBuy},
		{
			ClientOrderID: "id", ProductID: "BTC-USD", Side: SideBuy,
			OrderConfiguration: OrderConfiguration{MarketMarketIOC: &MarketIOCConfiguration{
				QuoteSize: "10", BaseSize: "0.1",
			}},
		},
	}
	for _, request := range invalidOrders {
		if err := request.validate(); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("PlaceOrderRequest.validate() error = %v", err)
		}
	}
	start := time.Unix(2, 0)
	end := time.Unix(1, 0)
	if err := (FillsRequest{Start: &start, End: &end}).validate(); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("FillsRequest.validate() error = %v", err)
	}
	if err := (MarketTradesRequest{ProductID: "BTC-USD"}).validate(); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("MarketTradesRequest.validate() error = %v", err)
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
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{Sender: sender, Limiter: limiter})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	descriptor := &credential.Descriptor{
		AccountID: "coinbase-main", Exchange: model.ExchangeCoinbase,
		SecretRef:             "secret/coinbase/main",
		Permissions:           []credential.Permission{credential.PermissionRead, credential.PermissionTrade},
		AllowedEgressRouteIDs: allowedRoutes,
	}
	client, err := New(Config{
		Executor: executor, Credentials: descriptor, CredentialProvider: provider,
		DefaultEgressRouteID: "route-a", BaseURL: baseURL, AllowInsecureHTTP: true,
		PublicRequestsPerSecond: 100, PrivateRequestsPerSecond: 100,
		Now: func() time.Time { return now }, Random: rand.Reader,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, limiter
}

func verifyRequestJWT(
	t *testing.T,
	request *http.Request,
	publicKey *ecdsa.PublicKey,
	keyName string,
	now time.Time,
) bool {
	t.Helper()
	authorization := request.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer ") {
		return false
	}
	header, claims := verifyTestJWT(t, strings.TrimPrefix(authorization, "Bearer "), publicKey)
	return header.KeyID == keyName && claims.Subject == keyName && claims.Issuer == "cdp" &&
		claims.NotBefore == now.Unix() && claims.ExpiresAt == now.Add(2*time.Minute).Unix() &&
		claims.URI == request.Method+" "+request.Host+request.URL.Path
}

func allZero(value []byte) bool {
	for _, item := range value {
		if item != 0 {
			return false
		}
	}
	return true
}

func timePointer(value time.Time) *time.Time {
	return &value
}
