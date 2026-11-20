package bybit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// maxBatchOrders는 Spot·Linear 일괄 요청 하나에 담을 수 있는 최대 주문 수다.
const maxBatchOrders = 10

// TriggerBy는 조건부 주문과 TP/SL의 트리거 가격 기준이다.
type TriggerBy string

const (
	TriggerByLastPrice  TriggerBy = "LastPrice"
	TriggerByIndexPrice TriggerBy = "IndexPrice"
	TriggerByMarkPrice  TriggerBy = "MarkPrice"
)

// TPSLMode는 TP/SL이 포지션 전체에 적용되는지 일부에 적용되는지 나타낸다.
type TPSLMode string

const (
	TPSLModeFull    TPSLMode = "Full"
	TPSLModePartial TPSLMode = "Partial"
)

// SMPType은 자전거래 방지(SMP) 체결 정책이다.
type SMPType string

const (
	SMPTypeNone        SMPType = "None"
	SMPTypeCancelMaker SMPType = "CancelMaker"
	SMPTypeCancelTaker SMPType = "CancelTaker"
	SMPTypeCancelBoth  SMPType = "CancelBoth"
)

// OrderFilter는 Spot 주문 종류이며, 전체 취소에서는 취소할 주문 종류다.
type OrderFilter string

const (
	// OrderFilterOrder는 일반 주문이다.
	OrderFilterOrder OrderFilter = "Order"
	// OrderFilterTPSLOrder는 Spot TP/SL 주문이다.
	OrderFilterTPSLOrder OrderFilter = "tpslOrder"
	// OrderFilterStopOrder는 조건부 주문이다.
	OrderFilterStopOrder OrderFilter = "StopOrder"
	// OrderFilterOcoOrder는 Spot OCO 주문이며 전체 취소에서만 쓴다.
	OrderFilterOcoOrder OrderFilter = "OcoOrder"
	// OrderFilterBidirectionalTPSLOrder는 Spot 양방향 TP/SL 주문이며 전체 취소에서만 쓴다.
	OrderFilterBidirectionalTPSLOrder OrderFilter = "BidirectionalTpslOrder"
	// OrderFilterOpenOrder는 Linear 전체 취소에서만 쓰는 주문 종류다.
	OrderFilterOpenOrder OrderFilter = "OpenOrder"
)

// StopOrderType은 전체 취소에서 취소할 조건부 주문의 세부 종류다.
type StopOrderType string

// StopOrderTypeStop은 TP/SL과 트레일링 스톱을 제외한 조건부 주문이다.
const StopOrderTypeStop StopOrderType = "Stop"

// MutationAck는 result 본문이 없거나 비어 있는 설정·변경 요청의 접수 결과다.
// 성공 응답은 접수이며 반영 결과는 조회 API 또는 private stream으로 확인해야 한다.
type MutationAck struct {
	Raw json.RawMessage
}

// AmendOrderRequest는 미체결 또는 부분 체결 주문의 수량·가격·TP/SL·트리거 가격 정정 요청이다.
// 수정하지 않을 필드는 비워 둔다. TakeProfit과 StopLoss를 "0"으로 보내면 기존 TP/SL을 취소한다.
// 일괄 정정(AmendBatchOrders)의 항목으로 쓸 때는 Category를 비워 두면 일괄 요청의 Category를 따른다.
type AmendOrderRequest struct {
	Category     Category  `json:"category,omitempty"`
	Symbol       string    `json:"symbol"`
	OrderID      string    `json:"orderId,omitempty"`
	OrderLinkID  string    `json:"orderLinkId,omitempty"`
	TriggerPrice string    `json:"triggerPrice,omitempty"`
	Quantity     string    `json:"qty,omitempty"`
	Price        string    `json:"price,omitempty"`
	TPSLMode     TPSLMode  `json:"tpslMode,omitempty"`
	TakeProfit   string    `json:"takeProfit,omitempty"`
	StopLoss     string    `json:"stopLoss,omitempty"`
	TPTriggerBy  TriggerBy `json:"tpTriggerBy,omitempty"`
	SLTriggerBy  TriggerBy `json:"slTriggerBy,omitempty"`
	TriggerBy    TriggerBy `json:"triggerBy,omitempty"`
	TPLimitPrice string    `json:"tpLimitPrice,omitempty"`
	SLLimitPrice string    `json:"slLimitPrice,omitempty"`
}

// CancelAllOrdersRequest는 조건에 맞는 미체결 주문 전체 취소 요청이다.
// symbol, baseCoin, settleCoin을 함께 지정하면 Bybit는 symbol, baseCoin, settleCoin 순서로 하나만 처리한다.
// Linear는 셋 중 하나가 필수이고, Spot은 settleCoin을 지원하지 않으며 모두 비우면 전체 Spot 일반 주문을 취소한다.
// OrderFilter를 비우면 Spot은 일반 주문(Order)만, Linear는 조건부·TP/SL·트레일링 스톱을 포함한 모든 주문을 취소한다.
type CancelAllOrdersRequest struct {
	Category      Category      `json:"category"`
	Symbol        string        `json:"symbol,omitempty"`
	BaseCoin      string        `json:"baseCoin,omitempty"`
	SettleCoin    string        `json:"settleCoin,omitempty"`
	OrderFilter   OrderFilter   `json:"orderFilter,omitempty"`
	StopOrderType StopOrderType `json:"stopOrderType,omitempty"`
}

// CancelAllOrdersResult는 전체 취소로 접수된 주문 목록이다.
// Success는 Bybit가 돌려준 "1"(성공) 또는 "0"(실패) 문자열이다.
type CancelAllOrdersResult struct {
	Orders  []OrderReference
	Success string
	Raw     json.RawMessage
}

// BatchPlaceOrdersRequest는 한 번에 최대 10건의 주문을 생성하는 요청이다.
// 각 항목의 Category는 비워 두거나 일괄 요청의 Category와 같아야 한다.
type BatchPlaceOrdersRequest struct {
	Category Category
	Orders   []PlaceOrderRequest
}

// BatchAmendOrdersRequest는 한 번에 최대 10건의 주문을 정정하는 요청이다.
// 각 항목의 Category는 비워 두거나 일괄 요청의 Category와 같아야 한다.
type BatchAmendOrdersRequest struct {
	Category Category
	Orders   []AmendOrderRequest
}

// BatchCancelOrdersRequest는 한 번에 최대 10건의 주문을 취소하는 요청이다.
// 각 항목의 Category는 비워 두거나 일괄 요청의 Category와 같아야 한다.
type BatchCancelOrdersRequest struct {
	Category Category
	Orders   []CancelOrderRequest
}

// BatchOrderItem은 일괄 요청에 담은 주문 한 건의 처리 결과다.
// Code와 Message는 응답 retExtInfo.list의 같은 위치 항목에서 가져오며 Code가 0이면 해당 주문이 접수된 것이다.
type BatchOrderItem struct {
	Category    Category        `json:"category"`
	Symbol      string          `json:"symbol"`
	OrderID     string          `json:"orderId"`
	OrderLinkID string          `json:"orderLinkId"`
	CreatedTime string          `json:"createAt"`
	Code        int             `json:"-"`
	Message     string          `json:"-"`
	Raw         json.RawMessage `json:"-"`
}

// Succeeded는 해당 주문이 접수되었는지 반환한다.
func (item BatchOrderItem) Succeeded() bool {
	return item.Code == 0
}

// BatchOrderResult는 일괄 주문 생성·정정·취소 응답이다.
// Items[i]는 요청의 i번째 주문 결과이며, 일부 주문만 실패해도 요청 전체는 성공으로 반환되므로
// 호출자는 항목별 Succeeded를 확인해야 한다. Raw는 result와 retExtInfo를 포함한 응답 본문 전체다.
type BatchOrderResult struct {
	Items []BatchOrderItem
	Raw   json.RawMessage
}

// DisconnectCancelAllRequest는 연결 끊김 보호(DCP) 시간 창 설정 요청이다.
// Bybit 문서상 옵션(Unified Account) 주문에만 적용되며 시간 창은 3~300초다.
type DisconnectCancelAllRequest struct {
	TimeWindow int `json:"timeWindow"`
}

// SpotBorrowCheckRequest는 Spot 주문 가능 수량 조회 조건이다.
type SpotBorrowCheckRequest struct {
	Symbol string
	Side   Side
}

// SpotBorrowQuota는 Spot 현물·마진 거래에서 거래할 수 있는 최대 수량과 금액이다.
type SpotBorrowQuota struct {
	Symbol               string          `json:"symbol"`
	Side                 Side            `json:"side"`
	MaxTradeQuantity     string          `json:"maxTradeQty"`
	MaxTradeAmount       string          `json:"maxTradeAmount"`
	SpotMaxTradeQuantity string          `json:"spotMaxTradeQty"`
	SpotMaxTradeAmount   string          `json:"spotMaxTradeAmount"`
	BorrowCoin           string          `json:"borrowCoin"`
	Raw                  json.RawMessage `json:"-"`
}

// AmendOrder는 미체결 또는 부분 체결 주문을 정정한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) AmendOrder(
	ctx context.Context,
	request AmendOrderRequest,
	options ...trade.RequestOption,
) (OrderReference, error) {
	if err := request.validate(); err != nil {
		return OrderReference{}, err
	}
	response, result, err := client.executeTradeMutation(
		ctx, "/v5/order/amend", request, accountLimit(10), credential.PermissionTrade, options...,
	)
	if err != nil {
		return OrderReference{}, err
	}
	return decodeOrderReference(response, result, commonexchange.OperationMutation, client)
}

// CancelAllOrders는 조건에 맞는 미체결 주문을 모두 취소한다.
// 성공 응답은 취소 접수이며 최종 상태는 주문 조회 또는 private stream으로 확인해야 한다.
func (client *Client) CancelAllOrders(
	ctx context.Context,
	request CancelAllOrdersRequest,
	options ...trade.RequestOption,
) (CancelAllOrdersResult, error) {
	if err := request.validate(); err != nil {
		return CancelAllOrdersResult{}, err
	}
	response, result, err := client.executeTradeMutation(
		ctx, "/v5/order/cancel-all", request, accountLimit(10), credential.PermissionTrade, options...,
	)
	if err != nil {
		return CancelAllOrdersResult{}, err
	}
	var rawResult struct {
		Items   []json.RawMessage `json:"list"`
		Success json.RawMessage   `json:"success"`
	}
	if err := decodeResult(result, &rawResult); err != nil {
		return CancelAllOrdersResult{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	cancelled := CancelAllOrdersResult{
		Orders:  make([]OrderReference, len(rawResult.Items)),
		Success: strings.Trim(string(rawResult.Success), `"`),
		Raw:     cloneBytes(result),
	}
	for index, item := range rawResult.Items {
		if err := decodeResult(item, &cancelled.Orders[index]); err != nil {
			return CancelAllOrdersResult{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
		}
		cancelled.Orders[index].Raw = cloneBytes(item)
	}
	return cancelled, nil
}

// PlaceBatchOrders는 최대 10건의 주문을 한 번에 생성한다.
// 한도는 요청 수가 아니라 포함한 주문 수만큼 소비된다. 자동 재시도하지 않는다.
func (client *Client) PlaceBatchOrders(
	ctx context.Context,
	request BatchPlaceOrdersRequest,
	options ...trade.RequestOption,
) (BatchOrderResult, error) {
	if err := request.validate(); err != nil {
		return BatchOrderResult{}, err
	}
	response, result, err := client.executeTradeMutation(
		ctx, "/v5/order/create-batch", request.payload(),
		batchAccountLimit(10, len(request.Orders)), credential.PermissionTrade, options...,
	)
	if err != nil {
		return BatchOrderResult{}, err
	}
	return client.decodeBatchOrderResult(response, result)
}

// AmendBatchOrders는 최대 10건의 주문을 한 번에 정정한다.
// 한도는 요청 수가 아니라 포함한 주문 수만큼 소비된다. 자동 재시도하지 않는다.
func (client *Client) AmendBatchOrders(
	ctx context.Context,
	request BatchAmendOrdersRequest,
	options ...trade.RequestOption,
) (BatchOrderResult, error) {
	if err := request.validate(); err != nil {
		return BatchOrderResult{}, err
	}
	response, result, err := client.executeTradeMutation(
		ctx, "/v5/order/amend-batch", request.payload(),
		batchAccountLimit(10, len(request.Orders)), credential.PermissionTrade, options...,
	)
	if err != nil {
		return BatchOrderResult{}, err
	}
	return client.decodeBatchOrderResult(response, result)
}

// CancelBatchOrders는 최대 10건의 주문을 한 번에 취소한다.
// 한도는 요청 수가 아니라 포함한 주문 수만큼 소비된다. 자동 재시도하지 않는다.
func (client *Client) CancelBatchOrders(
	ctx context.Context,
	request BatchCancelOrdersRequest,
	options ...trade.RequestOption,
) (BatchOrderResult, error) {
	if err := request.validate(); err != nil {
		return BatchOrderResult{}, err
	}
	response, result, err := client.executeTradeMutation(
		ctx, "/v5/order/cancel-batch", request.payload(),
		batchAccountLimit(10, len(request.Orders)), credential.PermissionTrade, options...,
	)
	if err != nil {
		return BatchOrderResult{}, err
	}
	return client.decodeBatchOrderResult(response, result)
}

// SetDisconnectCancelAll은 연결 끊김 보호(DCP) 시간 창을 설정한다.
// Bybit 문서상 옵션 주문에만 적용되며, 반영까지 시간이 걸리므로 10초 뒤 조회하거나 다시 설정하는 것이 권장된다.
func (client *Client) SetDisconnectCancelAll(
	ctx context.Context,
	request DisconnectCancelAllRequest,
	options ...trade.RequestOption,
) (MutationAck, error) {
	if err := request.validate(); err != nil {
		return MutationAck{}, err
	}
	_, result, err := client.executeTradeMutation(
		ctx, "/v5/order/disconnected-cancel-all", request, accountLimit(5), credential.PermissionTrade, options...,
	)
	if err != nil {
		return MutationAck{}, err
	}
	return MutationAck{Raw: cloneBytes(result)}, nil
}

// SpotBorrowCheck는 Spot 심볼의 현물·마진 거래 가능 최대 수량과 금액을 조회한다.
func (client *Client) SpotBorrowCheck(
	ctx context.Context,
	request SpotBorrowCheckRequest,
	options ...trade.RequestOption,
) (SpotBorrowQuota, error) {
	if err := request.validate(); err != nil {
		return SpotBorrowQuota{}, err
	}
	response, result, err := client.executeTradeRead(
		ctx, "/v5/order/spot-borrow-check", request.values(), accountLimit(20), options...,
	)
	if err != nil {
		return SpotBorrowQuota{}, err
	}
	var quota SpotBorrowQuota
	if err := decodeResult(result, &quota); err != nil {
		return SpotBorrowQuota{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	quota.Raw = cloneBytes(result)
	return quota, nil
}

// executeTradeMutation은 서명된 POST 변경 요청을 한 번만 전송하고 result를 반환한다.
// 변경 요청은 자동 재시도하지 않는다.
func (client *Client) executeTradeMutation(
	ctx context.Context,
	path string,
	payload any,
	limit endpointLimit,
	permission credential.Permission,
	options ...trade.RequestOption,
) (commonexchange.Response, json.RawMessage, error) {
	body, err := encodeBody(payload)
	if err != nil {
		return commonexchange.Response{}, nil, err
	}
	response, _, err := client.executeSigned(
		ctx, http.MethodPost, path, nil, body, limit, permission, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return commonexchange.Response{}, nil, err
	}
	result, _, err := client.responseResult(response, commonexchange.OperationMutation)
	if err != nil {
		return commonexchange.Response{}, nil, err
	}
	return response, result, nil
}

// executeTradeRead는 서명된 GET 조회 요청을 보내고 result를 반환한다.
func (client *Client) executeTradeRead(
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
		return commonexchange.Response{}, nil, err
	}
	result, _, err := client.responseResult(response, commonexchange.OperationRead)
	if err != nil {
		return commonexchange.Response{}, nil, err
	}
	return response, result, nil
}

// decodeBatchOrderResult는 result.list와 retExtInfo.list를 같은 위치끼리 묶는다.
// 두 목록의 길이가 다르면 항목별 성공 여부를 알 수 없으므로 실행 상태 불명으로 처리한다.
func (client *Client) decodeBatchOrderResult(
	response commonexchange.Response,
	result json.RawMessage,
) (BatchOrderResult, error) {
	var rawResult struct {
		Items []json.RawMessage `json:"list"`
	}
	if err := decodeResult(result, &rawResult); err != nil {
		return BatchOrderResult{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	var envelope struct {
		ExtInfo struct {
			Items []struct {
				Code    int    `json:"code"`
				Message string `json:"msg"`
			} `json:"list"`
		} `json:"retExtInfo"`
	}
	if err := decodeResult(response.Body, &envelope); err != nil {
		return BatchOrderResult{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	if len(envelope.ExtInfo.Items) != len(rawResult.Items) {
		return BatchOrderResult{}, client.decodeBodyError(
			response, commonexchange.OperationMutation,
			fmt.Errorf(
				"Bybit batch result has %d items but retExtInfo has %d",
				len(rawResult.Items), len(envelope.ExtInfo.Items),
			),
		)
	}
	batch := BatchOrderResult{Items: make([]BatchOrderItem, len(rawResult.Items)), Raw: cloneBytes(response.Body)}
	for index, item := range rawResult.Items {
		if err := decodeResult(item, &batch.Items[index]); err != nil {
			return BatchOrderResult{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
		}
		batch.Items[index].Code = envelope.ExtInfo.Items[index].Code
		batch.Items[index].Message = envelope.ExtInfo.Items[index].Message
		batch.Items[index].Raw = cloneBytes(item)
	}
	return batch, nil
}

// batchPayload는 일괄 요청의 JSON 본문이다. 항목에서는 category를 제거하고 최상위에만 둔다.
type batchPayload[T any] struct {
	Category Category `json:"category"`
	Request  []T      `json:"request"`
}

func (request BatchPlaceOrdersRequest) payload() batchPayload[PlaceOrderRequest] {
	orders := slices.Clone(request.Orders)
	for index := range orders {
		orders[index].Category = ""
	}
	return batchPayload[PlaceOrderRequest]{Category: request.Category, Request: orders}
}

func (request BatchAmendOrdersRequest) payload() batchPayload[AmendOrderRequest] {
	orders := slices.Clone(request.Orders)
	for index := range orders {
		orders[index].Category = ""
	}
	return batchPayload[AmendOrderRequest]{Category: request.Category, Request: orders}
}

func (request BatchCancelOrdersRequest) payload() batchPayload[CancelOrderRequest] {
	orders := slices.Clone(request.Orders)
	for index := range orders {
		orders[index].Category = ""
	}
	return batchPayload[CancelOrderRequest]{Category: request.Category, Request: orders}
}

func (request BatchPlaceOrdersRequest) validate() error {
	if err := validateBatchEnvelope(request.Category, len(request.Orders)); err != nil {
		return err
	}
	for index, order := range request.Orders {
		if err := validateBatchItemCategory(index, request.Category, order.Category); err != nil {
			return err
		}
		order.Category = request.Category
		if err := order.validate(); err != nil {
			return fmt.Errorf("batch order %d: %w", index, err)
		}
	}
	return nil
}

func (request BatchAmendOrdersRequest) validate() error {
	if err := validateBatchEnvelope(request.Category, len(request.Orders)); err != nil {
		return err
	}
	for index, order := range request.Orders {
		if err := validateBatchItemCategory(index, request.Category, order.Category); err != nil {
			return err
		}
		order.Category = request.Category
		if err := order.validate(); err != nil {
			return fmt.Errorf("batch order %d: %w", index, err)
		}
	}
	return nil
}

func (request BatchCancelOrdersRequest) validate() error {
	if err := validateBatchEnvelope(request.Category, len(request.Orders)); err != nil {
		return err
	}
	for index, order := range request.Orders {
		if err := validateBatchItemCategory(index, request.Category, order.Category); err != nil {
			return err
		}
		order.Category = request.Category
		if err := order.validate(); err != nil {
			return fmt.Errorf("batch order %d: %w", index, err)
		}
	}
	return nil
}

func validateBatchEnvelope(category Category, count int) error {
	if err := validateCategory(category); err != nil {
		return err
	}
	if count < 1 || count > maxBatchOrders {
		return validationError("batch request must contain between 1 and %d orders", maxBatchOrders)
	}
	return nil
}

func validateBatchItemCategory(index int, batch, item Category) error {
	if item != "" && item != batch {
		return validationError("batch order %d category must match the batch category", index)
	}
	return nil
}

func (request AmendOrderRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateRequiredText("symbol", request.Symbol); err != nil {
		return err
	}
	if err := validateOrderIdentity(request.OrderID, request.OrderLinkID); err != nil {
		return err
	}
	if err := validateTradeDecimals(
		false,
		tradeDecimalField{"qty", request.Quantity},
		tradeDecimalField{"price", request.Price},
		tradeDecimalField{"triggerPrice", request.TriggerPrice},
		tradeDecimalField{"tpLimitPrice", request.TPLimitPrice},
		tradeDecimalField{"slLimitPrice", request.SLLimitPrice},
	); err != nil {
		return err
	}
	if err := validateTradeDecimals(
		true,
		tradeDecimalField{"takeProfit", request.TakeProfit},
		tradeDecimalField{"stopLoss", request.StopLoss},
	); err != nil {
		return err
	}
	if err := validateTradeTriggers(
		tradeTriggerField{"triggerBy", request.TriggerBy},
		tradeTriggerField{"tpTriggerBy", request.TPTriggerBy},
		tradeTriggerField{"slTriggerBy", request.SLTriggerBy},
	); err != nil {
		return err
	}
	if request.TPSLMode != "" {
		if !request.TPSLMode.valid() {
			return validationError("tpslMode must be Full or Partial")
		}
		if request.Category != CategoryLinear {
			return validationError("tpslMode is available only for linear category")
		}
	}
	if request.Quantity == "" && request.Price == "" && request.TriggerPrice == "" &&
		request.TPSLMode == "" && request.TakeProfit == "" && request.StopLoss == "" &&
		request.TPTriggerBy == "" && request.SLTriggerBy == "" && request.TriggerBy == "" &&
		request.TPLimitPrice == "" && request.SLLimitPrice == "" {
		return validationError("amend order requires at least one field to modify")
	}
	return nil
}

func (request CancelAllOrdersRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateOrderFilters(request.Symbol, request.BaseCoin, request.SettleCoin); err != nil {
		return err
	}
	if request.Category == CategorySpot && (request.BaseCoin != "" || request.SettleCoin != "") {
		return validationError("Spot cancel all does not support baseCoin or settleCoin")
	}
	if request.Category == CategoryLinear && request.Symbol == "" &&
		request.BaseCoin == "" && request.SettleCoin == "" {
		return validationError("linear cancel all requires symbol, baseCoin, or settleCoin")
	}
	switch request.OrderFilter {
	case "", OrderFilterOrder, OrderFilterStopOrder:
	case OrderFilterTPSLOrder, OrderFilterOcoOrder, OrderFilterBidirectionalTPSLOrder:
		if request.Category != CategorySpot {
			return validationError("order filter %q is available only for Spot", request.OrderFilter)
		}
	case OrderFilterOpenOrder:
		if request.Category != CategoryLinear {
			return validationError("order filter %q is available only for linear", request.OrderFilter)
		}
	default:
		return validationError("unsupported order filter %q", request.OrderFilter)
	}
	if request.StopOrderType != "" &&
		(request.StopOrderType != StopOrderTypeStop || request.Category != CategoryLinear ||
			request.OrderFilter != OrderFilterStopOrder) {
		return validationError("stopOrderType Stop is available only for linear StopOrder cancellation")
	}
	return nil
}

func (request DisconnectCancelAllRequest) validate() error {
	if request.TimeWindow < 3 || request.TimeWindow > 300 {
		return validationError("time window must be between 3 and 300 seconds")
	}
	return nil
}

func (request SpotBorrowCheckRequest) validate() error {
	if err := validateRequiredText("symbol", request.Symbol); err != nil {
		return err
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("side must be Buy or Sell")
	}
	return nil
}

func (request SpotBorrowCheckRequest) values() url.Values {
	values := categoryValues(CategorySpot)
	values.Set("symbol", request.Symbol)
	values.Set("side", string(request.Side))
	return values
}

// validateQuantity는 qty를 검증한다.
// Linear reduceOnly·closeOnTrigger 주문은 qty "0"으로 현재 심볼의 전체 포지션을 닫을 수 있다.
func (request PlaceOrderRequest) validateQuantity() error {
	if request.Category == CategoryLinear && request.ReduceOnly && request.CloseOnTrigger {
		return validateZeroOrPositiveDecimal("qty", request.Quantity)
	}
	return validatePositiveDecimal("qty", request.Quantity)
}

// validateTradeExtensions는 조건부 주문, TP/SL, Spot 전용 파라미터를 검증한다.
func (request PlaceOrderRequest) validateTradeExtensions() error {
	if request.IsLeverage != 0 && request.IsLeverage != 1 {
		return validationError("isLeverage must be 0 or 1")
	}
	if request.Category != CategorySpot && (request.IsLeverage != 0 || request.OrderFilter != "") {
		return validationError("isLeverage and orderFilter are available only for Spot orders")
	}
	if request.OrderFilter != "" && request.OrderFilter != OrderFilterOrder &&
		request.OrderFilter != OrderFilterTPSLOrder && request.OrderFilter != OrderFilterStopOrder {
		return validationError("order filter must be Order, tpslOrder, or StopOrder")
	}
	if request.Category == CategorySpot && (request.TriggerDirection != 0 || request.TriggerBy != "" ||
		request.TPTriggerBy != "" || request.SLTriggerBy != "" || request.TPSLMode != "") {
		return validationError(
			"Spot order does not accept triggerDirection, triggerBy, tpTriggerBy, slTriggerBy, or tpslMode",
		)
	}
	if request.TriggerDirection < 0 || request.TriggerDirection > 2 {
		return validationError("trigger direction must be 1 or 2")
	}
	if request.TriggerDirection != 0 && request.TriggerPrice == "" {
		return validationError("trigger direction requires triggerPrice")
	}
	if err := validateTradeDecimals(
		false,
		tradeDecimalField{"triggerPrice", request.TriggerPrice},
		tradeDecimalField{"takeProfit", request.TakeProfit},
		tradeDecimalField{"stopLoss", request.StopLoss},
		tradeDecimalField{"tpLimitPrice", request.TPLimitPrice},
		tradeDecimalField{"slLimitPrice", request.SLLimitPrice},
	); err != nil {
		return err
	}
	if err := validateTradeTriggers(
		tradeTriggerField{"triggerBy", request.TriggerBy},
		tradeTriggerField{"tpTriggerBy", request.TPTriggerBy},
		tradeTriggerField{"slTriggerBy", request.SLTriggerBy},
	); err != nil {
		return err
	}
	if request.TPSLMode != "" && !request.TPSLMode.valid() {
		return validationError("tpslMode must be Full or Partial")
	}
	for _, orderType := range []struct {
		name  string
		value OrderType
	}{{"tpOrderType", request.TPOrderType}, {"slOrderType", request.SLOrderType}} {
		if orderType.value != "" && orderType.value != OrderTypeMarket && orderType.value != OrderTypeLimit {
			return validationError("%s must be Market or Limit", orderType.name)
		}
	}
	if request.TPLimitPrice != "" && request.TPOrderType != OrderTypeLimit {
		return validationError("tpLimitPrice requires tpOrderType Limit")
	}
	if request.SLLimitPrice != "" && request.SLOrderType != OrderTypeLimit {
		return validationError("slLimitPrice requires slOrderType Limit")
	}
	if request.Category == CategorySpot {
		if request.TPOrderType == OrderTypeLimit && (request.TakeProfit == "" || request.TPLimitPrice == "") {
			return validationError("Spot limit take profit requires takeProfit and tpLimitPrice")
		}
		if request.SLOrderType == OrderTypeLimit && (request.StopLoss == "" || request.SLLimitPrice == "") {
			return validationError("Spot limit stop loss requires stopLoss and slLimitPrice")
		}
	} else if (request.TPOrderType == OrderTypeLimit || request.SLOrderType == OrderTypeLimit) &&
		request.TPSLMode != TPSLModePartial {
		return validationError("limit take profit or stop loss requires tpslMode Partial")
	}
	if request.ReduceOnly && (request.TakeProfit != "" || request.StopLoss != "") {
		return validationError("reduceOnly order does not accept takeProfit or stopLoss")
	}
	if request.SMPType != "" && !request.SMPType.valid() {
		return validationError("unsupported smpType %q", request.SMPType)
	}
	return nil
}

func (triggerBy TriggerBy) valid() bool {
	return triggerBy == TriggerByLastPrice || triggerBy == TriggerByIndexPrice || triggerBy == TriggerByMarkPrice
}

func (mode TPSLMode) valid() bool {
	return mode == TPSLModeFull || mode == TPSLModePartial
}

func (smpType SMPType) valid() bool {
	switch smpType {
	case SMPTypeNone, SMPTypeCancelMaker, SMPTypeCancelTaker, SMPTypeCancelBoth:
		return true
	default:
		return false
	}
}

// tradeDecimalField는 선택적 십진수 문자열 필드의 이름과 값이다.
type tradeDecimalField struct {
	name  string
	value string
}

// tradeTriggerField는 선택적 트리거 가격 기준 필드의 이름과 값이다.
type tradeTriggerField struct {
	name  string
	value TriggerBy
}

// validateTradeDecimals는 비어 있지 않은 필드가 십진수 문자열인지 검증한다.
// allowZero가 true면 0도 허용하며(TP/SL 취소 등), false면 양수만 허용한다.
func validateTradeDecimals(allowZero bool, fields ...tradeDecimalField) error {
	for _, field := range fields {
		if field.value == "" {
			continue
		}
		var err error
		if allowZero {
			err = validateZeroOrPositiveDecimal(field.name, field.value)
		} else {
			err = validatePositiveDecimal(field.name, field.value)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func validateTradeTriggers(fields ...tradeTriggerField) error {
	for _, field := range fields {
		if field.value != "" && !field.value.valid() {
			return validationError("%s must be LastPrice, IndexPrice, or MarkPrice", field.name)
		}
	}
	return nil
}

func validateZeroOrPositiveDecimal(name, value string) error {
	if !positiveDecimalPattern.MatchString(value) {
		return validationError("%s must be a non-negative decimal string", name)
	}
	return nil
}
