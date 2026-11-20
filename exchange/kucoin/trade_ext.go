package kucoin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const maxBatchOrders = 20

// ModifyOrderRequest는 HF 주문 정정 조건이다. 기존 주문을 취소하고 같은 거래쌍에 새 주문을 낸다.
// OrderID 또는 ClientOrderID 중 하나와 NewPrice, NewSize 중 하나 이상이 필요하다.
type ModifyOrderRequest struct {
	Symbol        string `json:"symbol"`
	OrderID       string `json:"orderId,omitempty"`
	ClientOrderID string `json:"clientOid,omitempty"`
	NewPrice      string `json:"newPrice,omitempty"`
	NewSize       string `json:"newSize,omitempty"`
}

// ModifyOrderResult는 정정으로 생성된 새 주문 ID와 원래 사용자 주문 ID다.
type ModifyOrderResult struct {
	NewOrderID    string          `json:"newOrderId"`
	ClientOrderID string          `json:"clientOid"`
	Raw           json.RawMessage `json:"-"`
}

func (request ModifyOrderRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateOrderIdentity(request.OrderID, request.ClientOrderID); err != nil {
		return err
	}
	if request.NewPrice == "" && request.NewSize == "" {
		return validationError("at least one of newPrice or newSize is required")
	}
	if request.NewPrice != "" {
		if err := validatePositiveDecimal("newPrice", request.NewPrice); err != nil {
			return err
		}
	}
	if request.NewSize != "" {
		if err := validatePositiveDecimal("newSize", request.NewSize); err != nil {
			return err
		}
	}
	return nil
}

// OrderSyncResult는 동기식 주문 생성·취소가 반환하는 주문 상태다.
// 주문 생성 응답에는 OrderTime과 MatchTime이, 취소 응답에는 두 값이 없다.
type OrderSyncResult struct {
	OrderID       string          `json:"orderId"`
	ClientOrderID string          `json:"clientOid"`
	OrderTime     int64           `json:"orderTime"`
	OriginSize    string          `json:"originSize"`
	DealSize      string          `json:"dealSize"`
	RemainSize    string          `json:"remainSize"`
	CanceledSize  string          `json:"canceledSize"`
	Status        string          `json:"status"`
	MatchTime     int64           `json:"matchTime"`
	Raw           json.RawMessage `json:"-"`
}

// BatchOrdersRequest는 같은 거래쌍 지정가 주문 일괄 생성 조건이다. 최대 20개다.
type BatchOrdersRequest struct {
	Orders []PlaceOrderRequest
}

func (request BatchOrdersRequest) validate() error {
	if len(request.Orders) == 0 || len(request.Orders) > maxBatchOrders {
		return validationError("batch order count must be between 1 and %d", maxBatchOrders)
	}
	seen := make(map[string]struct{}, len(request.Orders))
	for _, order := range request.Orders {
		if err := order.validate(); err != nil {
			return err
		}
		if order.Type != OrderTypeLimit {
			return validationError("batch orders must be limit orders")
		}
		if _, exists := seen[order.ClientOrderID]; exists {
			return validationError("batch orders contain a duplicate client order ID")
		}
		seen[order.ClientOrderID] = struct{}{}
	}
	return nil
}

// BatchOrderResult는 일괄 주문 항목별 접수 결과다. Success가 false인 항목은 FailMessage에 사유가 있다.
// 동기식 일괄 주문에서만 OrderTime 이하 상태 필드가 채워진다.
type BatchOrderResult struct {
	OrderID       string          `json:"orderId"`
	ClientOrderID string          `json:"clientOid"`
	OrderTime     int64           `json:"orderTime"`
	OriginSize    string          `json:"originSize"`
	DealSize      string          `json:"dealSize"`
	RemainSize    string          `json:"remainSize"`
	CanceledSize  string          `json:"canceledSize"`
	Status        string          `json:"status"`
	MatchTime     int64           `json:"matchTime"`
	Success       bool            `json:"success"`
	FailMessage   string          `json:"failMsg"`
	Raw           json.RawMessage `json:"-"`
}

// CancelPartialOrderRequest는 주문 일부 수량 취소 조건이다.
type CancelPartialOrderRequest struct {
	OrderID    string
	Symbol     string
	CancelSize string
}

// PartialCancelResult는 부분 취소된 주문 ID와 취소 수량이다.
type PartialCancelResult struct {
	OrderID    string          `json:"orderId"`
	CancelSize string          `json:"cancelSize"`
	Raw        json.RawMessage `json:"-"`
}

func (request CancelPartialOrderRequest) validate() error {
	if err := validateOrderID(request.OrderID); err != nil {
		return err
	}
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	return validatePositiveDecimal("cancelSize", request.CancelSize)
}

// CancelAllResult는 전 심볼 취소의 성공·실패 거래쌍이다.
type CancelAllResult struct {
	SucceededSymbols []string        `json:"succeedSymbols"`
	FailedSymbols    []FailedSymbol  `json:"failedSymbols"`
	Raw              json.RawMessage `json:"-"`
}

// FailedSymbol은 전 심볼 취소에서 실패한 거래쌍과 사유다.
type FailedSymbol struct {
	Symbol string `json:"symbol"`
	Error  string `json:"error"`
}

// DeadCancelRequest는 DCP(데드맨 취소) 설정이다.
// Timeout은 -1(해제) 또는 5~86400초이며 Symbols가 비면 전체 거래쌍이다. 최대 50개다.
type DeadCancelRequest struct {
	Timeout int      `json:"timeout"`
	Symbols []string `json:"-"`
}

func (request DeadCancelRequest) validate() error {
	if request.Timeout != -1 && (request.Timeout < 5 || request.Timeout > 86400) {
		return validationError("dead cancel timeout must be -1 or between 5 and 86400 seconds")
	}
	if len(request.Symbols) > 50 {
		return validationError("dead cancel symbols cannot exceed 50")
	}
	seen := make(map[string]struct{}, len(request.Symbols))
	for _, symbol := range request.Symbols {
		if err := validateSymbol(symbol); err != nil {
			return err
		}
		if _, exists := seen[symbol]; exists {
			return validationError("dead cancel symbols contain a duplicate")
		}
		seen[symbol] = struct{}{}
	}
	return nil
}

func (request DeadCancelRequest) body() map[string]any {
	body := map[string]any{"timeout": request.Timeout}
	if len(request.Symbols) > 0 {
		body["symbols"] = joinList(request.Symbols)
	}
	return body
}

// DeadCancelSetting은 DCP 설정과 트리거 시각(초)이다. 설정이 없으면 Configured가 false다.
type DeadCancelSetting struct {
	Configured  bool
	Timeout     int             `json:"timeout"`
	Symbols     string          `json:"symbols"`
	CurrentTime int64           `json:"currentTime"`
	TriggerTime int64           `json:"triggerTime"`
	Raw         json.RawMessage `json:"-"`
}

// ClosedOrdersRequest는 체결 완료·취소 주문 이력 조회 조건이다.
// Limit은 1~100(기본 20)이며 LastID로 다음 구간을 조회한다.
type ClosedOrdersRequest struct {
	Symbol string
	Side   Side
	Type   OrderType
	LastID int64
	Limit  int
	Start  *time.Time
	End    *time.Time
}

func (request ClosedOrdersRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	return validateHistoryFilter(request.Side, request.Type, request.LastID, request.Limit, request.Start, request.End)
}

func (request ClosedOrdersRequest) values() url.Values {
	return historyValues(request.Symbol, "", request.Side, request.Type, request.LastID, request.Limit, request.Start, request.End)
}

// FillsRequest는 내 체결 내역 조회 조건이다. 기본 최근 7일이다.
type FillsRequest struct {
	Symbol  string
	OrderID string
	Side    Side
	Type    OrderType
	LastID  int64
	Limit   int
	Start   *time.Time
	End     *time.Time
}

func (request FillsRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.OrderID != "" {
		if err := validateOrderID(request.OrderID); err != nil {
			return err
		}
	}
	return validateHistoryFilter(request.Side, request.Type, request.LastID, request.Limit, request.Start, request.End)
}

func (request FillsRequest) values() url.Values {
	return historyValues(request.Symbol, request.OrderID, request.Side, request.Type, request.LastID, request.Limit, request.Start, request.End)
}

func validateHistoryFilter(
	side Side, orderType OrderType, lastID int64, limit int, start, end *time.Time,
) error {
	if err := validateOptionalSide(side); err != nil {
		return err
	}
	if err := validateOptionalOrderType(orderType); err != nil {
		return err
	}
	if lastID < 0 {
		return validationError("lastId cannot be negative")
	}
	if err := validatePageSize("limit", limit, 1, 100); err != nil {
		return err
	}
	return validateTimeRange(start, end)
}

func historyValues(
	symbol, orderID string, side Side, orderType OrderType, lastID int64, limit int, start, end *time.Time,
) url.Values {
	values := url.Values{"symbol": {symbol}}
	setIfNotEmpty(values, "orderId", orderID)
	setIfNotEmpty(values, "side", string(side))
	setIfNotEmpty(values, "type", string(orderType))
	setPositiveInt64(values, "lastId", lastID)
	setPositiveInt(values, "limit", limit)
	setMillis(values, "startAt", start)
	setMillis(values, "endAt", end)
	return values
}

// ClosedOrder는 체결 완료 또는 취소된 HF 주문 이력 한 건이다.
type ClosedOrder struct {
	ID            string          `json:"id"`
	Symbol        string          `json:"symbol"`
	OperationType string          `json:"opType"`
	Type          OrderType       `json:"type"`
	Side          Side            `json:"side"`
	Price         string          `json:"price"`
	Size          string          `json:"size"`
	Funds         string          `json:"funds"`
	DealSize      string          `json:"dealSize"`
	DealFunds     string          `json:"dealFunds"`
	Fee           string          `json:"fee"`
	FeeCurrency   string          `json:"feeCurrency"`
	SelfTradeMode string          `json:"stp"`
	TimeInForce   TimeInForce     `json:"timeInForce"`
	PostOnly      bool            `json:"postOnly"`
	Hidden        bool            `json:"hidden"`
	Iceberg       bool            `json:"iceberg"`
	VisibleSize   string          `json:"visibleSize"`
	CancelAfter   int64           `json:"cancelAfter"`
	Channel       string          `json:"channel"`
	ClientOrderID string          `json:"clientOid"`
	Remark        string          `json:"remark"`
	Tags          string          `json:"tags"`
	CancelExists  bool            `json:"cancelExist"`
	CreatedAt     int64           `json:"createdAt"`
	LastUpdatedAt int64           `json:"lastUpdatedAt"`
	TradeType     string          `json:"tradeType"`
	InOrderBook   bool            `json:"inOrderBook"`
	CancelledSize string          `json:"cancelledSize"`
	CancelledFund string          `json:"cancelledFunds"`
	RemainSize    string          `json:"remainSize"`
	RemainFunds   string          `json:"remainFunds"`
	Tax           string          `json:"tax"`
	Active        bool            `json:"active"`
	CancelReason  int             `json:"cancelReason"`
	Raw           json.RawMessage `json:"-"`
}

// Fill은 내 체결 내역 한 건이다.
type Fill struct {
	ID             int64           `json:"id"`
	Symbol         string          `json:"symbol"`
	TradeID        int64           `json:"tradeId"`
	OrderID        string          `json:"orderId"`
	CounterOrderID string          `json:"counterOrderId"`
	Side           Side            `json:"side"`
	Liquidity      string          `json:"liquidity"`
	ForceTaker     bool            `json:"forceTaker"`
	Price          string          `json:"price"`
	Size           string          `json:"size"`
	Funds          string          `json:"funds"`
	Fee            string          `json:"fee"`
	FeeRate        string          `json:"feeRate"`
	FeeCurrency    string          `json:"feeCurrency"`
	Stop           string          `json:"stop"`
	TradeType      string          `json:"tradeType"`
	TaxRate        string          `json:"taxRate"`
	Tax            string          `json:"tax"`
	Type           OrderType       `json:"type"`
	CreatedAt      int64           `json:"createdAt"`
	Raw            json.RawMessage `json:"-"`
}

// CursorPage는 lastId 기반 목록이다. 다음 조회에 LastID를 그대로 넘긴다.
type CursorPage[T any] struct {
	LastID int64
	Items  []T
	Raw    json.RawMessage
}

func decodeCursorPage[T any](
	client *Client,
	response commonexchange.Response,
	setRaw func(*T, json.RawMessage),
) (CursorPage[T], error) {
	var wire struct {
		LastID int64             `json:"lastId"`
		Items  []json.RawMessage `json:"items"`
	}
	data, err := client.decodeData(response, commonexchange.OperationRead, &wire)
	if err != nil {
		return CursorPage[T]{}, err
	}
	page := CursorPage[T]{LastID: wire.LastID, Items: make([]T, len(wire.Items)), Raw: cloneBytes(data)}
	for index, raw := range wire.Items {
		if err := json.Unmarshal(raw, &page.Items[index]); err != nil {
			return CursorPage[T]{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
		}
		setRaw(&page.Items[index], cloneBytes(raw))
	}
	return page, nil
}

// ModifyOrder는 HF 주문 가격·수량을 정정한다. 기존 주문 ID는 취소되고 새 주문 ID가 반환된다.
func (client *Client) ModifyOrder(
	ctx context.Context,
	request ModifyOrderRequest,
	options ...trade.RequestOption,
) (ModifyOrderResult, error) {
	if err := request.validate(); err != nil {
		return ModifyOrderResult{}, err
	}
	var result ModifyOrderResult
	response, data, err := client.mutate(
		ctx, http.MethodPost, "/api/v1/hf/orders/alter", nil, request,
		spotLimit(1), credential.PermissionTrade, &result, options...,
	)
	if err != nil {
		return ModifyOrderResult{}, err
	}
	if result.NewOrderID == "" {
		return ModifyOrderResult{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("KuCoin modify order acknowledgement is empty"),
		)
	}
	result.Raw = cloneBytes(data)
	return result, nil
}

// PlaceOrderSync는 주문 생성 후 접수 상태를 동기로 반환받는다.
func (client *Client) PlaceOrderSync(
	ctx context.Context,
	request PlaceOrderRequest,
	options ...trade.RequestOption,
) (OrderSyncResult, error) {
	if err := request.validate(); err != nil {
		return OrderSyncResult{}, err
	}
	return client.syncOrder(ctx, http.MethodPost, "/api/v1/hf/orders/sync", nil, request, 1, options...)
}

// TestOrder는 주문을 실제로 생성하지 않고 요청 형식과 조건만 검증한다.
func (client *Client) TestOrder(
	ctx context.Context,
	request PlaceOrderRequest,
	options ...trade.RequestOption,
) (OrderReference, error) {
	if err := request.validate(); err != nil {
		return OrderReference{}, err
	}
	var reference OrderReference
	_, data, err := client.mutate(
		ctx, http.MethodPost, "/api/v1/hf/orders/test", nil, request,
		spotLimit(1), credential.PermissionTrade, &reference, options...,
	)
	if err != nil {
		return OrderReference{}, err
	}
	reference.Raw = cloneBytes(data)
	return reference, nil
}

// BatchPlaceOrders는 같은 거래쌍 지정가 주문 최대 20개를 일괄 생성한다.
// 항목별 성공 여부는 BatchOrderResult.Success로 확인한다.
func (client *Client) BatchPlaceOrders(
	ctx context.Context,
	request BatchOrdersRequest,
	options ...trade.RequestOption,
) ([]BatchOrderResult, error) {
	return client.batchOrders(ctx, "/api/v1/hf/orders/multi", request, options...)
}

// BatchPlaceOrdersSync는 일괄 주문 생성 후 항목별 접수 상태를 동기로 반환받는다. 최대 20개다.
func (client *Client) BatchPlaceOrdersSync(
	ctx context.Context,
	request BatchOrdersRequest,
	options ...trade.RequestOption,
) ([]BatchOrderResult, error) {
	return client.batchOrders(ctx, "/api/v1/hf/orders/multi/sync", request, options...)
}

func (client *Client) batchOrders(
	ctx context.Context,
	path string,
	request BatchOrdersRequest,
	options ...trade.RequestOption,
) ([]BatchOrderResult, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	body := struct {
		OrderList []PlaceOrderRequest `json:"orderList"`
	}{OrderList: request.Orders}
	response, err := client.executeMutation(
		ctx, http.MethodPost, path, nil, body, spotLimit(1), credential.PermissionTrade, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationMutation, func(item *BatchOrderResult, raw json.RawMessage) {
		item.Raw = raw
	})
}

// CancelOrderSync는 주문 취소 후 최종 상태를 동기로 반환받는다.
func (client *Client) CancelOrderSync(
	ctx context.Context,
	request CancelOrderRequest,
	options ...trade.RequestOption,
) (OrderSyncResult, error) {
	if err := request.validate(); err != nil {
		return OrderSyncResult{}, err
	}
	path := "/api/v1/hf/orders/sync/client-order/" + url.PathEscape(request.ClientOrderID)
	if request.OrderID != "" {
		path = "/api/v1/hf/orders/sync/" + url.PathEscape(request.OrderID)
	}
	return client.syncOrder(ctx, http.MethodDelete, path, request.values(), nil, 1, options...)
}

func (client *Client) syncOrder(
	ctx context.Context,
	method, path string,
	query url.Values,
	body any,
	weight int,
	options ...trade.RequestOption,
) (OrderSyncResult, error) {
	var result OrderSyncResult
	response, data, err := client.mutate(
		ctx, method, path, query, body, spotLimit(weight), credential.PermissionTrade, &result, options...,
	)
	if err != nil {
		return OrderSyncResult{}, err
	}
	if result.OrderID == "" && result.ClientOrderID == "" {
		return OrderSyncResult{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("KuCoin sync order acknowledgement is empty"),
		)
	}
	result.Raw = cloneBytes(data)
	return result, nil
}

// CancelPartialOrder는 주문의 지정 수량만 취소한다. 대기열 순서는 유지된다.
func (client *Client) CancelPartialOrder(
	ctx context.Context,
	request CancelPartialOrderRequest,
	options ...trade.RequestOption,
) (PartialCancelResult, error) {
	if err := request.validate(); err != nil {
		return PartialCancelResult{}, err
	}
	var result PartialCancelResult
	query := url.Values{"symbol": {request.Symbol}, "cancelSize": {request.CancelSize}}
	_, data, err := client.mutate(
		ctx, http.MethodDelete, "/api/v1/hf/orders/cancel/"+url.PathEscape(request.OrderID), query, nil,
		spotLimit(2), credential.PermissionTrade, &result, options...,
	)
	if err != nil {
		return PartialCancelResult{}, err
	}
	result.Raw = cloneBytes(data)
	return result, nil
}

// CancelAllOrdersBySymbol은 지정한 거래쌍의 모든 HF 미체결 주문 취소를 접수한다.
func (client *Client) CancelAllOrdersBySymbol(
	ctx context.Context,
	symbol string,
	options ...trade.RequestOption,
) (Acknowledgement, error) {
	if err := validateSymbol(symbol); err != nil {
		return Acknowledgement{}, err
	}
	return client.acknowledge(
		ctx, http.MethodDelete, "/api/v1/hf/orders", url.Values{"symbol": {symbol}}, nil,
		spotLimit(2), credential.PermissionTrade, options...,
	)
}

// CancelAllOrders는 모든 거래쌍의 HF 미체결 주문을 취소하고 거래쌍별 성공·실패를 반환한다.
func (client *Client) CancelAllOrders(
	ctx context.Context,
	options ...trade.RequestOption,
) (CancelAllResult, error) {
	var result CancelAllResult
	_, data, err := client.mutate(
		ctx, http.MethodDelete, "/api/v1/hf/orders/cancelAll", nil, nil,
		spotLimit(30), credential.PermissionTrade, &result, options...,
	)
	if err != nil {
		return CancelAllResult{}, err
	}
	result.Raw = cloneBytes(data)
	return result, nil
}

// DeadCancelAll은 DCP(데드맨 취소)를 설정한다. Timeout -1은 설정을 해제한다.
func (client *Client) DeadCancelAll(
	ctx context.Context,
	request DeadCancelRequest,
	options ...trade.RequestOption,
) (DeadCancelSetting, error) {
	if err := request.validate(); err != nil {
		return DeadCancelSetting{}, err
	}
	var result DeadCancelSetting
	_, data, err := client.mutate(
		ctx, http.MethodPost, "/api/v1/hf/orders/dead-cancel-all", nil, request.body(),
		spotLimit(2), credential.PermissionTrade, &result, options...,
	)
	if err != nil {
		return DeadCancelSetting{}, err
	}
	result.Configured = true
	result.Raw = cloneBytes(data)
	return result, nil
}

// DeadCancelSettingQuery는 현재 DCP 설정을 조회한다. 설정이 없으면 Configured가 false다.
func (client *Client) DeadCancelSettingQuery(
	ctx context.Context,
	options ...trade.RequestOption,
) (DeadCancelSetting, error) {
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/hf/orders/dead-cancel-all/query", nil, nil,
		spotLimit(2), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return DeadCancelSetting{}, err
	}
	data, err := client.responseData(response, commonexchange.OperationRead)
	if err != nil {
		return DeadCancelSetting{}, err
	}
	if isNullData(data) {
		return DeadCancelSetting{}, nil
	}
	var result DeadCancelSetting
	if err := json.Unmarshal(data, &result); err != nil {
		return DeadCancelSetting{}, client.decodeBodyError(response, commonexchange.OperationRead, err)
	}
	result.Configured = true
	result.Raw = cloneBytes(data)
	return result, nil
}

// ClosedOrders는 체결 완료·취소된 HF 주문 이력을 조회한다.
func (client *Client) ClosedOrders(
	ctx context.Context,
	request ClosedOrdersRequest,
	options ...trade.RequestOption,
) (CursorPage[ClosedOrder], error) {
	if err := request.validate(); err != nil {
		return CursorPage[ClosedOrder]{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/hf/orders/done", request.values(), nil,
		spotLimit(2), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return CursorPage[ClosedOrder]{}, err
	}
	return decodeCursorPage(client, response, func(item *ClosedOrder, raw json.RawMessage) { item.Raw = raw })
}

// Fills는 내 HF 체결 내역을 조회한다.
func (client *Client) Fills(
	ctx context.Context,
	request FillsRequest,
	options ...trade.RequestOption,
) (CursorPage[Fill], error) {
	if err := request.validate(); err != nil {
		return CursorPage[Fill]{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/api/v1/hf/fills", request.values(), nil,
		spotLimit(2), credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return CursorPage[Fill]{}, err
	}
	return decodeCursorPage(client, response, func(item *Fill, raw json.RawMessage) { item.Raw = raw })
}

func isNullData(data json.RawMessage) bool {
	trimmed := string(data)
	return trimmed == "" || trimmed == "null" || trimmed == "{}"
}
