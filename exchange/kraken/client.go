package kraken

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

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const (
	DefaultBaseURL                  = "https://api.kraken.com"
	DefaultRequestTimeout           = 10 * time.Second
	DefaultPublicRequestsPerSecond  = 1
	DefaultPrivateCounterLimit      = 20
	DefaultPrivateCounterWindow     = 40 * time.Second
	DefaultTradingRequestsPerSecond = 1
	publicPrefix                    = "/0/public/"
	privatePrefix                   = "/0/private/"
)

// Config는 Kraken Spot REST 클라이언트 설정이다.
type Config struct {
	Executor                 *commonexchange.Executor
	Credentials              *credential.Descriptor
	CredentialProvider       credential.Provider
	DefaultEgressRouteID     transport.EgressRouteID
	BaseURL                  string
	AllowInsecureHTTP        bool
	RequestTimeout           time.Duration
	PublicRequestsPerSecond  int
	PrivateCounterLimit      int
	PrivateCounterWindow     time.Duration
	TradingRequestsPerSecond int
	Now                      func() time.Time
}

// Client는 Kraken Spot REST API를 요청별 송신 경로 선택과 함께 제공한다.
type Client struct {
	executor                 *commonexchange.Executor
	credentials              *credential.Descriptor
	credentialProvider       credential.Provider
	defaultEgressRouteID     transport.EgressRouteID
	baseURL                  *url.URL
	requestTimeout           time.Duration
	publicRequestsPerSecond  int
	privateCounterLimit      int
	privateCounterWindow     time.Duration
	tradingRequestsPerSecond int
	now                      func() time.Time
	lastNonce                atomic.Uint64
}

// New는 Kraken Spot REST 클라이언트를 생성한다.
func New(config Config) (*Client, error) {
	if config.Executor == nil {
		return nil, fmt.Errorf("Kraken executor is required")
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
		return nil, fmt.Errorf("invalid Kraken base URL %q", baseURL)
	}
	if parsedBaseURL.Scheme != "https" && !(config.AllowInsecureHTTP && parsedBaseURL.Scheme == "http") {
		return nil, fmt.Errorf("Kraken base URL must use HTTPS")
	}
	parsedBaseURL.Path = ""
	if config.RequestTimeout == 0 {
		config.RequestTimeout = DefaultRequestTimeout
	}
	if config.RequestTimeout < 0 {
		return nil, fmt.Errorf("Kraken request timeout cannot be negative")
	}
	if config.PublicRequestsPerSecond == 0 {
		config.PublicRequestsPerSecond = DefaultPublicRequestsPerSecond
	}
	if config.PrivateCounterLimit == 0 {
		config.PrivateCounterLimit = DefaultPrivateCounterLimit
	}
	if config.PrivateCounterWindow == 0 {
		config.PrivateCounterWindow = DefaultPrivateCounterWindow
	}
	if config.TradingRequestsPerSecond == 0 {
		config.TradingRequestsPerSecond = DefaultTradingRequestsPerSecond
	}
	if config.PublicRequestsPerSecond < 1 || config.PrivateCounterLimit < 4 ||
		config.PrivateCounterWindow < time.Second || config.TradingRequestsPerSecond < 1 {
		return nil, fmt.Errorf("Kraken request limits are invalid")
	}
	if config.Now == nil {
		config.Now = time.Now
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeKraken {
			return nil, fmt.Errorf("credential exchange must be Kraken")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private Kraken requests")
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
		publicRequestsPerSecond: config.PublicRequestsPerSecond,
		privateCounterLimit:     config.PrivateCounterLimit, privateCounterWindow: config.PrivateCounterWindow,
		tradingRequestsPerSecond: config.TradingRequestsPerSecond, now: config.Now,
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
	charges, err := rateLimitCharges(
		client.executor.Limiter(), resolved.EgressRouteID, "", "", limitPublic,
		client.publicRequestsPerSecond, client.privateCounterLimit,
		client.privateCounterWindow, client.tradingRequestsPerSecond,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}
	return client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeKraken, EgressRouteID: resolved.EgressRouteID,
		Timeout: resolved.Timeout, Charges: charges, Operation: commonexchange.OperationRead,
		Build: func(context.Context) (*http.Request, error) {
			return client.newRequest(http.MethodGet, path, query, nil)
		},
	})
}

func (client *Client) executePrivate(
	ctx context.Context,
	path string,
	values url.Values,
	permission credential.Permission,
	operation commonexchange.OperationKind,
	limit limitKind,
	pair string,
	options ...trade.RequestOption,
) (commonexchange.Response, error) {
	resolved, err := client.resolveOptions(options...)
	if err != nil {
		return commonexchange.Response{}, err
	}
	if client.credentials == nil || client.credentialProvider == nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthentication, Exchange: model.ExchangeKraken,
			Cause: errors.New("private Kraken request requires credentials"),
		}
	}
	if err := client.credentials.RequireEgressRoute(resolved.EgressRouteID); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeKraken,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	if err := client.credentials.RequirePermission(permission); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeKraken,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	charges, err := rateLimitCharges(
		client.executor.Limiter(), resolved.EgressRouteID, client.credentials.AccountID, pair, limit,
		client.publicRequestsPerSecond, client.privateCounterLimit,
		client.privateCounterWindow, client.tradingRequestsPerSecond,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}

	var material credential.Material
	defer material.Destroy()
	return client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeKraken, AccountID: client.credentials.AccountID,
		EgressRouteID: resolved.EgressRouteID, Timeout: resolved.Timeout,
		Charges: charges, Operation: operation,
		Build: func(buildContext context.Context) (*http.Request, error) {
			resolvedMaterial, resolveErr := client.credentialProvider.Resolve(
				buildContext, client.credentials.SecretRef,
			)
			material = resolvedMaterial
			if resolveErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeKraken,
					AccountID: client.credentials.AccountID, Cause: resolveErr,
				}
			}
			if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeKraken,
					AccountID: client.credentials.AccountID,
					Cause:     errors.New("Kraken API key and Base64 secret are required"),
				}
			}
			payloadValues := cloneValues(values)
			nonce := client.nextNonce()
			payloadValues.Set("nonce", nonce)
			payload := payloadValues.Encode()
			signature, signErr := SignREST(path, nonce, payload, material.SecretKey)
			if signErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeKraken,
					AccountID: client.credentials.AccountID, Cause: signErr,
				}
			}
			request, requestErr := client.newRequest(
				http.MethodPost, path, nil, []byte(payload),
			)
			if requestErr != nil {
				return nil, requestErr
			}
			request.Header.Set("API-Key", string(material.APIKey))
			request.Header.Set("API-Sign", signature)
			return request, nil
		},
	})
}

func (client *Client) nextNonce() string {
	candidate := uint64(client.now().UnixMilli())
	for {
		previous := client.lastNonce.Load()
		if candidate <= previous {
			candidate = previous + 1
		}
		if client.lastNonce.CompareAndSwap(previous, candidate) {
			return strconv.FormatUint(candidate, 10)
		}
	}
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
	method, path string,
	query url.Values,
	body []byte,
) (*http.Request, error) {
	requestURL := *client.baseURL
	requestURL.Path = path
	requestURL.RawQuery = cloneValues(query).Encode()
	request, err := http.NewRequest(method, requestURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create Kraken request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "proven-trade-sdk-go/0")
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return request, nil
}

func (client *Client) decodeResult(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
) (json.RawMessage, error) {
	var envelope struct {
		Errors []string        `json:"error"`
		Result json.RawMessage `json:"result"`
	}
	decodeErr := json.Unmarshal(response.Body, &envelope)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices ||
		len(envelope.Errors) > 0 {
		return nil, client.decodeErrorResponse(response, operation, envelope.Errors, decodeErr)
	}
	if decodeErr != nil {
		return nil, client.decodeBodyError(response, operation, decodeErr)
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return nil, client.decodeBodyError(response, operation, errors.New("Kraken result is empty"))
	}
	return cloneBytes(envelope.Result), nil
}

func (client *Client) decodeErrorResponse(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	exchangeErrors []string,
	cause error,
) error {
	message := strings.Join(exchangeErrors, "; ")
	if message == "" {
		message = strings.TrimSpace(string(response.Body))
	}
	code := ""
	if len(exchangeErrors) > 0 {
		code = exchangeErrors[0]
	}
	category, retryable := classifyError(response.StatusCode, message, operation)
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: category, Exchange: model.ExchangeKraken, AccountID: accountID,
		Retryable: retryable, HTTPStatus: response.StatusCode,
		ExchangeCode: code, ExchangeMessage: message, Cause: cause,
	}
}

func (client *Client) decodeBodyError(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	cause error,
) error {
	category := trade.ErrorExchangeUnavailable
	retryable := operation == commonexchange.OperationRead
	if operation == commonexchange.OperationMutation {
		category, retryable = trade.ErrorUnknownExecutionState, false
	}
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: category, Exchange: model.ExchangeKraken, AccountID: accountID,
		Retryable: retryable, HTTPStatus: response.StatusCode,
		Cause: fmt.Errorf("decode Kraken JSON response: %w", cause),
	}
}

func classifyError(
	status int,
	message string,
	operation commonexchange.OperationKind,
) (trade.ErrorCategory, bool) {
	normalized := strings.ToLower(message)
	if status == http.StatusTooManyRequests || strings.Contains(normalized, "rate limit") ||
		strings.Contains(normalized, "throttled") {
		return trade.ErrorRateLimited, true
	}
	if status == http.StatusUnauthorized || strings.Contains(normalized, "invalid key") ||
		strings.Contains(normalized, "invalid signature") || strings.Contains(normalized, "invalid nonce") {
		return trade.ErrorAuthentication, false
	}
	if status == http.StatusForbidden || strings.Contains(normalized, "permission denied") {
		return trade.ErrorAuthorization, false
	}
	if strings.Contains(normalized, "insufficient funds") {
		return trade.ErrorInsufficientBalance, false
	}
	if strings.Contains(normalized, "unknown order") || strings.Contains(normalized, "order not found") {
		return trade.ErrorOrderNotFound, false
	}
	if status >= http.StatusInternalServerError || strings.Contains(normalized, "service:unavailable") ||
		strings.Contains(normalized, "service unavailable") {
		if operation == commonexchange.OperationMutation {
			return trade.ErrorUnknownExecutionState, false
		}
		return trade.ErrorExchangeUnavailable, true
	}
	if status >= http.StatusBadRequest || message != "" {
		return trade.ErrorValidation, false
	}
	return trade.ErrorInternal, false
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
