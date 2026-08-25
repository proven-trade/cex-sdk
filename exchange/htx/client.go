// Package htx는 HTX Spot REST·WebSocket·공통 어댑터를 제공한다.
package htx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const (
	DefaultBaseURL                 = "https://api.huobi.pro"
	DefaultAWSBaseURL              = "https://api-aws.huobi.pro"
	DefaultRequestTimeout          = 10 * time.Second
	DefaultPublicRequestsPerSecond = 10
	DefaultAccountQuota            = 100
	DefaultOrderQuota              = 100
	DefaultOrderReadQuota          = 50
	DefaultTradeHistoryQuota       = 20
)

// Config는 HTX Spot REST 클라이언트 설정이다.
type Config struct {
	Executor                *commonexchange.Executor
	Credentials             *credential.Descriptor
	CredentialProvider      credential.Provider
	DefaultEgressRouteID    transport.EgressRouteID
	BaseURL                 string
	AllowInsecureHTTP       bool
	RequestTimeout          time.Duration
	PublicRequestsPerSecond int
	AccountQuota            int
	OrderQuota              int
	OrderReadQuota          int
	TradeHistoryQuota       int
	Now                     func() time.Time
}

// Client는 HTX Spot REST API를 요청별 EIP 선택과 함께 제공한다.
type Client struct {
	executor                *commonexchange.Executor
	credentials             *credential.Descriptor
	credentialProvider      credential.Provider
	defaultEgressRouteID    transport.EgressRouteID
	baseURL                 *url.URL
	requestTimeout          time.Duration
	publicRequestsPerSecond int
	accountQuota            int
	orderQuota              int
	orderReadQuota          int
	tradeHistoryQuota       int
	now                     func() time.Time
}

// New는 검증된 HTX Spot REST 클라이언트를 생성한다.
func New(config Config) (*Client, error) {
	if config.Executor == nil {
		return nil, fmt.Errorf("HTX executor is required")
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
		return nil, fmt.Errorf("invalid HTX base URL %q", baseURL)
	}
	if parsedBaseURL.Scheme != "https" && !(config.AllowInsecureHTTP && parsedBaseURL.Scheme == "http") {
		return nil, fmt.Errorf("HTX base URL must use HTTPS")
	}
	parsedBaseURL.Path = ""
	if config.RequestTimeout == 0 {
		config.RequestTimeout = DefaultRequestTimeout
	}
	if config.RequestTimeout < 0 {
		return nil, fmt.Errorf("HTX request timeout cannot be negative")
	}
	if config.PublicRequestsPerSecond == 0 {
		config.PublicRequestsPerSecond = DefaultPublicRequestsPerSecond
	}
	if config.PublicRequestsPerSecond < 1 {
		return nil, fmt.Errorf("HTX public request quota must be positive")
	}
	if config.AccountQuota == 0 {
		config.AccountQuota = DefaultAccountQuota
	}
	if config.OrderQuota == 0 {
		config.OrderQuota = DefaultOrderQuota
	}
	if config.OrderReadQuota == 0 {
		config.OrderReadQuota = DefaultOrderReadQuota
	}
	if config.TradeHistoryQuota == 0 {
		config.TradeHistoryQuota = DefaultTradeHistoryQuota
	}
	if config.AccountQuota < 1 || config.OrderQuota < 1 ||
		config.OrderReadQuota < 1 || config.TradeHistoryQuota < 1 {
		return nil, fmt.Errorf("HTX private request quotas must be positive")
	}
	if config.Now == nil {
		config.Now = time.Now
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeHTX {
			return nil, fmt.Errorf("credential exchange must be HTX")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private HTX requests")
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
		publicRequestsPerSecond: config.PublicRequestsPerSecond, now: config.Now,
		accountQuota: config.AccountQuota, orderQuota: config.OrderQuota,
		orderReadQuota: config.OrderReadQuota, tradeHistoryQuota: config.TradeHistoryQuota,
	}, nil
}

func (client *Client) executePublic(
	ctx context.Context,
	path string,
	query url.Values,
	endpoint string,
	options ...trade.RequestOption,
) (commonexchange.Response, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultEgressRouteID, options...)
	if err != nil {
		return commonexchange.Response{}, err
	}
	if resolved.Timeout == 0 {
		resolved.Timeout = client.requestTimeout
	}
	limit, charges, err := publicRateLimit(
		client.executor.Limiter(), resolved.EgressRouteID, endpoint,
		client.publicRequestsPerSecond,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}
	response, err := client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeHTX, EgressRouteID: resolved.EgressRouteID,
		Timeout: resolved.Timeout, Charges: charges, Operation: commonexchange.OperationRead,
		Build: func(context.Context) (*http.Request, error) {
			return client.newRequest(http.MethodGet, path, query)
		},
	})
	if err == nil {
		observeRateLimit(client.executor.Limiter(), limit, response.Header, client.now())
	}
	return response, err
}

func (client *Client) executePrivate(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	body []byte,
	group rateGroup,
	quota int,
	permission credential.Permission,
	operation commonexchange.OperationKind,
	options ...trade.RequestOption,
) (commonexchange.Response, error) {
	resolved, err := trade.ResolveRequestOptions(client.defaultEgressRouteID, options...)
	if err != nil {
		return commonexchange.Response{}, err
	}
	if resolved.Timeout == 0 {
		resolved.Timeout = client.requestTimeout
	}
	if client.credentials == nil || client.credentialProvider == nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthentication, Exchange: model.ExchangeHTX,
			Cause: errors.New("private HTX request requires credentials"),
		}
	}
	if err := client.credentials.RequireEgressRoute(resolved.EgressRouteID); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeHTX,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	if err := client.credentials.RequirePermission(permission); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeHTX,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	limit, charges, err := privateRateLimit(
		client.executor.Limiter(), client.credentials.AccountID, group, quota,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}

	baseQuery := cloneValues(query)
	bodyCopy := cloneBytes(body)
	var material credential.Material
	defer material.Destroy()
	response, err := client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeHTX, AccountID: client.credentials.AccountID,
		EgressRouteID: resolved.EgressRouteID, Timeout: resolved.Timeout,
		Charges: charges, Operation: operation,
		Build: func(buildContext context.Context) (*http.Request, error) {
			resolvedMaterial, resolveErr := client.credentialProvider.Resolve(
				buildContext, client.credentials.SecretRef,
			)
			material = resolvedMaterial
			if resolveErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeHTX,
					AccountID: client.credentials.AccountID, Cause: resolveErr,
				}
			}
			if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeHTX,
					AccountID: client.credentials.AccountID,
					Cause:     errors.New("HTX access key and HMAC secret are required"),
				}
			}
			timestamp := client.now().UTC()
			if timestamp.Unix() <= 0 {
				return nil, validationError("HTX timestamp must be after the Unix epoch")
			}
			signedQuery := cloneValues(baseQuery)
			signedQuery.Set("AccessKeyId", string(material.APIKey))
			signedQuery.Set("SignatureMethod", "HmacSHA256")
			signedQuery.Set("SignatureVersion", "2")
			signedQuery.Set("Timestamp", timestamp.Format("2006-01-02T15:04:05"))
			payload := SignaturePayload(
				method, strings.ToLower(client.baseURL.Host), path, canonicalQuery(signedQuery),
			)
			signature, signErr := SignHMACSHA256Base64(material.SecretKey, payload)
			if signErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeHTX,
					AccountID: client.credentials.AccountID, Cause: signErr,
				}
			}
			signedQuery.Set("Signature", signature)
			return client.newRequestWithBody(method, path, signedQuery, bodyCopy)
		},
	})
	if err == nil {
		observeRateLimit(client.executor.Limiter(), limit, response.Header, client.now())
	}
	return response, err
}

func (client *Client) newRequest(method, path string, query url.Values) (*http.Request, error) {
	return client.newRequestWithBody(method, path, query, nil)
}

func (client *Client) newRequestWithBody(
	method, path string,
	query url.Values,
	body []byte,
) (*http.Request, error) {
	requestURL := *client.baseURL
	requestURL.Path = path
	requestURL.RawQuery = cloneValues(query).Encode()
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequest(method, requestURL.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("create HTX request: %w", err)
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
	return client.decodeResponseForOperation(response, commonexchange.OperationRead, target)
}

func (client *Client) decodeResponseForOperation(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	target any,
) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(response.Body)
	if len(trimmed) == 0 || !json.Valid(trimmed) || trimmed[0] != '{' {
		return nil, client.decodeBodyErrorForOperation(
			response, operation, errors.New("HTX response is not a JSON object"),
		)
	}
	var envelope struct {
		Status       string          `json:"status"`
		ErrorCode    string          `json:"err-code"`
		ErrorMessage string          `json:"err-msg"`
		Code         json.RawMessage `json:"code"`
		Message      string          `json:"message"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return nil, client.decodeBodyErrorForOperation(response, operation, err)
	}
	code, err := optionalScalarText(envelope.Code)
	if err != nil {
		return nil, client.decodeBodyErrorForOperation(
			response, operation, fmt.Errorf("decode HTX response code: %w", err),
		)
	}
	if envelope.ErrorCode != "" {
		code = envelope.ErrorCode
	}
	message := envelope.ErrorMessage
	if message == "" {
		message = envelope.Message
	}
	httpSuccess := response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices
	logicalSuccess := envelope.Status == "ok" ||
		(envelope.Status == "" && (code == "0" || code == "200"))
	if httpSuccess && envelope.Status == "" && code == "" {
		return nil, client.decodeBodyErrorForOperation(
			response, operation, errors.New("HTX response status is missing"),
		)
	}
	if !httpSuccess || !logicalSuccess {
		return nil, client.apiError(response, code, message, operation, nil)
	}
	if err := json.Unmarshal(trimmed, target); err != nil {
		return nil, client.decodeBodyErrorForOperation(response, operation, err)
	}
	return cloneBytes(trimmed), nil
}

func (client *Client) decodeDataForOperation(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	target any,
) (json.RawMessage, error) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if _, err := client.decodeResponseForOperation(response, operation, &envelope); err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(envelope.Data)) == 0 {
		return nil, client.decodeBodyErrorForOperation(
			response, operation, errors.New("HTX response data is missing"),
		)
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		return nil, client.decodeBodyErrorForOperation(response, operation, err)
	}
	return cloneBytes(envelope.Data), nil
}

func (client *Client) apiError(
	response commonexchange.Response,
	code, message string,
	operation commonexchange.OperationKind,
	cause error,
) error {
	category, retryable := classifyError(response.StatusCode, code, message, operation)
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: category, Exchange: model.ExchangeHTX, AccountID: accountID,
		RequestID: firstNonEmpty(
			response.Header.Get("request-id"), response.Header.Get("X-Request-Id"),
		),
		Retryable: retryable, HTTPStatus: response.StatusCode,
		ExchangeCode: code, ExchangeMessage: message, Cause: cause,
	}
}

func (client *Client) decodeBodyError(
	response commonexchange.Response,
	cause error,
) error {
	return client.decodeBodyErrorForOperation(response, commonexchange.OperationRead, cause)
}

func (client *Client) decodeBodyErrorForOperation(
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
		Category: category, Exchange: model.ExchangeHTX, AccountID: accountID,
		RequestID: firstNonEmpty(
			response.Header.Get("request-id"), response.Header.Get("X-Request-Id"),
		),
		Retryable: retryable, HTTPStatus: response.StatusCode,
		Cause: fmt.Errorf("decode HTX JSON response: %w", cause),
	}
}

func classifyError(
	status int,
	code, message string,
	operation commonexchange.OperationKind,
) (trade.ErrorCategory, bool) {
	code = strings.ToLower(strings.TrimSpace(code))
	message = strings.ToLower(strings.TrimSpace(message))
	if status == http.StatusTooManyRequests ||
		code == "too-many-requests" || code == "frequent-visit" || code == "1006" {
		return trade.ErrorRateLimited, true
	}
	if status >= http.StatusInternalServerError {
		if operation == commonexchange.OperationMutation {
			return trade.ErrorUnknownExecutionState, false
		}
		return trade.ErrorExchangeUnavailable, true
	}
	if status == http.StatusUnauthorized || code == "require-auth" || code == "login-required" ||
		code == "api-signature-not-valid" || code == "1002" || code == "1003" {
		return trade.ErrorAuthentication, false
	}
	if status == http.StatusForbidden || code == "api-key-no-permission" ||
		code == "base-operation-forbidden" || strings.HasPrefix(code, "operation-forbidden") ||
		strings.Contains(message, "permission") || strings.Contains(message, "ip address") {
		return trade.ErrorAuthorization, false
	}
	if code == "order-accountbalance-error" || strings.Contains(code, "balance") ||
		strings.Contains(message, "insufficient balance") {
		return trade.ErrorInsufficientBalance, false
	}
	if code == "not-found" || code == "base-not-found" || code == "base-record-invalid" ||
		code == "1007" ||
		strings.Contains(message, "order not found") {
		return trade.ErrorOrderNotFound, false
	}
	if code == "gateway-internal-error" || code == "service-unavailable" ||
		code == "base-system-error" || code == "order-update-error" ||
		code == "request-timeout" || code == "500" || strings.Contains(message, "request timeout") {
		if operation == commonexchange.OperationMutation {
			return trade.ErrorUnknownExecutionState, false
		}
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
	if bytes.Equal(trimmed, []byte("true")) || bytes.Equal(trimmed, []byte("false")) {
		return "", fmt.Errorf("value is not a string or number")
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
