package coinone

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
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

const (
	extTestNonce  = "123e4567-e89b-42d3-a456-426614174000"
	extPublicKey  = "coinone:route:route-a:public:1minute"
	extPrivateKey = "coinone:account:coinone-portfolio:private:1second"
	extOrderKey   = "coinone:account:coinone-portfolio:order:1second"
)

// extTestNow는 확장 API 테스트 클라이언트가 사용하는 현재 시각이다.
var extTestNow = time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)

// extRequest는 확장 API 테스트 서버가 받은 요청 한 건이다.
type extRequest struct {
	Method  string
	Path    string
	Query   string
	Body    string
	Payload string
}

// extReply는 확장 API 테스트 서버가 요청 순서대로 돌려주는 응답이다.
type extReply struct {
	status int
	body   string
}

// extServer는 받은 요청을 기록하고 준비한 응답을 순서대로 돌려주는 테스트 서버다.
// 준비한 응답보다 많은 요청이 오면 테스트를 실패시킨다.
type extServer struct {
	*httptest.Server
	mu       sync.Mutex
	replies  []extReply
	requests []extRequest
}

func newExtServer(t *testing.T, replies ...extReply) *extServer {
	t.Helper()
	server := &extServer{replies: replies}
	server.Server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/v2.1/") {
			verifySignedRequest(t, request, []byte("secret-key"), extTestNonce)
		}
		body := readRequestBody(t, request)
		server.mu.Lock()
		index := len(server.requests)
		server.requests = append(server.requests, extRequest{
			Method: request.Method, Path: request.URL.Path, Query: request.URL.RawQuery,
			Body: string(body), Payload: request.Header.Get("X-COINONE-PAYLOAD"),
		})
		server.mu.Unlock()
		reply := errorReply(http.StatusInternalServerError, "500", "unscripted request")
		if index < len(server.replies) {
			reply = server.replies[index]
		} else {
			t.Errorf("unexpected request %d: %s %s", index+1, request.Method, request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(reply.status)
		_, _ = io.WriteString(writer, reply.body)
	}))
	t.Cleanup(server.Close)
	return server
}

func (server *extServer) received() []extRequest {
	server.mu.Lock()
	defer server.mu.Unlock()
	return slices.Clone(server.requests)
}

// single은 정확히 한 건의 요청이 도착했는지 확인하고 그 요청을 반환한다.
func (server *extServer) single(t *testing.T) extRequest {
	t.Helper()
	requests := server.received()
	if len(requests) != 1 {
		t.Fatalf("requests = %+v, want exactly one", requests)
	}
	return requests[0]
}

// successReply는 result와 error_code 뒤에 fields를 붙인 성공 응답이다.
func successReply(fields string) extReply {
	body := `{"result":"success","error_code":"0"`
	if fields != "" {
		body += "," + fields
	}
	return extReply{status: http.StatusOK, body: body + "}"}
}

func errorReply(status int, code, message string) extReply {
	return extReply{status: status, body: `{"result":"error","error_code":"` + code + `","error_msg":"` + message + `"}`}
}

// extBody는 서명된 요청 본문에서 access_token과 nonce 뒤에 fields를 붙인 JSON이다.
// fields는 비어 있거나 쉼표로 시작해야 한다.
func extBody(fields string) string {
	return `{"access_token":"access-key","nonce":"` + extTestNonce + `"` + fields + `}`
}

func newExtTestClient(
	t *testing.T,
	baseURL string,
	sender commonexchange.Sender,
	provider *recordingProvider,
	permissions ...credential.Permission,
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
			AccountID: "coinone-portfolio", Exchange: model.ExchangeCoinone, SecretRef: "secret/coinone",
			Permissions:           permissions,
			AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"},
		},
		CredentialProvider: provider, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true,
		NonceSource: func() (string, error) { return extTestNonce, nil },
		Now:         func() time.Time { return extTestNow },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, limiter
}

func assertPublicRequest(t *testing.T, got extRequest, path, query string) {
	t.Helper()
	if got.Method != http.MethodGet || got.Path != path || got.Query != query || got.Body != "" || got.Payload != "" {
		t.Fatalf("public request = %+v, want unsigned GET %s?%s", got, path, query)
	}
}

func assertPrivateRequest(t *testing.T, got extRequest, path, fields string) {
	t.Helper()
	if got.Method != http.MethodPost || got.Path != path || got.Query != "" || got.Body != extBody(fields) || got.Payload == "" {
		t.Fatalf("private request = %+v, want signed POST %s with body %s", got, path, extBody(fields))
	}
}

func assertLimiterUsed(t *testing.T, limiter *ratelimit.Limiter, key string, want int) {
	t.Helper()
	snapshot, err := limiter.Snapshot(key)
	if err != nil || snapshot.Used != want {
		t.Fatalf("limiter %q snapshot = %+v, error = %v, want used %d", key, snapshot, err, want)
	}
}

// assertLimiterCharged는 key의 bucket이 요청으로 한 번 이상 차감됐는지 확인한다.
// 1초 창을 쓰는 private·order bucket은 여러 요청 사이에 창이 지나면 사용량이 줄 수 있어
// 요청이 둘 이상인 테스트는 정확한 횟수 대신 이 함수를 사용한다.
func assertLimiterCharged(t *testing.T, limiter *ratelimit.Limiter, key string) {
	t.Helper()
	snapshot, err := limiter.Snapshot(key)
	if err != nil || snapshot.Used < 1 {
		t.Fatalf("limiter %q snapshot = %+v, error = %v, want charged", key, snapshot, err)
	}
}

func assertLimiterUnknown(t *testing.T, limiter *ratelimit.Limiter, key string) {
	t.Helper()
	if _, err := limiter.Snapshot(key); !errors.Is(err, ratelimit.ErrUnknownRule) {
		t.Fatalf("limiter %q snapshot error = %v, want ErrUnknownRule", key, err)
	}
}

func assertExchangeError(t *testing.T, err error, category trade.ErrorCategory, code string) {
	t.Helper()
	var apiError *trade.APIError
	if !errors.As(err, &apiError) || apiError.Category != category || apiError.ExchangeCode != code {
		t.Fatalf("error = %#v, want category %s with exchange code %q", err, category, code)
	}
}
