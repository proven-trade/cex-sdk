package binance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	trade "github.com/proven-trade/cex-sdk"
)

// ContingencyType은 주문 리스트의 종류다.
type ContingencyType string

const (
	ContingencyTypeOCO ContingencyType = "OCO"
	ContingencyTypeOTO ContingencyType = "OTO"
)

// ListStatusType은 주문 리스트 상태 메시지의 종류다.
type ListStatusType string

const (
	ListStatusTypeResponse    ListStatusType = "RESPONSE"
	ListStatusTypeExecStarted ListStatusType = "EXEC_STARTED"
	ListStatusTypeUpdated     ListStatusType = "UPDATED"
	ListStatusTypeAllDone     ListStatusType = "ALL_DONE"
)

// ListOrderStatus는 주문 리스트의 진행 상태다.
type ListOrderStatus string

const (
	ListOrderStatusExecuting ListOrderStatus = "EXECUTING"
	ListOrderStatusAllDone   ListOrderStatus = "ALL_DONE"
	ListOrderStatusReject    ListOrderStatus = "REJECT"
)

// OrderListOrder는 주문 리스트에 속한 주문의 식별자다.
type OrderListOrder struct {
	Symbol        string `json:"symbol"`
	OrderID       int64  `json:"orderId"`
	ClientOrderID string `json:"clientOrderId"`
}

// OrderList는 OCO, OTO, OTOCO, OPO, OPOCO 주문 리스트의 상태다.
// 주문 리스트를 만들거나 취소한 응답에는 OrderReports가 있고 조회 응답에는 없다.
type OrderList struct {
	OrderListID       int64            `json:"orderListId"`
	ContingencyType   ContingencyType  `json:"contingencyType"`
	ListStatusType    ListStatusType   `json:"listStatusType"`
	ListOrderStatus   ListOrderStatus  `json:"listOrderStatus"`
	ListClientOrderID string           `json:"listClientOrderId"`
	TransactionTime   int64            `json:"transactionTime"`
	Symbol            string           `json:"symbol"`
	Orders            []OrderListOrder `json:"orders"`
	OrderReports      []Order          `json:"orderReports"`
	Raw               json.RawMessage  `json:"-"`
}

func decodeOrderList(body []byte) (OrderList, error) {
	var orderList OrderList
	if err := decodeJSON(body, &orderList); err != nil {
		return OrderList{}, err
	}
	orderList.Raw = cloneBytes(body)
	return orderList, nil
}

func decodeOrderLists(body []byte) ([]OrderList, error) {
	return decodeList(body, func(orderList *OrderList, raw json.RawMessage) { orderList.Raw = raw })
}

var (
	workingOrderTypes = []OrderType{OrderTypeLimit, OrderTypeLimitMaker}
	pendingOrderTypes = []OrderType{
		OrderTypeLimit,
		OrderTypeMarket,
		OrderTypeStopLoss,
		OrderTypeStopLossLimit,
		OrderTypeTakeProfit,
		OrderTypeTakeProfitLimit,
		OrderTypeLimitMaker,
	}
	ocoOrderTypes = []OrderType{
		OrderTypeLimitMaker,
		OrderTypeStopLoss,
		OrderTypeStopLossLimit,
		OrderTypeTakeProfit,
		OrderTypeTakeProfitLimit,
	}
)

// OrderListLeg는 주문 리스트를 이루는 주문 한 건의 조건이다.
// 방향과 수량은 주문 리스트 종류마다 위치가 달라 각 요청 타입이 따로 가진다.
// 가격 파라미터는 Type에 따라 필수 여부가 달라지며 검증 규칙은 NewOrderRequest와 같다.
type OrderListLeg struct {
	Type               OrderType
	ClientOrderID      string
	Price              string
	StopPrice          string
	TrailingDelta      *int64
	IcebergQuantity    string
	TimeInForce        TimeInForce
	StrategyID         *int64
	StrategyType       *int64
	PegPriceType       string
	PegOffsetType      string
	PegOffsetValue     *int
	QuoteNotionalLimit string
}

// setValues는 주문 조건을 prefix가 붙은 Binance 파라미터 이름으로 values에 쓴다.
// 예를 들어 prefix가 above이면 aboveType, abovePrice, aboveStopPrice가 된다.
func (leg OrderListLeg) setValues(values url.Values, prefix string) {
	setIfNotEmpty(values, prefix+"Type", string(leg.Type))
	setIfNotEmpty(values, prefix+"ClientOrderId", leg.ClientOrderID)
	setIfNotEmpty(values, prefix+"Price", leg.Price)
	setIfNotEmpty(values, prefix+"StopPrice", leg.StopPrice)
	setInt64(values, prefix+"TrailingDelta", leg.TrailingDelta)
	setIfNotEmpty(values, prefix+"IcebergQty", leg.IcebergQuantity)
	setIfNotEmpty(values, prefix+"TimeInForce", string(leg.TimeInForce))
	setInt64(values, prefix+"StrategyId", leg.StrategyID)
	setInt64(values, prefix+"StrategyType", leg.StrategyType)
	setIfNotEmpty(values, prefix+"PegPriceType", leg.PegPriceType)
	setIfNotEmpty(values, prefix+"PegOffsetType", leg.PegOffsetType)
	if leg.PegOffsetValue != nil {
		values.Set(prefix+"PegOffsetValue", strconv.Itoa(*leg.PegOffsetValue))
	}
	setIfNotEmpty(values, prefix+"QuoteNotionalLimit", leg.QuoteNotionalLimit)
}

func (leg OrderListLeg) validate(name string, allowed []OrderType) error {
	if !slices.Contains(allowed, leg.Type) {
		return validationError("%s type %q is not supported", name, leg.Type)
	}
	if err := validateOptionalDecimal(name+" price", leg.Price); err != nil {
		return err
	}
	if err := validateOptionalDecimal(name+" stopPrice", leg.StopPrice); err != nil {
		return err
	}
	if err := validateOptionalDecimal(name+" icebergQty", leg.IcebergQuantity); err != nil {
		return err
	}
	if err := validateOptionalDecimal(name+" quoteNotionalLimit", leg.QuoteNotionalLimit); err != nil {
		return err
	}
	if leg.ClientOrderID != "" && !clientOrderIDPattern.MatchString(leg.ClientOrderID) {
		return validationError("%s clientOrderId has an invalid format", name)
	}
	if leg.TimeInForce != "" && !leg.TimeInForce.valid() {
		return validationError("%s has unsupported time in force %q", name, leg.TimeInForce)
	}
	if leg.TrailingDelta != nil && *leg.TrailingDelta <= 0 {
		return validationError("%s trailingDelta must be positive", name)
	}
	if leg.StrategyType != nil && *leg.StrategyType < 1_000_000 {
		return validationError("%s strategyType must be at least 1000000", name)
	}
	if err := validatePeg(name, leg.PegPriceType, leg.PegOffsetType, leg.PegOffsetValue); err != nil {
		return err
	}
	if leg.IcebergQuantity != "" && leg.TimeInForce != TimeInForceGTC && leg.Type != OrderTypeLimitMaker {
		return validationError("%s iceberg order requires GTC time in force or LIMIT_MAKER", name)
	}
	hasPrice := leg.Price != "" || leg.PegPriceType != ""
	hasTrigger := leg.StopPrice != "" || leg.TrailingDelta != nil
	switch leg.Type {
	case OrderTypeLimit:
		if !hasPrice || leg.TimeInForce == "" || hasTrigger {
			return validationError("%s LIMIT requires price or pegPriceType and timeInForce, without a trigger", name)
		}
	case OrderTypeLimitMaker:
		if !hasPrice || hasTrigger {
			return validationError("%s LIMIT_MAKER requires price or pegPriceType, without a trigger", name)
		}
	case OrderTypeMarket:
		if hasPrice || hasTrigger || leg.TimeInForce != "" || leg.IcebergQuantity != "" {
			return validationError("%s MARKET does not accept price, trigger, timeInForce, or iceberg parameters", name)
		}
	case OrderTypeStopLoss, OrderTypeTakeProfit:
		if !hasTrigger {
			return validationError("%s %s requires stopPrice or trailingDelta", name, leg.Type)
		}
	case OrderTypeStopLossLimit, OrderTypeTakeProfitLimit:
		if !hasPrice || !hasTrigger || leg.TimeInForce == "" {
			return validationError(
				"%s %s requires price or pegPriceType, stopPrice or trailingDelta, and timeInForce",
				name,
				leg.Type,
			)
		}
	}
	return nil
}

// validateOptionalLeg는 아무 값도 없는 주문 조건은 건너뛰고 값이 있으면 검증한다.
func validateOptionalLeg(name string, leg OrderListLeg, allowed []OrderType) error {
	if leg == (OrderListLeg{}) {
		return nil
	}
	return leg.validate(name, allowed)
}

func validateOrderListHeader(symbol, listClientOrderID string, responseType NewOrderResponseType) error {
	if err := validateSymbol(symbol); err != nil {
		return err
	}
	if listClientOrderID != "" && !clientOrderIDPattern.MatchString(listClientOrderID) {
		return validationError("listClientOrderId has an invalid format")
	}
	if responseType != "" && !responseType.valid() {
		return validationError("unsupported response type %q", responseType)
	}
	return nil
}

func orderListHeaderValues(
	symbol, listClientOrderID string,
	responseType NewOrderResponseType,
	selfTradePreventionMode string,
) url.Values {
	values := make(url.Values)
	values.Set("symbol", symbol)
	setIfNotEmpty(values, "listClientOrderId", listClientOrderID)
	setIfNotEmpty(values, "newOrderRespType", string(responseType))
	setIfNotEmpty(values, "selfTradePreventionMode", selfTradePreventionMode)
	return values
}

func validateWorkingOrder(leg OrderListLeg, side Side, quantity string) error {
	if err := validateSide("workingSide", side); err != nil {
		return err
	}
	if err := validatePositiveDecimal("workingQuantity", quantity); err != nil {
		return err
	}
	return leg.validate("working", workingOrderTypes)
}

func setWorkingValues(values url.Values, leg OrderListLeg, side Side, quantity string) {
	values.Set("workingSide", string(side))
	values.Set("workingQuantity", quantity)
	leg.setValues(values, "working")
}

// OCOOrderListRequest는 한쪽이 체결되면 다른 쪽이 취소되는 OCO 주문 리스트다.
// Above와 Below는 같은 Side와 Quantity를 공유한다. 미체결 주문 count 2를 사용한다.
type OCOOrderListRequest struct {
	Symbol                  string
	ListClientOrderID       string
	Side                    Side
	Quantity                string
	Above                   OrderListLeg
	Below                   OrderListLeg
	ResponseType            NewOrderResponseType
	SelfTradePreventionMode string
}

func (request OCOOrderListRequest) validate() error {
	if err := validateOrderListHeader(request.Symbol, request.ListClientOrderID, request.ResponseType); err != nil {
		return err
	}
	if err := validateSide("side", request.Side); err != nil {
		return err
	}
	if err := validatePositiveDecimal("quantity", request.Quantity); err != nil {
		return err
	}
	if err := request.Above.validate("above", ocoOrderTypes); err != nil {
		return err
	}
	return request.Below.validate("below", ocoOrderTypes)
}

func (request OCOOrderListRequest) values() url.Values {
	values := orderListHeaderValues(
		request.Symbol,
		request.ListClientOrderID,
		request.ResponseType,
		request.SelfTradePreventionMode,
	)
	values.Set("side", string(request.Side))
	values.Set("quantity", request.Quantity)
	request.Above.setValues(values, "above")
	request.Below.setValues(values, "below")
	return values
}

// OTOOrderListRequest는 working 주문이 전량 체결되면 pending 주문이 나가는 OTO 주문 리스트다.
// Working은 LIMIT 또는 LIMIT_MAKER여야 한다. 미체결 주문 count 2를 사용한다.
type OTOOrderListRequest struct {
	Symbol                  string
	ListClientOrderID       string
	Working                 OrderListLeg
	WorkingSide             Side
	WorkingQuantity         string
	Pending                 OrderListLeg
	PendingSide             Side
	PendingQuantity         string
	ResponseType            NewOrderResponseType
	SelfTradePreventionMode string
}

func (request OTOOrderListRequest) validate() error {
	if err := validateOrderListHeader(request.Symbol, request.ListClientOrderID, request.ResponseType); err != nil {
		return err
	}
	if err := validateWorkingOrder(request.Working, request.WorkingSide, request.WorkingQuantity); err != nil {
		return err
	}
	if err := validateSide("pendingSide", request.PendingSide); err != nil {
		return err
	}
	if err := validatePositiveDecimal("pendingQuantity", request.PendingQuantity); err != nil {
		return err
	}
	return request.Pending.validate("pending", pendingOrderTypes)
}

func (request OTOOrderListRequest) values() url.Values {
	values := orderListHeaderValues(
		request.Symbol,
		request.ListClientOrderID,
		request.ResponseType,
		request.SelfTradePreventionMode,
	)
	setWorkingValues(values, request.Working, request.WorkingSide, request.WorkingQuantity)
	values.Set("pendingSide", string(request.PendingSide))
	values.Set("pendingQuantity", request.PendingQuantity)
	request.Pending.setValues(values, "pending")
	return values
}

// OTOCOOrderListRequest는 working 주문이 전량 체결되면 위·아래 pending OCO 쌍이 나가는 OTOCO 주문 리스트다.
// 두 pending 주문은 PendingSide와 PendingQuantity를 공유한다.
// 공식 문서가 pendingBelowType을 선택 파라미터로 표시하므로 PendingBelow는 비워 둘 수 있다.
// 미체결 주문 count 3을 사용한다.
type OTOCOOrderListRequest struct {
	Symbol                  string
	ListClientOrderID       string
	Working                 OrderListLeg
	WorkingSide             Side
	WorkingQuantity         string
	PendingSide             Side
	PendingQuantity         string
	PendingAbove            OrderListLeg
	PendingBelow            OrderListLeg
	ResponseType            NewOrderResponseType
	SelfTradePreventionMode string
}

func (request OTOCOOrderListRequest) validate() error {
	if err := validateOrderListHeader(request.Symbol, request.ListClientOrderID, request.ResponseType); err != nil {
		return err
	}
	if err := validateWorkingOrder(request.Working, request.WorkingSide, request.WorkingQuantity); err != nil {
		return err
	}
	if err := validateSide("pendingSide", request.PendingSide); err != nil {
		return err
	}
	if err := validatePositiveDecimal("pendingQuantity", request.PendingQuantity); err != nil {
		return err
	}
	if err := request.PendingAbove.validate("pendingAbove", ocoOrderTypes); err != nil {
		return err
	}
	return validateOptionalLeg("pendingBelow", request.PendingBelow, ocoOrderTypes)
}

func (request OTOCOOrderListRequest) values() url.Values {
	values := orderListHeaderValues(
		request.Symbol,
		request.ListClientOrderID,
		request.ResponseType,
		request.SelfTradePreventionMode,
	)
	setWorkingValues(values, request.Working, request.WorkingSide, request.WorkingQuantity)
	values.Set("pendingSide", string(request.PendingSide))
	values.Set("pendingQuantity", request.PendingQuantity)
	request.PendingAbove.setValues(values, "pendingAbove")
	request.PendingBelow.setValues(values, "pendingBelow")
	return values
}

// OPOOrderListRequest는 working 주문이 체결된 수량만큼 pending 주문이 나가는 OPO 주문 리스트다.
// pending 수량은 working 주문의 체결 수량에서 정해지므로 PendingQuantity가 없다.
// 미체결 주문 count 2를 사용한다.
type OPOOrderListRequest struct {
	Symbol                  string
	ListClientOrderID       string
	Working                 OrderListLeg
	WorkingSide             Side
	WorkingQuantity         string
	Pending                 OrderListLeg
	PendingSide             Side
	ResponseType            NewOrderResponseType
	SelfTradePreventionMode string
}

func (request OPOOrderListRequest) validate() error {
	if err := validateOrderListHeader(request.Symbol, request.ListClientOrderID, request.ResponseType); err != nil {
		return err
	}
	if err := validateWorkingOrder(request.Working, request.WorkingSide, request.WorkingQuantity); err != nil {
		return err
	}
	if err := validateSide("pendingSide", request.PendingSide); err != nil {
		return err
	}
	if request.Pending.QuoteNotionalLimit != "" {
		return validationError("OPO accepts quoteNotionalLimit only on the working order")
	}
	return request.Pending.validate("pending", pendingOrderTypes)
}

func (request OPOOrderListRequest) values() url.Values {
	values := orderListHeaderValues(
		request.Symbol,
		request.ListClientOrderID,
		request.ResponseType,
		request.SelfTradePreventionMode,
	)
	setWorkingValues(values, request.Working, request.WorkingSide, request.WorkingQuantity)
	values.Set("pendingSide", string(request.PendingSide))
	request.Pending.setValues(values, "pending")
	return values
}

// OPOCOOrderListRequest는 working 주문이 체결된 수량만큼 위·아래 pending OCO 쌍이 나가는 OPOCO 주문 리스트다.
// 두 pending 주문은 PendingSide를 공유하며 수량은 working 주문의 체결 수량에서 정해진다.
// PendingBelow는 비워 둘 수 있다. 미체결 주문 count 3을 사용한다.
type OPOCOOrderListRequest struct {
	Symbol                  string
	ListClientOrderID       string
	Working                 OrderListLeg
	WorkingSide             Side
	WorkingQuantity         string
	PendingSide             Side
	PendingAbove            OrderListLeg
	PendingBelow            OrderListLeg
	ResponseType            NewOrderResponseType
	SelfTradePreventionMode string
}

func (request OPOCOOrderListRequest) validate() error {
	if err := validateOrderListHeader(request.Symbol, request.ListClientOrderID, request.ResponseType); err != nil {
		return err
	}
	if err := validateWorkingOrder(request.Working, request.WorkingSide, request.WorkingQuantity); err != nil {
		return err
	}
	if err := validateSide("pendingSide", request.PendingSide); err != nil {
		return err
	}
	if request.PendingAbove.QuoteNotionalLimit != "" || request.PendingBelow.QuoteNotionalLimit != "" {
		return validationError("OPOCO accepts quoteNotionalLimit only on the working order")
	}
	if err := request.PendingAbove.validate("pendingAbove", ocoOrderTypes); err != nil {
		return err
	}
	return validateOptionalLeg("pendingBelow", request.PendingBelow, ocoOrderTypes)
}

func (request OPOCOOrderListRequest) values() url.Values {
	values := orderListHeaderValues(
		request.Symbol,
		request.ListClientOrderID,
		request.ResponseType,
		request.SelfTradePreventionMode,
	)
	setWorkingValues(values, request.Working, request.WorkingSide, request.WorkingQuantity)
	values.Set("pendingSide", string(request.PendingSide))
	request.PendingAbove.setValues(values, "pendingAbove")
	request.PendingBelow.setValues(values, "pendingBelow")
	return values
}

// NewOCOOrderList는 OCO 주문 리스트를 만든다. 불명확한 전송 오류는 QueryOrderList로 상태를 조정해야 한다.
func (client *Client) NewOCOOrderList(
	ctx context.Context,
	request OCOOrderListRequest,
	options ...trade.RequestOption,
) (OrderList, error) {
	if err := request.validate(); err != nil {
		return OrderList{}, err
	}
	return client.placeOrderList(ctx, "/api/v3/orderList/oco", request.values(), 2, options...)
}

// NewOTOOrderList는 OTO 주문 리스트를 만든다.
func (client *Client) NewOTOOrderList(
	ctx context.Context,
	request OTOOrderListRequest,
	options ...trade.RequestOption,
) (OrderList, error) {
	if err := request.validate(); err != nil {
		return OrderList{}, err
	}
	return client.placeOrderList(ctx, "/api/v3/orderList/oto", request.values(), 2, options...)
}

// NewOTOCOOrderList는 OTOCO 주문 리스트를 만든다.
func (client *Client) NewOTOCOOrderList(
	ctx context.Context,
	request OTOCOOrderListRequest,
	options ...trade.RequestOption,
) (OrderList, error) {
	if err := request.validate(); err != nil {
		return OrderList{}, err
	}
	return client.placeOrderList(ctx, "/api/v3/orderList/otoco", request.values(), 3, options...)
}

// NewOPOOrderList는 OPO 주문 리스트를 만든다.
func (client *Client) NewOPOOrderList(
	ctx context.Context,
	request OPOOrderListRequest,
	options ...trade.RequestOption,
) (OrderList, error) {
	if err := request.validate(); err != nil {
		return OrderList{}, err
	}
	return client.placeOrderList(ctx, "/api/v3/orderList/opo", request.values(), 2, options...)
}

// NewOPOCOOrderList는 OPOCO 주문 리스트를 만든다.
func (client *Client) NewOPOCOOrderList(
	ctx context.Context,
	request OPOCOOrderListRequest,
	options ...trade.RequestOption,
) (OrderList, error) {
	if err := request.validate(); err != nil {
		return OrderList{}, err
	}
	return client.placeOrderList(ctx, "/api/v3/orderList/opoco", request.values(), 3, options...)
}

func (client *Client) placeOrderList(
	ctx context.Context,
	path string,
	values url.Values,
	orderUnits int,
	options ...trade.RequestOption,
) (OrderList, error) {
	body, err := client.mutateSigned(ctx, http.MethodPost, path, values, 1, orderUnits, options...)
	if err != nil {
		return OrderList{}, err
	}
	return decodeOrderList(body)
}

// CancelOrderListRequest는 주문 리스트 전체를 취소하는 조건이다.
// OrderListID 또는 ListClientOrderID로 지정하며 둘 다 주면 두 값이 같은 주문 리스트를 가리켜야 한다.
type CancelOrderListRequest struct {
	Symbol            string
	OrderListID       *int64
	ListClientOrderID string
	NewClientOrderID  string
}

func (request CancelOrderListRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateOrderListIdentity(request.OrderListID, request.ListClientOrderID); err != nil {
		return err
	}
	if request.NewClientOrderID != "" && !clientOrderIDPattern.MatchString(request.NewClientOrderID) {
		return validationError("newClientOrderId has an invalid format")
	}
	return nil
}

func (request CancelOrderListRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	setInt64(values, "orderListId", request.OrderListID)
	setIfNotEmpty(values, "listClientOrderId", request.ListClientOrderID)
	setIfNotEmpty(values, "newClientOrderId", request.NewClientOrderID)
	return values
}

// CancelOrderList는 주문 리스트 전체를 취소한다.
// 리스트에 속한 개별 주문을 취소해도 주문 리스트 전체가 취소된다.
func (client *Client) CancelOrderList(
	ctx context.Context,
	request CancelOrderListRequest,
	options ...trade.RequestOption,
) (OrderList, error) {
	if err := request.validate(); err != nil {
		return OrderList{}, err
	}
	body, err := client.mutateSigned(ctx, http.MethodDelete, "/api/v3/orderList", request.values(), 1, 0, options...)
	if err != nil {
		return OrderList{}, err
	}
	return decodeOrderList(body)
}

// QueryOrderListRequest는 주문 리스트 한 건의 조회 조건이다.
// OrderListID 또는 ListClientOrderID 중 하나 이상이 필요하다.
type QueryOrderListRequest struct {
	OrderListID       *int64
	ListClientOrderID string
}

func (request QueryOrderListRequest) validate() error {
	return validateOrderListIdentity(request.OrderListID, request.ListClientOrderID)
}

func (request QueryOrderListRequest) values() url.Values {
	values := make(url.Values)
	setInt64(values, "orderListId", request.OrderListID)
	setIfNotEmpty(values, "origClientOrderId", request.ListClientOrderID)
	return values
}

// QueryOrderList는 주문 리스트 한 건의 상태를 조회한다. 응답에는 OrderReports가 없다.
func (client *Client) QueryOrderList(
	ctx context.Context,
	request QueryOrderListRequest,
	options ...trade.RequestOption,
) (OrderList, error) {
	if err := request.validate(); err != nil {
		return OrderList{}, err
	}
	body, err := client.readSigned(ctx, "/api/v3/orderList", request.values(), 4, options...)
	if err != nil {
		return OrderList{}, err
	}
	return decodeOrderList(body)
}

// AllOrderListsRequest는 주문 리스트 이력의 조회 범위다.
// FromID는 StartTime·EndTime과 함께 쓸 수 없고, 두 시각의 간격은 24시간을 넘을 수 없다.
type AllOrderListsRequest struct {
	FromID    *int64
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
}

func (request AllOrderListsRequest) validate() error {
	if request.FromID != nil && *request.FromID < 0 {
		return validationError("allOrderList fromId cannot be negative")
	}
	if request.FromID != nil && (request.StartTime != nil || request.EndTime != nil) {
		return validationError("allOrderList fromId cannot be combined with startTime or endTime")
	}
	if err := validateDayWindow("allOrderList", request.StartTime, request.EndTime); err != nil {
		return err
	}
	return validateOptionalLimit("allOrderList", request.Limit, 1000)
}

func (request AllOrderListsRequest) values() url.Values {
	values := make(url.Values)
	setInt64(values, "fromId", request.FromID)
	setTimeMillis(values, "startTime", request.StartTime)
	setTimeMillis(values, "endTime", request.EndTime)
	setLimit(values, request.Limit)
	return values
}

// AllOrderLists는 주문 리스트 이력을 조회한다.
func (client *Client) AllOrderLists(
	ctx context.Context,
	request AllOrderListsRequest,
	options ...trade.RequestOption,
) ([]OrderList, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	body, err := client.readSigned(ctx, "/api/v3/allOrderList", request.values(), 20, options...)
	if err != nil {
		return nil, err
	}
	return decodeOrderLists(body)
}

// OpenOrderLists는 진행 중인 모든 주문 리스트를 조회한다.
func (client *Client) OpenOrderLists(ctx context.Context, options ...trade.RequestOption) ([]OrderList, error) {
	body, err := client.readSigned(ctx, "/api/v3/openOrderList", nil, 6, options...)
	if err != nil {
		return nil, err
	}
	return decodeOrderLists(body)
}

func validateOrderListIdentity(orderListID *int64, listClientOrderID string) error {
	if orderListID == nil && listClientOrderID == "" {
		return validationError("orderListId or listClientOrderId is required")
	}
	if orderListID != nil && *orderListID < 0 {
		return validationError("orderListId cannot be negative")
	}
	if listClientOrderID != "" && !clientOrderIDPattern.MatchString(listClientOrderID) {
		return validationError("listClientOrderId has an invalid format")
	}
	return nil
}
