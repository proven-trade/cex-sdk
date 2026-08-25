package okx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const (
	DefaultBaseURL        = "https://www.okx.com"
	DefaultRequestTimeout = 10 * time.Second
	okxTimestampLayout    = "2006-01-02T15:04:05.000Z"
)

// Config는 OKX V5 Spot·SWAP REST 클라이언트 설정이다.
type Config struct {
	Executor             *commonexchange.Executor
	Credentials          *credential.Descriptor
	CredentialProvider   credential.Provider
	DefaultEgressRouteID transport.EgressRouteID
	BaseURL              string
	AllowInsecureHTTP    bool
	RequestTimeout       time.Duration
	DemoTrading          bool
	Now                  func() time.Time
}

// Client는 OKX V5 Spot·SWAP REST API를 요청별 송신 경로 선택과 함께 제공한다.
type Client struct {
	executor             *commonexchange.Executor
	credentials          *credential.Descriptor
	credentialProvider   credential.Provider
	defaultEgressRouteID transport.EgressRouteID
	baseURL              *url.URL
	requestTimeout       time.Duration
	demoTrading          bool
	now                  func() time.Time
	clockOffsetMillis    atomic.Int64
}

// New는 OKX V5 REST 클라이언트를 생성한다.
func New(config Config) (*Client, error) {
	if config.Executor == nil {
		return nil, fmt.Errorf("OKX executor is required")
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
		return nil, fmt.Errorf("invalid OKX base URL %q", baseURL)
	}
	if parsedBaseURL.Scheme != "https" && !(config.AllowInsecureHTTP && parsedBaseURL.Scheme == "http") {
		return nil, fmt.Errorf("OKX base URL must use HTTPS")
	}
	parsedBaseURL.Path = ""
	if config.RequestTimeout == 0 {
		config.RequestTimeout = DefaultRequestTimeout
	}
	if config.RequestTimeout < 0 {
		return nil, fmt.Errorf("OKX request timeout cannot be negative")
	}
	if config.Now == nil {
		config.Now = time.Now
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeOKX {
			return nil, fmt.Errorf("credential exchange must be OKX")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private OKX requests")
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
		executor:             config.Executor,
		credentials:          credentialsCopy,
		credentialProvider:   config.CredentialProvider,
		defaultEgressRouteID: defaultRouteID,
		baseURL:              parsedBaseURL,
		requestTimeout:       config.RequestTimeout,
		demoTrading:          config.DemoTrading,
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
	charges, err := rateLimitCharges(
		client.executor.Limiter(), resolved.EgressRouteID, "", method, path, limit,
	)
	if err != nil {
		return commonexchange.Response{}, "", err
	}
	response, err := client.executor.Execute(ctx, commonexchange.Execution{
		Exchange:      model.ExchangeOKX,
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
			Category: trade.ErrorAuthentication, Exchange: model.ExchangeOKX,
			Cause: errors.New("private OKX request requires credentials"),
		}
	}
	if err := client.credentials.RequireEgressRoute(resolved.EgressRouteID); err != nil {
		return commonexchange.Response{}, "", &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeOKX,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	if err := client.credentials.RequirePermission(permission); err != nil {
		return commonexchange.Response{}, "", &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeOKX,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	charges, err := rateLimitCharges(
		client.executor.Limiter(), resolved.EgressRouteID, client.credentials.AccountID, method, path, limit,
	)
	if err != nil {
		return commonexchange.Response{}, "", err
	}

	var material credential.Material
	defer material.Destroy()
	response, err := client.executor.Execute(ctx, commonexchange.Execution{
		Exchange:      model.ExchangeOKX,
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
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeOKX,
					AccountID: client.credentials.AccountID, Cause: resolveErr,
				}
			}
			if len(material.APIKey) == 0 || len(material.SecretKey) == 0 || len(material.Passphrase) == 0 {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeOKX,
					AccountID: client.credentials.AccountID,
					Cause:     errors.New("OKX API key, HMAC secret, and passphrase are required"),
				}
			}
			request, requestErr := client.newRequest(method, path, query, body)
			if requestErr != nil {
				return nil, requestErr
			}
			timestampMillis := client.now().UnixMilli() + client.clockOffsetMillis.Load()
			timestamp := time.UnixMilli(timestampMillis).UTC().Format(okxTimestampLayout)
			requestPath := request.URL.EscapedPath()
			if request.URL.RawQuery != "" {
				requestPath += "?" + request.URL.RawQuery
			}
			signature, signErr := SignHMACSHA256(
				material.SecretKey, signaturePayload(timestamp, method, requestPath, body),
			)
			if signErr != nil {
				return nil, signErr
			}
			request.Header.Set("OK-ACCESS-KEY", string(material.APIKey))
			request.Header.Set("OK-ACCESS-SIGN", signature)
			request.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
			request.Header.Set("OK-ACCESS-PASSPHRASE", string(material.Passphrase))
			return request, nil
		},
	})
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
		return nil, fmt.Errorf("create OKX request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "proven-trade-sdk-go/0")
	if client.demoTrading {
		request.Header.Set("x-simulated-trading", "1")
	}
	return request, nil
}

func (client *Client) responseData(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
) (json.RawMessage, error) {
	var envelope struct {
		Code    string          `json:"code"`
		Message string          `json:"msg"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err != nil {
		return nil, client.decodeBodyError(response, operation, err)
	}
	if envelope.Code == "" {
		return nil, client.decodeBodyError(
			response, operation, errors.New("OKX response code is missing"),
		)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices ||
		envelope.Code != "0" {
		return nil, client.decodeError(response, envelope.Code, envelope.Message, operation, nil)
	}
	return cloneBytes(envelope.Data), nil
}

func (client *Client) decodeError(
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
		Category: category, Exchange: model.ExchangeOKX, AccountID: accountID,
		RequestID: firstNonEmpty(response.Header.Get("OK-ACCESS-TRACE"), response.Header.Get("X-Request-ID")),
		Retryable: retryable, HTTPStatus: response.StatusCode, ExchangeCode: code,
		ExchangeMessage: message, Cause: cause,
	}
}

func (client *Client) decodeBodyError(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	cause error,
) error {
	category, retryable := classifyError(response.StatusCode, "", operation)
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
		Category: category, Exchange: model.ExchangeOKX, AccountID: accountID,
		Retryable: retryable, HTTPStatus: response.StatusCode,
		Cause: fmt.Errorf("decode OKX JSON response: %w", cause),
	}
}

func classifyError(
	status int,
	code string,
	operation commonexchange.OperationKind,
) (trade.ErrorCategory, bool) {
	if status == http.StatusTooManyRequests || code == "50011" || code == "50040" || code == "50061" {
		return trade.ErrorRateLimited, true
	}
	switch code {
	case "50102", "50103", "50104", "50105", "50106", "50111", "50112", "50113", "50119":
		return trade.ErrorAuthentication, false
	case "50114", "50120":
		return trade.ErrorAuthorization, false
	case "51400", "51401", "51603":
		return trade.ErrorOrderNotFound, false
	case "51008", "51119", "51131":
		return trade.ErrorInsufficientBalance, false
	case "50001", "50004", "50013", "50026", "51149":
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
		return nil, fmt.Errorf("encode OKX JSON request: %w", err)
	}
	return body, nil
}

func decodeData(raw json.RawMessage, target any) error {
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode OKX response data: %w", err)
	}
	return nil
}

func decodeItems[T any](raw json.RawMessage, setRaw func(*T, []byte)) ([]T, error) {
	var rawItems []json.RawMessage
	if err := decodeData(raw, &rawItems); err != nil {
		return nil, err
	}
	items := make([]T, len(rawItems))
	for index, rawItem := range rawItems {
		if err := decodeData(rawItem, &items[index]); err != nil {
			return nil, err
		}
		if setRaw != nil {
			setRaw(&items[index], cloneBytes(rawItem))
		}
	}
	return items, nil
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
