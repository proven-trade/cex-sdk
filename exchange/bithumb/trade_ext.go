package bithumb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const (
	// maxOrderSearchIDs는 다건 주문 조회가 한 번에 받는 ID 목록의 최대 개수다.
	maxOrderSearchIDs = 100
	// maxBatchOrders는 다건 주문 생성과 다건 주문 취소가 한 번에 받는 최대 개수다.
	maxBatchOrders = 30
)

// OrderChanceRequest는 마켓별 주문 가능 정보 조회 조건이다.
type OrderChanceRequest struct {
	Market string
}

// OrderChance는 마켓별 수수료율, 주문 제약, 매수·매도에 쓰는 계좌 상태다.
type OrderChance struct {
	BidFee      Decimal           `json:"bid_fee"`
	AskFee      Decimal           `json:"ask_fee"`
	MakerBidFee Decimal           `json:"maker_bid_fee"`
	MakerAskFee Decimal           `json:"maker_ask_fee"`
	Market      OrderChanceMarket `json:"market"`
	BidAccount  Balance           `json:"bid_account"`
	AskAccount  Balance           `json:"ask_account"`
	Raw         json.RawMessage   `json:"-"`
}

// OrderChanceMarket은 주문 가능 정보에 포함된 마켓 정보와 주문 제약이다.
type OrderChanceMarket struct {
	ID         string                `json:"id"`
	Name       string                `json:"name"`
	OrderTypes []OrderType           `json:"order_types"`
	OrderSides []Side                `json:"order_sides"`
	BidTypes   []OrderType           `json:"bid_types"`
	AskTypes   []OrderType           `json:"ask_types"`
	Bid        OrderChanceConstraint `json:"bid"`
	Ask        OrderChanceConstraint `json:"ask"`
	MaxTotal   Decimal               `json:"max_total"`
	State      string                `json:"state"`
}

// OrderChanceConstraint는 매수 또는 매도 쪽 주문 금액 단위와 최소 주문 금액이다.
type OrderChanceConstraint struct {
	Currency  string  `json:"currency"`
	PriceUnit Decimal `json:"price_unit"`
	MinTotal  Decimal `json:"min_total"`
}

// SearchOrdersRequest는 주문 ID 또는 사용자 주문 ID 목록으로 주문을 한 번에 조회하는 조건이다.
// OrderIDs와 ClientOrderIDs 중 정확히 하나만 지정하며 각각 최대 100건이다.
// Market과 OrderBy(asc 또는 desc)는 선택이다.
type SearchOrdersRequest struct {
	Market         string
	OrderIDs       []string
	ClientOrderIDs []string
	OrderBy        string
}

// BatchOrdersRequest는 다건 주문 생성 요청이다.
// 한 번에 최대 30건이며 각 주문은 PlaceOrderRequest와 같은 규칙으로 검증한다.
// 같은 요청 안에서 ClientOrderID가 중복되면 거래소가 뒤의 주문을 실패 처리하므로 SDK가 먼저 거부한다.
type BatchOrdersRequest struct {
	Orders []PlaceOrderRequest
}

// BatchOrderResult는 다건 주문 응답의 주문 한 건 결과다.
// 거래소는 일부 주문이 실패해도 HTTP 200을 반환하므로 항목마다 Succeeded를 확인해야 한다.
// 실패 항목에는 ErrorName과 ErrorMessage만 채워진다.
type BatchOrderResult struct {
	OrderID       string          `json:"order_id"`
	ClientOrderID string          `json:"client_order_id"`
	Market        string          `json:"market"`
	Side          Side            `json:"side"`
	OrderType     OrderType       `json:"order_type"`
	TimeInForce   TimeInForce     `json:"time_in_force"`
	CreatedAt     string          `json:"created_at"`
	STPType       string          `json:"stp_type"`
	ErrorName     string          `json:"name"`
	ErrorMessage  string          `json:"message"`
	Raw           json.RawMessage `json:"-"`
}

// BatchOrdersResult는 다건 주문 생성 응답이다. Orders는 거래소가 돌려준 항목 순서를 따른다.
type BatchOrdersResult struct {
	Orders []BatchOrderResult `json:"batch_orders_response"`
	Raw    json.RawMessage    `json:"-"`
}

// CancelBatchOrdersRequest는 주문 ID 또는 사용자 주문 ID 목록으로 여러 주문의 취소를 요청한다.
// OrderIDs와 ClientOrderIDs 중 정확히 하나만 지정하며 각각 최대 30건이다.
type CancelBatchOrdersRequest struct {
	OrderIDs       []string
	ClientOrderIDs []string
}

// BatchItemError는 다건 요청 항목 하나의 실패 사유다.
type BatchItemError struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// BatchCancelFailure는 다건 주문 취소에서 접수되지 않은 주문 한 건이다.
// client_order_id로 요청했지만 매칭되는 주문이 없으면 OrderID가 비어 있을 수 있다.
type BatchCancelFailure struct {
	OrderID       string          `json:"order_id"`
	ClientOrderID string          `json:"client_order_id"`
	Reason        BatchItemError  `json:"error"`
	Raw           json.RawMessage `json:"-"`
}

// BatchCancelResult는 다건 주문 취소 접수 결과다. Success는 취소가 접수된 주문이고 최종 상태는 다시 조회해야 한다.
type BatchCancelResult struct {
	Success []CancelResult       `json:"success"`
	Fail    []BatchCancelFailure `json:"fail"`
	Raw     json.RawMessage      `json:"-"`
}

// Succeeded는 거래소가 주문을 접수했는지 반환한다. 문서 기준으로 order_id가 있으면 성공이다.
func (result BatchOrderResult) Succeeded() bool {
	return result.OrderID != ""
}

// OrderChance는 마켓별 수수료율, 주문 제약, 매수·매도 계좌 상태를 조회한다.
func (client *Client) OrderChance(
	ctx context.Context,
	request OrderChanceRequest,
	options ...trade.RequestOption,
) (OrderChance, error) {
	if err := request.validate(); err != nil {
		return OrderChance{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/v1/orders/chance", request.parameters(), nil, rateLimitPrivateOther,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return OrderChance{}, err
	}
	var result OrderChance
	if err := client.decodeResponse(response, commonexchange.OperationRead, &result); err != nil {
		return OrderChance{}, err
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

// SearchOrders는 주문 ID 또는 사용자 주문 ID 목록으로 주문을 한 번에 조회한다.
// 매칭되지 않는 ID는 응답에서 제외되므로 결과 개수가 요청 개수보다 적을 수 있다.
// POST 요청이지만 상태를 바꾸지 않는 조회이므로 읽기 재시도 정책을 따른다.
func (client *Client) SearchOrders(
	ctx context.Context,
	request SearchOrdersRequest,
	options ...trade.RequestOption,
) ([]OrderSummary, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	params := request.parameters()
	body, err := encodeBody(params)
	if err != nil {
		return nil, validationError("encode order search body: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v2/orders/search", params, body, rateLimitPrivateOther,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(item *OrderSummary, raw []byte) {
		item.Raw = raw
	})
}

// PlaceBatchOrders는 여러 주문을 한 번에 생성한다. 요청 제한은 단건 주문과 분리된 다건 주문 한도를 따른다.
// 일부 주문이 실패해도 HTTP 200이 오므로 각 BatchOrderResult.Succeeded를 확인해야 한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) PlaceBatchOrders(
	ctx context.Context,
	request BatchOrdersRequest,
	options ...trade.RequestOption,
) (BatchOrdersResult, error) {
	if err := request.validate(); err != nil {
		return BatchOrdersResult{}, err
	}
	hashParams, items := request.parameters()
	body, err := encodeBatchOrdersBody(items)
	if err != nil {
		return BatchOrdersResult{}, validationError("encode batch order body: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v2/orders/batch", hashParams, body, rateLimitOrderBatch,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return BatchOrdersResult{}, err
	}
	var envelope struct {
		Orders []json.RawMessage `json:"batch_orders_response"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationMutation, &envelope); err != nil {
		return BatchOrdersResult{}, err
	}
	orders, err := decodeRawItems(envelope.Orders, func(item *BatchOrderResult, raw []byte) {
		item.Raw = raw
	})
	if err != nil {
		return BatchOrdersResult{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, fmt.Errorf("decode Bithumb batch order item: %w", err),
		)
	}
	return BatchOrdersResult{Orders: orders, Raw: cloneBytes(response.Body)}, nil
}

// CancelBatchOrders는 여러 주문의 취소를 한 번에 요청한다. 요청 제한은 다건 주문 생성과 같은 한도를 공유한다.
// 성공 응답은 취소 접수이며 최종 상태는 다시 조회해야 한다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) CancelBatchOrders(
	ctx context.Context,
	request CancelBatchOrdersRequest,
	options ...trade.RequestOption,
) (BatchCancelResult, error) {
	if err := request.validate(); err != nil {
		return BatchCancelResult{}, err
	}
	params := request.parameters()
	body, err := encodeBody(params)
	if err != nil {
		return BatchCancelResult{}, validationError("encode batch cancel body: %v", err)
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/v2/orders/cancel", params, body, rateLimitOrderBatch,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return BatchCancelResult{}, err
	}
	var envelope struct {
		Success []json.RawMessage `json:"success"`
		Fail    []json.RawMessage `json:"fail"`
	}
	if err := client.decodeResponse(response, commonexchange.OperationMutation, &envelope); err != nil {
		return BatchCancelResult{}, err
	}
	success, err := decodeRawItems(envelope.Success, func(item *CancelResult, raw []byte) {
		item.Raw = raw
	})
	if err != nil {
		return BatchCancelResult{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, fmt.Errorf("decode Bithumb batch cancel item: %w", err),
		)
	}
	fail, err := decodeRawItems(envelope.Fail, func(item *BatchCancelFailure, raw []byte) {
		item.Raw = raw
	})
	if err != nil {
		return BatchCancelResult{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, fmt.Errorf("decode Bithumb batch cancel failure: %w", err),
		)
	}
	return BatchCancelResult{Success: success, Fail: fail, Raw: cloneBytes(response.Body)}, nil
}

func (request OrderChanceRequest) validate() error {
	return validateMarket(request.Market)
}

func (request OrderChanceRequest) parameters() parameters {
	return parameters{{key: "market", value: request.Market}}
}

func (request SearchOrdersRequest) validate() error {
	if request.Market != "" {
		if err := validateMarket(request.Market); err != nil {
			return err
		}
	}
	if (len(request.OrderIDs) == 0) == (len(request.ClientOrderIDs) == 0) {
		return validationError("exactly one of OrderIDs or ClientOrderIDs is required")
	}
	if err := validateIDList("orderIDs", request.OrderIDs, maxOrderSearchIDs, nil); err != nil {
		return err
	}
	if err := validateIDList("clientOrderIDs", request.ClientOrderIDs, maxOrderSearchIDs, clientOrderIDPattern); err != nil {
		return err
	}
	return validateOrderBy(request.OrderBy)
}

func (request SearchOrdersRequest) parameters() parameters {
	values := parameters{}
	values.add("market", request.Market)
	values.addAll("order_ids[]", request.OrderIDs)
	values.addAll("client_order_ids[]", request.ClientOrderIDs)
	values.add("order_by", request.OrderBy)
	return values
}

func (request BatchOrdersRequest) validate() error {
	if len(request.Orders) == 0 || len(request.Orders) > maxBatchOrders {
		return validationError("batch orders must contain between 1 and %d orders", maxBatchOrders)
	}
	seen := make(map[string]struct{}, len(request.Orders))
	for index, order := range request.Orders {
		if err := order.validate(); err != nil {
			return batchItemError(index, err)
		}
		if order.ClientOrderID == "" {
			continue
		}
		if _, exists := seen[order.ClientOrderID]; exists {
			return validationError("duplicate clientOrderID %q in batch orders", order.ClientOrderID)
		}
		seen[order.ClientOrderID] = struct{}{}
	}
	return nil
}

// parameters는 서명 해시에 쓰는 `batch_orders[i][key]` 평탄화 parameter와
// JSON 본문에 쓰는 주문별 parameter를 같은 순서로 함께 만든다.
func (request BatchOrdersRequest) parameters() (parameters, []parameters) {
	flat := parameters{}
	items := make([]parameters, len(request.Orders))
	for index, order := range request.Orders {
		items[index] = order.parameters()
		prefix := "batch_orders[" + strconv.Itoa(index) + "]["
		for _, value := range items[index] {
			flat = append(flat, parameter{key: prefix + value.key + "]", value: value.value})
		}
	}
	return flat, items
}

func (request CancelBatchOrdersRequest) validate() error {
	if (len(request.OrderIDs) == 0) == (len(request.ClientOrderIDs) == 0) {
		return validationError("exactly one of OrderIDs or ClientOrderIDs is required")
	}
	if err := validateIDList("orderIDs", request.OrderIDs, maxBatchOrders, nil); err != nil {
		return err
	}
	return validateIDList("clientOrderIDs", request.ClientOrderIDs, maxBatchOrders, clientOrderIDPattern)
}

func (request CancelBatchOrdersRequest) parameters() parameters {
	values := parameters{}
	values.addAll("order_ids[]", request.OrderIDs)
	values.addAll("client_order_ids[]", request.ClientOrderIDs)
	return values
}

// batchItemError는 다건 주문 항목의 검증 오류에 항목 순번을 붙인다.
func batchItemError(index int, err error) error {
	var apiError *trade.APIError
	if errors.As(err, &apiError) && apiError.Cause != nil {
		return validationError("batch order %d: %v", index, apiError.Cause)
	}
	return validationError("batch order %d: %v", index, err)
}

// decodeRawItems는 이미 읽은 JSON 배열 항목을 하나씩 복호화하고 항목별 원본 JSON을 보존한다.
func decodeRawItems[T any](rawItems []json.RawMessage, setRaw func(*T, []byte)) ([]T, error) {
	items := make([]T, len(rawItems))
	for index, rawItem := range rawItems {
		if err := json.Unmarshal(rawItem, &items[index]); err != nil {
			return nil, err
		}
		setRaw(&items[index], cloneBytes(rawItem))
	}
	return items, nil
}
