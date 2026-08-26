package bybit

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
)

var (
	positiveDecimalPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
	orderLinkIDPattern     = regexp.MustCompile(`^[0-9A-Za-z_-]{1,36}$`)
)

// CandleInterval은 Bybit V5 캔들 구간 문자열이다.
type CandleInterval string

const (
	Candle1Minute   CandleInterval = "1"
	Candle3Minutes  CandleInterval = "3"
	Candle5Minutes  CandleInterval = "5"
	Candle15Minutes CandleInterval = "15"
	Candle30Minutes CandleInterval = "30"
	Candle1Hour     CandleInterval = "60"
	Candle2Hours    CandleInterval = "120"
	Candle4Hours    CandleInterval = "240"
	Candle6Hours    CandleInterval = "360"
	Candle12Hours   CandleInterval = "720"
	Candle1Day      CandleInterval = "D"
	Candle1Week     CandleInterval = "W"
	Candle1Month    CandleInterval = "M"
)

// AccountType은 Bybit 계정 구조다.
type AccountType string

const AccountTypeUnified AccountType = "UNIFIED"

// MarketUnit은 Spot 시장가 주문 수량의 기준 자산이다.
type MarketUnit string

const (
	MarketUnitBaseCoin  MarketUnit = "baseCoin"
	MarketUnitQuoteCoin MarketUnit = "quoteCoin"
)

// InstrumentsRequest는 상품 규칙 조회 조건이다.
type InstrumentsRequest struct {
	Category Category
	Symbol   string
	BaseCoin string
	Status   string
	Limit    int
	Cursor   string
}

// TickersRequest는 현재 시세 조회 조건이다.
type TickersRequest struct {
	Category Category
	Symbol   string
}

// OrderBookRequest는 호가 스냅샷 조회 조건이다.
type OrderBookRequest struct {
	Category Category
	Symbol   string
	Limit    int
}

// RecentTradesRequest는 최근 공개 체결 조회 조건이다.
type RecentTradesRequest struct {
	Category Category
	Symbol   string
	Limit    int
}

// CandlesRequest는 OHLCV 캔들 조회 조건이다.
type CandlesRequest struct {
	Category  Category
	Symbol    string
	Interval  CandleInterval
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
}

// WalletBalanceRequest는 통합 계정 잔고 조회 조건이다.
type WalletBalanceRequest struct {
	AccountType AccountType
	Coins       []string
}

// PositionsRequest는 Linear 포지션 조회 조건이다.
type PositionsRequest struct {
	Category   Category
	Symbol     string
	SettleCoin string
	Limit      int
	Cursor     string
}

// PlaceOrderRequest는 Spot 또는 Linear 신규 주문이다.
type PlaceOrderRequest struct {
	Category       Category    `json:"category"`
	Symbol         string      `json:"symbol"`
	Side           Side        `json:"side"`
	OrderType      OrderType   `json:"orderType"`
	Quantity       string      `json:"qty"`
	Price          string      `json:"price,omitempty"`
	TimeInForce    TimeInForce `json:"timeInForce,omitempty"`
	PositionIndex  int         `json:"positionIdx,omitempty"`
	OrderLinkID    string      `json:"orderLinkId,omitempty"`
	ReduceOnly     bool        `json:"reduceOnly,omitempty"`
	CloseOnTrigger bool        `json:"closeOnTrigger,omitempty"`
	MarketUnit     MarketUnit  `json:"marketUnit,omitempty"`
}

// CancelOrderRequest는 주문 ID 또는 주문 연결 ID로 취소를 요청한다.
type CancelOrderRequest struct {
	Category    Category `json:"category"`
	Symbol      string   `json:"symbol"`
	OrderID     string   `json:"orderId,omitempty"`
	OrderLinkID string   `json:"orderLinkId,omitempty"`
}

// OrderInfoRequest는 주문 ID 또는 주문 연결 ID로 단건 주문을 조회한다.
type OrderInfoRequest struct {
	Category    Category
	Symbol      string
	OrderID     string
	OrderLinkID string
}

// OpenOrdersRequest는 미체결 주문 목록 조회 조건이다.
type OpenOrdersRequest struct {
	Category   Category
	Symbol     string
	BaseCoin   string
	SettleCoin string
	Limit      int
	Cursor     string
}

// OrderHistoryRequest는 주문 이력 조회 조건이다.
type OrderHistoryRequest struct {
	Category   Category
	Symbol     string
	BaseCoin   string
	SettleCoin string
	StartTime  *time.Time
	EndTime    *time.Time
	Limit      int
	Cursor     string
}

func (request InstrumentsRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateOptionalText("symbol", request.Symbol); err != nil {
		return err
	}
	if err := validateOptionalText("baseCoin", request.BaseCoin); err != nil {
		return err
	}
	if err := validateOptionalText("status", request.Status); err != nil {
		return err
	}
	return validatePage(request.Limit, 1000, request.Cursor)
}

func (request InstrumentsRequest) values() url.Values {
	values := categoryValues(request.Category)
	setIfNotEmpty(values, "symbol", request.Symbol)
	setIfNotEmpty(values, "baseCoin", request.BaseCoin)
	setIfNotEmpty(values, "status", request.Status)
	setPositiveInt(values, "limit", request.Limit)
	setIfNotEmpty(values, "cursor", request.Cursor)
	return values
}

func (request TickersRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateOptionalText("symbol", request.Symbol); err != nil {
		return err
	}
	return nil
}

func (request TickersRequest) values() url.Values {
	values := categoryValues(request.Category)
	setIfNotEmpty(values, "symbol", request.Symbol)
	return values
}

func (request OrderBookRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateRequiredText("symbol", request.Symbol); err != nil {
		return err
	}
	maximum := 1000
	if request.Limit < 0 || request.Limit > maximum {
		return validationError("order book limit must be between 1 and %d or zero for default", maximum)
	}
	return nil
}

func (request OrderBookRequest) values() url.Values {
	values := categoryValues(request.Category)
	values.Set("symbol", request.Symbol)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request RecentTradesRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateRequiredText("symbol", request.Symbol); err != nil {
		return err
	}
	if request.Limit < 0 || request.Limit > 1000 {
		return validationError("recent trade limit must be between 1 and 1000 or zero for default")
	}
	return nil
}

func (request RecentTradesRequest) values() url.Values {
	values := categoryValues(request.Category)
	values.Set("symbol", request.Symbol)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request CandlesRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateRequiredText("symbol", request.Symbol); err != nil {
		return err
	}
	if !request.Interval.valid() {
		return validationError("unsupported candle interval %q", request.Interval)
	}
	if request.Limit < 0 || request.Limit > 1000 {
		return validationError("candle limit must be between 1 and 1000 or zero for default")
	}
	if request.StartTime != nil && request.EndTime != nil && request.StartTime.After(*request.EndTime) {
		return validationError("candle start time cannot be after end time")
	}
	return nil
}

func (request CandlesRequest) values() url.Values {
	values := categoryValues(request.Category)
	values.Set("symbol", request.Symbol)
	values.Set("interval", string(request.Interval))
	setTime(values, "start", request.StartTime)
	setTime(values, "end", request.EndTime)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request WalletBalanceRequest) validate() error {
	if request.AccountType != "" && request.AccountType != AccountTypeUnified {
		return validationError("account type must be UNIFIED or empty for default")
	}
	seen := make(map[string]struct{}, len(request.Coins))
	for _, coin := range request.Coins {
		if err := validateRequiredText("coin", coin); err != nil {
			return err
		}
		if strings.Contains(coin, ",") {
			return validationError("coin cannot contain a comma")
		}
		if _, exists := seen[coin]; exists {
			return validationError("duplicate coin %q", coin)
		}
		seen[coin] = struct{}{}
	}
	return nil
}

func (request WalletBalanceRequest) values() url.Values {
	values := make(url.Values)
	accountType := request.AccountType
	if accountType == "" {
		accountType = AccountTypeUnified
	}
	values.Set("accountType", string(accountType))
	if len(request.Coins) > 0 {
		values.Set("coin", strings.Join(request.Coins, ","))
	}
	return values
}

func (request PositionsRequest) validate() error {
	if request.Category != CategoryLinear {
		return validationError("positions support only linear category")
	}
	if err := validateOptionalText("symbol", request.Symbol); err != nil {
		return err
	}
	if err := validateOptionalText("settleCoin", request.SettleCoin); err != nil {
		return err
	}
	if request.Symbol == "" && request.SettleCoin == "" {
		return validationError("symbol or settleCoin is required")
	}
	return validatePage(request.Limit, 200, request.Cursor)
}

func (request PositionsRequest) values() url.Values {
	values := categoryValues(request.Category)
	setIfNotEmpty(values, "symbol", request.Symbol)
	setIfNotEmpty(values, "settleCoin", request.SettleCoin)
	setPositiveInt(values, "limit", request.Limit)
	setIfNotEmpty(values, "cursor", request.Cursor)
	return values
}

func (request PlaceOrderRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateRequiredText("symbol", request.Symbol); err != nil {
		return err
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("side must be Buy or Sell")
	}
	if request.OrderType != OrderTypeLimit && request.OrderType != OrderTypeMarket {
		return validationError("order type must be Limit or Market")
	}
	if err := validatePositiveDecimal("qty", request.Quantity); err != nil {
		return err
	}
	if request.OrderType == OrderTypeLimit {
		if err := validatePositiveDecimal("price", request.Price); err != nil {
			return err
		}
	} else if request.Price != "" {
		return validationError("market order does not accept price")
	}
	if request.TimeInForce != "" && !request.TimeInForce.valid() {
		return validationError("unsupported time in force %q", request.TimeInForce)
	}
	if request.PositionIndex < 0 || request.PositionIndex > 2 {
		return validationError("position index must be 0, 1, or 2")
	}
	if request.OrderLinkID != "" && !orderLinkIDPattern.MatchString(request.OrderLinkID) {
		return validationError("orderLinkId has an invalid format")
	}
	if request.Category == CategorySpot && (request.PositionIndex != 0 || request.ReduceOnly || request.CloseOnTrigger) {
		return validationError("Spot order does not accept positionIdx, reduceOnly, or closeOnTrigger")
	}
	if request.MarketUnit != "" && request.MarketUnit != MarketUnitBaseCoin && request.MarketUnit != MarketUnitQuoteCoin {
		return validationError("market unit must be baseCoin or quoteCoin")
	}
	if request.MarketUnit != "" && (request.Category != CategorySpot || request.OrderType != OrderTypeMarket) {
		return validationError("market unit is available only for Spot market orders")
	}
	return nil
}

func (request CancelOrderRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateRequiredText("symbol", request.Symbol); err != nil {
		return err
	}
	return validateOrderIdentity(request.OrderID, request.OrderLinkID)
}

func (request OrderInfoRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateOptionalText("symbol", request.Symbol); err != nil {
		return err
	}
	return validateOrderIdentity(request.OrderID, request.OrderLinkID)
}

func (request OrderInfoRequest) values() url.Values {
	values := categoryValues(request.Category)
	setIfNotEmpty(values, "symbol", request.Symbol)
	setIfNotEmpty(values, "orderId", request.OrderID)
	setIfNotEmpty(values, "orderLinkId", request.OrderLinkID)
	return values
}

func (request OpenOrdersRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateOrderFilters(request.Symbol, request.BaseCoin, request.SettleCoin); err != nil {
		return err
	}
	if request.Category == CategorySpot && (request.BaseCoin != "" || request.SettleCoin != "") {
		return validationError("Spot open orders support only symbol filter")
	}
	if request.Category == CategoryLinear && request.Symbol == "" &&
		request.BaseCoin == "" && request.SettleCoin == "" {
		return validationError("linear open orders require symbol, baseCoin, or settleCoin")
	}
	return validatePage(request.Limit, 50, request.Cursor)
}

func (request OpenOrdersRequest) values() url.Values {
	values := orderFilterValues(request.Category, request.Symbol, request.BaseCoin, request.SettleCoin)
	setPositiveInt(values, "limit", request.Limit)
	setIfNotEmpty(values, "cursor", request.Cursor)
	return values
}

func (request OrderHistoryRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateOrderFilters(request.Symbol, request.BaseCoin, request.SettleCoin); err != nil {
		return err
	}
	if request.StartTime != nil && request.EndTime != nil && request.StartTime.After(*request.EndTime) {
		return validationError("order history start time cannot be after end time")
	}
	if request.StartTime != nil && request.EndTime != nil && request.EndTime.Sub(*request.StartTime) > 7*24*time.Hour {
		return validationError("order history time range cannot exceed 7 days")
	}
	return validatePage(request.Limit, 50, request.Cursor)
}

func (request OrderHistoryRequest) values() url.Values {
	values := orderFilterValues(request.Category, request.Symbol, request.BaseCoin, request.SettleCoin)
	setTime(values, "startTime", request.StartTime)
	setTime(values, "endTime", request.EndTime)
	setPositiveInt(values, "limit", request.Limit)
	setIfNotEmpty(values, "cursor", request.Cursor)
	return values
}

func validateCategory(category Category) error {
	if category != CategorySpot && category != CategoryLinear {
		return validationError("category must be spot or linear")
	}
	return nil
}

func validateRequiredText(name, value string) error {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return validationError("%s is required and cannot have surrounding whitespace", name)
	}
	return nil
}

func validateOptionalText(name, value string) error {
	if value == "" {
		return nil
	}
	return validateRequiredText(name, value)
}

func validatePositiveDecimal(name, value string) error {
	if !positiveDecimalPattern.MatchString(value) || strings.Trim(value, "0.") == "" {
		return validationError("%s must be a positive decimal string", name)
	}
	return nil
}

func validateOrderIdentity(orderID, orderLinkID string) error {
	if err := validateOptionalText("orderId", orderID); err != nil {
		return err
	}
	if err := validateOptionalText("orderLinkId", orderLinkID); err != nil {
		return err
	}
	if orderID == "" && orderLinkID == "" {
		return validationError("orderId or orderLinkId is required")
	}
	if orderLinkID != "" && !orderLinkIDPattern.MatchString(orderLinkID) {
		return validationError("orderLinkId has an invalid format")
	}
	return nil
}

func validateOrderFilters(symbol, baseCoin, settleCoin string) error {
	if err := validateOptionalText("symbol", symbol); err != nil {
		return err
	}
	if err := validateOptionalText("baseCoin", baseCoin); err != nil {
		return err
	}
	return validateOptionalText("settleCoin", settleCoin)
}

func validatePage(limit, maximum int, cursor string) error {
	if limit < 0 || limit > maximum {
		return validationError("page limit must be between 1 and %d or zero for default", maximum)
	}
	return validateOptionalText("cursor", cursor)
}

func categoryValues(category Category) url.Values {
	values := make(url.Values)
	values.Set("category", string(category))
	return values
}

func orderFilterValues(category Category, symbol, baseCoin, settleCoin string) url.Values {
	values := categoryValues(category)
	setIfNotEmpty(values, "symbol", symbol)
	setIfNotEmpty(values, "baseCoin", baseCoin)
	setIfNotEmpty(values, "settleCoin", settleCoin)
	return values
}

func setIfNotEmpty(values url.Values, key, value string) {
	if value != "" {
		values.Set(key, value)
	}
}

func setPositiveInt(values url.Values, key string, value int) {
	if value > 0 {
		values.Set(key, strconv.Itoa(value))
	}
}

func setTime(values url.Values, key string, value *time.Time) {
	if value != nil {
		values.Set(key, strconv.FormatInt(value.UnixMilli(), 10))
	}
}

func (interval CandleInterval) valid() bool {
	switch interval {
	case Candle1Minute, Candle3Minutes, Candle5Minutes, Candle15Minutes, Candle30Minutes,
		Candle1Hour, Candle2Hours, Candle4Hours, Candle6Hours, Candle12Hours,
		Candle1Day, Candle1Week, Candle1Month:
		return true
	default:
		return false
	}
}

func (timeInForce TimeInForce) valid() bool {
	return timeInForce == TimeInForceGTC || timeInForce == TimeInForceIOC ||
		timeInForce == TimeInForceFOK || timeInForce == TimeInForcePostOnly
}

func validationError(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", trade.ErrValidation, fmt.Sprintf(format, arguments...))
}
