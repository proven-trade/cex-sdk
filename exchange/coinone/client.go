package coinone

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
	DefaultBaseURL                  = "https://api.coinone.co.kr"
	DefaultRequestTimeout           = 10 * time.Second
	DefaultPublicRequestsPerMinute  = 1200
	DefaultPrivateRequestsPerSecond = 80
	DefaultOrderRequestsPerSecond   = 40
)

// NonceSource는 인증 요청마다 중복되지 않는 UUID v4 nonce를 생성한다.
type NonceSource func() (string, error)

// Config는 코인원 Spot REST 클라이언트 설정이다.
type Config struct {
	Executor                 *commonexchange.Executor
	Credentials              *credential.Descriptor
	CredentialProvider       credential.Provider
	DefaultEgressRouteID     transport.EgressRouteID
	BaseURL                  string
	AllowInsecureHTTP        bool
	RequestTimeout           time.Duration
	NonceSource              NonceSource
	Now                      func() time.Time
	PublicRequestsPerMinute  int
	PrivateRequestsPerSecond int
	OrderRequestsPerSecond   int
}

// Client는 코인원 Spot REST API를 요청별 EIP 선택과 함께 제공한다.
type Client struct {
	executor                 *commonexchange.Executor
	credentials              *credential.Descriptor
	credentialProvider       credential.Provider
	defaultEgressRouteID     transport.EgressRouteID
	baseURL                  *url.URL
	requestTimeout           time.Duration
	nonceSource              NonceSource
	now                      func() time.Time
	publicRequestsPerMinute  int
	privateRequestsPerSecond int
	orderRequestsPerSecond   int
}

// New는 코인원 Spot REST 클라이언트를 생성한다.
func New(config Config) (*Client, error) {
	if config.Executor == nil {
		return nil, fmt.Errorf("Coinone executor is required")
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
		return nil, fmt.Errorf("invalid Coinone base URL %q", baseURL)
	}
	if parsedBaseURL.Scheme != "https" && !(config.AllowInsecureHTTP && parsedBaseURL.Scheme == "http") {
		return nil, fmt.Errorf("Coinone base URL must use HTTPS")
	}
	parsedBaseURL.Path = ""
	if config.RequestTimeout == 0 {
		config.RequestTimeout = DefaultRequestTimeout
	}
	if config.RequestTimeout < 0 {
		return nil, fmt.Errorf("Coinone request timeout cannot be negative")
	}
	if config.NonceSource == nil {
		config.NonceSource = randomNonce
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.PublicRequestsPerMinute == 0 {
		config.PublicRequestsPerMinute = DefaultPublicRequestsPerMinute
	}
	if config.PrivateRequestsPerSecond == 0 {
		config.PrivateRequestsPerSecond = DefaultPrivateRequestsPerSecond
	}
	if config.OrderRequestsPerSecond == 0 {
		config.OrderRequestsPerSecond = DefaultOrderRequestsPerSecond
	}
	if config.PublicRequestsPerMinute < 1 || config.PrivateRequestsPerSecond < 1 || config.OrderRequestsPerSecond < 1 {
		return nil, fmt.Errorf("Coinone request limits must be positive")
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeCoinone {
			return nil, fmt.Errorf("credential exchange must be Coinone")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private Coinone requests")
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
		nonceSource: config.NonceSource, now: config.Now,
		publicRequestsPerMinute:  config.PublicRequestsPerMinute,
		privateRequestsPerSecond: config.PrivateRequestsPerSecond,
		orderRequestsPerSecond:   config.OrderRequestsPerSecond,
	}, nil
}

func (client *Client) executePublic(
	ctx context.Context,
	path string,
	query url.Values,
	options ...trade.RequestOption,
) (commonexchange.Response, error) {
	resolved, err := client.resolveOptions(options...)
	if err != nil {
		return commonexchange.Response{}, err
	}
	limit, charges, err := publicRateLimit(
		client.executor.Limiter(), resolved.EgressRouteID, client.publicRequestsPerMinute,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}
	response, err := client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeCoinone, EgressRouteID: resolved.EgressRouteID,
		Timeout: resolved.Timeout, Charges: charges, Operation: commonexchange.OperationRead,
		Build: func(context.Context) (*http.Request, error) {
			return client.newRequest(http.MethodGet, path, query, nil)
		},
	})
	if err == nil {
		observeRateLimit(client.executor.Limiter(), limit, response.Header)
	}
	return response, err
}

func (client *Client) executePrivate(
	ctx context.Context,
	path string,
	fields payloadFields,
	order bool,
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
			Category: trade.ErrorAuthentication, Exchange: model.ExchangeCoinone,
			Cause: errors.New("private Coinone request requires credentials"),
		}
	}
	if err := client.credentials.RequireEgressRoute(resolved.EgressRouteID); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeCoinone,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	if err := client.credentials.RequirePermission(permission); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeCoinone,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	limit, charges, err := privateRateLimit(
		client.executor.Limiter(), client.credentials.AccountID, order,
		client.privateRequestsPerSecond, client.orderRequestsPerSecond,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}

	var material credential.Material
	var privateBody []byte
	defer func() {
		material.Destroy()
		clear(privateBody)
	}()
	response, err := client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeCoinone, AccountID: client.credentials.AccountID,
		EgressRouteID: resolved.EgressRouteID, Timeout: resolved.Timeout,
		Charges: charges, Operation: operation,
		Build: func(buildContext context.Context) (*http.Request, error) {
			resolvedMaterial, resolveErr := client.credentialProvider.Resolve(
				buildContext, client.credentials.SecretRef,
			)
			material = resolvedMaterial
			if resolveErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeCoinone,
					AccountID: client.credentials.AccountID, Cause: resolveErr,
				}
			}
			if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeCoinone,
					AccountID: client.credentials.AccountID,
					Cause:     errors.New("Coinone access token and secret key are required"),
				}
			}
			nonce, nonceErr := client.nonceSource()
			if nonceErr != nil {
				return nil, nonceErr
			}
			privateBody, err = encodePrivatePayload(material.APIKey, nonce, fields)
			if err != nil {
				return nil, err
			}
			payload, signature, signErr := SignPayload(material.SecretKey, privateBody)
			if signErr != nil {
				return nil, signErr
			}
			request, requestErr := client.newRequest(http.MethodPost, path, nil, privateBody)
			if requestErr != nil {
				return nil, requestErr
			}
			request.Header.Set("X-COINONE-PAYLOAD", payload)
			request.Header.Set("X-COINONE-SIGNATURE", signature)
			return request, nil
		},
	})
	if err == nil {
		observeRateLimit(client.executor.Limiter(), limit, response.Header)
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

func (client *Client) newRequest(method, path string, query url.Values, body []byte) (*http.Request, error) {
	requestURL := *client.baseURL
	requestURL.Path = path
	requestURL.RawQuery = query.Encode()
	request, err := http.NewRequest(method, requestURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create Coinone request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "proven-trade-sdk-go/0")
	return request, nil
}

func (client *Client) decodeResponse(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	target any,
) error {
	var envelope struct {
		Result       string `json:"result"`
		ErrorCode    string `json:"error_code"`
		ErrorMessage string `json:"error_msg"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err != nil {
		return client.decodeBodyError(response, operation, err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return client.apiError(response, operation, envelope.ErrorCode, envelope.ErrorMessage)
	}
	if envelope.Result == "" || envelope.ErrorCode == "" {
		return client.decodeBodyError(response, operation, errors.New("Coinone response envelope is incomplete"))
	}
	if envelope.Result != "success" || envelope.ErrorCode != "0" {
		return client.apiError(response, operation, envelope.ErrorCode, envelope.ErrorMessage)
	}
	if target != nil {
		if err := json.Unmarshal(response.Body, target); err != nil {
			return client.decodeBodyError(response, operation, err)
		}
	}
	return nil
}

func (client *Client) apiError(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	code, message string,
) error {
	category, retryable := classifyError(response.StatusCode, code, operation)
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: category, Exchange: model.ExchangeCoinone, AccountID: accountID,
		RequestID: firstNonEmpty(response.Header.Get("X-Request-ID"), response.Header.Get("Request-ID")),
		Retryable: retryable, HTTPStatus: response.StatusCode,
		ExchangeCode: code, ExchangeMessage: message,
	}
}

func (client *Client) decodeBodyError(
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
		Category: category, Exchange: model.ExchangeCoinone, AccountID: accountID,
		Retryable: retryable, HTTPStatus: response.StatusCode,
		Cause: fmt.Errorf("decode Coinone JSON response: %w", cause),
	}
}

func classifyError(status int, code string, operation commonexchange.OperationKind) (trade.ErrorCategory, bool) {
	if status == http.StatusTooManyRequests || code == "4" {
		return trade.ErrorRateLimited, true
	}
	switch code {
	case "8", "11", "12", "23", "24", "25", "27", "28", "120", "121", "122", "123",
		"130", "131", "132", "133", "151", "1206":
		return trade.ErrorAuthentication, false
	case "10", "22", "40", "315":
		return trade.ErrorAuthorization, false
	case "103", "157":
		return trade.ErrorInsufficientBalance, false
	case "104":
		return trade.ErrorOrderNotFound, false
	case "405":
		if operation == commonexchange.OperationMutation {
			return trade.ErrorUnknownExecutionState, false
		}
		return trade.ErrorExchangeUnavailable, true
	}
	if status >= http.StatusInternalServerError {
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
	if status == http.StatusNotFound {
		return trade.ErrorOrderNotFound, false
	}
	if status >= http.StatusBadRequest || code != "" {
		return trade.ErrorValidation, false
	}
	return trade.ErrorInternal, false
}

func cloneBytes(value []byte) []byte {
	return append([]byte(nil), value...)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
