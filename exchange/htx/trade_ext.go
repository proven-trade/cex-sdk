package htx

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const (
	maximumBatchOrders            = 10
	maximumBatchCancelSymbols     = 10
	maximumBatchCancelOrders      = 100
	maximumBatchCancelByIDs       = 50
	maximumAlgoCancelOrders       = 50
	minimumCancelAllAfterSeconds  = 5
	maximumAlgoPageLimit          = 500
	minimumRecentOrderHistorySize = 10
	maximumRecentOrderHistorySize = 1000
)

// SortOrder는 목록 조회의 정렬 방향이다.
type SortOrder string

const (
	SortAscending  SortOrder = "asc"
	SortDescending SortOrder = "desc"
)

// PlaceBatchOrdersRequest는 최대 10건의 Spot 주문을 한 번에 접수하는 조건이다.
// 각 주문은 PlaceOrder와 같은 검증을 통과해야 하며 사용자 주문 ID는 배치 안에서 유일해야 한다.
type PlaceBatchOrdersRequest struct {
	Orders []PlaceOrderRequest
}

// BatchOrderResult는 일괄 주문에 포함된 주문 한 건의 접수 결과다.
// 거절된 주문은 OrderID 없이 ErrorCode와 ErrorMessage를 가진다.
type BatchOrderResult struct {
	OrderID       Scalar          `json:"order-id"`
	ClientOrderID string          `json:"client-order-id"`
	ErrorCode     string          `json:"err-code"`
	ErrorMessage  string          `json:"err-msg"`
	Raw           json.RawMessage `json:"-"`
}

// Accepted는 거래소가 해당 주문을 접수했는지 반환한다.
func (result BatchOrderResult) Accepted() bool {
	return result.ErrorCode == "" && result.OrderID != ""
}

// CancelOpenOrdersRequest는 조건에 맞는 미체결 주문을 일괄 취소하는 범위다.
// 거래쌍을 비우면 전체 거래쌍이 대상이 되므로 실수로 범위를 넓히지 않도록
// Symbols와 AllSymbols 중 정확히 하나를 지정해야 한다.
type CancelOpenOrdersRequest struct {
	AccountID  string
	Symbols    []string
	AllSymbols bool
	Types      []OrderType
	Side       Side
	Size       int
}

// CancelOpenOrdersResult는 조건부 일괄 취소의 접수·실패 건수와 다음 취소 대상 주문 ID다.
// NextID가 -1이면 남은 미체결 주문이 없다.
type CancelOpenOrdersResult struct {
	SuccessCount int             `json:"success-count"`
	FailedCount  int             `json:"failed-count"`
	NextID       Scalar          `json:"next-id"`
	Raw          json.RawMessage `json:"-"`
}

// CancelOrdersByIDsRequest는 주문 ID 목록 또는 사용자 주문 ID 목록 중 하나로 일괄 취소를 지정한다.
// 공식 문서는 더 빠르고 안정적인 주문 ID 사용을 권장한다.
type CancelOrdersByIDsRequest struct {
	OrderIDs       []string
	ClientOrderIDs []string
}

// BatchCancelResult는 주문 ID 목록 일괄 취소의 접수·실패 목록이다.
// Succeeded는 요청 방식에 따라 주문 ID 또는 사용자 주문 ID이며 최종 상태는 주문 조회로 확인해야 한다.
type BatchCancelResult struct {
	Succeeded []Scalar
	Failed    []BatchCancelFailure
	Raw       json.RawMessage
}

// BatchCancelFailure는 일괄 취소가 거절된 주문 한 건이다.
type BatchCancelFailure struct {
	OrderID       Scalar          `json:"order-id"`
	ClientOrderID string          `json:"client-order-id"`
	ErrorCode     string          `json:"err-code"`
	ErrorMessage  string          `json:"err-msg"`
	OrderState    Scalar          `json:"order-state"`
	Raw           json.RawMessage `json:"-"`
}

// CancelAllAfterRequest는 데드맨 스위치의 제한 시간이다.
// 0은 스위치를 끄고, 켤 때는 5초 이상이어야 한다.
type CancelAllAfterRequest struct {
	TimeoutSeconds int
}

// CancelAllAfterResult는 데드맨 스위치 설정 직후의 서버 시각과 발동 예정 시각이다.
// TriggerTime이 0이면 스위치가 꺼진 것이다.
type CancelAllAfterResult struct {
	CurrentTime int64           `json:"currentTime"`
	TriggerTime int64           `json:"triggerTime"`
	Raw         json.RawMessage `json:"-"`
}

// RecentOrderHistoryRequest는 최근 48시간 주문 이력의 필터와 시간 범위다.
type RecentOrderHistoryRequest struct {
	Symbol    string
	Start     *time.Time
	End       *time.Time
	Direction QueryDirection
	Size      int
}

// RecentOrderHistory는 최근 48시간 주문 이력 한 쪽과 다음 조회 시각이다.
// NextTime은 응답이 size를 넘을 때만 있으며 direct=prev면 다음 start-time,
// direct=next면 다음 end-time으로 쓴다.
type RecentOrderHistory struct {
	Orders   []Order
	NextTime Scalar
	Raw      json.RawMessage
}

// TransactFeeRateRequest는 수수료율을 조회할 거래쌍 목록이다.
type TransactFeeRateRequest struct {
	Symbols []string
}

// TransactFeeRate는 거래쌍별 기본 수수료율과 차감 적용 후 실제 수수료율이다.
// maker 값이 음수이면 수수료가 아니라 리베이트율이다.
type TransactFeeRate struct {
	Symbol          string          `json:"symbol"`
	MakerFeeRate    Decimal         `json:"makerFeeRate"`
	TakerFeeRate    Decimal         `json:"takerFeeRate"`
	ActualMakerRate Decimal         `json:"actualMakerRate"`
	ActualTakerRate Decimal         `json:"actualTakerRate"`
	Raw             json.RawMessage `json:"-"`
}

// AlgoOrderType은 조건부 주문이 발동한 뒤 만들 주문의 가격 정책이다.
type AlgoOrderType string

const (
	AlgoOrderTypeLimit  AlgoOrderType = "limit"
	AlgoOrderTypeMarket AlgoOrderType = "market"
)

// AlgoTimeInForce는 조건부 주문이 발동한 뒤의 주문 유효 조건이다.
type AlgoTimeInForce string

const (
	AlgoTimeInForceGTC AlgoTimeInForce = "gtc"
	AlgoTimeInForceBOC AlgoTimeInForce = "boc"
	AlgoTimeInForceIOC AlgoTimeInForce = "ioc"
	AlgoTimeInForceFOK AlgoTimeInForce = "fok"
)

// AlgoOrderStatus는 조건부 주문의 수명주기 상태다.
type AlgoOrderStatus string

const (
	AlgoOrderStatusCreated   AlgoOrderStatus = "created"
	AlgoOrderStatusTriggered AlgoOrderStatus = "triggered"
	AlgoOrderStatusCanceled  AlgoOrderStatus = "canceled"
	AlgoOrderStatusRejected  AlgoOrderStatus = "rejected"
)

// PlaceAlgoOrderRequest는 사용자 주문 ID를 강제하는 조건부(스탑·트레일링) 주문 조건이다.
// limit 주문은 Price·Size를 쓰고, market 매수는 Value, market 매도는 Size를 쓴다.
// TrailingRate는 market 주문에서만 쓸 수 있으며 0.001~0.05 범위다.
type PlaceAlgoOrderRequest struct {
	AccountID     string
	Symbol        string
	Side          Side
	Type          AlgoOrderType
	ClientOrderID string
	StopPrice     string
	Price         string
	Size          string
	Value         string
	TimeInForce   AlgoTimeInForce
	TrailingRate  string
}

// AlgoOrderReference는 조건부 주문 접수 결과다.
type AlgoOrderReference struct {
	ClientOrderID string
	Raw           json.RawMessage
}

// CancelAlgoOrdersRequest는 발동 전 조건부 주문을 사용자 주문 ID로 취소하는 조건이다.
type CancelAlgoOrdersRequest struct {
	ClientOrderIDs []string
}

// CancelAlgoOrdersResult는 조건부 주문 취소의 접수·거절 사용자 주문 ID 목록이다.
type CancelAlgoOrdersResult struct {
	Accepted []string
	Rejected []string
	Raw      json.RawMessage
}

// OpenAlgoOrdersRequest는 발동 전 조건부 주문의 필터와 커서를 지정한다.
type OpenAlgoOrdersRequest struct {
	AccountID string
	Symbol    string
	Side      Side
	Type      AlgoOrderType
	Sort      SortOrder
	Limit     int
	FromID    string
}

// AlgoOrderHistoryRequest는 취소·거절·발동된 조건부 주문 이력의 필터와 커서를 지정한다.
// Symbol과 Status는 필수다.
type AlgoOrderHistoryRequest struct {
	AccountID string
	Symbol    string
	Side      Side
	Type      AlgoOrderType
	Status    AlgoOrderStatus
	Start     *time.Time
	End       *time.Time
	Sort      SortOrder
	Limit     int
	FromID    string
}

// AlgoOrder는 조건부 주문 한 건이다. 가격·수량 필드는 주문 유형에 따라 비어 있을 수 있다.
type AlgoOrder struct {
	AccountID       Scalar          `json:"accountId"`
	Source          string          `json:"source"`
	ClientOrderID   string          `json:"clientOrderId"`
	OrderID         Scalar          `json:"orderId"`
	Symbol          string          `json:"symbol"`
	OrderPrice      Scalar          `json:"orderPrice"`
	OrderSize       Scalar          `json:"orderSize"`
	OrderValue      Scalar          `json:"orderValue"`
	OrderSide       Side            `json:"orderSide"`
	TimeInForce     AlgoTimeInForce `json:"timeInForce"`
	OrderType       AlgoOrderType   `json:"orderType"`
	StopPrice       Scalar          `json:"stopPrice"`
	TrailingRate    Scalar          `json:"trailingRate"`
	OrderOrigTime   int64           `json:"orderOrigTime"`
	LastActTime     int64           `json:"lastActTime"`
	OrderCreateTime int64           `json:"orderCreateTime"`
	Status          AlgoOrderStatus `json:"orderStatus"`
	ErrorCode       Scalar          `json:"errCode"`
	ErrorMessage    string          `json:"errMessage"`
	Raw             json.RawMessage `json:"-"`
}

// AlgoOrderPage는 조건부 주문 목록 한 쪽과 다음 쪽 커서다.
type AlgoOrderPage struct {
	Orders []AlgoOrder
	NextID Scalar
	Raw    json.RawMessage
}

// PlaceBatchOrders는 최대 10건의 Spot 주문을 한 번에 접수한다.
// 일부 주문만 거절될 수 있으므로 각 결과의 Accepted를 확인해야 한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) PlaceBatchOrders(
	ctx context.Context,
	request PlaceBatchOrdersRequest,
	options ...trade.RequestOption,
) ([]BatchOrderResult, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	wire := make([]batchOrderWire, len(request.Orders))
	for index, order := range request.Orders {
		wire[index] = batchOrderWire{
			AccountID: order.AccountID, Amount: order.Amount, Price: order.Price,
			Source: "spot-api", Symbol: order.Symbol, Type: order.orderType(),
			ClientOrderID: order.ClientOrderID,
		}
		if order.SelfMatchPrevent {
			wire[index].SelfMatchPrevent = 1
		}
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return nil, validationError("encode HTX batch orders: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v1/order/batch-orders", nil, body,
		rateGroupBatchOrder, min(client.orderQuota, batchOrderRequestLimit),
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return nil, err
	}
	var rawItems []json.RawMessage
	if _, err := client.decodeDataForOperation(
		response, commonexchange.OperationMutation, &rawItems,
	); err != nil {
		return nil, err
	}
	results := make([]BatchOrderResult, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &results[index]); err != nil {
			return nil, client.decodeBodyErrorForOperation(
				response, commonexchange.OperationMutation, err,
			)
		}
		if results[index].ErrorCode == "" && !orderIDPattern.MatchString(string(results[index].OrderID)) {
			return nil, client.decodeBodyErrorForOperation(
				response, commonexchange.OperationMutation,
				errors.New("HTX batch order acknowledgement has an invalid order ID"),
			)
		}
		results[index].Raw = cloneBytes(raw)
	}
	return results, nil
}

// CancelOpenOrders는 조건에 맞는 미체결 주문을 최대 100건까지 일괄 취소 접수한다.
// 응답은 접수 건수일 뿐이므로 최종 상태는 주문 조회 또는 private stream으로 확인해야 한다.
func (client *Client) CancelOpenOrders(
	ctx context.Context,
	request CancelOpenOrdersRequest,
	options ...trade.RequestOption,
) (CancelOpenOrdersResult, error) {
	if err := request.validate(); err != nil {
		return CancelOpenOrdersResult{}, err
	}
	wire := struct {
		AccountID string `json:"account-id,omitempty"`
		Symbol    string `json:"symbol,omitempty"`
		Types     string `json:"types,omitempty"`
		Side      Side   `json:"side,omitempty"`
		Size      int    `json:"size,omitempty"`
	}{
		AccountID: request.AccountID, Symbol: strings.Join(request.Symbols, ","),
		Types: joinOrderTypes(request.Types), Side: request.Side, Size: request.Size,
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return CancelOpenOrdersResult{}, validationError("encode HTX batch cancel request: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v1/order/orders/batchCancelOpenOrders", nil, body,
		rateGroupBatchOrder, min(client.orderQuota, batchOrderRequestLimit),
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CancelOpenOrdersResult{}, err
	}
	var value CancelOpenOrdersResult
	raw, err := client.decodeDataForOperation(
		response, commonexchange.OperationMutation, &value,
	)
	if err != nil {
		return CancelOpenOrdersResult{}, err
	}
	value.Raw = raw
	return value, nil
}

// CancelOrdersByIDs는 주문 ID 또는 사용자 주문 ID 목록(최대 50건)을 일괄 취소 접수한다.
// 일부만 실패할 수 있으므로 Failed를 확인하고 최종 상태는 주문 조회로 확인해야 한다.
func (client *Client) CancelOrdersByIDs(
	ctx context.Context,
	request CancelOrdersByIDsRequest,
	options ...trade.RequestOption,
) (BatchCancelResult, error) {
	if err := request.validate(); err != nil {
		return BatchCancelResult{}, err
	}
	body, err := json.Marshal(struct {
		OrderIDs       []string `json:"order-ids,omitempty"`
		ClientOrderIDs []string `json:"client-order-ids,omitempty"`
	}{OrderIDs: request.OrderIDs, ClientOrderIDs: request.ClientOrderIDs})
	if err != nil {
		return BatchCancelResult{}, validationError("encode HTX batch cancel request: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v1/order/orders/batchcancel", nil, body,
		rateGroupBatchOrder, min(client.orderQuota, batchOrderRequestLimit),
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return BatchCancelResult{}, err
	}
	var wire struct {
		Succeeded []Scalar          `json:"success"`
		Failed    []json.RawMessage `json:"failed"`
	}
	raw, err := client.decodeDataForOperation(
		response, commonexchange.OperationMutation, &wire,
	)
	if err != nil {
		return BatchCancelResult{}, err
	}
	value := BatchCancelResult{
		Succeeded: wire.Succeeded, Failed: make([]BatchCancelFailure, len(wire.Failed)), Raw: raw,
	}
	for index, failedRaw := range wire.Failed {
		if err := json.Unmarshal(failedRaw, &value.Failed[index]); err != nil {
			return BatchCancelResult{}, client.decodeBodyErrorForOperation(
				response, commonexchange.OperationMutation, err,
			)
		}
		value.Failed[index].Raw = cloneBytes(failedRaw)
	}
	return value, nil
}

// CancelAllAfter는 데드맨 스위치를 켜거나 끈다.
// 켠 뒤 제한 시간 안에 다시 호출하지 않으면 거래소가 Spot 미체결 주문을 최대 500건까지 취소한다.
// TimeoutSeconds가 0이면 스위치를 끈다.
func (client *Client) CancelAllAfter(
	ctx context.Context,
	request CancelAllAfterRequest,
	options ...trade.RequestOption,
) (CancelAllAfterResult, error) {
	if err := request.validate(); err != nil {
		return CancelAllAfterResult{}, err
	}
	body, err := json.Marshal(struct {
		Timeout int `json:"timeout"`
	}{Timeout: request.TimeoutSeconds})
	if err != nil {
		return CancelAllAfterResult{}, validationError("encode HTX dead man's switch request: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v2/algo-orders/cancel-all-after", nil, body,
		rateGroupOrder, client.orderQuota, credential.PermissionTrade,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CancelAllAfterResult{}, err
	}
	var value CancelAllAfterResult
	raw, err := client.decodeDataForOperation(
		response, commonexchange.OperationMutation, &value,
	)
	if err != nil {
		return CancelAllAfterResult{}, err
	}
	if value.CurrentTime <= 0 {
		return CancelAllAfterResult{}, client.decodeBodyErrorForOperation(
			response, commonexchange.OperationMutation,
			errors.New("HTX dead man's switch acknowledgement is missing the server time"),
		)
	}
	value.Raw = raw
	return value, nil
}

// RecentOrderHistory는 `/v1/order/history`로 최근 48시간 주문 이력을 조회한다.
// API로 만든 주문은 취소된 뒤 2시간이 지나면 조회되지 않는다.
func (client *Client) RecentOrderHistory(
	ctx context.Context,
	request RecentOrderHistoryRequest,
	options ...trade.RequestOption,
) (RecentOrderHistory, error) {
	if err := request.validate(); err != nil {
		return RecentOrderHistory{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v1/order/history", request.values(), nil,
		rateGroupTradeHistory, client.tradeHistoryQuota, credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return RecentOrderHistory{}, err
	}
	var envelope struct {
		Data     []json.RawMessage `json:"data"`
		NextTime Scalar            `json:"next-time"`
	}
	raw, err := client.decodeResponse(response, &envelope)
	if err != nil {
		return RecentOrderHistory{}, err
	}
	value := RecentOrderHistory{
		Orders: make([]Order, len(envelope.Data)), NextTime: envelope.NextTime, Raw: raw,
	}
	for index, itemRaw := range envelope.Data {
		if err := json.Unmarshal(itemRaw, &value.Orders[index]); err != nil {
			return RecentOrderHistory{}, client.decodeBodyError(response, err)
		}
		if value.Orders[index].ID == "" || value.Orders[index].Symbol == "" {
			return RecentOrderHistory{}, client.decodeBodyError(
				response, errors.New("HTX order history item is missing an order ID or symbol"),
			)
		}
		value.Orders[index].Raw = cloneBytes(itemRaw)
	}
	return value, nil
}

// TransactFeeRates는 지정한 거래쌍에 현재 계정이 적용받는 수수료율을 조회한다.
func (client *Client) TransactFeeRates(
	ctx context.Context,
	request TransactFeeRateRequest,
	options ...trade.RequestOption,
) ([]TransactFeeRate, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/reference/transact-fee-rate",
		url.Values{"symbols": {strings.Join(request.Symbols, ",")}}, nil,
		rateGroupAccount, client.accountQuota, credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	var rawItems []json.RawMessage
	if _, err := client.decodeDataForOperation(
		response, commonexchange.OperationRead, &rawItems,
	); err != nil {
		return nil, err
	}
	items := make([]TransactFeeRate, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeBodyError(response, err)
		}
		if items[index].Symbol == "" {
			return nil, client.decodeBodyError(
				response, errors.New("HTX fee rate item is missing a symbol"),
			)
		}
		items[index].Raw = cloneBytes(raw)
	}
	return items, nil
}

// PlaceAlgoOrder는 사용자 주문 ID를 가진 조건부(스탑·트레일링) 주문을 접수한다.
// 발동 전에는 증거금을 잠그지 않으며 발동 시점에 잔고가 부족하면 발동이 실패한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) PlaceAlgoOrder(
	ctx context.Context,
	request PlaceAlgoOrderRequest,
	options ...trade.RequestOption,
) (AlgoOrderReference, error) {
	if err := request.validate(); err != nil {
		return AlgoOrderReference{}, err
	}
	body, err := request.body()
	if err != nil {
		return AlgoOrderReference{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v2/algo-orders", nil, body,
		rateGroupAlgoOrder, min(client.orderQuota, algoOrderRequestLimit),
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return AlgoOrderReference{}, err
	}
	var wire struct {
		ClientOrderID string `json:"clientOrderId"`
	}
	raw, err := client.decodeDataForOperation(
		response, commonexchange.OperationMutation, &wire,
	)
	if err != nil {
		return AlgoOrderReference{}, err
	}
	if wire.ClientOrderID == "" {
		return AlgoOrderReference{}, client.decodeBodyErrorForOperation(
			response, commonexchange.OperationMutation,
			errors.New("HTX conditional order acknowledgement is missing the client order ID"),
		)
	}
	return AlgoOrderReference{ClientOrderID: wire.ClientOrderID, Raw: raw}, nil
}

// CancelAlgoOrders는 발동 전 조건부 주문을 사용자 주문 ID(최대 50건)로 취소 접수한다.
// 이미 발동한 주문은 일반 주문 취소 API로 취소해야 한다.
func (client *Client) CancelAlgoOrders(
	ctx context.Context,
	request CancelAlgoOrdersRequest,
	options ...trade.RequestOption,
) (CancelAlgoOrdersResult, error) {
	if err := request.validate(); err != nil {
		return CancelAlgoOrdersResult{}, err
	}
	body, err := json.Marshal(struct {
		ClientOrderIDs []string `json:"clientOrderIds"`
	}{ClientOrderIDs: request.ClientOrderIDs})
	if err != nil {
		return CancelAlgoOrdersResult{}, validationError("encode HTX conditional cancel request: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v2/algo-orders/cancellation", nil, body,
		rateGroupAlgoOrder, min(client.orderQuota, algoOrderRequestLimit),
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CancelAlgoOrdersResult{}, err
	}
	var wire struct {
		Accepted []string `json:"accepted"`
		Rejected []string `json:"rejected"`
	}
	raw, err := client.decodeDataForOperation(
		response, commonexchange.OperationMutation, &wire,
	)
	if err != nil {
		return CancelAlgoOrdersResult{}, err
	}
	return CancelAlgoOrdersResult{Accepted: wire.Accepted, Rejected: wire.Rejected, Raw: raw}, nil
}

// OpenAlgoOrders는 아직 발동하지 않은(created) 조건부 주문 목록을 조회한다.
func (client *Client) OpenAlgoOrders(
	ctx context.Context,
	request OpenAlgoOrdersRequest,
	options ...trade.RequestOption,
) (AlgoOrderPage, error) {
	if err := request.validate(); err != nil {
		return AlgoOrderPage{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/algo-orders/opening", request.values(), nil,
		rateGroupAlgoOrder, min(client.orderQuota, algoOrderRequestLimit),
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return AlgoOrderPage{}, err
	}
	return client.decodeAlgoOrderPage(response)
}

// AlgoOrderHistory는 취소·거절·발동된 조건부 주문 이력을 조회한다.
// 발동된 주문의 최신 상태는 일반 주문 조회 API로 확인해야 한다.
func (client *Client) AlgoOrderHistory(
	ctx context.Context,
	request AlgoOrderHistoryRequest,
	options ...trade.RequestOption,
) (AlgoOrderPage, error) {
	if err := request.validate(); err != nil {
		return AlgoOrderPage{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/algo-orders/history", request.values(), nil,
		rateGroupAlgoOrder, min(client.orderQuota, algoOrderRequestLimit),
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return AlgoOrderPage{}, err
	}
	return client.decodeAlgoOrderPage(response)
}

// AlgoOrder는 사용자 주문 ID로 조건부 주문 한 건을 조회한다.
func (client *Client) AlgoOrder(
	ctx context.Context,
	clientOrderID string,
	options ...trade.RequestOption,
) (AlgoOrder, error) {
	if !clientOrderIDPattern.MatchString(clientOrderID) {
		return AlgoOrder{}, validationError("invalid HTX client order ID %q", clientOrderID)
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v2/algo-orders/specific",
		url.Values{"clientOrderId": {clientOrderID}}, nil,
		rateGroupAlgoOrder, min(client.orderQuota, algoOrderRequestLimit),
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return AlgoOrder{}, err
	}
	var value AlgoOrder
	raw, err := client.decodeDataForOperation(response, commonexchange.OperationRead, &value)
	if err != nil {
		return AlgoOrder{}, err
	}
	if value.ClientOrderID == "" || value.Symbol == "" {
		return AlgoOrder{}, client.decodeBodyError(
			response, errors.New("HTX conditional order is missing a client order ID or symbol"),
		)
	}
	value.Raw = raw
	return value, nil
}

func (client *Client) decodeAlgoOrderPage(response commonexchange.Response) (AlgoOrderPage, error) {
	var envelope struct {
		Data   []json.RawMessage `json:"data"`
		NextID Scalar            `json:"nextId"`
	}
	raw, err := client.decodeResponse(response, &envelope)
	if err != nil {
		return AlgoOrderPage{}, err
	}
	value := AlgoOrderPage{
		Orders: make([]AlgoOrder, len(envelope.Data)), NextID: envelope.NextID, Raw: raw,
	}
	for index, itemRaw := range envelope.Data {
		if err := json.Unmarshal(itemRaw, &value.Orders[index]); err != nil {
			return AlgoOrderPage{}, client.decodeBodyError(response, err)
		}
		if value.Orders[index].ClientOrderID == "" || value.Orders[index].Symbol == "" {
			return AlgoOrderPage{}, client.decodeBodyError(
				response, errors.New("HTX conditional order item is missing a client order ID or symbol"),
			)
		}
		value.Orders[index].Raw = cloneBytes(itemRaw)
	}
	return value, nil
}

// batchOrderWire는 일괄 주문 body의 주문 한 건이다. 단건 주문과 같은 필드를 쓴다.
type batchOrderWire struct {
	AccountID        string    `json:"account-id"`
	Amount           string    `json:"amount"`
	Price            string    `json:"price,omitempty"`
	Source           string    `json:"source"`
	Symbol           string    `json:"symbol"`
	Type             OrderType `json:"type"`
	ClientOrderID    string    `json:"client-order-id"`
	SelfMatchPrevent int       `json:"self-match-prevent,omitempty"`
}

func (request PlaceBatchOrdersRequest) validate() error {
	if len(request.Orders) == 0 || len(request.Orders) > maximumBatchOrders {
		return validationError("batch order count must be between 1 and %d", maximumBatchOrders)
	}
	seen := make(map[string]struct{}, len(request.Orders))
	for _, order := range request.Orders {
		if err := order.validate(); err != nil {
			return err
		}
		if _, exists := seen[order.ClientOrderID]; exists {
			return validationError("batch orders contain a duplicate client order ID")
		}
		seen[order.ClientOrderID] = struct{}{}
	}
	return nil
}

func (request CancelOpenOrdersRequest) validate() error {
	if request.AccountID != "" && !accountIDPattern.MatchString(request.AccountID) {
		return validationError("invalid HTX account ID %q", request.AccountID)
	}
	if len(request.Symbols) == 0 && !request.AllSymbols {
		return validationError("batch cancel requires symbols or an explicit all-symbols scope")
	}
	if len(request.Symbols) > 0 && request.AllSymbols {
		return validationError("batch cancel symbols and all-symbols scope are mutually exclusive")
	}
	if len(request.Symbols) > maximumBatchCancelSymbols {
		return validationError("batch cancel accepts at most %d symbols", maximumBatchCancelSymbols)
	}
	seen := make(map[string]struct{}, len(request.Symbols))
	for _, symbol := range request.Symbols {
		if err := validateSymbol(symbol); err != nil {
			return err
		}
		if _, exists := seen[symbol]; exists {
			return validationError("batch cancel symbols contain a duplicate")
		}
		seen[symbol] = struct{}{}
	}
	if err := validateOrderTypes(request.Types); err != nil {
		return err
	}
	if request.Side != "" && request.Side != SideBuy && request.Side != SideSell {
		return validationError("batch cancel side must be buy or sell")
	}
	if request.Size < 0 || request.Size > maximumBatchCancelOrders {
		return validationError("batch cancel size must be between 1 and %d or zero", maximumBatchCancelOrders)
	}
	return nil
}

func (request CancelOrdersByIDsRequest) validate() error {
	if (len(request.OrderIDs) == 0) == (len(request.ClientOrderIDs) == 0) {
		return validationError("exactly one of order IDs or client order IDs is required")
	}
	if len(request.OrderIDs) > maximumBatchCancelByIDs ||
		len(request.ClientOrderIDs) > maximumBatchCancelByIDs {
		return validationError("batch cancel accepts at most %d IDs", maximumBatchCancelByIDs)
	}
	seen := make(map[string]struct{}, len(request.OrderIDs)+len(request.ClientOrderIDs))
	for _, orderID := range request.OrderIDs {
		if !orderIDPattern.MatchString(orderID) {
			return validationError("invalid HTX order ID %q", orderID)
		}
		if _, exists := seen[orderID]; exists {
			return validationError("batch cancel order IDs contain a duplicate")
		}
		seen[orderID] = struct{}{}
	}
	for _, clientOrderID := range request.ClientOrderIDs {
		if !clientOrderIDPattern.MatchString(clientOrderID) {
			return validationError("invalid HTX client order ID %q", clientOrderID)
		}
		if _, exists := seen[clientOrderID]; exists {
			return validationError("batch cancel client order IDs contain a duplicate")
		}
		seen[clientOrderID] = struct{}{}
	}
	return nil
}

func (request CancelAllAfterRequest) validate() error {
	if request.TimeoutSeconds != 0 && request.TimeoutSeconds < minimumCancelAllAfterSeconds {
		return validationError(
			"dead man's switch timeout must be zero or at least %d seconds", minimumCancelAllAfterSeconds,
		)
	}
	return nil
}

func (request RecentOrderHistoryRequest) validate() error {
	if request.Symbol != "" {
		if err := validateSymbol(request.Symbol); err != nil {
			return err
		}
	}
	if err := validateTimeRange(request.Start, request.End); err != nil {
		return err
	}
	if request.Direction != "" && request.Direction != QueryDirectionNext &&
		request.Direction != QueryDirectionPrev {
		return validationError("order history direction must be next or prev")
	}
	if request.Size != 0 &&
		(request.Size < minimumRecentOrderHistorySize || request.Size > maximumRecentOrderHistorySize) {
		return validationError(
			"order history size must be between %d and %d or zero",
			minimumRecentOrderHistorySize, maximumRecentOrderHistorySize,
		)
	}
	return nil
}

func (request RecentOrderHistoryRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "symbol", request.Symbol)
	setTimeRange(values, request.Start, request.End)
	setIfNotEmpty(values, "direct", string(request.Direction))
	setPositiveInt(values, "size", request.Size)
	return values
}

func (request TransactFeeRateRequest) validate() error {
	if len(request.Symbols) == 0 {
		return validationError("fee rate symbols are required")
	}
	seen := make(map[string]struct{}, len(request.Symbols))
	for _, symbol := range request.Symbols {
		if err := validateSymbol(symbol); err != nil {
			return err
		}
		if _, exists := seen[symbol]; exists {
			return validationError("fee rate symbols contain a duplicate")
		}
		seen[symbol] = struct{}{}
	}
	return nil
}

func (value SortOrder) valid() bool {
	return value == SortAscending || value == SortDescending
}

func (value AlgoOrderType) valid() bool {
	return value == AlgoOrderTypeLimit || value == AlgoOrderTypeMarket
}

func (value AlgoTimeInForce) valid() bool {
	switch value {
	case AlgoTimeInForceGTC, AlgoTimeInForceBOC, AlgoTimeInForceIOC, AlgoTimeInForceFOK:
		return true
	default:
		return false
	}
}

func (value AlgoOrderStatus) validHistoryStatus() bool {
	return value == AlgoOrderStatusCanceled || value == AlgoOrderStatusRejected ||
		value == AlgoOrderStatusTriggered
}

func (request PlaceAlgoOrderRequest) validate() error {
	if !accountIDPattern.MatchString(request.AccountID) {
		return validationError("invalid HTX account ID %q", request.AccountID)
	}
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("conditional order side must be buy or sell")
	}
	if !request.Type.valid() {
		return validationError("unsupported conditional order type %q", request.Type)
	}
	if !clientOrderIDPattern.MatchString(request.ClientOrderID) {
		return validationError("client order ID is required and must match [0-9A-Za-z_-]{1,64}")
	}
	if err := validatePositiveDecimal("stop price", request.StopPrice); err != nil {
		return err
	}
	if request.TimeInForce != "" && !request.TimeInForce.valid() {
		return validationError("unsupported conditional order time in force %q", request.TimeInForce)
	}
	if request.Type == AlgoOrderTypeLimit {
		if request.Value != "" {
			return validationError("limit conditional order does not accept value")
		}
		if err := validatePositiveDecimal("price", request.Price); err != nil {
			return err
		}
		if err := validatePositiveDecimal("size", request.Size); err != nil {
			return err
		}
	} else {
		if request.Price != "" {
			return validationError("market conditional order does not accept price")
		}
		if request.TimeInForce != "" && request.TimeInForce != AlgoTimeInForceIOC {
			return validationError("market conditional order only accepts ioc time in force")
		}
		if request.Side == SideBuy {
			if request.Size != "" {
				return validationError("market buy conditional order does not accept size")
			}
			if err := validatePositiveDecimal("value", request.Value); err != nil {
				return err
			}
		} else {
			if request.Value != "" {
				return validationError("market sell conditional order does not accept value")
			}
			if err := validatePositiveDecimal("size", request.Size); err != nil {
				return err
			}
		}
	}
	if request.TrailingRate != "" {
		if request.Type != AlgoOrderTypeMarket {
			return validationError("trailing rate is only valid for market conditional orders")
		}
		if err := validatePositiveDecimal("trailing rate", request.TrailingRate); err != nil {
			return err
		}
		rate, _ := new(big.Rat).SetString(request.TrailingRate)
		if rate.Cmp(big.NewRat(1, 1000)) < 0 || rate.Cmp(big.NewRat(5, 100)) > 0 {
			return validationError("trailing rate must be between 0.001 and 0.05")
		}
	}
	return nil
}

// body는 검증된 조건부 주문을 HTX JSON body로 직렬화한다.
func (request PlaceAlgoOrderRequest) body() ([]byte, error) {
	accountID, err := strconv.ParseInt(request.AccountID, 10, 64)
	if err != nil {
		return nil, validationError("invalid HTX account ID %q", request.AccountID)
	}
	body, err := json.Marshal(struct {
		AccountID     int64           `json:"accountId"`
		Symbol        string          `json:"symbol"`
		OrderPrice    string          `json:"orderPrice,omitempty"`
		OrderSide     Side            `json:"orderSide"`
		OrderSize     string          `json:"orderSize,omitempty"`
		OrderValue    string          `json:"orderValue,omitempty"`
		TimeInForce   AlgoTimeInForce `json:"timeInForce,omitempty"`
		OrderType     AlgoOrderType   `json:"orderType"`
		ClientOrderID string          `json:"clientOrderId"`
		StopPrice     string          `json:"stopPrice"`
		TrailingRate  string          `json:"trailingRate,omitempty"`
	}{
		AccountID: accountID, Symbol: request.Symbol, OrderPrice: request.Price,
		OrderSide: request.Side, OrderSize: request.Size, OrderValue: request.Value,
		TimeInForce: request.TimeInForce, OrderType: request.Type,
		ClientOrderID: request.ClientOrderID, StopPrice: request.StopPrice,
		TrailingRate: request.TrailingRate,
	})
	if err != nil {
		return nil, validationError("encode HTX conditional order: %v", err)
	}
	return body, nil
}

func (request CancelAlgoOrdersRequest) validate() error {
	if len(request.ClientOrderIDs) == 0 || len(request.ClientOrderIDs) > maximumAlgoCancelOrders {
		return validationError(
			"conditional order cancel count must be between 1 and %d", maximumAlgoCancelOrders,
		)
	}
	seen := make(map[string]struct{}, len(request.ClientOrderIDs))
	for _, clientOrderID := range request.ClientOrderIDs {
		if !clientOrderIDPattern.MatchString(clientOrderID) {
			return validationError("invalid HTX client order ID %q", clientOrderID)
		}
		if _, exists := seen[clientOrderID]; exists {
			return validationError("conditional order cancel IDs contain a duplicate")
		}
		seen[clientOrderID] = struct{}{}
	}
	return nil
}

// validateAlgoFilter는 조건부 주문 목록 조회가 공유하는 필터와 페이지 조건을 검증한다.
func validateAlgoFilter(
	accountID string,
	side Side,
	orderType AlgoOrderType,
	sortOrder SortOrder,
	limit int,
	fromID string,
) error {
	if accountID != "" && !accountIDPattern.MatchString(accountID) {
		return validationError("invalid HTX account ID %q", accountID)
	}
	if side != "" && side != SideBuy && side != SideSell {
		return validationError("conditional order side must be buy or sell")
	}
	if orderType != "" && !orderType.valid() {
		return validationError("unsupported conditional order type %q", orderType)
	}
	if sortOrder != "" && !sortOrder.valid() {
		return validationError("sort order must be asc or desc")
	}
	if limit < 0 || limit > maximumAlgoPageLimit {
		return validationError("conditional order limit must be between 1 and %d or zero", maximumAlgoPageLimit)
	}
	if fromID != "" && !orderIDPattern.MatchString(fromID) {
		return validationError("invalid HTX cursor %q", fromID)
	}
	return nil
}

// algoFilterValues는 조건부 주문 목록 조회가 공유하는 쿼리를 만든다.
func algoFilterValues(
	accountID string,
	side Side,
	orderType AlgoOrderType,
	sortOrder SortOrder,
	limit int,
	fromID string,
) url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "accountId", accountID)
	setIfNotEmpty(values, "orderSide", string(side))
	setIfNotEmpty(values, "orderType", string(orderType))
	setIfNotEmpty(values, "sort", string(sortOrder))
	setPositiveInt(values, "limit", limit)
	setIfNotEmpty(values, "fromId", fromID)
	return values
}

func (request OpenAlgoOrdersRequest) validate() error {
	if request.Symbol != "" {
		if err := validateSymbol(request.Symbol); err != nil {
			return err
		}
	}
	return validateAlgoFilter(
		request.AccountID, request.Side, request.Type, request.Sort, request.Limit, request.FromID,
	)
}

func (request OpenAlgoOrdersRequest) values() url.Values {
	values := algoFilterValues(
		request.AccountID, request.Side, request.Type, request.Sort, request.Limit, request.FromID,
	)
	setIfNotEmpty(values, "symbol", request.Symbol)
	return values
}

func (request AlgoOrderHistoryRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if !request.Status.validHistoryStatus() {
		return validationError("conditional order history status must be canceled, rejected or triggered")
	}
	if request.Start != nil && request.Start.UnixMilli() <= 0 ||
		request.End != nil && request.End.UnixMilli() <= 0 {
		return validationError("query timestamps must be after the Unix epoch")
	}
	if request.Start != nil && request.End != nil && !request.Start.Before(*request.End) {
		return validationError("query start must be before end")
	}
	return validateAlgoFilter(
		request.AccountID, request.Side, request.Type, request.Sort, request.Limit, request.FromID,
	)
}

func (request AlgoOrderHistoryRequest) values() url.Values {
	values := algoFilterValues(
		request.AccountID, request.Side, request.Type, request.Sort, request.Limit, request.FromID,
	)
	values.Set("symbol", request.Symbol)
	values.Set("orderStatus", string(request.Status))
	if request.Start != nil {
		values.Set("startTime", strconv.FormatInt(request.Start.UnixMilli(), 10))
	}
	if request.End != nil {
		values.Set("endTime", strconv.FormatInt(request.End.UnixMilli(), 10))
	}
	return values
}

func joinOrderTypes(types []OrderType) string {
	items := make([]string, len(types))
	for index, value := range types {
		items[index] = string(value)
	}
	return strings.Join(items, ",")
}
