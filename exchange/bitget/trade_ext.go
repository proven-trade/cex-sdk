package bitget

import (
	"context"
	"encoding/json"
	"strconv"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

// maxBatchOrders는 일괄 요청당 최대 주문 수다.
// 공식 문서는 batch-modify-order의 20건 상한만 명시하므로 세 일괄 endpoint에 같은 상한을 적용한다.
const maxBatchOrders = 20

// 일괄 endpoint는 부분 성공을 허용한다. 공식 문서가 JS 렌더링이라 항목별 응답 필드를
// SDK 계약으로 고정하지 않고 data 필드를 json.RawMessage로 반환한다. 호출자는 항목별 결과를
// 확인해야 하며, SDK는 mutation을 자동 재시도하지 않는다.

// PlaceBatch는 같은 business line의 주문 최대 20건을 한 번에 접수한다.
// place-batch는 TP/SL을 지원하지 않으므로 TP/SL 필드가 있으면 거부한다.
// (POST /api/v3/trade/place-batch, Trade 권한)
func (client *Client) PlaceBatch(
	ctx context.Context,
	requests []PlaceOrderRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateBatchSize(len(requests)); err != nil {
		return nil, err
	}
	for _, request := range requests {
		if err := request.validate(); err != nil {
			return nil, err
		}
		if request.hasTPSL() {
			return nil, validationError("place-batch does not support take profit or stop loss")
		}
	}
	return client.mutateRaw(ctx, "/api/v3/trade/place-batch", requests, credential.PermissionTrade, options...)
}

// BatchCancelItem은 일괄 취소 대상 주문 한 건이다. OrderID와 ClientOrderID 중 하나만 지정한다.
type BatchCancelItem struct {
	OrderID       string   `json:"orderId,omitempty"`
	ClientOrderID string   `json:"clientOid,omitempty"`
	Category      Category `json:"category"`
	Symbol        string   `json:"symbol,omitempty"`
}

// CancelBatch는 주문 최대 20건을 한 번에 취소 접수한다.
// (POST /api/v3/trade/cancel-batch, Trade 권한)
func (client *Client) CancelBatch(
	ctx context.Context,
	items []BatchCancelItem,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateBatchSize(len(items)); err != nil {
		return nil, err
	}
	for _, item := range items {
		if err := validateCategory(item.Category); err != nil {
			return nil, err
		}
		if err := validateOrderIdentity(item.OrderID, item.ClientOrderID); err != nil {
			return nil, err
		}
		if item.OrderID != "" && item.ClientOrderID != "" {
			return nil, validationError("batch cancel item must use either orderId or clientOid, not both")
		}
		if err := validateOptionalSymbol(item.Symbol); err != nil {
			return nil, err
		}
	}
	return client.mutateRaw(ctx, "/api/v3/trade/cancel-batch", items, credential.PermissionTrade, options...)
}

// ModifyOrderRequest는 주문 정정 요청이다. OrderID와 ClientOrderID 중 하나로 주문을 식별한다.
type ModifyOrderRequest struct {
	Category      Category `json:"category"`
	Symbol        string   `json:"symbol"`
	OrderID       string   `json:"orderId,omitempty"`
	ClientOrderID string   `json:"clientOid,omitempty"`
	Quantity      string   `json:"qty,omitempty"`
	Price         string   `json:"price,omitempty"`
	// AutoCancel은 정정 실패 시 원 주문을 취소할지 나타내는 Bitget 값(yes 또는 no)이다.
	AutoCancel string `json:"autoCancel,omitempty"`
}

func (request ModifyOrderRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateOrderIdentity(request.OrderID, request.ClientOrderID); err != nil {
		return err
	}
	if request.Quantity == "" && request.Price == "" {
		return validationError("modify order requires qty or price")
	}
	if err := validateOptionalDecimal("qty", request.Quantity); err != nil {
		return err
	}
	if err := validateOptionalDecimal("price", request.Price); err != nil {
		return err
	}
	if request.AutoCancel != "" && request.AutoCancel != "yes" && request.AutoCancel != "no" {
		return validationError("autoCancel must be yes or no")
	}
	return nil
}

// ModifyOrder는 미체결 주문의 가격 또는 수량을 정정한다.
// 이전 정정 결과를 받기 전에 같은 주문을 반복 정정할 수 없다.
// (POST /api/v3/trade/modify-order, Trade 권한)
func (client *Client) ModifyOrder(
	ctx context.Context,
	request ModifyOrderRequest,
	options ...trade.RequestOption,
) (OrderReference, error) {
	if err := request.validate(); err != nil {
		return OrderReference{}, err
	}
	return client.mutateReference(ctx, "/api/v3/trade/modify-order", request, options...)
}

// BatchModifyOrder는 주문 최대 20건을 한 번에 정정한다. 같은 요청에서 한 주문은 한 번만 나올 수 있다.
// (POST /api/v3/trade/batch-modify-order, Trade 권한)
func (client *Client) BatchModifyOrder(
	ctx context.Context,
	requests []ModifyOrderRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateBatchSize(len(requests)); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(requests))
	for _, request := range requests {
		if err := request.validate(); err != nil {
			return nil, err
		}
		key := request.OrderID + "\x00" + request.ClientOrderID
		if _, duplicated := seen[key]; duplicated {
			return nil, validationError("batch modify cannot contain the same order twice")
		}
		seen[key] = struct{}{}
	}
	return client.mutateRaw(
		ctx, "/api/v3/trade/batch-modify-order", requests, credential.PermissionTrade, options...,
	)
}

// CancelSymbolOrdersRequest는 심볼 또는 category 단위 전체 취소 범위다. Symbol이 비면 category 전체다.
type CancelSymbolOrdersRequest struct {
	Category Category `json:"category"`
	Symbol   string   `json:"symbol,omitempty"`
}

// CancelSymbolOrders는 심볼 또는 category의 미체결·부분체결 주문을 모두 취소 접수한다.
// (POST /api/v3/trade/cancel-symbol-order, Trade 권한)
func (client *Client) CancelSymbolOrders(
	ctx context.Context,
	request CancelSymbolOrdersRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateCategory(request.Category); err != nil {
		return nil, err
	}
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return nil, err
	}
	return client.mutateRaw(ctx, "/api/v3/trade/cancel-symbol-order", request, credential.PermissionTrade, options...)
}

// CountdownCancelAll은 dead man switch를 설정한다. countdownSeconds 동안 갱신 호출이 없으면
// 계정의 모든 주문이 취소되고, 0이면 스위치를 해제한다.
// 사용하려면 Bitget 사업 담당자를 통한 별도 신청이 필요하다.
// (POST /api/v3/trade/countdown-cancel-all, Trade 권한)
func (client *Client) CountdownCancelAll(
	ctx context.Context,
	countdownSeconds int,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if countdownSeconds < 0 {
		return nil, validationError("countdown cannot be negative")
	}
	body := struct {
		Countdown string `json:"countdown"`
	}{Countdown: strconv.Itoa(countdownSeconds)}
	return client.mutateRaw(ctx, "/api/v3/trade/countdown-cancel-all", body, credential.PermissionTrade, options...)
}

// ClosePositionsRequest는 포지션 일괄 청산 범위다. PositionSide가 비면 지정 범위의 모든 방향이다.
type ClosePositionsRequest struct {
	Category     Category     `json:"category"`
	Symbol       string       `json:"symbol,omitempty"`
	PositionSide PositionSide `json:"posSide,omitempty"`
}

// ClosePositions는 포지션을 시장가로 일괄 청산한다. 슬리피지가 발생할 수 있다.
// (POST /api/v3/trade/close-positions, Trade 권한)
func (client *Client) ClosePositions(
	ctx context.Context,
	request ClosePositionsRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if request.Category != CategoryUSDTFutures {
		return nil, validationError("close positions currently supports only USDT-FUTURES")
	}
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return nil, err
	}
	if request.PositionSide != "" && !request.PositionSide.valid() {
		return nil, validationError("posSide must be long or short")
	}
	return client.mutateRaw(ctx, "/api/v3/trade/close-positions", request, credential.PermissionTrade, options...)
}

// FillsRequest는 내 체결 내역 조회 조건이다.
type FillsRequest struct {
	Category Category
	OrderID  string
	Page
}

// Fills는 내 체결 내역을 cursor 기반으로 조회한다. (GET /api/v3/trade/fills, Read 권한)
func (client *Client) Fills(
	ctx context.Context,
	request FillsRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateCategory(request.Category); err != nil {
		return nil, err
	}
	if err := validateOptionalText("orderId", request.OrderID); err != nil {
		return nil, err
	}
	values := queryValues("category", string(request.Category), "orderId", request.OrderID)
	if err := request.Page.apply(values); err != nil {
		return nil, err
	}
	return client.readData(ctx, "/api/v3/trade/fills", values, options...)
}

// StrategyOrderType은 전략 주문 종류다. Bitget 문서의 type 값을 그대로 전달한다.
type StrategyOrderType string

// StrategyPrices는 TP/SL 가격과 트리거 설정이다. 값의 허용 범위는 Bitget 문서를 따른다.
type StrategyPrices struct {
	TakeProfit        string `json:"takeProfit,omitempty"`
	StopLoss          string `json:"stopLoss,omitempty"`
	TakeProfitTrigger string `json:"tpTriggerBy,omitempty"`
	StopLossTrigger   string `json:"slTriggerBy,omitempty"`
	TakeProfitType    string `json:"tpOrderType,omitempty"`
	StopLossType      string `json:"slOrderType,omitempty"`
	TakeProfitLimit   string `json:"tpLimitPrice,omitempty"`
	StopLossLimit     string `json:"slLimitPrice,omitempty"`
}

func (prices StrategyPrices) validate() error {
	for name, value := range map[string]string{
		"takeProfit": prices.TakeProfit, "stopLoss": prices.StopLoss,
		"tpLimitPrice": prices.TakeProfitLimit, "slLimitPrice": prices.StopLossLimit,
	} {
		if err := validateOptionalDecimal(name, value); err != nil {
			return err
		}
	}
	return nil
}

// PlaceStrategyOrderRequest는 TP/SL 전략 주문 요청이다.
type PlaceStrategyOrderRequest struct {
	Category      Category          `json:"category"`
	Symbol        string            `json:"symbol"`
	ClientOrderID string            `json:"clientOid,omitempty"`
	Type          StrategyOrderType `json:"type"`
	TPSLMode      string            `json:"tpslMode,omitempty"`
	Quantity      string            `json:"qty,omitempty"`
	PositionSide  PositionSide      `json:"posSide,omitempty"`
	StrategyPrices
}

func (request PlaceStrategyOrderRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if err := requireText("type", string(request.Type)); err != nil {
		return err
	}
	if request.ClientOrderID != "" && !clientOrderIDPattern.MatchString(request.ClientOrderID) {
		return validationError("clientOid has an invalid format")
	}
	if request.PositionSide != "" && !request.PositionSide.valid() {
		return validationError("posSide must be long or short")
	}
	if request.TakeProfit == "" && request.StopLoss == "" {
		return validationError("strategy order requires takeProfit or stopLoss")
	}
	if err := validateOptionalDecimal("qty", request.Quantity); err != nil {
		return err
	}
	return request.StrategyPrices.validate()
}

// PlaceStrategyOrder는 TP/SL 전략 주문을 생성한다.
// (POST /api/v3/trade/place-strategy-order, Trade 권한)
func (client *Client) PlaceStrategyOrder(
	ctx context.Context,
	request PlaceStrategyOrderRequest,
	options ...trade.RequestOption,
) (OrderReference, error) {
	if err := request.validate(); err != nil {
		return OrderReference{}, err
	}
	return client.mutateReference(ctx, "/api/v3/trade/place-strategy-order", request, options...)
}

// ModifyStrategyOrderRequest는 전략 주문 정정 요청이다.
type ModifyStrategyOrderRequest struct {
	OrderID       string `json:"orderId,omitempty"`
	ClientOrderID string `json:"clientOid,omitempty"`
	Quantity      string `json:"qty,omitempty"`
	StrategyPrices
}

// ModifyStrategyOrder는 전략 주문을 정정한다. (POST /api/v3/trade/modify-strategy-order, Trade 권한)
func (client *Client) ModifyStrategyOrder(
	ctx context.Context,
	request ModifyStrategyOrderRequest,
	options ...trade.RequestOption,
) (OrderReference, error) {
	if err := validateOrderIdentity(request.OrderID, request.ClientOrderID); err != nil {
		return OrderReference{}, err
	}
	if err := validateOptionalDecimal("qty", request.Quantity); err != nil {
		return OrderReference{}, err
	}
	if err := request.StrategyPrices.validate(); err != nil {
		return OrderReference{}, err
	}
	return client.mutateReference(ctx, "/api/v3/trade/modify-strategy-order", request, options...)
}

// CancelStrategyOrderRequest는 전략 주문 취소 요청이다.
type CancelStrategyOrderRequest struct {
	OrderID       string `json:"orderId,omitempty"`
	ClientOrderID string `json:"clientOid,omitempty"`
}

// CancelStrategyOrder는 전략 주문을 취소한다. (POST /api/v3/trade/cancel-strategy-order, Trade 권한)
func (client *Client) CancelStrategyOrder(
	ctx context.Context,
	request CancelStrategyOrderRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateOrderIdentity(request.OrderID, request.ClientOrderID); err != nil {
		return nil, err
	}
	return client.mutateRaw(
		ctx, "/api/v3/trade/cancel-strategy-order", request, credential.PermissionTrade, options...,
	)
}

// UnfilledStrategyOrdersRequest는 미체결 전략 주문 조회 조건이다. Type은 선택이다.
type UnfilledStrategyOrdersRequest struct {
	Category Category
	Type     StrategyOrderType
}

// UnfilledStrategyOrders는 미체결 전략 주문을 조회한다.
// (GET /api/v3/trade/unfilled-strategy-orders, Read 권한)
func (client *Client) UnfilledStrategyOrders(
	ctx context.Context,
	request UnfilledStrategyOrdersRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateCategory(request.Category); err != nil {
		return nil, err
	}
	if err := validateOptionalText("type", string(request.Type)); err != nil {
		return nil, err
	}
	values := queryValues("category", string(request.Category), "type", string(request.Type))
	return client.readData(ctx, "/api/v3/trade/unfilled-strategy-orders", values, options...)
}

// HistoryStrategyOrdersRequest는 전략 주문 이력 조회 조건이다. Type은 선택이다.
type HistoryStrategyOrdersRequest struct {
	Category Category
	Type     StrategyOrderType
	Page
}

// HistoryStrategyOrders는 전략 주문 이력을 cursor 기반으로 조회한다.
// (GET /api/v3/trade/history-strategy-orders, Read 권한)
func (client *Client) HistoryStrategyOrders(
	ctx context.Context,
	request HistoryStrategyOrdersRequest,
	options ...trade.RequestOption,
) (json.RawMessage, error) {
	if err := validateCategory(request.Category); err != nil {
		return nil, err
	}
	if err := validateOptionalText("type", string(request.Type)); err != nil {
		return nil, err
	}
	values := queryValues("category", string(request.Category), "type", string(request.Type))
	if err := request.Page.apply(values); err != nil {
		return nil, err
	}
	return client.readData(ctx, "/api/v3/trade/history-strategy-orders", values, options...)
}

func validateBatchSize(count int) error {
	if count == 0 || count > maxBatchOrders {
		return validationError("batch must contain between 1 and %d orders", maxBatchOrders)
	}
	return nil
}

// mutateReference는 mutation 응답을 OrderReference로 decode한다.
func (client *Client) mutateReference(
	ctx context.Context,
	path string,
	body any,
	options ...trade.RequestOption,
) (OrderReference, error) {
	response, data, err := client.mutate(ctx, path, body, credential.PermissionTrade, options...)
	if err != nil {
		return OrderReference{}, err
	}
	reference, err := decodeOrderReference(data)
	if err != nil {
		return OrderReference{}, client.decodeBodyError(response, commonexchange.OperationMutation, err)
	}
	return reference, nil
}
