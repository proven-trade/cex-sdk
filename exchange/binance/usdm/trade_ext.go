package usdm

import (
	"context"
	"encoding/json"
	"fmt"
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
	// maxBatchOrders는 일괄 주문·정정이 한 요청에 담을 수 있는 최대 주문 수다.
	maxBatchOrders = 5
	// maxBatchCancelIDs는 일괄 취소가 한 요청에 담을 수 있는 최대 식별자 수다.
	maxBatchCancelIDs = 10
)

// batchOrderCost는 일괄 주문·정정의 주문 count 단위다.
// 공식 문서는 10초 창에 5, 1분 창에 1을 차감한다고 적는다.
var batchOrderCost = orderCost{tenSeconds: 5, minute: 1}

// StatusCode는 숫자 200과 문자열 "200"으로 모두 오는 응답 code를 정수로 받는다.
type StatusCode int

// UnmarshalJSON은 숫자와 숫자 문자열을 모두 받고 null과 빈 문자열은 무시한다.
func (code *StatusCode) UnmarshalJSON(data []byte) error {
	text := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if text == "" || text == "null" {
		return nil
	}
	value, err := strconv.Atoi(text)
	if err != nil {
		return fmt.Errorf("Binance USD-M status code %q is not an integer", text)
	}
	*code = StatusCode(value)
	return nil
}

// AckResult는 설정 변경과 전체 취소가 code와 msg만 돌려주는 응답이다. 성공하면 Code가 200이다.
type AckResult struct {
	Code    StatusCode      `json:"code"`
	Message string          `json:"msg"`
	Raw     json.RawMessage `json:"-"`
}

func (item *AckResult) setRaw(raw json.RawMessage) { item.Raw = raw }

// decodeObject는 응답 본문을 T 한 건으로 해석하고 원본 JSON을 보존한다.
func decodeObject[T any, P rawSetter[T]](client *Client, response commonexchange.Response, operation commonexchange.OperationKind) (T, error) {
	var item T
	if err := client.decode(response, operation, &item); err != nil {
		var zero T
		return zero, err
	}
	P(&item).setRaw(cloneBytes(response.Body))
	return item, nil
}

// decodeArray는 배열 응답을 항목마다 원본 JSON을 보존한 T 목록으로 해석한다.
func decodeArray[T any, P rawSetter[T]](client *Client, response commonexchange.Response, operation commonexchange.OperationKind) ([]T, error) {
	var rawItems []json.RawMessage
	if err := client.decode(response, operation, &rawItems); err != nil {
		return nil, err
	}
	items := make([]T, len(rawItems))
	for index, raw := range rawItems {
		if err := json.Unmarshal(raw, &items[index]); err != nil {
			return nil, client.decodeError(response, operation, err)
		}
		P(&items[index]).setRaw(cloneBytes(raw))
	}
	return items, nil
}

// validatePositiveDecimal은 값이 0보다 큰 decimal 문자열인지 확인한다. required가 false이면 빈 값을 허용한다.
func validatePositiveDecimal(name, value string, required bool) error {
	if value == "" {
		if required {
			return validationError("%s is required", name)
		}
		return nil
	}
	if !decimalPattern.MatchString(value) || strings.Trim(value, "0.") == "" {
		return validationError("%s must be a positive decimal string", name)
	}
	return nil
}

func validPriceMatch(value string) bool {
	switch value {
	case "OPPONENT", "OPPONENT_5", "OPPONENT_10", "OPPONENT_20", "QUEUE", "QUEUE_5", "QUEUE_10", "QUEUE_20":
		return true
	default:
		return false
	}
}

func validateBatchSize(name string, size, maximum int) error {
	if size < 1 || size > maximum {
		return validationError("%s must contain between 1 and %d items", name, maximum)
	}
	return nil
}

// batchOrdersParam은 주문별 파라미터를 공식 문서가 요구하는 문자열 값 JSON 배열로 직렬화한다.
func batchOrdersParam(items []url.Values) (string, error) {
	list := make([]map[string]string, len(items))
	for index, values := range items {
		item := make(map[string]string, len(values))
		for key, entries := range values {
			item[key] = entries[0]
		}
		list[index] = item
	}
	encoded, err := json.Marshal(list)
	if err != nil {
		return "", fmt.Errorf("encode Binance USD-M batch orders: %w", err)
	}
	return string(encoded), nil
}

// TestOrder는 주문을 매칭 엔진에 제출하지 않고 요청 형식과 계정 조건만 검증한다.
// 응답에 포함된 필드만 Order에 채워진다. POST /fapi/v1/order/test이며 credential.PermissionTrade가 필요하다.
// 공식 문서에 별도 weight가 없고 주문이 제출되지 않으므로 제한을 차감하지 않는다.
func (client *Client) TestOrder(ctx context.Context, request PlaceOrderRequest, options ...trade.RequestOption) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodPost, "/fapi/v1/order/test", request.values(), 0, 0, credential.PermissionTrade, commonexchange.OperationMutation, options...)
	if err != nil {
		return Order{}, err
	}
	return client.decodeOrder(response, commonexchange.OperationMutation)
}

// OpenOrder는 현재 미체결 상태인 주문 한 건을 조회한다. 이미 체결되었거나 취소된 주문은 오류로 반환된다.
// GET /fapi/v1/openOrder이며 credential.PermissionRead가 필요하고 IP weight는 1이다.
func (client *Client) OpenOrder(ctx context.Context, request OrderInfoRequest, options ...trade.RequestOption) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/openOrder", request.values(), 1, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return Order{}, err
	}
	return client.decodeOrder(response, commonexchange.OperationRead)
}

// ModifyOrderRequest는 미체결 LIMIT 주문의 수량·가격 정정 조건이다.
// 주문은 OrderID 또는 ClientOrderID 중 정확히 하나로 지정하고, 가격은 Price 또는 PriceMatch 중 정확히 하나로 지정한다.
// ModifyID는 서버가 그대로 돌려주는 사용자 정의 정정 식별자이며 유일성은 검증되지 않는다.
// ReduceOnly는 최소 주문 금액 검증을 건너뛰는 데만 쓰이고 원 주문의 reduceOnly 속성은 바뀌지 않는다.
type ModifyOrderRequest struct {
	Symbol        string
	OrderID       *int64
	ClientOrderID string
	Side          Side
	Quantity      string
	Price         string
	PriceMatch    string
	ModifyID      *int64
	ReduceOnly    bool
}

// ModifiedOrder는 정정된 주문 상태다. 요청에 ModifyID를 보냈을 때만 ModifyID가 채워진다.
type ModifiedOrder struct {
	Order
	ModifyID int64 `json:"modifyId"`
}

func (item *ModifiedOrder) setRaw(raw json.RawMessage) { item.Raw = raw }

func (request ModifyOrderRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if (request.OrderID == nil) == (request.ClientOrderID == "") {
		return validationError("exactly one order identity is required")
	}
	if request.OrderID != nil && *request.OrderID <= 0 {
		return validationError("orderId must be positive")
	}
	if strings.TrimSpace(request.ClientOrderID) != request.ClientOrderID {
		return validationError("client order ID cannot have surrounding whitespace")
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("side must be BUY or SELL")
	}
	if err := validatePositiveDecimal("quantity", request.Quantity, true); err != nil {
		return err
	}
	if (request.Price == "") == (request.PriceMatch == "") {
		return validationError("exactly one of price or priceMatch is required")
	}
	if err := validatePositiveDecimal("price", request.Price, false); err != nil {
		return err
	}
	if request.PriceMatch != "" && !validPriceMatch(request.PriceMatch) {
		return validationError("unsupported priceMatch %q", request.PriceMatch)
	}
	if request.ModifyID != nil && *request.ModifyID < 0 {
		return validationError("modifyId cannot be negative")
	}
	return nil
}

func (request ModifyOrderRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}, "side": {string(request.Side)}, "quantity": {request.Quantity}}
	if request.OrderID != nil {
		values.Set("orderId", strconv.FormatInt(*request.OrderID, 10))
	}
	set(values, "origClientOrderId", request.ClientOrderID)
	set(values, "price", request.Price)
	set(values, "priceMatch", request.PriceMatch)
	if request.ModifyID != nil {
		values.Set("modifyId", strconv.FormatInt(*request.ModifyID, 10))
	}
	if request.ReduceOnly {
		values.Set("reduceOnly", "true")
	}
	return values
}

// ModifyOrder는 미체결 LIMIT 주문의 수량과 가격을 정정한다. 정정된 주문은 매칭 큐에서 재배치된다.
// PUT /fapi/v1/order이며 credential.PermissionTrade가 필요하다. 주문 count 1을 차감하고 IP weight는 0이다.
// 불명확한 전송 결과는 UNKNOWN_EXECUTION_STATE로 반환하며 자동 재시도하지 않는다.
func (client *Client) ModifyOrder(ctx context.Context, request ModifyOrderRequest, options ...trade.RequestOption) (ModifiedOrder, error) {
	if err := request.validate(); err != nil {
		return ModifiedOrder{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodPut, "/fapi/v1/order", request.values(), 0, 1, credential.PermissionTrade, commonexchange.OperationMutation, options...)
	if err != nil {
		return ModifiedOrder{}, err
	}
	return decodeObject[ModifiedOrder](client, response, commonexchange.OperationMutation)
}

// BatchOrderResult는 일괄 주문·정정·취소 응답 항목 한 건이다.
// 항목마다 성공 여부가 달라 거부된 항목은 주문 필드 없이 음수 Code와 Message만 채워진다.
type BatchOrderResult struct {
	Order
	ModifyID int64  `json:"modifyId"`
	Code     int    `json:"code"`
	Message  string `json:"msg"`
}

func (item *BatchOrderResult) setRaw(raw json.RawMessage) { item.Raw = raw }

// Failed는 이 항목이 거래소에서 거부되었는지 알려준다.
func (item BatchOrderResult) Failed() bool { return item.Code < 0 }

// PlaceBatchOrdersRequest는 한 번에 최대 5건을 보내는 일괄 신규 주문이다.
// 각 주문의 검증과 파라미터는 PlaceOrder와 같다.
type PlaceBatchOrdersRequest struct{ Orders []PlaceOrderRequest }

func (request PlaceBatchOrdersRequest) validate() error {
	if err := validateBatchSize("batch orders", len(request.Orders), maxBatchOrders); err != nil {
		return err
	}
	for index, order := range request.Orders {
		if err := order.validate(); err != nil {
			return fmt.Errorf("batch order %d: %w", index, err)
		}
	}
	return nil
}

func (request PlaceBatchOrdersRequest) values() (url.Values, error) {
	items := make([]url.Values, len(request.Orders))
	for index, order := range request.Orders {
		items[index] = order.values()
	}
	param, err := batchOrdersParam(items)
	if err != nil {
		return nil, err
	}
	return url.Values{"batchOrders": {param}}, nil
}

// PlaceBatchOrders는 최대 5건의 신규 주문을 한 번에 보낸다. 주문은 동시에 처리되어 체결 순서가 보장되지 않는다.
// POST /fapi/v1/batchOrders이며 credential.PermissionTrade가 필요하다. 주문 count는 10초 창 5, 1분 창 1이고 IP weight는 5다.
// 응답 목록은 요청 순서와 같고 항목별로 성공 또는 거부(BatchOrderResult.Failed)가 갈린다.
// 불명확한 전송 결과는 UNKNOWN_EXECUTION_STATE로 반환하며 자동 재시도하지 않는다.
func (client *Client) PlaceBatchOrders(ctx context.Context, request PlaceBatchOrdersRequest, options ...trade.RequestOption) ([]BatchOrderResult, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	values, err := request.values()
	if err != nil {
		return nil, err
	}
	response, err := client.executeSignedCost(ctx, http.MethodPost, "/fapi/v1/batchOrders", values, 5, batchOrderCost, credential.PermissionTrade, commonexchange.OperationMutation, options...)
	if err != nil {
		return nil, err
	}
	return decodeArray[BatchOrderResult](client, response, commonexchange.OperationMutation)
}

// ModifyBatchOrdersRequest는 한 번에 최대 5건을 보내는 일괄 주문 정정이다.
// 각 정정의 검증과 파라미터는 ModifyOrder와 같지만 일괄 정정은 ReduceOnly를 지원하지 않는다.
type ModifyBatchOrdersRequest struct{ Orders []ModifyOrderRequest }

func (request ModifyBatchOrdersRequest) validate() error {
	if err := validateBatchSize("batch modifications", len(request.Orders), maxBatchOrders); err != nil {
		return err
	}
	for index, order := range request.Orders {
		if order.ReduceOnly {
			return fmt.Errorf("batch modification %d: %w", index, validationError("reduceOnly is not supported by batch modification"))
		}
		if err := order.validate(); err != nil {
			return fmt.Errorf("batch modification %d: %w", index, err)
		}
	}
	return nil
}

func (request ModifyBatchOrdersRequest) values() (url.Values, error) {
	items := make([]url.Values, len(request.Orders))
	for index, order := range request.Orders {
		items[index] = order.values()
	}
	param, err := batchOrdersParam(items)
	if err != nil {
		return nil, err
	}
	return url.Values{"batchOrders": {param}}, nil
}

// ModifyBatchOrders는 최대 5건의 미체결 LIMIT 주문을 한 번에 정정한다.
// PUT /fapi/v1/batchOrders이며 credential.PermissionTrade가 필요하다. 주문 count는 10초 창 5, 1분 창 1이고 IP weight는 5다.
// 주문 하나는 10,000회 미만만 정정할 수 있고 응답 목록은 요청 순서와 같다.
// 불명확한 전송 결과는 UNKNOWN_EXECUTION_STATE로 반환하며 자동 재시도하지 않는다.
func (client *Client) ModifyBatchOrders(ctx context.Context, request ModifyBatchOrdersRequest, options ...trade.RequestOption) ([]BatchOrderResult, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	values, err := request.values()
	if err != nil {
		return nil, err
	}
	response, err := client.executeSignedCost(ctx, http.MethodPut, "/fapi/v1/batchOrders", values, 5, batchOrderCost, credential.PermissionTrade, commonexchange.OperationMutation, options...)
	if err != nil {
		return nil, err
	}
	return decodeArray[BatchOrderResult](client, response, commonexchange.OperationMutation)
}

// CancelBatchOrdersRequest는 한 계약의 주문 최대 10건을 한 번에 취소하는 조건이다.
// OrderIDs 또는 ClientOrderIDs 중 정확히 하나만 지정한다.
type CancelBatchOrdersRequest struct {
	Symbol         string
	OrderIDs       []int64
	ClientOrderIDs []string
}

func (request CancelBatchOrdersRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if (len(request.OrderIDs) == 0) == (len(request.ClientOrderIDs) == 0) {
		return validationError("exactly one of orderIdList or origClientOrderIdList is required")
	}
	if err := validateBatchSize("batch cancel identities", len(request.OrderIDs)+len(request.ClientOrderIDs), maxBatchCancelIDs); err != nil {
		return err
	}
	for _, orderID := range request.OrderIDs {
		if orderID <= 0 {
			return validationError("orderId must be positive")
		}
	}
	for _, clientOrderID := range request.ClientOrderIDs {
		if clientOrderID == "" || strings.TrimSpace(clientOrderID) != clientOrderID {
			return validationError("client order ID is required without surrounding whitespace")
		}
	}
	return nil
}

func (request CancelBatchOrdersRequest) values() (url.Values, error) {
	values := url.Values{"symbol": {request.Symbol}}
	if len(request.OrderIDs) > 0 {
		encoded, err := json.Marshal(request.OrderIDs)
		if err != nil {
			return nil, fmt.Errorf("encode Binance USD-M order ID list: %w", err)
		}
		values.Set("orderIdList", string(encoded))
	}
	if len(request.ClientOrderIDs) > 0 {
		encoded, err := json.Marshal(request.ClientOrderIDs)
		if err != nil {
			return nil, fmt.Errorf("encode Binance USD-M client order ID list: %w", err)
		}
		values.Set("origClientOrderIdList", string(encoded))
	}
	return values, nil
}

// CancelBatchOrders는 한 계약의 미체결 주문을 최대 10건까지 한 번에 취소한다.
// DELETE /fapi/v1/batchOrders이며 credential.PermissionTrade가 필요하고 IP weight는 1이다.
// 응답 목록은 요청 식별자 순서와 같고 항목별로 취소 성공 또는 거부(BatchOrderResult.Failed)가 갈린다.
func (client *Client) CancelBatchOrders(ctx context.Context, request CancelBatchOrdersRequest, options ...trade.RequestOption) ([]BatchOrderResult, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	values, err := request.values()
	if err != nil {
		return nil, err
	}
	response, err := client.executeSigned(ctx, http.MethodDelete, "/fapi/v1/batchOrders", values, 1, 0, credential.PermissionTrade, commonexchange.OperationMutation, options...)
	if err != nil {
		return nil, err
	}
	return decodeArray[BatchOrderResult](client, response, commonexchange.OperationMutation)
}

// CancelAllOpenOrdersRequest는 미체결 주문을 모두 취소할 계약이다.
type CancelAllOpenOrdersRequest struct{ Symbol string }

// CancelAllOpenOrders는 한 계약의 미체결 주문을 모두 취소한다.
// DELETE /fapi/v1/allOpenOrders이며 credential.PermissionTrade가 필요하고 IP weight는 1이다.
// Algo 조건부 주문은 취소되지 않으므로 CancelAllAlgoOpenOrders를 따로 호출한다.
func (client *Client) CancelAllOpenOrders(ctx context.Context, request CancelAllOpenOrdersRequest, options ...trade.RequestOption) (AckResult, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return AckResult{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodDelete, "/fapi/v1/allOpenOrders", url.Values{"symbol": {request.Symbol}}, 1, 0, credential.PermissionTrade, commonexchange.OperationMutation, options...)
	if err != nil {
		return AckResult{}, err
	}
	return decodeObject[AckResult](client, response, commonexchange.OperationMutation)
}

// CancelAllAlgoOpenOrdersRequest는 미체결 Algo 조건부 주문을 모두 취소할 계약이다.
type CancelAllAlgoOpenOrdersRequest struct{ Symbol string }

// CancelAllAlgoOpenOrders는 한 계약의 미체결 Algo 조건부 주문(TP/SL, 트레일링 스톱 포함)을 모두 취소한다.
// DELETE /fapi/v1/algoOpenOrders이며 credential.PermissionTrade가 필요하고 IP weight는 1이다.
func (client *Client) CancelAllAlgoOpenOrders(ctx context.Context, request CancelAllAlgoOpenOrdersRequest, options ...trade.RequestOption) (AckResult, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return AckResult{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodDelete, "/fapi/v1/algoOpenOrders", url.Values{"symbol": {request.Symbol}}, 1, 0, credential.PermissionTrade, commonexchange.OperationMutation, options...)
	if err != nil {
		return AckResult{}, err
	}
	return decodeObject[AckResult](client, response, commonexchange.OperationMutation)
}

// CountdownCancelAllRequest는 카운트다운 자동 전체 취소 조건이다.
// Countdown은 밀리초 단위의 정수 배수여야 하며 0이면 해당 계약의 카운트다운을 중지한다.
type CountdownCancelAllRequest struct {
	Symbol    string
	Countdown time.Duration
}

// CountdownCancelAllResult는 설정된 카운트다운이다. CountdownTime은 밀리초이며 숫자 또는 숫자 문자열로 온다.
type CountdownCancelAllResult struct {
	Symbol        string          `json:"symbol"`
	CountdownTime json.Number     `json:"countdownTime"`
	Raw           json.RawMessage `json:"-"`
}

func (item *CountdownCancelAllResult) setRaw(raw json.RawMessage) { item.Raw = raw }

func (request CountdownCancelAllRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Countdown < 0 || request.Countdown%time.Millisecond != 0 {
		return validationError("countdown must be a non-negative whole number of milliseconds")
	}
	return nil
}

func (request CountdownCancelAllRequest) values() url.Values {
	return url.Values{"symbol": {request.Symbol}, "countdownTime": {strconv.FormatInt(request.Countdown.Milliseconds(), 10)}}
}

// CountdownCancelAll은 지정한 카운트다운이 끝나면 해당 계약의 미체결 주문을 모두 취소하도록 예약한다.
// 같은 계약에 다시 호출하면 기존 카운트다운이 새 값으로 교체되므로 하트비트처럼 반복 호출해야 하며,
// 서버는 약 10밀리초마다 카운트다운을 확인하므로 지나치게 짧은 값은 피해야 한다. Countdown이 0이면 중지한다.
// POST /fapi/v1/countdownCancelAll이며 credential.PermissionTrade가 필요하고 IP weight는 10이다.
// 자동 재시도하지 않는다.
func (client *Client) CountdownCancelAll(ctx context.Context, request CountdownCancelAllRequest, options ...trade.RequestOption) (CountdownCancelAllResult, error) {
	if err := request.validate(); err != nil {
		return CountdownCancelAllResult{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodPost, "/fapi/v1/countdownCancelAll", request.values(), 10, 0, credential.PermissionTrade, commonexchange.OperationMutation, options...)
	if err != nil {
		return CountdownCancelAllResult{}, err
	}
	return decodeObject[CountdownCancelAllResult](client, response, commonexchange.OperationMutation)
}

// OrderAmendmentChange는 정정 전후 값이다.
type OrderAmendmentChange struct {
	Before string `json:"before"`
	After  string `json:"after"`
}

// OrderAmendmentDetail은 정정된 가격·수량과 해당 주문의 누적 정정 횟수다.
type OrderAmendmentDetail struct {
	Price            OrderAmendmentChange `json:"price"`
	OriginalQuantity OrderAmendmentChange `json:"origQty"`
	Count            int64                `json:"count"`
	ModifyID         int64                `json:"modifyId"`
}

// OrderAmendment는 주문 정정 이력 한 건이다.
type OrderAmendment struct {
	AmendmentID   int64                `json:"amendmentId"`
	Symbol        string               `json:"symbol"`
	Pair          string               `json:"pair"`
	OrderID       int64                `json:"orderId"`
	ClientOrderID string               `json:"clientOrderId"`
	Time          int64                `json:"time"`
	Amendment     OrderAmendmentDetail `json:"amendment"`
	Raw           json.RawMessage      `json:"-"`
}

func (item *OrderAmendment) setRaw(raw json.RawMessage) { item.Raw = raw }

// OrderAmendmentsRequest는 주문 정정 이력 조회 조건이다.
// 주문은 OrderID 또는 ClientOrderID 중 정확히 하나로 지정하고 Limit 최대는 100, 서버 기본값은 50이다.
type OrderAmendmentsRequest struct {
	Symbol        string
	OrderID       *int64
	ClientOrderID string
	StartTime     *time.Time
	EndTime       *time.Time
	Limit         int
}

func (request OrderAmendmentsRequest) validate() error {
	if err := (OrderInfoRequest{Symbol: request.Symbol, OrderID: request.OrderID, ClientOrderID: request.ClientOrderID}).validate(); err != nil {
		return err
	}
	if err := validateLimit("order amendment", request.Limit, 100); err != nil {
		return err
	}
	return validateTimeRange(request.StartTime, request.EndTime)
}

func (request OrderAmendmentsRequest) values() url.Values {
	values := OrderInfoRequest{Symbol: request.Symbol, OrderID: request.OrderID, ClientOrderID: request.ClientOrderID}.values()
	setTimeRange(values, request.StartTime, request.EndTime)
	setLimit(values, request.Limit)
	return values
}

// OrderAmendments는 주문 정정 이력을 조회한다. 3개월보다 오래된 정정 이력은 조회되지 않는다.
// GET /fapi/v1/orderAmendment이며 credential.PermissionRead가 필요하고 IP weight는 1이다.
func (client *Client) OrderAmendments(ctx context.Context, request OrderAmendmentsRequest, options ...trade.RequestOption) ([]OrderAmendment, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/orderAmendment", request.values(), 1, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return nil, err
	}
	return decodeArray[OrderAmendment](client, response, commonexchange.OperationRead)
}

// ChangeLeverageRequest는 계약별 초기 레버리지 변경 조건이다.
// 허용 최대값은 계약마다 다르며 LeverageBrackets 첫 구간의 InitialLeverage와 같다.
type ChangeLeverageRequest struct {
	Symbol   string
	Leverage int
}

// LeverageResult는 변경된 초기 레버리지와 그 레버리지에서 허용되는 최대 명목가다.
type LeverageResult struct {
	Symbol           string          `json:"symbol"`
	Leverage         int             `json:"leverage"`
	MaxNotionalValue string          `json:"maxNotionalValue"`
	Raw              json.RawMessage `json:"-"`
}

func (item *LeverageResult) setRaw(raw json.RawMessage) { item.Raw = raw }

func (request ChangeLeverageRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Leverage < 1 {
		return validationError("leverage must be positive")
	}
	return nil
}

func (request ChangeLeverageRequest) values() url.Values {
	return url.Values{"symbol": {request.Symbol}, "leverage": {strconv.Itoa(request.Leverage)}}
}

// ChangeLeverage는 계약별 초기 레버리지를 변경한다.
// POST /fapi/v1/leverage이며 credential.PermissionTrade가 필요하고 IP weight는 1이다.
func (client *Client) ChangeLeverage(ctx context.Context, request ChangeLeverageRequest, options ...trade.RequestOption) (LeverageResult, error) {
	if err := request.validate(); err != nil {
		return LeverageResult{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodPost, "/fapi/v1/leverage", request.values(), 1, 0, credential.PermissionTrade, commonexchange.OperationMutation, options...)
	if err != nil {
		return LeverageResult{}, err
	}
	return decodeObject[LeverageResult](client, response, commonexchange.OperationMutation)
}

// ChangePositionModeRequest는 전체 계약에 적용되는 포지션 모드 변경 조건이다.
// DualSidePosition이 true이면 헤지 모드, false이면 단방향 모드다.
type ChangePositionModeRequest struct{ DualSidePosition bool }

// ChangePositionMode는 모든 계약의 포지션 모드를 헤지 또는 단방향으로 바꾼다.
// 미체결 주문이나 포지션이 있으면 거래소가 거부한다.
// POST /fapi/v1/positionSide/dual이며 credential.PermissionTrade가 필요하고 IP weight는 1이다.
func (client *Client) ChangePositionMode(ctx context.Context, request ChangePositionModeRequest, options ...trade.RequestOption) (AckResult, error) {
	values := url.Values{"dualSidePosition": {strconv.FormatBool(request.DualSidePosition)}}
	response, err := client.executeSigned(ctx, http.MethodPost, "/fapi/v1/positionSide/dual", values, 1, 0, credential.PermissionTrade, commonexchange.OperationMutation, options...)
	if err != nil {
		return AckResult{}, err
	}
	return decodeObject[AckResult](client, response, commonexchange.OperationMutation)
}

// ChangeMultiAssetsModeRequest는 전체 계약에 적용되는 멀티에셋 모드 변경 조건이다.
// MultiAssetsMargin이 true이면 멀티에셋 모드, false이면 단일에셋 모드다.
type ChangeMultiAssetsModeRequest struct{ MultiAssetsMargin bool }

// ChangeMultiAssetsMode는 모든 계약의 마진 모드를 멀티에셋 또는 단일에셋으로 바꾼다.
// POST /fapi/v1/multiAssetsMargin이며 credential.PermissionTrade가 필요하고 IP weight는 1이다.
func (client *Client) ChangeMultiAssetsMode(ctx context.Context, request ChangeMultiAssetsModeRequest, options ...trade.RequestOption) (AckResult, error) {
	values := url.Values{"multiAssetsMargin": {strconv.FormatBool(request.MultiAssetsMargin)}}
	response, err := client.executeSigned(ctx, http.MethodPost, "/fapi/v1/multiAssetsMargin", values, 1, 0, credential.PermissionTrade, commonexchange.OperationMutation, options...)
	if err != nil {
		return AckResult{}, err
	}
	return decodeObject[AckResult](client, response, commonexchange.OperationMutation)
}

// PositionMarginChange는 격리 포지션 마진 조정 방향이다.
type PositionMarginChange int

const (
	PositionMarginAdd    PositionMarginChange = 1
	PositionMarginReduce PositionMarginChange = 2
)

// ModifyPositionMarginRequest는 격리 포지션 마진 증감 조건이다.
// 헤지 모드에서는 PositionSide를 LONG 또는 SHORT로 지정한다.
type ModifyPositionMarginRequest struct {
	Symbol       string
	PositionSide PositionSide
	Amount       string
	Type         PositionMarginChange
}

// PositionMarginResult는 격리 포지션 마진 조정 결과다. Amount는 JSON 숫자 표기를 보존한다.
type PositionMarginResult struct {
	Amount  json.Number          `json:"amount"`
	Code    StatusCode           `json:"code"`
	Message string               `json:"msg"`
	Type    PositionMarginChange `json:"type"`
	Raw     json.RawMessage      `json:"-"`
}

func (item *PositionMarginResult) setRaw(raw json.RawMessage) { item.Raw = raw }

func (request ModifyPositionMarginRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.PositionSide != "" && request.PositionSide != PositionSideBoth && request.PositionSide != PositionSideLong && request.PositionSide != PositionSideShort {
		return validationError("invalid positionSide")
	}
	if err := validatePositiveDecimal("amount", request.Amount, true); err != nil {
		return err
	}
	if request.Type != PositionMarginAdd && request.Type != PositionMarginReduce {
		return validationError("type must be 1 (add) or 2 (reduce)")
	}
	return nil
}

func (request ModifyPositionMarginRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}, "amount": {request.Amount}, "type": {strconv.Itoa(int(request.Type))}}
	set(values, "positionSide", string(request.PositionSide))
	return values
}

// ModifyPositionMargin은 격리 마진 포지션의 마진을 늘리거나 줄인다. 교차 마진 계약에는 쓸 수 없다.
// POST /fapi/v1/positionMargin이며 credential.PermissionTrade가 필요하고 IP weight는 1이다.
func (client *Client) ModifyPositionMargin(ctx context.Context, request ModifyPositionMarginRequest, options ...trade.RequestOption) (PositionMarginResult, error) {
	if err := request.validate(); err != nil {
		return PositionMarginResult{}, err
	}
	response, err := client.executeSigned(ctx, http.MethodPost, "/fapi/v1/positionMargin", request.values(), 1, 0, credential.PermissionTrade, commonexchange.OperationMutation, options...)
	if err != nil {
		return PositionMarginResult{}, err
	}
	return decodeObject[PositionMarginResult](client, response, commonexchange.OperationMutation)
}

// PositionMarginRecord는 격리 포지션 마진 변경 이력 한 건이다.
type PositionMarginRecord struct {
	Symbol       string               `json:"symbol"`
	Type         PositionMarginChange `json:"type"`
	DeltaType    string               `json:"deltaType"`
	Amount       string               `json:"amount"`
	Asset        string               `json:"asset"`
	Time         int64                `json:"time"`
	PositionSide PositionSide         `json:"positionSide"`
	Raw          json.RawMessage      `json:"-"`
}

func (item *PositionMarginRecord) setRaw(raw json.RawMessage) { item.Raw = raw }

// PositionMarginHistoryRequest는 격리 포지션 마진 변경 이력 조회 조건이다.
// Type을 비우면 증가·감소를 모두 조회하고, 최근 30일 이력만 조회되며 두 시각의 간격도 30일을 넘을 수 없다.
type PositionMarginHistoryRequest struct {
	Symbol    string
	Type      PositionMarginChange
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
}

func (request PositionMarginHistoryRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Type != 0 && request.Type != PositionMarginAdd && request.Type != PositionMarginReduce {
		return validationError("type must be 1 (add) or 2 (reduce)")
	}
	if request.Limit < 0 {
		return validationError("position margin history limit cannot be negative")
	}
	if err := validateTimeRange(request.StartTime, request.EndTime); err != nil {
		return err
	}
	if request.StartTime != nil && request.EndTime != nil && request.EndTime.Sub(*request.StartTime) > 30*24*time.Hour {
		return validationError("position margin history range cannot exceed 30 days")
	}
	return nil
}

func (request PositionMarginHistoryRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	if request.Type != 0 {
		values.Set("type", strconv.Itoa(int(request.Type)))
	}
	setTimeRange(values, request.StartTime, request.EndTime)
	setLimit(values, request.Limit)
	return values
}

// PositionMarginHistory는 격리 포지션의 마진 증감 이력을 조회한다.
// GET /fapi/v1/positionMargin/history이며 IP weight는 1이다. 공식 보안 등급은 TRADE이지만 조회 전용이므로
// SDK는 credential.PermissionRead로 검사하고 OperationRead로 실행한다. API Key에는 Futures 권한이 있어야 한다.
func (client *Client) PositionMarginHistory(ctx context.Context, request PositionMarginHistoryRequest, options ...trade.RequestOption) ([]PositionMarginRecord, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/positionMargin/history", request.values(), 1, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return nil, err
	}
	return decodeArray[PositionMarginRecord](client, response, commonexchange.OperationRead)
}

// UserTrade는 계정 체결 한 건이다.
type UserTrade struct {
	ID              int64           `json:"id"`
	OrderID         int64           `json:"orderId"`
	Symbol          string          `json:"symbol"`
	Side            Side            `json:"side"`
	PositionSide    PositionSide    `json:"positionSide"`
	Price           string          `json:"price"`
	Quantity        string          `json:"qty"`
	QuoteValue      string          `json:"quoteQty"`
	RealizedPnl     string          `json:"realizedPnl"`
	Commission      string          `json:"commission"`
	CommissionAsset string          `json:"commissionAsset"`
	MarginAsset     string          `json:"marginAsset"`
	Buyer           bool            `json:"buyer"`
	Maker           bool            `json:"maker"`
	Time            int64           `json:"time"`
	Raw             json.RawMessage `json:"-"`
}

func (item *UserTrade) setRaw(raw json.RawMessage) { item.Raw = raw }

// UserTradesRequest는 계정 체결 내역 조회 조건이다.
// FromID는 StartTime·EndTime과 함께 보낼 수 없고, 시각을 모두 생략하면 최근 7일을 조회하며 두 시각의 간격은 7일을 넘을 수 없다.
// 최근 3개월 체결만 조회되고 Limit 최대는 1,000, 서버 기본값은 500이다.
type UserTradesRequest struct {
	Symbol    string
	OrderID   *int64
	FromID    *int64
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
}

func (request UserTradesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.OrderID != nil && *request.OrderID <= 0 {
		return validationError("orderId must be positive")
	}
	if request.FromID != nil {
		if *request.FromID < 0 {
			return validationError("fromId cannot be negative")
		}
		if request.StartTime != nil || request.EndTime != nil {
			return validationError("fromId cannot be combined with startTime or endTime")
		}
	}
	if err := validateLimit("user trade", request.Limit, 1000); err != nil {
		return err
	}
	if err := validateTimeRange(request.StartTime, request.EndTime); err != nil {
		return err
	}
	if request.StartTime != nil && request.EndTime != nil && request.EndTime.Sub(*request.StartTime) > 7*24*time.Hour {
		return validationError("user trade range cannot exceed seven days")
	}
	return nil
}

func (request UserTradesRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	if request.OrderID != nil {
		values.Set("orderId", strconv.FormatInt(*request.OrderID, 10))
	}
	if request.FromID != nil {
		values.Set("fromId", strconv.FormatInt(*request.FromID, 10))
	}
	setTimeRange(values, request.StartTime, request.EndTime)
	setLimit(values, request.Limit)
	return values
}

// UserTrades는 단일 계약의 계정 체결 내역을 조회한다.
// GET /fapi/v1/userTrades이며 credential.PermissionRead가 필요하고 IP weight는 5다.
func (client *Client) UserTrades(ctx context.Context, request UserTradesRequest, options ...trade.RequestOption) ([]UserTrade, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/userTrades", request.values(), 5, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return nil, err
	}
	return decodeArray[UserTrade](client, response, commonexchange.OperationRead)
}

// AutoCloseType은 강제청산 주문 조회 구분이다.
type AutoCloseType string

const (
	AutoCloseLiquidation AutoCloseType = "LIQUIDATION"
	AutoCloseADL         AutoCloseType = "ADL"
)

// ForceOrdersRequest는 강제청산·ADL 주문 조회 조건이다.
// Symbol과 AutoCloseType을 비우면 전체 계약과 두 유형을 조회하고, StartTime을 생략하면 EndTime 이전 7일을 조회한다.
// Limit 최대는 100, 서버 기본값은 50이다.
type ForceOrdersRequest struct {
	Symbol        string
	AutoCloseType AutoCloseType
	StartTime     *time.Time
	EndTime       *time.Time
	Limit         int
}

func (request ForceOrdersRequest) validate() error {
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return err
	}
	if request.AutoCloseType != "" && request.AutoCloseType != AutoCloseLiquidation && request.AutoCloseType != AutoCloseADL {
		return validationError("autoCloseType must be LIQUIDATION or ADL")
	}
	if err := validateLimit("force order", request.Limit, 100); err != nil {
		return err
	}
	return validateTimeRange(request.StartTime, request.EndTime)
}

func (request ForceOrdersRequest) values() url.Values {
	values := optionalSymbolValues(request.Symbol)
	set(values, "autoCloseType", string(request.AutoCloseType))
	setTimeRange(values, request.StartTime, request.EndTime)
	setLimit(values, request.Limit)
	return values
}

// ForceOrders는 계정의 강제청산·ADL 주문 내역을 조회한다.
// GET /fapi/v1/forceOrders이며 credential.PermissionRead가 필요하고 IP weight는 Symbol 지정 20, 생략 50이다.
func (client *Client) ForceOrders(ctx context.Context, request ForceOrdersRequest, options ...trade.RequestOption) ([]Order, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/forceOrders", request.values(), symbolWeight(request.Symbol, 20, 50), 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return nil, err
	}
	return client.decodeOrders(response)
}

// ADLQuantileRequest는 ADL 분위 조회 범위다. Symbol을 비우면 포지션이 있는 전체 계약을 조회한다.
type ADLQuantileRequest struct{ Symbol string }

// ADLQuantile은 계약의 포지션 방향별 ADL 분위다.
// Quantile 키는 LONG, SHORT, BOTH이고 교차 헤지 모드에서는 BOTH 대신 HEDGE가 오며 이 값은 표시일 뿐 무시한다.
// 값 0~4는 ADL 가능성이 낮은 쪽에서 높은 쪽으로 가는 대기열 위치이며 30초마다 갱신된다.
type ADLQuantile struct {
	Symbol   string               `json:"symbol"`
	Quantile map[PositionSide]int `json:"adlQuantile"`
	Raw      json.RawMessage      `json:"-"`
}

func (item *ADLQuantile) setRaw(raw json.RawMessage) { item.Raw = raw }

// ADLQuantiles는 포지션별 ADL 분위 추정값을 조회한다.
// GET /fapi/v1/adlQuantile이며 credential.PermissionRead가 필요하고 IP weight는 5다.
func (client *Client) ADLQuantiles(ctx context.Context, request ADLQuantileRequest, options ...trade.RequestOption) ([]ADLQuantile, error) {
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return nil, err
	}
	response, err := client.executeSigned(ctx, http.MethodGet, "/fapi/v1/adlQuantile", optionalSymbolValues(request.Symbol), 5, 0, credential.PermissionRead, commonexchange.OperationRead, options...)
	if err != nil {
		return nil, err
	}
	return decodeArray[ADLQuantile](client, response, commonexchange.OperationRead)
}

// SignTradFiPerpsContract는 TradFi 무기한 계약 거래 약관에 서명한다.
// POST /fapi/v1/stock/contract이며 IP weight는 50이다. 공식 보안 등급은 USER_DATA이지만 계정 상태를 바꾸는
// 요청이므로 SDK는 credential.PermissionTrade로 검사하고 OperationMutation으로 실행하며 자동 재시도하지 않는다.
func (client *Client) SignTradFiPerpsContract(ctx context.Context, options ...trade.RequestOption) (AckResult, error) {
	response, err := client.executeSigned(ctx, http.MethodPost, "/fapi/v1/stock/contract", nil, 50, 0, credential.PermissionTrade, commonexchange.OperationMutation, options...)
	if err != nil {
		return AckResult{}, err
	}
	return decodeObject[AckResult](client, response, commonexchange.OperationMutation)
}
