package bithumb

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
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

var (
	extReadOnly = []credential.Permission{credential.PermissionRead}
	extTrader   = []credential.Permission{credential.PermissionRead, credential.PermissionTrade}
	extTransfer = []credential.Permission{credential.PermissionRead, credential.PermissionTransfer}
	extWithdraw = []credential.Permission{credential.PermissionRead, credential.PermissionWithdraw}
)

// extRecord는 테스트 서버가 받은 요청 한 건이다.
type extRecord struct {
	method        string
	path          string
	rawQuery      string
	body          string
	authorization string
}

// extReply는 테스트 서버가 돌려줄 응답이다.
type extReply struct {
	status int
	body   string
}

// extRoute는 "METHOD /path" 요청에 순서대로 돌려줄 응답 목록이다.
type extRoute struct {
	key     string
	replies []extReply
}

func extStatus(status int, body string) extReply {
	return extReply{status: status, body: body}
}

func extReplies(key string, replies ...extReply) extRoute {
	return extRoute{key: key, replies: replies}
}

func extOK(key, body string) extRoute {
	return extReplies(key, extStatus(http.StatusOK, body))
}

// extServer는 "METHOD /path" 별로 준비한 응답을 순서대로 돌려주고 받은 요청을 기록한다.
// 같은 경로를 준비한 응답보다 많이 호출하면 마지막 응답을 반복한다.
type extServer struct {
	server  *httptest.Server
	mu      sync.Mutex
	replies map[string][]extReply
	calls   map[string]int
	records []extRecord
}

func newExtServer(t *testing.T, routes []extRoute) *extServer {
	t.Helper()
	replies := make(map[string][]extReply, len(routes))
	for _, route := range routes {
		replies[route.key] = route.replies
	}
	extension := &extServer{replies: replies, calls: map[string]int{}}
	extension.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		key := request.Method + " " + request.URL.Path
		extension.mu.Lock()
		extension.records = append(extension.records, extRecord{
			method: request.Method, path: request.URL.Path, rawQuery: request.URL.RawQuery,
			body: string(body), authorization: request.Header.Get("Authorization"),
		})
		queue := extension.replies[key]
		index := extension.calls[key]
		extension.calls[key]++
		extension.mu.Unlock()
		if len(queue) == 0 {
			http.NotFound(writer, request)
			return
		}
		reply := queue[min(index, len(queue)-1)]
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(reply.status)
		_, _ = io.WriteString(writer, reply.body)
	}))
	t.Cleanup(extension.server.Close)
	return extension
}

func (extension *extServer) requests() []extRecord {
	extension.mu.Lock()
	defer extension.mu.Unlock()
	return slices.Clone(extension.records)
}

// extFixture는 테스트 서버, 서버를 바라보는 클라이언트, 자격증명 provider와 limiter를 묶는다.
type extFixture struct {
	client   *Client
	server   *extServer
	provider *recordingProvider
	limiter  *ratelimit.Limiter
}

func newExtFixture(
	t *testing.T,
	routes []extRoute,
	permissions []credential.Permission,
	mutate ...func(*Config),
) *extFixture {
	t.Helper()
	server := newExtServer(t, routes)
	client, provider, limiter := newExtClient(t, server.server.URL, &directSender{}, permissions, mutate...)
	return &extFixture{client: client, server: server, provider: provider, limiter: limiter}
}

func newExtClient(
	t *testing.T,
	baseURL string,
	sender commonexchange.Sender,
	permissions []credential.Permission,
	mutate ...func(*Config),
) (*Client, *recordingProvider, *ratelimit.Limiter) {
	t.Helper()
	limiter, err := ratelimit.New()
	if err != nil {
		t.Fatalf("ratelimit.New() error = %v", err)
	}
	executor, err := commonexchange.NewExecutor(commonexchange.ExecutorConfig{Sender: sender, Limiter: limiter})
	if err != nil {
		t.Fatalf("exchange.NewExecutor() error = %v", err)
	}
	provider := &recordingProvider{}
	config := Config{
		Executor: executor,
		Credentials: &credential.Descriptor{
			AccountID: "bithumb-ext", Exchange: model.ExchangeBithumb, SecretRef: "secret/bithumb",
			Permissions:           permissions,
			AllowedEgressRouteIDs: []transport.EgressRouteID{"route-a"},
		},
		CredentialProvider: provider, DefaultEgressRouteID: "route-a",
		BaseURL: baseURL, AllowInsecureHTTP: true,
		NonceSource: func() (string, error) { return "nonce-fixed", nil },
		Now:         func() time.Time { return time.UnixMilli(1700000000123) },
	}
	for _, change := range mutate {
		change(&config)
	}
	client, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, provider, limiter
}

// extRequests는 서버가 정확히 want건의 요청을 받았는지 확인하고 기록을 반환한다.
func extRequests(t *testing.T, server *extServer, want int) []extRecord {
	t.Helper()
	records := server.requests()
	if len(records) != want {
		t.Fatalf("request count = %d, want %d: %+v", len(records), want, records)
	}
	return records
}

// assertExtPublicRequest는 인증 헤더 없는 공개 GET 요청의 경로와 쿼리를 확인한다.
func assertExtPublicRequest(t *testing.T, record extRecord, path, rawQuery string) {
	t.Helper()
	if record.method != http.MethodGet || record.path != path || record.rawQuery != rawQuery || record.body != "" {
		t.Errorf(
			"request = %s %s?%s body %q, want GET %s?%s with empty body",
			record.method, record.path, record.rawQuery, record.body, path, rawQuery,
		)
	}
	if record.authorization != "" {
		t.Errorf("public request Authorization = %q, want empty", record.authorization)
	}
}

// assertExtRequest는 인증 요청의 메서드, 경로, 전송 쿼리, 본문과 JWT 해시 입력을 확인한다.
// hashInput은 구현이 계산하지 않은 명세상 `key=value` 연결 문자열이며 비어 있으면 query_hash가 없어야 한다.
func assertExtRequest(t *testing.T, record extRecord, method, path, rawQuery, body, hashInput string) {
	t.Helper()
	if record.method != method || record.path != path || record.rawQuery != rawQuery || record.body != body {
		t.Errorf(
			"request = %s %s?%s body %q, want %s %s?%s body %q",
			record.method, record.path, record.rawQuery, record.body, method, path, rawQuery, body,
		)
	}
	verifyExtJWT(t, record.authorization, hashInput)
}

func verifyExtJWT(t *testing.T, authorization, hashInput string) {
	t.Helper()
	token, found := strings.CutPrefix(authorization, "Bearer ")
	if !found {
		t.Errorf("Authorization = %q, want Bearer token", authorization)
		return
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Errorf("JWT has %d parts, want 3", len(parts))
		return
	}
	mac := hmac.New(sha256.New, []byte("secret-key"))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if parts[2] != base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) {
		t.Error("JWT signature is invalid")
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Errorf("decode JWT payload: %v", err)
		return
	}
	var payload struct {
		AccessKey    string `json:"access_key"`
		Nonce        string `json:"nonce"`
		Timestamp    int64  `json:"timestamp"`
		QueryHash    string `json:"query_hash"`
		QueryHashAlg string `json:"query_hash_alg"`
	}
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Errorf("decode JWT payload JSON: %v", err)
		return
	}
	if payload.AccessKey != "access-key" || payload.Nonce != "nonce-fixed" || payload.Timestamp != 1700000000123 {
		t.Errorf("JWT payload = %+v", payload)
	}
	if hashInput == "" {
		if payload.QueryHash != "" || payload.QueryHashAlg != "" {
			t.Errorf("unexpected query hash payload = %+v", payload)
		}
		return
	}
	if payload.QueryHash != QueryHash(hashInput) || payload.QueryHashAlg != "SHA512" {
		t.Errorf("JWT query hash payload = %+v, want SHA512 of %q", payload, hashInput)
	}
}

// assertExtLimiter는 limiter bucket의 기본 한도와 사용량을 확인한다.
func assertExtLimiter(t *testing.T, limiter *ratelimit.Limiter, key string, limit, used int) {
	t.Helper()
	snapshot, err := limiter.Snapshot(key)
	if err != nil {
		t.Errorf("limiter.Snapshot(%q) error = %v", key, err)
		return
	}
	if snapshot.Rule.Limit != limit || snapshot.Used != used {
		t.Errorf("limiter %q = limit %d used %d, want limit %d used %d", key, snapshot.Rule.Limit, snapshot.Used, limit, used)
	}
}
