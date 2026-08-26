package bybit

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
	"sync/atomic"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/transport"
)

const (
	DefaultBaseURL        = "https://api.bybit.com"
	DefaultTestnetBaseURL = "https://api-testnet.bybit.com"
	DefaultRequestTimeout = 10 * time.Second
	DefaultReceiveWindow  = 5 * time.Second
)

// Config는 Bybit V5 Spot·Linear REST 클라이언트 설정이다.
type Config struct {
	Executor             *commonexchange.Executor
	Credentials          *credential.Descriptor
	CredentialProvider   credential.Provider
	DefaultEgressRouteID transport.EgressRouteID
	BaseURL              string
	Testnet              bool
	AllowInsecureHTTP    bool
	RequestTimeout       time.Duration
	ReceiveWindow        time.Duration
	Now                  func() time.Time
}

// Client는 Bybit V5 Spot·Linear REST API를 요청별 송신 경로 선택과 함께 제공한다.
type Client struct {
	executor             *commonexchange.Executor
	credentials          *credential.Descriptor
	credentialProvider   credential.Provider
	defaultEgressRouteID transport.EgressRouteID
	baseURL              *url.URL
	requestTimeout       time.Duration
	receiveWindowMillis  int64
	now                  func() time.Time
	clockOffsetMillis    atomic.Int64
}

// New는 Bybit V5 REST 클라이언트를 생성한다.
func New(config Config) (*Client, error) {
	if config.Executor == nil {
		return nil, fmt.Errorf("Bybit executor is required")
	}
	defaultRouteID := transport.EgressRouteID(strings.TrimSpace(string(config.DefaultEgressRouteID)))
	if defaultRouteID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
		if config.Testnet {
			baseURL = DefaultTestnetBaseURL
		}
	}
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil || parsedBaseURL.Host == "" || parsedBaseURL.User != nil ||
		parsedBaseURL.RawQuery != "" || parsedBaseURL.Fragment != "" ||
		(parsedBaseURL.Path != "" && parsedBaseURL.Path != "/") {
		return nil, fmt.Errorf("invalid Bybit base URL %q", baseURL)
	}
	if parsedBaseURL.Scheme != "https" && !(config.AllowInsecureHTTP && parsedBaseURL.Scheme == "http") {
		return nil, fmt.Errorf("Bybit base URL must use HTTPS")
	}
	parsedBaseURL.Path = ""
	if config.RequestTimeout == 0 {
		config.RequestTimeout = DefaultRequestTimeout
	}
	if config.RequestTimeout < 0 {
		return nil, fmt.Errorf("Bybit request timeout cannot be negative")
	}
	if config.ReceiveWindow == 0 {
		config.ReceiveWindow = DefaultReceiveWindow
	}
	if config.ReceiveWindow <= 0 || config.ReceiveWindow > 60*time.Second ||
		config.ReceiveWindow%time.Millisecond != 0 {
		return nil, fmt.Errorf("Bybit receive window must be 1-60000 whole milliseconds")
	}
	if config.Now == nil {
		config.Now = time.Now
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeBybit {
			return nil, fmt.Errorf("credential exchange must be Bybit")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private Bybit requests")
		}
		copyValue := *config.Credentials
		copyValue.Permissions = append([]credential.Permission(nil), config.Credentials.Permissions...)
		copyValue.AllowedEgressRouteIDs = append(
			[]transport.EgressRouteID(nil),
			config.Credentials.AllowedEgressRouteIDs...,
		)
		credentialsCopy = &copyValue
	}
	if config.Credentials == nil && config.CredentialProvider != nil {
		return nil, fmt.Errorf("credential descriptor is required with credential provider")
	}

	return &Client{
		executor:             config.Executor,
		credentials:          credentialsCopy,
		credentialProvider:   config.CredentialProvider,
		defaultEgressRouteID: defaultRouteID,
		baseURL:              parsedBaseURL,
		requestTimeout:       config.RequestTimeout,
		receiveWindowMillis:  config.ReceiveWindow.Milliseconds(),
		now:                  config.Now,
	}, nil
}

func (client *Client) executePublic(
	ctx context.Context,
	method, path string,
	query url.Values,
	limit endpointLimit,
	options ...trade.RequestOption,
) (commonexchange.Response, transport.EgressRouteID, error) {
	return client.executePublicWithBuildHook(ctx, method, path, query, limit, nil, options...)
}

func (client *Client) executePublicWithBuildHook(
	ctx context.Context,
	method, path string,
	query url.Values,
	limit endpointLimit,
	beforeBuild func(),
	options ...trade.RequestOption,
) (commonexchange.Response, transport.EgressRouteID, error) {
	resolved, err := client.resolveOptions(options...)
	if err != nil {
		return commonexchange.Response{}, "", err
	}
	charges, err := rateLimitCharges(client.executor.Limiter(), resolved.EgressRouteID, "", path, limit)
	if err != nil {
		return commonexchange.Response{}, "", err
	}
	response, err := client.executor.Execute(ctx, commonexchange.Execution{
		Exchange:      model.ExchangeBybit,
		EgressRouteID: resolved.EgressRouteID,
		Timeout:       resolved.Timeout,
		Charges:       charges,
		Operation:     commonexchange.OperationRead,
		Build: func(context.Context) (*http.Request, error) {
			if beforeBuild != nil {
				beforeBuild()
			}
			return client.newRequest(method, path, query, nil)
		},
	})
	if err == nil {
		observeRemaining(
			client.executor.Limiter(), "route", string(resolved.EgressRouteID), path, limit, response.Header,
		)
	}
	return response, resolved.EgressRouteID, err
}

func (client *Client) executeSigned(
	ctx context.Context,
	method, path string,
	query url.Values,
	body []byte,
	limit endpointLimit,
	permission credential.Permission,
	operation commonexchange.OperationKind,
	options ...trade.RequestOption,
) (commonexchange.Response, transport.EgressRouteID, error) {
	resolved, err := client.resolveOptions(options...)
	if err != nil {
		return commonexchange.Response{}, "", err
	}
	if client.credentials == nil || client.credentialProvider == nil {
		return commonexchange.Response{}, "", &trade.APIError{
			Category: trade.ErrorAuthentication,
			Exchange: model.ExchangeBybit,
			Cause:    errors.New("private Bybit request requires credentials"),
		}
	}
	if err := client.credentials.RequireEgressRoute(resolved.EgressRouteID); err != nil {
		return commonexchange.Response{}, "", &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeBybit,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	if err := client.credentials.RequirePermission(permission); err != nil {
		return commonexchange.Response{}, "", &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeBybit,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	charges, err := rateLimitCharges(
		client.executor.Limiter(), resolved.EgressRouteID, client.credentials.AccountID, path, limit,
	)
	if err != nil {
		return commonexchange.Response{}, "", err
	}

	var material credential.Material
	defer material.Destroy()
	response, err := client.executor.Execute(ctx, commonexchange.Execution{
		Exchange:      model.ExchangeBybit,
		AccountID:     client.credentials.AccountID,
		EgressRouteID: resolved.EgressRouteID,
		Timeout:       resolved.Timeout,
		Charges:       charges,
		Operation:     operation,
		Build: func(buildContext context.Context) (*http.Request, error) {
			resolvedMaterial, resolveErr := client.credentialProvider.Resolve(
				buildContext, client.credentials.SecretRef,
			)
			material = resolvedMaterial
			if resolveErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeBybit,
					AccountID: client.credentials.AccountID, Cause: resolveErr,
				}
			}
			if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeBybit,
					AccountID: client.credentials.AccountID,
					Cause:     errors.New("Bybit API key and HMAC secret are required"),
				}
			}

			request, requestErr := client.newRequest(method, path, query, body)
			if requestErr != nil {
				return nil, requestErr
			}
			timestampMillis := client.now().UnixMilli() + client.clockOffsetMillis.Load()
			timestamp := strconv.FormatInt(timestampMillis, 10)
			content := body
			if method == http.MethodGet {
				content = []byte(request.URL.RawQuery)
			}
			signature, signErr := SignHMACSHA256(
				material.SecretKey,
				signaturePayload(timestamp, material.APIKey, client.receiveWindowMillis, content),
			)
			if signErr != nil {
				return nil, signErr
			}
			request.Header.Set("X-BAPI-API-KEY", string(material.APIKey))
			request.Header.Set("X-BAPI-TIMESTAMP", timestamp)
			request.Header.Set("X-BAPI-RECV-WINDOW", strconv.FormatInt(client.receiveWindowMillis, 10))
			request.Header.Set("X-BAPI-SIGN", signature)
			return request, nil
		},
	})
	if err == nil {
		observeRemaining(
			client.executor.Limiter(), "account", client.credentials.AccountID, path, limit, response.Header,
		)
	}
	return response, resolved.EgressRouteID, err
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
	requestURL.RawQuery = cloneValues(query).Encode()
	request, err := http.NewRequest(method, requestURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create Bybit request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "cex-sdk-go/0")
	return request, nil
}

func (client *Client) responseResult(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
) (json.RawMessage, int64, error) {
	var envelope struct {
		ReturnCode    int             `json:"retCode"`
		ReturnMessage string          `json:"retMsg"`
		Result        json.RawMessage `json:"result"`
		Time          int64           `json:"time"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err != nil {
		return nil, 0, client.decodeBodyError(response, operation, err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices ||
		envelope.ReturnCode != 0 {
		return nil, envelope.Time, client.decodeError(
			response, strconv.Itoa(envelope.ReturnCode), envelope.ReturnMessage, operation, nil,
		)
	}
	return cloneBytes(envelope.Result), envelope.Time, nil
}

func (client *Client) decodeError(
	response commonexchange.Response,
	code, message string,
	operation commonexchange.OperationKind,
	cause error,
) error {
	if code == "0" {
		code = ""
	}
	category, retryable := classifyError(response.StatusCode, code, message, operation)
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: category, Exchange: model.ExchangeBybit, AccountID: accountID,
		RequestID: firstNonEmpty(response.Header.Get("Traceid"), response.Header.Get("X-Request-ID")),
		Retryable: retryable, HTTPStatus: response.StatusCode, ExchangeCode: code,
		ExchangeMessage: message, Cause: cause,
	}
}

func (client *Client) decodeBodyError(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	cause error,
) error {
	category, retryable := classifyError(response.StatusCode, "", string(response.Body), operation)
	if category == trade.ErrorInternal {
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
		Category: category, Exchange: model.ExchangeBybit, AccountID: accountID,
		Retryable: retryable, HTTPStatus: response.StatusCode,
		Cause: fmt.Errorf("decode Bybit JSON response: %w", cause),
	}
}

func classifyError(
	status int,
	code, message string,
	operation commonexchange.OperationKind,
) (trade.ErrorCategory, bool) {
	if status == http.StatusTooManyRequests || code == "10006" ||
		(status == http.StatusForbidden && strings.Contains(strings.ToLower(message), "too frequent")) {
		return trade.ErrorRateLimited, true
	}
	switch code {
	case "10003", "10004", "33004":
		return trade.ErrorAuthentication, false
	case "10005":
		return trade.ErrorAuthorization, false
	case "110001":
		return trade.ErrorOrderNotFound, false
	case "110004", "110007", "110012":
		return trade.ErrorInsufficientBalance, false
	case "10000", "10016":
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
	if status >= http.StatusBadRequest && status < http.StatusInternalServerError || code != "" {
		return trade.ErrorValidation, false
	}
	return trade.ErrorInternal, false
}

func encodeBody(value any) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode Bybit JSON request: %w", err)
	}
	return body, nil
}

func decodeResult(raw json.RawMessage, target any) error {
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode Bybit response result: %w", err)
	}
	return nil
}

func cloneValues(values url.Values) url.Values {
	cloned := make(url.Values, len(values))
	for key, items := range values {
		cloned[key] = append([]string(nil), items...)
	}
	return cloned
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
