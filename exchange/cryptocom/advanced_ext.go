package cryptocom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const (
	methodAdvancedCreateOrder     = "private/advanced/create-order"
	methodAdvancedAmendOrder      = "private/advanced/amend-order"
	methodAdvancedCreateOCO       = "private/advanced/create-oco"
	methodAdvancedCancelOCO       = "private/advanced/cancel-oco"
	methodAdvancedCreateOTO       = "private/advanced/create-oto"
	methodAdvancedCancelOTO       = "private/advanced/cancel-oto"
	methodAdvancedCreateOTOCO     = "private/advanced/create-otoco"
	methodAdvancedCancelOTOCO     = "private/advanced/cancel-otoco"
	methodAdvancedCancelOrder     = "private/advanced/cancel-order"
	methodAdvancedCancelAllOrders = "private/advanced/cancel-all-orders"
	methodAdvancedGetOpenOrders   = "private/advanced/get-open-orders"
	methodAdvancedGetOrderDetail  = "private/advanced/get-order-detail"
	methodAdvancedGetOrderHistory = "private/advanced/get-order-history"
	methodAdvancedCreateOrderList = "private/advanced/create-order-list"
	methodAdvancedCancelOrderList = "private/advanced/cancel-order-list"
	methodAdvancedGetOrderListDet = "private/advanced/get-order-list-detail"
	maximumAdvancedListLookups    = 10
)

// AdvancedOrderType은 고급 주문에서 쓰는 주문 종류다. 트리거 주문은 STOP·TAKE_PROFIT 계열이다.
type AdvancedOrderType string

const (
	AdvancedOrderLimit           AdvancedOrderType = "LIMIT"
	AdvancedOrderMarket          AdvancedOrderType = "MARKET"
	AdvancedOrderStopLoss        AdvancedOrderType = "STOP_LOSS"
	AdvancedOrderStopLimit       AdvancedOrderType = "STOP_LIMIT"
	AdvancedOrderTakeProfit      AdvancedOrderType = "TAKE_PROFIT"
	AdvancedOrderTakeProfitLimit AdvancedOrderType = "TAKE_PROFIT_LIMIT"
)

// RefPriceType은 트리거 주문이 비교하는 기준 가격 종류다.
type RefPriceType string

const (
	RefPriceMark  RefPriceType = "MARK_PRICE"
	RefPriceIndex RefPriceType = "INDEX_PRICE"
	RefPriceLast  RefPriceType = "LAST_PRICE"
)

// AdvancedOrder는 고급 주문 한 건(LIMIT·MARKET 주 주문 또는 트리거 주문)의 입력이다.
type AdvancedOrder struct {
	InstrumentName string
	Side           OrderSide
	Type           AdvancedOrderType
	ClientOrderID  string
	Quantity       string
	// Notional은 MARKET·STOP_LOSS·TAKE_PROFIT 매수에서 Quantity 대신 쓰는 quote 지출액이다.
	Notional     string
	Price        string
	RefPrice     string
	RefPriceType RefPriceType
	TimeInForce  TimeInForce
	PostOnly     bool
	// AttachOrderID는 기존 주문에 TP/SL을 붙일 때 대상 주문 ID다.
	AttachOrderID string
}

// CreateAdvancedOrderRequest는 단일 트리거 주문 생성 조건이다.
type CreateAdvancedOrderRequest struct {
	Order AdvancedOrder
}

// AmendAdvancedOrderRequest는 트리거 주문의 기준가·수량·가격 정정 조건이다.
// NewPrice가 "0"이면 트리거 지정가를 시장가로, 0보다 크면 시장가를 지정가로 바꾼다.
type AmendAdvancedOrderRequest struct {
	OrderID       string
	ClientOrderID string
	NewRefPrice   string
	NewQuantity   string
	NewPrice      string
}

// AdvancedListRequest는 OCO·OTO·OTOCO 생성에 쓰는 주문 묶음이다.
type AdvancedListRequest struct {
	Orders []AdvancedOrder
}

// CancelAdvancedListRequest는 list_id로 OCO·OTO·OTOCO 묶음을 취소하는 조건이다.
type CancelAdvancedListRequest struct {
	ListID string
}

// CancelAdvancedOrderRequest는 OTO/OTOCO의 개별 주문 취소 대상을 지정한다.
type CancelAdvancedOrderRequest struct {
	OrderID       string
	ClientOrderID string
}

// CancelAllAdvancedOrdersRequest는 고급 주문 일괄 취소 조건이다.
// PositionType은 파생 전용이며 InstrumentName이 필요하고 Type과 함께 쓸 수 없다.
type CancelAllAdvancedOrdersRequest struct {
	InstrumentName string
	Type           CancelAllType
	PositionType   string
}

// AdvancedOpenOrdersRequest는 선택적 상품으로 열린 고급 주문을 조회한다.
type AdvancedOpenOrdersRequest struct {
	InstrumentName string
}

// AdvancedOrderInfoRequest는 주문 ID 또는 사용자 주문 ID로 고급 주문 한 건을 조회한다.
type AdvancedOrderInfoRequest struct {
	OrderID       string
	ClientOrderID string
}

// AdvancedOrderHistoryRequest는 고급 주문 이력 조회 조건이다.
type AdvancedOrderHistoryRequest struct {
	InstrumentName string
	Start          *time.Time
	End            *time.Time
	Limit          int
}

// AdvancedOrderListDetailRequest는 list_id 목록으로 OCO/SpotAttach 상세를 조회한다.
type AdvancedOrderListDetailRequest struct {
	ListIDs []string
}

// AdvancedOrderDetail은 기본 Order 필드에 고급 주문 전용 필드를 더한 응답이다.
type AdvancedOrderDetail struct {
	Order
	ListID            Scalar  `json:"list_id"`
	ContingencyType   string  `json:"contingency_type"`
	LegID             Scalar  `json:"leg_id"`
	RefPrice          Decimal `json:"ref_price"`
	RefPriceType      string  `json:"ref_price_type"`
	ExchangeOrderID   Scalar  `json:"exchange_order_id"`
	RejectReason      string  `json:"reject_reason"`
	AttachIsolationID string  `json:"attach_isolation_id"`
}

// AdvancedListReceipt는 OCO·OTO·OTOCO 생성 접수 결과의 list_id다.
type AdvancedListReceipt struct {
	ListID Scalar          `json:"list_id"`
	Raw    json.RawMessage `json:"-"`
}

func (value AdvancedOrderType) isTrigger() bool {
	switch value {
	case AdvancedOrderStopLoss, AdvancedOrderStopLimit,
		AdvancedOrderTakeProfit, AdvancedOrderTakeProfitLimit:
		return true
	default:
		return false
	}
}

func (value AdvancedOrderType) needsPrice() bool {
	switch value {
	case AdvancedOrderLimit, AdvancedOrderStopLimit, AdvancedOrderTakeProfitLimit:
		return true
	default:
		return false
	}
}

func (value AdvancedOrderType) valid() bool {
	return value == AdvancedOrderLimit || value == AdvancedOrderMarket || value.isTrigger()
}

func (value RefPriceType) valid() bool {
	switch value {
	case RefPriceMark, RefPriceIndex, RefPriceLast:
		return true
	default:
		return false
	}
}

func (order AdvancedOrder) validate() error {
	if err := validateSymbol(order.InstrumentName); err != nil {
		return err
	}
	if order.Side != OrderSideBuy && order.Side != OrderSideSell {
		return validationError("unsupported Crypto.com order side %q", order.Side)
	}
	if !order.Type.valid() {
		return validationError("unsupported Crypto.com advanced order type %q", order.Type)
	}
	if order.ClientOrderID != "" {
		if err := validateClientOrderID(order.ClientOrderID); err != nil {
			return err
		}
	}
	// 수량 또는 (매수 시장가·트리거의) quote 지출액 중 정확히 하나가 필요하다.
	switch {
	case order.Quantity != "" && order.Notional != "":
		return validationError("advanced order quantity and notional are mutually exclusive")
	case order.Quantity != "":
		if err := validatePositiveDecimal("order quantity", order.Quantity); err != nil {
			return err
		}
	case order.Notional != "":
		if order.Side != OrderSideBuy || order.Type == AdvancedOrderLimit ||
			order.Type == AdvancedOrderStopLimit || order.Type == AdvancedOrderTakeProfitLimit {
			return validationError("notional is only valid for market-style BUY orders")
		}
		if err := validatePositiveDecimal("order notional", order.Notional); err != nil {
			return err
		}
	default:
		return validationError("advanced order quantity or notional is required")
	}
	if order.Type.needsPrice() {
		if err := validatePositiveDecimal("order price", order.Price); err != nil {
			return err
		}
	} else if order.Price != "" {
		return validationError("%s order cannot set price", order.Type)
	}
	if order.Type.isTrigger() {
		if err := validatePositiveDecimal("trigger price", order.RefPrice); err != nil {
			return err
		}
		if order.RefPriceType != "" && !order.RefPriceType.valid() {
			return validationError("unsupported Crypto.com reference price type %q", order.RefPriceType)
		}
	} else if order.RefPrice != "" || order.RefPriceType != "" {
		return validationError("%s order cannot set trigger price", order.Type)
	}
	if order.TimeInForce != "" && !order.TimeInForce.valid() {
		return validationError("unsupported Crypto.com time in force %q", order.TimeInForce)
	}
	if order.PostOnly && order.TimeInForce != "" && order.TimeInForce != TimeInForceGoodTillCancel {
		return validationError("post-only order must use GOOD_TILL_CANCEL or omit time in force")
	}
	if order.AttachOrderID != "" {
		if !order.Type.isTrigger() {
			return validationError("attach order ID is only valid for trigger orders")
		}
		return validateNumericID("attach order ID", order.AttachOrderID)
	}
	return nil
}

func (order AdvancedOrder) params() map[string]any {
	params := map[string]any{
		"instrument_name": order.InstrumentName,
		"side":            string(order.Side),
		"type":            string(order.Type),
	}
	setStringParam(params, "client_oid", order.ClientOrderID)
	setStringParam(params, "quantity", order.Quantity)
	setStringParam(params, "notional", order.Notional)
	setStringParam(params, "price", order.Price)
	setStringParam(params, "ref_price", order.RefPrice)
	setStringParam(params, "ref_price_type", string(order.RefPriceType))
	setStringParam(params, "time_in_force", string(order.TimeInForce))
	setStringParam(params, "attach_order_id", order.AttachOrderID)
	if order.PostOnly {
		params["exec_inst"] = []string{"POST_ONLY"}
	}
	return params
}

func (request CreateAdvancedOrderRequest) validate() error {
	if err := request.Order.validate(); err != nil {
		return err
	}
	if !request.Order.Type.isTrigger() {
		return validationError("private/advanced/create-order requires a trigger order type")
	}
	return nil
}

func (request CreateAdvancedOrderRequest) params() map[string]any {
	return request.Order.params()
}

func (request AmendAdvancedOrderRequest) validate() error {
	if err := validateOrderIdentity(request.OrderID, request.ClientOrderID); err != nil {
		return err
	}
	if request.NewRefPrice != "" {
		if err := validatePositiveDecimal("amended trigger price", request.NewRefPrice); err != nil {
			return err
		}
	}
	if err := validatePositiveDecimal("amended quantity", request.NewQuantity); err != nil {
		return err
	}
	return validateNonNegativeDecimal("amended price", request.NewPrice)
}

func (request AmendAdvancedOrderRequest) params() map[string]any {
	params := orderIdentityParams(request.OrderID, request.ClientOrderID)
	params["new_quantity"] = request.NewQuantity
	params["new_price"] = request.NewPrice
	setStringParam(params, "new_ref_price", request.NewRefPrice)
	return params
}

// validateOrders는 주문 묶음의 개수와 각 주문의 유형 구성을 검사한다.
func (request AdvancedListRequest) validateOrders(
	count int,
	check func(types []AdvancedOrderType) error,
) error {
	if len(request.Orders) != count {
		return validationError("Crypto.com advanced list requires exactly %d orders", count)
	}
	types := make([]AdvancedOrderType, count)
	for index, order := range request.Orders {
		if err := order.validate(); err != nil {
			return fmt.Errorf("advanced order %d: %w", index, err)
		}
		types[index] = order.Type
	}
	return check(types)
}

func (request AdvancedListRequest) params() map[string]any {
	orders := make([]any, len(request.Orders))
	for index, order := range request.Orders {
		orders[index] = order.params()
	}
	return map[string]any{"order_list": orders}
}

// CreateOCORequest는 LIMIT 1건과 트리거 1건으로 이루어진 OCO 조건이다(Spot 전용, MARK_PRICE만).
type CreateOCORequest struct{ AdvancedListRequest }

// CreateOTORequest는 LIMIT·MARKET 주 주문 1건과 트리거 1건의 OTO 조건이다.
type CreateOTORequest struct{ AdvancedListRequest }

// CreateOTOCORequest는 LIMIT·MARKET 주 주문 1건과 손절·익절 트리거 2건의 OTOCO 조건이다.
type CreateOTOCORequest struct{ AdvancedListRequest }

func countTypes(types []AdvancedOrderType, match func(AdvancedOrderType) bool) int {
	count := 0
	for _, value := range types {
		if match(value) {
			count++
		}
	}
	return count
}

func isPrimaryType(value AdvancedOrderType) bool {
	return value == AdvancedOrderLimit || value == AdvancedOrderMarket
}

func (request CreateOCORequest) validate() error {
	if err := request.validateOrders(2, func(types []AdvancedOrderType) error {
		if countTypes(types, func(value AdvancedOrderType) bool { return value == AdvancedOrderLimit }) != 1 ||
			countTypes(types, AdvancedOrderType.isTrigger) != 1 {
			return validationError("OCO requires one LIMIT order and one trigger order")
		}
		return nil
	}); err != nil {
		return err
	}
	for _, order := range request.Orders {
		if order.Type.isTrigger() && order.RefPriceType != "" && order.RefPriceType != RefPriceMark {
			return validationError("OCO trigger order supports only MARK_PRICE")
		}
	}
	return nil
}

func (request CreateOTORequest) validate() error {
	return request.validateOrders(2, func(types []AdvancedOrderType) error {
		if countTypes(types, isPrimaryType) != 1 || countTypes(types, AdvancedOrderType.isTrigger) != 1 {
			return validationError("OTO requires one LIMIT or MARKET order and one trigger order")
		}
		return nil
	})
}

func (request CreateOTOCORequest) validate() error {
	return request.validateOrders(3, func(types []AdvancedOrderType) error {
		stops := countTypes(types, func(value AdvancedOrderType) bool {
			return value == AdvancedOrderStopLoss || value == AdvancedOrderStopLimit
		})
		takes := countTypes(types, func(value AdvancedOrderType) bool {
			return value == AdvancedOrderTakeProfit || value == AdvancedOrderTakeProfitLimit
		})
		if countTypes(types, isPrimaryType) != 1 || stops != 1 || takes != 1 {
			return validationError(
				"OTOCO requires one LIMIT or MARKET order, one stop trigger, and one take-profit trigger",
			)
		}
		return nil
	})
}

func validateListID(value string) error {
	return validateNumericID("list ID", value)
}

func (request CancelAdvancedListRequest) validate() error { return validateListID(request.ListID) }

func (request CancelAdvancedListRequest) params() map[string]any {
	return map[string]any{"list_id": request.ListID}
}

func (request CancelAdvancedOrderRequest) validate() error {
	return validateOrderIdentity(request.OrderID, request.ClientOrderID)
}

func (request CancelAllAdvancedOrdersRequest) validate() error {
	if err := validateOptionalInstrumentName(request.InstrumentName); err != nil {
		return err
	}
	if request.Type != "" && !request.Type.valid() {
		return validationError("unsupported Crypto.com cancel-all type %q", request.Type)
	}
	switch request.PositionType {
	case "":
	case "CROSS_MARGIN", "ISOLATED_MARGIN":
		if request.InstrumentName == "" || request.Type == CancelAllLimit || request.Type == CancelAllTrigger {
			return validationError("position type requires an instrument and cannot combine with LIMIT or TRIGGER")
		}
	default:
		return validationError("unsupported Crypto.com position type %q", request.PositionType)
	}
	return nil
}

func (request CancelAllAdvancedOrdersRequest) params() map[string]any {
	params := make(map[string]any)
	setStringParam(params, "instrument_name", request.InstrumentName)
	setStringParam(params, "type", string(request.Type))
	setStringParam(params, "position_type", request.PositionType)
	return params
}

func (request AdvancedOpenOrdersRequest) validate() error {
	return validateOptionalSymbol(request.InstrumentName)
}

func (request AdvancedOpenOrdersRequest) params() map[string]any {
	params := make(map[string]any)
	setStringParam(params, "instrument_name", request.InstrumentName)
	return params
}

func (request AdvancedOrderInfoRequest) validate() error {
	return validateOrderIdentity(request.OrderID, request.ClientOrderID)
}

func (request AdvancedOrderInfoRequest) params() map[string]any {
	return orderIdentityParams(request.OrderID, request.ClientOrderID)
}

func (request AdvancedOrderHistoryRequest) validate() error {
	if err := validateOptionalSymbol(request.InstrumentName); err != nil {
		return err
	}
	return validatePrivateHistory(request.Start, request.End, request.Limit)
}

func (request AdvancedOrderHistoryRequest) params() map[string]any {
	return privateHistoryParams(request.InstrumentName, request.Start, request.End, request.Limit)
}

func (request AdvancedOrderListDetailRequest) validate() error {
	if len(request.ListIDs) < 1 || len(request.ListIDs) > maximumAdvancedListLookups {
		return validationError("Crypto.com list ID lookup requires 1 to %d list IDs", maximumAdvancedListLookups)
	}
	for _, listID := range request.ListIDs {
		if err := validateListID(listID); err != nil {
			return err
		}
	}
	return nil
}

func (request AdvancedOrderListDetailRequest) params() map[string]any {
	return map[string]any{"list_id_list": append([]string(nil), request.ListIDs...)}
}

// CreateAdvancedOrder는 STOP_LOSS·STOP_LIMIT·TAKE_PROFIT·TAKE_PROFIT_LIMIT 트리거 주문을 접수한다.
// 권한은 trade다.
func (client *Client) CreateAdvancedOrder(
	ctx context.Context,
	request CreateAdvancedOrderRequest,
	options ...trade.RequestOption,
) (OrderReceipt, error) {
	if err := request.validate(); err != nil {
		return OrderReceipt{}, err
	}
	return client.orderReceiptCall(
		ctx, methodAdvancedCreateOrder, request.params(), "", request.Order.ClientOrderID, options...,
	)
}

// AmendAdvancedOrder는 트리거 주문의 기준가·수량·가격 정정을 접수한다. 권한은 trade다.
func (client *Client) AmendAdvancedOrder(
	ctx context.Context,
	request AmendAdvancedOrderRequest,
	options ...trade.RequestOption,
) (OrderReceipt, error) {
	if err := request.validate(); err != nil {
		return OrderReceipt{}, err
	}
	return client.orderReceiptCall(
		ctx, methodAdvancedAmendOrder, request.params(), request.OrderID, request.ClientOrderID, options...,
	)
}

// CreateOCO는 OCO 주문 묶음을 접수하고 list_id를 반환한다. 권한은 trade다.
func (client *Client) CreateOCO(
	ctx context.Context,
	request CreateOCORequest,
	options ...trade.RequestOption,
) (AdvancedListReceipt, error) {
	if err := request.validate(); err != nil {
		return AdvancedListReceipt{}, err
	}
	return client.advancedListCall(ctx, methodAdvancedCreateOCO, request.params(), options...)
}

// CreateOTO는 OTO 주문 묶음을 접수하고 list_id를 반환한다. 권한은 trade다.
func (client *Client) CreateOTO(
	ctx context.Context,
	request CreateOTORequest,
	options ...trade.RequestOption,
) (AdvancedListReceipt, error) {
	if err := request.validate(); err != nil {
		return AdvancedListReceipt{}, err
	}
	return client.advancedListCall(ctx, methodAdvancedCreateOTO, request.params(), options...)
}

// CreateOTOCO는 OTOCO 주문 묶음을 접수하고 list_id를 반환한다. 권한은 trade다.
func (client *Client) CreateOTOCO(
	ctx context.Context,
	request CreateOTOCORequest,
	options ...trade.RequestOption,
) (AdvancedListReceipt, error) {
	if err := request.validate(); err != nil {
		return AdvancedListReceipt{}, err
	}
	return client.advancedListCall(ctx, methodAdvancedCreateOTOCO, request.params(), options...)
}

// CancelOCO는 list_id로 OCO 주문 취소를 접수한다. 권한은 trade다.
func (client *Client) CancelOCO(
	ctx context.Context,
	request CancelAdvancedListRequest,
	options ...trade.RequestOption,
) (Acknowledgement, error) {
	return client.cancelAdvancedList(ctx, methodAdvancedCancelOCO, request, options...)
}

// CancelOTO는 list_id로 OTO(SpotAttach·DerivAttach) 주문 취소를 접수한다. 권한은 trade다.
func (client *Client) CancelOTO(
	ctx context.Context,
	request CancelAdvancedListRequest,
	options ...trade.RequestOption,
) (Acknowledgement, error) {
	return client.cancelAdvancedList(ctx, methodAdvancedCancelOTO, request, options...)
}

// CancelOTOCO는 list_id로 OTOCO(SpotAttach·DerivAttach) 주문 취소를 접수한다. 권한은 trade다.
func (client *Client) CancelOTOCO(
	ctx context.Context,
	request CancelAdvancedListRequest,
	options ...trade.RequestOption,
) (Acknowledgement, error) {
	return client.cancelAdvancedList(ctx, methodAdvancedCancelOTOCO, request, options...)
}

func (client *Client) cancelAdvancedList(
	ctx context.Context,
	method string,
	request CancelAdvancedListRequest,
	options ...trade.RequestOption,
) (Acknowledgement, error) {
	if err := request.validate(); err != nil {
		return Acknowledgement{}, err
	}
	return client.acknowledgeCall(ctx, method, request.params(), credential.PermissionTrade, options...)
}

// CancelAdvancedOrder는 OTO/OTOCO의 개별 주문 취소를 접수한다. 권한은 trade다.
func (client *Client) CancelAdvancedOrder(
	ctx context.Context,
	request CancelAdvancedOrderRequest,
	options ...trade.RequestOption,
) (CancelAcknowledgement, error) {
	if err := request.validate(); err != nil {
		return CancelAcknowledgement{}, err
	}
	response, requestID, resultRaw, err := client.callPrivate(
		ctx, methodAdvancedCancelOrder, orderIdentityParams(request.OrderID, request.ClientOrderID),
		credential.PermissionTrade, commonexchange.OperationMutation, false, options...,
	)
	if err != nil {
		return CancelAcknowledgement{}, err
	}
	value := CancelAcknowledgement{
		OrderID: Scalar(request.OrderID), ClientOrderID: request.ClientOrderID,
		Raw: cloneBytes(resultRaw),
	}
	if len(resultRaw) > 0 {
		if err := json.Unmarshal(resultRaw, &value); err != nil {
			return CancelAcknowledgement{}, client.decodePrivateBodyError(
				response, commonexchange.OperationMutation, requestID, err,
			)
		}
		value.Raw = cloneBytes(resultRaw)
	}
	if request.OrderID != "" && value.OrderID != "" && string(value.OrderID) != request.OrderID ||
		request.ClientOrderID != "" && value.ClientOrderID != "" &&
			value.ClientOrderID != request.ClientOrderID {
		return CancelAcknowledgement{}, client.decodePrivateBodyError(
			response, commonexchange.OperationMutation, requestID,
			errors.New("Crypto.com advanced cancel acknowledgement identity is inconsistent"),
		)
	}
	return value, nil
}

// CancelAllAdvancedOrders는 고급 주문 일괄 취소를 접수한다. 권한은 trade다.
func (client *Client) CancelAllAdvancedOrders(
	ctx context.Context,
	request CancelAllAdvancedOrdersRequest,
	options ...trade.RequestOption,
) (Acknowledgement, error) {
	if err := request.validate(); err != nil {
		return Acknowledgement{}, err
	}
	return client.acknowledgeCall(
		ctx, methodAdvancedCancelAllOrders, request.params(), credential.PermissionTrade, options...,
	)
}

// CreateAdvancedOrderList는 트리거 주문 1~10건을 한 번에 접수하고 항목별 결과를 반환한다. 권한은 trade다.
func (client *Client) CreateAdvancedOrderList(
	ctx context.Context,
	request AdvancedListRequest,
	options ...trade.RequestOption,
) ([]OrderListResult, error) {
	if len(request.Orders) < 1 || len(request.Orders) > maximumOrderListSize {
		return nil, validationError("Crypto.com order list must contain 1 to %d orders", maximumOrderListSize)
	}
	for index, order := range request.Orders {
		if err := order.validate(); err != nil {
			return nil, fmt.Errorf("advanced order %d: %w", index, err)
		}
		if !order.Type.isTrigger() {
			return nil, validationError("advanced order %d must be a trigger order", index)
		}
	}
	return client.orderListCall(
		ctx, methodAdvancedCreateOrderList, request.params(), len(request.Orders), options...,
	)
}

// CancelAdvancedOrderList는 고급 주문 최대 10건 취소를 접수하고 항목별 결과를 반환한다. 권한은 trade다.
func (client *Client) CancelAdvancedOrderList(
	ctx context.Context,
	request CancelAdvancedOrderListRequest,
	options ...trade.RequestOption,
) ([]OrderListResult, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return client.orderListCall(
		ctx, methodAdvancedCancelOrderList, request.params(), len(request.Orders), options...,
	)
}

// CancelAdvancedOrderListRequest는 고급 주문 목록 취소 대상이다.
type CancelAdvancedOrderListRequest struct {
	Orders []CancelAdvancedOrderRequest
}

func (request CancelAdvancedOrderListRequest) validate() error {
	if len(request.Orders) < 1 || len(request.Orders) > maximumOrderListSize {
		return validationError("Crypto.com cancel list must contain 1 to %d orders", maximumOrderListSize)
	}
	for index, item := range request.Orders {
		if err := item.validate(); err != nil {
			return fmt.Errorf("advanced cancel item %d: %w", index, err)
		}
	}
	return nil
}

func (request CancelAdvancedOrderListRequest) params() map[string]any {
	orders := make([]any, len(request.Orders))
	for index, item := range request.Orders {
		orders[index] = orderIdentityParams(item.OrderID, item.ClientOrderID)
	}
	return map[string]any{"order_list": orders}
}

// AdvancedOpenOrders는 열린 OTO/OTOCO 고급 주문을 조회한다. 권한은 read다.
func (client *Client) AdvancedOpenOrders(
	ctx context.Context,
	request AdvancedOpenOrdersRequest,
	options ...trade.RequestOption,
) ([]AdvancedOrderDetail, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return client.advancedOrdersCall(ctx, methodAdvancedGetOpenOrders, request.params(), options...)
}

// AdvancedOrderHistory는 OTO/OTOCO 고급 주문 이력을 조회한다. 권한은 read다.
func (client *Client) AdvancedOrderHistory(
	ctx context.Context,
	request AdvancedOrderHistoryRequest,
	options ...trade.RequestOption,
) ([]AdvancedOrderDetail, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return client.advancedOrdersCall(ctx, methodAdvancedGetOrderHistory, request.params(), options...)
}

// AdvancedOrderListDetail은 list_id 목록으로 OCO/SpotAttach 주문 상세를 조회한다. 권한은 read다.
func (client *Client) AdvancedOrderListDetail(
	ctx context.Context,
	request AdvancedOrderListDetailRequest,
	options ...trade.RequestOption,
) ([]AdvancedOrderDetail, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	return client.advancedOrdersCall(ctx, methodAdvancedGetOrderListDet, request.params(), options...)
}

// AdvancedOrderInfo는 주문 ID 또는 사용자 주문 ID로 고급 주문 한 건을 조회한다. 권한은 read다.
func (client *Client) AdvancedOrderInfo(
	ctx context.Context,
	request AdvancedOrderInfoRequest,
	options ...trade.RequestOption,
) (AdvancedOrderDetail, error) {
	if err := request.validate(); err != nil {
		return AdvancedOrderDetail{}, err
	}
	response, requestID, resultRaw, err := client.callPrivate(
		ctx, methodAdvancedGetOrderDetail, request.params(), credential.PermissionRead,
		commonexchange.OperationRead, true, options...,
	)
	if err != nil {
		return AdvancedOrderDetail{}, err
	}
	value, err := client.decodeAdvancedOrder(response, requestID, resultRaw)
	if err != nil {
		return AdvancedOrderDetail{}, err
	}
	if request.OrderID != "" && string(value.OrderID) != request.OrderID ||
		request.ClientOrderID != "" && value.ClientOrderID != request.ClientOrderID {
		return AdvancedOrderDetail{}, client.decodePrivateBodyError(
			response, commonexchange.OperationRead, requestID,
			errors.New("Crypto.com advanced order detail identity is inconsistent"),
		)
	}
	return value, nil
}

func (client *Client) advancedOrdersCall(
	ctx context.Context,
	method string,
	params map[string]any,
	options ...trade.RequestOption,
) ([]AdvancedOrderDetail, error) {
	response, requestID, resultRaw, err := client.callPrivate(
		ctx, method, params, credential.PermissionRead,
		commonexchange.OperationRead, true, options...,
	)
	if err != nil {
		return nil, err
	}
	itemsRaw, err := client.decodePrivateDataItems(
		response, resultRaw, commonexchange.OperationRead, requestID,
	)
	if err != nil {
		return nil, err
	}
	items := make([]AdvancedOrderDetail, len(itemsRaw))
	for index, itemRaw := range itemsRaw {
		item, err := client.decodeAdvancedOrder(response, requestID, itemRaw)
		if err != nil {
			return nil, err
		}
		items[index] = item
	}
	return items, nil
}

func (client *Client) decodeAdvancedOrder(
	response commonexchange.Response,
	requestID string,
	raw json.RawMessage,
) (AdvancedOrderDetail, error) {
	var value AdvancedOrderDetail
	if err := json.Unmarshal(raw, &value); err != nil {
		return AdvancedOrderDetail{}, client.decodePrivateBodyError(
			response, commonexchange.OperationRead, requestID, err,
		)
	}
	if value.OrderID == "" || value.InstrumentName == "" ||
		(value.Side != OrderSideBuy && value.Side != OrderSideSell) {
		return AdvancedOrderDetail{}, client.decodePrivateBodyError(
			response, commonexchange.OperationRead, requestID,
			errors.New("Crypto.com advanced order identity or side is invalid"),
		)
	}
	value.Raw = cloneBytes(raw)
	return value, nil
}

func (client *Client) advancedListCall(
	ctx context.Context,
	method string,
	params map[string]any,
	options ...trade.RequestOption,
) (AdvancedListReceipt, error) {
	response, requestID, resultRaw, err := client.callPrivate(
		ctx, method, params, credential.PermissionTrade,
		commonexchange.OperationMutation, true, options...,
	)
	if err != nil {
		return AdvancedListReceipt{}, err
	}
	var value AdvancedListReceipt
	if err := json.Unmarshal(resultRaw, &value); err != nil {
		return AdvancedListReceipt{}, client.decodePrivateBodyError(
			response, commonexchange.OperationMutation, requestID, err,
		)
	}
	if value.ListID == "" {
		return AdvancedListReceipt{}, client.decodePrivateBodyError(
			response, commonexchange.OperationMutation, requestID,
			errors.New("Crypto.com advanced list ID is missing"),
		)
	}
	value.Raw = cloneBytes(resultRaw)
	return value, nil
}
