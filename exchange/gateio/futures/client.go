// Package futures는 Gate.io API v4 무기한 Futures REST 어댑터를 제공한다.
package futures

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/transport"
)

const (
	DefaultBaseURL        = "https://api.gateio.ws"
	DefaultRequestTimeout = 10 * time.Second
	DefaultPublicQuota    = 200
	DefaultPrivateQuota   = 200
	DefaultOrderQuota     = 100
	DefaultCancelQuota    = 200
	apiPrefix             = "/api/v4"
)

// Config는 Gate.io API v4 무기한 Futures REST 클라이언트 설정이다.
type Config struct {
	Executor             *commonexchange.Executor
	Credentials          *credential.Descriptor
	CredentialProvider   credential.Provider
	DefaultEgressRouteID transport.EgressRouteID
	BaseURL              string
	AllowInsecureHTTP    bool
	RequestTimeout       time.Duration
	PublicQuota          int
	PrivateQuota         int
	OrderQuota           int
	CancelQuota          int
	Now                  func() time.Time
}

// Client는 Gate.io API v4 무기한 Futures REST API를 요청별 송신 경로 선택과 함께 제공한다.
type Client struct {
	executor             *commonexchange.Executor
	credentials          *credential.Descriptor
	credentialProvider   credential.Provider
	defaultEgressRouteID transport.EgressRouteID
	baseURL              *url.URL
	requestTimeout       time.Duration
	publicQuota          int
	privateQuota         int
	orderQuota           int
	cancelQuota          int
	now                  func() time.Time
}

// New는 검증된 Gate.io API v4 무기한 Futures REST 클라이언트를 생성한다.
func New(config Config) (*Client, error) {
	if config.Executor == nil {
		return nil, fmt.Errorf("Gate.io Futures executor is required")
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
		return nil, fmt.Errorf("invalid Gate.io Futures base URL %q", baseURL)
	}
	if parsedBaseURL.Scheme != "https" && !(config.AllowInsecureHTTP && parsedBaseURL.Scheme == "http") {
		return nil, fmt.Errorf("Gate.io Futures base URL must use HTTPS")
	}
	parsedBaseURL.Path = ""
	if config.RequestTimeout == 0 {
		config.RequestTimeout = DefaultRequestTimeout
	}
	if config.RequestTimeout < 0 {
		return nil, fmt.Errorf("Gate.io Futures request timeout cannot be negative")
	}
	if config.PublicQuota == 0 {
		config.PublicQuota = DefaultPublicQuota
	}
	if config.PrivateQuota == 0 {
		config.PrivateQuota = DefaultPrivateQuota
	}
	if config.OrderQuota == 0 {
		config.OrderQuota = DefaultOrderQuota
	}
	if config.CancelQuota == 0 {
		config.CancelQuota = DefaultCancelQuota
	}
	if config.PublicQuota < 1 || config.PrivateQuota < 1 || config.OrderQuota < 1 ||
		config.CancelQuota < 1 {
		return nil, fmt.Errorf("Gate.io Futures request quotas must be positive")
	}
	if config.Now == nil {
		config.Now = time.Now
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeGateIO {
			return nil, fmt.Errorf("credential exchange must be Gate.io")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private Gate.io Futures requests")
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

	return &Client{
		executor: config.Executor, credentials: credentialsCopy,
		credentialProvider: config.CredentialProvider, defaultEgressRouteID: defaultRouteID,
		baseURL: parsedBaseURL, requestTimeout: config.RequestTimeout,
		publicQuota: config.PublicQuota, privateQuota: config.PrivateQuota,
		orderQuota: config.OrderQuota, cancelQuota: config.CancelQuota, now: config.Now,
	}, nil
}

func (client *Client) executePublic(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	limit endpointLimit,
	options ...trade.RequestOption,
) (commonexchange.Response, error) {
	resolved, err := client.resolveOptions(options...)
	if err != nil {
		return commonexchange.Response{}, err
	}
	registered, charges, err := rateLimitCharges(
		client.executor.Limiter(), resolved.EgressRouteID, "", limit,
		client.publicQuota, client.privateQuota, client.orderQuota, client.cancelQuota,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}
	response, err := client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeGateIO, EgressRouteID: resolved.EgressRouteID,
		Timeout: resolved.Timeout, Charges: charges, Operation: commonexchange.OperationRead,
		Build: func(context.Context) (*http.Request, error) {
			return client.newRequest(method, path, query, nil)
		},
	})
	if err == nil {
		observeRateLimit(client.executor.Limiter(), registered, response.Header, client.now())
	}
	return response, err
}

func (client *Client) executePrivate(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	body []byte,
	limit endpointLimit,
	permission credential.Permission,
	operation commonexchange.OperationKind,
	options ...trade.RequestOption,
) (commonexchange.Response, error) {
	resolved, err := client.resolveOptions(options...)
	if err != nil {
		return commonexchange.Response{}, err
	}
	if client.credentials == nil || client.credentialProvider == nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthentication, Exchange: model.ExchangeGateIO,
			Cause: errors.New("private Gate.io Futures request requires credentials"),
		}
	}
	if err := client.credentials.RequireEgressRoute(resolved.EgressRouteID); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeGateIO,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	if err := client.credentials.RequirePermission(permission); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeGateIO,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	registered, charges, err := rateLimitCharges(
		client.executor.Limiter(), resolved.EgressRouteID, client.credentials.AccountID, limit,
		client.publicQuota, client.privateQuota, client.orderQuota, client.cancelQuota,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}

	var material credential.Material
	defer material.Destroy()
	response, err := client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeGateIO, AccountID: client.credentials.AccountID,
		EgressRouteID: resolved.EgressRouteID, Timeout: resolved.Timeout,
		Charges: charges, Operation: operation,
		Build: func(buildContext context.Context) (*http.Request, error) {
			resolvedMaterial, resolveErr := client.credentialProvider.Resolve(
				buildContext, client.credentials.SecretRef,
			)
			material = resolvedMaterial
			if resolveErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeGateIO,
					AccountID: client.credentials.AccountID, Cause: resolveErr,
				}
			}
			if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeGateIO,
					AccountID: client.credentials.AccountID,
					Cause:     errors.New("Gate.io Futures API key and HMAC secret are required"),
				}
			}
			request, requestErr := client.newRequest(method, path, query, body)
			if requestErr != nil {
				return nil, requestErr
			}
			timestamp := client.now().Unix()
			if timestamp <= 0 {
				return nil, validationError("Gate.io Futures timestamp must be after the Unix epoch")
			}
			timestampText := strconv.FormatInt(timestamp, 10)
			signature, signErr := SignHMACSHA512(material.SecretKey, signaturePayload(
				method, request.URL.EscapedPath(), request.URL.RawQuery,
				PayloadHash(body), timestampText,
			))
			if signErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeGateIO,
					AccountID: client.credentials.AccountID, Cause: signErr,
				}
			}
			request.Header.Set("KEY", string(material.APIKey))
			request.Header.Set("Timestamp", timestampText)
			request.Header.Set("SIGN", signature)
			return request, nil
		},
	})
	if err == nil {
		observeRateLimit(client.executor.Limiter(), registered, response.Header, client.now())
	}
	return response, err
}

func (client *Client) resolveOptions(options ...trade.RequestOption) (trade.RequestOptions, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultEgressRouteID, options...)
	if err != nil {
		return trade.RequestOptions{}, err
	}
	if resolved.Timeout == 0 {
		resolved.Timeout = client.requestTimeout
	}
	return resolved, nil
}

func (client *Client) newRequest(
	method string,
	path string,
	query url.Values,
	body []byte,
) (*http.Request, error) {
	requestURL := *client.baseURL
	requestURL.Path = apiPrefix + path
	requestURL.RawQuery = cloneValues(query).Encode()
	request, err := http.NewRequest(method, requestURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create Gate.io Futures request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "cex-sdk-go/0")
	return request, nil
}

func (client *Client) decodeData(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	target any,
) (json.RawMessage, error) {
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, client.errorResponse(response, operation)
	}
	trimmed := bytes.TrimSpace(response.Body)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, client.decodeBodyError(
			response, operation, errors.New("Gate.io Futures response body is missing"),
		)
	}
	if err := json.Unmarshal(trimmed, target); err != nil {
		return nil, client.decodeBodyError(response, operation, err)
	}
	return cloneBytes(trimmed), nil
}

func (client *Client) errorResponse(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
) error {
	var envelope struct {
		Label   string          `json:"label"`
		Message string          `json:"message"`
		Detail  json.RawMessage `json:"detail"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err != nil {
		return client.apiError(response, "", "", operation, fmt.Errorf(
			"decode Gate.io Futures JSON error response: %w", err,
		))
	}
	message := envelope.Message
	if message == "" && len(envelope.Detail) > 0 && !bytes.Equal(envelope.Detail, []byte("null")) {
		message = strings.Trim(string(envelope.Detail), `"`)
	}
	return client.apiError(response, envelope.Label, message, operation, nil)
}

func (client *Client) apiError(
	response commonexchange.Response,
	code string,
	message string,
	operation commonexchange.OperationKind,
	cause error,
) error {
	category, retryable := classifyError(response.StatusCode, code, message, operation)
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: category, Exchange: model.ExchangeGateIO, AccountID: accountID,
		RequestID: firstNonEmpty(
			response.Header.Get("X-Gate-Trace-ID"), response.Header.Get("X-Request-Id"),
		),
		Retryable: retryable, HTTPStatus: response.StatusCode,
		ExchangeCode: code, ExchangeMessage: message, Cause: cause,
	}
}

func (client *Client) decodeBodyError(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	cause error,
) error {
	category, retryable := classifyError(response.StatusCode, "", "", operation)
	if category == trade.ErrorInternal || category == trade.ErrorValidation {
		category, retryable = trade.ErrorExchangeUnavailable, operation == commonexchange.OperationRead
		if operation == commonexchange.OperationMutation {
			category, retryable = trade.ErrorUnknownExecutionState, false
		}
	}
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: category, Exchange: model.ExchangeGateIO, AccountID: accountID,
		RequestID: firstNonEmpty(
			response.Header.Get("X-Gate-Trace-ID"), response.Header.Get("X-Request-Id"),
		),
		Retryable: retryable, HTTPStatus: response.StatusCode,
		Cause: fmt.Errorf("decode Gate.io Futures JSON response: %w", cause),
	}
}

func classifyError(
	status int,
	code string,
	message string,
	operation commonexchange.OperationKind,
) (trade.ErrorCategory, bool) {
	normalizedCode := strings.ToUpper(strings.TrimSpace(code))
	if status == http.StatusTooManyRequests || normalizedCode == "TOO_FAST" ||
		normalizedCode == "RATE_LIMIT_EXCEEDED" {
		return trade.ErrorRateLimited, true
	}
	switch normalizedCode {
	case "INVALID_KEY", "INVALID_SIGNATURE", "REQUEST_EXPIRED", "MISSING_REQUIRED_HEADER":
		return trade.ErrorAuthentication, false
	case "FORBIDDEN", "IP_FORBIDDEN", "READ_ONLY", "NO_PRIVILEGE":
		return trade.ErrorAuthorization, false
	case "BALANCE_NOT_ENOUGH", "INSUFFICIENT_BALANCE", "MARGIN_BALANCE_NOT_ENOUGH":
		return trade.ErrorInsufficientBalance, false
	case "ORDER_NOT_FOUND", "ORDER_NOT_FOUND_OR_TOO_LATE":
		return trade.ErrorOrderNotFound, false
	case "INTERNAL", "SERVER_ERROR", "SERVICE_UNAVAILABLE":
		if operation == commonexchange.OperationMutation {
			return trade.ErrorUnknownExecutionState, false
		}
		return trade.ErrorExchangeUnavailable, true
	}
	lowerMessage := strings.ToLower(message)
	if strings.Contains(lowerMessage, "order") &&
		(strings.Contains(lowerMessage, "not found") || strings.Contains(lowerMessage, "not exist")) {
		return trade.ErrorOrderNotFound, false
	}
	if strings.Contains(lowerMessage, "balance") &&
		(strings.Contains(lowerMessage, "not enough") || strings.Contains(lowerMessage, "insufficient")) {
		return trade.ErrorInsufficientBalance, false
	}
	if status == http.StatusUnauthorized {
		return trade.ErrorAuthentication, false
	}
	if status == http.StatusForbidden {
		return trade.ErrorAuthorization, false
	}
	if status >= http.StatusInternalServerError {
		if operation == commonexchange.OperationMutation {
			return trade.ErrorUnknownExecutionState, false
		}
		return trade.ErrorExchangeUnavailable, true
	}
	if status == http.StatusNotFound {
		return trade.ErrorOrderNotFound, false
	}
	if status >= http.StatusBadRequest || normalizedCode != "" {
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
