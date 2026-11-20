package upbit

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/ratelimit"
)

// callCase는 서버에 닿기 전에 거부되어야 하는 클라이언트 호출 한 건이다.
type callCase struct {
	name string
	call func(context.Context, *Client) error
}

// newPublicTestClient는 자격증명 없이 공개 시세 API만 호출하는 클라이언트를 만든다.
func newPublicTestClient(t *testing.T, baseURL string, sender *directSender) (*Client, *ratelimit.Limiter) {
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
		Executor: executor, DefaultEgressRouteID: "route-a", BaseURL: baseURL, AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client, limiter
}

// newPrivateTestClient는 permissions만 가진 서명 클라이언트를 만든다. 입금 테스트와 같은 구성을 쓴다.
func newPrivateTestClient(
	t *testing.T,
	baseURL string,
	permissions ...credential.Permission,
) (*Client, *recordingProvider, *ratelimit.Limiter) {
	t.Helper()
	return newDepositTestClient(t, baseURL, permissions...)
}

// numberedValues는 prefix1, prefix2, ... 형식의 중복 없는 값 count개를 만든다.
func numberedValues(prefix string, count int) []string {
	values := make([]string, count)
	for index := range values {
		values[index] = prefix + strconv.Itoa(index+1)
	}
	return values
}

// numberedPairs는 KRW-C1, KRW-C2, ... 형식의 중복 없는 페어 count개를 만든다.
func numberedPairs(count int) []string {
	return numberedValues("KRW-C", count)
}

// verifySignedQuery는 GET·DELETE 요청 JWT의 query_hash가 디코드된 쿼리 문자열(wantHashInput)의 SHA-512인지 검증한다.
// wantHashInput이 비어 있으면 query_hash 없이 서명되어야 한다.
func verifySignedQuery(t *testing.T, request *http.Request, wantHashInput string) {
	t.Helper()
	payload := decodeSignedPayload(t, request, []byte("secret-key"))
	if payload == nil {
		return
	}
	if payload["access_key"] != "access-key" || payload["nonce"] != "nonce-fixed" {
		t.Errorf("JWT payload = %v, want access-key and nonce-fixed", payload)
	}
	if wantHashInput == "" {
		if payload["query_hash"] != "" || payload["query_hash_alg"] != "" {
			t.Errorf("JWT payload = %v, want no query hash", payload)
		}
		return
	}
	if payload["query_hash"] != QueryHash(wantHashInput) || payload["query_hash_alg"] != "SHA512" {
		t.Errorf("JWT payload = %v, want query hash of %q", payload, wantHashInput)
	}
}

// assertSecretsDestroyed는 Secret을 wantCalls번 조회했고 사용한 key, secret byte가 모두 지워졌는지 검증한다.
func assertSecretsDestroyed(t *testing.T, provider *recordingProvider, wantCalls int) {
	t.Helper()
	calls, key, secret := provider.snapshot()
	if calls != wantCalls || !allZero(key) || !allZero(secret) {
		t.Fatalf(
			"provider calls = %d (want %d), key zero = %v, secret zero = %v",
			calls, wantCalls, allZero(key), allZero(secret),
		)
	}
}

// assertRateRule은 limiter에 key 규칙이 limit, window로 등록됐는지 검증한다.
func assertRateRule(t *testing.T, limiter *ratelimit.Limiter, key string, limit int, window time.Duration) {
	t.Helper()
	snapshot, err := limiter.Snapshot(key)
	if err != nil || snapshot.Rule.Limit != limit || snapshot.Rule.Window != window {
		t.Fatalf("limiter snapshot %q = %+v, error = %v, want limit %d window %s", key, snapshot, err, limit, window)
	}
}

// requireAPIError는 err가 거래소 오류 응답에서 온 trade.APIError인지 확인하고 반환한다.
func requireAPIError(t *testing.T, err error, wantStatus int, wantCode string) *trade.APIError {
	t.Helper()
	var apiError *trade.APIError
	if !errors.As(err, &apiError) || apiError.HTTPStatus != wantStatus || apiError.ExchangeCode != wantCode {
		t.Fatalf("error = %v, want APIError with HTTP %d and code %q", err, wantStatus, wantCode)
	}
	return apiError
}

// assertRejectedBeforeSend는 permissions만 가진 클라이언트가 cases의 모든 호출을 wantErr로 거부하는지 검증한다.
// 거부는 Secret 조회와 네트워크 요청보다 먼저 일어나야 한다.
func assertRejectedBeforeSend(t *testing.T, wantErr error, cases []callCase, permissions ...credential.Permission) {
	t.Helper()

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{}`)
	}))
	defer server.Close()

	client, provider, _ := newDepositTestClient(t, server.URL, permissions...)
	ctx := context.Background()
	for _, test := range cases {
		if err := test.call(ctx, client); !errors.Is(err, wantErr) {
			t.Fatalf("%s: error = %v, want %v", test.name, err, wantErr)
		}
	}
	calls, _, _ := provider.snapshot()
	if hits.Load() != 0 || calls != 0 {
		t.Fatalf("server hits = %d, provider calls = %d, want no request or secret resolution", hits.Load(), calls)
	}
}

// assertValidationCases는 valid가 모두 통과하고 invalid가 모두 ErrValidation인지 검증한다.
func assertValidationCases(t *testing.T, valid, invalid []depositValidationCase) {
	t.Helper()
	for _, test := range valid {
		if test.err != nil {
			t.Fatalf("%s: validate() error = %v", test.name, test.err)
		}
	}
	for _, test := range invalid {
		if !errors.Is(test.err, trade.ErrValidation) {
			t.Fatalf("%s: validate() error = %v, want ErrValidation", test.name, test.err)
		}
	}
}

func TestCommonExtValidators(t *testing.T) {
	t.Parallel()

	valid := []depositValidationCase{
		{"currency", validateCurrencyCode("currency", "USDT")},
		{"currency with digit", validateCurrencyCode("currency", "1INCH")},
		{"request value", validateRequestValue("uuid", "9ca023a5-851b-4fec-9f0a-48cd83c2eaae")},
		{"optional request value empty", validateOptionalRequestValue("uuid", "")},
		{"value list", validateValueList("uuids", []string{"a", "b"}, 2)},
		{"value list without limit", validateValueList("uuids", []string{"a", "b", "c"}, 0)},
		{"optional value list empty", validateOptionalValueList("uuids", nil, 20)},
		{"transfer identifier empty", validateTransferIdentifier("")},
		{"transfer identifier charset", validateTransferIdentifier("unique_transfer.id-01")},
		{"transfer identifier max length", validateTransferIdentifier(strings.Repeat("a", 64))},
		{"page limit default", validatePageLimit("page", 0, 100)},
		{"page limit maximum", validatePageLimit("page", 100, 100)},
		{"currency list", validateCurrencyList("quote currency", []string{"KRW", "BTC"})},
	}
	invalid := []depositValidationCase{
		{"currency lowercase", validateCurrencyCode("currency", "krw")},
		{"currency empty", validateCurrencyCode("currency", "")},
		{"currency too long", validateCurrencyCode("currency", strings.Repeat("A", 21))},
		{"request value empty", validateRequestValue("uuid", "")},
		{"request value space", validateRequestValue("uuid", "a b")},
		{"request value tab", validateRequestValue("uuid", "a\tb")},
		{"request value newline", validateRequestValue("uuid", "ab\n")},
		{"request value control character", validateRequestValue("uuid", "a\x00b")},
		{"optional request value space", validateOptionalRequestValue("uuid", " a")},
		{"value list empty", validateValueList("uuids", nil, 2)},
		{"value list too many", validateValueList("uuids", []string{"a", "b", "c"}, 2)},
		{"value list duplicate", validateValueList("uuids", []string{"a", "a"}, 2)},
		{"value list blank item", validateValueList("uuids", []string{"a", ""}, 2)},
		{"optional value list invalid item", validateOptionalValueList("uuids", []string{"a b"}, 20)},
		{"transfer identifier space", validateTransferIdentifier("a b")},
		{"transfer identifier slash", validateTransferIdentifier("a/b")},
		{"transfer identifier too long", validateTransferIdentifier(strings.Repeat("a", 65))},
		{"page limit negative", validatePageLimit("page", -1, 100)},
		{"page limit above maximum", validatePageLimit("page", 101, 100)},
		{"currency list empty", validateCurrencyList("quote currency", nil)},
		{"currency list lowercase", validateCurrencyList("quote currency", []string{"krw"})},
		{"currency list duplicate", validateCurrencyList("quote currency", []string{"KRW", "KRW"})},
	}
	assertValidationCases(t, valid, invalid)
}

func TestParametersAddAllRepeatsKeyInOrder(t *testing.T) {
	t.Parallel()

	values := parameters{}
	values.add("market", "KRW-BTC")
	values.addAll("uuids[]", []string{"b", "a"})
	values.addAll("identifiers[]", nil)
	values.addBool("include_expired", true)
	values.addBool("skipped", false)
	want := "market=KRW-BTC&uuids[]=b&uuids[]=a&include_expired=true"
	if got := values.encoded(); got != want {
		t.Fatalf("encoded() = %q, want %q", got, want)
	}
	hashInput, err := values.hashString()
	if err != nil || hashInput != want {
		t.Fatalf("hashString() = %q, error = %v, want %q", hashInput, err, want)
	}
}

func TestParametersEncodeAndHashSpecialCharacters(t *testing.T) {
	t.Parallel()

	values := parameters{}
	values.add("pairs", "KRW-BTC,KRW-ETH")
	values.add("start_time", "2026-05-28T15:17:51+09:00")
	wantQuery := "pairs=KRW-BTC%2CKRW-ETH&start_time=2026-05-28T15%3A17%3A51%2B09%3A00"
	if got := values.encoded(); got != wantQuery {
		t.Fatalf("encoded() = %q, want %q", got, wantQuery)
	}
	// 쿼리 해시는 URL 인코딩 전 문자열로 계산한다.
	wantHash := "pairs=KRW-BTC,KRW-ETH&start_time=2026-05-28T15:17:51+09:00"
	hashInput, err := values.hashString()
	if err != nil || hashInput != wantHash {
		t.Fatalf("hashString() = %q, error = %v, want %q", hashInput, err, wantHash)
	}
}

func TestPrivateExtMethodsRequireCredentials(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{}`)
	}))
	defer server.Close()

	client, _ := newPublicTestClient(t, server.URL, &directSender{})
	ctx := context.Background()
	calls := []callCase{
		{"OrderChance", func(ctx context.Context, client *Client) error {
			_, err := client.OrderChance(ctx, OrderChanceRequest{Market: "KRW-BTC"})
			return err
		}},
		{"CancelOpenOrders", func(ctx context.Context, client *Client) error {
			_, err := client.CancelOpenOrders(ctx, CancelOpenOrdersRequest{AllMarkets: true})
			return err
		}},
		{"WalletStatuses", func(ctx context.Context, client *Client) error {
			_, err := client.WalletStatuses(ctx)
			return err
		}},
		{"Pockets", func(ctx context.Context, client *Client) error {
			_, err := client.Pockets(ctx)
			return err
		}},
		{"CreateUniversalTransfer", func(ctx context.Context, client *Client) error {
			_, err := client.CreateUniversalTransfer(ctx, UniversalTransferRequest{To: "pocket", Currency: "KRW", Amount: "1000"})
			return err
		}},
		{"WithdrawChance", func(ctx context.Context, client *Client) error {
			_, err := client.WithdrawChance(ctx, WithdrawChanceRequest{Currency: "KRW"})
			return err
		}},
		{"WithdrawKRW", func(ctx context.Context, client *Client) error {
			_, err := client.WithdrawKRW(ctx, KRWWithdrawRequest{Amount: "10000", TwoFactorType: TwoFactorKakao})
			return err
		}},
	}
	for _, test := range calls {
		if err := test.call(ctx, client); !errors.Is(err, trade.ErrAuthentication) {
			t.Fatalf("%s: error = %v, want ErrAuthentication", test.name, err)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("server hits = %d, want no request without credentials", hits.Load())
	}
}
