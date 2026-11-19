package korbit

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

// loggedRequest는 테스트 서버가 받은 요청의 메서드·경로와 서명을 제외한 파라미터다.
type loggedRequest struct {
	Method string
	Path   string
	Params string
	Signed bool
}

type requestLog struct {
	mu      sync.Mutex
	entries []loggedRequest
}

func (log *requestLog) add(entry loggedRequest) {
	log.mu.Lock()
	defer log.mu.Unlock()
	log.entries = append(log.entries, entry)
}

func (log *requestLog) snapshot() []loggedRequest {
	log.mu.Lock()
	defer log.mu.Unlock()
	return slices.Clone(log.entries)
}

// newKorbitStubServer는 "METHOD /path"별 고정 응답을 돌려주는 테스트 서버를 만든다.
// X-KAPI-KEY가 있는 요청은 HMAC 서명을 검증한 뒤 서명 전 파라미터를 기록하고, 없는 공개 요청은 query를 그대로 기록한다.
func newKorbitStubServer(t *testing.T, bodies map[string]string) (*httptest.Server, *requestLog) {
	t.Helper()
	recorder := &requestLog{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		entry := loggedRequest{Method: request.Method, Path: request.URL.Path, Params: request.URL.RawQuery}
		if request.Header.Get("X-KAPI-KEY") != "" {
			verifyKorbitSignedRequest(t, request, []byte("secret-key"))
			entry.Params = unsignedKorbitParameters(t, request)
			entry.Signed = true
		}
		recorder.add(entry)
		body, found := bodies[request.Method+" "+request.URL.Path]
		if !found {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, body)
	}))
	t.Cleanup(server.Close)
	return server, recorder
}

// newFixedResponseServer는 요청 내용과 관계없이 같은 상태 코드와 본문을 돌려주고 요청 횟수를 센다.
func newFixedResponseServer(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_, _ = io.WriteString(writer, body)
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

func newExtTestExecutor(t *testing.T, sender commonexchange.Sender) (*commonexchange.Executor, *ratelimit.Limiter) {
	t.Helper()
	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{Sender: sender, Limiter: limiter})
	if err != nil {
		t.Fatalf("exchange.NewExecutor() error = %v", err)
	}
	return executor, limiter
}

// newExtTestClient는 지정한 SDK 권한만 가진 자격증명으로 REST 클라이언트를 만든다. tweak가 있으면 Config를 조정한다.
func newExtTestClient(
	t *testing.T,
	baseURL string,
	sender commonexchange.Sender,
	provider *recordingProvider,
	permissions []credential.Permission,
	tweak func(*Config),
) (*Client, *ratelimit.Limiter) {
	t.Helper()
	executor, limiter := newExtTestExecutor(t, sender)
	config := Config{
		Executor: executor,
		Credentials: &credential.Descriptor{
			AccountID: "korbit-account", Exchange: model.ExchangeKorbit, SecretRef: "secret/korbit",
			Permissions:           permissions,
			AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"},
		},
		CredentialProvider: provider, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true,
		Now: func() time.Time { return time.UnixMilli(1700000000123) },
	}
	if tweak != nil {
		tweak(&config)
	}
	client, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, limiter
}

// newAssetTestClient는 입출금 제한(초당 5회)에 테스트가 막히지 않도록 asset 제한을 높인 직접 송신 클라이언트를 만든다.
func newAssetTestClient(t *testing.T, baseURL string, permissions ...credential.Permission) *Client {
	t.Helper()
	client, _ := newExtTestClient(
		t, baseURL, &directSender{}, &recordingProvider{}, permissions,
		func(config *Config) { config.AssetRequestsPerSecond = 1000 },
	)
	return client
}

// newPublicTestClient는 자격증명 없이 공개 endpoint만 호출하는 클라이언트를 만든다.
func newPublicTestClient(t *testing.T, baseURL string) (*Client, *ratelimit.Limiter) {
	t.Helper()
	executor, limiter := newExtTestExecutor(t, &directSender{})
	client, err := New(Config{
		Executor: executor, DefaultEgressRouteID: "route-a", BaseURL: baseURL, AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, limiter
}

var allTestPermissions = []credential.Permission{
	credential.PermissionRead, credential.PermissionTrade,
	credential.PermissionTransfer, credential.PermissionWithdraw,
}

// permissionsExcept는 지정한 권한만 뺀 나머지 SDK 권한을 반환한다.
func permissionsExcept(excluded credential.Permission) []credential.Permission {
	return slices.DeleteFunc(slices.Clone(allTestPermissions), func(permission credential.Permission) bool {
		return permission == excluded
	})
}

type namedCall struct {
	name string
	call func() error
}

// requireRejectedBeforeSend는 모든 호출이 서버로 요청을 보내기 전에 검증 오류로 거부되는지 확인한다.
func requireRejectedBeforeSend(t *testing.T, recorder *requestLog, calls []namedCall) {
	t.Helper()
	for _, test := range calls {
		if err := test.call(); !errors.Is(err, trade.ErrValidation) {
			t.Errorf("%s error = %v, want ErrValidation", test.name, err)
		}
	}
	if entries := recorder.snapshot(); len(entries) != 0 {
		t.Errorf("invalid input sent requests: %+v", entries)
	}
}

func TestAssetParameterValidators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		invalid bool
	}{
		{"main account omitted", validateMainAccountSeq(0), false},
		{"main account", validateMainAccountSeq(1), false},
		{"sub account for deposit API", validateMainAccountSeq(2), true},
		{"negative main account", validateMainAccountSeq(-1), true},
		{"required account lower bound", validateRequiredAccountSeq("accountSeq", 1), false},
		{"required account upper bound", validateRequiredAccountSeq("accountSeq", 9999), false},
		{"required account omitted", validateRequiredAccountSeq("accountSeq", 0), true},
		{"required account too large", validateRequiredAccountSeq("accountSeq", 10000), true},
		{"single character currency", validateCurrency("s"), false},
		{"upper case currency", validateCurrency("BTC"), true},
		{"empty currency", validateCurrency(""), true},
		{"empty optional currency", validateOptionalCurrency(""), false},
		{"invalid optional currency", validateOptionalCurrency("b t c"), true},
		{"network", validateNetwork("ETH"), false},
		{"network with space", validateNetwork("E T H"), true},
		{"empty network", validateNetwork(""), true},
		{"empty optional network", validateOptionalNetwork(""), false},
		{"invalid optional network", validateOptionalNetwork("ETH/L2"), true},
		{"optional limit default", validateOptionalLimit("test", 0, 100), false},
		{"optional limit maximum", validateOptionalLimit("test", 100, 100), false},
		{"optional limit over maximum", validateOptionalLimit("test", 101, 100), true},
		{"optional limit negative", validateOptionalLimit("test", -1, 100), true},
		{"required limit omitted", validateRequiredLimit("test", 0, 100), true},
		{"required limit maximum", validateRequiredLimit("test", 100, 100), false},
		{"required limit over maximum", validateRequiredLimit("test", 101, 100), true},
		{"positive ID", validatePositiveID("test", 1), false},
		{"zero ID", validatePositiveID("test", 0), true},
		{"negative ID", validatePositiveID("test", -1), true},
		{"request key uuid", validateRequestKey("b6f1a9a2-4c3e-4a7d-9d3c-2f8f5e1a7b10"), false},
		{"request key too long", validateRequestKey(strings.Repeat("a", 37)), true},
		{"request key with space", validateRequestKey("a b"), true},
		{"empty request key", validateRequestKey(""), true},
		{"withdrawal address", validateWithdrawalAddress("1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"), false},
		{"empty withdrawal address", validateWithdrawalAddress(""), true},
		{"withdrawal address with space", validateWithdrawalAddress("0x05a5 6e2d"), true},
		{"withdrawal address with newline", validateWithdrawalAddress("0x05a56e2d\n"), true},
		{"empty secondary address", validateSecondaryAddress(""), false},
		{"secondary address memo with space", validateSecondaryAddress("order memo 1"), false},
		{"secondary address with control character", validateSecondaryAddress("tag\n"), true},
		{"transfer quantity", validateTransferQty("0.5"), false},
		{"transfer quantity with eight decimals", validateTransferQty("0.12345678"), false},
		{"transfer quantity with nine decimals", validateTransferQty("0.123456789"), true},
		{"transfer quantity exponent", validateTransferQty("1e5"), true},
		{"transfer quantity zero", validateTransferQty("0"), true},
		{"transfer quantity negative", validateTransferQty("-1"), true},
		{"empty transfer quantity", validateTransferQty(""), true},
	}
	for _, test := range tests {
		if test.invalid {
			if !errors.Is(test.err, trade.ErrValidation) {
				t.Errorf("%s error = %v, want ErrValidation", test.name, test.err)
			}
			continue
		}
		if test.err != nil {
			t.Errorf("%s error = %v, want nil", test.name, test.err)
		}
	}
}

func TestAssetRequestsPerSecondConfig(t *testing.T) {
	t.Parallel()

	executor, _ := newExtTestExecutor(t, &directSender{})
	client, err := New(Config{Executor: executor, DefaultEgressRouteID: "route-a"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if DefaultAssetRequestsPerSecond != 5 || client.assetRequestsPerSecond != 5 {
		t.Fatalf("default asset limit = %d, client limit = %d", DefaultAssetRequestsPerSecond, client.assetRequestsPerSecond)
	}
	custom, err := New(Config{Executor: executor, DefaultEgressRouteID: "route-a", AssetRequestsPerSecond: 3})
	if err != nil {
		t.Fatalf("New() custom asset limit error = %v", err)
	}
	if custom.assetRequestsPerSecond != 3 {
		t.Fatalf("custom asset limit = %d, want 3", custom.assetRequestsPerSecond)
	}
	if _, err := New(Config{Executor: executor, DefaultEgressRouteID: "route-a", AssetRequestsPerSecond: -1}); err == nil {
		t.Fatal("New() accepted a negative asset limit")
	}
}

func TestDefaultEndpointsPointToDigitalX(t *testing.T) {
	t.Parallel()

	if DefaultBaseURL != "https://api.digitalx.miraeasset.com" ||
		DefaultPublicWebSocketURL != "wss://ws-api.digitalx.miraeasset.com/v2/public" ||
		DefaultPrivateWebSocketURL != "wss://ws-api.digitalx.miraeasset.com/v2/private" {
		t.Fatalf(
			"defaults = %q, %q, %q",
			DefaultBaseURL, DefaultPublicWebSocketURL, DefaultPrivateWebSocketURL,
		)
	}
	executor, _ := newExtTestExecutor(t, &directSender{})
	client, err := New(Config{Executor: executor, DefaultEgressRouteID: "route-a"})
	if err != nil {
		t.Fatalf("New() default error = %v", err)
	}
	if client.baseURL.String() != DefaultBaseURL {
		t.Fatalf("default base URL = %q, want %q", client.baseURL.String(), DefaultBaseURL)
	}
	legacy, err := New(Config{
		Executor: executor, DefaultEgressRouteID: "route-a", BaseURL: "https://api.korbit.co.kr",
	})
	if err != nil {
		t.Fatalf("New() legacy base URL error = %v", err)
	}
	if legacy.baseURL.String() != "https://api.korbit.co.kr" {
		t.Fatalf("legacy base URL = %q", legacy.baseURL.String())
	}
	streams, err := NewStreamClient(StreamClientConfig{
		Connector: &korbitWebSocketTestConnector{}, DefaultEgressRouteID: "route-a",
	})
	if err != nil {
		t.Fatalf("NewStreamClient() default error = %v", err)
	}
	if streams.publicURL != DefaultPublicWebSocketURL || streams.privateURL != DefaultPrivateWebSocketURL {
		t.Fatalf("default stream URLs = %q, %q", streams.publicURL, streams.privateURL)
	}
	legacyStreams, err := NewStreamClient(StreamClientConfig{
		Connector: &korbitWebSocketTestConnector{}, DefaultEgressRouteID: "route-a",
		PublicWebSocketURL:  "wss://ws-api.korbit.co.kr/v2/public",
		PrivateWebSocketURL: "wss://ws-api.korbit.co.kr/v2/private",
	})
	if err != nil {
		t.Fatalf("NewStreamClient() legacy error = %v", err)
	}
	if legacyStreams.publicURL != "wss://ws-api.korbit.co.kr/v2/public" ||
		legacyStreams.privateURL != "wss://ws-api.korbit.co.kr/v2/private" {
		t.Fatalf("legacy stream URLs = %q, %q", legacyStreams.publicURL, legacyStreams.privateURL)
	}
}
