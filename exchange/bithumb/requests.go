package bithumb

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/model"
)

var (
	positiveDecimalPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
	clientOrderIDPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,36}$`)
	marketPattern          = regexp.MustCompile(`^[A-Z0-9]+-[A-Z0-9]+$`)
)

// MarketsRequest는 전체 마켓 조회 조건이다.
type MarketsRequest struct {
	IncludeDetails bool
}

// TickersRequest는 현재가를 조회할 마켓 목록이다.
type TickersRequest struct {
	Markets []string
}

// OrderBooksRequest는 호가를 조회할 마켓 목록이다.
type OrderBooksRequest struct {
	Markets []string
}

// RecentTradesRequest는 공개 최근 체결 조회 조건이다.
type RecentTradesRequest struct {
	Market  string
	To      string
	Count   int
	Cursor  int64
	DaysAgo int
}

// MinuteUnit은 빗썸이 지원하는 분봉 단위다.
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

// PlaceOrderRequest는 빗썸 Spot 신규 주문이다.
type PlaceOrderRequest struct {
	Market        string
	Side          Side
	Volume        string
	Price         string
	OrderType     OrderType
	TimeInForce   TimeInForce
	ClientOrderID string
}

// OrderInfoRequest는 UUID 또는 사용자 주문 ID로 주문 한 건을 조회한다.
type OrderInfoRequest struct {
	UUID          string
	ClientOrderID string
}

// CancelOrderRequest는 거래소 주문 ID 또는 사용자 주문 ID로 주문을 취소한다.
type CancelOrderRequest struct {
	OrderID       string
	ClientOrderID string
}

// PendingOrdersRequest는 미체결 주문 목록 조회 조건이다.
// Market 없이 조회하려면 AllMarkets를 명시해야 한다.
type PendingOrdersRequest struct {
	Market     string
	AllMarkets bool
	State      OrderState
	Limit      int
	OrderBy    string
	NextKey    string
}

// OrderHistoryRequest는 주문 이력 조회 조건이다.
// Market 없이 조회하려면 AllMarkets를 명시해야 한다.
type OrderHistoryRequest struct {
	Market     string
	AllMarkets bool
	State      OrderState
	States     []OrderState
	StartTime  *time.Time
	EndTime    *time.Time
	Limit      int
	OrderBy    string
	NextKey    string
}

func (request MarketsRequest) parameters() parameters {
	values := parameters{}
	values.addBool("isDetails", request.IncludeDetails)
	return values
}

func (request TickersRequest) validate() error {
	return validateMarkets(request.Markets)
}

func (request TickersRequest) parameters() parameters {
	return parameters{{key: "markets", value: strings.Join(request.Markets, ",")}}
}

func (request OrderBooksRequest) validate() error {
	return validateMarkets(request.Markets)
}

func (request OrderBooksRequest) parameters() parameters {
	return parameters{{key: "markets", value: strings.Join(request.Markets, ",")}}
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
	return validateParameterValue("to", request.To)
}

func (request RecentTradesRequest) parameters() parameters {
	values := parameters{{key: "market", value: request.Market}}
	values.add("to", request.To)
	values.addInt("count", request.Count)
	values.addInt64("cursor", request.Cursor)
	values.addInt("daysAgo", request.DaysAgo)
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
	return validateParameterValue("to", request.To)
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
	if request.TimeInForce != "" && !strings.HasPrefix(request.Market, "KRW-") {
		return validationError("timeInForce is only supported in KRW markets")
	}
	switch request.OrderType {
	case OrderTypeLimit:
		if err := validatePositiveDecimal("volume", request.Volume); err != nil {
			return err
		}
		if err := validatePositiveDecimal("price", request.Price); err != nil {
			return err
		}
		if request.TimeInForce != "" && !request.TimeInForce.valid() {
			return validationError("unsupported timeInForce %q", request.TimeInForce)
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
		if request.TimeInForce != "" {
			return validationError("price order does not accept timeInForce")
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
		if request.TimeInForce != "" {
			return validationError("market order does not accept timeInForce")
		}
	case OrderTypeBest:
		if !strings.HasPrefix(request.Market, "KRW-") {
			return validationError("best order is only supported in KRW markets")
		}
		if request.TimeInForce != TimeInForceIOC && request.TimeInForce != TimeInForceFOK {
			return validationError("best order requires ioc or fok timeInForce")
		}
		if request.Side == SideBid {
			if request.Volume != "" {
				return validationError("best bid order does not accept volume")
			}
			if err := validatePositiveDecimal("price", request.Price); err != nil {
				return err
			}
		} else {
			if request.Price != "" {
				return validationError("best ask order does not accept price")
			}
			if err := validatePositiveDecimal("volume", request.Volume); err != nil {
				return err
			}
		}
	default:
		return validationError("orderType must be limit, price, market, or best")
	}
	if request.TimeInForce == TimeInForcePostOnly && request.OrderType != OrderTypeLimit {
		return validationError("post_only is only supported for limit orders")
	}
	if request.ClientOrderID != "" && !clientOrderIDPattern.MatchString(request.ClientOrderID) {
		return validationError("clientOrderID must contain 1 to 36 letters, digits, underscores, or hyphens")
	}
	return nil
}

func (request PlaceOrderRequest) parameters() parameters {
	values := parameters{}
	values.add("market", request.Market)
	values.add("side", string(request.Side))
	values.add("volume", request.Volume)
	values.add("price", request.Price)
	values.add("order_type", string(request.OrderType))
	values.add("time_in_force", string(request.TimeInForce))
	values.add("client_order_id", request.ClientOrderID)
	return values
}

func (request OrderInfoRequest) validate() error {
	return validateOrderIdentity(request.UUID, request.ClientOrderID, "UUID")
}

func (request OrderInfoRequest) parameters() parameters {
	values := parameters{}
	values.add("uuid", request.UUID)
	values.add("client_order_id", request.ClientOrderID)
	return values
}

func (request CancelOrderRequest) validate() error {
	return validateOrderIdentity(request.OrderID, request.ClientOrderID, "orderID")
}

func (request CancelOrderRequest) parameters() parameters {
	values := parameters{}
	values.add("order_id", request.OrderID)
	values.add("client_order_id", request.ClientOrderID)
	return values
}

func (request PendingOrdersRequest) validate() error {
	if err := validateMarketScope(request.Market, request.AllMarkets); err != nil {
		return err
	}
	if request.State != "" && request.State != OrderStateWait && request.State != OrderStateWatch {
		return validationError("pending order state must be wait or watch")
	}
	if request.Limit < 0 || request.Limit > 100 {
		return validationError("pending order limit must be between 1 and 100 or zero for default")
	}
	if err := validateOrderBy(request.OrderBy); err != nil {
		return err
	}
	return validateParameterValue("nextKey", request.NextKey)
}

func (request PendingOrdersRequest) parameters() parameters {
	values := parameters{}
	values.add("market", request.Market)
	values.add("state", string(request.State))
	values.addInt("limit", request.Limit)
	values.add("order_by", request.OrderBy)
	values.add("next_key", request.NextKey)
	return values
}

func (request OrderHistoryRequest) validate() error {
	if err := validateMarketScope(request.Market, request.AllMarkets); err != nil {
		return err
	}
	if request.State != "" && len(request.States) > 0 {
		return validationError("state and states cannot be used together")
	}
	if request.State != "" && request.State != OrderStateDone && request.State != OrderStateCancel {
		return validationError("history state must be done or cancel")
	}
	for _, state := range request.States {
		if state != OrderStateDone && state != OrderStateCancel {
			return validationError("history states must contain only done or cancel")
		}
	}
	if request.StartTime != nil && request.EndTime != nil {
		if request.StartTime.After(*request.EndTime) {
			return validationError("startTime cannot be after endTime")
		}
		if request.EndTime.Sub(*request.StartTime) > 7*24*time.Hour {
			return validationError("order history time range cannot exceed seven days")
		}
	}
	if request.Limit < 0 || request.Limit > 1000 {
		return validationError("order history limit must be between 1 and 1000 or zero for default")
	}
	if err := validateOrderBy(request.OrderBy); err != nil {
		return err
	}
	return validateParameterValue("nextKey", request.NextKey)
}

func (request OrderHistoryRequest) parameters() parameters {
	values := parameters{}
	values.add("market", request.Market)
	values.add("state", string(request.State))
	for _, state := range request.States {
		values.add("states[]", string(state))
	}
	if request.StartTime != nil {
		values.add("start_time", strconv.FormatInt(request.StartTime.UnixMilli(), 10))
	}
	if request.EndTime != nil {
		values.add("end_time", strconv.FormatInt(request.EndTime.UnixMilli(), 10))
	}
	values.addInt("limit", request.Limit)
	values.add("order_by", request.OrderBy)
	values.add("next_key", request.NextKey)
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
	if !marketPattern.MatchString(market) {
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

func validateOrderIdentity(exchangeID, clientOrderID, exchangeName string) error {
	if (exchangeID == "") == (clientOrderID == "") {
		return validationError("exactly one of %s or clientOrderID is required", exchangeName)
	}
	if strings.TrimSpace(exchangeID) != exchangeID || strings.ContainsAny(exchangeID, "&=") {
		return validationError("invalid %s", exchangeName)
	}
	if clientOrderID != "" && !clientOrderIDPattern.MatchString(clientOrderID) {
		return validationError("invalid clientOrderID")
	}
	return nil
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

func validateParameterValue(name, value string) error {
	if strings.Contains(value, "&") {
		return validationError("%s cannot contain ampersand", name)
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

func validationError(format string, values ...any) error {
	return &trade.APIError{
		Category: trade.ErrorValidation,
		Exchange: model.ExchangeBithumb,
		Cause:    fmt.Errorf(format, values...),
	}
}
