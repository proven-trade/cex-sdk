package upbit

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
	DefaultBaseURL        = "https://api.upbit.com"
	DefaultRequestTimeout = 10 * time.Second
)

// NonceSource는 인증 요청마다 중복되지 않는 nonce를 생성한다.
type NonceSource func() (string, error)

// Config는 업비트 Spot 클라이언트 설정이다.
type Config struct {
	Executor             *commonexchange.Executor
	Credentials          *credential.Descriptor
	CredentialProvider   credential.Provider
	DefaultEgressRouteID transport.EgressRouteID
	BaseURL              string
	AllowInsecureHTTP    bool
	RequestTimeout       time.Duration
	NonceSource          NonceSource
}

// Client는 업비트 Spot REST API를 제공한다.
type Client struct {
	executor             *commonexchange.Executor
	credentials          *credential.Descriptor
	credentialProvider   credential.Provider
	defaultEgressRouteID transport.EgressRouteID
	baseURL              *url.URL
	requestTimeout       time.Duration
	nonceSource          NonceSource
}

// New는 업비트 Spot REST 클라이언트를 생성한다.
func New(config Config) (*Client, error) {
	if config.Executor == nil {
		return nil, fmt.Errorf("Upbit executor is required")
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
		return nil, fmt.Errorf("invalid Upbit base URL %q", baseURL)
	}
	if parsedBaseURL.Scheme != "https" && !(config.AllowInsecureHTTP && parsedBaseURL.Scheme == "http") {
		return nil, fmt.Errorf("Upbit base URL must use HTTPS")
	}
	parsedBaseURL.Path = ""
	if config.RequestTimeout == 0 {
		config.RequestTimeout = DefaultRequestTimeout
	}
	if config.RequestTimeout < 0 {
		return nil, fmt.Errorf("Upbit request timeout cannot be negative")
	}
	if config.NonceSource == nil {
		config.NonceSource = randomNonce
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeUpbit {
			return nil, fmt.Errorf("credential exchange must be Upbit")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private Upbit requests")
		}
		copyValue := *config.Credentials
		copyValue.Permissions = append([]credential.Permission(nil), config.Credentials.Permissions...)
		copyValue.AllowedEgressRouteIDs = append([]transport.EgressRouteID(nil), config.Credentials.AllowedEgressRouteIDs...)
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
		nonceSource:          config.NonceSource,
	}, nil
}

func (client *Client) executePublic(
	ctx context.Context,
	path string,
	query parameters,
	group rateGroup,
	options ...trade.RequestOption,
) (commonexchange.Response, error) {
	resolved, err := client.resolveOptions(options...)
	if err != nil {
		return commonexchange.Response{}, err
	}
	charges, rateKey, err := rateLimitCharges(client.executor.Limiter(), resolved.EgressRouteID, "", group)
	if err != nil {
		return commonexchange.Response{}, err
	}
	response, err := client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeUpbit, EgressRouteID: resolved.EgressRouteID,
		Timeout: resolved.Timeout, Charges: charges, Operation: commonexchange.OperationRead,
		Build: func(context.Context) (*http.Request, error) {
			return client.newRequest(http.MethodGet, path, query, nil)
		},
	})
	if err == nil {
		observeRemaining(client.executor.Limiter(), rateKey, group, response.Header)
	}
	return response, err
}

func (client *Client) executeSigned(
	ctx context.Context,
	method, path string,
	params parameters,
	body []byte,
	group rateGroup,
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
			Category: trade.ErrorAuthentication, Exchange: model.ExchangeUpbit,
			Cause: errors.New("private Upbit request requires credentials"),
		}
	}
	if err := client.credentials.RequireEgressRoute(resolved.EgressRouteID); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeUpbit,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	if err := client.credentials.RequirePermission(permission); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeUpbit,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	charges, rateKey, err := rateLimitCharges(
		client.executor.Limiter(), resolved.EgressRouteID, client.credentials.AccountID, group,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}

	var material credential.Material
	defer material.Destroy()
	response, err := client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeUpbit, AccountID: client.credentials.AccountID,
		EgressRouteID: resolved.EgressRouteID, Timeout: resolved.Timeout,
		Charges: charges, Operation: operation,
		Build: func(buildContext context.Context) (*http.Request, error) {
			resolvedMaterial, resolveErr := client.credentialProvider.Resolve(buildContext, client.credentials.SecretRef)
			material = resolvedMaterial
			if resolveErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeUpbit,
					AccountID: client.credentials.AccountID, Cause: resolveErr,
				}
			}
			if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeUpbit,
					AccountID: client.credentials.AccountID,
					Cause:     errors.New("Upbit access key and secret key are required"),
				}
			}
			nonce, nonceErr := client.nonceSource()
			if nonceErr != nil {
				return nil, nonceErr
			}
			hashInput, hashErr := params.hashString()
			if hashErr != nil {
				return nil, hashErr
			}
			token, signErr := SignJWT(material.APIKey, material.SecretKey, nonce, hashInput)
			if signErr != nil {
				return nil, signErr
			}
			query := params
			if method == http.MethodPost {
				query = nil
			}
			request, requestErr := client.newRequest(method, path, query, body)
			if requestErr != nil {
				return nil, requestErr
			}
			request.Header.Set("Authorization", "Bearer "+token)
			return request, nil
		},
	})
	if err == nil {
		observeRemaining(client.executor.Limiter(), rateKey, group, response.Header)
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

func (client *Client) newRequest(method, path string, query parameters, body []byte) (*http.Request, error) {
	requestURL := *client.baseURL
	requestURL.Path = path
	requestURL.RawQuery = query.encoded()
	request, err := http.NewRequest(method, requestURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create Upbit request: %w", err)
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
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return client.decodeErrorResponse(response, operation)
	}
	if err := json.Unmarshal(response.Body, target); err != nil {
		return client.decodeBodyError(response, operation, err)
	}
	return nil
}

func (client *Client) decodeErrorResponse(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
) error {
	var envelope struct {
		Error struct {
			Name    json.RawMessage `json:"name"`
			Message string          `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err != nil {
		return client.decodeBodyError(response, operation, err)
	}
	code := strings.Trim(string(envelope.Error.Name), `"`)
	category, retryable := classifyError(response.StatusCode, code, operation)
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: category, Exchange: model.ExchangeUpbit, AccountID: accountID,
		RequestID: firstNonEmpty(response.Header.Get("X-Request-ID"), response.Header.Get("Request-ID")),
		Retryable: retryable, HTTPStatus: response.StatusCode,
		ExchangeCode: code, ExchangeMessage: envelope.Error.Message,
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
		Category: category, Exchange: model.ExchangeUpbit, AccountID: accountID,
		Retryable: retryable, HTTPStatus: response.StatusCode,
		Cause: fmt.Errorf("decode Upbit JSON response: %w", cause),
	}
}

func classifyError(status int, code string, operation commonexchange.OperationKind) (trade.ErrorCategory, bool) {
	if status == http.StatusTooManyRequests || status == http.StatusTeapot {
		return trade.ErrorRateLimited, true
	}
	switch code {
	case "invalid_query_payload", "jwt_verification", "expired_access_key", "nonce_used", "no_authorization_token":
		return trade.ErrorAuthentication, false
	case "no_authorization_ip", "out_of_scope":
		return trade.ErrorAuthorization, false
	case "insufficient_funds_ask", "insufficient_funds_bid":
		return trade.ErrorInsufficientBalance, false
	case "order_not_found":
		return trade.ErrorOrderNotFound, false
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

func encodeOrderBody(params parameters) ([]byte, error) {
	buffer := bytes.NewBufferString("{")
	for index, value := range params {
		if index > 0 {
			buffer.WriteByte(',')
		}
		key, err := json.Marshal(value.key)
		if err != nil {
			return nil, err
		}
		item, err := json.Marshal(value.value)
		if err != nil {
			return nil, err
		}
		buffer.Write(key)
		buffer.WriteByte(':')
		buffer.Write(item)
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
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
