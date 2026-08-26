package futures

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
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

func TestClientFuturesLifecycle(t *testing.T) {
	t.Parallel()

	fixedNow := time.Unix(1_700_000_000, 0)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/v4/futures/usdt/contracts" {
			writer.Header().Set("X-Gate-RateLimit-Limit", "200")
			writer.Header().Set("X-Gate-RateLimit-Requests-Remain", "198")
		}
		if isPrivatePath(request.URL.Path) && !verifySignedRequest(t, request, []byte("test-secret")) {
			http.Error(writer, `{"label":"INVALID_SIGNATURE","message":"invalid signature"}`, http.StatusUnauthorized)
			return
		}
		switch {
		case request.URL.Path == "/api/v4/futures/usdt/contracts":
			if request.URL.Query().Get("limit") != "100" || request.URL.Query().Get("offset") != "10" {
				http.Error(writer, `{"label":"INVALID_PARAM_VALUE","message":"bad page"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `[{"name":"BTC_USDT","type":"direct","quanto_multiplier":"0.0001","last_price":"64000","mark_price":"64001","index_price":"63999","order_price_round":"0.1","order_size_min":"1","enable_decimal":false,"leverage_max":"100","maker_fee_rate":"-0.00025","taker_fee_rate":"0.00075"}]`)
		case request.URL.Path == "/api/v4/futures/usdt/contracts/BTC_USDT":
			_, _ = io.WriteString(writer, `{"name":"BTC_USDT","type":"direct","last_price":"64000","enable_decimal":true}`)
		case request.URL.Path == "/api/v4/futures/usdt/tickers":
			if request.URL.Query().Get("contract") != "BTC_USDT" {
				http.Error(writer, `{"label":"INVALID_PARAM_VALUE","message":"bad contract"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `[{"contract":"BTC_USDT","last":"64000","mark_price":"64001","index_price":"63999","highest_bid":"63998","highest_size":"2","lowest_ask":"64002","lowest_size":"3"}]`)
		case request.URL.Path == "/api/v4/futures/usdt/order_book":
			if request.URL.Query().Get("contract") != "BTC_USDT" ||
				request.URL.Query().Get("limit") != "20" || request.URL.Query().Get("with_id") != "true" {
				http.Error(writer, `{"label":"INVALID_PARAM_VALUE","message":"bad order book"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"id":12,"current":1700000000.123,"update":1700000000.121,"asks":[{"p":"64001","s":"3"}],"bids":[{"p":"64000","s":"2"}]}`)
		case request.URL.Path == "/api/v4/futures/usdt/trades" && !isPrivatePath(request.URL.Path):
			_, _ = io.WriteString(writer, `[{"id":121234231,"create_time":1700000000,"create_time_ms":1700000000123,"contract":"BTC_USDT","size":"-2","price":"64000"}]`)
		case request.URL.Path == "/api/v4/futures/usdt/candlesticks":
			if request.URL.Query().Get("interval") != "1m" || request.URL.Query().Get("timezone") != "utc0" {
				http.Error(writer, `{"label":"INVALID_PARAM_VALUE","message":"bad candle"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `[{"t":1700000000,"v":"10","c":"64000","h":"65000","l":"62000","o":"63000","sum":"640000"}]`)
		case request.URL.Path == "/api/v4/futures/usdt/accounts":
			_, _ = io.WriteString(writer, `{"user":1666,"currency":"USDT","total":"1000","unrealised_pnl":"2","order_margin":"10","available":"990","in_dual_mode":false}`)
		case request.URL.Path == "/api/v4/futures/usdt/positions":
			if request.URL.Query().Get("holding") != "true" || request.URL.Query().Get("limit") != "100" {
				http.Error(writer, `{"label":"INVALID_PARAM_VALUE","message":"bad positions"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `[{"user":1666,"contract":"BTC_USDT","size":"-2","leverage":"10","value":"12800","margin":"1280","entry_price":"65000","liq_price":"70000","mark_price":"64000","unrealised_pnl":"20","realised_pnl":"1","mode":"single","pos_margin_mode":"isolated","lever":"10"}]`)
		case request.URL.Path == "/api/v4/futures/usdt/orders" && request.Method == http.MethodPost:
			body, _ := io.ReadAll(request.Body)
			if string(body) != `{"contract":"BTC_USDT","size":"2","price":"64000","tif":"gtc","text":"t-strategy-1"}` {
				http.Error(writer, `{"label":"INVALID_PARAM_VALUE","message":"unexpected body"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, orderJSON("open", ""))
		case request.URL.Path == "/api/v4/futures/usdt/orders/12345" && request.Method == http.MethodGet:
			_, _ = io.WriteString(writer, orderJSON("open", ""))
		case request.URL.Path == "/api/v4/futures/usdt/orders/12345" && request.Method == http.MethodDelete:
			_, _ = io.WriteString(writer, orderJSON("finished", "cancelled"))
		case request.URL.Path == "/api/v4/futures/usdt/orders" && request.Method == http.MethodGet:
			if request.URL.Query().Get("status") != "open" || request.URL.Query().Get("contract") != "BTC_USDT" {
				http.Error(writer, `{"label":"INVALID_PARAM_VALUE","message":"bad orders"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `[`+orderJSON("open", "")+`]`)
		case request.URL.Path == "/api/v4/futures/usdt/my_trades":
			if request.URL.Query().Get("order") != "12345" {
				http.Error(writer, `{"label":"INVALID_PARAM_VALUE","message":"bad trades"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `[{"id":88,"create_time":1700000000.123,"contract":"BTC_USDT","order_id":"12345","size":"2","price":"64000","text":"t-strategy-1","fee":"0.1","role":"taker","close_size":"0","trade_value":"128000"}]`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, limiter := newTestClient(
		t, server.URL, sender, provider,
		[]transport.EgressRouteID{"route-a", "route-b"}, func() time.Time { return fixedNow },
	)
	contracts, err := client.Contracts(context.Background(), ContractsRequest{
		Settlement: SettlementUSDT, Limit: 100, Offset: 10,
	}, trade.WithEgressRoute("route-b"))
	if err != nil || len(contracts) != 1 || contracts[0].LeverageMaximum != "100" || len(contracts[0].Raw) == 0 {
		t.Fatalf("Contracts() = %+v, error = %v", contracts, err)
	}
	contract, err := client.Contract(context.Background(), SettlementUSDT, "BTC_USDT")
	if err != nil || contract.Name != "BTC_USDT" || !contract.EnableDecimal || len(contract.Raw) == 0 {
		t.Fatalf("Contract() = %+v, error = %v", contract, err)
	}
	tickers, err := client.Tickers(context.Background(), TickersRequest{
		Settlement: SettlementUSDT, Contract: "BTC_USDT",
	})
	if err != nil || len(tickers) != 1 || tickers[0].HighestBidSize != "2" || len(tickers[0].Raw) == 0 {
		t.Fatalf("Tickers() = %+v, error = %v", tickers, err)
	}
	book, err := client.OrderBook(context.Background(), OrderBookRequest{
		Settlement: SettlementUSDT, Contract: "BTC_USDT", Limit: 20,
	})
	if err != nil || book.ID != 12 || book.Bids[0].Size != "2" ||
		book.Current != "1700000000.123" || len(book.Raw) == 0 {
		t.Fatalf("OrderBook() = %+v, error = %v", book, err)
	}
	trades, err := client.RecentTrades(context.Background(), TradesRequest{
		Settlement: SettlementUSDT, Contract: "BTC_USDT", Limit: 100,
	})
	if err != nil || len(trades) != 1 || trades[0].ID != "121234231" ||
		trades[0].CreatedAtMilli != "1700000000123" || len(trades[0].Raw) == 0 {
		t.Fatalf("RecentTrades() = %+v, error = %v", trades, err)
	}
	candles, err := client.Candles(context.Background(), CandlesRequest{
		Settlement: SettlementUSDT, Contract: "BTC_USDT", Interval: Candle1Minute, Limit: 100,
	})
	if err != nil || len(candles) != 1 || candles[0].Close != "64000" || len(candles[0].Raw) == 0 {
		t.Fatalf("Candles() = %+v, error = %v", candles, err)
	}
	account, err := client.Account(context.Background(), AccountRequest{Settlement: SettlementUSDT})
	if err != nil || account.Available != "990" || len(account.Raw) == 0 {
		t.Fatalf("Account() = %+v, error = %v", account, err)
	}
	positions, err := client.Positions(context.Background(), PositionsRequest{
		Settlement: SettlementUSDT, HoldingOnly: true, Limit: 100,
	})
	if err != nil || len(positions) != 1 || positions[0].Size != "-2" ||
		positions[0].PositionMarginMode != PositionMarginModeIsolated || len(positions[0].Raw) == 0 {
		t.Fatalf("Positions() = %+v, error = %v", positions, err)
	}
	placed, err := client.PlaceOrder(context.Background(), PlaceOrderRequest{
		Settlement: SettlementUSDT, Type: OrderTypeLimit, Contract: "BTC_USDT",
		Size: "2", Price: "64000", ClientOrderID: "t-strategy-1",
	}, trade.WithEgressRoute("route-b"))
	if err != nil || placed.ID != "12345" || len(placed.Raw) == 0 {
		t.Fatalf("PlaceOrder() = %+v, error = %v", placed, err)
	}
	order, err := client.OrderInfo(context.Background(), OrderInfoRequest{
		Settlement: SettlementUSDT, OrderID: "12345",
	})
	if err != nil || order.Status != "open" || len(order.Raw) == 0 {
		t.Fatalf("OrderInfo() = %+v, error = %v", order, err)
	}
	canceled, err := client.CancelOrder(context.Background(), CancelOrderRequest{
		Settlement: SettlementUSDT, OrderID: "12345",
	})
	if err != nil || canceled.FinishAs != "cancelled" || len(canceled.Raw) == 0 {
		t.Fatalf("CancelOrder() = %+v, error = %v", canceled, err)
	}
	orders, err := client.Orders(context.Background(), OrdersRequest{
		Settlement: SettlementUSDT, Contract: "BTC_USDT", Status: OrderStatusOpen, Limit: 100,
	})
	if err != nil || len(orders) != 1 || orders[0].ClientOrderID != "t-strategy-1" || len(orders[0].Raw) == 0 {
		t.Fatalf("Orders() = %+v, error = %v", orders, err)
	}
	myTrades, err := client.MyTrades(context.Background(), MyTradesRequest{
		Settlement: SettlementUSDT, OrderID: "12345", Limit: 100,
	})
	if err != nil || len(myTrades) != 1 || myTrades[0].ID != "88" || len(myTrades[0].Raw) == 0 {
		t.Fatalf("MyTrades() = %+v, error = %v", myTrades, err)
	}

	if routes := sender.snapshot(); len(routes) != 13 || routes[0] != "route-b" || routes[8] != "route-b" {
		t.Fatalf("sender routes = %v", routes)
	}
	calls, apiKey, secret := provider.snapshot()
	if calls != 7 || !allZero(apiKey) || !allZero(secret) {
		t.Fatalf("provider snapshot = calls %d, key %v, secret %v", calls, apiKey, secret)
	}
	publicSnapshot, err := limiter.Snapshot("gateio:route:route-b:futures-public:contracts:10seconds")
	if err != nil || publicSnapshot.Used != 2 {
		t.Fatalf("public rate snapshot = %+v, error = %v", publicSnapshot, err)
	}
	if _, err := limiter.Snapshot("gateio:account:gateio-main:futures-order:1second"); err != nil {
		t.Fatalf("order rate snapshot error = %v", err)
	}
	if _, err := limiter.Snapshot("gateio:account:gateio-main:futures-cancel:1second"); err != nil {
		t.Fatalf("cancel rate snapshot error = %v", err)
	}
}

func TestFuturesMarketOrderCanonicalization(t *testing.T) {
	t.Parallel()

	fixedNow := time.Unix(1_700_000_000, 0)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"contract":"BTC_USDT","size":"-0.25","price":"0","reduce_only":true,"tif":"ioc","text":"t-market-1","pos_margin_mode":"cross"}` {
			http.Error(writer, `{"label":"INVALID_PARAM_VALUE","message":"bad market body"}`, http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, orderJSON("open", ""))
	}))
	defer server.Close()

	client, _ := newTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, func() time.Time { return fixedNow },
	)
	order, err := client.PlaceOrder(context.Background(), PlaceOrderRequest{
		Settlement: SettlementUSDT, Type: OrderTypeMarket, Contract: "BTC_USDT",
		Size: "-0.25", ReduceOnly: true, ClientOrderID: "t-market-1",
		PositionMarginMode: PositionMarginModeCross,
	})
	if err != nil || order.ID != "12345" {
		t.Fatalf("PlaceOrder() = %+v, error = %v", order, err)
	}
}

func TestFuturesValidationAndExactJSONTypes(t *testing.T) {
	t.Parallel()

	invalid := []PlaceOrderRequest{
		{Settlement: "eth", Type: OrderTypeLimit, Contract: "BTC_USDT", Size: "1", Price: "1", ClientOrderID: "t-a"},
		{Settlement: SettlementUSDT, Type: OrderTypeLimit, Contract: "BTC-USDT", Size: "1", Price: "1", ClientOrderID: "t-a"},
		{Settlement: SettlementUSDT, Type: OrderTypeMarket, Contract: "BTC_USDT", Size: "1", Price: "2", ClientOrderID: "t-a"},
		{Settlement: SettlementUSDT, Type: OrderTypeLimit, Contract: "BTC_USDT", Size: "0", Price: "1", ClientOrderID: "t-a"},
		{Settlement: SettlementUSDT, Type: OrderTypeLimit, Contract: "BTC_USDT", Size: "0", Price: "1", ClientOrderID: "t-a", AutoSize: AutoSizeCloseLong},
	}
	for _, request := range invalid {
		if err := request.validate(); !errors.Is(err, trade.ErrValidation) {
			t.Fatalf("PlaceOrderRequest.validate(%+v) error = %v", request, err)
		}
	}
	if err := (CandlesRequest{
		Settlement: SettlementUSDT, Contract: "BTC_USDT", Interval: Candle1Minute, Limit: 2001,
	}).validate(); !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("CandlesRequest.validate() error = %v", err)
	}
	var decoded struct {
		ID        Identifier `json:"id"`
		Timestamp Decimal    `json:"timestamp"`
	}
	if err := json.Unmarshal([]byte(`{"id":9007199254740993,"timestamp":1.700000000123e9}`), &decoded); err != nil {
		t.Fatalf("decode exact JSON types: %v", err)
	}
	if decoded.ID != "9007199254740993" || decoded.Timestamp != "1.700000000123e9" {
		t.Fatalf("decoded exact JSON types = %+v", decoded)
	}
}

func TestFuturesUnknownMutationStateAndRouteAuthorization(t *testing.T) {
	t.Parallel()

	fixedNow := time.Unix(1_700_000_000, 0)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		http.Error(writer, `{"label":"SERVER_ERROR","message":"temporary"}`, http.StatusInternalServerError)
	}))
	defer server.Close()

	provider := &recordingProvider{}
	client, _ := newTestClient(
		t, server.URL, &directSender{}, provider,
		[]transport.EgressRouteID{"route-a"}, func() time.Time { return fixedNow },
	)
	request := PlaceOrderRequest{
		Settlement: SettlementUSDT, Type: OrderTypeLimit, Contract: "BTC_USDT",
		Size: "1", Price: "64000", ClientOrderID: "t-unknown-1",
	}
	_, err := client.PlaceOrder(context.Background(), request)
	if !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceOrder() error = %v", err)
	}
	_, err = client.PlaceOrder(context.Background(), request, trade.WithEgressRoute("route-b"))
	if !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("disallowed route error = %v", err)
	}
	calls, _, _ := provider.snapshot()
	if calls != 1 {
		t.Fatalf("credential provider calls = %d", calls)
	}
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	if _, err := New(Config{}); err == nil {
		t.Fatal("New(empty) error = nil")
	}
	limiter, _ := ratelimit.New()
	executor, _ := commonexchange.NewExecutor(commonexchange.ExecutorConfig{Sender: &directSender{}, Limiter: limiter})
	if _, err := New(Config{Executor: executor}); !errors.Is(err, trade.ErrMissingEgressRoute) {
		t.Fatalf("missing route error = %v", err)
	}
	if _, err := New(Config{
		Executor: executor, DefaultEgressRouteID: "route-a", BaseURL: "http://example.com",
	}); err == nil {
		t.Fatal("insecure base URL error = nil")
	}
}

func orderJSON(status, finishAs string) string {
	finish := ""
	if finishAs != "" {
		finish = `,"finish_time":1700000001,"finish_as":"` + finishAs + `"`
	}
	return `{"id":12345,"user":1666,"create_time":1700000000,"update_time":1700000000,"status":"` +
		status + `","contract":"BTC_USDT","size":"2","iceberg":"0","left":"2","price":"64000","fill_price":"0","tif":"gtc","is_reduce_only":false,"is_close":false,"is_liq":false,"text":"t-strategy-1"` + finish + `}`
}

func isPrivatePath(path string) bool {
	return path == "/api/v4/futures/usdt/accounts" ||
		path == "/api/v4/futures/usdt/positions" ||
		path == "/api/v4/futures/usdt/orders" ||
		path == "/api/v4/futures/usdt/orders/12345" ||
		path == "/api/v4/futures/usdt/my_trades"
}

func verifySignedRequest(t *testing.T, request *http.Request, secret []byte) bool {
	t.Helper()
	if request.Header.Get("KEY") != "test-api-key" || request.Header.Get("Timestamp") == "" {
		return false
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	payload := signaturePayload(
		request.Method, request.URL.EscapedPath(), request.URL.RawQuery,
		PayloadHash(body), request.Header.Get("Timestamp"),
	)
	mac := hmac.New(sha512.New, secret)
	_, _ = mac.Write(payload)
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(request.Header.Get("SIGN")), []byte(want))
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
		Sender: sender, Limiter: limiter,
	})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	client, err := New(Config{
		Executor: executor,
		Credentials: &credential.Descriptor{
			AccountID: "gateio-main", Exchange: model.ExchangeGateIO,
			SecretRef: "secret/gateio-main",
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
