package binance

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

const (
	harnessWeightKey = "binance:route:route-a:request_weight:1minute"
	harnessOrdersKey = "binance:account:binance-main:orders:10second"
)

// extensionHarness는 받은 요청을 기록하고 정해진 응답을 돌려주는 테스트 서버와 클라이언트를 묶는다.
// 하나의 harness에서는 요청 한 건만 보내 weight와 주문 count를 그대로 검증한다.
type extensionHarness struct {
	client   *Client
	limiter  *ratelimit.Limiter
	sender   *directSender
	provider *recordingProvider
	mu       sync.Mutex
	requests []*http.Request
}

func newExtensionHarness(t *testing.T, status int, body string) *extensionHarness {
	t.Helper()
	return newPermissionHarness(t, status, body, credential.PermissionRead, credential.PermissionTrade)
}

func newPermissionHarness(
	t *testing.T,
	status int,
	body string,
	permissions ...credential.Permission,
) *extensionHarness {
	t.Helper()
	harness := &extensionHarness{sender: &directSender{}, provider: &recordingProvider{}}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		harness.mu.Lock()
		harness.requests = append(harness.requests, request.Clone(context.Background()))
		harness.mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_, _ = io.WriteString(writer, body)
	}))
	t.Cleanup(server.Close)

	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{
		Sender:  harness.sender,
		Limiter: limiter,
	})
	if err != nil {
		t.Fatalf("exchange.NewExecutor() error = %v", err)
	}
	descriptor := &credential.Descriptor{
		AccountID:             "binance-main",
		Exchange:              model.ExchangeBinance,
		SecretRef:             "secret/binance/main",
		Permissions:           permissions,
		AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"},
	}
	client, err := New(Config{
		Executor:             executor,
		Credentials:          descriptor,
		CredentialProvider:   harness.provider,
		DefaultEgressRouteID: "route-a",
		BaseURL:              server.URL,
		AllowInsecureHTTP:    true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	harness.client = client
	harness.limiter = limiter
	return harness
}

func (harness *extensionHarness) requestCount() int {
	harness.mu.Lock()
	defer harness.mu.Unlock()
	return len(harness.requests)
}

func (harness *extensionHarness) last(t *testing.T) *http.Request {
	t.Helper()
	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.requests) == 0 {
		t.Fatal("no request reached the server")
	}
	return harness.requests[len(harness.requests)-1]
}

// expect는 마지막 요청의 메서드, 경로, 서명 여부와 파라미터, 그리고 소비한 weight와 주문 count를 검증한다.
// params는 "key=value" 형식이며 서명 요청의 timestamp, recvWindow, signature는 비교에서 제외한다.
func (harness *extensionHarness) expect(
	t *testing.T,
	method, path string,
	signed bool,
	weight, orderUnits int,
	params ...string,
) {
	t.Helper()
	request := harness.last(t)
	if request.Method != method || request.URL.Path != path {
		t.Fatalf("request = %s %s, want %s %s", request.Method, request.URL.Path, method, path)
	}
	query := request.URL.Query()
	if signed {
		if !verifySignedRequest(t, request, []byte("test-secret-key")) {
			t.FailNow()
		}
		query.Del("timestamp")
		query.Del("recvWindow")
		query.Del("signature")
	} else if request.Header.Get("X-MBX-APIKEY") != "" || query.Has("signature") {
		t.Fatalf("public request carried credentials: header = %q, query = %v", request.Header.Get("X-MBX-APIKEY"), query)
	}
	got := flattenQuery(query)
	want := slices.Clone(params)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("query = %v, want %v", got, want)
	}

	weightSnapshot, err := harness.limiter.Snapshot(harnessWeightKey)
	if err != nil {
		t.Fatalf("limiter.Snapshot(weight) error = %v", err)
	}
	if weightSnapshot.Used != weight {
		t.Fatalf("request weight = %d, want %d", weightSnapshot.Used, weight)
	}
	ordersUsed := 0
	if ordersSnapshot, err := harness.limiter.Snapshot(harnessOrdersKey); err == nil {
		ordersUsed = ordersSnapshot.Used
	}
	if ordersUsed != orderUnits {
		t.Fatalf("order count = %d, want %d", ordersUsed, orderUnits)
	}
}

func flattenQuery(values url.Values) []string {
	pairs := make([]string, 0, len(values))
	for key, items := range values {
		for _, item := range items {
			pairs = append(pairs, key+"="+item)
		}
	}
	slices.Sort(pairs)
	return pairs
}

// requireValidation은 err가 SDK 검증 오류이고 서버에 요청이 가지 않았는지 확인한다.
func (harness *extensionHarness) requireValidation(t *testing.T, name string, err error) {
	t.Helper()
	if !errors.Is(err, trade.ErrValidation) {
		t.Fatalf("%s error = %v, want ErrValidation", name, err)
	}
	if count := harness.requestCount(); count != 0 {
		t.Fatalf("%s sent %d requests, want 0", name, count)
	}
}

// TestMutationMethodsRequireTradePermission은 읽기 전용 자격증명으로 모든 변경 메서드가 요청 전에 거부되는지 확인한다.
func TestMutationMethodsRequireTradePermission(t *testing.T) {
	t.Parallel()

	harness := newPermissionHarness(t, http.StatusOK, `{}`, credential.PermissionRead)
	ctx := context.Background()
	orderID := int64(7)
	_, testErr := harness.client.TestOrder(ctx, TestOrderRequest{Order: sampleLimitOrder()})
	_, cancelAllErr := harness.client.CancelOpenOrders(ctx, CancelOpenOrdersRequest{Symbol: "BTCUSDT"})
	_, replaceErr := harness.client.CancelReplaceOrder(ctx, sampleCancelReplaceRequest())
	_, amendErr := harness.client.AmendOrderKeepPriority(ctx, AmendOrderRequest{Symbol: "BTCUSDT", OrderID: &orderID, NewQuantity: "1"})
	_, ocoErr := harness.client.NewOCOOrderList(ctx, sampleOCORequest())
	_, otoErr := harness.client.NewOTOOrderList(ctx, sampleOTORequest())
	_, otocoErr := harness.client.NewOTOCOOrderList(ctx, sampleOTOCORequest())
	_, opoErr := harness.client.NewOPOOrderList(ctx, sampleOPORequest())
	_, opocoErr := harness.client.NewOPOCOOrderList(ctx, sampleOPOCORequest())
	_, cancelListErr := harness.client.CancelOrderList(ctx, CancelOrderListRequest{Symbol: "BTCUSDT", OrderListID: &orderID})
	_, sorErr := harness.client.SOROrder(ctx, sampleSORRequest())
	_, sorTestErr := harness.client.TestSOROrder(ctx, TestSOROrderRequest{Order: sampleSORRequest()})

	for name, err := range map[string]error{
		"TestOrder":              testErr,
		"CancelOpenOrders":       cancelAllErr,
		"CancelReplaceOrder":     replaceErr,
		"AmendOrderKeepPriority": amendErr,
		"NewOCOOrderList":        ocoErr,
		"NewOTOOrderList":        otoErr,
		"NewOTOCOOrderList":      otocoErr,
		"NewOPOOrderList":        opoErr,
		"NewOPOCOOrderList":      opocoErr,
		"CancelOrderList":        cancelListErr,
		"SOROrder":               sorErr,
		"TestSOROrder":           sorTestErr,
	} {
		if !errors.Is(err, trade.ErrAuthorization) {
			t.Errorf("%s error = %v, want ErrAuthorization", name, err)
		}
	}
	providerCalls, _, _ := harness.provider.snapshot()
	_, senderCalls := harness.sender.snapshot()
	if providerCalls != 0 || senderCalls != 0 || harness.requestCount() != 0 {
		t.Fatalf("provider calls = %d, sender calls = %d, requests = %d, want all zero", providerCalls, senderCalls, harness.requestCount())
	}
}

// TestReadMethodsWorkWithReadOnlyCredential은 읽기 전용 자격증명으로도 서명 조회 메서드가 동작하는지 확인한다.
func TestReadMethodsWorkWithReadOnlyCredential(t *testing.T) {
	t.Parallel()

	harness := newPermissionHarness(t, http.StatusOK, `[]`, credential.PermissionRead)
	if _, err := harness.client.OpenOrderLists(context.Background()); err != nil {
		t.Fatalf("OpenOrderLists() error = %v", err)
	}
	harness.expect(t, http.MethodGet, "/api/v3/openOrderList", true, 6, 0)
}
