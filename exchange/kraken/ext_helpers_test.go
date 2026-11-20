package kraken

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

var extTestSecret = []byte(base64.StdEncoding.EncodeToString([]byte("test-secret")))

// extRequest는 확장 endpoint 테스트 서버가 받은 요청이다.
type extRequest struct {
	Method      string
	Path        string
	Query       url.Values
	ContentType string
	Body        string
}

// form은 form 본문을 parse한다.
func (request extRequest) form(t *testing.T) url.Values {
	t.Helper()
	values, err := url.ParseQuery(request.Body)
	if err != nil {
		t.Fatalf("parse form body %q: %v", request.Body, err)
	}
	return values
}

// jsonBody는 JSON 본문을 parse한다. 숫자는 json.Number로 보존한다.
func (request extRequest) jsonBody(t *testing.T) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(request.Body))
	decoder.UseNumber()
	var body map[string]any
	if err := decoder.Decode(&body); err != nil {
		t.Fatalf("parse JSON body %q: %v", request.Body, err)
	}
	return body
}

// extServer는 private 요청의 API-Key와 API-Sign을 검증하고 받은 요청을 기록하는 테스트 서버다.
type extServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []extRequest
}

func (server *extServer) last(t *testing.T) extRequest {
	t.Helper()
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.requests) == 0 {
		t.Fatal("test server received no request")
	}
	return server.requests[len(server.requests)-1]
}

func (server *extServer) count() int {
	server.mu.Lock()
	defer server.mu.Unlock()
	return len(server.requests)
}

// newExtServer는 path별 고정 응답을 돌려주는 테스트 서버를 만든다.
func newExtServer(t *testing.T, responses map[string]string) *extServer {
	t.Helper()
	server := &extServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		writer.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(request.URL.Path, privatePrefix) {
			nonce := extNonce(request.Header.Get("Content-Type"), body)
			expected, signErr := SignREST(request.URL.Path, nonce, string(body), extTestSecret)
			if signErr != nil || request.Header.Get("API-Key") != "test-api-key" ||
				request.Header.Get("API-Sign") != expected {
				http.Error(writer, `{"error":["EAPI:Invalid signature"]}`, http.StatusUnauthorized)
				return
			}
		}
		server.mu.Lock()
		server.requests = append(server.requests, extRequest{
			Method: request.Method, Path: request.URL.Path, Query: request.URL.Query(),
			ContentType: request.Header.Get("Content-Type"), Body: string(body),
		})
		server.mu.Unlock()
		response, found := responses[request.URL.Path]
		if !found {
			http.NotFound(writer, request)
			return
		}
		_, _ = io.WriteString(writer, response)
	}))
	t.Cleanup(server.Close)
	return server
}

func extNonce(contentType string, body []byte) string {
	if strings.HasPrefix(contentType, jsonContentType) {
		var payload struct {
			Nonce json.Number `json:"nonce"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return ""
		}
		return payload.Nonce.String()
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return ""
	}
	return values.Get("nonce")
}

// newExtClient는 지정한 권한만 가진 Kraken 클라이언트를 만든다. 권한을 생략하면 네 권한을 모두 준다.
func newExtClient(t *testing.T, baseURL string, permissions ...credential.Permission) (*Client, *ratelimit.Limiter) {
	t.Helper()
	if len(permissions) == 0 {
		permissions = []credential.Permission{
			credential.PermissionRead, credential.PermissionTrade,
			credential.PermissionTransfer, credential.PermissionWithdraw,
		}
	}
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
	fixedNow := time.UnixMilli(1_700_000_000_000)
	client, err := New(Config{
		Executor: executor,
		Credentials: &credential.Descriptor{
			AccountID: "kraken-main", Exchange: model.ExchangeKraken,
			SecretRef:             "secret/kraken/main",
			Permissions:           permissions,
			AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"},
		},
		CredentialProvider: &recordingProvider{secret: extTestSecret}, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true,
		PublicRequestsPerSecond: 100, PrivateCounterLimit: 100,
		PrivateCounterWindow: time.Second, TradingRequestsPerSecond: 100,
		Now: func() time.Time { return fixedNow },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, limiter
}
