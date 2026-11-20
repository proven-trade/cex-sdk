package okx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const (
	// maxBatchOrders는 일괄 주문·취소·정정 요청 하나에 담을 수 있는 최대 주문 수다.
	maxBatchOrders = 20
	// batchOrderRateLimit는 일괄 endpoint의 2초당 주문 수 제한이다.
	batchOrderRateLimit = 300
)

var (
	cancelAllAfterTagPattern = regexp.MustCompile(`^[0-9A-Za-z]{1,16}$`)
	requestIDPattern         = regexp.MustCompile(`^[0-9A-Za-z]{1,32}$`)
)

// BatchPlaceOrdersRequest는 최대 20건의 일괄 신규 주문이다.
type BatchPlaceOrdersRequest struct {
	Orders []PlaceOrderRequest
}

// BatchCancelOrdersRequest는 최대 20건의 일괄 주문 취소다.
type BatchCancelOrdersRequest struct {
	Orders []CancelOrderRequest
}

// AmendOrderRequest는 미체결 주문의 수량 또는 가격 정정이다.
// NewSize는 정정 후 주문 전체 목표 수량이며 남은 수량이 아니다.
type AmendOrderRequest struct {
	InstrumentID   string `json:"instId"`
	OrderID        string `json:"ordId,omitempty"`
	ClientOrderID  string `json:"clOrdId,omitempty"`
	RequestID      string `json:"reqId,omitempty"`
	CancelOnFail   bool   `json:"cxlOnFail,omitempty"`
	NewSize        string `json:"newSz,omitempty"`
	NewPrice       string `json:"newPx,omitempty"`
	PriceAmendType string `json:"pxAmendType,omitempty"`
}

// BatchAmendOrdersRequest는 최대 20건의 일괄 주문 정정이다.
type BatchAmendOrdersRequest struct {
	Orders []AmendOrderRequest
}

// CancelAllAfterRequest는 카운트다운 전체 취소(Dead man's switch) 설정이다.
// TimeOut이 0이면 비활성화하고, 활성화하려면 10초에서 120초 사이여야 한다.
type CancelAllAfterRequest struct {
	TimeOut time.Duration
	Tag     string
}

// ClosePositionRequest는 포지션을 시장가로 전량 청산한다.
type ClosePositionRequest struct {
	InstrumentID  string       `json:"instId"`
	PositionSide  PositionSide `json:"posSide,omitempty"`
	MarginMode    TradeMode    `json:"mgnMode"`
	Currency      string       `json:"ccy,omitempty"`
	AutoCancel    bool         `json:"autoCxl,omitempty"`
	ClientOrderID string       `json:"clOrdId,omitempty"`
	Tag           string       `json:"tag,omitempty"`
}

// FillsRequest는 내 체결 내역 조회 조건이다. FillsHistory에서는 InstrumentType이 필수다.
type FillsRequest struct {
	InstrumentType   InstrumentType
	InstrumentFamily string
	InstrumentID     string
	OrderID          string
	SubType          string
	After            string
	Before           string
	BeginTime        *time.Time
	EndTime          *time.Time
	Limit            int
}

// AmendOrderReference는 주문 정정 접수 결과다.
type AmendOrderReference struct {
	OrderID       string          `json:"ordId"`
	ClientOrderID string          `json:"clOrdId"`
	Timestamp     string          `json:"ts"`
	RequestID     string          `json:"reqId"`
	StatusCode    string          `json:"sCode"`
	StatusMessage string          `json:"sMsg"`
	SubCode       string          `json:"subCode"`
	Raw           json.RawMessage `json:"-"`
}

// BatchOrderResult는 일괄 주문 또는 일괄 취소의 항목별 접수 결과다.
// PartialSuccess가 true이면 OKX가 일부 항목만 성공(code 2)으로 응답한 것이며,
// 각 항목의 StatusCode로 성공 여부를 확인해야 한다.
type BatchOrderResult struct {
	Orders         []OrderReference
	PartialSuccess bool
	Raw            json.RawMessage
}

// BatchAmendResult는 일괄 주문 정정의 항목별 접수 결과다.
type BatchAmendResult struct {
	Orders         []AmendOrderReference
	PartialSuccess bool
	Raw            json.RawMessage
}

// CancelAllAfterResult는 카운트다운 전체 취소 설정 결과다.
// TriggerTime이 "0"이면 비활성화된 상태다.
type CancelAllAfterResult struct {
	TriggerTime string          `json:"triggerTime"`
	Tag         string          `json:"tag"`
	Timestamp   string          `json:"ts"`
	Raw         json.RawMessage `json:"-"`
}

// ClosePositionResult는 포지션 청산 접수 결과다.
type ClosePositionResult struct {
	InstrumentID  string          `json:"instId"`
	PositionSide  PositionSide    `json:"posSide"`
	ClientOrderID string          `json:"clOrdId"`
	Tag           string          `json:"tag"`
	Raw           json.RawMessage `json:"-"`
}

// OrderPrecheck는 주문 전후 계정 증거금 변화 예측이다.
type OrderPrecheck struct {
	AdjustedEquity       string          `json:"adjEq"`
	AdjustedEquityChange string          `json:"adjEqChg"`
	InitialMargin        string          `json:"imr"`
	InitialMarginChange  string          `json:"imrChg"`
	MaintenanceMargin    string          `json:"mmr"`
	MaintenanceChange    string          `json:"mmrChg"`
	MarginRatio          string          `json:"mgnRatio"`
	MarginRatioChange    string          `json:"mgnRatioChg"`
	AvailableBalance     string          `json:"availBal"`
	AvailableChange      string          `json:"availBalChg"`
	LiquidationPrice     string          `json:"liqPx"`
	LiquidationDiff      string          `json:"liqPxDiff"`
	LiquidationDiffRatio string          `json:"liqPxDiffRatio"`
	PositionBalance      string          `json:"posBal"`
	PositionBalanceDiff  string          `json:"posBalChg"`
	Liability            string          `json:"liab"`
	LiabilityChange      string          `json:"liabChg"`
	LiabilityChangeCcy   string          `json:"liabChgCcy"`
	UnitType             string          `json:"type"`
	Raw                  json.RawMessage `json:"-"`
}

// Fill은 내 체결 한 건이다.
type Fill struct {
	InstrumentType InstrumentType  `json:"instType"`
	InstrumentID   string          `json:"instId"`
	TradeID        string          `json:"tradeId"`
	OrderID        string          `json:"ordId"`
	ClientOrderID  string          `json:"clOrdId"`
	BillID         string          `json:"billId"`
	SubType        string          `json:"subType"`
	Tag            string          `json:"tag"`
	FillPrice      string          `json:"fillPx"`
	FillSize       string          `json:"fillSz"`
	FillIndexPrice string          `json:"fillIdxPx"`
	FillPnL        string          `json:"fillPnl"`
	FillMarkPrice  string          `json:"fillMarkPx"`
	Side           Side            `json:"side"`
	PositionSide   PositionSide    `json:"posSide"`
	ExecutionType  string          `json:"execType"`
	FeeCurrency    string          `json:"feeCcy"`
	Fee            string          `json:"fee"`
	Timestamp      string          `json:"ts"`
	FillTime       string          `json:"fillTime"`
	FeeRate        string          `json:"feeRate"`
	TradeQuoteCcy  string          `json:"tradeQuoteCcy"`
	Raw            json.RawMessage `json:"-"`
}

// FillPage는 체결 목록과 원본 응답이다.
type FillPage struct {
	Fills []Fill
	Raw   json.RawMessage
}

// BatchPlaceOrders는 최대 20건의 주문을 순서대로 일괄 생성한다.
// 일부만 실패해도 오류 없이 항목별 결과를 반환하므로 각 항목의 StatusCode를 확인해야 한다.
// 자동 재시도하지 않는다.
func (client *Client) BatchPlaceOrders(
	ctx context.Context,
	request BatchPlaceOrdersRequest,
	options ...trade.RequestOption,
) (BatchOrderResult, error) {
	if err := request.validate(); err != nil {
		return BatchOrderResult{}, err
	}
	body, err := encodeBody(request.Orders)
	if err != nil {
		return BatchOrderResult{}, err
	}
	items, partial, raw, err := executeBatch(
		ctx, client, "/api/v5/trade/batch-orders", body,
		accountLimit(batchOrderRateLimit, 2*time.Second, "").withUnits(len(request.Orders)),
		func(item *OrderReference, raw []byte) { item.Raw = raw }, options...,
	)
	return BatchOrderResult{Orders: items, PartialSuccess: partial, Raw: raw}, err
}

// BatchCancelOrders는 최대 20건의 주문 취소를 일괄 접수한다.
func (client *Client) BatchCancelOrders(
	ctx context.Context,
	request BatchCancelOrdersRequest,
	options ...trade.RequestOption,
) (BatchOrderResult, error) {
	if err := request.validate(); err != nil {
		return BatchOrderResult{}, err
	}
	body, err := encodeBody(request.Orders)
	if err != nil {
		return BatchOrderResult{}, err
	}
	items, partial, raw, err := executeBatch(
		ctx, client, "/api/v5/trade/cancel-batch-orders", body,
		accountLimit(batchOrderRateLimit, 2*time.Second, "").withUnits(len(request.Orders)),
		func(item *OrderReference, raw []byte) { item.Raw = raw }, options...,
	)
	return BatchOrderResult{Orders: items, PartialSuccess: partial, Raw: raw}, err
}

// AmendOrder는 미체결 주문의 수량 또는 가격을 정정한다.
func (client *Client) AmendOrder(
	ctx context.Context,
	request AmendOrderRequest,
	options ...trade.RequestOption,
) (AmendOrderReference, error) {
	if err := request.validate(); err != nil {
		return AmendOrderReference{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return AmendOrderReference{}, err
	}
	response, data, err := client.tradeMutate(
		ctx, "/api/v5/trade/amend-order", body,
		accountLimit(60, 2*time.Second, request.InstrumentID), options...,
	)
	if err != nil {
		return AmendOrderReference{}, err
	}
	items, err := tradeDecodeItems(
		client, response, data, commonexchange.OperationMutation,
		func(item *AmendOrderReference, raw []byte) { item.Raw = raw },
	)
	if err != nil {
		return AmendOrderReference{}, err
	}
	if len(items) == 0 {
		return AmendOrderReference{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("OKX amend acknowledgement is empty"),
		)
	}
	if items[0].StatusCode != "0" {
		return AmendOrderReference{}, client.decodeError(
			response, firstNonEmpty(items[0].StatusCode, "1"), items[0].StatusMessage,
			commonexchange.OperationMutation, nil,
		)
	}
	return items[0], nil
}

// BatchAmendOrders는 최대 20건의 주문 정정을 일괄 접수한다.
func (client *Client) BatchAmendOrders(
	ctx context.Context,
	request BatchAmendOrdersRequest,
	options ...trade.RequestOption,
) (BatchAmendResult, error) {
	if err := request.validate(); err != nil {
		return BatchAmendResult{}, err
	}
	body, err := encodeBody(request.Orders)
	if err != nil {
		return BatchAmendResult{}, err
	}
	items, partial, raw, err := executeBatch(
		ctx, client, "/api/v5/trade/amend-batch-orders", body,
		accountLimit(batchOrderRateLimit, 2*time.Second, "").withUnits(len(request.Orders)),
		func(item *AmendOrderReference, raw []byte) { item.Raw = raw }, options...,
	)
	return BatchAmendResult{Orders: items, PartialSuccess: partial, Raw: raw}, err
}

// CancelAllAfter는 카운트다운 후 모든 미체결 주문을 취소하도록 예약한다.
// 같은 요청을 주기적으로 보내 카운트다운을 갱신하며, TimeOut을 0으로 보내면 해제한다.
func (client *Client) CancelAllAfter(
	ctx context.Context,
	request CancelAllAfterRequest,
	options ...trade.RequestOption,
) (CancelAllAfterResult, error) {
	if err := request.validate(); err != nil {
		return CancelAllAfterResult{}, err
	}
	body, err := encodeBody(struct {
		TimeOut string `json:"timeOut"`
		Tag     string `json:"tag,omitempty"`
	}{TimeOut: strconv.FormatInt(int64(request.TimeOut/time.Second), 10), Tag: request.Tag})
	if err != nil {
		return CancelAllAfterResult{}, err
	}
	response, data, err := client.tradeMutate(
		ctx, "/api/v5/trade/cancel-all-after", body,
		accountLimit(1, time.Second, request.Tag), options...,
	)
	if err != nil {
		return CancelAllAfterResult{}, err
	}
	return tradeDecodeOne(
		client, response, data, commonexchange.OperationMutation,
		func(item *CancelAllAfterResult, raw []byte) { item.Raw = raw },
	)
}

// OrderPrecheck는 주문을 제출하지 않고 주문 전후 증거금 변화를 예측한다.
// 다중통화 마진과 포트폴리오 마진 계정에서만 사용할 수 있다.
// 공식 문서가 필요 권한을 명시하지 않아 주문 계열과 같은 Trade 권한을 요구하지만
// 주문을 변경하지 않으므로 읽기 작업으로 실행한다.
func (client *Client) OrderPrecheck(
	ctx context.Context,
	request PlaceOrderRequest,
	options ...trade.RequestOption,
) (OrderPrecheck, error) {
	if err := request.validate(); err != nil {
		return OrderPrecheck{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return OrderPrecheck{}, err
	}
	response, _, err := client.executeSigned(
		ctx, http.MethodPost, "/api/v5/trade/order-precheck", nil, body,
		accountLimit(5, 2*time.Second, ""),
		credential.PermissionTrade, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return OrderPrecheck{}, err
	}
	data, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return OrderPrecheck{}, err
	}
	return tradeDecodeOne(
		client, response, data, commonexchange.OperationRead,
		func(item *OrderPrecheck, raw []byte) { item.Raw = raw },
	)
}

// ClosePosition은 포지션을 시장가로 전량 청산한다.
func (client *Client) ClosePosition(
	ctx context.Context,
	request ClosePositionRequest,
	options ...trade.RequestOption,
) (ClosePositionResult, error) {
	if err := request.validate(); err != nil {
		return ClosePositionResult{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return ClosePositionResult{}, err
	}
	response, data, err := client.tradeMutate(
		ctx, "/api/v5/trade/close-position", body,
		accountLimit(20, 2*time.Second, request.InstrumentID), options...,
	)
	if err != nil {
		return ClosePositionResult{}, err
	}
	return tradeDecodeOne(
		client, response, data, commonexchange.OperationMutation,
		func(item *ClosePositionResult, raw []byte) { item.Raw = raw },
	)
}

// Fills는 최근 3일 체결 내역을 조회한다.
func (client *Client) Fills(
	ctx context.Context,
	request FillsRequest,
	options ...trade.RequestOption,
) (FillPage, error) {
	return client.fills(ctx, "/api/v5/trade/fills", accountLimit(60, 2*time.Second, ""), false, request, options...)
}

// FillsHistory는 최근 3개월 체결 내역을 조회한다. InstrumentType이 필수다.
func (client *Client) FillsHistory(
	ctx context.Context,
	request FillsRequest,
	options ...trade.RequestOption,
) (FillPage, error) {
	return client.fills(
		ctx, "/api/v5/trade/fills-history", accountLimit(10, 2*time.Second, ""), true, request, options...,
	)
}

func (client *Client) fills(
	ctx context.Context,
	path string,
	limit endpointLimit,
	requireType bool,
	request FillsRequest,
	options ...trade.RequestOption,
) (FillPage, error) {
	if err := request.validate(requireType); err != nil {
		return FillPage{}, err
	}
	response, data, err := client.tradeRead(ctx, path, request.values(), limit, options...)
	if err != nil {
		return FillPage{}, err
	}
	fills, err := tradeDecodeItems(
		client, response, data, commonexchange.OperationRead,
		func(item *Fill, raw []byte) { item.Raw = raw },
	)
	if err != nil {
		return FillPage{}, err
	}
	return FillPage{Fills: fills, Raw: cloneBytes(data)}, nil
}

func (request BatchPlaceOrdersRequest) validate() error {
	if len(request.Orders) == 0 || len(request.Orders) > maxBatchOrders {
		return validationError("batch order count must be between 1 and %d", maxBatchOrders)
	}
	for _, order := range request.Orders {
		if err := order.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (request BatchCancelOrdersRequest) validate() error {
	if len(request.Orders) == 0 || len(request.Orders) > maxBatchOrders {
		return validationError("batch cancel count must be between 1 and %d", maxBatchOrders)
	}
	for _, order := range request.Orders {
		if err := order.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (request AmendOrderRequest) validate() error {
	if err := validateRequiredText("instId", request.InstrumentID); err != nil {
		return err
	}
	if err := validateOrderIdentity(request.OrderID, request.ClientOrderID); err != nil {
		return err
	}
	if request.RequestID != "" && !requestIDPattern.MatchString(request.RequestID) {
		return validationError("reqId has an invalid format")
	}
	if request.NewSize == "" && request.NewPrice == "" {
		return validationError("newSz or newPx is required")
	}
	if request.NewSize != "" {
		if err := validatePositiveDecimal("newSz", request.NewSize); err != nil {
			return err
		}
	}
	if request.NewPrice != "" {
		if err := validatePositiveDecimal("newPx", request.NewPrice); err != nil {
			return err
		}
	}
	if request.PriceAmendType != "" && request.PriceAmendType != "0" && request.PriceAmendType != "1" {
		return validationError("pxAmendType must be 0 or 1")
	}
	return nil
}

func (request BatchAmendOrdersRequest) validate() error {
	if len(request.Orders) == 0 || len(request.Orders) > maxBatchOrders {
		return validationError("batch amend count must be between 1 and %d", maxBatchOrders)
	}
	for _, order := range request.Orders {
		if err := order.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (request CancelAllAfterRequest) validate() error {
	if request.TimeOut%time.Second != 0 {
		return validationError("cancel-all-after timeout must be a whole number of seconds")
	}
	seconds := request.TimeOut / time.Second
	if seconds != 0 && (seconds < 10 || seconds > 120) {
		return validationError("cancel-all-after timeout must be 0 or between 10 and 120 seconds")
	}
	if request.Tag != "" && !cancelAllAfterTagPattern.MatchString(request.Tag) {
		return validationError("cancel-all-after tag has an invalid format")
	}
	return nil
}

func (request ClosePositionRequest) validate() error {
	if err := validateRequiredText("instId", request.InstrumentID); err != nil {
		return err
	}
	if err := validateMarginMode(request.MarginMode); err != nil {
		return err
	}
	if request.PositionSide != "" && !request.PositionSide.valid() {
		return validationError("unsupported position side %q", request.PositionSide)
	}
	if err := validateOptionalText("ccy", request.Currency); err != nil {
		return err
	}
	if request.ClientOrderID != "" && !clientOrderIDPattern.MatchString(request.ClientOrderID) {
		return validationError("clOrdId has an invalid format")
	}
	return validateOptionalText("tag", request.Tag)
}

func (request FillsRequest) validate(requireType bool) error {
	if requireType {
		if err := validateRequiredText("instType", string(request.InstrumentType)); err != nil {
			return err
		}
	} else if err := validateOptionalText("instType", string(request.InstrumentType)); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"instFamily": request.InstrumentFamily, "instId": request.InstrumentID,
		"ordId": request.OrderID, "subType": request.SubType,
	} {
		if err := validateOptionalText(name, value); err != nil {
			return err
		}
	}
	if request.BeginTime != nil && request.EndTime != nil && request.BeginTime.After(*request.EndTime) {
		return validationError("fills begin time cannot be after end time")
	}
	return validateOrderPage(request.After, request.Before, request.Limit)
}

func (request FillsRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "instType", string(request.InstrumentType))
	setIfNotEmpty(values, "instFamily", request.InstrumentFamily)
	setIfNotEmpty(values, "instId", request.InstrumentID)
	setIfNotEmpty(values, "ordId", request.OrderID)
	setIfNotEmpty(values, "subType", request.SubType)
	setIfNotEmpty(values, "after", request.After)
	setIfNotEmpty(values, "before", request.Before)
	setTime(values, "begin", request.BeginTime)
	setTime(values, "end", request.EndTime)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func validateMarginMode(mode TradeMode) error {
	if mode != TradeModeCross && mode != TradeModeIsolated {
		return validationError("margin mode must be cross or isolated")
	}
	return nil
}

// tradeRead는 서명된 GET 요청을 읽기 권한으로 실행하고 응답 data를 반환한다.
func (client *Client) tradeRead(
	ctx context.Context,
	path string,
	query url.Values,
	limit endpointLimit,
	options ...trade.RequestOption,
) (commonexchange.Response, json.RawMessage, error) {
	response, _, err := client.executeSigned(
		ctx, http.MethodGet, path, query, nil, limit,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return response, nil, err
	}
	data, err := client.responseData(response, commonexchange.OperationRead)
	return response, data, err
}

// tradeMutate는 서명된 POST 요청을 Trade 권한 변경 작업으로 실행하고 응답 data를 반환한다.
// 자동 재시도하지 않는다.
func (client *Client) tradeMutate(
	ctx context.Context,
	path string,
	body []byte,
	limit endpointLimit,
	options ...trade.RequestOption,
) (commonexchange.Response, json.RawMessage, error) {
	response, _, err := client.executeSigned(
		ctx, http.MethodPost, path, nil, body, limit,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return response, nil, err
	}
	data, err := client.responseData(response, commonexchange.OperationMutation)
	return response, data, err
}

func tradeDecodeItems[T any](
	client *Client,
	response commonexchange.Response,
	data json.RawMessage,
	operation commonexchange.OperationKind,
	setRaw func(*T, []byte),
) ([]T, error) {
	items, err := decodeItems(data, setRaw)
	if err != nil {
		return nil, client.decodeBodyError(response, operation, err)
	}
	return items, nil
}

func tradeDecodeOne[T any](
	client *Client,
	response commonexchange.Response,
	data json.RawMessage,
	operation commonexchange.OperationKind,
	setRaw func(*T, []byte),
) (T, error) {
	var zero T
	items, err := tradeDecodeItems(client, response, data, operation, setRaw)
	if err != nil {
		return zero, err
	}
	if len(items) == 0 {
		return zero, client.decodeBodyError(response, operation, errors.New("OKX response data is empty"))
	}
	return items[0], nil
}

// executeBatch는 일괄 POST를 실행한다. OKX는 일부 항목만 성공하면 top-level code 2,
// 전부 실패하면 code 1로 응답하므로 data를 버리지 않고 항목별 결과를 함께 반환한다.
func executeBatch[T any](
	ctx context.Context,
	client *Client,
	path string,
	body []byte,
	limit endpointLimit,
	setRaw func(*T, []byte),
	options ...trade.RequestOption,
) ([]T, bool, json.RawMessage, error) {
	response, _, err := client.executeSigned(
		ctx, http.MethodPost, path, nil, body, limit,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return nil, false, nil, err
	}
	data, partial, batchErr := client.tradeBatchData(response)
	if batchErr != nil && data == nil {
		return nil, false, nil, batchErr
	}
	items, err := decodeItems(data, setRaw)
	if err != nil {
		return nil, false, nil, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	if batchErr == nil && len(items) == 0 {
		return nil, false, nil, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("OKX batch acknowledgement is empty"),
		)
	}
	return items, partial, data, batchErr
}

// tradeBatchData는 일괄 응답 envelope를 해석한다.
// code 0과 2는 오류 없이 data를 반환하고(2는 partial=true), code 1은 data와 오류를 함께 반환한다.
func (client *Client) tradeBatchData(response commonexchange.Response) (json.RawMessage, bool, error) {
	var envelope struct {
		Code    string          `json:"code"`
		Message string          `json:"msg"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err != nil {
		return nil, false, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	if envelope.Code == "" {
		return nil, false, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("OKX response code is missing"),
		)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, false, client.decodeError(
			response, envelope.Code, envelope.Message, commonexchange.OperationMutation, nil,
		)
	}
	switch envelope.Code {
	case "0":
		return cloneBytes(envelope.Data), false, nil
	case "2":
		return cloneBytes(envelope.Data), true, nil
	case "1":
		code, message := envelope.Code, envelope.Message
		var items []struct {
			Code    string `json:"sCode"`
			Message string `json:"sMsg"`
		}
		if json.Unmarshal(envelope.Data, &items) == nil && len(items) > 0 && items[0].Code != "" {
			code, message = items[0].Code, items[0].Message
		}
		data := cloneBytes(envelope.Data)
		if len(items) == 0 {
			data = nil
		}
		return data, false, client.decodeError(response, code, message, commonexchange.OperationMutation, nil)
	default:
		return nil, false, client.decodeError(
			response, envelope.Code, envelope.Message, commonexchange.OperationMutation, nil,
		)
	}
}
