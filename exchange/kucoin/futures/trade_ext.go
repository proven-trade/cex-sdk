package futures

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

const (
	maximumBatchOrders       = 20
	maximumBatchCancelOrders = 10
	maximumExtendedPageSize  = 1000
)

// StopPriceType은 조건 가격을 판정하는 기준 가격이다.
type StopPriceType string

const (
	StopPriceTypeTrade StopPriceType = "TP"
	StopPriceTypeMark  StopPriceType = "MP"
	StopPriceTypeIndex StopPriceType = "IP"
)

// TakeProfitStopLossOrderRequest는 익절·손절 가격을 함께 지정하는 Futures 주문이다.
// 주문 본문은 PlaceOrderRequest와 같고 TriggerStopUpPrice(익절)와 TriggerStopDownPrice(손절) 중 하나 이상이 필요하다.
type TakeProfitStopLossOrderRequest struct {
	PlaceOrderRequest
	StopPriceType        StopPriceType `json:"stopPriceType,omitempty"`
	TriggerStopUpPrice   string        `json:"triggerStopUpPrice,omitempty"`
	TriggerStopDownPrice string        `json:"triggerStopDownPrice,omitempty"`
}

// BatchOrderResult는 일괄 주문 생성에서 주문 한 건의 접수 결과다.
// 요청 전체가 성공해도 항목별 Code가 "200000"이 아닐 수 있으므로 항목마다 확인해야 한다.
type BatchOrderResult struct {
	OrderID       string          `json:"orderId"`
	ClientOrderID string          `json:"clientOid"`
	Symbol        string          `json:"symbol"`
	Code          string          `json:"code"`
	Message       string          `json:"msg"`
	Raw           json.RawMessage `json:"-"`
}

// ClientOrderRef는 일괄 취소에서 사용자 주문 ID로 취소할 주문 한 건이다.
type ClientOrderRef struct {
	Symbol        string `json:"symbol"`
	ClientOrderID string `json:"clientOid"`
}

// BatchCancelOrdersRequest는 주문 ID 목록 또는 사용자 주문 ID 목록 중 정확히 하나로 최대 10건을 취소한다.
type BatchCancelOrdersRequest struct {
	OrderIDs     []string         `json:"orderIdsList,omitempty"`
	ClientOrders []ClientOrderRef `json:"clientOidsList,omitempty"`
}

// BatchCancelResult는 일괄 취소에서 주문 한 건의 처리 결과다.
// 주문 ID로 취소하면 OrderID가, 사용자 주문 ID로 취소하면 ClientOrderID가 채워진다.
type BatchCancelResult struct {
	OrderID       string          `json:"orderId"`
	ClientOrderID string          `json:"clientOid"`
	Code          string          `json:"code"`
	Message       string          `json:"msg"`
	Raw           json.RawMessage `json:"-"`
}

// CancelAllOrdersRequest는 계약 하나의 모든 미체결 주문 취소 조건이다.
type CancelAllOrdersRequest struct {
	Symbol string
}

// CancelAllStopOrdersRequest는 미발동 스탑 주문 일괄 취소 조건이다. Symbol이 비어 있으면 모든 계약이 대상이다.
type CancelAllStopOrdersRequest struct {
	Symbol string
}

// OrderByClientOrderIDRequest는 사용자 주문 ID로 주문 한 건을 조회한다.
type OrderByClientOrderIDRequest struct {
	ClientOrderID string
}

// ClosedOrdersRequest는 종료된 Futures 주문의 페이지 조회 조건이다. PageSize는 최대 1000이다.
type ClosedOrdersRequest struct {
	Symbol      string
	Side        Side
	Type        string
	StartAt     *time.Time
	EndAt       *time.Time
	CurrentPage int
	PageSize    int
}

// StopOrdersRequest는 미발동 스탑 주문의 페이지 조회 조건이다. PageSize는 최대 1000이다.
type StopOrdersRequest struct {
	Symbol      string
	Side        Side
	Type        OrderType
	StartAt     *time.Time
	EndAt       *time.Time
	CurrentPage int
	PageSize    int
}

// RecentClosedOrdersRequest는 최근 24시간 종료 주문 조회 조건이다. Symbol이 비어 있으면 모든 계약이 대상이다.
type RecentClosedOrdersRequest struct {
	Symbol string
}

// OpenOrderValueRequest는 계약 하나의 미체결 주문 가치 조회 조건이다.
type OpenOrderValueRequest struct {
	Symbol string
}

// RecentFillsRequest는 최근 24시간 체결 조회 조건이다. Symbol이 비어 있으면 모든 계약이 대상이다.
type RecentFillsRequest struct {
	Symbol string
}

// OpenOrderValue는 계약 하나의 미체결 매수·매도 수량과 필요 비용이다.
type OpenOrderValue struct {
	OpenOrderBuySize  int64           `json:"openOrderBuySize"`
	OpenOrderSellSize int64           `json:"openOrderSellSize"`
	OpenOrderBuyCost  Decimal         `json:"openOrderBuyCost"`
	OpenOrderSellCost Decimal         `json:"openOrderSellCost"`
	SettleCurrency    string          `json:"settleCurrency"`
	Raw               json.RawMessage `json:"-"`
}

// PlaceOrderTest는 주문 요청과 서명을 검증한다. 주문은 매칭 엔진에 들어가지 않으며 조회할 수도 없다.
func (client *Client) PlaceOrderTest(
	ctx context.Context,
	request PlaceOrderRequest,
	options ...trade.RequestOption,
) (OrderReference, error) {
	if err := request.validate(); err != nil {
		return OrderReference{}, err
	}
	body, err := json.Marshal(request)
	if err != nil {
		return OrderReference{}, validationError("encode KuCoin Futures test order: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/api/v1/orders/test", nil, body,
		futuresLimit(2), credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return OrderReference{}, err
	}
	return decodeObject(client, response, commonexchange.OperationMutation,
		func(value *OrderReference, raw json.RawMessage) { value.Raw = raw })
}

// BatchPlaceOrders는 최대 20건의 지정가·시장가 주문을 한 번에 생성한다.
// 항목별 성공 여부는 각 결과의 Code로 판단하며 SDK는 자동 재시도하지 않는다.
func (client *Client) BatchPlaceOrders(
	ctx context.Context,
	requests []PlaceOrderRequest,
	options ...trade.RequestOption,
) ([]BatchOrderResult, error) {
	if len(requests) == 0 || len(requests) > maximumBatchOrders {
		return nil, validationError("batch order count must be between 1 and %d", maximumBatchOrders)
	}
	for _, request := range requests {
		if err := request.validate(); err != nil {
			return nil, err
		}
	}
	body, err := json.Marshal(requests)
	if err != nil {
		return nil, validationError("encode KuCoin Futures batch orders: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/api/v1/orders/multi", nil, body,
		futuresLimit(20), credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList(client, response, commonexchange.OperationMutation,
		func(value *BatchOrderResult, raw json.RawMessage) { value.Raw = raw })
}

// PlaceTakeProfitStopLossOrder는 익절·손절 가격을 함께 지정한 Futures 주문을 생성한다.
func (client *Client) PlaceTakeProfitStopLossOrder(
	ctx context.Context,
	request TakeProfitStopLossOrderRequest,
	options ...trade.RequestOption,
) (OrderReference, error) {
	if err := request.validate(); err != nil {
		return OrderReference{}, err
	}
	body, err := json.Marshal(request)
	if err != nil {
		return OrderReference{}, validationError("encode KuCoin Futures TP/SL order: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/api/v1/st-orders", nil, body,
		futuresLimit(2), credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return OrderReference{}, err
	}
	reference, err := decodeObject(client, response, commonexchange.OperationMutation,
		func(value *OrderReference, raw json.RawMessage) { value.Raw = raw })
	if err != nil {
		return OrderReference{}, err
	}
	if reference.OrderID == "" {
		return OrderReference{}, client.decodeBodyError(
			response, commonexchange.OperationMutation,
			validationError("KuCoin Futures TP/SL order acknowledgement is empty"),
		)
	}
	return reference, nil
}

// BatchCancelOrders는 주문 ID 또는 사용자 주문 ID 목록으로 최대 10건을 취소한다.
// 항목별 성공 여부는 각 결과의 Code로 판단한다.
func (client *Client) BatchCancelOrders(
	ctx context.Context,
	request BatchCancelOrdersRequest,
	options ...trade.RequestOption,
) ([]BatchCancelResult, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, validationError("encode KuCoin Futures batch cancel: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodDelete, "/api/v1/orders/multi-cancel", nil, body,
		futuresLimit(20), credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList(client, response, commonexchange.OperationMutation,
		func(value *BatchCancelResult, raw json.RawMessage) { value.Raw = raw })
}

// CancelAllOrders는 지정한 계약의 모든 미체결 주문 취소를 접수한다.
func (client *Client) CancelAllOrders(
	ctx context.Context,
	request CancelAllOrdersRequest,
	options ...trade.RequestOption,
) (CancelResult, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return CancelResult{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodDelete, "/api/v3/orders", url.Values{"symbol": {request.Symbol}}, nil,
		futuresLimit(10), credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CancelResult{}, err
	}
	return decodeObject(client, response, commonexchange.OperationMutation,
		func(value *CancelResult, raw json.RawMessage) { value.Raw = raw })
}

// CancelAllStopOrders는 미발동 스탑 주문 취소를 접수한다. Symbol이 비어 있으면 모든 계약이 대상이다.
func (client *Client) CancelAllStopOrders(
	ctx context.Context,
	request CancelAllStopOrdersRequest,
	options ...trade.RequestOption,
) (CancelResult, error) {
	values := make(url.Values)
	if request.Symbol != "" {
		if err := validateSymbol(request.Symbol); err != nil {
			return CancelResult{}, err
		}
		values.Set("symbol", request.Symbol)
	}
	response, err := client.executePrivate(
		ctx, http.MethodDelete, "/api/v1/stopOrders", values, nil,
		futuresLimit(15), credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CancelResult{}, err
	}
	return decodeObject(client, response, commonexchange.OperationMutation,
		func(value *CancelResult, raw json.RawMessage) { value.Raw = raw })
}

// OrderByClientOrderID는 사용자 주문 ID로 Futures 주문 한 건을 조회한다.
func (client *Client) OrderByClientOrderID(
	ctx context.Context,
	request OrderByClientOrderIDRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if !clientOrderIDPattern.MatchString(request.ClientOrderID) {
		return Order{}, validationError("invalid client order ID")
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/orders/byClientOid",
		url.Values{"clientOid": {request.ClientOrderID}}, nil,
		futuresLimit(5), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return Order{}, err
	}
	return decodeObject(client, response, commonexchange.OperationRead,
		func(value *Order, raw json.RawMessage) { value.Raw = raw })
}

// ClosedOrders는 종료된 Futures 주문을 페이지 번호 기반으로 조회한다.
func (client *Client) ClosedOrders(
	ctx context.Context,
	request ClosedOrdersRequest,
	options ...trade.RequestOption,
) (OrderPage, error) {
	if err := request.validate(); err != nil {
		return OrderPage{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/orders", request.values(), nil,
		futuresLimit(2), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return OrderPage{}, err
	}
	return client.decodeOrderPage(response)
}

// StopOrders는 아직 발동하지 않은 Futures 스탑 주문을 페이지 번호 기반으로 조회한다.
func (client *Client) StopOrders(
	ctx context.Context,
	request StopOrdersRequest,
	options ...trade.RequestOption,
) (OrderPage, error) {
	if err := request.validate(); err != nil {
		return OrderPage{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/stopOrders", request.values(), nil,
		futuresLimit(6), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return OrderPage{}, err
	}
	return client.decodeOrderPage(response)
}

// RecentClosedOrders는 최근 24시간 동안 종료된 Futures 주문을 최대 1000건까지 조회한다.
func (client *Client) RecentClosedOrders(
	ctx context.Context,
	request RecentClosedOrdersRequest,
	options ...trade.RequestOption,
) ([]Order, error) {
	values, err := optionalSymbolValues(request.Symbol)
	if err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/recentDoneOrders", values, nil,
		futuresLimit(5), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList(client, response, commonexchange.OperationRead,
		func(value *Order, raw json.RawMessage) { value.Raw = raw })
}

// OpenOrderValue는 계약 하나의 미체결 주문 수량과 필요 비용을 조회한다.
func (client *Client) OpenOrderValue(
	ctx context.Context,
	request OpenOrderValueRequest,
	options ...trade.RequestOption,
) (OpenOrderValue, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return OpenOrderValue{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/openOrderStatistics", url.Values{"symbol": {request.Symbol}}, nil,
		futuresLimit(10), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return OpenOrderValue{}, err
	}
	return decodeObject(client, response, commonexchange.OperationRead,
		func(value *OpenOrderValue, raw json.RawMessage) { value.Raw = raw })
}

// RecentFills는 최근 24시간 동안의 Futures 체결을 최대 1000건까지 조회한다.
func (client *Client) RecentFills(
	ctx context.Context,
	request RecentFillsRequest,
	options ...trade.RequestOption,
) ([]Fill, error) {
	values, err := optionalSymbolValues(request.Symbol)
	if err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/recentFills", values, nil,
		futuresLimit(3), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList(client, response, commonexchange.OperationRead,
		func(value *Fill, raw json.RawMessage) { value.Raw = raw })
}

func (client *Client) decodeOrderPage(response commonexchange.Response) (OrderPage, error) {
	var rawPage struct {
		CurrentPage int               `json:"currentPage"`
		PageSize    int               `json:"pageSize"`
		TotalNumber int               `json:"totalNum"`
		TotalPages  int               `json:"totalPage"`
		Items       []json.RawMessage `json:"items"`
	}
	data, err := client.decodeData(response, commonexchange.OperationRead, &rawPage)
	if err != nil {
		return OrderPage{}, err
	}
	orders, err := decodeRawItems(client, response, commonexchange.OperationRead, rawPage.Items,
		func(value *Order, raw json.RawMessage) { value.Raw = raw })
	if err != nil {
		return OrderPage{}, err
	}
	return OrderPage{
		CurrentPage: rawPage.CurrentPage, PageSize: rawPage.PageSize,
		TotalNumber: rawPage.TotalNumber, TotalPages: rawPage.TotalPages,
		Orders: orders, Raw: cloneBytes(data),
	}, nil
}

func optionalSymbolValues(symbol string) (url.Values, error) {
	values := make(url.Values)
	if symbol == "" {
		return values, nil
	}
	if err := validateSymbol(symbol); err != nil {
		return nil, err
	}
	values.Set("symbol", symbol)
	return values, nil
}

func validateExtendedPage(currentPage, pageSize int) error {
	if currentPage < 0 {
		return validationError("current page cannot be negative")
	}
	if pageSize < 0 || pageSize > maximumExtendedPageSize {
		return validationError(
			"page size must be between 1 and %d or zero for default", maximumExtendedPageSize,
		)
	}
	return nil
}

func (request TakeProfitStopLossOrderRequest) validate() error {
	if err := request.PlaceOrderRequest.validate(); err != nil {
		return err
	}
	switch request.StopPriceType {
	case "", StopPriceTypeTrade, StopPriceTypeMark, StopPriceTypeIndex:
	default:
		return validationError("unsupported stop price type %q", request.StopPriceType)
	}
	if request.TriggerStopUpPrice == "" && request.TriggerStopDownPrice == "" {
		return validationError("take profit or stop loss trigger price is required")
	}
	if request.TriggerStopUpPrice != "" {
		if err := validatePositiveDecimal("take profit trigger price", request.TriggerStopUpPrice); err != nil {
			return err
		}
	}
	if request.TriggerStopDownPrice != "" {
		if err := validatePositiveDecimal("stop loss trigger price", request.TriggerStopDownPrice); err != nil {
			return err
		}
	}
	return nil
}

func (request BatchCancelOrdersRequest) validate() error {
	if (len(request.OrderIDs) == 0) == (len(request.ClientOrders) == 0) {
		return validationError("exactly one of order ID list or client order list is required")
	}
	if len(request.OrderIDs) > maximumBatchCancelOrders || len(request.ClientOrders) > maximumBatchCancelOrders {
		return validationError("batch cancel cannot exceed %d orders", maximumBatchCancelOrders)
	}
	for _, orderID := range request.OrderIDs {
		if err := validateOrderID(orderID); err != nil {
			return err
		}
	}
	for _, order := range request.ClientOrders {
		if err := validateSymbol(order.Symbol); err != nil {
			return err
		}
		if !clientOrderIDPattern.MatchString(order.ClientOrderID) {
			return validationError("invalid client order ID")
		}
	}
	return nil
}

func (request ClosedOrdersRequest) validate() error {
	if request.Symbol != "" {
		if err := validateSymbol(request.Symbol); err != nil {
			return err
		}
	}
	if request.Side != "" && request.Side != SideBuy && request.Side != SideSell {
		return validationError("order side must be buy, sell, or empty")
	}
	switch request.Type {
	case "", "limit", "market", "limit_stop", "market_stop", "oco_limit", "oco_stop":
	default:
		return validationError("unsupported order type %q", request.Type)
	}
	if err := validateTimeRange("closed orders", request.StartAt, request.EndAt); err != nil {
		return err
	}
	return validateExtendedPage(request.CurrentPage, request.PageSize)
}

func (request ClosedOrdersRequest) values() url.Values {
	values := url.Values{"status": {"done"}}
	setIfNotEmpty(values, "symbol", request.Symbol)
	setIfNotEmpty(values, "side", string(request.Side))
	setIfNotEmpty(values, "type", request.Type)
	setMillis(values, "startAt", request.StartAt)
	setMillis(values, "endAt", request.EndAt)
	setPositiveInt(values, "currentPage", request.CurrentPage)
	setPositiveInt(values, "pageSize", request.PageSize)
	return values
}

func (request StopOrdersRequest) validate() error {
	if request.Symbol != "" {
		if err := validateSymbol(request.Symbol); err != nil {
			return err
		}
	}
	if request.Side != "" && request.Side != SideBuy && request.Side != SideSell {
		return validationError("order side must be buy, sell, or empty")
	}
	if request.Type != "" && request.Type != OrderTypeLimit && request.Type != OrderTypeMarket {
		return validationError("order type must be limit, market, or empty")
	}
	if err := validateTimeRange("stop orders", request.StartAt, request.EndAt); err != nil {
		return err
	}
	return validateExtendedPage(request.CurrentPage, request.PageSize)
}

func (request StopOrdersRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "symbol", request.Symbol)
	setIfNotEmpty(values, "side", string(request.Side))
	setIfNotEmpty(values, "type", string(request.Type))
	setMillis(values, "startAt", request.StartAt)
	setMillis(values, "endAt", request.EndAt)
	setPositiveInt(values, "currentPage", request.CurrentPage)
	setPositiveInt(values, "pageSize", request.PageSize)
	return values
}
