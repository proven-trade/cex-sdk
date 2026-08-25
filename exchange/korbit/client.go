package korbit

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
	DefaultBaseURL                  = "https://api.korbit.co.kr"
	DefaultRequestTimeout           = 10 * time.Second
	DefaultReceiveWindow            = 5000
	DefaultPublicRequestsPerSecond  = 50
	DefaultPrivateRequestsPerSecond = 50
	DefaultOrderRequestsPerSecond   = 30
)

// Config는 코빗 Open API v2 Spot REST 클라이언트 설정이다.
type Config struct {
	Executor                 *commonexchange.Executor
	Credentials              *credential.Descriptor
	CredentialProvider       credential.Provider
	DefaultEgressRouteID     transport.EgressRouteID
	BaseURL                  string
	AllowInsecureHTTP        bool
	RequestTimeout           time.Duration
	SigningMode              SigningMode
	ReceiveWindow            int
	Now                      func() time.Time
	PublicRequestsPerSecond  int
	PrivateRequestsPerSecond int
	OrderRequestsPerSecond   int
}

// Client는 코빗 Open API v2 Spot REST API를 요청별 송신 경로 선택과 함께 제공한다.
type Client struct {
	executor                 *commonexchange.Executor
	credentials              *credential.Descriptor
	credentialProvider       credential.Provider
	defaultEgressRouteID     transport.EgressRouteID
	baseURL                  *url.URL
	requestTimeout           time.Duration
	signingMode              SigningMode
	receiveWindow            int
	now                      func() time.Time
	publicRequestsPerSecond  int
	privateRequestsPerSecond int
	orderRequestsPerSecond   int
}

// New는 코빗 Open API v2 Spot REST 클라이언트를 생성한다.
func New(config Config) (*Client, error) {
	if config.Executor == nil {
		return nil, fmt.Errorf("Korbit executor is required")
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
		return nil, fmt.Errorf("invalid Korbit base URL %q", baseURL)
	}
	if parsedBaseURL.Scheme != "https" && !(config.AllowInsecureHTTP && parsedBaseURL.Scheme == "http") {
		return nil, fmt.Errorf("Korbit base URL must use HTTPS")
	}
	parsedBaseURL.Path = ""
	if config.RequestTimeout == 0 {
		config.RequestTimeout = DefaultRequestTimeout
	}
	if config.RequestTimeout < 0 {
		return nil, fmt.Errorf("Korbit request timeout cannot be negative")
	}
	if config.SigningMode == "" {
		config.SigningMode = SigningModeHMACSHA256
	}
	if config.SigningMode != SigningModeHMACSHA256 && config.SigningMode != SigningModeED25519 {
		return nil, fmt.Errorf("unsupported Korbit signing mode %q", config.SigningMode)
	}
	if config.ReceiveWindow == 0 {
		config.ReceiveWindow = DefaultReceiveWindow
	}
	if config.ReceiveWindow < 1 || config.ReceiveWindow > 60000 {
		return nil, fmt.Errorf("Korbit receive window must be between 1 and 60000 milliseconds")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.PublicRequestsPerSecond == 0 {
		config.PublicRequestsPerSecond = DefaultPublicRequestsPerSecond
	}
	if config.PrivateRequestsPerSecond == 0 {
		config.PrivateRequestsPerSecond = DefaultPrivateRequestsPerSecond
	}
	if config.OrderRequestsPerSecond == 0 {
		config.OrderRequestsPerSecond = DefaultOrderRequestsPerSecond
	}
	if config.PublicRequestsPerSecond < 1 || config.PrivateRequestsPerSecond < 1 || config.OrderRequestsPerSecond < 1 {
		return nil, fmt.Errorf("Korbit request limits must be positive")
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeKorbit {
			return nil, fmt.Errorf("credential exchange must be Korbit")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private Korbit requests")
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
		signingMode: config.SigningMode, receiveWindow: config.ReceiveWindow, now: config.Now,
		publicRequestsPerSecond:  config.PublicRequestsPerSecond,
		privateRequestsPerSecond: config.PrivateRequestsPerSecond,
		orderRequestsPerSecond:   config.OrderRequestsPerSecond,
	}, nil
}

func (client *Client) executePublic(
	ctx context.Context,
	path string,
	parameters url.Values,
	options ...trade.RequestOption,
) (commonexchange.Response, error) {
	resolved, err := client.resolveOptions(options...)
	if err != nil {
		return commonexchange.Response{}, err
	}
	limit, charges, err := publicRateLimit(
		client.executor.Limiter(), resolved.EgressRouteID, client.publicRequestsPerSecond,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}
	response, err := client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeKorbit, EgressRouteID: resolved.EgressRouteID,
		Timeout: resolved.Timeout, Charges: charges, Operation: commonexchange.OperationRead,
		Build: func(context.Context) (*http.Request, error) {
			return client.newRequest(http.MethodGet, path, parameters.Encode())
		},
	})
	if err == nil {
		observeRateLimit(client.executor.Limiter(), limit, response.Header)
	}
	return response, err
}

func (client *Client) executePrivate(
	ctx context.Context,
	method, path string,
	parameters url.Values,
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
			Category: trade.ErrorAuthentication, Exchange: model.ExchangeKorbit,
			Cause: errors.New("private Korbit request requires credentials"),
		}
	}
	if err := client.credentials.RequireEgressRoute(resolved.EgressRouteID); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeKorbit,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	if err := client.credentials.RequirePermission(permission); err != nil {
		return commonexchange.Response{}, &trade.APIError{
			Category: trade.ErrorAuthorization, Exchange: model.ExchangeKorbit,
			AccountID: client.credentials.AccountID, Cause: err,
		}
	}
	limit, charges, err := privateRateLimit(
		client.executor.Limiter(), client.credentials.AccountID, group,
		client.privateRequestsPerSecond, client.orderRequestsPerSecond,
	)
	if err != nil {
		return commonexchange.Response{}, err
	}

	var material credential.Material
	defer material.Destroy()
	response, err := client.executor.Execute(ctx, commonexchange.Execution{
		Exchange: model.ExchangeKorbit, AccountID: client.credentials.AccountID,
		EgressRouteID: resolved.EgressRouteID, Timeout: resolved.Timeout,
		Charges: charges, Operation: operation,
		Build: func(buildContext context.Context) (*http.Request, error) {
			resolvedMaterial, resolveErr := client.credentialProvider.Resolve(
				buildContext, client.credentials.SecretRef,
			)
			material = resolvedMaterial
			if resolveErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeKorbit,
					AccountID: client.credentials.AccountID, Cause: resolveErr,
				}
			}
			if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeKorbit,
					AccountID: client.credentials.AccountID,
					Cause:     errors.New("Korbit API key and signing key are required"),
				}
			}
			finalParameters := cloneValues(parameters)
			timestamp := client.now().UnixMilli()
			if timestamp <= 0 {
				return nil, validationError("Korbit timestamp must be after the Unix epoch")
			}
			finalParameters.Set("timestamp", fmt.Sprintf("%d", timestamp))
			finalParameters.Set("recvWindow", fmt.Sprintf("%d", client.receiveWindow))
			unsigned := finalParameters.Encode()
			signature, signErr := signParameters(client.signingMode, material.SecretKey, unsigned)
			if signErr != nil {
				return nil, &trade.APIError{
					Category: trade.ErrorAuthentication, Exchange: model.ExchangeKorbit,
					AccountID: client.credentials.AccountID, Cause: signErr,
				}
			}
			signed := unsigned + "&signature=" + url.QueryEscape(signature)
			request, requestErr := client.newRequest(method, path, signed)
			if requestErr != nil {
				return nil, requestErr
			}
			request.Header.Set("X-KAPI-KEY", string(material.APIKey))
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

func (client *Client) newRequest(method, path, encodedParameters string) (*http.Request, error) {
	requestURL := *client.baseURL
	requestURL.Path = path
	if method != http.MethodPost {
		requestURL.RawQuery = encodedParameters
	}
	var bodyReader *strings.Reader
	if method == http.MethodPost {
		bodyReader = strings.NewReader(encodedParameters)
	} else {
		bodyReader = strings.NewReader("")
	}
	request, err := http.NewRequest(method, requestURL.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create Korbit request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	request.Header.Set("User-Agent", "proven-trade-sdk-go/0")
	return request, nil
}

func (client *Client) decodeResponse(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	target any,
) ([]byte, error) {
	var envelope struct {
		Success *bool           `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err != nil {
		return nil, client.decodeBodyError(response, operation, err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices ||
		envelope.Success != nil && !*envelope.Success {
		return nil, client.apiError(response, operation, envelope.Error.Message)
	}
	if envelope.Success == nil || !*envelope.Success {
		return nil, client.decodeBodyError(response, operation, errors.New("Korbit response envelope is incomplete"))
	}
	if target != nil {
		if len(envelope.Data) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Data), []byte("null")) {
			return nil, client.decodeBodyError(response, operation, errors.New("Korbit response data is missing"))
		}
		if err := json.Unmarshal(envelope.Data, target); err != nil {
			return nil, client.decodeBodyError(response, operation, err)
		}
	}
	return cloneBytes(envelope.Data), nil
}

func (client *Client) apiError(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	code string,
) error {
	category, retryable := classifyError(response.StatusCode, code, operation)
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category: category, Exchange: model.ExchangeKorbit, AccountID: accountID,
		RequestID: firstNonEmpty(
			response.Header.Get("X-Request-ID"), response.Header.Get("X-Korbit-Request-ID"),
		),
		Retryable: retryable, HTTPStatus: response.StatusCode,
		ExchangeCode: code,
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
		Category: category, Exchange: model.ExchangeKorbit, AccountID: accountID,
		Retryable: retryable, HTTPStatus: response.StatusCode,
		Cause: fmt.Errorf("decode Korbit JSON response: %w", cause),
	}
}

func classifyError(status int, code string, operation commonexchange.OperationKind) (trade.ErrorCategory, bool) {
	if status == http.StatusTooManyRequests {
		return trade.ErrorRateLimited, true
	}
	switch code {
	case "NO_BALANCE":
		return trade.ErrorInsufficientBalance, false
	case "ORDER_NOT_FOUND", "ORDER_ALREADY_CANCELED", "ORDER_ALREADY_FILLED", "ORDER_ALREADY_EXPIRED":
		return trade.ErrorOrderNotFound, false
	case "INVALID_USER_STATUS":
		return trade.ErrorAuthorization, false
	case "TRY_AGAIN":
		return trade.ErrorExchangeUnavailable, true
	case "EXCEED_TIME_WINDOW":
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
