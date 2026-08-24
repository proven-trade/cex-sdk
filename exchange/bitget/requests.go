package bitget

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
)

var (
	positiveDecimalPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
	clientOrderIDPattern   = regexp.MustCompile(`^[0-9A-Za-z_:#\-+ ]{1,32}$`)
)

// InstrumentsRequest는 상품 규칙 조회 범위다.
type InstrumentsRequest struct {
	Category Category
	Symbol   string
}

// TickersRequest는 현재 시세 조회 범위다.
type TickersRequest struct {
	Category Category
	Symbol   string
}

// OrderBookRequest는 호가 조회 조건이다.
type OrderBookRequest struct {
	Category Category
	Symbol   string
	Limit    int
}

// RecentFillsRequest는 공개 최근 체결 조회 조건이다.
type RecentFillsRequest struct {
	Category Category
	Symbol   string
	Limit    int
}

// CandleInterval은 캔들 구간 문자열이다.
type CandleInterval string

const (
	Candle1Minute   CandleInterval = "1m"
	Candle3Minutes  CandleInterval = "3m"
	Candle5Minutes  CandleInterval = "5m"
	Candle15Minutes CandleInterval = "15m"
	Candle30Minutes CandleInterval = "30m"
	Candle1Hour     CandleInterval = "1H"
	Candle4Hours    CandleInterval = "4H"
	Candle6Hours    CandleInterval = "6H"
	Candle12Hours   CandleInterval = "12H"
	Candle1Day      CandleInterval = "1D"
)

// CandleType은 Futures 캔들의 기준 가격이다.
type CandleType string

const (
	CandleTypeMarket  CandleType = "market"
	CandleTypeMark    CandleType = "mark"
	CandleTypeIndex   CandleType = "index"
	CandleTypePremium CandleType = "premium"
)

// CandlesRequest는 OHLCV 캔들 조회 조건이다.
type CandlesRequest struct {
	Category  Category
	Symbol    string
	Interval  CandleInterval
	StartTime *time.Time
	EndTime   *time.Time
	Type      CandleType
	Limit     int
}

// PlaceOrderRequest는 Spot 또는 Futures 신규 주문이다.
type PlaceOrderRequest struct {
	Category            Category     `json:"category"`
	Symbol              string       `json:"symbol"`
	Quantity            string       `json:"qty"`
	Price               string       `json:"price,omitempty"`
	Side                Side         `json:"side"`
	OrderType           OrderType    `json:"orderType"`
	TimeInForce         TimeInForce  `json:"timeInForce,omitempty"`
	PositionSide        PositionSide `json:"posSide,omitempty"`
	ClientOrderID       string       `json:"clientOid,omitempty"`
	ReduceOnly          string       `json:"reduceOnly,omitempty"`
	SelfTradePrevention string       `json:"stpMode,omitempty"`
}

// CancelOrderRequest는 주문 ID 또는 client order ID로 주문 취소를 요청한다.
type CancelOrderRequest struct {
	OrderID       string   `json:"orderId,omitempty"`
	ClientOrderID string   `json:"clientOid,omitempty"`
	Category      Category `json:"category,omitempty"`
}

// OrderInfoRequest는 주문 ID 또는 client order ID로 단건 주문을 조회한다.
type OrderInfoRequest struct {
	OrderID       string
	ClientOrderID string
}

// OpenOrdersRequest는 미체결 주문 조회 범위다.
// 전체 category 조회는 AllCategories를 명시해야 한다.
type OpenOrdersRequest struct {
	Category      Category
	AllCategories bool
	Symbol        string
	StartTime     *time.Time
	EndTime       *time.Time
	Limit         int
	Cursor        string
}

// OrderHistoryRequest는 최근 90일 이내 주문 이력 조회 조건이다.
type OrderHistoryRequest struct {
	Category  Category
	Symbol    string
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Cursor    string
}

// PositionsRequest는 USDT-M Futures 포지션 조회 범위다.
type PositionsRequest struct {
	Category     Category
	Symbol       string
	PositionSide PositionSide
}

func (request InstrumentsRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	return validateOptionalSymbol(request.Symbol)
}

func (request InstrumentsRequest) values() url.Values {
	return categorySymbolValues(request.Category, request.Symbol)
}

func (request TickersRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	return validateOptionalSymbol(request.Symbol)
}

func (request TickersRequest) values() url.Values {
	return categorySymbolValues(request.Category, request.Symbol)
}

func (request OrderBookRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Limit < 0 || request.Limit > 1000 {
		return validationError("order book limit must be between 1 and 1000 or zero for default")
	}
	return nil
}

func (request OrderBookRequest) values() url.Values {
	values := categorySymbolValues(request.Category, request.Symbol)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request RecentFillsRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Limit < 0 || request.Limit > 100 {
		return validationError("recent fills limit must be between 1 and 100 or zero for default")
	}
	return nil
}

func (request RecentFillsRequest) values() url.Values {
	values := categorySymbolValues(request.Category, request.Symbol)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request CandlesRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if !request.Interval.valid() {
		return validationError("unsupported candle interval %q", request.Interval)
	}
	if request.Type != "" && !request.Type.valid() {
		return validationError("unsupported candle type %q", request.Type)
	}
	if request.Category == CategorySpot && request.Type != "" && request.Type != CandleTypeMarket {
		return validationError("Spot candles only support market price")
	}
	if request.Limit < 0 || request.Limit > 1000 {
		return validationError("candle limit must be between 1 and 1000 or zero for default")
	}
	if request.StartTime != nil && request.EndTime != nil && request.StartTime.After(*request.EndTime) {
		return validationError("candle startTime cannot be after endTime")
	}
	return nil
}

func (request CandlesRequest) values() url.Values {
	values := categorySymbolValues(request.Category, request.Symbol)
	values.Set("interval", string(request.Interval))
	setTime(values, "startTime", request.StartTime)
	setTime(values, "endTime", request.EndTime)
	setIfNotEmpty(values, "type", string(request.Type))
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request PlaceOrderRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("side must be buy or sell")
	}
	if request.OrderType != OrderTypeLimit && request.OrderType != OrderTypeMarket {
		return validationError("orderType must be limit or market")
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
		return validationError("unsupported timeInForce %q", request.TimeInForce)
	}
	if request.OrderType == OrderTypeMarket && request.TimeInForce != "" {
		return validationError("market order does not accept timeInForce")
	}
	if request.ClientOrderID != "" && !clientOrderIDPattern.MatchString(request.ClientOrderID) {
		return validationError("clientOid has an invalid format")
	}
	if request.PositionSide != "" && !request.PositionSide.valid() {
		return validationError("posSide must be long or short")
	}
	if request.Category == CategorySpot && (request.PositionSide != "" || request.ReduceOnly != "") {
		return validationError("Spot order does not accept posSide or reduceOnly")
	}
	if request.ReduceOnly != "" && request.ReduceOnly != "yes" && request.ReduceOnly != "no" {
		return validationError("reduceOnly must be yes or no")
	}
	if request.SelfTradePrevention != "" && !validSTP(request.SelfTradePrevention) {
		return validationError("unsupported stpMode %q", request.SelfTradePrevention)
	}
	return nil
}

func (request CancelOrderRequest) validate() error {
	if err := validateOrderIdentity(request.OrderID, request.ClientOrderID); err != nil {
		return err
	}
	if request.Category != "" {
		return validateCategory(request.Category)
	}
	return nil
}

func (request OrderInfoRequest) validate() error {
	return validateOrderIdentity(request.OrderID, request.ClientOrderID)
}

func (request OrderInfoRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "orderId", request.OrderID)
	setIfNotEmpty(values, "clientOid", request.ClientOrderID)
	return values
}

func (request OpenOrdersRequest) validate() error {
	if request.AllCategories && request.Category != "" {
		return validationError("open orders cannot set category and AllCategories together")
	}
	if !request.AllCategories {
		if err := validateCategory(request.Category); err != nil {
			return err
		}
	}
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return err
	}
	return validatePage(request.StartTime, request.EndTime, 90*24*time.Hour, request.Limit, request.Cursor)
}

func (request OpenOrdersRequest) values() url.Values {
	values := make(url.Values)
	if !request.AllCategories {
		values.Set("category", string(request.Category))
	}
	setPageValues(values, request.Symbol, request.StartTime, request.EndTime, request.Limit, request.Cursor)
	return values
}

func (request OrderHistoryRequest) validate() error {
	if err := validateCategory(request.Category); err != nil {
		return err
	}
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return err
	}
	return validatePage(request.StartTime, request.EndTime, 30*24*time.Hour, request.Limit, request.Cursor)
}

func (request OrderHistoryRequest) values() url.Values {
	values := make(url.Values)
	values.Set("category", string(request.Category))
	setPageValues(values, request.Symbol, request.StartTime, request.EndTime, request.Limit, request.Cursor)
	return values
}

func (request PositionsRequest) validate() error {
	if request.Category != CategoryUSDTFutures {
		return validationError("positions currently support only USDT-FUTURES")
	}
	if err := validateOptionalSymbol(request.Symbol); err != nil {
		return err
	}
	if request.PositionSide != "" && !request.PositionSide.valid() {
		return validationError("posSide must be long or short")
	}
	return nil
}

func (request PositionsRequest) values() url.Values {
	values := categorySymbolValues(request.Category, request.Symbol)
	setIfNotEmpty(values, "posSide", string(request.PositionSide))
	return values
}

func validateCategory(category Category) error {
	if category != CategorySpot && category != CategoryUSDTFutures {
		return validationError("category must be SPOT or USDT-FUTURES")
	}
	return nil
}

func validateSymbol(symbol string) error {
	if strings.TrimSpace(symbol) == "" || strings.TrimSpace(symbol) != symbol {
		return validationError("symbol is required and cannot have surrounding whitespace")
	}
	return nil
}

func validateOptionalSymbol(symbol string) error {
	if symbol == "" {
		return nil
	}
	return validateSymbol(symbol)
}

func validatePositiveDecimal(name, value string) error {
	if !positiveDecimalPattern.MatchString(value) || strings.Trim(value, "0.") == "" {
		return validationError("%s must be a positive decimal string", name)
	}
	return nil
}

func validateOrderIdentity(orderID, clientOrderID string) error {
	if strings.TrimSpace(orderID) == "" && strings.TrimSpace(clientOrderID) == "" {
		return validationError("orderId or clientOid is required")
	}
	if orderID != strings.TrimSpace(orderID) || clientOrderID != strings.TrimSpace(clientOrderID) {
		return validationError("order identity cannot have surrounding whitespace")
	}
	if clientOrderID != "" && !clientOrderIDPattern.MatchString(clientOrderID) {
		return validationError("clientOid has an invalid format")
	}
	return nil
}

func validatePage(start, end *time.Time, maximumRange time.Duration, limit int, cursor string) error {
	if limit < 0 || limit > 100 {
		return validationError("page limit must be between 1 and 100 or zero for default")
	}
	if strings.TrimSpace(cursor) != cursor {
		return validationError("cursor cannot have surrounding whitespace")
	}
	if start != nil && end != nil {
		if start.After(*end) {
			return validationError("startTime cannot be after endTime")
		}
		if end.Sub(*start) > maximumRange {
			return validationError("query time range exceeds %s", maximumRange)
		}
	}
	return nil
}

func categorySymbolValues(category Category, symbol string) url.Values {
	values := make(url.Values)
	values.Set("category", string(category))
	setIfNotEmpty(values, "symbol", symbol)
	return values
}

func setPageValues(values url.Values, symbol string, start, end *time.Time, limit int, cursor string) {
	setIfNotEmpty(values, "symbol", symbol)
	setTime(values, "startTime", start)
	setTime(values, "endTime", end)
	setPositiveInt(values, "limit", limit)
	setIfNotEmpty(values, "cursor", cursor)
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
	case Candle1Minute,
		Candle3Minutes,
		Candle5Minutes,
		Candle15Minutes,
		Candle30Minutes,
		Candle1Hour,
		Candle4Hours,
		Candle6Hours,
		Candle12Hours,
		Candle1Day:
		return true
	default:
		return false
	}
}

func (candleType CandleType) valid() bool {
	return candleType == CandleTypeMarket || candleType == CandleTypeMark ||
		candleType == CandleTypeIndex || candleType == CandleTypePremium
}

func (timeInForce TimeInForce) valid() bool {
	return timeInForce == TimeInForceGTC || timeInForce == TimeInForceIOC ||
		timeInForce == TimeInForceFOK || timeInForce == TimeInForcePostOnly ||
		timeInForce == TimeInForceRPI
}

func (positionSide PositionSide) valid() bool {
	return positionSide == PositionSideLong || positionSide == PositionSideShort
}

func validSTP(value string) bool {
	return value == "none" || value == "cancel_taker" || value == "cancel_maker" || value == "cancel_both"
}

func validationError(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", trade.ErrValidation, fmt.Sprintf(format, arguments...))
}
