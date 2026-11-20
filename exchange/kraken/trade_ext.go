package kraken

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const (
	maxBatchOrders       = 15
	minBatchOrders       = 2
	maxBatchCancelIDs    = 50
	maxCancelAfterSecond = 86400
)

// 일괄 주문의 조건부 주문과 conditional close에서 쓰는 추가 주문 종류다.
const (
	OrderTypeIceberg           OrderType = "iceberg"
	OrderTypeStopLoss          OrderType = "stop-loss"
	OrderTypeTakeProfit        OrderType = "take-profit"
	OrderTypeStopLossLimit     OrderType = "stop-loss-limit"
	OrderTypeTakeProfitLimit   OrderType = "take-profit-limit"
	OrderTypeTrailingStop      OrderType = "trailing-stop"
	OrderTypeTrailingStopLimit OrderType = "trailing-stop-limit"
)

// TimeInForceGTD는 expiretm까지 유지하는 주문이다. 일괄 주문에서 사용한다.
const TimeInForceGTD TimeInForce = "GTD"

// PriceTrigger는 조건부 주문을 활성화하는 가격 기준이다.
type PriceTrigger string

const (
	PriceTriggerLast  PriceTrigger = "last"
	PriceTriggerIndex PriceTrigger = "index"
)

// SelfTradePrevention은 자기 체결이 발생할 때 만료시킬 주문을 정한다.
type SelfTradePrevention string

const (
	SelfTradeCancelNewest SelfTradePrevention = "cancel-newest"
	SelfTradeCancelOldest SelfTradePrevention = "cancel-oldest"
	SelfTradeCancelBoth   SelfTradePrevention = "cancel-both"
)

var (
	priceExpressionPattern = regexp.MustCompile(`^[+\-#]?[0-9]+(?:\.[0-9]+)?%?$`)
	scheduleTimePattern    = regexp.MustCompile(`^\+?[0-9]+$`)
)

// AmendOrderRequest는 대기열 위치를 유지하는 원자적 주문 정정 조건이다.
// TransactionID 또는 ClientOrderID 중 정확히 하나가 필요하다.
type AmendOrderRequest struct {
	TransactionID   string
	ClientOrderID   string
	Quantity        string
	DisplayQuantity string
	// LimitPrice와 TriggerPrice는 +, - 접두사와 % 접미사로 상대 가격을 지정할 수 있다.
	LimitPrice   string
	TriggerPrice string
	// Pair는 non-crypto 상품 정정에서만 필요하다.
	Pair string
	// PostOnly가 true이면 limit 가격 정정이 passive로 등록되지 못할 때 거부된다.
	PostOnly bool
	// Deadline은 RFC3339 시각이며 이 시각 이후 도착한 요청은 엔진이 거부한다.
	Deadline string
}

// AmendResult는 주문 정정 접수 결과다.
type AmendResult struct {
	AmendID string          `json:"amend_id"`
	Raw     json.RawMessage `json:"-"`
}

// EditOrderRequest는 기존 주문을 취소하고 새 주문으로 다시 등록하는 레거시 정정 조건이다.
type EditOrderRequest struct {
	TransactionID string
	Pair          string
	Volume        string
	DisplayVolume string
	Price         string
	Price2        string
	// PostOnly는 oflags=post로 전달한다. 변경할 수 있는 유일한 주문 flag다.
	PostOnly      bool
	UserReference int32
	AssetClass    AssetClass
	Deadline      string
	// CancelResponse가 true이면 주문 교체가 끝나기 전에 pending replace 응답을 받는다.
	CancelResponse bool
	ValidateOnly   bool
}

// EditOrderResult는 주문 교체 결과다. 새 주문 ID는 TransactionID다.
type EditOrderResult struct {
	Status                string           `json:"status"`
	TransactionID         string           `json:"txid"`
	OriginalTransactionID string           `json:"originaltxid"`
	Volume                string           `json:"volume"`
	Price                 string           `json:"price"`
	Price2                string           `json:"price2"`
	OrdersCancelled       int              `json:"orders_cancelled"`
	Description           OrderDescription `json:"descr"`
	ErrorMessage          string           `json:"error_message"`
	Raw                   json.RawMessage  `json:"-"`
}

// BatchOrderClose는 일괄 주문에 붙이는 conditional close 조건이다.
type BatchOrderClose struct {
	OrderType OrderType
	Price     string
	Price2    string
}

// BatchOrder는 일괄 주문의 주문 한 건이다.
type BatchOrder struct {
	ClientOrderID       string
	UserReference       int32
	OrderType           OrderType
	Side                Side
	Volume              string
	DisplayVolume       string
	Price               string
	Price2              string
	Trigger             PriceTrigger
	Leverage            string
	ReduceOnly          bool
	SelfTradePrevention SelfTradePrevention
	PostOnly            bool
	VolumeInQuote       bool
	TimeInForce         TimeInForce
	// StartTime과 ExpireTime은 Unix 시각 또는 +초 형식이다.
	StartTime  string
	ExpireTime string
	Close      *BatchOrderClose
}

// AddOrderBatchRequest는 단일 상품에 대한 2~15개 주문 일괄 등록 조건이다.
type AddOrderBatchRequest struct {
	Pair       string
	Orders     []BatchOrder
	AssetClass AssetClass
	Deadline   string
	// ValidateOnly가 true이면 검증만 하고 매칭 엔진에 제출하지 않는다.
	ValidateOnly bool
}

// BatchOrderResult는 일괄 주문 중 한 건의 접수 결과다. 요청 순서와 같은 순서로 반환된다.
type BatchOrderResult struct {
	TransactionID string           `json:"txid"`
	Description   OrderDescription `json:"descr"`
	Error         string           `json:"error"`
}

// BatchOrderReference는 일괄 주문 접수 결과다.
type BatchOrderReference struct {
	Orders []BatchOrderResult `json:"orders"`
	Raw    json.RawMessage    `json:"-"`
}

// CancelOrderBatchRequest는 거래소 주문 ID(또는 userref)와 client order ID로 최대 50건을 한 번에 취소한다.
type CancelOrderBatchRequest struct {
	TransactionIDs []string
	ClientOrderIDs []string
}

// CancelAllOrdersAfterRequest는 dead man's switch 타이머 설정이다.
// Timeout은 1초 단위이며 86400초 미만이다. 0이면 타이머를 해제한다.
type CancelAllOrdersAfterRequest struct {
	Timeout time.Duration
}

// DeadManSwitch는 타이머 설정 결과다. 시각은 RFC3339 원문이다.
type DeadManSwitch struct {
	CurrentTime string          `json:"currentTime"`
	TriggerTime string          `json:"triggerTime"`
	Raw         json.RawMessage `json:"-"`
}

func (request AmendOrderRequest) validate() error {
	if (request.TransactionID == "") == (request.ClientOrderID == "") {
		return validationError("amend request requires exactly one transaction ID or client order ID")
	}
	if request.TransactionID != "" && !transactionIDPattern.MatchString(request.TransactionID) {
		return validationError("transaction ID contains unsupported characters")
	}
	if err := validateClientOrderID(request.ClientOrderID); err != nil {
		return err
	}
	if request.Quantity == "" && request.DisplayQuantity == "" &&
		request.LimitPrice == "" && request.TriggerPrice == "" {
		return validationError("amend request requires at least one field to change")
	}
	if err := validateOptionalDecimal("order quantity", request.Quantity); err != nil {
		return err
	}
	if err := validateOptionalDecimal("display quantity", request.DisplayQuantity); err != nil {
		return err
	}
	if err := validateOptionalPriceExpression("limit price", request.LimitPrice); err != nil {
		return err
	}
	if err := validateOptionalPriceExpression("trigger price", request.TriggerPrice); err != nil {
		return err
	}
	if request.PostOnly && request.LimitPrice == "" {
		return validationError("post-only applies to limit price amends only")
	}
	if request.Pair != "" {
		if err := validatePair(request.Pair); err != nil {
			return err
		}
	}
	return validateDeadline(request.Deadline)
}

func (request AmendOrderRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "txid", request.TransactionID)
	setIfNotEmpty(values, "cl_ord_id", request.ClientOrderID)
	setIfNotEmpty(values, "order_qty", request.Quantity)
	setIfNotEmpty(values, "display_qty", request.DisplayQuantity)
	setIfNotEmpty(values, "limit_price", request.LimitPrice)
	setIfNotEmpty(values, "trigger_price", request.TriggerPrice)
	setIfNotEmpty(values, "pair", request.Pair)
	setBool(values, "post_only", request.PostOnly)
	setIfNotEmpty(values, "deadline", request.Deadline)
	return values
}

func (request EditOrderRequest) validate() error {
	if !transactionIDPattern.MatchString(request.TransactionID) {
		return validationError("transaction ID is required and contains unsupported characters")
	}
	if err := validatePair(request.Pair); err != nil {
		return err
	}
	if request.Volume == "" && request.DisplayVolume == "" && request.Price == "" &&
		request.Price2 == "" && !request.PostOnly {
		return validationError("edit request requires at least one field to change")
	}
	if err := validateOptionalDecimal("volume", request.Volume); err != nil {
		return err
	}
	if err := validateOptionalDecimal("display volume", request.DisplayVolume); err != nil {
		return err
	}
	if err := validateOptionalPriceExpression("price", request.Price); err != nil {
		return err
	}
	if err := validateOptionalPriceExpression("price2", request.Price2); err != nil {
		return err
	}
	if request.UserReference < 0 {
		return validationError("user reference cannot be negative")
	}
	if request.AssetClass != "" && request.AssetClass != AssetClassTokenizedAsset {
		return validationError("edit asset class must be tokenized_asset or empty")
	}
	return validateDeadline(request.Deadline)
}

func (request EditOrderRequest) values() url.Values {
	values := url.Values{"txid": {request.TransactionID}, "pair": {request.Pair}}
	setIfNotEmpty(values, "volume", request.Volume)
	setIfNotEmpty(values, "displayvol", request.DisplayVolume)
	setIfNotEmpty(values, "price", request.Price)
	setIfNotEmpty(values, "price2", request.Price2)
	if request.PostOnly {
		values.Set("oflags", "post")
	}
	if request.UserReference > 0 {
		values.Set("userref", strconv.FormatInt(int64(request.UserReference), 10))
	}
	setAssetClass(values, "asset_class", request.AssetClass)
	setIfNotEmpty(values, "deadline", request.Deadline)
	setBool(values, "cancel_response", request.CancelResponse)
	setBool(values, "validate", request.ValidateOnly)
	return values
}

func (order BatchOrderClose) validate() error {
	switch order.OrderType {
	case OrderTypeLimit, OrderTypeIceberg, OrderTypeStopLoss, OrderTypeTakeProfit,
		OrderTypeStopLossLimit, OrderTypeTakeProfitLimit, OrderTypeTrailingStop,
		OrderTypeTrailingStopLimit:
	default:
		return validationError("unsupported conditional close order type %q", order.OrderType)
	}
	if !priceExpressionPattern.MatchString(order.Price) {
		return validationError("conditional close price is required and invalid")
	}
	if needsSecondaryPrice(order.OrderType) {
		if !priceExpressionPattern.MatchString(order.Price2) {
			return validationError("conditional close price2 is required and invalid")
		}
	} else if order.Price2 != "" {
		return validationError("conditional close price2 is available for limit-trigger types only")
	}
	return nil
}

func (order BatchOrder) validate() error {
	switch order.OrderType {
	case OrderTypeMarket, OrderTypeLimit, OrderTypeIceberg, OrderTypeStopLoss, OrderTypeTakeProfit,
		OrderTypeStopLossLimit, OrderTypeTakeProfitLimit, OrderTypeTrailingStop,
		OrderTypeTrailingStopLimit:
	default:
		return validationError("unsupported batch order type %q", order.OrderType)
	}
	if order.Side != SideBuy && order.Side != SideSell {
		return validationError("order side must be buy or sell")
	}
	if err := validateDecimalAmount("order volume", order.Volume); err != nil {
		return err
	}
	if order.OrderType == OrderTypeMarket {
		if order.Price != "" || order.Price2 != "" {
			return validationError("market order cannot include price")
		}
	} else if !priceExpressionPattern.MatchString(order.Price) {
		return validationError("%s order price is required and invalid", order.OrderType)
	}
	if needsSecondaryPrice(order.OrderType) {
		if !priceExpressionPattern.MatchString(order.Price2) {
			return validationError("%s order price2 is required and invalid", order.OrderType)
		}
	} else if order.Price2 != "" {
		return validationError("price2 is available for limit-trigger order types only")
	}
	if order.DisplayVolume != "" {
		if order.OrderType != OrderTypeIceberg {
			return validationError("display volume is available for iceberg orders only")
		}
		if err := validateDecimalAmount("display volume", order.DisplayVolume); err != nil {
			return err
		}
	}
	if order.Trigger != "" && order.Trigger != PriceTriggerLast && order.Trigger != PriceTriggerIndex {
		return validationError("unsupported price trigger %q", order.Trigger)
	}
	if err := validateOptionalDecimal("leverage", order.Leverage); err != nil {
		return err
	}
	switch order.SelfTradePrevention {
	case "", SelfTradeCancelNewest, SelfTradeCancelOldest, SelfTradeCancelBoth:
	default:
		return validationError("unsupported self trade prevention %q", order.SelfTradePrevention)
	}
	if order.TimeInForce != "" && order.TimeInForce != TimeInForceGTC &&
		order.TimeInForce != TimeInForceIOC && order.TimeInForce != TimeInForceGTD {
		return validationError("unsupported batch time in force %q", order.TimeInForce)
	}
	if order.TimeInForce == TimeInForceGTD && order.ExpireTime == "" {
		return validationError("GTD order requires expire time")
	}
	if order.PostOnly && order.OrderType != OrderTypeLimit {
		return validationError("post-only is available for limit orders only")
	}
	if order.PostOnly && order.TimeInForce != "" && order.TimeInForce != TimeInForceGTC {
		return validationError("post-only order time in force must be GTC")
	}
	if order.VolumeInQuote && (order.OrderType != OrderTypeMarket || order.Side != SideBuy) {
		return validationError("quote currency volume is available only for market buy orders")
	}
	for _, value := range []string{order.StartTime, order.ExpireTime} {
		if value != "" && !scheduleTimePattern.MatchString(value) {
			return validationError("start and expire time must be a Unix time or +seconds")
		}
	}
	if order.ClientOrderID != "" && order.UserReference != 0 {
		return validationError("client order ID and user reference are mutually exclusive")
	}
	if err := validateClientOrderID(order.ClientOrderID); err != nil {
		return err
	}
	if order.Close != nil {
		return order.Close.validate()
	}
	return nil
}

func (order BatchOrder) body() map[string]any {
	body := map[string]any{
		"ordertype": string(order.OrderType), "type": string(order.Side), "volume": order.Volume,
	}
	setBodyString(body, "cl_ord_id", order.ClientOrderID)
	setBodyString(body, "displayvol", order.DisplayVolume)
	setBodyString(body, "price", order.Price)
	setBodyString(body, "price2", order.Price2)
	setBodyString(body, "trigger", string(order.Trigger))
	setBodyString(body, "leverage", order.Leverage)
	setBodyString(body, "stptype", string(order.SelfTradePrevention))
	setBodyString(body, "timeinforce", string(order.TimeInForce))
	setBodyString(body, "starttm", order.StartTime)
	setBodyString(body, "expiretm", order.ExpireTime)
	if order.UserReference != 0 {
		body["userref"] = order.UserReference
	}
	if order.ReduceOnly {
		body["reduce_only"] = true
	}
	flags := make([]string, 0, 2)
	if order.PostOnly {
		flags = append(flags, "post")
	}
	if order.VolumeInQuote {
		flags = append(flags, "viqc")
	}
	if len(flags) > 0 {
		body["oflags"] = strings.Join(flags, ",")
	}
	if order.Close != nil {
		closeBody := map[string]any{"ordertype": string(order.Close.OrderType), "price": order.Close.Price}
		setBodyString(closeBody, "price2", order.Close.Price2)
		body["close"] = closeBody
	}
	return body
}

func (request AddOrderBatchRequest) validate() error {
	if err := validatePair(request.Pair); err != nil {
		return err
	}
	if len(request.Orders) < minBatchOrders || len(request.Orders) > maxBatchOrders {
		return validationError("batch must contain between %d and %d orders", minBatchOrders, maxBatchOrders)
	}
	seen := make(map[string]struct{}, len(request.Orders))
	for _, order := range request.Orders {
		if err := order.validate(); err != nil {
			return err
		}
		if order.ClientOrderID != "" {
			if _, exists := seen[order.ClientOrderID]; exists {
				return validationError("duplicate client order ID %q", order.ClientOrderID)
			}
			seen[order.ClientOrderID] = struct{}{}
		}
	}
	if request.AssetClass != "" && request.AssetClass != AssetClassTokenizedAsset {
		return validationError("batch asset class must be tokenized_asset or empty")
	}
	return validateDeadline(request.Deadline)
}

func (request AddOrderBatchRequest) body() map[string]any {
	orders := make([]map[string]any, len(request.Orders))
	for index, order := range request.Orders {
		orders[index] = order.body()
	}
	body := map[string]any{"pair": request.Pair, "orders": orders}
	setBodyString(body, "asset_class", string(request.AssetClass))
	setBodyString(body, "deadline", request.Deadline)
	if request.ValidateOnly {
		body["validate"] = true
	}
	return body
}

func (request CancelOrderBatchRequest) validate() error {
	total := len(request.TransactionIDs) + len(request.ClientOrderIDs)
	if total == 0 || total > maxBatchCancelIDs {
		return validationError("batch cancel requires between 1 and %d IDs", maxBatchCancelIDs)
	}
	seen := make(map[string]struct{}, total)
	for _, transactionID := range request.TransactionIDs {
		if !transactionIDPattern.MatchString(transactionID) {
			return validationError("transaction ID contains unsupported characters")
		}
		if _, exists := seen[transactionID]; exists {
			return validationError("duplicate transaction ID %q", transactionID)
		}
		seen[transactionID] = struct{}{}
	}
	for _, clientOrderID := range request.ClientOrderIDs {
		if clientOrderID == "" {
			return validationError("client order ID cannot be empty")
		}
		if err := validateClientOrderID(clientOrderID); err != nil {
			return err
		}
		if _, exists := seen[clientOrderID]; exists {
			return validationError("duplicate client order ID %q", clientOrderID)
		}
		seen[clientOrderID] = struct{}{}
	}
	return nil
}

func (request CancelOrderBatchRequest) body() map[string]any {
	body := make(map[string]any, 2)
	if len(request.TransactionIDs) > 0 {
		body["orders"] = append([]string(nil), request.TransactionIDs...)
	}
	if len(request.ClientOrderIDs) > 0 {
		body["cl_ord_ids"] = append([]string(nil), request.ClientOrderIDs...)
	}
	return body
}

func (request CancelAllOrdersAfterRequest) validate() error {
	if request.Timeout < 0 || request.Timeout >= maxCancelAfterSecond*time.Second {
		return validationError("cancel-after timeout must be between 0 and 86399 seconds")
	}
	if request.Timeout%time.Second != 0 {
		return validationError("cancel-after timeout must be a whole number of seconds")
	}
	return nil
}

func (request CancelAllOrdersAfterRequest) values() url.Values {
	return url.Values{"timeout": {strconv.FormatInt(int64(request.Timeout/time.Second), 10)}}
}

// AmendOrder는 주문 ID와 대기열 위치를 유지한 채 수량과 가격을 제자리에서 정정한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) AmendOrder(
	ctx context.Context,
	request AmendOrderRequest,
	options ...trade.RequestOption,
) (AmendResult, error) {
	if err := request.validate(); err != nil {
		return AmendResult{}, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"AmendOrder", request.values(), credential.PermissionTrade,
		commonexchange.OperationMutation, limitTrading, request.Pair, options...,
	)
	if err != nil {
		return AmendResult{}, err
	}
	var amended AmendResult
	if err := client.decodeResultInto(response, commonexchange.OperationMutation, result, &amended); err != nil {
		return AmendResult{}, err
	}
	if amended.AmendID == "" {
		return AmendResult{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Kraken amend acknowledgement is empty"),
		)
	}
	amended.Raw = cloneBytes(result)
	return amended, nil
}

// EditOrder는 기존 주문을 취소하고 조정한 조건의 새 주문을 등록한다. 새 주문 ID가 발급된다.
// 대기열 위치를 유지해야 하면 AmendOrder를 사용한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) EditOrder(
	ctx context.Context,
	request EditOrderRequest,
	options ...trade.RequestOption,
) (EditOrderResult, error) {
	if err := request.validate(); err != nil {
		return EditOrderResult{}, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"EditOrder", request.values(), credential.PermissionTrade,
		commonexchange.OperationMutation, limitTrading, request.Pair, options...,
	)
	if err != nil {
		return EditOrderResult{}, err
	}
	var edited EditOrderResult
	if err := client.decodeResultInto(response, commonexchange.OperationMutation, result, &edited); err != nil {
		return EditOrderResult{}, err
	}
	if !request.ValidateOnly && edited.TransactionID == "" {
		return EditOrderResult{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Kraken edit acknowledgement is empty"),
		)
	}
	edited.Raw = cloneBytes(result)
	return edited, nil
}

// AddOrderBatch는 단일 상품에 대한 2~15개 주문을 한 번에 등록한다.
// 일괄 검증이 실패하면 전체가 거부되고, 엔진 사전 검사(잔고 등)는 주문별로 실패할 수 있다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) AddOrderBatch(
	ctx context.Context,
	request AddOrderBatchRequest,
	options ...trade.RequestOption,
) (BatchOrderReference, error) {
	if err := request.validate(); err != nil {
		return BatchOrderReference{}, err
	}
	response, result, err := client.privateJSONResult(
		ctx, privatePrefix+"AddOrderBatch", request.body(), credential.PermissionTrade,
		commonexchange.OperationMutation, limitTrading, request.Pair, options...,
	)
	if err != nil {
		return BatchOrderReference{}, err
	}
	var reference BatchOrderReference
	if err := client.decodeResultInto(response, commonexchange.OperationMutation, result, &reference); err != nil {
		return BatchOrderReference{}, err
	}
	if !request.ValidateOnly && len(reference.Orders) != len(request.Orders) {
		return BatchOrderReference{}, client.decodeBodyError(
			response, commonexchange.OperationMutation,
			errors.New("Kraken batch order acknowledgement count does not match request"),
		)
	}
	reference.Raw = cloneBytes(result)
	return reference, nil
}

// CancelOrderBatch는 거래소 주문 ID(또는 userref)와 client order ID로 최대 50건을 한 번에 취소한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) CancelOrderBatch(
	ctx context.Context,
	request CancelOrderBatchRequest,
	options ...trade.RequestOption,
) (CancelResult, error) {
	if err := request.validate(); err != nil {
		return CancelResult{}, err
	}
	response, result, err := client.privateJSONResult(
		ctx, privatePrefix+"CancelOrderBatch", request.body(), credential.PermissionTrade,
		commonexchange.OperationMutation, limitPrivate, "", options...,
	)
	if err != nil {
		return CancelResult{}, err
	}
	var canceled CancelResult
	if err := client.decodeResultInto(response, commonexchange.OperationMutation, result, &canceled); err != nil {
		return CancelResult{}, err
	}
	if canceled.Count < 1 && !canceled.Pending {
		return CancelResult{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Kraken batch cancel acknowledgement is empty"),
		)
	}
	canceled.Raw = cloneBytes(result)
	return canceled, nil
}

// CancelAll은 계정의 모든 미체결 주문을 취소한다. 취소할 주문이 없으면 Count가 0이다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) CancelAll(
	ctx context.Context,
	options ...trade.RequestOption,
) (CancelResult, error) {
	response, result, err := client.privateResult(
		ctx, privatePrefix+"CancelAll", nil, credential.PermissionTrade,
		commonexchange.OperationMutation, limitPrivate, "", options...,
	)
	if err != nil {
		return CancelResult{}, err
	}
	var canceled CancelResult
	if err := client.decodeResultInto(response, commonexchange.OperationMutation, result, &canceled); err != nil {
		return CancelResult{}, err
	}
	canceled.Raw = cloneBytes(result)
	return canceled, nil
}

// CancelAllOrdersAfter는 dead man's switch 타이머를 설정하거나 연장하거나 해제한다.
// 타이머가 만료되면 계정의 모든 주문이 취소된다. 15~30초마다 60초 timeout으로 호출하는 것이 권장된다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) CancelAllOrdersAfter(
	ctx context.Context,
	request CancelAllOrdersAfterRequest,
	options ...trade.RequestOption,
) (DeadManSwitch, error) {
	if err := request.validate(); err != nil {
		return DeadManSwitch{}, err
	}
	response, result, err := client.privateResult(
		ctx, privatePrefix+"CancelAllOrdersAfter", request.values(), credential.PermissionTrade,
		commonexchange.OperationMutation, limitPrivate, "", options...,
	)
	if err != nil {
		return DeadManSwitch{}, err
	}
	var switchState DeadManSwitch
	if err := client.decodeResultInto(response, commonexchange.OperationMutation, result, &switchState); err != nil {
		return DeadManSwitch{}, err
	}
	switchState.Raw = cloneBytes(result)
	return switchState, nil
}

func needsSecondaryPrice(orderType OrderType) bool {
	return orderType == OrderTypeStopLossLimit || orderType == OrderTypeTakeProfitLimit ||
		orderType == OrderTypeTrailingStopLimit
}

func validateOptionalPriceExpression(name, value string) error {
	if value == "" {
		return nil
	}
	if !priceExpressionPattern.MatchString(value) {
		return validationError("%s must be a decimal string with optional +, -, # prefix and %% suffix", name)
	}
	return nil
}

func setBodyString(body map[string]any, key, value string) {
	if value != "" {
		body[key] = value
	}
}
