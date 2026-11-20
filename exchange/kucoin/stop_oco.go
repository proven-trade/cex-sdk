package kucoin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// StopDirection은 스탑 주문 트리거 방향이다.
type StopDirection string

const (
	// StopLoss는 가격이 stopPrice 이하가 되면 트리거한다. 기본값이다.
	StopLoss StopDirection = "loss"
	// StopEntry는 가격이 stopPrice 이상이 되면 트리거한다.
	StopEntry StopDirection = "entry"
)

// StopOrderRequest는 Classic Spot 스탑 주문 생성 조건이다. 거래 유형은 Spot(TRADE)으로 고정된다.
type StopOrderRequest struct {
	PlaceOrderRequest
	StopPrice string        `json:"stopPrice"`
	Stop      StopDirection `json:"stop,omitempty"`
}

func (request StopOrderRequest) validate() error {
	if err := request.PlaceOrderRequest.validate(); err != nil {
		return err
	}
	if err := validatePositiveDecimal("stopPrice", request.StopPrice); err != nil {
		return err
	}
	if request.Stop != "" && request.Stop != StopLoss && request.Stop != StopEntry {
		return validationError("stop direction must be loss, entry, or empty")
	}
	return nil
}

// StopOrderReference는 스탑 주문 생성 접수 결과다.
type StopOrderReference struct {
	OrderID string          `json:"orderId"`
	Raw     json.RawMessage `json:"-"`
}

// StopOrderIdentity는 스탑 주문을 주문 ID 또는 사용자 주문 ID로 지정한다.
// 사용자 주문 ID 조회·취소에서 Symbol은 선택이다.
type StopOrderIdentity struct {
	OrderID       string
	ClientOrderID string
	Symbol        string
}

func (request StopOrderIdentity) validate() error {
	if err := validateOrderIdentity(request.OrderID, request.ClientOrderID); err != nil {
		return err
	}
	return validateOptionalSymbol(request.Symbol)
}

func (request StopOrderIdentity) values() url.Values {
	values := make(url.Values)
	if request.OrderID == "" {
		values.Set("clientOid", request.ClientOrderID)
		setIfNotEmpty(values, "symbol", request.Symbol)
	}
	return values
}

// StopOrderCancellation은 스탑 주문 취소 결과다. 사용자 주문 ID 취소에서는 CancelledOrderID가 채워진다.
type StopOrderCancellation struct {
	ClientOrderID     string          `json:"clientOid"`
	CancelledOrderID  string          `json:"cancelledOrderId"`
	CancelledOrderIDs []string        `json:"cancelledOrderIds"`
	Raw               json.RawMessage `json:"-"`
}

// CancelStopOrdersRequest는 스탑 주문 일괄 취소 조건이다. 조건이 모두 비면 모든 스탑 주문이 대상이다.
type CancelStopOrdersRequest struct {
	Symbol   string
	OrderIDs []string
}

func (request CancelStopOrdersRequest) validate() error {
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return err
	}
	return validateIDList("order IDs", request.OrderIDs, 500)
}

func (request CancelStopOrdersRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "symbol", request.Symbol)
	if len(request.OrderIDs) > 0 {
		values.Set("orderIds", joinList(request.OrderIDs))
	}
	return values
}

// CancelledOrderIDs는 일괄 취소로 취소된 주문 ID 목록이다.
type CancelledOrderIDs struct {
	OrderIDs []string        `json:"cancelledOrderIds"`
	Raw      json.RawMessage `json:"-"`
}

// StopOrdersRequest는 스탑 주문 목록 조회 조건이다. PageSize는 10~500(기본 50)이다.
type StopOrdersRequest struct {
	Symbol      string
	Side        Side
	Type        OrderType
	Start       *time.Time
	End         *time.Time
	CurrentPage int
	PageSize    int
	OrderIDs    []string
}

func (request StopOrdersRequest) validate() error {
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateOptionalSide(request.Side); err != nil {
		return err
	}
	if err := validateOptionalOrderType(request.Type); err != nil {
		return err
	}
	if err := validateTimeRange(request.Start, request.End); err != nil {
		return err
	}
	if err := validatePage(request.CurrentPage, request.PageSize, 10, 500); err != nil {
		return err
	}
	return validateIDList("order IDs", request.OrderIDs, 500)
}

func (request StopOrdersRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "symbol", request.Symbol)
	setIfNotEmpty(values, "side", string(request.Side))
	setIfNotEmpty(values, "type", string(request.Type))
	setMillis(values, "startAt", request.Start)
	setMillis(values, "endAt", request.End)
	setPositiveInt(values, "currentPage", request.CurrentPage)
	setPositiveInt(values, "pageSize", request.PageSize)
	if len(request.OrderIDs) > 0 {
		values.Set("orderIds", joinList(request.OrderIDs))
	}
	return values
}

// StopOrder는 스탑 주문 한 건이다.
type StopOrder struct {
	ID                string          `json:"id"`
	Symbol            string          `json:"symbol"`
	UserID            string          `json:"userId"`
	Status            string          `json:"status"`
	Type              OrderType       `json:"type"`
	Side              Side            `json:"side"`
	Price             string          `json:"price"`
	Size              string          `json:"size"`
	Funds             string          `json:"funds"`
	SelfTradeMode     string          `json:"stp"`
	TimeInForce       TimeInForce     `json:"timeInForce"`
	CancelAfter       int64           `json:"cancelAfter"`
	PostOnly          bool            `json:"postOnly"`
	Hidden            bool            `json:"hidden"`
	Iceberg           bool            `json:"iceberg"`
	VisibleSize       string          `json:"visibleSize"`
	Channel           string          `json:"channel"`
	ClientOrderID     string          `json:"clientOid"`
	Remark            string          `json:"remark"`
	Tags              string          `json:"tags"`
	OrderTime         int64           `json:"orderTime"`
	DomainID          string          `json:"domainId"`
	TradeSource       string          `json:"tradeSource"`
	TradeType         string          `json:"tradeType"`
	FeeCurrency       string          `json:"feeCurrency"`
	TakerFeeRate      string          `json:"takerFeeRate"`
	MakerFeeRate      string          `json:"makerFeeRate"`
	CreatedAt         int64           `json:"createdAt"`
	Stop              string          `json:"stop"`
	StopTriggerTime   int64           `json:"stopTriggerTime"`
	StopPrice         string          `json:"stopPrice"`
	RelatedNumber     string          `json:"relatedNo"`
	LimitPrice        string          `json:"limitPrice"`
	Pop               string          `json:"pop"`
	ActivateCondition string          `json:"activateCondition"`
	Raw               json.RawMessage `json:"-"`
}

// PlaceStopOrder는 Classic Spot 스탑 주문을 생성한다.
func (client *Client) PlaceStopOrder(
	ctx context.Context,
	request StopOrderRequest,
	options ...trade.RequestOption,
) (StopOrderReference, error) {
	if err := request.validate(); err != nil {
		return StopOrderReference{}, err
	}
	var reference StopOrderReference
	response, data, err := client.mutate(
		ctx, http.MethodPost, "/api/v1/stop-order", nil, request,
		spotLimit(1), credential.PermissionTrade, &reference, options...,
	)
	if err != nil {
		return StopOrderReference{}, err
	}
	if reference.OrderID == "" {
		return StopOrderReference{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errEmptyAcknowledgement,
		)
	}
	reference.Raw = cloneBytes(data)
	return reference, nil
}

// CancelStopOrder는 스탑 주문 한 건의 취소를 접수한다.
func (client *Client) CancelStopOrder(
	ctx context.Context,
	request StopOrderIdentity,
	options ...trade.RequestOption,
) (StopOrderCancellation, error) {
	if err := request.validate(); err != nil {
		return StopOrderCancellation{}, err
	}
	path, limit := "/api/v1/stop-order/cancelOrderByClientOid", spotLimit(5)
	if request.OrderID != "" {
		path, limit = "/api/v1/stop-order/"+url.PathEscape(request.OrderID), spotLimit(3)
	}
	var result StopOrderCancellation
	_, data, err := client.mutate(
		ctx, http.MethodDelete, path, request.values(), nil, limit,
		credential.PermissionTrade, &result, options...,
	)
	if err != nil {
		return StopOrderCancellation{}, err
	}
	result.Raw = cloneBytes(data)
	return result, nil
}

// CancelStopOrders는 조건에 맞는 스탑 주문을 일괄 취소한다.
func (client *Client) CancelStopOrders(
	ctx context.Context,
	request CancelStopOrdersRequest,
	options ...trade.RequestOption,
) (CancelledOrderIDs, error) {
	if err := request.validate(); err != nil {
		return CancelledOrderIDs{}, err
	}
	var result CancelledOrderIDs
	_, data, err := client.mutate(
		ctx, http.MethodDelete, "/api/v1/stop-order/cancel", request.values(), nil,
		spotLimit(3), credential.PermissionTrade, &result, options...,
	)
	if err != nil {
		return CancelledOrderIDs{}, err
	}
	result.Raw = cloneBytes(data)
	return result, nil
}

// StopOrders는 스탑 주문 목록을 페이지 단위로 조회한다.
func (client *Client) StopOrders(
	ctx context.Context,
	request StopOrdersRequest,
	options ...trade.RequestOption,
) (Page[StopOrder], error) {
	if err := request.validate(); err != nil {
		return Page[StopOrder]{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/stop-order", request.values(), nil,
		spotLimit(8), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return Page[StopOrder]{}, err
	}
	return decodePage(client, response, commonexchange.OperationRead, func(item *StopOrder, raw json.RawMessage) {
		item.Raw = raw
	})
}

// StopOrderInfo는 스탑 주문 한 건을 주문 ID 또는 사용자 주문 ID로 조회한다.
// 사용자 주문 ID 조회는 같은 ID의 주문 배열을 반환할 수 있으므로 슬라이스로 돌려준다.
func (client *Client) StopOrderInfo(
	ctx context.Context,
	request StopOrderIdentity,
	options ...trade.RequestOption,
) ([]StopOrder, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	if request.OrderID == "" {
		response, err := client.executePrivate(
			ctx, http.MethodGet, "/api/v1/stop-order/queryOrderByClientOid", request.values(), nil,
			spotLimit(3), credential.PermissionRead, commonexchange.OperationRead, options...,
		)
		if err != nil {
			return nil, err
		}
		return decodeItems(client, response, commonexchange.OperationRead, func(item *StopOrder, raw json.RawMessage) {
			item.Raw = raw
		})
	}
	var order StopOrder
	data, err := client.readPrivate(
		ctx, "/api/v1/stop-order/"+url.PathEscape(request.OrderID), nil, spotLimit(3), &order, options...,
	)
	if err != nil {
		return nil, err
	}
	order.Raw = cloneBytes(data)
	return []StopOrder{order}, nil
}

// OCOOrderRequest는 Classic Spot OCO 주문 생성 조건이다. 거래 유형은 Spot(TRADE)으로 고정된다.
type OCOOrderRequest struct {
	ClientOrderID string `json:"clientOid"`
	Side          Side   `json:"side"`
	Symbol        string `json:"symbol"`
	Remark        string `json:"remark,omitempty"`
	Price         string `json:"price"`
	Size          string `json:"size"`
	StopPrice     string `json:"stopPrice"`
	LimitPrice    string `json:"limitPrice"`
}

func (request OCOOrderRequest) validate() error {
	if !clientOrderIDPattern.MatchString(request.ClientOrderID) {
		return validationError("client order ID is required and must match [0-9A-Za-z_-]{1,40}")
	}
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("order side must be buy or sell")
	}
	if len(request.Remark) > 20 {
		return validationError("order remark cannot exceed 20 bytes")
	}
	for name, value := range map[string]string{
		"price": request.Price, "size": request.Size,
		"stopPrice": request.StopPrice, "limitPrice": request.LimitPrice,
	} {
		if err := validatePositiveDecimal(name, value); err != nil {
			return err
		}
	}
	return nil
}

// OCOOrderReference는 OCO 주문 생성 접수 결과다.
type OCOOrderReference struct {
	OrderID string          `json:"orderId"`
	Raw     json.RawMessage `json:"-"`
}

// OCOOrderIdentity는 OCO 주문을 주문 ID 또는 사용자 주문 ID로 지정한다.
type OCOOrderIdentity struct {
	OrderID       string
	ClientOrderID string
}

func (request OCOOrderIdentity) validate() error {
	return validateOrderIdentity(request.OrderID, request.ClientOrderID)
}

func (request OCOOrderIdentity) path(orderPrefix, clientPrefix string) string {
	if request.OrderID != "" {
		return orderPrefix + url.PathEscape(request.OrderID)
	}
	return clientPrefix + url.PathEscape(request.ClientOrderID)
}

// CancelOCOOrdersRequest는 OCO 주문 일괄 취소 조건이다. 조건이 모두 비면 모든 OCO 주문이 대상이다.
type CancelOCOOrdersRequest struct {
	Symbol   string
	OrderIDs []string
}

func (request CancelOCOOrdersRequest) validate() error {
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return err
	}
	return validateIDList("order IDs", request.OrderIDs, 500)
}

func (request CancelOCOOrdersRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "symbol", request.Symbol)
	if len(request.OrderIDs) > 0 {
		values.Set("orderIds", joinList(request.OrderIDs))
	}
	return values
}

// OCOOrder는 OCO 주문 요약이다.
type OCOOrder struct {
	OrderID       string          `json:"orderId"`
	Symbol        string          `json:"symbol"`
	ClientOrderID string          `json:"clientOid"`
	OrderTime     int64           `json:"orderTime"`
	Status        string          `json:"status"`
	Raw           json.RawMessage `json:"-"`
}

// OCOSubOrder는 OCO 주문을 구성하는 하위 주문이다.
type OCOSubOrder struct {
	ID        string `json:"id"`
	Symbol    string `json:"symbol"`
	Side      Side   `json:"side"`
	Price     string `json:"price"`
	StopPrice string `json:"stopPrice"`
	Size      string `json:"size"`
	Status    string `json:"status"`
}

// OCOOrderDetail은 OCO 주문과 하위 주문이다.
type OCOOrderDetail struct {
	OrderID       string          `json:"orderId"`
	Symbol        string          `json:"symbol"`
	ClientOrderID string          `json:"clientOid"`
	OrderTime     int64           `json:"orderTime"`
	Status        string          `json:"status"`
	Orders        []OCOSubOrder   `json:"orders"`
	Raw           json.RawMessage `json:"-"`
}

// OCOOrdersRequest는 OCO 주문 목록 조회 조건이다. PageSize는 10~500(기본 50)이다.
type OCOOrdersRequest struct {
	Symbol      string
	Start       *time.Time
	End         *time.Time
	OrderIDs    []string
	PageSize    int
	CurrentPage int
}

func (request OCOOrdersRequest) validate() error {
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateTimeRange(request.Start, request.End); err != nil {
		return err
	}
	if err := validatePage(request.CurrentPage, request.PageSize, 10, 500); err != nil {
		return err
	}
	return validateIDList("order IDs", request.OrderIDs, 500)
}

func (request OCOOrdersRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "symbol", request.Symbol)
	setMillis(values, "startAt", request.Start)
	setMillis(values, "endAt", request.End)
	setPositiveInt(values, "currentPage", request.CurrentPage)
	setPositiveInt(values, "pageSize", request.PageSize)
	if len(request.OrderIDs) > 0 {
		values.Set("orderIds", joinList(request.OrderIDs))
	}
	return values
}

// PlaceOCOOrder는 Classic Spot OCO 주문을 생성한다.
func (client *Client) PlaceOCOOrder(
	ctx context.Context,
	request OCOOrderRequest,
	options ...trade.RequestOption,
) (OCOOrderReference, error) {
	if err := request.validate(); err != nil {
		return OCOOrderReference{}, err
	}
	var reference OCOOrderReference
	response, data, err := client.mutate(
		ctx, http.MethodPost, "/api/v3/oco/order", nil, request,
		spotLimit(2), credential.PermissionTrade, &reference, options...,
	)
	if err != nil {
		return OCOOrderReference{}, err
	}
	if reference.OrderID == "" {
		return OCOOrderReference{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errEmptyAcknowledgement,
		)
	}
	reference.Raw = cloneBytes(data)
	return reference, nil
}

// CancelOCOOrder는 OCO 주문 한 건의 취소를 접수한다.
func (client *Client) CancelOCOOrder(
	ctx context.Context,
	request OCOOrderIdentity,
	options ...trade.RequestOption,
) (CancelledOrderIDs, error) {
	if err := request.validate(); err != nil {
		return CancelledOrderIDs{}, err
	}
	var result CancelledOrderIDs
	_, data, err := client.mutate(
		ctx, http.MethodDelete, request.path("/api/v3/oco/order/", "/api/v3/oco/client-order/"), nil, nil,
		spotLimit(3), credential.PermissionTrade, &result, options...,
	)
	if err != nil {
		return CancelledOrderIDs{}, err
	}
	result.Raw = cloneBytes(data)
	return result, nil
}

// CancelOCOOrders는 조건에 맞는 OCO 주문을 일괄 취소한다.
func (client *Client) CancelOCOOrders(
	ctx context.Context,
	request CancelOCOOrdersRequest,
	options ...trade.RequestOption,
) (CancelledOrderIDs, error) {
	if err := request.validate(); err != nil {
		return CancelledOrderIDs{}, err
	}
	var result CancelledOrderIDs
	_, data, err := client.mutate(
		ctx, http.MethodDelete, "/api/v3/oco/orders", request.values(), nil,
		spotLimit(3), credential.PermissionTrade, &result, options...,
	)
	if err != nil {
		return CancelledOrderIDs{}, err
	}
	result.Raw = cloneBytes(data)
	return result, nil
}

// OCOOrderInfo는 OCO 주문 요약을 주문 ID 또는 사용자 주문 ID로 조회한다.
func (client *Client) OCOOrderInfo(
	ctx context.Context,
	request OCOOrderIdentity,
	options ...trade.RequestOption,
) (OCOOrder, error) {
	if err := request.validate(); err != nil {
		return OCOOrder{}, err
	}
	var order OCOOrder
	data, err := client.readPrivate(
		ctx, request.path("/api/v3/oco/order/", "/api/v3/oco/client-order/"), nil, spotLimit(2), &order, options...,
	)
	if err != nil {
		return OCOOrder{}, err
	}
	order.Raw = cloneBytes(data)
	return order, nil
}

// OCOOrderDetail은 OCO 주문과 하위 주문을 주문 ID로 조회한다.
func (client *Client) OCOOrderDetail(
	ctx context.Context,
	orderID string,
	options ...trade.RequestOption,
) (OCOOrderDetail, error) {
	if err := validateOrderID(orderID); err != nil {
		return OCOOrderDetail{}, err
	}
	var detail OCOOrderDetail
	data, err := client.readPrivate(
		ctx, "/api/v3/oco/order/details/"+url.PathEscape(orderID), nil, spotLimit(2), &detail, options...,
	)
	if err != nil {
		return OCOOrderDetail{}, err
	}
	detail.Raw = cloneBytes(data)
	return detail, nil
}

// OCOOrders는 OCO 주문 목록을 페이지 단위로 조회한다.
func (client *Client) OCOOrders(
	ctx context.Context,
	request OCOOrdersRequest,
	options ...trade.RequestOption,
) (Page[OCOOrder], error) {
	if err := request.validate(); err != nil {
		return Page[OCOOrder]{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v3/oco/orders", request.values(), nil,
		spotLimit(2), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return Page[OCOOrder]{}, err
	}
	return decodePage(client, response, commonexchange.OperationRead, func(item *OCOOrder, raw json.RawMessage) {
		item.Raw = raw
	})
}
