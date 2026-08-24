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
	"sync/atomic"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/credential"
	commonexchange "github.com/proven-trade/proven-trade-sdk/exchange"
	"github.com/proven-trade/proven-trade-sdk/model"
	"github.com/proven-trade/proven-trade-sdk/transport"
)

const (
	DefaultBaseURL                 = "https://futures.kraken.com"
	DefaultRequestTimeout          = 10 * time.Second
	DefaultPublicRequestsPerSecond = 20
	DefaultDerivativesPointLimit   = 500
	DefaultDerivativesWindow       = 10 * time.Second
	derivativesPrefix              = "/derivatives/api/v3/"
)

// Config는 Kraken Futures REST 클라이언트 설정이다.
type Config struct {
	Executor                *commonexchange.Executor
	Credentials             *credential.Descriptor
	CredentialProvider      credential.Provider
	DefaultEgressRouteID    transport.EgressRouteID
	BaseURL                 string
	AllowInsecureHTTP       bool
	RequestTimeout          time.Duration
	PublicRequestsPerSecond int
	DerivativesPointLimit   int
	DerivativesWindow       time.Duration
	Now                     func() time.Time
}

// Client는 Kraken Futures REST API를 요청별 EIP 선택과 함께 제공한다.
type Client struct {
	executor                *commonexchange.Executor
	credentials             *credential.Descriptor
	credentialProvider      credential.Provider
	defaultEgressRouteID    transport.EgressRouteID
	baseURL                 *url.URL
	requestTimeout          time.Duration
	publicRequestsPerSecond int
	derivativesPointLimit   int
	derivativesWindow       time.Duration
	now                     func() time.Time
	lastNonce               atomic.Uint64
}

// New는 Kraken Futures REST 클라이언트를 생성한다.
func New(config Config) (*Client, error) {
	if config.Executor == nil {
		return nil, fmt.Errorf("Kraken Futures executor is required")
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
		return nil, fmt.Errorf("invalid Kraken Futures base URL %q", baseURL)
	}
	if parsedBaseURL.Scheme != "https" && !(config.AllowInsecureHTTP && parsedBaseURL.Scheme == "http") {
		return nil, fmt.Errorf("Kraken Futures base URL must use HTTPS")
	}
	parsedBaseURL.Path = ""
	if config.RequestTimeout == 0 {
		config.RequestTimeout = DefaultRequestTimeout
	}
	if config.RequestTimeout < 0 {
		return nil, fmt.Errorf("Kraken Futures request timeout cannot be negative")
	}
	if config.PublicRequestsPerSecond == 0 {
		config.PublicRequestsPerSecond = DefaultPublicRequestsPerSecond
	}
	if config.DerivativesPointLimit == 0 {
		config.DerivativesPointLimit = DefaultDerivativesPointLimit
	}
	if config.DerivativesWindow == 0 {
		config.DerivativesWindow = DefaultDerivativesWindow
	}
	if config.PublicRequestsPerSecond < 1 || config.DerivativesPointLimit < 25 ||
		config.DerivativesWindow < time.Second {
		return nil, fmt.Errorf("Kraken Futures request limits are invalid")
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
			return nil, fmt.Errorf("credential provider is required for private Kraken Futures requests")
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
		derivativesPointLimit:   config.DerivativesPointLimit,
		derivativesWindow:       config.DerivativesWindow, now: config.Now,
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
	charges, err := publicRateLimitCharges(
		client.executor.Limiter(), resolved.EgressRouteID, client.publicRequestsPerSecond,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}
	return client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeKraken, EgressRouteID: resolved.EgressRouteID,
		Timeout: resolved.Timeout, Charges: charges, Operation: commonexchange.OperationRead,
		Build: func(context.Context) (*http.Request, error) {
			return client.newRequest(http.MethodGet, path, query)
		},
	})
}

func (client *Client) executePrivate(
	ctx context.Context,
	method, path string,
	values url.Values,
	cost int,
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
			Category: trade.ErrorAuthentication, Exchange: model.ExchangeKraken,
			Cause: errors.New("private Kraken Futures request requires credentials"),
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
	charges, err := privateRateLimitCharges(
		client.executor.Limiter(), client.credentials.AccountID,
		client.derivativesPointLimit, client.derivativesWindow, cost,
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
					Cause:     errors.New("Kraken Futures API key and Base64 secret are required"),
				}
			}
			request, requestErr := client.newRequest(method, path, values)
			if requestErr != nil {
				return nil, requestErr
			}
			nonce := client.nextNonce()
			signingPath := strings.TrimPrefix(path, "/derivatives")
			signature, signErr := SignAuthent(request.URL.RawQuery, nonce, signingPath, material.SecretKey)
			if signErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeKraken,
					AccountID: client.credentials.AccountID, Cause: signErr,
				}
			}
			request.Header.Set("APIKey", string(material.APIKey))
			request.Header.Set("Authent", signature)
			request.Header.Set("Nonce", nonce)
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

func (client *Client) newRequest(method, path string, query url.Values) (*http.Request, error) {
	requestURL := *client.baseURL
	requestURL.Path = path
	requestURL.RawQuery = cloneValues(query).Encode()
	request, err := http.NewRequest(method, requestURL.String(), bytes.NewReader(nil))
	if err != nil {
		return nil, fmt.Errorf("create Kraken Futures request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "proven-trade-sdk-go/0")
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return request, nil
}

func (client *Client) decodeSuccess(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	target any,
) error {
	var envelope struct {
		Result string          `json:"result"`
		Error  json.RawMessage `json:"error"`
		Errors json.RawMessage `json:"errors"`
	}
	decodeErr := json.Unmarshal(response.Body, &envelope)
	message := firstNonEmpty(rawMessageText(envelope.Error), rawMessageText(envelope.Errors))
	code := envelope.Result
	if code == "error" && message != "" {
		code = message
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices ||
		(envelope.Result != "" && envelope.Result != "success") || message != "" {
		return client.decodeError(response, code, message, operation, decodeErr)
	}
	if decodeErr != nil {
		return client.decodeBodyError(response, operation, decodeErr)
	}
	if err := json.Unmarshal(response.Body, target); err != nil {
		return client.decodeBodyError(response, operation, err)
	}
	return nil
}

func (client *Client) decodeError(
	response commonexchange.Response,
	code, message string,
	operation commonexchange.OperationKind,
	cause error,
) error {
	if message == "" {
		message = strings.TrimSpace(string(response.Body))
	}
	category, retryable := classifyError(response.StatusCode, code, message, operation)
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: category, Exchange: model.ExchangeKraken, AccountID: accountID,
		RequestID: response.Header.Get("X-Request-ID"), Retryable: retryable,
		HTTPStatus: response.StatusCode, ExchangeCode: code, ExchangeMessage: message, Cause: cause,
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
		Cause: fmt.Errorf("decode Kraken Futures JSON response: %w", cause),
	}
}

func classifyError(
	status int,
	code, message string,
	operation commonexchange.OperationKind,
) (trade.ErrorCategory, bool) {
	normalized := strings.ToLower(code + " " + message)
	if status == http.StatusTooManyRequests || strings.Contains(normalized, "api limit exceeded") ||
		strings.Contains(normalized, "rate limit") {
		return trade.ErrorRateLimited, true
	}
	if status == http.StatusUnauthorized || strings.Contains(normalized, "authentication") ||
		strings.Contains(normalized, "invalid api key") || strings.Contains(normalized, "invalid signature") ||
		strings.Contains(normalized, "nonce") {
		return trade.ErrorAuthentication, false
	}
	if status == http.StatusForbidden || strings.Contains(normalized, "permission") {
		return trade.ErrorAuthorization, false
	}
	if strings.Contains(normalized, "insufficient") {
		return trade.ErrorInsufficientBalance, false
	}
	if strings.Contains(normalized, "order not found") || strings.Contains(normalized, "unknown order") ||
		strings.Contains(normalized, "not found") {
		return trade.ErrorOrderNotFound, false
	}
	if status >= http.StatusInternalServerError || strings.Contains(normalized, "unavailable") ||
		strings.Contains(normalized, "server error") {
		if operation == commonexchange.OperationMutation {
			return trade.ErrorUnknownExecutionState, false
		}
		return trade.ErrorExchangeUnavailable, true
	}
	if status >= http.StatusBadRequest || code != "" || message != "" {
		return trade.ErrorValidation, false
	}
	return trade.ErrorInternal, false
}

func rawMessageText(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte(`""`)) ||
		bytes.Equal(trimmed, []byte("[]")) || bytes.Equal(trimmed, []byte("{}")) {
		return ""
	}
	var text string
	if json.Unmarshal(trimmed, &text) == nil {
		return text
	}
	var texts []string
	if json.Unmarshal(trimmed, &texts) == nil {
		return strings.Join(texts, "; ")
	}
	return string(trimmed)
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
