package coinbase

import (
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/ratelimit"
	"github.com/proven-trade/cex-sdk/transport"
)

type capturedRequest struct {
	method        string
	path          string
	query         url.Values
	body          string
	authorization string
}

// extHarness는 JWT를 검증하고 요청을 기록한 뒤 고정 응답을 돌려주는 서버와 Client 묶음이다.
type extHarness struct {
	client   *Client
	mu       sync.Mutex
	requests []capturedRequest
}

func permissionsOf(permissions ...credential.Permission) []credential.Permission {
	return permissions
}

func newExtHarness(
	t *testing.T,
	permissions []credential.Permission,
	status int,
	responseBody string,
) *extHarness {
	t.Helper()
	keyName := "organizations/test/apiKeys/main"
	privateKey, secret := newTestECKey(t, elliptic.P256())
	fixedNow := time.Unix(1_700_000_000, 0)
	harness := &extHarness{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		harness.record(capturedRequest{
			method: request.Method, path: request.URL.Path, query: request.URL.Query(),
			body: string(body), authorization: request.Header.Get("Authorization"),
		})
		if !verifyRequestJWT(t, request, &privateKey.PublicKey, keyName, fixedNow) {
			http.Error(writer, `{"error":"UNAUTHENTICATED","message":"invalid token"}`, http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_, _ = io.WriteString(writer, responseBody)
	}))
	t.Cleanup(server.Close)
	harness.client = newPermissionTestClient(t, server.URL, permissions, fixedNow, keyName, secret)
	return harness
}

func newPermissionTestClient(
	t *testing.T,
	baseURL string,
	permissions []credential.Permission,
	now time.Time,
	keyName string,
	secret []byte,
) *Client {
	t.Helper()
	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{Sender: &directSender{}, Limiter: limiter})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	descriptor := &credential.Descriptor{
		AccountID: "coinbase-main", Exchange: model.ExchangeCoinbase,
		SecretRef:             "secret/coinbase/main",
		Permissions:           permissions,
		AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"},
	}
	provider := &recordingProvider{keyName: keyName, secret: secret}
	client, err := New(Config{
		Executor: executor, Credentials: descriptor, CredentialProvider: provider,
		DefaultEgressRouteID: "route-a", BaseURL: baseURL, AllowInsecureHTTP: true,
		PublicRequestsPerSecond: 100, PrivateRequestsPerSecond: 100,
		Now: func() time.Time { return now }, Random: rand.Reader,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client
}

func (harness *extHarness) record(request capturedRequest) {
	harness.mu.Lock()
	defer harness.mu.Unlock()
	harness.requests = append(harness.requests, request)
}

func (harness *extHarness) requestCount() int {
	harness.mu.Lock()
	defer harness.mu.Unlock()
	return len(harness.requests)
}

// assertSingle은 정확히 한 건의 서명된 요청이 기대한 method, path, query, body로 전송됐는지 확인한다.
// body가 비어 있으면 본문이 없어야 하고, 아니면 JSON 의미가 같아야 한다.
func (harness *extHarness) assertSingle(
	t *testing.T,
	method, path string,
	query url.Values,
	body string,
) {
	t.Helper()
	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.requests) != 1 {
		t.Fatalf("request count = %d, want 1", len(harness.requests))
	}
	got := harness.requests[0]
	if got.method != method || got.path != path {
		t.Fatalf("request = %s %s, want %s %s", got.method, got.path, method, path)
	}
	if got.query.Encode() != query.Encode() {
		t.Fatalf("query = %q, want %q", got.query.Encode(), query.Encode())
	}
	if !strings.HasPrefix(got.authorization, "Bearer ") {
		t.Fatalf("authorization = %q, want bearer token", got.authorization)
	}
	if body == "" {
		if got.body != "" {
			t.Fatalf("body = %q, want empty", got.body)
		}
		return
	}
	assertJSONEqual(t, got.body, body)
}

func assertJSONEqual(t *testing.T, got, want string) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal([]byte(got), &gotValue); err != nil {
		t.Fatalf("decode request body %q: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("decode expected body %q: %v", want, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("body = %s, want %s", got, want)
	}
}
