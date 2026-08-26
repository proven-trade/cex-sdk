package coinbase

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/transport"
)

const (
	DefaultBaseURL                  = "https://api.coinbase.com"
	DefaultRequestTimeout           = 10 * time.Second
	DefaultPublicRequestsPerSecond  = 10
	DefaultPrivateRequestsPerSecond = 10
)

// Config는 Coinbase Advanced Trade Spot REST 클라이언트 설정이다.
type Config struct {
	Executor                 *commonexchange.Executor
	Credentials              *credential.Descriptor
	CredentialProvider       credential.Provider
	DefaultEgressRouteID     transport.EgressRouteID
	BaseURL                  string
	AllowInsecureHTTP        bool
	RequestTimeout           time.Duration
	PublicRequestsPerSecond  int
	PrivateRequestsPerSecond int
	Now                      func() time.Time
	Random                   io.Reader
}

// Client는 Coinbase Advanced Trade Spot API를 요청별 송신 경로 선택과 함께 제공한다.
type Client struct {
	executor                 *commonexchange.Executor
	credentials              *credential.Descriptor
	credentialProvider       credential.Provider
	defaultEgressRouteID     transport.EgressRouteID
	baseURL                  *url.URL
	requestTimeout           time.Duration
	publicRequestsPerSecond  int
	privateRequestsPerSecond int
	now                      func() time.Time
	random                   io.Reader
	randomMu                 sync.Mutex
}

// New는 Coinbase Advanced Trade REST 클라이언트를 생성한다.
func New(config Config) (*Client, error) {
	if config.Executor == nil {
		return nil, fmt.Errorf("Coinbase executor is required")
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
		return nil, fmt.Errorf("invalid Coinbase base URL %q", baseURL)
	}
	if parsedBaseURL.Scheme != "https" && !(config.AllowInsecureHTTP && parsedBaseURL.Scheme == "http") {
		return nil, fmt.Errorf("Coinbase base URL must use HTTPS")
	}
	parsedBaseURL.Path = ""
	if config.RequestTimeout == 0 {
		config.RequestTimeout = DefaultRequestTimeout
	}
	if config.RequestTimeout < 0 {
		return nil, fmt.Errorf("Coinbase request timeout cannot be negative")
	}
	if config.PublicRequestsPerSecond == 0 {
		config.PublicRequestsPerSecond = DefaultPublicRequestsPerSecond
	}
	if config.PrivateRequestsPerSecond == 0 {
		config.PrivateRequestsPerSecond = DefaultPrivateRequestsPerSecond
	}
	if config.PublicRequestsPerSecond < 1 || config.PrivateRequestsPerSecond < 1 {
		return nil, fmt.Errorf("Coinbase request limits must be positive")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeCoinbase {
			return nil, fmt.Errorf("credential exchange must be Coinbase")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private Coinbase requests")
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
		publicRequestsPerSecond:  config.PublicRequestsPerSecond,
		privateRequestsPerSecond: config.PrivateRequestsPerSecond,
		now:                      config.Now, random: config.Random,
	}, nil
}

func (client *Client) executePublic(
	ctx context.Context,
	method, path string,
	query url.Values,
	options ...trade.RequestOption,
) (commonexchange.Response, error) {
	resolved, err := client.resolveOptions(options...)
	if err != nil {
		return commonexchange.Response{}, err
	}
	charges, err := rateLimitCharges(
		client.executor.Limiter(), resolved.EgressRouteID, "", false,
		client.publicRequestsPerSecond, client.privateRequestsPerSecond,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}
	return client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeCoinbase, EgressRouteID: resolved.EgressRouteID,
		Timeout: resolved.Timeout, Charges: charges, Operation: commonexchange.OperationRead,
		Build: func(context.Context) (*http.Request, error) {
			request, requestErr := client.newRequest(method, path, query, nil)
			if requestErr == nil {
				request.Header.Set("Cache-Control", "no-cache")
			}
			return request, requestErr
		},
	})
}

func (client *Client) executePrivate(
	ctx context.Context,
	method, path string,
	query url.Values,
	body []byte,
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
			Category: trade.ErrorAuthentication, Exchange: model.ExchangeCoinbase,
			Cause: errors.New("private Coinbase request requires credentials"),
		}
	}
	if err := client.credentials.RequireEgressRoute(resolved.EgressRouteID); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeCoinbase,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	if err := client.credentials.RequirePermission(permission); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeCoinbase,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	charges, err := rateLimitCharges(
		client.executor.Limiter(), resolved.EgressRouteID, client.credentials.AccountID, true,
		client.publicRequestsPerSecond, client.privateRequestsPerSecond,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}

	var material credential.Material
	defer material.Destroy()
	return client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeCoinbase, AccountID: client.credentials.AccountID,
		EgressRouteID: resolved.EgressRouteID, Timeout: resolved.Timeout,
		Charges: charges, Operation: operation,
		Build: func(buildContext context.Context) (*http.Request, error) {
			resolvedMaterial, resolveErr := client.credentialProvider.Resolve(
				buildContext, client.credentials.SecretRef,
			)
			material = resolvedMaterial
			if resolveErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeCoinbase,
					AccountID: client.credentials.AccountID, Cause: resolveErr,
				}
			}
			if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeCoinbase,
					AccountID: client.credentials.AccountID,
					Cause:     errors.New("Coinbase API key name and EC private key are required"),
				}
			}
			request, requestErr := client.newRequest(method, path, query, body)
			if requestErr != nil {
				return nil, requestErr
			}
			client.randomMu.Lock()
			token, signErr := SignRESTJWT(
				string(material.APIKey), method, client.baseURL.Host, path,
				material.SecretKey, client.now(), client.random,
			)
			client.randomMu.Unlock()
			if signErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeCoinbase,
					AccountID: client.credentials.AccountID, Cause: signErr,
				}
			}
			request.Header.Set("Authorization", "Bearer "+token)
			return request, nil
		},
	})
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
		return nil, fmt.Errorf("create Coinbase request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "cex-sdk-go/0")
	return request, nil
}

func (client *Client) decodeSuccess(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	target any,
) error {
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return client.decodeErrorResponse(response, operation, nil)
	}
	if err := json.Unmarshal(response.Body, target); err != nil {
		return client.decodeBodyError(response, operation, err)
	}
	return nil
}

func (client *Client) decodeErrorResponse(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	cause error,
) error {
	var envelope struct {
		Error        string `json:"error"`
		ErrorType    string `json:"errorType"`
		Code         any    `json:"code"`
		Message      string `json:"message"`
		ErrorMessage string `json:"errorMessage"`
		Details      string `json:"error_details"`
	}
	_ = json.Unmarshal(response.Body, &envelope)
	code := firstNonEmpty(envelope.Error, envelope.ErrorType)
	if code == "" && envelope.Code != nil {
		code = fmt.Sprint(envelope.Code)
	}
	message := firstNonEmpty(envelope.Message, envelope.ErrorMessage, envelope.Details)
	if message == "" {
		message = strings.TrimSpace(string(response.Body))
	}
	category, retryable := classifyError(response.StatusCode, code, message, operation)
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: category, Exchange: model.ExchangeCoinbase, AccountID: accountID,
		RequestID: firstNonEmpty(response.Header.Get("X-Request-ID"), response.Header.Get("Trace-ID")),
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
		Category: category, Exchange: model.ExchangeCoinbase, AccountID: accountID,
		Retryable: retryable, HTTPStatus: response.StatusCode,
		Cause: fmt.Errorf("decode Coinbase JSON response: %w", cause),
	}
}

func classifyError(
	status int,
	code, message string,
	operation commonexchange.OperationKind,
) (trade.ErrorCategory, bool) {
	normalized := strings.ToLower(code + " " + message)
	if status == http.StatusTooManyRequests || strings.Contains(normalized, "rate limit") ||
		strings.Contains(normalized, "resource_exhausted") {
		return trade.ErrorRateLimited, true
	}
	if status == http.StatusUnauthorized || strings.Contains(normalized, "unauthenticated") ||
		strings.Contains(normalized, "invalid signature") {
		return trade.ErrorAuthentication, false
	}
	if status == http.StatusForbidden || strings.Contains(normalized, "permission_denied") {
		return trade.ErrorAuthorization, false
	}
	if strings.Contains(normalized, "insufficient") {
		return trade.ErrorInsufficientBalance, false
	}
	if strings.Contains(normalized, "order_not_found") || strings.Contains(normalized, "order not found") {
		return trade.ErrorOrderNotFound, false
	}
	if status >= http.StatusInternalServerError {
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

func encodeBody(value any) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode Coinbase JSON request: %w", err)
	}
	return body, nil
}

func escapePathSegment(name, value string) (string, error) {
	if err := validateRequiredText(name, value); err != nil {
		return "", err
	}
	if !pathSegmentPattern.MatchString(value) {
		return "", validationError("%s contains unsupported path characters", name)
	}
	return value, nil
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
