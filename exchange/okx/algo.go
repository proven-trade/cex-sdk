package okx

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// maxCancelAlgoOrders는 알고 주문 취소 요청 하나에 담을 수 있는 최대 주문 수다.
const maxCancelAlgoOrders = 10

var (
	algoClientOrderIDPattern = regexp.MustCompile(`^[0-9A-Za-z]{1,32}$`)
	algoTagPattern           = regexp.MustCompile(`^[0-9A-Za-z]{1,16}$`)
)

// AlgoOrderType은 알고 주문 유형이다.
type AlgoOrderType string

const (
	AlgoOrderTypeConditional   AlgoOrderType = "conditional"
	AlgoOrderTypeOCO           AlgoOrderType = "oco"
	AlgoOrderTypeTrigger       AlgoOrderType = "trigger"
	AlgoOrderTypeMoveOrderStop AlgoOrderType = "move_order_stop"
	AlgoOrderTypeTWAP          AlgoOrderType = "twap"
	AlgoOrderTypeChase         AlgoOrderType = "chase"
	AlgoOrderTypeSmartIceberg  AlgoOrderType = "smart_iceberg"
)

// PlaceAlgoOrderRequest는 알고 주문 생성 조건이다.
// conditional·oco·trigger·move_order_stop·twap 주문의 필드를 지원한다.
// chase·smart_iceberg 고급 파라미터와 attachAlgoOrds, triggerParams는 지원하지 않는다.
type PlaceAlgoOrderRequest struct {
	InstrumentID    string         `json:"instId"`
	TradeMode       TradeMode      `json:"tdMode"`
	Currency        string         `json:"ccy,omitempty"`
	Side            Side           `json:"side"`
	PositionSide    PositionSide   `json:"posSide,omitempty"`
	OrderType       AlgoOrderType  `json:"ordType"`
	Size            string         `json:"sz,omitempty"`
	CloseFraction   string         `json:"closeFraction,omitempty"`
	Tag             string         `json:"tag,omitempty"`
	TargetCurrency  TargetCurrency `json:"tgtCcy,omitempty"`
	ClientAlgoID    string         `json:"algoClOrdId,omitempty"`
	ReduceOnly      bool           `json:"reduceOnly,omitempty"`
	CancelOnClose   bool           `json:"cxlOnClosePos,omitempty"`
	TakeProfitPx    string         `json:"tpTriggerPx,omitempty"`
	TakeProfitType  string         `json:"tpTriggerPxType,omitempty"`
	TakeProfitOrder string         `json:"tpOrdPx,omitempty"`
	StopLossPx      string         `json:"slTriggerPx,omitempty"`
	StopLossType    string         `json:"slTriggerPxType,omitempty"`
	StopLossOrder   string         `json:"slOrdPx,omitempty"`
	TriggerPx       string         `json:"triggerPx,omitempty"`
	TriggerPxType   string         `json:"triggerPxType,omitempty"`
	OrderPx         string         `json:"orderPx,omitempty"`
	CallbackRatio   string         `json:"callbackRatio,omitempty"`
	CallbackSpread  string         `json:"callbackSpread,omitempty"`
	ActivePx        string         `json:"activePx,omitempty"`
	PxVar           string         `json:"pxVar,omitempty"`
	PxSpread        string         `json:"pxSpread,omitempty"`
	SizeLimit       string         `json:"szLimit,omitempty"`
	PxLimit         string         `json:"pxLimit,omitempty"`
	TimeInterval    string         `json:"timeInterval,omitempty"`
}

// CancelAlgoOrderRequest는 알고 주문 하나의 취소 대상이다.
type CancelAlgoOrderRequest struct {
	InstrumentID string `json:"instId"`
	AlgoID       string `json:"algoId"`
}

// CancelAlgoOrdersRequest는 최대 10건의 알고 주문 취소다.
type CancelAlgoOrdersRequest struct {
	Orders []CancelAlgoOrderRequest
}

// AmendAlgoOrderRequest는 알고 주문의 수량·TP/SL·트리거 가격 정정이다.
type AmendAlgoOrderRequest struct {
	InstrumentID       string `json:"instId"`
	AlgoID             string `json:"algoId,omitempty"`
	ClientAlgoID       string `json:"algoClOrdId,omitempty"`
	CancelOnFail       bool   `json:"cxlOnFail,omitempty"`
	RequestID          string `json:"reqId,omitempty"`
	NewSize            string `json:"newSz,omitempty"`
	NewTakeProfitPx    string `json:"newTpTriggerPx,omitempty"`
	NewTakeProfitOrder string `json:"newTpOrdPx,omitempty"`
	NewStopLossPx      string `json:"newSlTriggerPx,omitempty"`
	NewStopLossOrder   string `json:"newSlOrdPx,omitempty"`
	NewTakeProfitType  string `json:"newTpTriggerPxType,omitempty"`
	NewStopLossType    string `json:"newSlTriggerPxType,omitempty"`
	NewTriggerPx       string `json:"newTriggerPx,omitempty"`
	NewOrderPx         string `json:"newOrdPx,omitempty"`
	NewTriggerType     string `json:"newTriggerPxType,omitempty"`
}

// AlgoOrderDetailRequest는 알고 주문 ID 또는 client algo ID로 단건 조회한다.
type AlgoOrderDetailRequest struct {
	AlgoID       string
	ClientAlgoID string
}

// AlgoOrdersRequest는 알고 주문 미체결·이력 목록 조회 조건이다.
// OrderTypes는 필수이며 conditional과 oco만 함께 지정할 수 있다.
// 이력 조회(AlgoOrderHistory)는 State 또는 AlgoID 중 하나가 필수다.
type AlgoOrdersRequest struct {
	OrderTypes     []AlgoOrderType
	State          string
	AlgoID         string
	InstrumentType InstrumentType
	InstrumentID   string
	After          string
	Before         string
	Limit          int
}

// AlgoOrderReference는 알고 주문 생성·취소·정정 접수 결과다.
type AlgoOrderReference struct {
	AlgoID        string          `json:"algoId"`
	ClientOrderID string          `json:"clOrdId"`
	ClientAlgoID  string          `json:"algoClOrdId"`
	RequestID     string          `json:"reqId"`
	Tag           string          `json:"tag"`
	StatusCode    string          `json:"sCode"`
	StatusMessage string          `json:"sMsg"`
	Raw           json.RawMessage `json:"-"`
}

// BatchAlgoResult는 알고 주문 일괄 취소의 항목별 결과다.
type BatchAlgoResult struct {
	Orders         []AlgoOrderReference
	PartialSuccess bool
	Raw            json.RawMessage
}

// AlgoOrder는 알고 주문 상세다.
type AlgoOrder struct {
	InstrumentType InstrumentType  `json:"instType"`
	InstrumentID   string          `json:"instId"`
	Currency       string          `json:"ccy"`
	OrderID        string          `json:"ordId"`
	OrderIDList    []string        `json:"ordIdList"`
	SubAlgoIDList  []string        `json:"subAlgoIdList"`
	AlgoID         string          `json:"algoId"`
	ClientOrderID  string          `json:"clOrdId"`
	ClientAlgoID   string          `json:"algoClOrdId"`
	Size           string          `json:"sz"`
	CloseFraction  string          `json:"closeFraction"`
	OrderType      AlgoOrderType   `json:"ordType"`
	Side           Side            `json:"side"`
	PositionSide   PositionSide    `json:"posSide"`
	TradeMode      TradeMode       `json:"tdMode"`
	TargetCurrency TargetCurrency  `json:"tgtCcy"`
	State          string          `json:"state"`
	Leverage       string          `json:"lever"`
	TakeProfitPx   string          `json:"tpTriggerPx"`
	TakeProfitType string          `json:"tpTriggerPxType"`
	TakeProfitOrd  string          `json:"tpOrdPx"`
	StopLossPx     string          `json:"slTriggerPx"`
	StopLossType   string          `json:"slTriggerPxType"`
	StopLossOrd    string          `json:"slOrdPx"`
	TriggerPx      string          `json:"triggerPx"`
	TriggerPxType  string          `json:"triggerPxType"`
	OrderPx        string          `json:"ordPx"`
	ActualSize     string          `json:"actualSz"`
	ActualPx       string          `json:"actualPx"`
	ActualSide     string          `json:"actualSide"`
	TriggerTime    string          `json:"triggerTime"`
	CallbackRatio  string          `json:"callbackRatio"`
	CallbackSpread string          `json:"callbackSpread"`
	ActivePx       string          `json:"activePx"`
	MoveTriggerPx  string          `json:"moveTriggerPx"`
	ReduceOnly     string          `json:"reduceOnly"`
	FailCode       string          `json:"failCode"`
	Tag            string          `json:"tag"`
	CreatedTime    string          `json:"cTime"`
	UpdatedTime    string          `json:"uTime"`
	Raw            json.RawMessage `json:"-"`
}

// AlgoOrderPage는 알고 주문 목록과 원본 응답이다.
type AlgoOrderPage struct {
	Orders []AlgoOrder
	Raw    json.RawMessage
}

// PlaceAlgoOrder는 알고 주문(조건부, OCO, 트리거, 트레일링, TWAP)을 생성한다.
// 자동 재시도하지 않는다.
func (client *Client) PlaceAlgoOrder(
	ctx context.Context,
	request PlaceAlgoOrderRequest,
	options ...trade.RequestOption,
) (AlgoOrderReference, error) {
	if err := request.validate(); err != nil {
		return AlgoOrderReference{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return AlgoOrderReference{}, err
	}
	response, data, err := client.tradeMutate(
		ctx, "/api/v5/trade/order-algo", body,
		accountLimit(20, 2*time.Second, request.InstrumentID), options...,
	)
	if err != nil {
		return AlgoOrderReference{}, err
	}
	return client.decodeAlgoReference(response, data)
}

// CancelAlgoOrders는 최대 10건의 미체결 알고 주문을 취소한다.
func (client *Client) CancelAlgoOrders(
	ctx context.Context,
	request CancelAlgoOrdersRequest,
	options ...trade.RequestOption,
) (BatchAlgoResult, error) {
	if err := request.validate(); err != nil {
		return BatchAlgoResult{}, err
	}
	body, err := encodeBody(request.Orders)
	if err != nil {
		return BatchAlgoResult{}, err
	}
	items, partial, raw, err := executeBatch(
		ctx, client, "/api/v5/trade/cancel-algos", body,
		accountLimit(20, 2*time.Second, "").withUnits(len(request.Orders)),
		func(item *AlgoOrderReference, raw []byte) { item.Raw = raw }, options...,
	)
	return BatchAlgoResult{Orders: items, PartialSuccess: partial, Raw: raw}, err
}

// AmendAlgoOrder는 미체결 알고 주문의 수량, TP/SL, 트리거 가격을 정정한다.
func (client *Client) AmendAlgoOrder(
	ctx context.Context,
	request AmendAlgoOrderRequest,
	options ...trade.RequestOption,
) (AlgoOrderReference, error) {
	if err := request.validate(); err != nil {
		return AlgoOrderReference{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return AlgoOrderReference{}, err
	}
	response, data, err := client.tradeMutate(
		ctx, "/api/v5/trade/amend-algos", body,
		accountLimit(20, 2*time.Second, request.InstrumentID), options...,
	)
	if err != nil {
		return AlgoOrderReference{}, err
	}
	return client.decodeAlgoReference(response, data)
}

// AlgoOrderDetail은 알고 주문 한 건을 조회한다.
func (client *Client) AlgoOrderDetail(
	ctx context.Context,
	request AlgoOrderDetailRequest,
	options ...trade.RequestOption,
) (AlgoOrder, error) {
	if err := request.validate(); err != nil {
		return AlgoOrder{}, err
	}
	response, data, err := client.tradeRead(
		ctx, "/api/v5/trade/order-algo", request.values(), accountLimit(20, 2*time.Second, ""), options...,
	)
	if err != nil {
		return AlgoOrder{}, err
	}
	items, err := tradeDecodeItems(
		client, response, data, commonexchange.OperationRead,
		func(item *AlgoOrder, raw []byte) { item.Raw = raw },
	)
	if err != nil {
		return AlgoOrder{}, err
	}
	if len(items) == 0 {
		return AlgoOrder{}, client.decodeError(
			response, "51603", "algo order does not exist", commonexchange.OperationRead, nil,
		)
	}
	return items[0], nil
}

// AlgoOrdersPending은 트리거 전 알고 주문 목록을 조회한다.
func (client *Client) AlgoOrdersPending(
	ctx context.Context,
	request AlgoOrdersRequest,
	options ...trade.RequestOption,
) (AlgoOrderPage, error) {
	return client.algoOrders(ctx, "/api/v5/trade/orders-algo-pending", false, request, options...)
}

// AlgoOrderHistory는 최근 3개월 알고 주문 이력을 조회한다.
// State 또는 AlgoID 중 하나가 필수다.
func (client *Client) AlgoOrderHistory(
	ctx context.Context,
	request AlgoOrdersRequest,
	options ...trade.RequestOption,
) (AlgoOrderPage, error) {
	return client.algoOrders(ctx, "/api/v5/trade/orders-algo-history", true, request, options...)
}

func (client *Client) algoOrders(
	ctx context.Context,
	path string,
	history bool,
	request AlgoOrdersRequest,
	options ...trade.RequestOption,
) (AlgoOrderPage, error) {
	if err := request.validate(history); err != nil {
		return AlgoOrderPage{}, err
	}
	response, data, err := client.tradeRead(
		ctx, path, request.values(history), accountLimit(20, 2*time.Second, ""), options...,
	)
	if err != nil {
		return AlgoOrderPage{}, err
	}
	orders, err := tradeDecodeItems(
		client, response, data, commonexchange.OperationRead,
		func(item *AlgoOrder, raw []byte) { item.Raw = raw },
	)
	if err != nil {
		return AlgoOrderPage{}, err
	}
	return AlgoOrderPage{Orders: orders, Raw: cloneBytes(data)}, nil
}

func (client *Client) decodeAlgoReference(
	response commonexchange.Response,
	data json.RawMessage,
) (AlgoOrderReference, error) {
	items, err := tradeDecodeItems(
		client, response, data, commonexchange.OperationMutation,
		func(item *AlgoOrderReference, raw []byte) { item.Raw = raw },
	)
	if err != nil {
		return AlgoOrderReference{}, err
	}
	if len(items) == 0 {
		return AlgoOrderReference{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("OKX algo acknowledgement is empty"),
		)
	}
	if items[0].StatusCode == "" {
		return AlgoOrderReference{}, client.decodeBodyError(
			response, commonexchange.OperationMutation,
			errors.New("OKX algo acknowledgement status code is missing"),
		)
	}
	if items[0].StatusCode != "0" {
		return AlgoOrderReference{}, client.decodeError(
			response, items[0].StatusCode, items[0].StatusMessage, commonexchange.OperationMutation, nil,
		)
	}
	return items[0], nil
}

func (orderType AlgoOrderType) valid() bool {
	switch orderType {
	case AlgoOrderTypeConditional, AlgoOrderTypeOCO, AlgoOrderTypeTrigger,
		AlgoOrderTypeMoveOrderStop, AlgoOrderTypeTWAP, AlgoOrderTypeChase, AlgoOrderTypeSmartIceberg:
		return true
	default:
		return false
	}
}

func (request PlaceAlgoOrderRequest) validate() error {
	if err := validateRequiredText("instId", request.InstrumentID); err != nil {
		return err
	}
	if request.TradeMode != TradeModeCash && request.TradeMode != TradeModeCross &&
		request.TradeMode != TradeModeIsolated {
		return validationError("trade mode must be cash, cross or isolated")
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("side must be buy or sell")
	}
	if request.PositionSide != "" && !request.PositionSide.valid() {
		return validationError("unsupported position side %q", request.PositionSide)
	}
	switch request.OrderType {
	case AlgoOrderTypeConditional, AlgoOrderTypeOCO, AlgoOrderTypeTrigger,
		AlgoOrderTypeMoveOrderStop, AlgoOrderTypeTWAP:
	default:
		return validationError("unsupported algo order type %q", request.OrderType)
	}
	if request.Size == "" && request.CloseFraction == "" {
		return validationError("sz or closeFraction is required")
	}
	if request.Size != "" {
		if err := validatePositiveDecimal("sz", request.Size); err != nil {
			return err
		}
	}
	if request.ClientAlgoID != "" && !algoClientOrderIDPattern.MatchString(request.ClientAlgoID) {
		return validationError("algoClOrdId has an invalid format")
	}
	if request.Tag != "" && !algoTagPattern.MatchString(request.Tag) {
		return validationError("tag has an invalid format")
	}
	if request.TargetCurrency != "" && request.TargetCurrency != TargetCurrencyBase &&
		request.TargetCurrency != TargetCurrencyQuote {
		return validationError("target currency must be base_ccy or quote_ccy")
	}
	switch request.OrderType {
	case AlgoOrderTypeConditional, AlgoOrderTypeOCO:
		if request.TakeProfitPx == "" && request.StopLossPx == "" {
			return validationError("%s order requires tpTriggerPx or slTriggerPx", request.OrderType)
		}
		if request.OrderType == AlgoOrderTypeOCO &&
			(request.TakeProfitPx == "" || request.StopLossPx == "") {
			return validationError("oco order requires both tpTriggerPx and slTriggerPx")
		}
	case AlgoOrderTypeTrigger:
		if err := validatePositiveDecimal("triggerPx", request.TriggerPx); err != nil {
			return err
		}
		if request.OrderPx == "" {
			return validationError("trigger order requires orderPx (-1 for market)")
		}
	case AlgoOrderTypeMoveOrderStop:
		if (request.CallbackRatio == "") == (request.CallbackSpread == "") {
			return validationError("trailing order requires exactly one of callbackRatio or callbackSpread")
		}
	case AlgoOrderTypeTWAP:
		if request.SizeLimit == "" || request.PxLimit == "" || request.TimeInterval == "" {
			return validationError("twap order requires szLimit, pxLimit and timeInterval")
		}
		if (request.PxVar == "") == (request.PxSpread == "") {
			return validationError("twap order requires exactly one of pxVar or pxSpread")
		}
	}
	for name, value := range map[string]string{
		"tpTriggerPx": request.TakeProfitPx, "tpOrdPx": request.TakeProfitOrder,
		"slTriggerPx": request.StopLossPx, "slOrdPx": request.StopLossOrder,
		"closeFraction": request.CloseFraction, "ccy": request.Currency,
	} {
		if err := validateOptionalText(name, value); err != nil {
			return err
		}
	}
	return nil
}

func (request CancelAlgoOrderRequest) validate() error {
	if err := validateRequiredText("instId", request.InstrumentID); err != nil {
		return err
	}
	return validateRequiredText("algoId", request.AlgoID)
}

func (request CancelAlgoOrdersRequest) validate() error {
	if len(request.Orders) == 0 || len(request.Orders) > maxCancelAlgoOrders {
		return validationError("algo cancel count must be between 1 and %d", maxCancelAlgoOrders)
	}
	for _, order := range request.Orders {
		if err := order.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (request AmendAlgoOrderRequest) validate() error {
	if err := validateRequiredText("instId", request.InstrumentID); err != nil {
		return err
	}
	if err := validateAlgoIdentity(request.AlgoID, request.ClientAlgoID); err != nil {
		return err
	}
	if request.RequestID != "" && !requestIDPattern.MatchString(request.RequestID) {
		return validationError("reqId has an invalid format")
	}
	if request.NewSize != "" {
		if err := validatePositiveDecimal("newSz", request.NewSize); err != nil {
			return err
		}
	}
	if request.NewSize == "" && request.NewTakeProfitPx == "" && request.NewStopLossPx == "" &&
		request.NewTriggerPx == "" && request.NewTakeProfitOrder == "" && request.NewStopLossOrder == "" &&
		request.NewOrderPx == "" {
		return validationError("algo amend requires at least one new value")
	}
	return nil
}

func (request AlgoOrderDetailRequest) validate() error {
	return validateAlgoIdentity(request.AlgoID, request.ClientAlgoID)
}

func (request AlgoOrderDetailRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "algoId", request.AlgoID)
	setIfNotEmpty(values, "algoClOrdId", request.ClientAlgoID)
	return values
}

func validateAlgoIdentity(algoID, clientAlgoID string) error {
	if err := validateOptionalText("algoId", algoID); err != nil {
		return err
	}
	if err := validateOptionalText("algoClOrdId", clientAlgoID); err != nil {
		return err
	}
	if algoID == "" && clientAlgoID == "" {
		return validationError("algoId or algoClOrdId is required")
	}
	if clientAlgoID != "" && !algoClientOrderIDPattern.MatchString(clientAlgoID) {
		return validationError("algoClOrdId has an invalid format")
	}
	return nil
}

func (request AlgoOrdersRequest) validate(history bool) error {
	if len(request.OrderTypes) == 0 {
		return validationError("ordType is required")
	}
	for _, orderType := range request.OrderTypes {
		if !orderType.valid() {
			return validationError("unsupported algo order type %q", orderType)
		}
	}
	if len(request.OrderTypes) > 1 {
		pair := len(request.OrderTypes) == 2 &&
			(request.OrderTypes[0] == AlgoOrderTypeConditional && request.OrderTypes[1] == AlgoOrderTypeOCO ||
				request.OrderTypes[0] == AlgoOrderTypeOCO && request.OrderTypes[1] == AlgoOrderTypeConditional)
		if !pair {
			return validationError("only conditional and oco can be combined in one algo order query")
		}
	}
	if request.InstrumentType != "" {
		if err := validateInstrumentType(request.InstrumentType); err != nil {
			return err
		}
	}
	for name, value := range map[string]string{
		"state": request.State, "algoId": request.AlgoID, "instId": request.InstrumentID,
	} {
		if err := validateOptionalText(name, value); err != nil {
			return err
		}
	}
	if history && request.State == "" && request.AlgoID == "" {
		return validationError("algo order history requires state or algoId")
	}
	if !history && request.State != "" {
		return validationError("pending algo order query does not accept state")
	}
	return validateOrderPage(request.After, request.Before, request.Limit)
}

func (request AlgoOrdersRequest) values(history bool) url.Values {
	values := make(url.Values)
	types := make([]string, len(request.OrderTypes))
	for index, orderType := range request.OrderTypes {
		types[index] = string(orderType)
	}
	values.Set("ordType", strings.Join(types, ","))
	if history {
		setIfNotEmpty(values, "state", request.State)
	}
	setIfNotEmpty(values, "algoId", request.AlgoID)
	setIfNotEmpty(values, "instType", string(request.InstrumentType))
	setIfNotEmpty(values, "instId", request.InstrumentID)
	setIfNotEmpty(values, "after", request.After)
	setIfNotEmpty(values, "before", request.Before)
	setPositiveInt(values, "limit", request.Limit)
	return values
}
