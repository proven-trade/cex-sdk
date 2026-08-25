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
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const (
	DefaultBaseURL          = "https://api.mexc.com"
	DefaultRequestTimeout   = 10 * time.Second
	DefaultEndpointQuota    = 500
	DefaultReceiveWindow    = 5 * time.Second
	DefaultOrderQuota       = 5
	DefaultCancelQuota      = 50
	DefaultPrivateReadQuota = 50
	DefaultAccountQuota     = 2
)

// Config는 MEXC Spot V3 REST 클라이언트 설정이다.
type Config struct {
	Executor             *commonexchange.Executor
	Credentials          *credential.Descriptor
	CredentialProvider   credential.Provider
	DefaultEgressRouteID transport.EgressRouteID
	BaseURL              string
	AllowInsecureHTTP    bool
	RequestTimeout       time.Duration
	EndpointQuota        int
	ReceiveWindow        time.Duration
	OrderQuota           int
	CancelQuota          int
	PrivateReadQuota     int
	AccountQuota         int
	Now                  func() time.Time
}

// Client는 MEXC Spot V3 REST API를 요청별 EIP 선택과 함께 제공한다.
type Client struct {
	executor             *commonexchange.Executor
	credentials          *credential.Descriptor
	credentialProvider   credential.Provider
	defaultEgressRouteID transport.EgressRouteID
	baseURL              *url.URL
	requestTimeout       time.Duration
	endpointQuota        int
	receiveWindow        time.Duration
	orderQuota           int
	cancelQuota          int
	privateReadQuota     int
	accountQuota         int
	now                  func() time.Time
}

// New는 MEXC Spot V3 REST 클라이언트를 생성한다.
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
	if config.ReceiveWindow == 0 {
		config.ReceiveWindow = DefaultReceiveWindow
	}
	if config.ReceiveWindow < time.Millisecond || config.ReceiveWindow > 60*time.Second {
		return nil, fmt.Errorf("MEXC receive window must be between 1ms and 60s")
	}
	if config.OrderQuota == 0 {
		config.OrderQuota = DefaultOrderQuota
	}
	if config.CancelQuota == 0 {
		config.CancelQuota = DefaultCancelQuota
	}
	if config.PrivateReadQuota == 0 {
		config.PrivateReadQuota = DefaultPrivateReadQuota
	}
	if config.AccountQuota == 0 {
		config.AccountQuota = DefaultAccountQuota
	}
	if config.OrderQuota < 1 || config.CancelQuota < 1 ||
		config.PrivateReadQuota < 1 || config.AccountQuota < 1 {
		return nil, fmt.Errorf("MEXC private request quotas must be positive")
	}
	if config.Now == nil {
		config.Now = time.Now
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeMEXC {
			return nil, fmt.Errorf("credential exchange must be MEXC")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private MEXC requests")
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
		endpointQuota: config.EndpointQuota, receiveWindow: config.ReceiveWindow,
		orderQuota: config.OrderQuota, cancelQuota: config.CancelQuota,
		privateReadQuota: config.PrivateReadQuota, accountQuota: config.AccountQuota,
		now: config.Now,
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
			return client.newRequest(http.MethodGet, path, query)
		},
	})
}

func (client *Client) executePrivate(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	limit endpointLimit,
	frequencyName string,
	frequencyQuota int,
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
			Category: trade.ErrorAuthentication, Exchange: model.ExchangeMEXC,
			Cause: errors.New("private MEXC request requires credentials"),
		}
	}
	if err := client.credentials.RequireEgressRoute(resolved.EgressRouteID); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeMEXC,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	if err := client.credentials.RequirePermission(permission); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeMEXC,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	charges, err := privateRateLimitCharges(
		client.executor.Limiter(), resolved.EgressRouteID, client.credentials.AccountID,
		limit, client.endpointQuota, frequencyName, frequencyQuota,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}

	baseQuery := cloneValues(query)
	var material credential.Material
	defer material.Destroy()
	return client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeMEXC, AccountID: client.credentials.AccountID,
		EgressRouteID: resolved.EgressRouteID, Timeout: resolved.Timeout,
		Charges: charges, Operation: operation,
		Build: func(buildContext context.Context) (*http.Request, error) {
			resolvedMaterial, resolveErr := client.credentialProvider.Resolve(
				buildContext, client.credentials.SecretRef,
			)
			material = resolvedMaterial
			if resolveErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeMEXC,
					AccountID: client.credentials.AccountID, Cause: resolveErr,
				}
			}
			if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeMEXC,
					AccountID: client.credentials.AccountID,
					Cause:     errors.New("MEXC API key and HMAC secret are required"),
				}
			}
			signedQuery := cloneValues(baseQuery)
			timestamp := client.now().UnixMilli()
			if timestamp <= 0 {
				return nil, validationError("MEXC timestamp must be after the Unix epoch")
			}
			signedQuery.Set("recvWindow", fmt.Sprintf("%d", client.receiveWindow.Milliseconds()))
			signedQuery.Set("timestamp", fmt.Sprintf("%d", timestamp))
			signature, signErr := SignHMACSHA256(material.SecretKey, []byte(signedQuery.Encode()))
			if signErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeMEXC,
					AccountID: client.credentials.AccountID, Cause: signErr,
				}
			}
			signedQuery.Set("signature", signature)
			request, requestErr := client.newRequest(method, path, signedQuery)
			if requestErr != nil {
				return nil, requestErr
			}
			request.Header.Set("X-MEXC-APIKEY", string(material.APIKey))
			return request, nil
		},
	})
}

func (client *Client) newRequest(method, path string, query url.Values) (*http.Request, error) {
	requestURL := *client.baseURL
	requestURL.Path = path
	requestURL.RawQuery = cloneValues(query).Encode()
	request, err := http.NewRequest(method, requestURL.String(), nil)
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
	return client.decodeResponseForOperation(response, commonexchange.OperationRead, target)
}

func (client *Client) decodeResponseForOperation(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	target any,
) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(response.Body)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return nil, client.decodeBodyErrorForOperation(
			response, operation, errors.New("MEXC response is not valid JSON"),
		)
	}
	var envelope struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"msg"`
		Alt     string          `json:"message"`
	}
	if trimmed[0] == '{' {
		if err := json.Unmarshal(trimmed, &envelope); err != nil {
			return nil, client.decodeBodyErrorForOperation(response, operation, err)
		}
	}
	code, err := optionalScalarText(envelope.Code)
	if err != nil {
		return nil, client.decodeBodyErrorForOperation(
			response, operation, fmt.Errorf("decode MEXC response code: %w", err),
		)
	}
	message := envelope.Message
	if message == "" {
		message = envelope.Alt
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices ||
		(code != "" && code != "0" && code != "200") {
		return nil, client.apiError(response, code, message, operation, nil)
	}
	if err := json.Unmarshal(trimmed, target); err != nil {
		return nil, client.decodeBodyErrorForOperation(response, operation, err)
	}
	return cloneBytes(trimmed), nil
}

func (client *Client) apiError(
	response commonexchange.Response,
	code, message string,
	operation commonexchange.OperationKind,
	cause error,
) error {
	category, retryable := classifyError(response.StatusCode, code, operation)
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: category, Exchange: model.ExchangeMEXC, AccountID: accountID,
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
	return client.decodeBodyErrorForOperation(response, commonexchange.OperationRead, cause)
}

func (client *Client) decodeBodyErrorForOperation(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	cause error,
) error {
	category, retryable := classifyError(response.StatusCode, "", operation)
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
		Category: category, Exchange: model.ExchangeMEXC, AccountID: accountID,
		RequestID: firstNonEmpty(
			response.Header.Get("X-MEXC-Request-Id"), response.Header.Get("X-Request-Id"),
		),
		Retryable: retryable, HTTPStatus: response.StatusCode,
		Cause: fmt.Errorf("decode MEXC JSON response: %w", cause),
	}
}

func classifyError(
	status int,
	code string,
	operation commonexchange.OperationKind,
) (trade.ErrorCategory, bool) {
	if status == http.StatusTooManyRequests || status == http.StatusTeapot || code == "429" {
		return trade.ErrorRateLimited, true
	}
	if status >= http.StatusInternalServerError {
		if operation == commonexchange.OperationMutation {
			return trade.ErrorUnknownExecutionState, false
		}
		return trade.ErrorExchangeUnavailable, true
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
		if operation == commonexchange.OperationMutation {
			return trade.ErrorUnknownExecutionState, false
		}
		return trade.ErrorExchangeUnavailable, true
	}
	if status == http.StatusUnauthorized {
		return trade.ErrorAuthentication, false
	}
	if status == http.StatusForbidden {
		return trade.ErrorAuthorization, false
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
