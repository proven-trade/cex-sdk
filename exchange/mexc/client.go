package mexc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const (
	DefaultBaseURL        = "https://api.mexc.com"
	DefaultRequestTimeout = 10 * time.Second
	DefaultEndpointQuota  = 500
)

// Config는 MEXC Spot V3 공개 REST 클라이언트 설정이다.
type Config struct {
	Executor             *commonexchange.Executor
	DefaultEgressRouteID transport.EgressRouteID
	BaseURL              string
	AllowInsecureHTTP    bool
	RequestTimeout       time.Duration
	EndpointQuota        int
}

// Client는 MEXC Spot V3 공개 REST API를 요청별 EIP 선택과 함께 제공한다.
type Client struct {
	executor             *commonexchange.Executor
	defaultEgressRouteID transport.EgressRouteID
	baseURL              *url.URL
	requestTimeout       time.Duration
	endpointQuota        int
}

// New는 MEXC Spot V3 공개 REST 클라이언트를 생성한다.
func New(config Config) (*Client, error) {
	if config.Executor == nil {
		return nil, fmt.Errorf("MEXC executor is required")
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
		return nil, fmt.Errorf("invalid MEXC base URL %q", baseURL)
	}
	if parsedBaseURL.Scheme != "https" && !(config.AllowInsecureHTTP && parsedBaseURL.Scheme == "http") {
		return nil, fmt.Errorf("MEXC base URL must use HTTPS")
	}
	parsedBaseURL.Path = ""
	if config.RequestTimeout == 0 {
		config.RequestTimeout = DefaultRequestTimeout
	}
	if config.RequestTimeout < 0 {
		return nil, fmt.Errorf("MEXC request timeout cannot be negative")
	}
	if config.EndpointQuota == 0 {
		config.EndpointQuota = DefaultEndpointQuota
	}
	if config.EndpointQuota < 1 {
		return nil, fmt.Errorf("MEXC endpoint quota must be positive")
	}
	return &Client{
		executor: config.Executor, defaultEgressRouteID: defaultRouteID,
		baseURL: parsedBaseURL, requestTimeout: config.RequestTimeout,
		endpointQuota: config.EndpointQuota,
	}, nil
}

func (client *Client) executePublic(
	ctx context.Context,
	path string,
	query url.Values,
	limit endpointLimit,
	options ...trade.RequestOption,
) (commonexchange.Response, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultEgressRouteID, options...)
	if err != nil {
		return commonexchange.Response{}, err
	}
	if resolved.Timeout == 0 {
		resolved.Timeout = client.requestTimeout
	}
	charges, err := publicRateLimitCharges(
		client.executor.Limiter(), resolved.EgressRouteID, limit, client.endpointQuota,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}
	return client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeMEXC, EgressRouteID: resolved.EgressRouteID,
		Timeout: resolved.Timeout, Charges: charges, Operation: commonexchange.OperationRead,
		Build: func(context.Context) (*http.Request, error) {
			return client.newRequest(path, query)
		},
	})
}

func (client *Client) newRequest(path string, query url.Values) (*http.Request, error) {
	requestURL := *client.baseURL
	requestURL.Path = path
	requestURL.RawQuery = cloneValues(query).Encode()
	request, err := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create MEXC request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "proven-trade-sdk-go/0")
	return request, nil
}

func (client *Client) decodeResponse(
	response commonexchange.Response,
	target any,
) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(response.Body)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return nil, client.decodeBodyError(response, errors.New("MEXC response is not valid JSON"))
	}
	var envelope struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"msg"`
		Alt     string          `json:"message"`
	}
	if trimmed[0] == '{' {
		if err := json.Unmarshal(trimmed, &envelope); err != nil {
			return nil, client.decodeBodyError(response, err)
		}
	}
	code, err := optionalScalarText(envelope.Code)
	if err != nil {
		return nil, client.decodeBodyError(response, fmt.Errorf("decode MEXC response code: %w", err))
	}
	message := envelope.Message
	if message == "" {
		message = envelope.Alt
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices ||
		(code != "" && code != "0" && code != "200") {
		return nil, client.apiError(response, code, message, nil)
	}
	if err := json.Unmarshal(trimmed, target); err != nil {
		return nil, client.decodeBodyError(response, err)
	}
	return cloneBytes(trimmed), nil
}

func (client *Client) apiError(
	response commonexchange.Response,
	code, message string,
	cause error,
) error {
	category, retryable := classifyError(response.StatusCode, code)
	return &trade.APIError{
		Category: category, Exchange: model.ExchangeMEXC,
		RequestID: firstNonEmpty(
			response.Header.Get("X-MEXC-Request-Id"), response.Header.Get("X-Request-Id"),
		),
		Retryable: retryable, HTTPStatus: response.StatusCode,
		ExchangeCode: code, ExchangeMessage: message, Cause: cause,
	}
}

func (client *Client) decodeBodyError(
	response commonexchange.Response,
	cause error,
) error {
	category, retryable := classifyError(response.StatusCode, "")
	if category == trade.ErrorInternal || category == trade.ErrorValidation {
		category, retryable = trade.ErrorExchangeUnavailable, true
	}
	return &trade.APIError{
		Category: category, Exchange: model.ExchangeMEXC,
		RequestID: firstNonEmpty(
			response.Header.Get("X-MEXC-Request-Id"), response.Header.Get("X-Request-Id"),
		),
		Retryable: retryable, HTTPStatus: response.StatusCode,
		Cause: fmt.Errorf("decode MEXC JSON response: %w", cause),
	}
}

func classifyError(status int, code string) (trade.ErrorCategory, bool) {
	if status == http.StatusTooManyRequests || status == http.StatusTeapot || code == "429" {
		return trade.ErrorRateLimited, true
	}
	switch code {
	case "400", "401", "602", "10072", "10073", "700001", "700002", "700003":
		return trade.ErrorAuthentication, false
	case "403", "70011", "700006", "700007":
		return trade.ErrorAuthorization, false
	case "10101":
		return trade.ErrorInsufficientBalance, false
	case "-2011", "22222":
		return trade.ErrorOrderNotFound, false
	case "500", "503", "504", "20002", "730000":
		return trade.ErrorExchangeUnavailable, true
	}
	if status == http.StatusUnauthorized {
		return trade.ErrorAuthentication, false
	}
	if status == http.StatusForbidden {
		return trade.ErrorAuthorization, false
	}
	if status >= http.StatusInternalServerError {
		return trade.ErrorExchangeUnavailable, true
	}
	if status >= http.StatusBadRequest || code != "" {
		return trade.ErrorValidation, false
	}
	return trade.ErrorInternal, false
}

func optionalScalarText(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	if trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return "", err
		}
		return value, nil
	}
	if trimmed[0] == '{' || trimmed[0] == '[' || !json.Valid(trimmed) {
		return "", fmt.Errorf("value is not scalar")
	}
	return string(trimmed), nil
}

func cloneValues(values url.Values) url.Values {
	result := make(url.Values, len(values))
	for key, items := range values {
		result[key] = append([]string(nil), items...)
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
