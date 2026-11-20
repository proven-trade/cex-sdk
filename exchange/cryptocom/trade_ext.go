package cryptocom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const (
	methodAmendOrder       = "private/amend-order"
	methodCancelAllOrders  = "private/cancel-all-orders"
	methodCreateOrderList  = "private/create-order-list"
	methodCancelOrderList  = "private/cancel-order-list"
	methodGetFeeRate       = "private/get-fee-rate"
	methodGetInstrumentFee = "private/get-instrument-fee-rate"
	maximumOrderListSize   = 10
	contingencyTypeList    = "LIST"
)

// CancelAllType은 cancel-all-orders가 취소할 주문 범위다.
// private/cancel-all-orders는 LIMIT·ALL만, private/advanced/cancel-all-orders는 TRIGGER도 허용한다.
type CancelAllType string

const (
	CancelAllLimit   CancelAllType = "LIMIT"
	CancelAllTrigger CancelAllType = "TRIGGER"
	CancelAllAll     CancelAllType = "ALL"
)

// AmendOrderRequest는 기존 주문의 가격·수량 정정 조건이다.
// 정정은 거래소 내부에서 취소 후 재생성으로 처리되며 수량 감소만 큐 우선순위를 유지한다.
type AmendOrderRequest struct {
	OrderID               string
	OriginalClientOrderID string
	NewClientOrderID      string
	NewPrice              string
	NewQuantity           string
}

// CancelAllOrdersRequest는 선택한 거래쌍 또는 전체 주문의 일괄 취소 조건이다.
type CancelAllOrdersRequest struct {
	InstrumentName string
	Type           CancelAllType
}

// CreateOrderListRequest는 1~10건의 LIMIT·MARKET 주문을 한 번에 생성하는 조건이다.
type CreateOrderListRequest struct {
	Orders []PlaceOrderRequest
}

// CancelOrderListItem은 주문 목록 취소 대상 한 건이다.
type CancelOrderListItem struct {
	InstrumentName string
	OrderID        string
	ClientOrderID  string
}

// CancelOrderListRequest는 여러 주문을 한 번에 취소하는 조건이다.
type CancelOrderListRequest struct {
	Orders []CancelOrderListItem
}

// InstrumentFeeRateRequest는 상품별 수수료율 조회 조건이다.
type InstrumentFeeRateRequest struct {
	InstrumentName string
}

// OrderListResult는 주문 목록 요청에서 항목별 접수 결과다.
// 항목별 Code가 0이 아니면 해당 주문만 거부된 것이며 전체 호출은 성공이다.
type OrderListResult struct {
	Code          Integer         `json:"code"`
	Index         Integer         `json:"index"`
	ClientOrderID string          `json:"client_oid"`
	OrderID       Scalar          `json:"order_id"`
	Message       string          `json:"message"`
	Raw           json.RawMessage `json:"-"`
}

// FeeRate는 계정 30일 거래량 등급과 유효 수수료율(bps)이다.
type FeeRate struct {
	SpotTier               Integer         `json:"spot_tier"`
	DerivativesTier        Integer         `json:"deriv_tier"`
	EffectiveSpotMakerBps  Decimal         `json:"effective_spot_maker_rate_bps"`
	EffectiveSpotTakerBps  Decimal         `json:"effective_spot_taker_rate_bps"`
	EffectiveDerivMakerBps Decimal         `json:"effective_deriv_maker_rate_bps"`
	EffectiveDerivTakerBps Decimal         `json:"effective_deriv_taker_rate_bps"`
	Raw                    json.RawMessage `json:"-"`
}

// InstrumentFeeRate는 상품별 유효 수수료율(bps)이다.
type InstrumentFeeRate struct {
	InstrumentName    string          `json:"instrument_name"`
	EffectiveMakerBps Decimal         `json:"effective_maker_rate_bps"`
	EffectiveTakerBps Decimal         `json:"effective_taker_rate_bps"`
	Raw               json.RawMessage `json:"-"`
}

func (value CancelAllType) valid() bool {
	switch value {
	case CancelAllLimit, CancelAllTrigger, CancelAllAll:
		return true
	default:
		return false
	}
}

func (request AmendOrderRequest) validate() error {
	if err := validateOrderIdentity(request.OrderID, request.OriginalClientOrderID); err != nil {
		return err
	}
	if request.NewClientOrderID != "" {
		if err := validateClientOrderID(request.NewClientOrderID); err != nil {
			return err
		}
	}
	if err := validatePositiveDecimal("amended price", request.NewPrice); err != nil {
		return err
	}
	return validatePositiveDecimal("amended quantity", request.NewQuantity)
}

func (request AmendOrderRequest) params() map[string]any {
	params := map[string]any{
		"new_price":    request.NewPrice,
		"new_quantity": request.NewQuantity,
	}
	setStringParam(params, "order_id", request.OrderID)
	setStringParam(params, "orig_client_oid", request.OriginalClientOrderID)
	setStringParam(params, "client_oid", request.NewClientOrderID)
	return params
}

func (request CancelAllOrdersRequest) validate() error {
	if err := validateOptionalInstrumentName(request.InstrumentName); err != nil {
		return err
	}
	if request.Type != "" && request.Type != CancelAllLimit && request.Type != CancelAllAll {
		return validationError("unsupported Crypto.com cancel-all type %q", request.Type)
	}
	return nil
}

func (request CancelAllOrdersRequest) params() map[string]any {
	params := make(map[string]any)
	setStringParam(params, "instrument_name", request.InstrumentName)
	setStringParam(params, "type", string(request.Type))
	return params
}

func (request CreateOrderListRequest) validate() error {
	if len(request.Orders) < 1 || len(request.Orders) > maximumOrderListSize {
		return validationError("Crypto.com order list must contain 1 to %d orders", maximumOrderListSize)
	}
	for index, order := range request.Orders {
		if err := order.validate(); err != nil {
			return fmt.Errorf("order list item %d: %w", index, err)
		}
	}
	return nil
}

func (request CreateOrderListRequest) params() map[string]any {
	orders := make([]any, len(request.Orders))
	for index, order := range request.Orders {
		orders[index] = order.params()
	}
	return map[string]any{"contingency_type": contingencyTypeList, "order_list": orders}
}

func (item CancelOrderListItem) validate() error {
	if err := validateOptionalInstrumentName(item.InstrumentName); err != nil {
		return err
	}
	if err := validateNumericID("order ID", item.OrderID); err != nil {
		return err
	}
	if item.ClientOrderID != "" {
		return validateClientOrderID(item.ClientOrderID)
	}
	return nil
}

func (request CancelOrderListRequest) validate() error {
	if len(request.Orders) < 1 {
		return validationError("Crypto.com cancel order list requires at least one order")
	}
	for index, item := range request.Orders {
		if err := item.validate(); err != nil {
			return fmt.Errorf("cancel order list item %d: %w", index, err)
		}
	}
	return nil
}

func (request CancelOrderListRequest) params() map[string]any {
	orders := make([]any, len(request.Orders))
	for index, item := range request.Orders {
		params := map[string]any{"order_id": item.OrderID}
		setStringParam(params, "instrument_name", item.InstrumentName)
		setStringParam(params, "client_oid", item.ClientOrderID)
		orders[index] = params
	}
	return map[string]any{"contingency_type": contingencyTypeList, "order_list": orders}
}

func (request InstrumentFeeRateRequest) validate() error {
	return validateSymbol(request.InstrumentName)
}

func (request InstrumentFeeRateRequest) params() map[string]any {
	return map[string]any{"instrument_name": request.InstrumentName}
}

// AmendOrder는 주문 가격·수량 정정을 비동기로 접수한다. 권한은 trade다.
func (client *Client) AmendOrder(
	ctx context.Context,
	request AmendOrderRequest,
	options ...trade.RequestOption,
) (OrderReceipt, error) {
	if err := request.validate(); err != nil {
		return OrderReceipt{}, err
	}
	return client.orderReceiptCall(
		ctx, methodAmendOrder, request.params(), request.OrderID, "", options...,
	)
}

// CancelAllOrders는 선택한 거래쌍 또는 전체 주문 취소를 비동기로 접수한다. 권한은 trade다.
func (client *Client) CancelAllOrders(
	ctx context.Context,
	request CancelAllOrdersRequest,
	options ...trade.RequestOption,
) (Acknowledgement, error) {
	if err := request.validate(); err != nil {
		return Acknowledgement{}, err
	}
	return client.acknowledgeCall(
		ctx, methodCancelAllOrders, request.params(), credential.PermissionTrade, options...,
	)
}

// CreateOrderList는 1~10건의 주문 생성을 비동기로 접수하고 항목별 결과를 반환한다. 권한은 trade다.
func (client *Client) CreateOrderList(
	ctx context.Context,
	request CreateOrderListRequest,
	options ...trade.RequestOption,
) ([]OrderListResult, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return client.orderListCall(
		ctx, methodCreateOrderList, request.params(), len(request.Orders), options...,
	)
}

// CancelOrderList는 여러 주문 취소를 비동기로 접수하고 항목별 결과를 반환한다. 권한은 trade다.
func (client *Client) CancelOrderList(
	ctx context.Context,
	request CancelOrderListRequest,
	options ...trade.RequestOption,
) ([]OrderListResult, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return client.orderListCall(
		ctx, methodCancelOrderList, request.params(), len(request.Orders), options...,
	)
}

// FeeRate는 계정 수수료 등급과 유효 수수료율을 조회한다. 권한은 read다.
func (client *Client) FeeRate(
	ctx context.Context,
	options ...trade.RequestOption,
) (FeeRate, error) {
	response, requestID, resultRaw, err := client.callPrivate(
		ctx, methodGetFeeRate, nil, credential.PermissionRead,
		commonexchange.OperationRead, true, options...,
	)
	if err != nil {
		return FeeRate{}, err
	}
	var value FeeRate
	if err := json.Unmarshal(resultRaw, &value); err != nil {
		return FeeRate{}, client.decodePrivateBodyError(
			response, commonexchange.OperationRead, requestID, err,
		)
	}
	value.Raw = cloneBytes(resultRaw)
	return value, nil
}

// InstrumentFeeRate는 상품별 유효 수수료율을 조회한다. 권한은 read다.
func (client *Client) InstrumentFeeRate(
	ctx context.Context,
	request InstrumentFeeRateRequest,
	options ...trade.RequestOption,
) (InstrumentFeeRate, error) {
	if err := request.validate(); err != nil {
		return InstrumentFeeRate{}, err
	}
	response, requestID, resultRaw, err := client.callPrivate(
		ctx, methodGetInstrumentFee, request.params(), credential.PermissionRead,
		commonexchange.OperationRead, true, options...,
	)
	if err != nil {
		return InstrumentFeeRate{}, err
	}
	var value InstrumentFeeRate
	if err := json.Unmarshal(resultRaw, &value); err != nil {
		return InstrumentFeeRate{}, client.decodePrivateBodyError(
			response, commonexchange.OperationRead, requestID, err,
		)
	}
	if value.InstrumentName != request.InstrumentName {
		return InstrumentFeeRate{}, client.decodePrivateBodyError(
			response, commonexchange.OperationRead, requestID,
			errors.New("Crypto.com instrument fee rate instrument is inconsistent"),
		)
	}
	value.Raw = cloneBytes(resultRaw)
	return value, nil
}

// orderReceiptCall은 order_id·client_oid를 돌려주는 trade mutation을 실행한다.
// 요청에 쓴 식별자와 응답 식별자가 다르면 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) orderReceiptCall(
	ctx context.Context,
	method string,
	params map[string]any,
	wantOrderID string,
	wantClientOrderID string,
	options ...trade.RequestOption,
) (OrderReceipt, error) {
	response, requestID, resultRaw, err := client.callPrivate(
		ctx, method, params, credential.PermissionTrade,
		commonexchange.OperationMutation, true, options...,
	)
	if err != nil {
		return OrderReceipt{}, err
	}
	var value OrderReceipt
	if err := json.Unmarshal(resultRaw, &value); err != nil {
		return OrderReceipt{}, client.decodePrivateBodyError(
			response, commonexchange.OperationMutation, requestID, err,
		)
	}
	if value.OrderID == "" ||
		wantOrderID != "" && string(value.OrderID) != wantOrderID ||
		wantClientOrderID != "" && value.ClientOrderID != wantClientOrderID {
		return OrderReceipt{}, client.decodePrivateBodyError(
			response, commonexchange.OperationMutation, requestID,
			errors.New("Crypto.com order receipt identity is inconsistent"),
		)
	}
	value.Raw = cloneBytes(resultRaw)
	return value, nil
}

// acknowledgeCall은 result 본문이 없을 수 있는 mutation의 접수 응답을 반환한다.
func (client *Client) acknowledgeCall(
	ctx context.Context,
	method string,
	params map[string]any,
	permission credential.Permission,
	options ...trade.RequestOption,
) (Acknowledgement, error) {
	_, _, resultRaw, err := client.callPrivate(
		ctx, method, params, permission,
		commonexchange.OperationMutation, false, options...,
	)
	if err != nil {
		return Acknowledgement{}, err
	}
	return Acknowledgement{Raw: cloneBytes(resultRaw)}, nil
}

// orderListCall은 result가 항목별 배열인 trade mutation을 실행한다.
func (client *Client) orderListCall(
	ctx context.Context,
	method string,
	params map[string]any,
	requested int,
	options ...trade.RequestOption,
) ([]OrderListResult, error) {
	response, requestID, resultRaw, err := client.callPrivate(
		ctx, method, params, credential.PermissionTrade,
		commonexchange.OperationMutation, true, options...,
	)
	if err != nil {
		return nil, err
	}
	itemsRaw, err := client.decodePrivateArray(
		response, requestID, commonexchange.OperationMutation, resultRaw,
	)
	if err != nil {
		return nil, err
	}
	items, err := decodeItems(itemsRaw, func(item *OrderListResult, raw json.RawMessage) {
		item.Raw = raw
	})
	if err != nil {
		return nil, client.decodePrivateBodyError(
			response, commonexchange.OperationMutation, requestID, err,
		)
	}
	for _, item := range items {
		if item.Index < 0 || int(item.Index) >= requested {
			return nil, client.decodePrivateBodyError(
				response, commonexchange.OperationMutation, requestID,
				errors.New("Crypto.com order list result index is out of range"),
			)
		}
	}
	return items, nil
}
