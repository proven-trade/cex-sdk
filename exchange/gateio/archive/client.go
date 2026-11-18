// Package archive는 Gate.io 공식 과거 데이터 저장소(download.gatedata.org)의 월별 덤프를 제공한다.
// REST 캔들 API가 최근 10,000개만 허용하므로 그 이전 캔들과 체결은 이 패키지로 받는다.
package archive

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/transport"
)

const (
	DefaultBaseURL       = "https://download.gatedata.org"
	DefaultHeaderTimeout = 30 * time.Second
)

// ErrFileNotFound는 요청한 월·상품의 덤프 파일이 저장소에 없음을 나타낸다.
// 상장 이전 월, 아직 게시되지 않은 당월, 저장소가 생성하지 않은 구간에서 발생한다.
var ErrFileNotFound = errors.New("Gate.io archive file not found")

// Config는 Gate.io 과거 데이터 클라이언트 설정이다.
type Config struct {
	Executor             *commonexchange.Executor
	DefaultEgressRouteID transport.EgressRouteID
	BaseURL              string
	AllowInsecureHTTP    bool
	// HeaderTimeout은 응답 헤더 수신까지의 기본 제한 시간이다. 본문 수신 시간은 context로 제어한다.
	HeaderTimeout time.Duration
}

// Client는 요청별 송신 경로를 유지하며 월별 덤프를 압축 해제 스트림으로 읽는다.
type Client struct {
	executor             *commonexchange.Executor
	defaultEgressRouteID transport.EgressRouteID
	baseURL              *url.URL
	headerTimeout        time.Duration
}

// New는 검증된 Gate.io 과거 데이터 클라이언트를 생성한다.
func New(config Config) (*Client, error) {
	if config.Executor == nil {
		return nil, fmt.Errorf("Gate.io archive executor is required")
	}
	defaultRouteID := transport.EgressRouteID(strings.TrimSpace(string(config.DefaultEgressRouteID)))
	if defaultRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil || parsedBaseURL.Host == "" || parsedBaseURL.User != nil ||
		parsedBaseURL.RawQuery != "" || parsedBaseURL.Fragment != "" ||
		(parsedBaseURL.Path != "" && parsedBaseURL.Path != "/") {
		return nil, fmt.Errorf("invalid Gate.io archive base URL %q", baseURL)
	}
	if parsedBaseURL.Scheme != "https" && !(config.AllowInsecureHTTP && parsedBaseURL.Scheme == "http") {
		return nil, fmt.Errorf("Gate.io archive base URL must use HTTPS")
	}
	parsedBaseURL.Path = ""
	if config.HeaderTimeout == 0 {
		config.HeaderTimeout = DefaultHeaderTimeout
	}
	if config.HeaderTimeout < 0 {
		return nil, fmt.Errorf("Gate.io archive header timeout cannot be negative")
	}
	return &Client{
		executor: config.Executor, defaultEgressRouteID: defaultRouteID,
		baseURL: parsedBaseURL, headerTimeout: config.HeaderTimeout,
	}, nil
}

// open은 덤프 파일 하나를 요청해 gzip을 푼 CSV reader를 반환한다.
// trade.WithTimeout은 응답 헤더 수신까지만 적용한다.
func (client *Client) open(
	ctx context.Context,
	path string,
	fields int,
	options ...trade.RequestOption,
) (*csvStream, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultEgressRouteID, options...)
	if err != nil {
		return nil, err
	}
	if resolved.Timeout == 0 {
		resolved.Timeout = client.headerTimeout
	}
	requestURL := *client.baseURL
	requestURL.Path = "/" + path
	response, err := client.executor.Stream(ctx, commonexchange.Execution{
		Exchange: model.ExchangeGateIO, EndpointID: "GET archive",
		EgressRouteID: resolved.EgressRouteID, Timeout: resolved.Timeout,
		Operation: commonexchange.OperationRead,
		Build: func(context.Context) (*http.Request, error) {
			request, requestErr := http.NewRequest(http.MethodGet, requestURL.String(), nil)
			if requestErr != nil {
				return nil, fmt.Errorf("create Gate.io archive request: %w", requestErr)
			}
			request.Header.Set("User-Agent", "cex-sdk-go/0")
			return request, nil
		},
	})
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, statusError(response.StatusCode, path)
	}
	decompressed, err := gzip.NewReader(response.Body)
	if err != nil {
		_ = response.Body.Close()
		return nil, &trade.APIError{
			Category: trade.ErrorExchangeUnavailable, Exchange: model.ExchangeGateIO,
			HTTPStatus: response.StatusCode, Retryable: true,
			Cause: fmt.Errorf("open Gate.io archive %s: %w", path, err),
		}
	}
	reader := csv.NewReader(decompressed)
	reader.FieldsPerRecord = fields
	reader.TrimLeadingSpace = true
	reader.ReuseRecord = true
	return &csvStream{path: path, body: response.Body, decompressed: decompressed, reader: reader}, nil
}

func statusError(status int, path string) error {
	apiError := &trade.APIError{
		Category: trade.ErrorExchangeUnavailable, Exchange: model.ExchangeGateIO, HTTPStatus: status,
		Cause: fmt.Errorf("Gate.io archive %s returned HTTP %d", path, status),
	}
	switch {
	case status == http.StatusNotFound:
		apiError.Category = trade.ErrorUnsupportedCapability
		apiError.Cause = fmt.Errorf("%w: %s", ErrFileNotFound, path)
	case status == http.StatusTooManyRequests:
		apiError.Category, apiError.Retryable = trade.ErrorRateLimited, true
	case status >= http.StatusInternalServerError:
		apiError.Retryable = true
	}
	return apiError
}
