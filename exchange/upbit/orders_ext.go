package upbit

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/credential"
	commonexchange "github.com/proven-trade/cex-sdk/exchange"
)

const (
	// maxOrderIDLookup은 id로 주문 목록 조회가 한 번에 받는 최대 UUID 또는 identifier 수다.
	maxOrderIDLookup = 100
	// maxOrderIDCancel은 id로 주문 목록 취소가 한 번에 받는 최대 UUID 또는 identifier 수다.
	maxOrderIDCancel = 20
	// maxBatchCancelCount는 주문 일괄 취소 한 번이 취소할 수 있는 최대 주문 수다.
	maxBatchCancelCount = 300
	// maxBatchCancelPairs는 주문 일괄 취소의 pairs, excluded_pairs 각각의 최대 페어 수다.
	maxBatchCancelPairs = 20
	// NewVolumeRemainOnly는 취소 후 재주문에서 신규 주문 수량을 기존 주문의 잔량으로 지정한다.
	NewVolumeRemainOnly = "remain_only"
)

// OrderChanceRequest는 주문 가능 정보를 조회할 페어다.
type OrderChanceRequest struct {
	Market string
}

// OrderChanceConstraint는 매수 또는 매도 주문의 통화와 최소 주문 금액이다.
type OrderChanceConstraint struct {
	Currency string  `json:"currency"`
	MinTotal Decimal `json:"min_total"`
}

// OrderChanceMarket은 페어의 주문 정책이다. 더 이상 쓰이지 않는 order_types, price_unit은 Raw에만 남는다.
type OrderChanceMarket struct {
	ID         string                `json:"id"`
	Name       string                `json:"name"`
	OrderSides []Side                `json:"order_sides"`
	BidTypes   []string              `json:"bid_types"`
	AskTypes   []string              `json:"ask_types"`
	Bid        OrderChanceConstraint `json:"bid"`
	Ask        OrderChanceConstraint `json:"ask"`
	MaxTotal   Decimal               `json:"max_total"`
	State      string                `json:"state"`
}

// OrderChance는 페어별 수수료율, 주문 정책과 호가·기준 자산 잔고다.
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

// OrdersByIDsRequest는 UUID 또는 identifier 목록으로 주문을 조회하는 조건이다.
// UUIDs와 Identifiers 중 하나만 지정하며 각각 최대 100개다.
type OrdersByIDsRequest struct {
	Market      string
	UUIDs       []string
	Identifiers []string
	// OrderBy는 주문 생성 시각 기준 asc 또는 desc다. 비우면 desc다.
	OrderBy string
}

// CancelOrdersByIDsRequest는 UUID 또는 identifier 목록으로 주문을 취소하는 조건이다.
// UUIDs와 Identifiers 중 하나만 지정하며 각각 최대 20개다.
type CancelOrdersByIDsRequest struct {
	UUIDs       []string
	Identifiers []string
}

// CancelSide는 주문 일괄 취소 대상 주문의 방향이다.
type CancelSide string

const (
	CancelSideAll CancelSide = "all"
	CancelSideBid CancelSide = "bid"
	CancelSideAsk CancelSide = "ask"
)

// CancelOpenOrdersRequest는 조건에 맞는 체결 대기 주문을 최대 300개 일괄 취소하는 조건이다.
// Pairs와 QuoteCurrencies는 함께 쓸 수 없다. 둘 다 비우면 전체 마켓이 대상이 되므로 AllMarkets를 명시해야 한다.
// ExcludedPairs는 어느 경우에나 함께 쓸 수 있으며 대상에서 우선 제외된다.
type CancelOpenOrdersRequest struct {
	// QuoteCurrencies는 취소 대상을 제한할 마켓(호가 자산)이다. 예: KRW, BTC, USDT
	QuoteCurrencies []string
	// Pairs는 취소 대상 페어이며 최대 20개다.
	Pairs []string
	// ExcludedPairs는 취소에서 제외할 페어이며 최대 20개다.
	ExcludedPairs []string
	AllMarkets    bool
	// CancelSide를 비우면 all이다.
	CancelSide CancelSide
	// Count는 취소할 최대 주문 수이며 비우면 20, 최대 300이다.
	Count int
	// OrderBy는 취소 순서이며 desc(최신 주문 먼저, 기본값) 또는 asc다.
	OrderBy string
}

// BatchCancelOrder는 일괄 취소 결과에 포함된 주문 한 건이다.
type BatchCancelOrder struct {
	UUID       string `json:"uuid"`
	Market     string `json:"market"`
	Identifier string `json:"identifier"`
}

// BatchCancelGroup은 일괄 취소의 성공 또는 실패 주문 묶음이다.
type BatchCancelGroup struct {
	Count  int                `json:"count"`
	Orders []BatchCancelOrder `json:"orders"`
}

// BatchCancelResult는 일괄 취소 접수 결과다. 취소 접수일 뿐이며 최종 상태는 다시 조회해야 한다.
type BatchCancelResult struct {
	Success BatchCancelGroup `json:"success"`
	Failed  BatchCancelGroup `json:"failed"`
	Raw     json.RawMessage  `json:"-"`
}

// CancelAndNewOrderRequest는 기존 주문을 취소하고 같은 페어·같은 방향의 신규 주문을 만드는 조건이다.
// PrevOrderUUID와 PrevOrderIdentifier 중 하나만 지정한다.
type CancelAndNewOrderRequest struct {
	PrevOrderUUID       string
	PrevOrderIdentifier string
	NewOrderType        OrderType
	// NewVolume은 신규 주문 수량이다. NewVolumeRemainOnly를 쓰면 기존 주문의 잔량으로 지정한다.
	NewVolume string
	// NewPrice는 신규 주문 단가이며 시장가 매수에서는 매수 총액이다.
	NewPrice       string
	NewIdentifier  string
	NewTimeInForce TimeInForce
	NewSMPType     SMPType
}

// CancelAndNewOrderResult는 취소된 기존 주문과 신규 주문 식별자다.
// 기존 주문이 취소되기 전에 전량 체결되면 신규 주문은 만들어지지 않고 NewOrderUUID가 비어 있다.
type CancelAndNewOrderResult struct {
	CanceledOrder      Order
	NewOrderUUID       string
	NewOrderIdentifier string
	Raw                json.RawMessage
}

// UnmarshalJSON은 평평한 응답에서 취소된 주문 필드와 신규 주문 식별자를 나누어 읽는다.
func (result *CancelAndNewOrderResult) UnmarshalJSON(data []byte) error {
	var wire struct {
		NewOrderUUID       string `json:"new_order_uuid"`
		NewOrderIdentifier string `json:"new_order_identifier"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	var canceled Order
	if err := json.Unmarshal(data, &canceled); err != nil {
		return err
	}
	result.CanceledOrder = canceled
	result.NewOrderUUID = wire.NewOrderUUID
	result.NewOrderIdentifier = wire.NewOrderIdentifier
	return nil
}

// OrderChance는 페어의 수수료율, 주문 정책과 계좌 잔고를 조회한다.
// 업비트 API Key에 주문조회 권한이 필요하다.
func (client *Client) OrderChance(
	ctx context.Context,
	request OrderChanceRequest,
	options ...trade.RequestOption,
) (OrderChance, error) {
	if err := request.validate(); err != nil {
		return OrderChance{}, err
	}
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/orders/chance", request.parameters(), nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return OrderChance{}, err
	}
	return decodeObject(client, response, commonexchange.OperationRead, func(item *OrderChance, raw []byte) {
		item.Raw = raw
	})
}

// TestOrder는 주문 생성 요청을 실제 주문 없이 검증한다. 응답은 주문 생성 응답과 같은 형식이며 체결되지 않는다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) TestOrder(
	ctx context.Context,
	request PlaceOrderRequest,
	options ...trade.RequestOption,
) (Order, error) {
	if err := request.validate(); err != nil {
		return Order{}, err
	}
	response, err := client.executeSignedPost(
		ctx, "/v1/orders/test", request.parameters(), orderTestRate, credential.PermissionTrade, options...,
	)
	if err != nil {
		return Order{}, err
	}
	return client.decodeOrder(response, commonexchange.OperationMutation)
}

// OrdersByIDs는 UUID 또는 identifier 목록으로 주문을 조회한다.
func (client *Client) OrdersByIDs(
	ctx context.Context,
	request OrdersByIDsRequest,
	options ...trade.RequestOption,
) ([]Order, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	response, err := client.executeSigned(
		ctx, http.MethodGet, "/v1/orders/uuids", request.parameters(), nil, defaultRate,
		credential.PermissionRead, commonexchange.OperationRead, options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeItems(client, response, commonexchange.OperationRead, func(order *Order, raw []byte) {
		order.Raw = raw
	})
}

// CancelOrdersByIDs는 UUID 또는 identifier 목록으로 주문을 한 번에 최대 20개 취소 접수한다.
// 일부만 실패할 수 있으므로 BatchCancelResult.Failed를 확인해야 한다.
func (client *Client) CancelOrdersByIDs(
	ctx context.Context,
	request CancelOrdersByIDsRequest,
	options ...trade.RequestOption,
) (BatchCancelResult, error) {
	if err := request.validate(); err != nil {
		return BatchCancelResult{}, err
	}
	response, err := client.executeSigned(
		ctx, http.MethodDelete, "/v1/orders/uuids", request.parameters(), nil, defaultRate,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return BatchCancelResult{}, err
	}
	return decodeObject(client, response, commonexchange.OperationMutation, func(item *BatchCancelResult, raw []byte) {
		item.Raw = raw
	})
}

// CancelOpenOrders는 조건에 맞는 체결 대기(wait) 주문을 최대 300개 일괄 취소 접수한다.
// 예약(watch) 주문은 대상이 아니다. 2초에 한 번만 호출할 수 있다.
func (client *Client) CancelOpenOrders(
	ctx context.Context,
	request CancelOpenOrdersRequest,
	options ...trade.RequestOption,
) (BatchCancelResult, error) {
	if err := request.validate(); err != nil {
		return BatchCancelResult{}, err
	}
	response, err := client.executeSigned(
		ctx, http.MethodDelete, "/v1/orders/open", request.parameters(), nil, cancelAllRate,
		credential.PermissionTrade, commonexchange.OperationMutation, options...,
	)
	if err != nil {
		return BatchCancelResult{}, err
	}
	return decodeObject(client, response, commonexchange.OperationMutation, func(item *BatchCancelResult, raw []byte) {
		item.Raw = raw
	})
}

// CancelAndNewOrder는 한 번의 요청으로 기존 주문을 취소하고 신규 주문을 만든다.
// 전송 결과가 불명확하면 자동 재시도하지 않고 UNKNOWN_EXECUTION_STATE로 반환한다.
func (client *Client) CancelAndNewOrder(
	ctx context.Context,
	request CancelAndNewOrderRequest,
	options ...trade.RequestOption,
) (CancelAndNewOrderResult, error) {
	if err := request.validate(); err != nil {
		return CancelAndNewOrderResult{}, err
	}
	response, err := client.executeSignedPost(
		ctx, "/v1/orders/cancel_and_new", request.parameters(), orderRate, credential.PermissionTrade, options...,
	)
	if err != nil {
		return CancelAndNewOrderResult{}, err
	}
	return decodeObject(
		client, response, commonexchange.OperationMutation,
		func(item *CancelAndNewOrderResult, raw []byte) { item.Raw = raw },
	)
}

func (request OrderChanceRequest) validate() error {
	return validateMarket(request.Market)
}

func (request OrderChanceRequest) parameters() parameters {
	return parameters{{key: "market", value: request.Market}}
}

func (request OrdersByIDsRequest) validate() error {
	if request.Market != "" {
		if err := validateMarket(request.Market); err != nil {
			return err
		}
	}
	if err := validateOrderIDLists(request.UUIDs, request.Identifiers, maxOrderIDLookup); err != nil {
		return err
	}
	return validateOrderBy(request.OrderBy)
}

func (request OrdersByIDsRequest) parameters() parameters {
	values := parameters{}
	values.add("market", request.Market)
	values.addAll("uuids[]", request.UUIDs)
	values.addAll("identifiers[]", request.Identifiers)
	values.add("order_by", request.OrderBy)
	return values
}

func (request CancelOrdersByIDsRequest) validate() error {
	return validateOrderIDLists(request.UUIDs, request.Identifiers, maxOrderIDCancel)
}

func (request CancelOrdersByIDsRequest) parameters() parameters {
	values := parameters{}
	values.addAll("uuids[]", request.UUIDs)
	values.addAll("identifiers[]", request.Identifiers)
	return values
}

func (request CancelOpenOrdersRequest) validate() error {
	if len(request.Pairs) > 0 && len(request.QuoteCurrencies) > 0 {
		return validationError("pairs and quote currencies cannot be used together")
	}
	scoped := len(request.Pairs) > 0 || len(request.QuoteCurrencies) > 0
	if !scoped && !request.AllMarkets {
		return validationError("pairs or quote currencies are required unless AllMarkets is true")
	}
	if scoped && request.AllMarkets {
		return validationError("AllMarkets cannot be combined with pairs or quote currencies")
	}
	if len(request.QuoteCurrencies) > 0 {
		if err := validateCurrencyList("quote currency", request.QuoteCurrencies); err != nil {
			return err
		}
	}
	if err := validateMarketList("pairs", request.Pairs, maxBatchCancelPairs); err != nil {
		return err
	}
	if err := validateMarketList("excluded pairs", request.ExcludedPairs, maxBatchCancelPairs); err != nil {
		return err
	}
	switch request.CancelSide {
	case "", CancelSideAll, CancelSideBid, CancelSideAsk:
	default:
		return validationError("cancel side must be all, bid, or ask")
	}
	if request.Count < 0 || request.Count > maxBatchCancelCount {
		return validationError("cancel count must be between 1 and %d or zero for default", maxBatchCancelCount)
	}
	return validateOrderBy(request.OrderBy)
}

func (request CancelOpenOrdersRequest) parameters() parameters {
	values := parameters{}
	values.add("quote_currencies", strings.Join(request.QuoteCurrencies, ","))
	values.add("cancel_side", string(request.CancelSide))
	values.addInt("count", request.Count)
	values.add("order_by", request.OrderBy)
	values.add("pairs", strings.Join(request.Pairs, ","))
	values.add("excluded_pairs", strings.Join(request.ExcludedPairs, ","))
	return values
}

func (request CancelAndNewOrderRequest) validate() error {
	if err := validateOrderIdentity(request.PrevOrderUUID, request.PrevOrderIdentifier); err != nil {
		return err
	}
	switch request.NewOrderType {
	case OrderTypeLimit:
		if err := validateNewVolume(request.NewVolume); err != nil {
			return err
		}
		if err := validatePositiveDecimal("new price", request.NewPrice); err != nil {
			return err
		}
	case OrderTypePrice:
		if request.NewVolume != "" {
			return validationError("price order does not accept new volume")
		}
		if err := validatePositiveDecimal("new price", request.NewPrice); err != nil {
			return err
		}
	case OrderTypeMarket:
		if request.NewPrice != "" {
			return validationError("market order does not accept new price")
		}
		if err := validateNewVolume(request.NewVolume); err != nil {
			return err
		}
	case OrderTypeBest:
		if request.NewTimeInForce != TimeInForceIOC && request.NewTimeInForce != TimeInForceFOK {
			return validationError("best order requires ioc or fok new time in force")
		}
		if (request.NewPrice == "") == (request.NewVolume == "") {
			return validationError("best order requires exactly one of new price (bid) or new volume (ask)")
		}
		if request.NewPrice != "" {
			if err := validatePositiveDecimal("new price", request.NewPrice); err != nil {
				return err
			}
		} else if err := validateNewVolume(request.NewVolume); err != nil {
			return err
		}
	default:
		return validationError("new order type must be limit, price, market, or best")
	}
	if request.NewTimeInForce != "" && !request.NewTimeInForce.valid() {
		return validationError("unsupported new time in force %q", request.NewTimeInForce)
	}
	if request.NewOrderType != OrderTypeLimit && request.NewOrderType != OrderTypeBest && request.NewTimeInForce != "" {
		return validationError("new time in force is only supported for limit and best orders")
	}
	if request.NewSMPType != "" && !request.NewSMPType.valid() {
		return validationError("unsupported new SMP type %q", request.NewSMPType)
	}
	if request.NewTimeInForce == TimeInForcePostOnly && request.NewSMPType != "" && request.NewSMPType != SMPTypeNone {
		return validationError("post_only can only be combined with new SMP type none")
	}
	if strings.TrimSpace(request.NewIdentifier) != request.NewIdentifier {
		return validationError("new identifier cannot have surrounding whitespace")
	}
	if utf8.RuneCountInString(request.NewIdentifier) > 64 {
		return validationError("new identifier must be at most 64 characters")
	}
	if request.NewIdentifier != "" && request.NewIdentifier == request.PrevOrderIdentifier {
		return validationError("new identifier must differ from the previous order identifier")
	}
	return nil
}

func (request CancelAndNewOrderRequest) parameters() parameters {
	values := parameters{}
	values.add("prev_order_uuid", request.PrevOrderUUID)
	values.add("prev_order_identifier", request.PrevOrderIdentifier)
	values.add("new_ord_type", string(request.NewOrderType))
	values.add("new_volume", request.NewVolume)
	values.add("new_price", request.NewPrice)
	values.add("new_identifier", request.NewIdentifier)
	values.add("new_time_in_force", string(request.NewTimeInForce))
	values.add("new_smp_type", string(request.NewSMPType))
	return values
}

// validateOrderIDLists는 UUID 목록과 identifier 목록 중 정확히 하나만 지정됐는지 검사한다.
func validateOrderIDLists(uuids, identifiers []string, limit int) error {
	if (len(uuids) == 0) == (len(identifiers) == 0) {
		return validationError("exactly one of UUIDs or identifiers is required")
	}
	if len(uuids) > 0 {
		return validateValueList("order UUIDs", uuids, limit)
	}
	return validateValueList("order identifiers", identifiers, limit)
}

// validateMarketList는 비어 있어도 되는 페어 목록의 개수, 형식과 중복을 검사한다.
func validateMarketList(name string, markets []string, limit int) error {
	if len(markets) > limit {
		return validationError("%s must contain at most %d items", name, limit)
	}
	seen := make(map[string]struct{}, len(markets))
	for _, market := range markets {
		if err := validateMarket(market); err != nil {
			return err
		}
		if _, exists := seen[market]; exists {
			return validationError("duplicate market %q in %s", market, name)
		}
		seen[market] = struct{}{}
	}
	return nil
}

func validateNewVolume(value string) error {
	if value == NewVolumeRemainOnly {
		return nil
	}
	return validatePositiveDecimal("new volume", value)
}
