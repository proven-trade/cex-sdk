package cryptocom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		envelope, ok := verifyPrivateRequest(t, request, fixedNow)
		if !ok {
			http.Error(writer, `{"id":"0","method":"","code":"40101","message":"UNAUTHORIZED"}`, http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		switch envelope.Method {
		case methodUserBalance:
			writePrivateSuccess(writer, envelope, `{"data":[{"instrument_name":"USD","total_available_balance":"900","total_margin_balance":"1000","total_initial_margin":"10","total_maintenance_margin":"5","total_position_cost":"0","total_cash_balance":"1000","total_collateral_value":"995","total_session_unrealized_pnl":"0","total_session_realized_pnl":"1","is_liquidating":false,"total_effective_leverage":"0","position_limit":"1000000","used_position_limit":"0","total_isolated_cash_balance":"0","position_balances":[{"instrument_name":"USDT","quantity":"1000","market_value":"1000","collateral_eligible":true,"haircut":"0.005","collateral_amount":"995","max_withdrawal_balance":"900","reserved_qty":"100","hourly_interest_rate":"0"}],"isolated_positions":[]}]}`)
		case methodCreateOrder:
			if stringParam(envelope.Params, "instrument_name") != "BTC_USDT" ||
				stringParam(envelope.Params, "side") != "BUY" ||
				stringParam(envelope.Params, "type") != "LIMIT" ||
				stringParam(envelope.Params, "price") != "64000" ||
				stringParam(envelope.Params, "quantity") != "0.1" ||
				stringParam(envelope.Params, "client_oid") != "strategy-1" ||
				stringParam(envelope.Params, "time_in_force") != "GOOD_TILL_CANCEL" ||
				stringParam(envelope.Params, "notional") != "" ||
				!stringSliceParamEquals(envelope.Params, "exec_inst", []string{"POST_ONLY"}) {
				http.Error(writer, `{"id":"0","method":"private/create-order","code":"40001"}`, http.StatusBadRequest)
				return
			}
			writePrivateSuccess(writer, envelope, `{"client_oid":"strategy-1","order_id":"18342311"}`)
		case methodGetOrderDetail:
			if stringParam(envelope.Params, "order_id") != "18342311" {
				http.Error(writer, `{"id":"0","method":"private/get-order-detail","code":"212"}`, http.StatusBadRequest)
				return
			}
			writePrivateSuccess(writer, envelope, privateOrderJSON("ACTIVE"))
		case methodCancelOrder:
			if stringParam(envelope.Params, "client_oid") != "strategy-1" {
				http.Error(writer, `{"id":"0","method":"private/cancel-order","code":"212"}`, http.StatusBadRequest)
				return
			}
			writePrivateSuccess(writer, envelope, `{"client_oid":"strategy-1","order_id":"18342311"}`)
		case methodGetOpenOrders:
			if stringParam(envelope.Params, "instrument_name") != "BTC_USDT" {
				http.Error(writer, `{"id":"0","method":"private/get-open-orders","code":"40001"}`, http.StatusBadRequest)
				return
			}
			writePrivateSuccess(writer, envelope, `{"data":[`+privateOrderJSON("ACTIVE")+`]}`)
		case methodGetOrderHistory:
			if stringParam(envelope.Params, "instrument_name") != "BTC_USDT" ||
				stringParam(envelope.Params, "start_time") != "1699900000000000000" ||
				stringParam(envelope.Params, "end_time") != "1699986400000000000" ||
				stringParam(envelope.Params, "limit") != "100" {
				http.Error(writer, `{"id":"0","method":"private/get-order-history","code":"40001"}`, http.StatusBadRequest)
				return
			}
			writePrivateSuccess(writer, envelope, `{"data":[`+privateOrderJSON("FILLED")+`]}`)
		case methodGetAccountTrades:
			if stringParam(envelope.Params, "instrument_name") != "BTC_USDT" ||
				stringParam(envelope.Params, "limit") != "100" {
				http.Error(writer, `{"id":"0","method":"private/get-trades","code":"40001"}`, http.StatusBadRequest)
				return
			}
			writePrivateSuccess(writer, envelope, `{"data":[{"account_id":"account-1","event_date":"2026-08-25","journal_type":"TRADING","traded_quantity":"0.1","traded_price":"64000","fees":"-0.0001","fee_credits":"0.00002","order_id":"18342311","trade_id":"9007199254740993","trade_match_id":"9007199254740994","client_oid":"strategy-1","taker_side":"MAKER","side":"BUY","instrument_name":"BTC_USDT","fee_instrument_name":"USDT","create_time":"1700000000000","create_time_ns":"1700000000000123456","match_count":"1","match_index":0,"transact_time_ns":"1700000000000123999"}]}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sender := &directSender{}
	provider := &recordingProvider{}
	client, limiter := newPrivateTestClient(
		t, server.URL+"/exchange/v1", sender, provider,
		[]transport.EgressRouteID{"route-a", "route-b"},
		[]credential.Permission{credential.PermissionRead, credential.PermissionTrade}, fixedNow,
	)
	ctx := context.Background()
	balance, err := client.Balance(ctx, trade.WithEgressRoute("route-b"))
	if err != nil || len(balance.Accounts) != 1 || balance.Accounts[0].TotalCashBalance != "1000" ||
		len(balance.Accounts[0].PositionBalances) != 1 ||
		balance.Accounts[0].PositionBalances[0].InstrumentName != "USDT" ||
		balance.Accounts[0].PositionBalances[0].ReservedQuantity != "100" ||
		len(balance.Accounts[0].PositionBalances[0].Raw) == 0 || len(balance.Raw) == 0 {
		t.Fatalf("Balance() = %+v, error = %v", balance, err)
	}
	placed, err := client.PlaceOrder(ctx, PlaceOrderRequest{
		InstrumentName: "BTC_USDT", Side: OrderSideBuy, Type: OrderTypeLimit,
		Price: "64000", Quantity: "0.1", ClientOrderID: "strategy-1",
		TimeInForce: TimeInForceGoodTillCancel, PostOnly: true,
	}, trade.WithEgressRoute("route-b"))
	if err != nil || placed.OrderID != "18342311" || placed.ClientOrderID != "strategy-1" ||
		len(placed.Raw) == 0 {
		t.Fatalf("PlaceOrder() = %+v, error = %v", placed, err)
	}
	order, err := client.OrderInfo(ctx, OrderInfoRequest{OrderID: "18342311"})
	if err != nil || order.Status != OrderStatusActive || order.LimitPrice != "64000" ||
		order.CreateTimeNS != "1700000000000123456" || len(order.Raw) == 0 {
		t.Fatalf("OrderInfo() = %+v, error = %v", order, err)
	}
	canceled, err := client.CancelOrder(
		ctx, CancelOrderRequest{ClientOrderID: "strategy-1"},
		trade.WithEgressRoute("route-b"),
	)
	if err != nil || canceled.OrderID != "18342311" ||
		canceled.ClientOrderID != "strategy-1" || len(canceled.Raw) == 0 {
		t.Fatalf("CancelOrder() = %+v, error = %v", canceled, err)
	}
	openOrders, err := client.OpenOrders(ctx, OpenOrdersRequest{InstrumentName: "BTC_USDT"})
	if err != nil || len(openOrders) != 1 || openOrders[0].Status != OrderStatusActive {
		t.Fatalf("OpenOrders() = %+v, error = %v", openOrders, err)
	}
	history, err := client.OrderHistory(ctx, OrderHistoryRequest{
		InstrumentName: "BTC_USDT", Start: &start, End: &end, Limit: 100,
	})
	if err != nil || len(history) != 1 || history[0].Status != OrderStatusFilled {
		t.Fatalf("OrderHistory() = %+v, error = %v", history, err)
	}
	trades, err := client.AccountTrades(ctx, AccountTradesRequest{
		InstrumentName: "BTC_USDT", Limit: 100,
	})
	if err != nil || len(trades) != 1 || trades[0].TradeID != "9007199254740993" ||
		trades[0].FeeCredits != "0.00002" || trades[0].MatchCount != 1 ||
		trades[0].TransactionTimeNS != "1700000000000123999" || len(trades[0].Raw) == 0 {
		t.Fatalf("AccountTrades() = %+v, error = %v", trades, err)
	}

	if routes := sender.snapshot(); !slices.Equal(routes, []transport.EgressRouteID{
		"route-b", "route-b", "route-a", "route-b", "route-a", "route-a", "route-a",
	}) {
		t.Fatalf("private sender routes = %v", routes)
	}
	calls, apiKey, secret := provider.snapshot()
	if calls != 7 || !allZero(apiKey) || !allZero(secret) {
		t.Fatalf("provider state calls=%d apiKey=%v secret=%v", calls, apiKey, secret)
	}
	assertPrivateLimit(t, limiter, "cryptocom:account:cryptocom-main:private:user-balance:100milliseconds", 1, 3)
	assertPrivateLimit(t, limiter, "cryptocom:account:cryptocom-main:private:create-order:100milliseconds", 1, 15)
	assertPrivateLimit(t, limiter, "cryptocom:account:cryptocom-main:private:get-order-detail:100milliseconds", 1, 30)
	assertPrivateLimit(t, limiter, "cryptocom:account:cryptocom-main:private:get-order-history:1second", 1, 1)
	assertPrivateLimit(t, limiter, "cryptocom:account:cryptocom-main:private:get-trades:1second", 1, 1)
}

func TestClientRejectsPrivateRouteAndPermissionBeforeSecretResolution(t *testing.T) {
	t.Parallel()
	provider := &recordingProvider{}
	client, _ := newPrivateTestClient(
		t, "http://127.0.0.1/exchange/v1", &directSender{}, provider,
		[]transport.EgressRouteID{"route-a"}, []credential.Permission{credential.PermissionRead},
		time.UnixMilli(1_700_000_000_000),
	)
	if _, err := client.Balance(
		context.Background(), trade.WithEgressRoute("route-b"),
	); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("Balance() route error = %v, want authorization", err)
	}
	if _, err := client.PlaceOrder(context.Background(), validLimitOrder()); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("PlaceOrder() permission error = %v, want authorization", err)
	}
	if calls, _, _ := provider.snapshot(); calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestClientPrivateCredentialFailuresAndSecretDestruction(t *testing.T) {
	t.Parallel()
	publicClient, _ := newTestClient(t, "http://127.0.0.1/exchange/v1", &directSender{})
	if _, err := publicClient.Balance(context.Background()); !errors.Is(err, trade.ErrAuthentication) {
		t.Fatalf("Balance() without credentials error = %v, want authentication", err)
	}

	apiKey := []byte("discard-api-key")
	secret := []byte("discard-secret")
	failing := providerFunc(func(context.Context, string) (credential.Material, error) {
		return credential.Material{APIKey: apiKey, SecretKey: secret}, errors.New("vault unavailable")
	})
	client, _ := newPrivateTestClient(
		t, "http://127.0.0.1/exchange/v1", &directSender{}, failing,
		[]transport.EgressRouteID{"route-a"}, []credential.Permission{credential.PermissionRead},
		time.UnixMilli(1_700_000_000_000),
	)
	if _, err := client.Balance(context.Background()); !errors.Is(err, trade.ErrAuthentication) {
		t.Fatalf("Balance() provider error = %v, want authentication", err)
	}
	if !allZero(apiKey) || !allZero(secret) {
		t.Fatalf("failed provider material was not destroyed: key=%v secret=%v", apiKey, secret)
	}
}

func TestPrivateClientConfigurationValidation(t *testing.T) {
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
		AccountID: "cryptocom-main", Exchange: model.ExchangeCryptoCom,
		SecretRef: "secret/cryptocom-main", Permissions: []credential.Permission{credential.PermissionRead},
		AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"},
	}
	provider := &recordingProvider{}
	tests := []struct {
		name   string
		config Config
	}{
		{name: "missing provider", config: Config{Executor: executor, DefaultEgressRouteID: "route-a", Credentials: descriptor}},
		{name: "provider without descriptor", config: Config{Executor: executor, DefaultEgressRouteID: "route-a", CredentialProvider: provider}},
		{name: "wrong exchange", config: Config{Executor: executor, DefaultEgressRouteID: "route-a", Credentials: &credential.Descriptor{AccountID: "other", Exchange: model.ExchangeBinance, SecretRef: "secret/other", Permissions: []credential.Permission{credential.PermissionRead}, AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"}}, CredentialProvider: provider}},
		{name: "order quota", config: Config{Executor: executor, DefaultEgressRouteID: "route-a", OrderRequestsPer100Milliseconds: 16}},
		{name: "detail quota", config: Config{Executor: executor, DefaultEgressRouteID: "route-a", OrderDetailRequestsPer100Milliseconds: 31}},
		{name: "history quota", config: Config{Executor: executor, DefaultEgressRouteID: "route-a", HistoryRequestsPerSecond: 2}},
		{name: "other quota", config: Config{Executor: executor, DefaultEgressRouteID: "route-a", OtherPrivateRequestsPer100Milliseconds: 4}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := New(test.config); err == nil {
				t.Fatal("New() error = nil")
			}
		})
	}
}

func TestPrivateMutationUnknownStateAndKnownRejections(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		envelope, ok := verifyPrivateRequest(t, request, time.UnixMilli(1_700_000_000_000))
		if !ok {
			http.Error(writer, `{"code":"40101"}`, http.StatusUnauthorized)
			return
		}
		switch stringParam(envelope.Params, "client_oid") {
		case "broken":
			_, _ = io.WriteString(writer, `{`)
		case "down":
			writer.WriteHeader(http.StatusInternalServerError)
			writePrivateError(writer, envelope, "50001", "INTERNAL_SERVER_ERROR")
		case "poor":
			writer.WriteHeader(http.StatusInternalServerError)
			writePrivateError(writer, envelope, "306", "INSUFFICIENT_AVAILABLE_BALANCE")
		case "rate":
			writer.Header().Set("Retry-After", "3")
			writer.WriteHeader(http.StatusTooManyRequests)
			writePrivateError(writer, envelope, "42901", "TOO_MANY_REQUESTS")
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, limiter := newPrivateTestClient(
		t, server.URL+"/exchange/v1", &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a", "route-rate"},
		[]credential.Permission{credential.PermissionRead, credential.PermissionTrade},
		time.UnixMilli(1_700_000_000_000),
	)
	for _, clientOrderID := range []string{"broken", "down"} {
		request := validLimitOrder()
		request.ClientOrderID = clientOrderID
		if _, err := client.PlaceOrder(context.Background(), request); !errors.Is(err, trade.ErrUnknownExecutionState) {
			t.Fatalf("PlaceOrder(%q) error = %v, want unknown execution state", clientOrderID, err)
		}
	}
	poor := validLimitOrder()
	poor.ClientOrderID = "poor"
	if _, err := client.PlaceOrder(context.Background(), poor); !errors.Is(err, trade.ErrInsufficientBalance) {
		t.Fatalf("PlaceOrder(poor) error = %v, want insufficient balance", err)
	}
	rate := validLimitOrder()
	rate.ClientOrderID = "rate"
	_, err := client.PlaceOrder(
		context.Background(), rate, trade.WithEgressRoute("route-rate"),
	)
	if !errors.Is(err, trade.ErrRateLimited) {
		t.Fatalf("PlaceOrder(rate) error = %v, want rate limited", err)
	}
	snapshot, snapshotErr := limiter.Snapshot(
		"cryptocom:account:cryptocom-main:private:create-order:100milliseconds",
	)
	if snapshotErr != nil || !snapshot.BlockedUntil.After(time.Now().Add(2*time.Second)) {
		t.Fatalf("private Retry-After snapshot = %+v, error = %v", snapshot, snapshotErr)
	}
}

func TestPrivateReadOrderNotFoundAndMalformedResponse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		envelope, ok := verifyPrivateRequest(t, request, time.UnixMilli(1_700_000_000_000))
		if !ok {
			http.Error(writer, `{"code":"40101"}`, http.StatusUnauthorized)
			return
		}
		if stringParam(envelope.Params, "order_id") == "212" {
			writePrivateError(writer, envelope, "212", "INVALID_ORDERID")
			return
		}
		_, _ = io.WriteString(writer, `{`)
	}))
	defer server.Close()
	client, _ := newPrivateTestClient(
		t, server.URL+"/exchange/v1", &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"}, []credential.Permission{credential.PermissionRead},
		time.UnixMilli(1_700_000_000_000),
	)
	if _, err := client.OrderInfo(context.Background(), OrderInfoRequest{OrderID: "212"}); !errors.Is(err, trade.ErrOrderNotFound) {
		t.Fatalf("OrderInfo(212) error = %v, want order not found", err)
	}
	if _, err := client.OrderInfo(context.Background(), OrderInfoRequest{OrderID: "213"}); !errors.Is(err, trade.ErrExchangeUnavailable) {
		t.Fatalf("OrderInfo(213) error = %v, want exchange unavailable", err)
	}
}

func verifyPrivateRequest(
	t *testing.T,
	request *http.Request,
	now time.Time,
) (privateRequestEnvelope, bool) {
	t.Helper()
	if request.Method != http.MethodPost ||
		request.Header.Get("Accept") != "application/json" ||
		request.Header.Get("Content-Type") != "application/json" ||
		request.Header.Get("User-Agent") != "cex-sdk-go/0" {
		return privateRequestEnvelope{}, false
	}
	var envelope privateRequestEnvelope
	decoder := json.NewDecoder(request.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil || envelope.APIKey != "test-api-key" ||
		envelope.Nonce != fmt.Sprint(now.UnixMilli()) || envelope.ID == "" ||
		request.URL.Path != "/exchange/v1/"+envelope.Method {
		return privateRequestEnvelope{}, false
	}
	want, err := Sign(
		envelope.Method, envelope.ID, []byte(envelope.APIKey), envelope.Params,
		envelope.Nonce, []byte("test-secret"),
	)
	if err != nil || envelope.Sig != want {
		return privateRequestEnvelope{}, false
	}
	return envelope, true
}

func writePrivateSuccess(
	writer http.ResponseWriter,
	envelope privateRequestEnvelope,
	result string,
) {
	_, _ = fmt.Fprintf(
		writer, `{"id":%q,"method":%q,"code":"0","result":%s}`,
		envelope.ID, envelope.Method, result,
	)
}

func writePrivateError(
	writer http.ResponseWriter,
	envelope privateRequestEnvelope,
	code string,
	message string,
) {
	_, _ = fmt.Fprintf(
		writer, `{"id":%q,"method":%q,"code":%q,"message":%q}`,
		envelope.ID, envelope.Method, code, message,
	)
}

func privateOrderJSON(status string) string {
	return fmt.Sprintf(`{"account_id":"account-1","order_id":"18342311","client_oid":"strategy-1","order_type":"LIMIT","time_in_force":"GOOD_TILL_CANCEL","side":"BUY","exec_inst":["POST_ONLY"],"quantity":"0.1","limit_price":"64000","order_value":"6400","maker_fee_rate":"0.00025","taker_fee_rate":"0.0004","avg_price":"64000","cumulative_quantity":"0.1","cumulative_value":"6400","cumulative_fee":"0.0001","status":%q,"update_user_id":"user-1","order_date":"2026-08-25","instrument_name":"BTC_USDT","fee_instrument_name":"USDT","create_time":"1700000000000","create_time_ns":"1700000000000123456","update_time":1700000000001}`, status)
}

func stringParam(params map[string]any, key string) string {
	value, _ := params[key].(string)
	return value
}

func stringSliceParamEquals(params map[string]any, key string, want []string) bool {
	value, ok := params[key].([]any)
	if !ok || len(value) != len(want) {
		return false
	}
	for index, item := range value {
		if item != want[index] {
			return false
		}
	}
	return true
}

func validLimitOrder() PlaceOrderRequest {
	return PlaceOrderRequest{
		InstrumentName: "BTC_USDT", Side: OrderSideBuy, Type: OrderTypeLimit,
		Price: "64000", Quantity: "0.1", ClientOrderID: "strategy-1",
	}
}

func newPrivateTestClient(
	t *testing.T,
	baseURL string,
	sender commonexchange.Sender,
	provider credential.Provider,
	routes []transport.EgressRouteID,
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
		Executor: executor, Credentials: &credential.Descriptor{
			AccountID: "cryptocom-main", Exchange: model.ExchangeCryptoCom,
			SecretRef: "secret/cryptocom-main", Permissions: permissions,
			AllowedEgressRouteIDs: routes,
		},
		CredentialProvider: provider, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, limiter
}

func assertPrivateLimit(
	t *testing.T,
	limiter *ratelimit.Limiter,
	key string,
	used int,
	limit int,
) {
	t.Helper()
	snapshot, err := limiter.Snapshot(key)
	if err != nil || snapshot.Used != used || snapshot.Rule.Limit != limit {
		t.Fatalf("limiter snapshot %q = %+v, error = %v", key, snapshot, err)
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
