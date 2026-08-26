package usdm

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
	"github.com/proven-trade/cex-sdk/exchange/binance"
	"github.com/proven-trade/cex-sdk/model"
	"github.com/proven-trade/cex-sdk/transport"
)

const (
	DefaultBaseURL        = "https://fapi.binance.com"
	DefaultRequestTimeout = 10 * time.Second
	DefaultReceiveWindow  = 5 * time.Second
)

// Config는 Binance USDⓈ-M Futures 클라이언트 설정이다.
type Config struct {
	Executor             *commonexchange.Executor
	Credentials          *credential.Descriptor
	CredentialProvider   credential.Provider
	DefaultEgressRouteID transport.EgressRouteID
	BaseURL              string
	AllowInsecureHTTP    bool
	RequestTimeout       time.Duration
	ReceiveWindow        time.Duration
	Now                  func() time.Time
}

// Client는 Binance USDⓈ-M Futures REST API를 제공한다.
type Client struct {
	executor             *commonexchange.Executor
	credentials          *credential.Descriptor
	credentialProvider   credential.Provider
	defaultEgressRouteID transport.EgressRouteID
	baseURL              *url.URL
	requestTimeout       time.Duration
	receiveWindow        time.Duration
	now                  func() time.Time
	clockOffsetMillis    atomic.Int64
	limits               *limitCatalog
}

// New는 Binance USDⓈ-M Futures REST 클라이언트를 생성한다.
func New(config Config) (*Client, error) {
	if config.Executor == nil {
		return nil, fmt.Errorf("Binance USD-M executor is required")
	}
	routeID := transport.EgressRouteID(strings.TrimSpace(string(config.DefaultEgressRouteID)))
	if routeID == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, fmt.Errorf("invalid Binance USD-M base URL %q", baseURL)
	}
	if parsed.Scheme != "https" && !(config.AllowInsecureHTTP && parsed.Scheme == "http") {
		return nil, fmt.Errorf("Binance USD-M base URL must use HTTPS")
	}
	parsed.Path = ""
	if config.RequestTimeout == 0 {
		config.RequestTimeout = DefaultRequestTimeout
	}
	if config.RequestTimeout < 0 {
		return nil, fmt.Errorf("request timeout cannot be negative")
	}
	if config.ReceiveWindow == 0 {
		config.ReceiveWindow = DefaultReceiveWindow
	}
	if config.ReceiveWindow.Milliseconds() < 1 || config.ReceiveWindow > time.Minute {
		return nil, fmt.Errorf("receive window must be between 1 millisecond and 1 minute")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	var descriptor *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeBinance {
			return nil, fmt.Errorf("credential exchange must be Binance")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private Binance USD-M requests")
		}
		copyValue := *config.Credentials
		copyValue.Permissions = append([]credential.Permission(nil), config.Credentials.Permissions...)
		copyValue.AllowedEgressRouteIDs = append([]transport.EgressRouteID(nil), config.Credentials.AllowedEgressRouteIDs...)
		descriptor = &copyValue
	} else if config.CredentialProvider != nil {
		return nil, fmt.Errorf("credential descriptor is required with credential provider")
	}
	return &Client{executor: config.Executor, credentials: descriptor, credentialProvider: config.CredentialProvider, defaultEgressRouteID: routeID, baseURL: parsed, requestTimeout: config.RequestTimeout, receiveWindow: config.ReceiveWindow, now: config.Now, limits: newLimitCatalog()}, nil
}

func (client *Client) executePublic(ctx context.Context, path string, values url.Values, weight int, options ...trade.RequestOption) (commonexchange.Response, error) {
	resolved, err := client.resolveOptions(options...)
	if err != nil {
		return commonexchange.Response{}, err
	}
	charges, err := client.limits.charges(client.executor.Limiter(), resolved.EgressRouteID, "", weight, 0)
	if err != nil {
		return commonexchange.Response{}, err
	}
	response, err := client.executor.Execute(ctx, commonexchange.Execution{Exchange: model.ExchangeBinance, EgressRouteID: resolved.EgressRouteID, Timeout: resolved.Timeout, Charges: charges, Operation: commonexchange.OperationRead, Build: func(context.Context) (*http.Request, error) { return client.newRequest(http.MethodGet, path, values) }})
	if err == nil {
		observeHeaders(client.executor.Limiter(), resolved.EgressRouteID, "", response.Header)
	}
	return response, err
}

func (client *Client) executeSigned(ctx context.Context, method, path string, values url.Values, weight, orders int, permission credential.Permission, operation commonexchange.OperationKind, options ...trade.RequestOption) (commonexchange.Response, error) {
	resolved, err := client.resolveOptions(options...)
	if err != nil {
		return commonexchange.Response{}, err
	}
	if client.credentials == nil || client.credentialProvider == nil {
		return commonexchange.Response{}, &trade.APIError{Category: trade.ErrorAuthentication, Exchange: model.ExchangeBinance, Cause: errors.New("private Binance USD-M request requires credentials")}
	}
	if err := client.credentials.RequireEgressRoute(resolved.EgressRouteID); err != nil {
		return commonexchange.Response{}, &trade.APIError{Category: trade.ErrorAuthorization, Exchange: model.ExchangeBinance, AccountID: client.credentials.AccountID, Cause: err}
	}
	if err := client.credentials.RequirePermission(permission); err != nil {
		return commonexchange.Response{}, &trade.APIError{Category: trade.ErrorAuthorization, Exchange: model.ExchangeBinance, AccountID: client.credentials.AccountID, Cause: err}
	}
	charges, err := client.limits.charges(client.executor.Limiter(), resolved.EgressRouteID, client.credentials.AccountID, weight, orders)
	if err != nil {
		return commonexchange.Response{}, err
	}
	baseValues := cloneValues(values)
	var material credential.Material
	defer material.Destroy()
	response, err := client.executor.Execute(ctx, commonexchange.Execution{Exchange: model.ExchangeBinance, AccountID: client.credentials.AccountID, EgressRouteID: resolved.EgressRouteID, Timeout: resolved.Timeout, Charges: charges, Operation: operation, Build: func(buildContext context.Context) (*http.Request, error) {
		resolvedMaterial, resolveErr := client.credentialProvider.Resolve(buildContext, client.credentials.SecretRef)
		material = resolvedMaterial
		if resolveErr != nil {
			return nil, &trade.APIError{Category: trade.ErrorAuthentication, Exchange: model.ExchangeBinance, AccountID: client.credentials.AccountID, Cause: resolveErr}
		}
		if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
			return nil, &trade.APIError{Category: trade.ErrorAuthentication, Exchange: model.ExchangeBinance, AccountID: client.credentials.AccountID, Cause: errors.New("Binance API key and HMAC secret are required")}
		}
		finalValues := cloneValues(baseValues)
		finalValues.Set("recvWindow", strconv.FormatInt(client.receiveWindow.Milliseconds(), 10))
		finalValues.Set("timestamp", strconv.FormatInt(client.now().UnixMilli()+client.clockOffsetMillis.Load(), 10))
		signature, signErr := binance.SignHMACSHA256(material.SecretKey, []byte(finalValues.Encode()))
		if signErr != nil {
			return nil, signErr
		}
		finalValues.Set("signature", signature)
		request, requestErr := client.newRequest(method, path, finalValues)
		if requestErr != nil {
			return nil, requestErr
		}
		request.Header.Set("X-MBX-APIKEY", string(material.APIKey))
		return request, nil
	}})
	if err == nil {
		observeHeaders(client.executor.Limiter(), resolved.EgressRouteID, client.credentials.AccountID, response.Header)
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

func (client *Client) newRequest(method, path string, values url.Values) (*http.Request, error) {
	requestURL := *client.baseURL
	requestURL.Path = path
	requestURL.RawQuery = cloneValues(values).Encode()
	request, err := http.NewRequest(method, requestURL.String(), bytes.NewReader(nil))
	if err != nil {
		return nil, fmt.Errorf("create Binance USD-M request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "cex-sdk-go/0")
	return request, nil
}

func (client *Client) decode(response commonexchange.Response, operation commonexchange.OperationKind, target any) error {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return client.decodeError(response, operation, nil)
	}
	if err := json.Unmarshal(response.Body, target); err != nil {
		return client.decodeError(response, operation, fmt.Errorf("decode Binance USD-M JSON: %w", err))
	}
	return nil
}

func (client *Client) decodeError(response commonexchange.Response, operation commonexchange.OperationKind, cause error) error {
	var payload struct {
		Code    int    `json:"code"`
		Message string `json:"msg"`
	}
	_ = json.Unmarshal(response.Body, &payload)
	category, retryable := classifyError(response.StatusCode, payload.Code, payload.Message, operation)
	if cause != nil && category == trade.ErrorInternal {
		category, retryable = trade.ErrorExchangeUnavailable, operation == commonexchange.OperationRead
		if operation == commonexchange.OperationMutation {
			category, retryable = trade.ErrorUnknownExecutionState, false
		}
	}
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{Category: category, Exchange: model.ExchangeBinance, AccountID: accountID, RequestID: response.Header.Get("X-MBX-UUID"), Retryable: retryable, HTTPStatus: response.StatusCode, ExchangeCode: strconv.Itoa(payload.Code), ExchangeMessage: payload.Message, Cause: cause}
}

func classifyError(status, code int, message string, operation commonexchange.OperationKind) (trade.ErrorCategory, bool) {
	if status == 429 || status == 418 || code == -1003 {
		return trade.ErrorRateLimited, true
	}
	if code == -2014 || code == -2015 || code == -1022 || code == -1021 {
		return trade.ErrorAuthentication, false
	}
	if code == -2011 || code == -2013 {
		return trade.ErrorOrderNotFound, false
	}
	if code == -2019 || code == -2020 {
		return trade.ErrorInsufficientBalance, false
	}
	if code == -1008 || message == "Service Unavailable." || strings.Contains(message, "Internal error; unable to process") {
		return trade.ErrorExchangeUnavailable, true
	}
	if operation == commonexchange.OperationMutation && (status == 408 || code == -1007 || strings.Contains(strings.ToLower(message), "unknown error")) {
		return trade.ErrorUnknownExecutionState, false
	}
	if status >= 500 {
		if operation == commonexchange.OperationMutation {
			return trade.ErrorUnknownExecutionState, false
		}
		return trade.ErrorExchangeUnavailable, true
	}
	if status == 401 {
		return trade.ErrorAuthentication, false
	}
	if status == 403 {
		return trade.ErrorAuthorization, false
	}
	if status >= 400 || code != 0 {
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
func cloneBytes(value []byte) []byte { return append([]byte(nil), value...) }
