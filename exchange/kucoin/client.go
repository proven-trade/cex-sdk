package kucoin

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
	DefaultBaseURL         = "https://api.kucoin.com"
	DefaultRequestTimeout  = 10 * time.Second
	DefaultAPIKeyVersion   = "2"
	DefaultPublicQuota     = 2000
	DefaultSpotQuota       = 4000
	DefaultManagementQuota = 2000
)

// Config는 KuCoin Classic Spot REST 클라이언트 설정이다.
type Config struct {
	Executor             *commonexchange.Executor
	Credentials          *credential.Descriptor
	CredentialProvider   credential.Provider
	DefaultEgressRouteID transport.EgressRouteID
	BaseURL              string
	AllowInsecureHTTP    bool
	RequestTimeout       time.Duration
	APIKeyVersion        string
	PublicQuota          int
	SpotQuota            int
	ManagementQuota      int
	Now                  func() time.Time
}

// Client는 KuCoin Classic Spot REST API를 요청별 송신 경로 선택과 함께 제공한다.
type Client struct {
	executor             *commonexchange.Executor
	credentials          *credential.Descriptor
	credentialProvider   credential.Provider
	defaultEgressRouteID transport.EgressRouteID
	baseURL              *url.URL
	requestTimeout       time.Duration
	apiKeyVersion        string
	publicQuota          int
	spotQuota            int
	managementQuota      int
	now                  func() time.Time
}

// New는 KuCoin Classic Spot REST 클라이언트를 생성한다.
func New(config Config) (*Client, error) {
	if config.Executor == nil {
		return nil, fmt.Errorf("KuCoin executor is required")
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
		return nil, fmt.Errorf("invalid KuCoin base URL %q", baseURL)
	}
	if parsedBaseURL.Scheme != "https" && !(config.AllowInsecureHTTP && parsedBaseURL.Scheme == "http") {
		return nil, fmt.Errorf("KuCoin base URL must use HTTPS")
	}
	parsedBaseURL.Path = ""
	if config.RequestTimeout == 0 {
		config.RequestTimeout = DefaultRequestTimeout
	}
	if config.RequestTimeout < 0 {
		return nil, fmt.Errorf("KuCoin request timeout cannot be negative")
	}
	if config.APIKeyVersion == "" {
		config.APIKeyVersion = DefaultAPIKeyVersion
	}
	if config.APIKeyVersion != "1" && config.APIKeyVersion != "2" {
		return nil, fmt.Errorf("KuCoin API key version must be 1 or 2")
	}
	if config.PublicQuota == 0 {
		config.PublicQuota = DefaultPublicQuota
	}
	if config.SpotQuota == 0 {
		config.SpotQuota = DefaultSpotQuota
	}
	if config.ManagementQuota == 0 {
		config.ManagementQuota = DefaultManagementQuota
	}
	if config.PublicQuota < 1 || config.SpotQuota < 1 || config.ManagementQuota < 1 {
		return nil, fmt.Errorf("KuCoin request quotas must be positive")
	}
	if config.Now == nil {
		config.Now = time.Now
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeKuCoin {
			return nil, fmt.Errorf("credential exchange must be KuCoin")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private KuCoin requests")
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
		apiKeyVersion: config.APIKeyVersion, publicQuota: config.PublicQuota,
		spotQuota: config.SpotQuota, managementQuota: config.ManagementQuota,
		now: config.Now,
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
		client.publicQuota, client.spotQuota, client.managementQuota,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}
	response, err := client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeKuCoin, EgressRouteID: resolved.EgressRouteID,
		Timeout: resolved.Timeout, Charges: charges, Operation: commonexchange.OperationRead,
		Build: func(context.Context) (*http.Request, error) {
			return client.newRequest(method, path, query, nil)
		},
	})
	if err == nil {
		observeRateLimit(client.executor.Limiter(), registered, response.Header)
	}
	return response, err
}

func (client *Client) executePrivate(
	ctx context.Context,
	method, path string,
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
			Category: trade.ErrorAuthentication, Exchange: model.ExchangeKuCoin,
			Cause: errors.New("private KuCoin request requires credentials"),
		}
	}
	if err := client.credentials.RequireEgressRoute(resolved.EgressRouteID); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeKuCoin,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	if err := client.credentials.RequirePermission(permission); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeKuCoin,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	registered, charges, err := rateLimitCharges(
		client.executor.Limiter(), resolved.EgressRouteID, client.credentials.AccountID, limit,
		client.publicQuota, client.spotQuota, client.managementQuota,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}

	var material credential.Material
	defer material.Destroy()
	response, err := client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeKuCoin, AccountID: client.credentials.AccountID,
		EgressRouteID: resolved.EgressRouteID, Timeout: resolved.Timeout,
		Charges: charges, Operation: operation,
		Build: func(buildContext context.Context) (*http.Request, error) {
			resolvedMaterial, resolveErr := client.credentialProvider.Resolve(
				buildContext, client.credentials.SecretRef,
			)
			material = resolvedMaterial
			if resolveErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeKuCoin,
					AccountID: client.credentials.AccountID, Cause: resolveErr,
				}
			}
			if len(material.APIKey) == 0 || len(material.SecretKey) == 0 || len(material.Passphrase) == 0 {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeKuCoin,
					AccountID: client.credentials.AccountID,
					Cause:     errors.New("KuCoin API key, HMAC secret, and passphrase are required"),
				}
			}
			request, requestErr := client.newRequest(method, path, query, body)
			if requestErr != nil {
				return nil, requestErr
			}
			timestamp := strconv.FormatInt(client.now().UnixMilli(), 10)
			if timestamp == "0" || strings.HasPrefix(timestamp, "-") {
				return nil, validationError("KuCoin timestamp must be after the Unix epoch")
			}
			endpoint := request.URL.EscapedPath()
			if request.URL.RawQuery != "" {
				endpoint += "?" + request.URL.RawQuery
			}
			signature, signErr := SignHMACSHA256(
				material.SecretKey, signaturePayload(timestamp, method, endpoint, body),
			)
			if signErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeKuCoin,
					AccountID: client.credentials.AccountID, Cause: signErr,
				}
			}
			passphrase := string(material.Passphrase)
			if client.apiKeyVersion == "2" {
				passphrase, signErr = SignHMACSHA256(material.SecretKey, material.Passphrase)
				if signErr != nil {
					return nil, &trade.APIError{
						Category: trade.ErrorAuthentication, Exchange: model.ExchangeKuCoin,
						AccountID: client.credentials.AccountID, Cause: signErr,
					}
				}
			}
			request.Header.Set("KC-API-KEY", string(material.APIKey))
			request.Header.Set("KC-API-SIGN", signature)
			request.Header.Set("KC-API-TIMESTAMP", timestamp)
			request.Header.Set("KC-API-PASSPHRASE", passphrase)
			request.Header.Set("KC-API-KEY-VERSION", client.apiKeyVersion)
			return request, nil
		},
	})
	if err == nil {
		observeRateLimit(client.executor.Limiter(), registered, response.Header)
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
	method, path string,
	query url.Values,
	body []byte,
) (*http.Request, error) {
	requestURL := *client.baseURL
	requestURL.Path = path
	requestURL.RawQuery = cloneValues(query).Encode()
	request, err := http.NewRequest(method, requestURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create KuCoin request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "cex-sdk-go/0")
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
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices ||
		envelope.Code != "200000" {
		if envelope.Code == "" && response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
			return nil, client.decodeBodyError(
				response, operation, errors.New("KuCoin response code is missing"),
			)
		}
		return nil, client.apiError(response, envelope.Code, envelope.Message, operation, nil)
	}
	return cloneBytes(envelope.Data), nil
}

func (client *Client) decodeData(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	target any,
) (json.RawMessage, error) {
	data, err := client.responseData(response, operation)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, client.decodeBodyError(response, operation, errors.New("KuCoin response data is missing"))
	}
	if err := json.Unmarshal(data, target); err != nil {
		return nil, client.decodeBodyError(response, operation, err)
	}
	return data, nil
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
		Category: category, Exchange: model.ExchangeKuCoin, AccountID: accountID,
		RequestID: firstNonEmpty(
			response.Header.Get("X-KuCoin-Request-Id"), response.Header.Get("X-Request-Id"),
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
		Category: category, Exchange: model.ExchangeKuCoin, AccountID: accountID,
		RequestID: firstNonEmpty(
			response.Header.Get("X-KuCoin-Request-Id"), response.Header.Get("X-Request-Id"),
		),
		Retryable: retryable, HTTPStatus: response.StatusCode,
		Cause: fmt.Errorf("decode KuCoin JSON response: %w", cause),
	}
}

func classifyError(
	status int,
	code string,
	operation commonexchange.OperationKind,
) (trade.ErrorCategory, bool) {
	if status == http.StatusTooManyRequests || code == "429000" {
		return trade.ErrorRateLimited, true
	}
	switch code {
	case "400001", "400002", "400003", "400004", "400005":
		return trade.ErrorAuthentication, false
	case "400006", "400007":
		return trade.ErrorAuthorization, false
	case "200004":
		return trade.ErrorInsufficientBalance, false
	case "230005", "500000":
		if operation == commonexchange.OperationMutation {
			return trade.ErrorUnknownExecutionState, false
		}
		return trade.ErrorExchangeUnavailable, true
	case "900001":
		return trade.ErrorValidation, false
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
	if status >= http.StatusBadRequest || code != "" {
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
