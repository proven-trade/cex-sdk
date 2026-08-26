// Package cryptocom은 Crypto.com Exchange v1 Spot REST·WebSocket·공통 어댑터를 제공한다.
package cryptocom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync/atomic"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/transport"
)

const (
	DefaultBaseURL                                = "https://api.crypto.com/exchange/v1"
	DefaultUATBaseURL                             = "https://uat-api.3ona.co/exchange/v1"
	DefaultRequestTimeout                         = 10 * time.Second
	DefaultPublicRequestsPerSecond                = 100
	DefaultOrderRequestsPer100Milliseconds        = 15
	DefaultOrderDetailRequestsPer100Milliseconds  = 30
	DefaultHistoryRequestsPerSecond               = 1
	DefaultOtherPrivateRequestsPer100Milliseconds = 3
)

// Config는 Crypto.com Exchange v1 Spot 공개 REST 클라이언트 설정이다.
type Config struct {
	Executor                               *commonexchange.Executor
	Credentials                            *credential.Descriptor
	CredentialProvider                     credential.Provider
	DefaultEgressRouteID                   transport.EgressRouteID
	BaseURL                                string
	AllowInsecureHTTP                      bool
	RequestTimeout                         time.Duration
	PublicRequestsPerSecond                int
	OrderRequestsPer100Milliseconds        int
	OrderDetailRequestsPer100Milliseconds  int
	HistoryRequestsPerSecond               int
	OtherPrivateRequestsPer100Milliseconds int
	Now                                    func() time.Time
}

// Client는 Crypto.com Exchange v1 Spot REST API를 요청별 송신 경로 선택과 함께 제공한다.
type Client struct {
	executor                               *commonexchange.Executor
	credentials                            *credential.Descriptor
	credentialProvider                     credential.Provider
	defaultEgressRouteID                   transport.EgressRouteID
	baseURL                                *url.URL
	requestTimeout                         time.Duration
	publicRequestsPerSecond                int
	orderRequestsPer100Milliseconds        int
	orderDetailRequestsPer100Milliseconds  int
	historyRequestsPerSecond               int
	otherPrivateRequestsPer100Milliseconds int
	now                                    func() time.Time
	requestID                              atomic.Int64
}

// New는 검증된 Crypto.com Exchange v1 Spot REST 클라이언트를 생성한다.
func New(config Config) (*Client, error) {
	if config.Executor == nil {
		return nil, fmt.Errorf("Crypto.com executor is required")
	}
	defaultRouteID := transport.EgressRouteID(strings.TrimSpace(string(config.DefaultEgressRouteID)))
	if defaultRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil || parsedBaseURL.Host == "" || parsedBaseURL.User != nil ||
		parsedBaseURL.RawQuery != "" || parsedBaseURL.Fragment != "" {
		return nil, fmt.Errorf("invalid Crypto.com base URL %q", baseURL)
	}
	if parsedBaseURL.Scheme != "https" && !(config.AllowInsecureHTTP && parsedBaseURL.Scheme == "http") {
		return nil, fmt.Errorf("Crypto.com base URL must use HTTPS")
	}
	cleanedPath := path.Clean(strings.TrimSpace(parsedBaseURL.Path))
	if cleanedPath == "." || cleanedPath == "/" {
		cleanedPath = ""
	}
	if cleanedPath != "" && cleanedPath != "/" && cleanedPath != "/exchange/v1" {
		return nil, fmt.Errorf("invalid Crypto.com base URL path %q", parsedBaseURL.Path)
	}
	parsedBaseURL.Path = strings.TrimRight(cleanedPath, "/")
	if config.RequestTimeout == 0 {
		config.RequestTimeout = DefaultRequestTimeout
	}
	if config.RequestTimeout < 0 {
		return nil, fmt.Errorf("Crypto.com request timeout cannot be negative")
	}
	if config.PublicRequestsPerSecond == 0 {
		config.PublicRequestsPerSecond = DefaultPublicRequestsPerSecond
	}
	if config.PublicRequestsPerSecond < 1 ||
		config.PublicRequestsPerSecond > DefaultPublicRequestsPerSecond {
		return nil, fmt.Errorf("Crypto.com public request quota must be between 1 and 100")
	}
	if config.OrderRequestsPer100Milliseconds == 0 {
		config.OrderRequestsPer100Milliseconds = DefaultOrderRequestsPer100Milliseconds
	}
	if config.OrderDetailRequestsPer100Milliseconds == 0 {
		config.OrderDetailRequestsPer100Milliseconds = DefaultOrderDetailRequestsPer100Milliseconds
	}
	if config.HistoryRequestsPerSecond == 0 {
		config.HistoryRequestsPerSecond = DefaultHistoryRequestsPerSecond
	}
	if config.OtherPrivateRequestsPer100Milliseconds == 0 {
		config.OtherPrivateRequestsPer100Milliseconds = DefaultOtherPrivateRequestsPer100Milliseconds
	}
	if config.OrderRequestsPer100Milliseconds < 1 ||
		config.OrderRequestsPer100Milliseconds > DefaultOrderRequestsPer100Milliseconds ||
		config.OrderDetailRequestsPer100Milliseconds < 1 ||
		config.OrderDetailRequestsPer100Milliseconds > DefaultOrderDetailRequestsPer100Milliseconds ||
		config.HistoryRequestsPerSecond < 1 ||
		config.HistoryRequestsPerSecond > DefaultHistoryRequestsPerSecond ||
		config.OtherPrivateRequestsPer100Milliseconds < 1 ||
		config.OtherPrivateRequestsPer100Milliseconds > DefaultOtherPrivateRequestsPer100Milliseconds {
		return nil, fmt.Errorf("Crypto.com private request quotas exceed official limits")
	}
	if config.Now == nil {
		config.Now = time.Now
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeCryptoCom {
			return nil, fmt.Errorf("credential exchange must be Crypto.com")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private Crypto.com requests")
		}
		copyValue := *config.Credentials
		copyValue.Permissions = append([]credential.Permission(nil), config.Credentials.Permissions...)
		copyValue.AllowedEgressRouteIDs = append(
			[]transport.EgressRouteID(nil), config.Credentials.AllowedEgressRouteIDs...,
		)
		credentialsCopy = &copyValue
	}
	if config.Credentials == nil && config.CredentialProvider != nil {
		return nil, fmt.Errorf("credential descriptor is required with credential provider")
	}

	client := &Client{
		executor: config.Executor, defaultEgressRouteID: defaultRouteID,
		baseURL: parsedBaseURL, requestTimeout: config.RequestTimeout,
		publicRequestsPerSecond: config.PublicRequestsPerSecond,
		credentials:             credentialsCopy, credentialProvider: config.CredentialProvider,
		orderRequestsPer100Milliseconds:        config.OrderRequestsPer100Milliseconds,
		orderDetailRequestsPer100Milliseconds:  config.OrderDetailRequestsPer100Milliseconds,
		historyRequestsPerSecond:               config.HistoryRequestsPerSecond,
		otherPrivateRequestsPer100Milliseconds: config.OtherPrivateRequestsPer100Milliseconds,
		now:                                    config.Now,
	}
	return client, nil
}

func (client *Client) executePublic(
	ctx context.Context,
	method string,
	query url.Values,
	options ...trade.RequestOption,
) (commonexchange.Response, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultEgressRouteID, options...)
	if err != nil {
		return commonexchange.Response{}, err
	}
	if resolved.Timeout == 0 {
		resolved.Timeout = client.requestTimeout
	}
	limitMethod := strings.TrimPrefix(method, "public/")
	charges, err := publicRateLimit(
		client.executor.Limiter(), resolved.EgressRouteID, limitMethod,
		client.publicRequestsPerSecond,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}
	return client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeCryptoCom, EgressRouteID: resolved.EgressRouteID,
		Timeout: resolved.Timeout, Charges: charges, Operation: commonexchange.OperationRead,
		Build: func(context.Context) (*http.Request, error) {
			return client.newRequest(method, query)
		},
	})
}

func (client *Client) newRequest(method string, query url.Values) (*http.Request, error) {
	requestURL := *client.baseURL
	requestURL.Path = strings.TrimRight(client.baseURL.Path, "/") + "/" + strings.TrimLeft(method, "/")
	requestURL.RawQuery = cloneValues(query).Encode()
	request, err := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create Crypto.com request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "cex-sdk-go/0")
	return request, nil
}

func (client *Client) decodeResult(
	response commonexchange.Response,
	expectedMethod string,
) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(response.Body)
	if len(trimmed) == 0 || !json.Valid(trimmed) || trimmed[0] != '{' {
		return nil, client.decodeBodyError(
			response, errors.New("Crypto.com response is not a JSON object"),
		)
	}
	var envelope struct {
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Code    json.RawMessage `json:"code"`
		Result  json.RawMessage `json:"result"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return nil, client.decodeBodyError(response, err)
	}
	code, err := optionalScalarText(envelope.Code)
	if err != nil {
		return nil, client.decodeBodyError(
			response, fmt.Errorf("decode Crypto.com response code: %w", err),
		)
	}
	if len(bytes.TrimSpace(envelope.Code)) == 0 || code == "" {
		return nil, client.decodeBodyError(
			response, errors.New("Crypto.com response code is missing"),
		)
	}
	requestID, err := optionalScalarText(envelope.ID)
	if err != nil {
		return nil, client.decodeBodyError(
			response, fmt.Errorf("decode Crypto.com response ID: %w", err),
		)
	}
	httpSuccess := response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices
	if !httpSuccess || code != "0" {
		return nil, client.apiError(response, code, envelope.Message, requestID, nil)
	}
	if envelope.Method == "" || envelope.Method != expectedMethod {
		return nil, client.decodeBodyError(
			response, fmt.Errorf("unexpected Crypto.com response method %q", envelope.Method),
		)
	}
	if len(bytes.TrimSpace(envelope.Result)) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Result), []byte("null")) {
		return nil, client.decodeBodyError(response, errors.New("Crypto.com response result is missing"))
	}
	return cloneBytes(envelope.Result), nil
}

func (client *Client) apiError(
	response commonexchange.Response,
	code string,
	message string,
	envelopeID string,
	cause error,
) error {
	category, retryable := classifyError(response.StatusCode, code)
	return &trade.APIError{
		Category: category, Exchange: model.ExchangeCryptoCom,
		RequestID: firstNonEmpty(
			response.Header.Get("X-Request-ID"), response.Header.Get("X-Request-Id"), envelopeID,
		),
		Retryable: retryable, HTTPStatus: response.StatusCode,
		ExchangeCode: code, ExchangeMessage: message, Cause: cause,
	}
}

func (client *Client) decodeBodyError(
	response commonexchange.Response,
	cause error,
) error {
	return &trade.APIError{
		Category: trade.ErrorExchangeUnavailable, Exchange: model.ExchangeCryptoCom,
		RequestID: firstNonEmpty(
			response.Header.Get("X-Request-ID"), response.Header.Get("X-Request-Id"),
		),
		Retryable: true, HTTPStatus: response.StatusCode,
		Cause: fmt.Errorf("decode Crypto.com JSON response: %w", cause),
	}
}

func classifyError(status int, code string) (trade.ErrorCategory, bool) {
	switch strings.TrimSpace(code) {
	case "40001":
		return trade.ErrorValidation, false
	case "40101":
		return trade.ErrorAuthentication, false
	case "40801", "50001":
		return trade.ErrorExchangeUnavailable, true
	case "42901":
		return trade.ErrorRateLimited, true
	}
	if status == http.StatusTooManyRequests {
		return trade.ErrorRateLimited, true
	}
	if status == http.StatusUnauthorized {
		return trade.ErrorAuthentication, false
	}
	if status == http.StatusForbidden {
		return trade.ErrorAuthorization, false
	}
	if status >= http.StatusInternalServerError || status == http.StatusRequestTimeout {
		return trade.ErrorExchangeUnavailable, true
	}
	if status >= http.StatusBadRequest || strings.TrimSpace(code) != "" {
		return trade.ErrorValidation, false
	}
	return trade.ErrorInternal, false
}

func cloneValues(values url.Values) url.Values {
	result := make(url.Values, len(values))
	for key, items := range values {
		result[key] = append([]string(nil), items...)
	}
	return result
}

func cloneBytes(value []byte) []byte {
	return append([]byte(nil), value...)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
