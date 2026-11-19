package binance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// CommissionDiscount는 BNB로 수수료를 낼 때 적용되는 할인 설정이다.
type CommissionDiscount struct {
	EnabledForAccount bool   `json:"enabledForAccount"`
	EnabledForSymbol  bool   `json:"enabledForSymbol"`
	DiscountAsset     string `json:"discountAsset"`
	Discount          string `json:"discount"`
}

// TestOrderRequest는 주문 검증 요청이다. 주문은 매칭 엔진에 전달되지 않고 서명, recvWindow, 필터만 검증한다.
// ComputeCommissionRates를 켜면 weight 20을 사용하고 주문에 적용될 수수료율을 함께 반환한다.
type TestOrderRequest struct {
	Order                  NewOrderRequest
	ComputeCommissionRates bool
}

func (request TestOrderRequest) validate() error {
	return request.Order.validate()
}

func (request TestOrderRequest) values() url.Values {
	values := request.Order.values()
	if request.ComputeCommissionRates {
		values.Set("computeCommissionRates", "true")
	}
	return values
}

// TestOrderResult는 테스트 주문 응답이다.
// ComputeCommissionRates를 켜지 않으면 거래소가 빈 객체를 반환하므로 모든 필드가 nil이다.
// 수수료율은 Maker와 Taker만 채워진다.
type TestOrderResult struct {
	StandardCommissionForOrder *CommissionRates    `json:"standardCommissionForOrder"`
	SpecialCommissionForOrder  *CommissionRates    `json:"specialCommissionForOrder"`
	TaxCommissionForOrder      *CommissionRates    `json:"taxCommissionForOrder"`
	Discount                   *CommissionDiscount `json:"discount"`
	Raw                        json.RawMessage     `json:"-"`
}

// testOrderWeight는 order/test와 sor/order/test의 weight다. 수수료율 계산을 켜면 20이다.
func testOrderWeight(computeCommissionRates bool) int {
	if computeCommissionRates {
		return 20
	}
	return 1
}

func decodeTestOrderResult(body []byte) (TestOrderResult, error) {
	var result TestOrderResult
	if err := decodeJSON(body, &result); err != nil {
		return TestOrderResult{}, err
	}
	result.Raw = cloneBytes(body)
	return result, nil
}

// TestOrder는 주문을 만들지 않고 생성 가능 여부만 검증한다. 미체결 주문 count를 소비하지 않는다.
// 주문 엔드포인트와 같은 TRADE 권한이 필요하므로 PermissionTrade를 요구하고 자동 재시도하지 않는다.
func (client *Client) TestOrder(
	ctx context.Context,
	request TestOrderRequest,
	options ...trade.RequestOption,
) (TestOrderResult, error) {
	if err := request.validate(); err != nil {
		return TestOrderResult{}, err
	}
	body, err := client.mutateSigned(
		ctx,
		http.MethodPost,
		"/api/v3/order/test",
		request.values(),
		testOrderWeight(request.ComputeCommissionRates),
		0,
		options...,
	)
	if err != nil {
		return TestOrderResult{}, err
	}
	return decodeTestOrderResult(body)
}

// CancelOpenOrdersRequest는 한 상품의 모든 미체결 주문을 취소하는 조건이다.
type CancelOpenOrdersRequest struct {
	Symbol string
}

// CancelOpenOrdersResult는 일괄 취소된 주문과 주문 리스트다.
// 주문 리스트에 속한 주문은 개별 Orders가 아니라 해당 OrderLists의 OrderReports에 담긴다.
type CancelOpenOrdersResult struct {
	Orders     []Order
	OrderLists []OrderList
	Raw        json.RawMessage
}

// CancelOpenOrders는 한 상품의 모든 미체결 주문과 주문 리스트를 취소한다.
// 취소 결과가 불명확한 전송 오류는 OpenOrders로 상태를 조정해야 한다.
func (client *Client) CancelOpenOrders(
	ctx context.Context,
	request CancelOpenOrdersRequest,
	options ...trade.RequestOption,
) (CancelOpenOrdersResult, error) {
	if err := validateSymbol(request.Symbol); err != nil {
		return CancelOpenOrdersResult{}, err
	}
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	body, err := client.mutateSigned(ctx, http.MethodDelete, "/api/v3/openOrders", values, 1, 0, options...)
	if err != nil {
		return CancelOpenOrdersResult{}, err
	}
	return decodeCancelOpenOrders(body)
}

// decodeCancelOpenOrders는 주문과 주문 리스트가 섞인 배열을 contingencyType 유무로 나눠 디코딩한다.
func decodeCancelOpenOrders(body []byte) (CancelOpenOrdersResult, error) {
	var rawItems []json.RawMessage
	if err := decodeJSON(body, &rawItems); err != nil {
		return CancelOpenOrdersResult{}, err
	}
	result := CancelOpenOrdersResult{Raw: cloneBytes(body)}
	for _, rawItem := range rawItems {
		var probe struct {
			ContingencyType ContingencyType `json:"contingencyType"`
		}
		if err := decodeJSON(rawItem, &probe); err != nil {
			return CancelOpenOrdersResult{}, err
		}
		if probe.ContingencyType != "" {
			var orderList OrderList
			if err := decodeJSON(rawItem, &orderList); err != nil {
				return CancelOpenOrdersResult{}, err
			}
			orderList.Raw = cloneBytes(rawItem)
			result.OrderLists = append(result.OrderLists, orderList)
			continue
		}
		var order Order
		if err := decodeJSON(rawItem, &order); err != nil {
			return CancelOpenOrdersResult{}, err
		}
		order.Raw = cloneBytes(rawItem)
		result.Orders = append(result.Orders, order)
	}
	return result, nil
}

// CancelReplaceMode는 취소가 실패했을 때 신규 주문을 시도할지 정한다.
type CancelReplaceMode string

const (
	CancelReplaceModeStopOnFailure CancelReplaceMode = "STOP_ON_FAILURE"
	CancelReplaceModeAllowFailure  CancelReplaceMode = "ALLOW_FAILURE"
)

// OrderRateLimitExceededMode는 미체결 주문 count를 넘었을 때 취소를 시도할지 정한다.
type OrderRateLimitExceededMode string

const (
	OrderRateLimitExceededModeDoNothing  OrderRateLimitExceededMode = "DO_NOTHING"
	OrderRateLimitExceededModeCancelOnly OrderRateLimitExceededMode = "CANCEL_ONLY"
)

// CancelReplaceOutcome은 cancelReplace에서 취소 또는 신규 주문 한쪽의 처리 결과다.
type CancelReplaceOutcome string

const (
	CancelReplaceOutcomeSuccess      CancelReplaceOutcome = "SUCCESS"
	CancelReplaceOutcomeFailure      CancelReplaceOutcome = "FAILURE"
	CancelReplaceOutcomeNotAttempted CancelReplaceOutcome = "NOT_ATTEMPTED"
)

// CancelReplaceOrderRequest는 기존 주문을 취소하고 같은 상품에 새 주문을 내는 요청이다.
// 취소할 주문은 CancelOrderID 또는 CancelOriginalClientOrderID로 지정하며,
// 새 주문의 Symbol, Side, Type과 나머지 파라미터는 NewOrder에 NewOrderRequest와 같은 규칙으로 넣는다.
type CancelReplaceOrderRequest struct {
	Mode                        CancelReplaceMode
	RateLimitExceededMode       OrderRateLimitExceededMode
	CancelOrderID               *int64
	CancelOriginalClientOrderID string
	CancelNewClientOrderID      string
	CancelRestriction           CancelRestriction
	NewOrder                    NewOrderRequest
}

func (request CancelReplaceOrderRequest) validate() error {
	if request.Mode != CancelReplaceModeStopOnFailure && request.Mode != CancelReplaceModeAllowFailure {
		return validationError("cancelReplaceMode must be STOP_ON_FAILURE or ALLOW_FAILURE")
	}
	if request.RateLimitExceededMode != "" &&
		request.RateLimitExceededMode != OrderRateLimitExceededModeDoNothing &&
		request.RateLimitExceededMode != OrderRateLimitExceededModeCancelOnly {
		return validationError("unsupported orderRateLimitExceededMode %q", request.RateLimitExceededMode)
	}
	if request.CancelOrderID == nil && request.CancelOriginalClientOrderID == "" {
		return validationError("cancelOrderId or cancelOrigClientOrderId is required")
	}
	if request.CancelOrderID != nil && *request.CancelOrderID <= 0 {
		return validationError("cancelOrderId must be positive")
	}
	if request.CancelOriginalClientOrderID != "" && !clientOrderIDPattern.MatchString(request.CancelOriginalClientOrderID) {
		return validationError("cancelOrigClientOrderId has an invalid format")
	}
	if request.CancelNewClientOrderID != "" && !clientOrderIDPattern.MatchString(request.CancelNewClientOrderID) {
		return validationError("cancelNewClientOrderId has an invalid format")
	}
	if request.CancelRestriction != "" &&
		request.CancelRestriction != CancelOnlyNew &&
		request.CancelRestriction != CancelOnlyPartiallyFilled {
		return validationError("unsupported cancel restriction %q", request.CancelRestriction)
	}
	return request.NewOrder.validate()
}

func (request CancelReplaceOrderRequest) values() url.Values {
	values := request.NewOrder.values()
	values.Set("cancelReplaceMode", string(request.Mode))
	setIfNotEmpty(values, "orderRateLimitExceededMode", string(request.RateLimitExceededMode))
	setInt64(values, "cancelOrderId", request.CancelOrderID)
	setIfNotEmpty(values, "cancelOrigClientOrderId", request.CancelOriginalClientOrderID)
	setIfNotEmpty(values, "cancelNewClientOrderId", request.CancelNewClientOrderID)
	setIfNotEmpty(values, "cancelRestrictions", string(request.CancelRestriction))
	return values
}

// CancelReplaceLeg는 cancelReplace 응답에서 취소 또는 신규 주문 한쪽의 결과다.
// 성공하면 Order가 채워지고, 실패하면 거래소 오류 Code와 Message가 채워진다.
// 시도하지 않은 쪽은 모든 필드가 비어 있다. Raw에는 해당 항목의 원본 JSON을 보존한다.
type CancelReplaceLeg struct {
	Order   *Order
	Code    int
	Message string
	Raw     json.RawMessage
}

// UnmarshalJSON은 주문 응답, 거래소 오류, null을 구분해 디코딩한다.
func (leg *CancelReplaceLeg) UnmarshalJSON(data []byte) error {
	*leg = CancelReplaceLeg{}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var probe struct {
		Code    *int   `json:"code"`
		Message string `json:"msg"`
	}
	if err := json.Unmarshal(trimmed, &probe); err != nil {
		return fmt.Errorf("decode Binance cancel-replace result: %w", err)
	}
	leg.Raw = cloneBytes(trimmed)
	if probe.Code != nil {
		leg.Code = *probe.Code
		leg.Message = probe.Message
		return nil
	}
	var order Order
	if err := json.Unmarshal(trimmed, &order); err != nil {
		return fmt.Errorf("decode Binance cancel-replace order: %w", err)
	}
	order.Raw = cloneBytes(trimmed)
	leg.Order = &order
	return nil
}

// CancelReplaceOrderResult는 cancelReplace의 취소와 신규 주문 결과다.
// 취소는 성공하고 신규 주문이 실패하는 부분 성공도 이 구조로 표현된다.
type CancelReplaceOrderResult struct {
	CancelResult     CancelReplaceOutcome `json:"cancelResult"`
	NewOrderResult   CancelReplaceOutcome `json:"newOrderResult"`
	CancelResponse   CancelReplaceLeg     `json:"cancelResponse"`
	NewOrderResponse CancelReplaceLeg     `json:"newOrderResponse"`
	Raw              json.RawMessage      `json:"-"`
}

// CancelReplaceOrder는 기존 주문을 취소하고 새 주문을 낸다.
// 거래소가 400, 409, 429 오류와 함께 단계별 결과를 돌려주면 오류와 함께 그 결과도 반환한다.
// 따라서 err가 nil이 아니어도 CancelResult와 NewOrderResult를 확인해야 하며,
// 특히 취소는 성공하고 신규 주문이 실패한 경우 기존 주문이 이미 사라졌음을 뜻한다.
func (client *Client) CancelReplaceOrder(
	ctx context.Context,
	request CancelReplaceOrderRequest,
	options ...trade.RequestOption,
) (CancelReplaceOrderResult, error) {
	if err := request.validate(); err != nil {
		return CancelReplaceOrderResult{}, err
	}
	response, _, err := client.executeSigned(
		ctx,
		http.MethodPost,
		"/api/v3/order/cancelReplace",
		request.values(),
		1,
		1,
		credential.PermissionTrade,
		commonexchange.OperationMutation,
		options...,
	)
	if err != nil {
		return CancelReplaceOrderResult{}, err
	}
	if err := client.ensureSuccess(response, commonexchange.OperationMutation); err != nil {
		if result, ok := decodeCancelReplaceFailure(response.Body); ok {
			return result, err
		}
		return CancelReplaceOrderResult{}, err
	}
	var result CancelReplaceOrderResult
	if err := decodeJSON(response.Body, &result); err != nil {
		return CancelReplaceOrderResult{}, err
	}
	result.Raw = cloneBytes(response.Body)
	return result, nil
}

// decodeCancelReplaceFailure는 오류 응답의 data에 담긴 단계별 결과를 디코딩한다.
func decodeCancelReplaceFailure(body []byte) (CancelReplaceOrderResult, bool) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Data) == 0 {
		return CancelReplaceOrderResult{}, false
	}
	var result CancelReplaceOrderResult
	if err := json.Unmarshal(envelope.Data, &result); err != nil || result.CancelResult == "" {
		return CancelReplaceOrderResult{}, false
	}
	result.Raw = cloneBytes(envelope.Data)
	return result, true
}

// AmendOrderRequest는 우선순위를 유지하면서 미체결 주문의 수량을 줄이는 요청이다.
// NewQuantity는 0보다 크고 현재 주문 수량보다 작아야 한다.
type AmendOrderRequest struct {
	Symbol                string
	OrderID               *int64
	OriginalClientOrderID string
	NewClientOrderID      string
	NewQuantity           string
}

func (request AmendOrderRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateOrderIdentity(request.OrderID, request.OriginalClientOrderID); err != nil {
		return err
	}
	if request.NewClientOrderID != "" && !clientOrderIDPattern.MatchString(request.NewClientOrderID) {
		return validationError("newClientOrderId has an invalid format")
	}
	return validatePositiveDecimal("newQty", request.NewQuantity)
}

func (request AmendOrderRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	setInt64(values, "orderId", request.OrderID)
	setIfNotEmpty(values, "origClientOrderId", request.OriginalClientOrderID)
	setIfNotEmpty(values, "newClientOrderId", request.NewClientOrderID)
	values.Set("newQty", request.NewQuantity)
	return values
}

// AmendedOrder는 수량이 정정된 주문이다. 주문 응답과 달리 수량 필드 이름이 qty, quoteOrderQty,
// cumulativeQuoteQty이므로 Order를 재사용하지 않는다. 그 밖의 조건부 필드는 AmendOrderResult.Raw에 있다.
type AmendedOrder struct {
	Symbol                  string      `json:"symbol"`
	OrderID                 int64       `json:"orderId"`
	OrderListID             int64       `json:"orderListId"`
	OriginalClientOrderID   string      `json:"origClientOrderId"`
	ClientOrderID           string      `json:"clientOrderId"`
	Price                   string      `json:"price"`
	Quantity                string      `json:"qty"`
	ExecutedQuantity        string      `json:"executedQty"`
	PreventedQuantity       string      `json:"preventedQty"`
	QuoteOrderQuantity      string      `json:"quoteOrderQty"`
	CumulativeQuoteQuantity string      `json:"cumulativeQuoteQty"`
	Status                  OrderStatus `json:"status"`
	TimeInForce             TimeInForce `json:"timeInForce"`
	Type                    OrderType   `json:"type"`
	Side                    Side        `json:"side"`
	WorkingTime             int64       `json:"workingTime"`
	SelfTradePreventionMode string      `json:"selfTradePreventionMode"`
	StopPrice               string      `json:"stopPrice"`
	IcebergQuantity         string      `json:"icebergQty"`
	ExpiryReason            string      `json:"expiryReason"`
}

// AmendOrderResult는 수량 정정 응답이다. 주문 리스트에 속한 주문이면 ListStatus가 채워진다.
type AmendOrderResult struct {
	TransactionTime int64           `json:"transactTime"`
	ExecutionID     int64           `json:"executionId"`
	AmendedOrder    AmendedOrder    `json:"amendedOrder"`
	ListStatus      *OrderList      `json:"listStatus"`
	Raw             json.RawMessage `json:"-"`
}

// AmendOrderKeepPriority는 대기열 우선순위를 유지한 채 미체결 주문의 수량을 줄인다.
// 주문 count를 소비하지 않으며 자동 재시도하지 않는다.
func (client *Client) AmendOrderKeepPriority(
	ctx context.Context,
	request AmendOrderRequest,
	options ...trade.RequestOption,
) (AmendOrderResult, error) {
	if err := request.validate(); err != nil {
		return AmendOrderResult{}, err
	}
	body, err := client.mutateSigned(
		ctx,
		http.MethodPut,
		"/api/v3/order/amend/keepPriority",
		request.values(),
		4,
		0,
		options...,
	)
	if err != nil {
		return AmendOrderResult{}, err
	}
	var result AmendOrderResult
	if err := decodeJSON(body, &result); err != nil {
		return AmendOrderResult{}, err
	}
	result.Raw = cloneBytes(body)
	return result, nil
}
