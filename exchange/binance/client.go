package binance

import (
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
	DefaultBaseURL        = "https://api.binance.com"
	DefaultRequestTimeout = 10 * time.Second
	DefaultReceiveWindow  = 5 * time.Second
)

// Config는 Binance Spot 클라이언트 설정이다.
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

// Client는 Binance Spot REST API를 요청별 EIP 선택과 함께 제공한다.
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
	limits               *limitCatalog
}

// New는 Binance Spot 클라이언트를 생성한다.
func New(config Config) (*Client, error) {
	if config.Executor == nil {
		return nil, fmt.Errorf("Binance executor is required")
	}
	if strings.TrimSpace(string(config.DefaultEgressRouteID)) == "" {
		return nil, trade.ErrMissingEgressRoute
	}
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil || parsedBaseURL.Host == "" || parsedBaseURL.User != nil ||
		parsedBaseURL.RawQuery != "" || parsedBaseURL.Fragment != "" {
		return nil, fmt.Errorf("invalid Binance base URL %q", baseURL)
	}
	if parsedBaseURL.Scheme != "https" && !(config.AllowInsecureHTTP && parsedBaseURL.Scheme == "http") {
		return nil, fmt.Errorf("Binance base URL must use HTTPS")
	}
	parsedBaseURL.Path = strings.TrimSuffix(parsedBaseURL.Path, "/")

	if config.RequestTimeout == 0 {
		config.RequestTimeout = DefaultRequestTimeout
	}
	if config.RequestTimeout < 0 {
		return nil, fmt.Errorf("Binance request timeout cannot be negative")
	}
	if config.ReceiveWindow == 0 {
		config.ReceiveWindow = DefaultReceiveWindow
	}
	if config.ReceiveWindow <= 0 || config.ReceiveWindow > 60*time.Second ||
		config.ReceiveWindow%time.Millisecond != 0 {
		return nil, fmt.Errorf("Binance receive window must be 1-60000 whole milliseconds")
	}
	if config.Now == nil {
		config.Now = time.Now
	}

	var credentialsCopy *credential.Descriptor
	if config.Credentials != nil {
		if err := config.Credentials.Validate(); err != nil {
			return nil, err
		}
		if config.Credentials.Exchange != model.ExchangeBinance {
			return nil, fmt.Errorf("credential exchange must be Binance")
		}
		if config.CredentialProvider == nil {
			return nil, fmt.Errorf("credential provider is required for private Binance requests")
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
		defaultEgressRouteID: config.DefaultEgressRouteID,
		baseURL:              parsedBaseURL,
		requestTimeout:       config.RequestTimeout,
		receiveWindowMillis:  config.ReceiveWindow.Milliseconds(),
		now:                  config.Now,
		limits:               newLimitCatalog(),
	}, nil
}

// Ping은 Binance REST API 연결 상태를 확인한다.
func (client *Client) Ping(ctx context.Context, options ...trade.RequestOption) error {
	response, _, err := client.executePublic(ctx, http.MethodGet, "/api/v3/ping", nil, 1, options...)
	if err != nil {
		return err
	}
	if err := client.ensureSuccess(response, commonexchange.OperationRead); err != nil {
		return err
	}
	var payload map[string]json.RawMessage
	return decodeJSON(response.Body, &payload)
}

// ServerTime은 Binance 서버 시간을 조회하고 로컬 서명 시계 offset을 갱신한다.
func (client *Client) ServerTime(ctx context.Context, options ...trade.RequestOption) (time.Time, error) {
	var startedAt time.Time
	response, _, err := client.executePublicWithBuildHook(
		ctx,
		http.MethodGet,
		"/api/v3/time",
		nil,
		1,
		func() { startedAt = client.now() },
		options...,
	)
	if err != nil {
		return time.Time{}, err
	}
	if err := client.ensureSuccess(response, commonexchange.OperationRead); err != nil {
		return time.Time{}, err
	}
	var payload struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := decodeJSON(response.Body, &payload); err != nil {
		return time.Time{}, err
	}
	if payload.ServerTime <= 0 {
		return time.Time{}, client.decodeError(response, commonexchange.OperationRead, errors.New("invalid server time"))
	}
	finishedAt := client.now()
	midpointMillis := startedAt.UnixMilli() + finishedAt.Sub(startedAt).Milliseconds()/2
	client.clockOffsetMillis.Store(payload.ServerTime - midpointMillis)
	return time.UnixMilli(payload.ServerTime), nil
}

// ExchangeInfo는 거래 규칙과 상품 메타데이터를 조회한다.
func (client *Client) ExchangeInfo(
	ctx context.Context,
	request ExchangeInfoRequest,
	options ...trade.RequestOption,
) (ExchangeInfo, error) {
	if request.Symbol != "" {
		if err := validateSymbol(request.Symbol); err != nil {
			return ExchangeInfo{}, err
		}
	}
	response, _, err := client.executePublic(
		ctx,
		http.MethodGet,
		"/api/v3/exchangeInfo",
		request.values(),
		20,
		options...,
	)
	if err != nil {
		return ExchangeInfo{}, err
	}
	if err := client.ensureSuccess(response, commonexchange.OperationRead); err != nil {
		return ExchangeInfo{}, err
	}
	var info ExchangeInfo
	if err := decodeJSON(response.Body, &info); err != nil {
		return ExchangeInfo{}, err
	}
	info.Raw = cloneBytes(response.Body)
	client.limits.update(info.RateLimits)
	return info, nil
}

// TickerPrice는 단일 상품의 최신 가격을 조회한다.
func (client *Client) TickerPrice(
	ctx context.Context,
	request TickerPriceRequest,
	options ...trade.RequestOption,
) (TickerPrice, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return TickerPrice{}, err
	}
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	response, _, err := client.executePublic(
		ctx,
		http.MethodGet,
		"/api/v3/ticker/price",
		values,
		2,
		options...,
	)
	if err != nil {
		return TickerPrice{}, err
	}
	if err := client.ensureSuccess(response, commonexchange.OperationRead); err != nil {
		return TickerPrice{}, err
	}
	var ticker TickerPrice
	if err := decodeJSON(response.Body, &ticker); err != nil {
		return TickerPrice{}, err
	}
	ticker.Raw = cloneBytes(response.Body)
	return ticker, nil
}

// Account는 현재 Spot 계정 정보와 잔고를 조회한다.
func (client *Client) Account(
	ctx context.Context,
	request AccountRequest,
	options ...trade.RequestOption,
) (Account, error) {
	values := make(url.Values)
	if request.OmitZeroBalances {
		values.Set("omitZeroBalances", "true")
	}
	response, _, err := client.executeSigned(
		ctx,
		http.MethodGet,
		"/api/v3/account",
		values,
		20,
		0,
		credential.PermissionRead,
		commonexchange.OperationRead,
		options...,
	)
	if err != nil {
		return Account{}, err
	}
	if err := client.ensureSuccess(response, commonexchange.OperationRead); err != nil {
		return Account{}, err
	}
	var account Account
	if err := decodeJSON(response.Body, &account); err != nil {
		return Account{}, err
	}
	account.Raw = cloneBytes(response.Body)
	return account, nil
}

// NewOrder는 Spot 주문을 생성한다.
// 불명확한 전송 오류는 자동 재시도하지 않고 ErrUnknownExecutionState로 반환한다.
func (client *Client) NewOrder(
	ctx context.Context,
	request NewOrderRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, _, err := client.executeSigned(
		ctx,
		http.MethodPost,
		"/api/v3/order",
		request.values(),
		1,
		1,
		credential.PermissionTrade,
		commonexchange.OperationMutation,
		options...,
	)
	if err != nil {
		return Order{}, err
	}
	if err := client.ensureSuccess(response, commonexchange.OperationMutation); err != nil {
		return Order{}, err
	}
	return decodeOrder(response.Body)
}

// QueryOrder는 order ID 또는 client order ID로 주문 상태를 조회한다.
func (client *Client) QueryOrder(
	ctx context.Context,
	request QueryOrderRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, _, err := client.executeSigned(
		ctx,
		http.MethodGet,
		"/api/v3/order",
		request.values(),
		4,
		0,
		credential.PermissionRead,
		commonexchange.OperationRead,
		options...,
	)
	if err != nil {
		return Order{}, err
	}
	if err := client.ensureSuccess(response, commonexchange.OperationRead); err != nil {
		return Order{}, err
	}
	return decodeOrder(response.Body)
}

// CancelOrder는 활성 주문을 취소한다.
// 불명확한 전송 오류는 주문 조회로 상태를 조정해야 한다.
func (client *Client) CancelOrder(
	ctx context.Context,
	request CancelOrderRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, _, err := client.executeSigned(
		ctx,
		http.MethodDelete,
		"/api/v3/order",
		request.values(),
		1,
		0,
		credential.PermissionTrade,
		commonexchange.OperationMutation,
		options...,
	)
	if err != nil {
		return Order{}, err
	}
	if err := client.ensureSuccess(response, commonexchange.OperationMutation); err != nil {
		return Order{}, err
	}
	return decodeOrder(response.Body)
}

func (client *Client) executePublic(
	ctx context.Context,
	method, path string,
	values url.Values,
	requestWeight int,
	options ...trade.RequestOption,
) (commonexchange.Response, transport.EgressRouteID, error) {
	return client.executePublicWithBuildHook(
		ctx,
		method,
		path,
		values,
		requestWeight,
		nil,
		options...,
	)
}

func (client *Client) executePublicWithBuildHook(
	ctx context.Context,
	method, path string,
	values url.Values,
	requestWeight int,
	beforeBuild func(),
	options ...trade.RequestOption,
) (commonexchange.Response, transport.EgressRouteID, error) {
	resolved, err := client.resolveOptions(options...)
	if err != nil {
		return commonexchange.Response{}, "", err
	}
	charges, err := client.limits.charges(
		client.executor.Limiter(),
		resolved.EgressRouteID,
		"",
		requestWeight,
		0,
	)
	if err != nil {
		return commonexchange.Response{}, "", err
	}
	response, err := client.executor.Execute(ctx, commonexchange.Execution{
		Exchange:      model.ExchangeBinance,
		EgressRouteID: resolved.EgressRouteID,
		Timeout:       resolved.Timeout,
		Charges:       charges,
		Operation:     commonexchange.OperationRead,
		Build: func(context.Context) (*http.Request, error) {
			if beforeBuild != nil {
				beforeBuild()
			}
			return client.newRequest(method, path, values, "")
		},
	})
	if err == nil {
		client.limits.observeHeaders(client.executor.Limiter(), resolved.EgressRouteID, "", response.Header)
	}
	return response, resolved.EgressRouteID, err
}

func (client *Client) executeSigned(
	ctx context.Context,
	method, path string,
	values url.Values,
	requestWeight, orderUnits int,
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
			Exchange: model.ExchangeBinance,
			Cause:    errors.New("private Binance request requires credentials"),
		}
	}
	if err := client.credentials.RequireEgressRoute(resolved.EgressRouteID); err != nil {
		return commonexchange.Response{}, "", &trade.APIError{
			Category:  trade.ErrorAuthorization,
			Exchange:  model.ExchangeBinance,
			AccountID: client.credentials.AccountID,
			Cause:     err,
		}
	}
	if err := client.credentials.RequirePermission(permission); err != nil {
		return commonexchange.Response{}, "", &trade.APIError{
			Category:  trade.ErrorAuthorization,
			Exchange:  model.ExchangeBinance,
			AccountID: client.credentials.AccountID,
			Cause:     err,
		}
	}
	charges, err := client.limits.charges(
		client.executor.Limiter(),
		resolved.EgressRouteID,
		client.credentials.AccountID,
		requestWeight,
		orderUnits,
	)
	if err != nil {
		return commonexchange.Response{}, "", err
	}

	var material credential.Material
	defer material.Destroy()
	response, err := client.executor.Execute(ctx, commonexchange.Execution{
		Exchange:      model.ExchangeBinance,
		AccountID:     client.credentials.AccountID,
		EgressRouteID: resolved.EgressRouteID,
		Timeout:       resolved.Timeout,
		Charges:       charges,
		Operation:     operation,
		Build: func(buildContext context.Context) (*http.Request, error) {
			resolvedMaterial, resolveErr := client.credentialProvider.Resolve(
				buildContext,
				client.credentials.SecretRef,
			)
			material = resolvedMaterial
			if resolveErr != nil {
				return nil, &trade.APIError{
					Category:  trade.ErrorAuthentication,
					Exchange:  model.ExchangeBinance,
					AccountID: client.credentials.AccountID,
					Cause:     resolveErr,
				}
			}
			if len(material.APIKey) == 0 || len(material.SecretKey) == 0 {
				return nil, &trade.APIError{
					Category:  trade.ErrorAuthentication,
					Exchange:  model.ExchangeBinance,
					AccountID: client.credentials.AccountID,
					Cause:     errors.New("Binance API key and HMAC secret are required"),
				}
			}

			signedValues := cloneValues(values)
			signedValues.Set("recvWindow", strconv.FormatInt(client.receiveWindowMillis, 10))
			signedValues.Set("timestamp", strconv.FormatInt(client.signedTimestampMillis(), 10))
			payload := signedValues.Encode()
			signature, signErr := SignHMACSHA256(material.SecretKey, []byte(payload))
			if signErr != nil {
				return nil, signErr
			}
			request, requestErr := client.newRequest(method, path, signedValues, signature)
			if requestErr != nil {
				return nil, requestErr
			}
			request.Header.Set("X-MBX-APIKEY", string(material.APIKey))
			return request, nil
		},
	})
	if err == nil {
		client.limits.observeHeaders(
			client.executor.Limiter(),
			resolved.EgressRouteID,
			client.credentials.AccountID,
			response.Header,
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

func (client *Client) newRequest(method, path string, values url.Values, signature string) (*http.Request, error) {
	requestURL := *client.baseURL
	requestURL.Path = strings.TrimSuffix(client.baseURL.Path, "/") + path
	rawQuery := cloneValues(values).Encode()
	if signature != "" {
		if rawQuery != "" {
			rawQuery += "&"
		}
		rawQuery += "signature=" + url.QueryEscape(signature)
	}
	requestURL.RawQuery = rawQuery
	request, err := http.NewRequest(method, requestURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create Binance request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "proven-trade-sdk-go/0")
	return request, nil
}

func (client *Client) signedTimestampMillis() int64 {
	return client.now().UnixMilli() + client.clockOffsetMillis.Load()
}

func (client *Client) ensureSuccess(response commonexchange.Response, operation commonexchange.OperationKind) error {
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	return client.decodeError(response, operation, nil)
}

func (client *Client) decodeError(
	response commonexchange.Response,
	operation commonexchange.OperationKind,
	cause error,
) error {
	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(response.Body, &payload)
	category, retryable := classifyError(response.StatusCode, payload.Code, payload.Msg, operation)
	accountID := ""
	if client.credentials != nil {
		accountID = client.credentials.AccountID
	}
	return &trade.APIError{
		Category:        category,
		Exchange:        model.ExchangeBinance,
		AccountID:       accountID,
		RequestID:       firstNonEmpty(response.Header.Get("X-MBX-UUID"), response.Header.Get("X-Response-ID")),
		Retryable:       retryable,
		HTTPStatus:      response.StatusCode,
		ExchangeCode:    exchangeCode(payload.Code),
		ExchangeMessage: payload.Msg,
		Cause:           cause,
	}
}

func classifyError(
	status, code int,
	message string,
	operation commonexchange.OperationKind,
) (trade.ErrorCategory, bool) {
	if status == http.StatusTooManyRequests || status == http.StatusTeapot || code == -1003 {
		return trade.ErrorRateLimited, true
	}
	if code == -1002 || code == -1022 || code == -2014 || code == -2015 || status == http.StatusUnauthorized {
		return trade.ErrorAuthentication, false
	}
	if status == http.StatusForbidden {
		return trade.ErrorAuthorization, false
	}
	if code == -2011 || code == -2013 || code == -2026 {
		return trade.ErrorOrderNotFound, false
	}
	if code == -2010 && strings.Contains(strings.ToLower(message), "insufficient") {
		return trade.ErrorInsufficientBalance, false
	}
	if code == -1007 || status >= http.StatusInternalServerError {
		if operation == commonexchange.OperationMutation {
			return trade.ErrorUnknownExecutionState, false
		}
		return trade.ErrorExchangeUnavailable, true
	}
	if status >= http.StatusBadRequest && status < http.StatusInternalServerError ||
		(code <= -1013 && code >= -1199) || code == -1021 || code == -1022 {
		return trade.ErrorValidation, false
	}
	return trade.ErrorInternal, false
}

func decodeOrder(body []byte) (Order, error) {
	var order Order
	if err := decodeJSON(body, &order); err != nil {
		return Order{}, err
	}
	order.Raw = cloneBytes(body)
	return order, nil
}

func decodeJSON(body []byte, target any) error {
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode Binance JSON response: %w", err)
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

func exchangeCode(code int) string {
	if code == 0 {
		return ""
	}
	return strconv.Itoa(code)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstHeader(headers map[string][]string, name string) string {
	for key, values := range headers {
		if strings.EqualFold(key, name) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

func parsePositiveInt(value string) (int, bool) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	return parsed, err == nil && parsed >= 0
}
