package upbit

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
)

var positiveDecimalPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

// MarketsRequest는 전체 마켓 조회 조건이다.
type MarketsRequest struct {
	IncludeDetails bool
}

// TickersRequest는 현재가를 조회할 마켓 목록이다.
type TickersRequest struct {
	Markets []string
}

// OrderBooksRequest는 호가를 조회할 마켓 목록과 깊이다.
type OrderBooksRequest struct {
	Markets []string
	Count   int
}

// RecentTradesRequest는 공개 최근 체결 조회 조건이다.
type RecentTradesRequest struct {
	Market  string
	To      string
	Count   int
	Cursor  int64
	DaysAgo int
}

// MinuteUnit은 업비트가 지원하는 분봉 단위다.
type MinuteUnit int

const (
	Minute1   MinuteUnit = 1
	Minute3   MinuteUnit = 3
	Minute5   MinuteUnit = 5
	Minute10  MinuteUnit = 10
	Minute15  MinuteUnit = 15
	Minute30  MinuteUnit = 30
	Minute60  MinuteUnit = 60
	Minute240 MinuteUnit = 240
)

// MinuteCandlesRequest는 분봉 조회 조건이다.
type MinuteCandlesRequest struct {
	Market string
	Unit   MinuteUnit
	To     string
	Count  int
}

// PlaceOrderRequest는 업비트 Spot 신규 주문이다.
type PlaceOrderRequest struct {
	Market      string
	Side        Side
	Volume      string
	Price       string
	OrderType   OrderType
	TimeInForce TimeInForce
	Identifier  string
	SMPType     SMPType
}

// OrderInfoRequest는 UUID 또는 사용자 식별자로 주문 한 건을 조회한다.
type OrderInfoRequest struct {
	UUID       string
	Identifier string
}

// CancelOrderRequest는 UUID 또는 사용자 식별자로 주문 취소를 요청한다.
type CancelOrderRequest struct {
	UUID       string
	Identifier string
}

// OpenOrdersRequest는 미체결 주문 목록 조회 조건이다.
// Market 없이 조회하려면 AllMarkets를 명시해야 한다.
type OpenOrdersRequest struct {
	Market     string
	AllMarkets bool
	State      OrderState
	States     []OrderState
	Page       int
	Limit      int
	OrderBy    string
}

// ClosedOrdersRequest는 종료 주문 목록 조회 조건이다.
// Market 없이 조회하려면 AllMarkets를 명시해야 한다.
type ClosedOrdersRequest struct {
	Market     string
	AllMarkets bool
	State      OrderState
	States     []OrderState
	StartTime  *time.Time
	EndTime    *time.Time
	Limit      int
	OrderBy    string
}

func (request MarketsRequest) parameters() parameters {
	values := parameters{}
	values.addBool("is_details", request.IncludeDetails)
	return values
}

func (request TickersRequest) validate() error {
	return validateMarkets(request.Markets)
}

func (request TickersRequest) parameters() parameters {
	return parameters{{key: "markets", value: strings.Join(request.Markets, ",")}}
}

func (request OrderBooksRequest) validate() error {
	if err := validateMarkets(request.Markets); err != nil {
		return err
	}
	if request.Count < 0 || request.Count > 30 {
		return validationError("order book count must be between 1 and 30 or zero for default")
	}
	return nil
}

func (request OrderBooksRequest) parameters() parameters {
	values := parameters{{key: "markets", value: strings.Join(request.Markets, ",")}}
	values.addInt("count", request.Count)
	return values
}

func (request RecentTradesRequest) validate() error {
	if err := validateMarket(request.Market); err != nil {
		return err
	}
	if request.Count < 0 || request.Count > 500 {
		return validationError("recent trade count must be between 1 and 500 or zero for default")
	}
	if request.Cursor < 0 {
		return validationError("recent trade cursor cannot be negative")
	}
	if request.DaysAgo < 0 || request.DaysAgo > 7 {
		return validationError("daysAgo must be between 1 and 7 or zero")
	}
	return nil
}

func (request RecentTradesRequest) parameters() parameters {
	values := parameters{{key: "market", value: request.Market}}
	values.add("to", request.To)
	values.addInt("count", request.Count)
	if request.Cursor > 0 {
		values.add("cursor", strconv.FormatInt(request.Cursor, 10))
	}
	values.addInt("days_ago", request.DaysAgo)
	return values
}

func (request MinuteCandlesRequest) validate() error {
	if err := validateMarket(request.Market); err != nil {
		return err
	}
	if !request.Unit.valid() {
		return validationError("unsupported minute candle unit %d", request.Unit)
	}
	if request.Count < 0 || request.Count > 200 {
		return validationError("candle count must be between 1 and 200 or zero for default")
	}
	return nil
}

func (request MinuteCandlesRequest) parameters() parameters {
	values := parameters{{key: "market", value: request.Market}}
	values.add("to", request.To)
	values.addInt("count", request.Count)
	return values
}

func (request PlaceOrderRequest) validate() error {
	if err := validateMarket(request.Market); err != nil {
		return err
	}
	if request.Side != SideAsk && request.Side != SideBid {
		return validationError("side must be ask or bid")
	}
	switch request.OrderType {
	case OrderTypeLimit:
		if err := validatePositiveDecimal("volume", request.Volume); err != nil {
			return err
		}
		if err := validatePositiveDecimal("price", request.Price); err != nil {
			return err
		}
	case OrderTypePrice:
		if request.Side != SideBid {
			return validationError("price order must use bid side")
		}
		if request.Volume != "" {
			return validationError("price order does not accept volume")
		}
		if err := validatePositiveDecimal("price", request.Price); err != nil {
			return err
		}
	case OrderTypeMarket:
		if request.Side != SideAsk {
			return validationError("market order must use ask side")
		}
		if request.Price != "" {
			return validationError("market order does not accept price")
		}
		if err := validatePositiveDecimal("volume", request.Volume); err != nil {
			return err
		}
	default:
		return validationError("orderType must be limit, price, or market")
	}
	if request.TimeInForce != "" && !request.TimeInForce.valid() {
		return validationError("unsupported timeInForce %q", request.TimeInForce)
	}
	if request.OrderType != OrderTypeLimit && request.TimeInForce != "" {
		return validationError("timeInForce is only supported for limit orders")
	}
	if request.SMPType != "" && !request.SMPType.valid() {
		return validationError("unsupported SMP type %q", request.SMPType)
	}
	if request.TimeInForce == TimeInForcePostOnly && request.SMPType != "" {
		return validationError("post_only cannot be combined with SMP")
	}
	if strings.TrimSpace(request.Identifier) != request.Identifier {
		return validationError("identifier cannot have surrounding whitespace")
	}
	return nil
}

func (request PlaceOrderRequest) parameters() parameters {
	values := parameters{}
	values.add("market", request.Market)
	values.add("side", string(request.Side))
	values.add("volume", request.Volume)
	values.add("price", request.Price)
	values.add("ord_type", string(request.OrderType))
	values.add("time_in_force", string(request.TimeInForce))
	values.add("identifier", request.Identifier)
	values.add("smp_type", string(request.SMPType))
	return values
}

func (request OrderInfoRequest) validate() error {
	return validateOrderIdentity(request.UUID, request.Identifier)
}

func (request OrderInfoRequest) parameters() parameters {
	return orderIdentityParameters(request.UUID, request.Identifier)
}

func (request CancelOrderRequest) validate() error {
	return validateOrderIdentity(request.UUID, request.Identifier)
}

func (request CancelOrderRequest) parameters() parameters {
	return orderIdentityParameters(request.UUID, request.Identifier)
}

func (request OpenOrdersRequest) validate() error {
	if err := validateMarketScope(request.Market, request.AllMarkets); err != nil {
		return err
	}
	if request.State != "" && len(request.States) > 0 {
		return validationError("state and states cannot be used together")
	}
	if request.State != "" && request.State != OrderStateWait && request.State != OrderStateWatch {
		return validationError("open order state must be wait or watch")
	}
	for _, state := range request.States {
		if state != OrderStateWait && state != OrderStateWatch {
			return validationError("open order states must contain only wait or watch")
		}
	}
	if request.Page < 0 {
		return validationError("page cannot be negative")
	}
	if request.Limit < 0 || request.Limit > 100 {
		return validationError("open order limit must be between 1 and 100 or zero for default")
	}
	return validateOrderBy(request.OrderBy)
}

func (request OpenOrdersRequest) parameters() parameters {
	values := parameters{}
	values.add("market", request.Market)
	values.add("state", string(request.State))
	for _, state := range request.States {
		values.add("states[]", string(state))
	}
	values.addInt("page", request.Page)
	values.addInt("limit", request.Limit)
	values.add("order_by", request.OrderBy)
	return values
}

func (request ClosedOrdersRequest) validate() error {
	if err := validateMarketScope(request.Market, request.AllMarkets); err != nil {
		return err
	}
	if request.State != "" && len(request.States) > 0 {
		return validationError("state and states cannot be used together")
	}
	if request.State != "" && request.State != OrderStateDone && request.State != OrderStateCancel {
		return validationError("closed order state must be done or cancel")
	}
	for _, state := range request.States {
		if state != OrderStateDone && state != OrderStateCancel {
			return validationError("closed order states must contain only done or cancel")
		}
	}
	if request.StartTime != nil && request.EndTime != nil {
		if request.StartTime.After(*request.EndTime) {
			return validationError("startTime cannot be after endTime")
		}
		if request.EndTime.Sub(*request.StartTime) > 7*24*time.Hour {
			return validationError("closed order time range cannot exceed seven days")
		}
	}
	if request.Limit < 0 || request.Limit > 1000 {
		return validationError("closed order limit must be between 1 and 1000 or zero for default")
	}
	return validateOrderBy(request.OrderBy)
}

func (request ClosedOrdersRequest) parameters() parameters {
	values := parameters{}
	values.add("market", request.Market)
	values.add("state", string(request.State))
	for _, state := range request.States {
		values.add("states[]", string(state))
	}
	if request.StartTime != nil {
		values.add("start_time", request.StartTime.Format(time.RFC3339))
	}
	if request.EndTime != nil {
		values.add("end_time", request.EndTime.Format(time.RFC3339))
	}
	values.addInt("limit", request.Limit)
	values.add("order_by", request.OrderBy)
	return values
}

func validateMarkets(markets []string) error {
	if len(markets) == 0 || len(markets) > 100 {
		return validationError("markets must contain between 1 and 100 items")
	}
	seen := make(map[string]struct{}, len(markets))
	for _, market := range markets {
		if err := validateMarket(market); err != nil {
			return err
		}
		if _, exists := seen[market]; exists {
			return validationError("duplicate market %q", market)
		}
		seen[market] = struct{}{}
	}
	return nil
}

func validateMarket(market string) error {
	if market == "" || strings.TrimSpace(market) != market || strings.ContainsAny(market, " ,&=") || !strings.Contains(market, "-") {
		return validationError("invalid market %q", market)
	}
	return nil
}

func validateMarketScope(market string, allMarkets bool) error {
	if market == "" {
		if !allMarkets {
			return validationError("market is required unless AllMarkets is true")
		}
		return nil
	}
	if allMarkets {
		return validationError("market and AllMarkets cannot be used together")
	}
	return validateMarket(market)
}

func validateOrderIdentity(uuid, identifier string) error {
	if (uuid == "") == (identifier == "") {
		return validationError("exactly one of UUID or identifier is required")
	}
	value := uuid
	if value == "" {
		value = identifier
	}
	if strings.TrimSpace(value) != value {
		return validationError("order identity cannot have surrounding whitespace")
	}
	return nil
}

func orderIdentityParameters(uuid, identifier string) parameters {
	values := parameters{}
	values.add("uuid", uuid)
	values.add("identifier", identifier)
	return values
}

func validatePositiveDecimal(name, value string) error {
	if !positiveDecimalPattern.MatchString(value) || strings.Trim(value, "0.") == "" {
		return validationError("%s must be a positive decimal", name)
	}
	return nil
}

func validateOrderBy(value string) error {
	if value != "" && value != "asc" && value != "desc" {
		return validationError("orderBy must be asc, desc, or empty")
	}
	return nil
}

func (unit MinuteUnit) valid() bool {
	switch unit {
	case Minute1, Minute3, Minute5, Minute10, Minute15, Minute30, Minute60, Minute240:
		return true
	default:
		return false
	}
}

func (value TimeInForce) valid() bool {
	return value == TimeInForceIOC || value == TimeInForceFOK || value == TimeInForcePostOnly
}

func (value SMPType) valid() bool {
	return value == SMPTypeCancelMaker || value == SMPTypeCancelTaker || value == SMPTypeReduce
}

func validationError(format string, values ...any) error {
	return &trade.APIError{
		Category: trade.ErrorValidation,
		Exchange: model.ExchangeUpbit,
		Cause:    fmt.Errorf(format, values...),
	}
}
