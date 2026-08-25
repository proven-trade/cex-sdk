package htx

import (
	"context"
	"crypto/hmac"
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

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/ratelimit"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

type recordingProvider struct {
	mu         sync.Mutex
	calls      int
	lastAPIKey []byte
	lastSecret []byte
}

type providerFunc func(context.Context, string) (credential.Material, error)

type privateErrorSender struct{}

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
		APIKey: []byte("test-access"), SecretKey: []byte("test-secret"),
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

func (privateErrorSender) Do(
	context.Context,
	transport.EgressRouteID,
	*http.Request,
) (*http.Response, error) {
	return nil, errors.New("network disconnected")
}

func TestClientPrivateSpotLifecycle(t *testing.T) {
	t.Parallel()
	fixedNow := time.Date(2026, time.August, 25, 3, 4, 5, 0, time.UTC)
	start := fixedNow.Add(-24 * time.Hour)
	end := fixedNow
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if !verifyHTXSignedRequest(t, request, []byte("test-secret"), fixedNow) {
			http.Error(writer, `{"status":"error","err-code":"api-signature-not-valid"}`, http.StatusUnauthorized)
			return
		}
		switch {
		case request.URL.Path == "/v1/account/accounts" && request.Method == http.MethodGet:
			_, _ = io.WriteString(writer, `{"status":"ok","data":[{"id":12345,"type":"spot","subtype":"","state":"working"}]}`)
		case request.URL.Path == "/v1/account/accounts/12345/balance" && request.Method == http.MethodGet:
			_, _ = io.WriteString(writer, `{"status":"ok","data":{"id":"12345","type":"spot","state":"working","list":[{"currency":"usdt","type":"trade","balance":"900.25","seq-num":17}]}}`)
		case request.URL.Path == "/v1/order/orders/place" && request.Method == http.MethodPost:
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil ||
				body["account-id"] != "12345" || body["client-order-id"] != "strategy-1" ||
				body["symbol"] != "btcusdt" || body["type"] != "buy-limit" ||
				body["amount"] != "0.1" || body["price"] != "64000" ||
				body["source"] != "spot-api" || body["self-match-prevent"] != float64(1) {
				http.Error(writer, `{"status":"error","err-code":"invalid-parameter"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":"10001"}`)
		case request.URL.Path == "/v1/order/orders/10001" && request.Method == http.MethodGet:
			_, _ = io.WriteString(writer, `{"status":"ok","data":`+htxOrderJSON("submitted", false)+`}`)
		case request.URL.Path == "/v1/order/orders/submitCancelClientOrder" && request.Method == http.MethodPost:
			var body struct {
				ClientOrderID string `json:"client-order-id"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body.ClientOrderID != "strategy-1" {
				http.Error(writer, `{"status":"error","err-code":"invalid-parameter"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":10}`)
		case request.URL.Path == "/v1/order/openOrders" && request.Method == http.MethodGet:
			query := request.URL.Query()
			if query.Get("account-id") != "12345" || query.Get("symbol") != "btcusdt" ||
				query.Get("side") != "buy" || query.Get("from") != "10000" ||
				query.Get("direct") != "prev" || query.Get("size") != "20" {
				http.Error(writer, `{"status":"error","err-code":"invalid-parameter"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":[`+htxOrderJSON("submitted", true)+`]}`)
		case request.URL.Path == "/v1/order/orders" && request.Method == http.MethodGet:
			query := request.URL.Query()
			if query.Get("symbol") != "btcusdt" || query.Get("types") != "buy-limit" ||
				query.Get("states") != "filled" || query.Get("start-time") != "1787540645000" ||
				query.Get("end-time") != "1787627045000" || query.Get("size") != "100" {
				http.Error(writer, `{"status":"error","err-code":"invalid-parameter"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":[`+htxOrderJSON("filled", false)+`]}`)
		case request.URL.Path == "/v1/order/matchresults" && request.Method == http.MethodGet:
			if request.URL.Query().Get("types") != "buy-limit" || request.URL.Query().Get("size") != "100" {
				http.Error(writer, `{"status":"error","err-code":"invalid-parameter"}`, http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(writer, `{"status":"ok","data":[`+htxMatchJSON()+`]}`)
		case request.URL.Path == "/v1/order/orders/10001/matchresults" && request.Method == http.MethodGet:
			_, _ = io.WriteString(writer, `{"status":"ok","data":[`+htxMatchJSON()+`]}`)
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
	accounts, err := client.Accounts(ctx)
	if err != nil || len(accounts) != 1 || accounts[0].ID != "12345" || len(accounts[0].Raw) == 0 {
		t.Fatalf("Accounts() = %+v, error = %v", accounts, err)
	}
	balance, err := client.AccountBalance(ctx, "12345", trade.WithEgressRoute("route-b"))
	if err != nil || balance.ID != "12345" || len(balance.Balances) != 1 ||
		balance.Balances[0].Balance != "900.25" || balance.Balances[0].Sequence != "17" ||
		len(balance.Raw) == 0 || len(balance.Balances[0].Raw) == 0 {
		t.Fatalf("AccountBalance() = %+v, error = %v", balance, err)
	}
	placed, err := client.PlaceOrder(ctx, validHTXLimitOrder(), trade.WithEgressRoute("route-b"))
	if err != nil || placed.OrderID != "10001" || placed.ClientOrderID != "strategy-1" || len(placed.Raw) == 0 {
		t.Fatalf("PlaceOrder() = %+v, error = %v", placed, err)
	}
	order, err := client.OrderInfo(ctx, OrderInfoRequest{OrderID: "10001"})
	if err != nil || order.State != OrderStateSubmitted || order.FilledAmount != "0" || len(order.Raw) == 0 {
		t.Fatalf("OrderInfo() = %+v, error = %v", order, err)
	}
	canceled, err := client.CancelOrder(
		ctx, CancelOrderRequest{ClientOrderID: "strategy-1"}, trade.WithEgressRoute("route-b"),
	)
	if err != nil || canceled.ClientOrderID != "strategy-1" || canceled.StatusCode == nil ||
		*canceled.StatusCode != 10 || len(canceled.Raw) == 0 {
		t.Fatalf("CancelOrder() = %+v, error = %v", canceled, err)
	}
	openOrders, err := client.OpenOrders(ctx, OpenOrdersRequest{
		AccountID: "12345", Symbol: "btcusdt", Side: SideBuy,
		From: "10000", Direction: QueryDirectionPrev, Size: 20,
	})
	if err != nil || len(openOrders) != 1 || openOrders[0].FilledAmount != "0.01" ||
		openOrders[0].FilledCashAmount != "640" || openOrders[0].FilledFees != "0.00001" {
		t.Fatalf("OpenOrders() = %+v, error = %v", openOrders, err)
	}
	history, err := client.OrderHistory(ctx, OrderHistoryRequest{
		Symbol: "btcusdt", Types: []OrderType{OrderTypeBuyLimit},
		States: []OrderState{OrderStateFilled}, Start: &start, End: &end, Size: 100,
	})
	if err != nil || len(history) != 1 || history[0].State != OrderStateFilled || len(history[0].Raw) == 0 {
		t.Fatalf("OrderHistory() = %+v, error = %v", history, err)
	}
	matches, err := client.MatchResults(ctx, MatchResultsRequest{
		Symbol: "btcusdt", Types: []OrderType{OrderTypeBuyLimit},
		Start: &start, End: &end, Size: 100,
	})
	if err != nil || len(matches) != 1 || matches[0].TradeID != "30001" ||
		matches[0].FilledFees != "0.00001" || len(matches[0].Raw) == 0 {
		t.Fatalf("MatchResults() = %+v, error = %v", matches, err)
	}
	orderMatches, err := client.OrderMatches(ctx, "10001")
	if err != nil || len(orderMatches) != 1 || orderMatches[0].OrderID != "10001" {
		t.Fatalf("OrderMatches() = %+v, error = %v", orderMatches, err)
	}

	if routes := sender.snapshot(); !slices.Equal(routes, []transport.EgressRouteID{
		"route-a", "route-b", "route-b", "route-a", "route-b",
		"route-a", "route-a", "route-a", "route-a",
	}) {
		t.Fatalf("private sender routes = %v", routes)
	}
	calls, apiKey, secret := provider.snapshot()
	if calls != 9 || !allZero(apiKey) || !allZero(secret) {
		t.Fatalf("provider state calls=%d apiKey=%v secret=%v", calls, apiKey, secret)
	}
	assertHTXPrivateLimit(t, limiter, "htx:account:htx-main:account:2seconds", 2, DefaultAccountQuota)
	assertHTXPrivateLimit(t, limiter, "htx:account:htx-main:order:2seconds", 2, DefaultOrderQuota)
	assertHTXPrivateLimit(t, limiter, "htx:account:htx-main:order-read:2seconds", 4, DefaultOrderReadQuota)
	assertHTXPrivateLimit(t, limiter, "htx:account:htx-main:trade-history:2seconds", 1, DefaultTradeHistoryQuota)
}

func TestClientRejectsPrivateRouteAndPermissionBeforeSecretResolution(t *testing.T) {
	t.Parallel()
	provider := &recordingProvider{}
	client, _ := newPrivateTestClient(
		t, "http://127.0.0.1", &directSender{}, provider,
		[]transport.EgressRouteID{"route-a"}, []credential.Permission{credential.PermissionRead},
		time.Date(2026, time.August, 25, 3, 4, 5, 0, time.UTC),
	)
	if _, err := client.Accounts(
		context.Background(), trade.WithEgressRoute("route-b"),
	); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("Accounts() route error = %v, want authorization", err)
	}
	if _, err := client.PlaceOrder(
		context.Background(), validHTXLimitOrder(),
	); !errors.Is(err, trade.ErrAuthorization) {
		t.Fatalf("PlaceOrder() permission error = %v, want authorization", err)
	}
	if calls, _, _ := provider.snapshot(); calls != 0 {
		t.Fatalf("provider calls = %d, want 0", calls)
	}
}

func TestClientPrivateCredentialFailures(t *testing.T) {
	t.Parallel()
	publicClient, _ := newTestClient(t, "http://127.0.0.1", &directSender{})
	if _, err := publicClient.Accounts(context.Background()); !errors.Is(err, trade.ErrAuthentication) {
		t.Fatalf("Accounts() without credentials error = %v, want authentication", err)
	}

	apiKey := []byte("discard-access")
	secret := []byte("discard-secret")
	failing := providerFunc(func(context.Context, string) (credential.Material, error) {
		return credential.Material{APIKey: apiKey, SecretKey: secret}, errors.New("vault unavailable")
	})
	client, _ := newPrivateTestClient(
		t, "http://127.0.0.1", &directSender{}, failing,
		[]transport.EgressRouteID{"route-a"}, []credential.Permission{credential.PermissionRead},
		time.Date(2026, time.August, 25, 3, 4, 5, 0, time.UTC),
	)
	if _, err := client.Accounts(context.Background()); !errors.Is(err, trade.ErrAuthentication) {
		t.Fatalf("Accounts() provider error = %v, want authentication", err)
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
		time.Date(2026, time.August, 25, 3, 4, 5, 0, time.UTC),
	)
	if _, err := client.Accounts(context.Background()); !errors.Is(err, trade.ErrAuthentication) {
		t.Fatalf("Accounts() empty material error = %v, want authentication", err)
	}
}

func TestClientClassifiesPrivateErrorsAndUnknownMutationState(t *testing.T) {
	t.Parallel()
	fixedNow := time.Date(2026, time.August, 25, 3, 4, 5, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.URL.Path == "/v1/order/orders/place":
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(writer, `{"status":"error","err-code":"base-system-error","err-msg":"internal error"}`)
		case strings.HasSuffix(request.URL.Path, "/submitcancel"):
			_, _ = io.WriteString(writer, `{`)
		case request.URL.Path == "/v1/account/accounts":
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(writer, `{"status":"error","err-code":"order-accountbalance-error","err-msg":"insufficient balance"}`)
		default:
			writer.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(writer, `{"status":"error","err-code":"not-found","err-msg":"order not found"}`)
		}
	}))
	defer server.Close()
	client, _ := newPrivateTestClient(
		t, server.URL, &directSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"},
		[]credential.Permission{credential.PermissionRead, credential.PermissionTrade}, fixedNow,
	)
	if _, err := client.PlaceOrder(
		context.Background(), validHTXLimitOrder(),
	); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceOrder() error = %v, want unknown execution state", err)
	}
	if _, err := client.CancelOrder(
		context.Background(), CancelOrderRequest{OrderID: "10001"},
	); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("CancelOrder() error = %v, want unknown execution state", err)
	}
	if _, err := client.Accounts(context.Background()); !errors.Is(err, trade.ErrInsufficientBalance) {
		t.Fatalf("Accounts() error = %v, want insufficient balance", err)
	}
	if _, err := client.OrderInfo(
		context.Background(), OrderInfoRequest{OrderID: "10001"},
	); !errors.Is(err, trade.ErrOrderNotFound) {
		t.Fatalf("OrderInfo() error = %v, want order not found", err)
	}

	networkClient, _ := newPrivateTestClient(
		t, "http://127.0.0.1", privateErrorSender{}, &recordingProvider{},
		[]transport.EgressRouteID{"route-a"},
		[]credential.Permission{credential.PermissionTrade}, fixedNow,
	)
	if _, err := networkClient.PlaceOrder(
		context.Background(), validHTXLimitOrder(),
	); !errors.Is(err, trade.ErrUnknownExecutionState) {
		t.Fatalf("PlaceOrder() network error = %v, want unknown execution state", err)
	}
}

func TestPrivateRequestValidation(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC)
	tooLate := start.Add(48*time.Hour + time.Millisecond)
	tests := []struct {
		name string
		err  error
	}{
		{name: "account ID", err: (PlaceOrderRequest{ClientOrderID: "strategy", Symbol: "btcusdt", Side: SideBuy, Kind: OrderKindMarket, Amount: "10"}).validate()},
		{name: "client order ID", err: (PlaceOrderRequest{AccountID: "12345", Symbol: "btcusdt", Side: SideBuy, Kind: OrderKindMarket, Amount: "10"}).validate()},
		{name: "zero amount", err: (PlaceOrderRequest{AccountID: "12345", ClientOrderID: "strategy", Symbol: "btcusdt", Side: SideBuy, Kind: OrderKindMarket, Amount: "0"}).validate()},
		{name: "market price", err: (PlaceOrderRequest{AccountID: "12345", ClientOrderID: "strategy", Symbol: "btcusdt", Side: SideBuy, Kind: OrderKindMarket, Amount: "10", Price: "1"}).validate()},
		{name: "limit price", err: (PlaceOrderRequest{AccountID: "12345", ClientOrderID: "strategy", Symbol: "btcusdt", Side: SideBuy, Kind: OrderKindLimit, Amount: "1"}).validate()},
		{name: "order identity", err: (OrderInfoRequest{OrderID: "10001", ClientOrderID: "strategy"}).validate()},
		{name: "cancel identity", err: (CancelOrderRequest{}).validate()},
		{name: "open direction", err: (OpenOrdersRequest{Direction: QueryDirectionNext}).validate()},
		{name: "open size", err: (OpenOrdersRequest{Size: 501}).validate()},
		{name: "history states", err: (OrderHistoryRequest{Symbol: "btcusdt"}).validate()},
		{name: "history active state", err: (OrderHistoryRequest{Symbol: "btcusdt", States: []OrderState{OrderStateSubmitted}}).validate()},
		{name: "history range", err: (OrderHistoryRequest{Symbol: "btcusdt", States: []OrderState{OrderStateFilled}, Start: &start, End: &tooLate}).validate()},
		{name: "match size", err: (MatchResultsRequest{Symbol: "btcusdt", Size: 501}).validate()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !errors.Is(test.err, trade.ErrValidation) {
				t.Fatalf("validation error = %v", test.err)
			}
		})
	}
}

func TestHTXSignatureVector(t *testing.T) {
	t.Parallel()
	values := url.Values{
		"AccessKeyId":      {"test-access"},
		"SignatureMethod":  {"HmacSHA256"},
		"SignatureVersion": {"2"},
		"Timestamp":        {"2017-05-11T15:19:30"},
		"order-id":         {"1234567890"},
	}
	payload := SignaturePayload(
		http.MethodGet, "API.HUOBI.PRO", "/v1/order/orders", canonicalQuery(values),
	)
	got, err := SignHMACSHA256Base64([]byte("test-secret"), payload)
	if err != nil {
		t.Fatalf("SignHMACSHA256Base64() error = %v", err)
	}
	if want := "IFQqnxO4oK2FPlHQhMZXixQ2ioEPLb50Th2VuYVQtig="; got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
	if gotQuery := canonicalQuery(url.Values{"memo": {"a b"}}); gotQuery != "memo=a%20b" {
		t.Fatalf("canonical query = %q, want percent-encoded space", gotQuery)
	}
	if _, err := SignHMACSHA256Base64(nil, payload); err == nil {
		t.Fatal("SignHMACSHA256Base64() accepted an empty secret")
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
		AccountID: "htx-main", Exchange: model.ExchangeHTX, SecretRef: "secret/htx-main",
		Permissions:           []credential.Permission{credential.PermissionRead},
		AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"},
	}
	tests := []Config{
		{Executor: executor, DefaultEgressRouteID: "route-a", Credentials: descriptor},
		{Executor: executor, DefaultEgressRouteID: "route-a", CredentialProvider: &recordingProvider{}},
		{Executor: executor, DefaultEgressRouteID: "route-a", AccountQuota: -1},
		{Executor: executor, DefaultEgressRouteID: "route-a", OrderQuota: -1},
		{Executor: executor, DefaultEgressRouteID: "route-a", OrderReadQuota: -1},
		{Executor: executor, DefaultEgressRouteID: "route-a", TradeHistoryQuota: -1},
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
	if err != nil || client.accountQuota != DefaultAccountQuota ||
		client.orderQuota != DefaultOrderQuota || client.orderReadQuota != DefaultOrderReadQuota ||
		client.tradeHistoryQuota != DefaultTradeHistoryQuota {
		t.Fatalf("New() private defaults = %+v, error = %v", client, err)
	}
}

func verifyHTXSignedRequest(
	t *testing.T,
	request *http.Request,
	secret []byte,
	now time.Time,
) bool {
	t.Helper()
	if request.Header.Get("Authorization") != "" || request.Header.Get("X-API-Key") != "" ||
		request.Header.Get("Accept") != "application/json" ||
		request.Header.Get("Content-Type") != "application/json" {
		return false
	}
	values := request.URL.Query()
	signature := values.Get("Signature")
	values.Del("Signature")
	if values.Get("AccessKeyId") != "test-access" ||
		values.Get("SignatureMethod") != "HmacSHA256" || values.Get("SignatureVersion") != "2" ||
		values.Get("Timestamp") != "2026-08-25T03:04:05" ||
		now.Format("2006-01-02T15:04:05") != "2026-08-25T03:04:05" ||
		strings.Contains(values.Encode(), "test-secret") {
		return false
	}
	want, err := SignHMACSHA256Base64(
		secret,
		SignaturePayload(request.Method, request.Host, request.URL.Path, canonicalQuery(values)),
	)
	if err != nil {
		t.Fatalf("SignHMACSHA256Base64() error = %v", err)
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
			AccountID: "htx-main", Exchange: model.ExchangeHTX, SecretRef: "secret/htx-main",
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

func validHTXLimitOrder() PlaceOrderRequest {
	return PlaceOrderRequest{
		AccountID: "12345", ClientOrderID: "strategy-1", Symbol: "btcusdt",
		Side: SideBuy, Kind: OrderKindLimit, Amount: "0.1", Price: "64000",
		SelfMatchPrevent: true,
	}
}

func htxOrderJSON(state OrderState, alternateFields bool) string {
	filledFields := `"field-amount":"0","field-cash-amount":"0","field-fees":"0"`
	if alternateFields {
		filledFields = `"filled-amount":"0.01","filled-cash-amount":"640","filled-fees":"0.00001"`
	}
	return `{"id":10001,"account-id":12345,"client-order-id":"strategy-1","symbol":"btcusdt","amount":"0.1","price":"64000","created-at":1787627045000,"finished-at":0,"canceled-at":0,"type":"buy-limit",` + filledFields + `,"source":"spot-api","state":"` + string(state) + `"}`
}

func htxMatchJSON() string {
	return `{"id":20001,"symbol":"btcusdt","order-id":10001,"match-id":20001,"trade-id":"30001","price":"64000","created-at":1787627045000,"type":"buy-limit","filled-amount":"0.01","filled-fees":"0.00001","fee-currency":"btc","source":"spot-api","role":"maker"}`
}

func assertHTXPrivateLimit(
	t *testing.T,
	limiter *ratelimit.Limiter,
	key string,
	wantUsed int,
	wantLimit int,
) {
	t.Helper()
	snapshot, err := limiter.Snapshot(key)
	if err != nil || snapshot.Used != wantUsed || snapshot.Rule.Limit != wantLimit ||
		snapshot.Rule.Window != 2*time.Second {
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
