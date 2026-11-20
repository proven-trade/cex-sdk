package gateio

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

var numericIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)

const (
	maxBatchOrderPairs    = 4
	maxBatchOrdersPerPair = 10
	maxBatchCancelOrders  = 20
	maxBatchAmendOrders   = 5
	minCountdownSeconds   = 5
)

// FinishedOrdersRequest는 종료(체결 또는 취소) Spot 주문 이력의 조회 조건이다.
// CurrencyPair를 비우면 전체 거래쌍을 조회하고 From/To는 주문 종료 시각 기준이다.
type FinishedOrdersRequest struct {
	CurrencyPair string
	Side         Side
	From         *time.Time
	To           *time.Time
	Page         int
	Limit        int
}

func (request FinishedOrdersRequest) validate() error {
	if request.CurrencyPair != "" {
		if err := validateCurrencyPair(request.CurrencyPair); err != nil {
			return err
		}
	}
	if err := validateOptionalSide(request.Side); err != nil {
		return err
	}
	if err := validateTimeRange(request.From, request.To); err != nil {
		return err
	}
	return validatePage(request.Page, request.Limit)
}

func (request FinishedOrdersRequest) values() url.Values {
	values := url.Values{"status": {"finished"}, "account": {"spot"}}
	if request.CurrencyPair != "" {
		values.Set("currency_pair", request.CurrencyPair)
	}
	if request.Side != "" {
		values.Set("side", string(request.Side))
	}
	setUnixTime(values, "from", request.From)
	setUnixTime(values, "to", request.To)
	setPositiveInt(values, "page", request.Page)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

// FinishedOrders는 종료된 Gate.io Spot 주문 이력을 조회한다. 권한은 Read다.
// GET /spot/orders?status=finished
func (client *Client) FinishedOrders(
	ctx context.Context,
	request FinishedOrdersRequest,
	options ...trade.RequestOption,
) ([]Order, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/spot/orders", request.values(), nil,
		privateLimit("finished-orders"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[Order](client, response, commonexchange.OperationRead)
}

func (order *Order) setRaw(raw json.RawMessage) { order.Raw = raw }

// BatchOrder는 일괄 주문·정정 응답의 항목이다. Succeeded가 false이면 Label과 Message에 실패 사유가 있다.
type BatchOrder struct {
	Order
	OrderID   string `json:"order_id"`
	AmendText string `json:"amend_text"`
	Succeeded bool   `json:"succeeded"`
	Label     string `json:"label"`
	Message   string `json:"message"`
}

// PlaceBatchOrdersRequest는 일괄 Spot 주문이다. 모든 주문은 PlaceOrderRequest 검증을 통과해야 하며
// 거래쌍은 최대 4개, 거래쌍당 최대 10건이다.
type PlaceBatchOrdersRequest struct {
	Orders []PlaceOrderRequest
}

func (request PlaceBatchOrdersRequest) validate() error {
	if len(request.Orders) == 0 {
		return validationError("batch orders cannot be empty")
	}
	perPair := make(map[string]int)
	for _, order := range request.Orders {
		if err := order.validate(); err != nil {
			return err
		}
		perPair[order.CurrencyPair]++
		if perPair[order.CurrencyPair] > maxBatchOrdersPerPair {
			return validationError("batch orders allow at most %d orders per currency pair", maxBatchOrdersPerPair)
		}
	}
	if len(perPair) > maxBatchOrderPairs {
		return validationError("batch orders allow at most %d currency pairs", maxBatchOrderPairs)
	}
	return nil
}

// PlaceBatchOrders는 Spot 주문을 일괄 생성한다. 권한은 Trade이며 자동 재시도하지 않는다.
// 항목별 성공 여부는 BatchOrder.Succeeded로 확인해야 한다.
// POST /spot/batch_orders
func (client *Client) PlaceBatchOrders(
	ctx context.Context,
	request PlaceBatchOrdersRequest,
	options ...trade.RequestOption,
) ([]BatchOrder, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	body, err := encodeBody(request.Orders)
	if err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/spot/batch_orders", nil, body,
		orderLimit(request.Orders[0].CurrencyPair), credential.PermissionTrade,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[BatchOrder](client, response, commonexchange.OperationMutation)
}

// AmendOrderRequest는 미체결 주문 한 건의 수량 또는 가격 정정 조건이다.
// Amount와 Price 중 하나 이상이 필요하다.
type AmendOrderRequest struct {
	OrderID      string `json:"-"`
	CurrencyPair string `json:"-"`
	Amount       string `json:"amount,omitempty"`
	Price        string `json:"price,omitempty"`
	AmendText    string `json:"amend_text,omitempty"`
}

func (request AmendOrderRequest) validate() error {
	if err := validateOrderIdentity(request.OrderID, request.CurrencyPair); err != nil {
		return err
	}
	return validateAmendFields(request.Amount, request.Price)
}

func validateAmendFields(amount, price string) error {
	if amount == "" && price == "" {
		return validationError("amend requires amount or price")
	}
	if amount != "" {
		if err := validatePositiveDecimal("amount", amount); err != nil {
			return err
		}
	}
	if price != "" {
		return validatePositiveDecimal("price", price)
	}
	return nil
}

// AmendOrder는 미체결 Spot 주문의 수량 또는 가격을 정정한다. 권한은 Trade이며 자동 재시도하지 않는다.
// PATCH /spot/orders/{order_id}
func (client *Client) AmendOrder(
	ctx context.Context,
	request AmendOrderRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return Order{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPatch, "/spot/orders/"+url.PathEscape(request.OrderID),
		url.Values{"currency_pair": {request.CurrencyPair}, "account": {"spot"}}, body,
		orderLimit(request.CurrencyPair), credential.PermissionTrade,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return Order{}, err
	}
	return client.decodeOrder(response, commonexchange.OperationMutation)
}

// AmendBatchItem은 일괄 정정의 주문 한 건이다.
type AmendBatchItem struct {
	OrderID      string `json:"order_id"`
	CurrencyPair string `json:"currency_pair"`
	Amount       string `json:"amount,omitempty"`
	Price        string `json:"price,omitempty"`
	AmendText    string `json:"amend_text,omitempty"`
}

// AmendBatchOrdersRequest는 최대 5건의 일괄 주문 정정이다.
type AmendBatchOrdersRequest struct {
	Items []AmendBatchItem
}

func (request AmendBatchOrdersRequest) validate() error {
	if len(request.Items) == 0 || len(request.Items) > maxBatchAmendOrders {
		return validationError("batch amend requires 1 to %d items", maxBatchAmendOrders)
	}
	for _, item := range request.Items {
		if err := validateOrderIdentity(item.OrderID, item.CurrencyPair); err != nil {
			return err
		}
		if err := validateAmendFields(item.Amount, item.Price); err != nil {
			return err
		}
	}
	return nil
}

// AmendBatchOrders는 미체결 Spot 주문을 일괄 정정한다. 권한은 Trade이며 자동 재시도하지 않는다.
// 항목별 성공 여부는 BatchOrder.Succeeded로 확인해야 한다.
// POST /spot/amend_batch_orders
func (client *Client) AmendBatchOrders(
	ctx context.Context,
	request AmendBatchOrdersRequest,
	options ...trade.RequestOption,
) ([]BatchOrder, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	body, err := encodeBody(request.Items)
	if err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/spot/amend_batch_orders", nil, body,
		orderLimit(request.Items[0].CurrencyPair), credential.PermissionTrade,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[BatchOrder](client, response, commonexchange.OperationMutation)
}

// CancelAllOrdersRequest는 Spot 미체결 주문 전체 취소 조건이다.
// CurrencyPair를 비우면 모든 거래쌍의 Spot 미체결 주문을 취소한다.
type CancelAllOrdersRequest struct {
	CurrencyPair string
	Side         Side
}

func (request CancelAllOrdersRequest) validate() error {
	if request.CurrencyPair != "" {
		if err := validateCurrencyPair(request.CurrencyPair); err != nil {
			return err
		}
	}
	return validateOptionalSide(request.Side)
}

func (request CancelAllOrdersRequest) values() url.Values {
	values := url.Values{"account": {"spot"}}
	if request.CurrencyPair != "" {
		values.Set("currency_pair", request.CurrencyPair)
	}
	if request.Side != "" {
		values.Set("side", string(request.Side))
	}
	return values
}

// CancelAllOrders는 Spot 미체결 주문을 일괄 취소하고 취소된 주문을 반환한다. 권한은 Trade이며
// 자동 재시도하지 않는다. 성공 응답은 취소 접수이므로 최종 상태는 조회로 확인해야 한다.
// DELETE /spot/orders
func (client *Client) CancelAllOrders(
	ctx context.Context,
	request CancelAllOrdersRequest,
	options ...trade.RequestOption,
) ([]Order, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodDelete, "/spot/orders", request.values(), nil,
		cancelLimit(), credential.PermissionTrade,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[Order](client, response, commonexchange.OperationMutation)
}

// CancelBatchOrderItem은 일괄 취소 대상 주문 한 건이다. ID는 거래소 주문 ID 또는 사용자 정의 text다.
type CancelBatchOrderItem struct {
	CurrencyPair string `json:"currency_pair"`
	ID           string `json:"id"`
}

// CancelBatchOrdersRequest는 최대 20건의 ID 목록 일괄 취소다.
type CancelBatchOrdersRequest struct {
	Orders []CancelBatchOrderItem
}

func (request CancelBatchOrdersRequest) validate() error {
	if len(request.Orders) == 0 || len(request.Orders) > maxBatchCancelOrders {
		return validationError("batch cancel requires 1 to %d orders", maxBatchCancelOrders)
	}
	for _, item := range request.Orders {
		if err := validateOrderIdentity(item.ID, item.CurrencyPair); err != nil {
			return err
		}
	}
	return nil
}

// CancelOrderResult는 일괄 취소의 항목별 결과다.
type CancelOrderResult struct {
	CurrencyPair string          `json:"currency_pair"`
	ID           string          `json:"id"`
	ClientText   string          `json:"text"`
	Succeeded    bool            `json:"succeeded"`
	Label        string          `json:"label"`
	Message      string          `json:"message"`
	Raw          json.RawMessage `json:"-"`
}

func (result *CancelOrderResult) setRaw(raw json.RawMessage) { result.Raw = raw }

// CancelBatchOrders는 ID 목록으로 Spot 주문을 일괄 취소한다. 권한은 Trade이며 자동 재시도하지 않는다.
// 항목별 성공 여부는 CancelOrderResult.Succeeded로 확인해야 한다.
// POST /spot/cancel_batch_orders
func (client *Client) CancelBatchOrders(
	ctx context.Context,
	request CancelBatchOrdersRequest,
	options ...trade.RequestOption,
) ([]CancelOrderResult, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	body, err := encodeBody(request.Orders)
	if err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/spot/cancel_batch_orders", nil, body,
		cancelLimit(), credential.PermissionTrade,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[CancelOrderResult](client, response, commonexchange.OperationMutation)
}

// CountdownCancelAllRequest는 카운트다운 전체 취소(dead-man switch) 설정이다.
// TimeoutSeconds는 5 이상이며 0이면 기존 카운트다운을 해제한다.
type CountdownCancelAllRequest struct {
	TimeoutSeconds int    `json:"timeout"`
	CurrencyPair   string `json:"currency_pair,omitempty"`
}

func (request CountdownCancelAllRequest) validate() error {
	if request.TimeoutSeconds != 0 && request.TimeoutSeconds < minCountdownSeconds {
		return validationError("countdown timeout must be 0 or at least %d seconds", minCountdownSeconds)
	}
	if request.CurrencyPair != "" {
		return validateCurrencyPair(request.CurrencyPair)
	}
	return nil
}

// CountdownTrigger는 카운트다운 종료 예정 시각이다.
type CountdownTrigger struct {
	TriggerTimeMilli int64           `json:"triggerTime"`
	Raw              json.RawMessage `json:"-"`
}

func (trigger *CountdownTrigger) setRaw(raw json.RawMessage) { trigger.Raw = raw }

// CountdownCancelAll은 지정 시간 안에 갱신하지 않으면 미체결 주문을 전부 취소하는 카운트다운을 설정한다.
// 권한은 Trade이며 자동 재시도하지 않는다.
// POST /spot/countdown_cancel_all
func (client *Client) CountdownCancelAll(
	ctx context.Context,
	request CountdownCancelAllRequest,
	options ...trade.RequestOption,
) (CountdownTrigger, error) {
	if err := request.validate(); err != nil {
		return CountdownTrigger{}, err
	}
	body, err := encodeBody(request)
	if err != nil {
		return CountdownTrigger{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/spot/countdown_cancel_all", nil, body,
		privateLimit("countdown-cancel-all"), credential.PermissionTrade,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return CountdownTrigger{}, err
	}
	return decodeObject[CountdownTrigger](client, response, commonexchange.OperationMutation)
}

// PriceOrderStatus는 가격 트리거 주문 목록의 상태 필터다.
type PriceOrderStatus string

const (
	PriceOrderOpen     PriceOrderStatus = "open"
	PriceOrderFinished PriceOrderStatus = "finished"
)

// PriceOrdersRequest는 가격 트리거 주문 목록 조회 조건이다.
type PriceOrdersRequest struct {
	Status PriceOrderStatus
	Market string
	Limit  int
	Offset int
}

func (request PriceOrdersRequest) validate() error {
	if request.Status != PriceOrderOpen && request.Status != PriceOrderFinished {
		return validationError("price order status must be open or finished")
	}
	if request.Market != "" {
		if err := validateCurrencyPair(request.Market); err != nil {
			return err
		}
	}
	return validateOffsetLimit(request.Offset, request.Limit, 1000)
}

func (request PriceOrdersRequest) values() url.Values {
	values := url.Values{"status": {string(request.Status)}}
	if request.Market != "" {
		values.Set("market", request.Market)
	}
	setPositiveInt(values, "limit", request.Limit)
	if request.Offset > 0 {
		values.Set("offset", strconv.Itoa(request.Offset))
	}
	return values
}

// PriceOrderTrigger는 가격 트리거 조건이다. Rule은 ">=" 또는 "<="이며 ExpirationSeconds는 선택이다.
type PriceOrderTrigger struct {
	Price             string `json:"price"`
	Rule              string `json:"rule"`
	ExpirationSeconds int    `json:"expiration,omitempty"`
}

// PriceOrderPut은 트리거 발동 시 생성할 Spot 주문이다. Account는 현물 거래 계정 "normal"로 고정된다.
type PriceOrderPut struct {
	Type        OrderType   `json:"type,omitempty"`
	Side        Side        `json:"side"`
	Price       string      `json:"price"`
	Amount      string      `json:"amount"`
	Account     string      `json:"account"`
	TimeInForce TimeInForce `json:"time_in_force"`
	Text        string      `json:"text,omitempty"`
}

// PriceOrder는 가격 트리거 주문 상태다.
type PriceOrder struct {
	Trigger      PriceOrderTrigger `json:"trigger"`
	Put          PriceOrderPut     `json:"put"`
	ID           int64             `json:"id"`
	User         int64             `json:"user"`
	Market       string            `json:"market"`
	CreatedAt    int64             `json:"ctime"`
	FinishedAt   int64             `json:"ftime"`
	FiredOrderID int64             `json:"fired_order_id"`
	Status       string            `json:"status"`
	Reason       string            `json:"reason"`
	Raw          json.RawMessage   `json:"-"`
}

func (order *PriceOrder) setRaw(raw json.RawMessage) { order.Raw = raw }

// PriceOrders는 가격 트리거 주문 목록을 조회한다. 권한은 Read다.
// GET /spot/price_orders
func (client *Client) PriceOrders(
	ctx context.Context,
	request PriceOrdersRequest,
	options ...trade.RequestOption,
) ([]PriceOrder, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/spot/price_orders", request.values(), nil,
		privateLimit("price-orders"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[PriceOrder](client, response, commonexchange.OperationRead)
}

// PlacePriceOrderRequest는 가격 트리거 Spot 주문 생성 조건이다.
// 지정가는 Price가 필요하고 시장가는 Price를 보내지 않는다.
type PlacePriceOrderRequest struct {
	Market      string
	Trigger     PriceOrderTrigger
	Type        OrderType
	Side        Side
	Price       string
	Amount      string
	TimeInForce TimeInForce
	Text        string
}

func (request PlacePriceOrderRequest) validate() error {
	if err := validateCurrencyPair(request.Market); err != nil {
		return err
	}
	if err := validatePositiveDecimal("trigger price", request.Trigger.Price); err != nil {
		return err
	}
	if request.Trigger.Rule != ">=" && request.Trigger.Rule != "<=" {
		return validationError("trigger rule must be >= or <=")
	}
	if request.Trigger.ExpirationSeconds < 0 {
		return validationError("trigger expiration cannot be negative")
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("order side must be buy or sell")
	}
	if err := validatePositiveDecimal("amount", request.Amount); err != nil {
		return err
	}
	switch request.Type {
	case OrderTypeLimit:
		if err := validatePositiveDecimal("price", request.Price); err != nil {
			return err
		}
	case OrderTypeMarket:
		if request.Price != "" {
			return validationError("market price order does not accept price")
		}
	default:
		return validationError("order type must be limit or market")
	}
	if request.TimeInForce != TimeInForceGTC && request.TimeInForce != TimeInForceIOC {
		return validationError("price order time in force must be gtc or ioc")
	}
	return nil
}

type placePriceOrderBody struct {
	Trigger PriceOrderTrigger `json:"trigger"`
	Put     PriceOrderPut     `json:"put"`
	Market  string            `json:"market"`
}

// PriceOrderAck는 가격 트리거 주문 생성 응답이다.
type PriceOrderAck struct {
	ID       int64           `json:"id"`
	IDString string          `json:"id_string"`
	Raw      json.RawMessage `json:"-"`
}

func (ack *PriceOrderAck) setRaw(raw json.RawMessage) { ack.Raw = raw }

// PlacePriceOrder는 가격 트리거 Spot 주문을 생성한다. 권한은 Trade이며 자동 재시도하지 않는다.
// POST /spot/price_orders
func (client *Client) PlacePriceOrder(
	ctx context.Context,
	request PlacePriceOrderRequest,
	options ...trade.RequestOption,
) (PriceOrderAck, error) {
	if err := request.validate(); err != nil {
		return PriceOrderAck{}, err
	}
	price := request.Price
	if request.Type == OrderTypeMarket {
		// 시장가 트리거 주문은 price 필드가 필수이므로 "0"을 보낸다.
		price = "0"
	}
	body, err := encodeBody(placePriceOrderBody{
		Trigger: request.Trigger, Market: request.Market,
		Put: PriceOrderPut{
			Type: request.Type, Side: request.Side, Price: price,
			Amount: request.Amount, Account: "normal",
			TimeInForce: request.TimeInForce, Text: request.Text,
		},
	})
	if err != nil {
		return PriceOrderAck{}, err
	}
	response, err := client.executePrivate(
		ctx, http.MethodPost, "/spot/price_orders", nil, body,
		orderLimit(request.Market), credential.PermissionTrade,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return PriceOrderAck{}, err
	}
	ack, err := decodeObject[PriceOrderAck](client, response, commonexchange.OperationMutation)
	if err != nil {
		return PriceOrderAck{}, err
	}
	if ack.ID == 0 && ack.IDString == "" {
		return PriceOrderAck{}, client.decodeBodyError(
			response, commonexchange.OperationMutation, errors.New("Gate.io price order acknowledgement is empty"),
		)
	}
	return ack, nil
}

// CancelAllPriceOrdersRequest는 가격 트리거 주문 전체 취소 조건이다. Market을 비우면 전체 시장이다.
type CancelAllPriceOrdersRequest struct{ Market string }

func (request CancelAllPriceOrdersRequest) validate() error {
	if request.Market != "" {
		return validateCurrencyPair(request.Market)
	}
	return nil
}

// CancelAllPriceOrders는 미발동 가격 트리거 주문을 일괄 취소하고 취소된 주문을 반환한다.
// 권한은 Trade이며 자동 재시도하지 않는다.
// DELETE /spot/price_orders
func (client *Client) CancelAllPriceOrders(
	ctx context.Context,
	request CancelAllPriceOrdersRequest,
	options ...trade.RequestOption,
) ([]PriceOrder, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	query := make(url.Values)
	if request.Market != "" {
		query.Set("market", request.Market)
	}
	response, err := client.executePrivate(
		ctx, http.MethodDelete, "/spot/price_orders", query, nil,
		cancelLimit(), credential.PermissionTrade,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeList[PriceOrder](client, response, commonexchange.OperationMutation)
}

// PriceOrder는 가격 트리거 주문 한 건을 조회한다. 권한은 Read다.
// GET /spot/price_orders/{order_id}
func (client *Client) PriceOrder(
	ctx context.Context,
	orderID string,
	options ...trade.RequestOption,
) (PriceOrder, error) {
	if !numericIDPattern.MatchString(orderID) {
		return PriceOrder{}, validationError("invalid price order ID")
	}
	response, err := client.executePrivate(
		ctx, http.MethodGet, "/spot/price_orders/"+url.PathEscape(orderID), nil, nil,
		privateLimit("price-order-info"), credential.PermissionRead,
		commonexchange.OperationRead, options...,
	)
	if err != nil {
		return PriceOrder{}, err
	}
	return decodeObject[PriceOrder](client, response, commonexchange.OperationRead)
}

// CancelPriceOrder는 가격 트리거 주문 한 건을 취소한다. 권한은 Trade이며 자동 재시도하지 않는다.
// DELETE /spot/price_orders/{order_id}
func (client *Client) CancelPriceOrder(
	ctx context.Context,
	orderID string,
	options ...trade.RequestOption,
) (PriceOrder, error) {
	if !numericIDPattern.MatchString(orderID) {
		return PriceOrder{}, validationError("invalid price order ID")
	}
	response, err := client.executePrivate(
		ctx, http.MethodDelete, "/spot/price_orders/"+url.PathEscape(orderID), nil, nil,
		cancelLimit(), credential.PermissionTrade,
		commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return PriceOrder{}, err
	}
	return decodeObject[PriceOrder](client, response, commonexchange.OperationMutation)
}
