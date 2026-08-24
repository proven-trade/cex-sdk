package korbit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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
	material := credential.Material{APIKey: []byte("api-key"), SecretKey: []byte("secret-key")}
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
		if !isKorbitPublicPath(request.URL.Path) {
			verifyKorbitSignedRequest(t, request, []byte("secret-key"))
		}
		switch request.URL.Path {
		case "/v2/time":
			_, _ = io.WriteString(writer, `{"success":true,"data":{"time":1700000000000}}`)
		case "/v2/tickers":
			if request.URL.Query().Get("symbol") != "btc_krw,eth_krw" {
				t.Errorf("ticker query = %q", request.URL.RawQuery)
			}
			_, _ = io.WriteString(writer, `{"success":true,"data":[{"symbol":"btc_krw","open":"63000000","high":"65000000","low":"62000000","close":"64000000","prevClose":"63000000","priceChange":"1000000","priceChangePercent":"1.58","volume":"10.5","quoteVolume":"670000000","bestBidPrice":"64000000","bestAskPrice":"64001000","lastTradedAt":1700000000000}]}`)
		case "/v2/orderbook":
			_, _ = io.WriteString(writer, `{"success":true,"data":{"timestamp":1700000000000,"bids":[{"price":"64000000","qty":"0.1","amt":"6400000"}],"asks":[{"price":"64001000","qty":"0.2","amt":"12800200"}]}}`)
		case "/v2/trades":
			_, _ = io.WriteString(writer, `{"success":true,"data":[{"timestamp":1700000000000,"price":"64000000","qty":"0.01","isBuyerTaker":true,"tradeId":1004}]}`)
		case "/v2/candles":
			_, _ = io.WriteString(writer, `{"success":true,"data":[{"timestamp":1700000000000,"open":"63000000","high":"65000000","low":"62000000","close":"64000000","volume":"10.5"}]}`)
		case "/v2/currencyPairs":
			_, _ = io.WriteString(writer, `{"success":true,"data":[{"symbol":"btc_krw","status":"launched","baseCurrency":"btc","quoteCurrency":"krw","minOrderValue":"5000","maxOrderValue":"1000000000"}]}`)
		case "/v2/tickSizePolicy":
			_, _ = io.WriteString(writer, `{"success":true,"data":[{"symbol":"btc_krw","tickSizePolicy":[{"priceGte":"0","tickSize":"0.0001"},{"priceGte":"1000000","tickSize":"1000"}],"orderbookLevels":["1000","10000"]}]}`)
		case "/v2/balance":
			_, _ = io.WriteString(writer, `{"success":true,"data":[{"currency":"krw","balance":"1000000","available":"900000","tradeInUse":"100000","withdrawalInUse":"0","avgPrice":"0"}]}`)
		case "/v2/orders":
			switch request.Method {
			case http.MethodPost:
				unsigned := unsignedKorbitParameters(t, request)
				want := "accountSeq=2&clientOrderId=strategy-1&orderType=limit&price=64000000&qty=0.01&recvWindow=5000&side=buy&symbol=btc_krw&timeInForce=po&timestamp=1700000000123"
				if unsigned != want {
					t.Errorf("order unsigned body = %q, want %q", unsigned, want)
				}
				_, _ = io.WriteString(writer, `{"success":true,"data":{"orderId":1234}}`)
			case http.MethodGet:
				_, _ = io.WriteString(writer, `{"success":true,"data":{"orderId":1234,"clientOrderId":"strategy-1","symbol":"btc_krw","orderType":"limit","side":"buy","timeInForce":"po","price":"64000000","qty":"0.01","filledQty":"0","filledAmt":"0","createdAt":1700000000000,"status":"open"}}`)
			case http.MethodDelete:
				_, _ = io.WriteString(writer, `{"success":true}`)
			default:
				http.NotFound(writer, request)
			}
		case "/v2/openOrders":
			_, _ = io.WriteString(writer, `{"success":true,"data":[{"orderId":1234,"clientOrderId":"strategy-1","symbol":"btc_krw","orderType":"limit","side":"buy","price":"64000000","qty":"0.01","filledQty":"0","filledAmt":"0","createdAt":1700000000000,"status":"open"}]}`)
		case "/v2/allOrders":
			_, _ = io.WriteString(writer, `{"success":true,"data":[{"orderId":1233,"clientOrderId":"strategy-0","symbol":"btc_krw","orderType":"limit","side":"sell","timeInForce":"gtc","price":"63000000","qty":"0.01","filledQty":"0.01","filledAmt":"630000","avgPrice":"63000000","createdAt":1700000000000,"lastFilledAt":1700000001000,"status":"filled"}]}`)
		case "/v2/myTrades":
			_, _ = io.WriteString(writer, `{"success":true,"data":[{"symbol":"btc_krw","tradeId":52,"orderId":1233,"side":"sell","price":"63000000","qty":"0.01","amt":"630000","tradedAt":1700000001000,"isTaker":false,"feeCurrency":"krw","feeQty":"1260"}]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, limiter := newTestClient(t, server.URL, sender, provider, []transport.EgressRouteID{"route-a", "route-b"})

	serverTime, err := client.ServerTime(context.Background(), trade.WithEgressRoute("route-b"))
	if err != nil || serverTime.Time != 1700000000000 || len(serverTime.Raw) == 0 {
		t.Fatalf("ServerTime() = %+v, error = %v", serverTime, err)
	}
	tickers, err := client.Tickers(context.Background(), TickersRequest{Symbols: []string{"btc_krw", "eth_krw"}}, trade.WithEgressRoute("route-b"))
	if err != nil || len(tickers) != 1 || tickers[0].Close != "64000000" || len(tickers[0].Raw) == 0 {
		t.Fatalf("Tickers() = %+v, error = %v", tickers, err)
	}
	book, err := client.OrderBook(context.Background(), OrderBookRequest{Symbol: "btc_krw", Level: "1000"})
	if err != nil || book.Bids[0].Amount != "6400000" || len(book.Raw) == 0 {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	trades, err := client.RecentTrades(context.Background(), RecentTradesRequest{Symbol: "btc_krw", Limit: 100})
	if err != nil || len(trades) != 1 || trades[0].TradeID != 1004 || len(trades[0].Raw) == 0 {
		t.Fatalf("RecentTrades() = %+v, error = %v", trades, err)
	}
	candles, err := client.Candles(context.Background(), CandlesRequest{Symbol: "btc_krw", Interval: Candle1Hour, Limit: 5})
	if err != nil || len(candles) != 1 || candles[0].Close != "64000000" || len(candles[0].Raw) == 0 {
		t.Fatalf("Candles() = %+v, error = %v", candles, err)
	}
	pairs, err := client.CurrencyPairs(context.Background())
	if err != nil || len(pairs) != 1 || pairs[0].MinimumOrderValue != "5000" {
		t.Fatalf("CurrencyPairs() = %+v, error = %v", pairs, err)
	}
	policies, err := client.TickSizePolicy(context.Background(), TickSizePolicyRequest{Symbol: "btc_krw"})
	if err != nil || len(policies) != 1 || policies[0].TickSizePolicy[1].TickSize != "1000" {
		t.Fatalf("TickSizePolicy() = %+v, error = %v", policies, err)
	}
	balances, err := client.Balances(context.Background(), BalanceRequest{AccountSeq: 2, Currencies: []string{"krw"}})
	if err != nil || len(balances) != 1 || balances[0].Available != "900000" || len(balances[0].Raw) == 0 {
		t.Fatalf("Balances() = %+v, error = %v", balances, err)
	}
	placed, err := client.PlaceOrder(context.Background(), PlaceOrderRequest{
		Symbol: "btc_krw", AccountSeq: 2, Side: SideBuy, Price: "64000000", Qty: "0.01",
		OrderType: OrderTypeLimit, TimeInForce: TimeInForcePostOnly, ClientOrderID: "strategy-1",
	}, trade.WithEgressRoute("route-b"))
	if err != nil || placed.OrderID != 1234 || len(placed.Raw) == 0 {
		t.Fatalf("PlaceOrder() = %+v, error = %v", placed, err)
	}
	detail, err := client.OrderInfo(context.Background(), OrderInfoRequest{Symbol: "btc_krw", AccountSeq: 2, ClientOrderID: "strategy-1"})
	if err != nil || detail.Status != OrderStatusOpen || detail.FilledQty != "0" || len(detail.Raw) == 0 {
		t.Fatalf("OrderInfo() = %+v, error = %v", detail, err)
	}
	canceled, err := client.CancelOrder(context.Background(), CancelOrderRequest{Symbol: "btc_krw", AccountSeq: 2, OrderID: 1234})
	if err != nil || !canceled.Accepted || len(canceled.Raw) == 0 {
		t.Fatalf("CancelOrder() = %+v, error = %v", canceled, err)
	}
	openOrders, err := client.OpenOrders(context.Background(), OpenOrdersRequest{Symbol: "btc_krw", AccountSeq: 2, Limit: 100})
	if err != nil || len(openOrders) != 1 || openOrders[0].OrderID != 1234 {
		t.Fatalf("OpenOrders() = %+v, error = %v", openOrders, err)
	}
	end := time.UnixMilli(1700000000123)
	start := end.Add(-24 * time.Hour)
	history, err := client.OrderHistory(context.Background(), OrderHistoryRequest{Symbol: "btc_krw", AccountSeq: 2, Start: &start, End: &end, Limit: 100})
	if err != nil || len(history) != 1 || history[0].AveragePrice != "63000000" {
		t.Fatalf("OrderHistory() = %+v, error = %v", history, err)
	}
	myTrades, err := client.MyTrades(context.Background(), MyTradesRequest{Symbol: "btc_krw", AccountSeq: 2, Start: &start, End: &end, Limit: 100})
	if err != nil || len(myTrades) != 1 || myTrades[0].FeeQty != "1260" {
		t.Fatalf("MyTrades() = %+v, error = %v", myTrades, err)
	}

	routes := sender.snapshot()
	if len(routes) != 14 || routes[0] != "route-b" || routes[1] != "route-b" || routes[8] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
	providerCalls, key, secret := provider.snapshot()
	if providerCalls != 7 || !allZero(key) || !allZero(secret) {
		t.Fatalf("provider calls = %d, key zero = %v, secret zero = %v", providerCalls, allZero(key), allZero(secret))
	}
	publicSnapshot, err := limiter.Snapshot("korbit:route:route-b:public:1second")
	if err != nil || publicSnapshot.Used != 2 || publicSnapshot.Rule.Limit != 50 {
		t.Fatalf("public limiter snapshot = %+v, error = %v", publicSnapshot, err)
	}
	privateSnapshot, err := limiter.Snapshot("korbit:account:korbit-account:private:1second")
	if err != nil || privateSnapshot.Used != 5 || privateSnapshot.Rule.Limit != 50 {
		t.Fatalf("private limiter snapshot = %+v, error = %v", privateSnapshot, err)
	}
	placeSnapshot, err := limiter.Snapshot("korbit:account:korbit-account:order-place:1second")
	if err != nil || placeSnapshot.Used != 1 || placeSnapshot.Rule.Limit != 30 {
		t.Fatalf("place limiter snapshot = %+v, error = %v", placeSnapshot, err)
	}
	cancelSnapshot, err := limiter.Snapshot("korbit:account:korbit-account:order-cancel:1second")
	if err != nil || cancelSnapshot.Used != 1 || cancelSnapshot.Rule.Limit != 30 {
		t.Fatalf("cancel limiter snapshot = %+v, error = %v", cancelSnapshot, err)
	}
}

func TestClientRejectsCredentialRouteBeforeSecretResolution(t *testing.T) {
	t.Parallel()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, _ := newTestClient(t, "http://127.0.0.1", sender, provider, []transport.EgressRouteID{"route-a"})
	_, err := client.Balances(context.Background(), BalanceRequest{}, trade.WithEgressRoute("route-b"))
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("Balances() error = %v, want ErrAuthorization", err)
	}
	calls, _, _ := provider.snapshot()
	if calls != 0 || len(sender.snapshot()) != 0 {
		t.Fatalf("provider calls = %d, routes = %v", calls, sender.snapshot())
	}
}

func TestMutationNetworkAndMalformedSuccessAreUnknown(t *testing.T) {
	t.Parallel()

	request := PlaceOrderRequest{
		Symbol: "btc_krw", Side: SideBuy, Price: "64000000", Qty: "0.01",
		OrderType: OrderTypeLimit, ClientOrderID: "strategy-1",
	}
	networkClient, _ := newTestClient(
		t, "http://127.0.0.1", errorSender{}, &recordingProvider{}, []transport.EgressRouteID{"route-a"},
	)
	_, err := networkClient.PlaceOrder(context.Background(), request)
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceOrder() network error = %v, want ErrUnknownExecutionState", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"success":true,"data":{}}`)
	}))
	defer server.Close()
	decodeClient, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{}, []transport.EgressRouteID{"route-a"},
	)
	_, err = decodeClient.PlaceOrder(context.Background(), request)
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceOrder() malformed success error = %v, want ErrUnknownExecutionState", err)
	}
}

func TestLogicalErrorsAndClassification(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, `{"success":false,"error":{"code":400,"message":"NO_BALANCE"}}`)
	}))
	defer server.Close()
	client, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{}, []transport.EgressRouteID{"route-a"},
	)
	_, err := client.PlaceOrder(context.Background(), PlaceOrderRequest{
		Symbol: "btc_krw", Side: SideBuy, Price: "64000000", Qty: "0.01",
		OrderType: OrderTypeLimit, ClientOrderID: "strategy-1",
	})
	if !errors.Is(err, trade.ErrInsufficientBalance) {
		t.Fatalf("PlaceOrder() error = %v, want ErrInsufficientBalance", err)
	}
	tests := []struct {
		status    int
		code      string
		operation commonexchange.OperationKind
		category  trade.ErrorCategory
		retryable bool
	}{
		{429, "", commonexchange.OperationRead, trade.ErrorRateLimited, true},
		{401, "", commonexchange.OperationRead, trade.ErrorAuthentication, false},
		{403, "", commonexchange.OperationRead, trade.ErrorAuthorization, false},
		{400, "ORDER_NOT_FOUND", commonexchange.OperationRead, trade.ErrorOrderNotFound, false},
		{400, "TRY_AGAIN", commonexchange.OperationMutation, trade.ErrorExchangeUnavailable, true},
		{500, "", commonexchange.OperationMutation, trade.ErrorUnknownExecutionState, false},
	}
	for _, test := range tests {
		category, retryable := classifyError(test.status, test.code, test.operation)
		if category != test.category || retryable != test.retryable {
			t.Fatalf("classifyError(%d, %q) = (%s, %v), want (%s, %v)", test.status, test.code, category, retryable, test.category, test.retryable)
		}
	}
}

func TestRequestValidation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	tooOld := now.Add(-37 * time.Hour)
	invalid := []error{
		(TickersRequest{}).validate(),
		(TickersRequest{Symbols: []string{"btc_krw"}, AllSymbols: true}).validate(),
		(CandlesRequest{Symbol: "btc_krw", Interval: Candle1Hour}).validate(),
		(PlaceOrderRequest{Symbol: "btc_krw", Side: SideBuy, Price: "1", Qty: "1", OrderType: OrderTypeLimit}).validate(),
		(PlaceOrderRequest{Symbol: "btc_krw", Side: SideBuy, Qty: "1", OrderType: OrderTypeMarket, ClientOrderID: "one"}).validate(),
		(PlaceOrderRequest{Symbol: "btc_krw", Side: SideSell, Amount: "5000", OrderType: OrderTypeBest, BestNth: 1, TimeInForce: TimeInForceIOC, ClientOrderID: "two"}).validate(),
		(OrderInfoRequest{Symbol: "btc_krw", OrderID: 1, ClientOrderID: "two"}).validate(),
		(OrderHistoryRequest{Symbol: "btc_krw", Start: &tooOld, Limit: 100}).validate(now),
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
			AccountID: "korbit-account", Exchange: model.ExchangeKorbit, SecretRef: "secret/korbit",
			Permissions:           []credential.Permission{credential.PermissionRead, credential.PermissionTrade},
			AllowedEgressRouteIDs: allowedRoutes,
		},
		CredentialProvider: provider, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true,
		Now: func() time.Time { return time.UnixMilli(1700000000123) },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, limiter
}

func isKorbitPublicPath(path string) bool {
	switch path {
	case "/v2/time", "/v2/tickers", "/v2/orderbook", "/v2/trades", "/v2/candles",
		"/v2/currencyPairs", "/v2/tickSizePolicy":
		return true
	default:
		return false
	}
}

func verifyKorbitSignedRequest(t *testing.T, request *http.Request, secret []byte) {
	t.Helper()
	if request.Header.Get("X-KAPI-KEY") != "api-key" {
		t.Errorf("X-KAPI-KEY = %q", request.Header.Get("X-KAPI-KEY"))
	}
	unsigned := unsignedKorbitParameters(t, request)
	encoded := encodedKorbitParameters(t, request)
	_, signature, found := strings.Cut(encoded, "&signature=")
	if !found {
		t.Errorf("signed parameters have no signature: %q", encoded)
		return
	}
	decodedSignature, err := url.QueryUnescape(signature)
	if err != nil {
		t.Errorf("decode signature: %v", err)
		return
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(unsigned))
	want := hex.EncodeToString(mac.Sum(nil))
	if decodedSignature != want {
		t.Errorf("signature = %q, want %q", decodedSignature, want)
	}
	values, err := url.ParseQuery(unsigned)
	if err != nil {
		t.Errorf("parse unsigned parameters: %v", err)
	}
	if values.Get("timestamp") != "1700000000123" || values.Get("recvWindow") != "5000" {
		t.Errorf("signed time parameters = %v", values)
	}
}

func unsignedKorbitParameters(t *testing.T, request *http.Request) string {
	t.Helper()
	encoded := encodedKorbitParameters(t, request)
	unsigned, _, found := strings.Cut(encoded, "&signature=")
	if !found {
		t.Fatalf("signed parameters have no signature: %q", encoded)
	}
	return unsigned
}

func encodedKorbitParameters(t *testing.T, request *http.Request) string {
	t.Helper()
	if request.Method != http.MethodPost {
		return request.URL.RawQuery
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	request.Body = io.NopCloser(strings.NewReader(string(body)))
	return string(body)
}

func allZero(value []byte) bool {
	for _, item := range value {
		if item != 0 {
			return false
		}
	}
	return true
}
