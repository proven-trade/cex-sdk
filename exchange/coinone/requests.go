package coinone

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
)

var (
	currencyPattern    = regexp.MustCompile(`^[A-Z0-9]{2,20}$`)
	decimalPattern     = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
	userOrderIDPattern = regexp.MustCompile(`^[a-z0-9._-]{1,150}$`)
)

// MarketsRequest는 기준 통화별 마켓 조회 조건이다.
type MarketsRequest struct {
	QuoteCurrency string
}

// OrderBookRequest는 호가 스냅샷 조회 조건이다.
type OrderBookRequest struct {
	QuoteCurrency  string
	TargetCurrency string
	Size           int
	OrderBookUnit  string
}

// RecentTradesRequest는 공개 최근 체결 조회 조건이다.
type RecentTradesRequest struct {
	QuoteCurrency  string
	TargetCurrency string
	Size           int
}

// TickerRequest는 개별 마켓 현재가 조회 조건이다.
type TickerRequest struct {
	QuoteCurrency  string
	TargetCurrency string
	AdditionalData bool
}

// CandleInterval은 코인원 캔들 구간이다.
type CandleInterval string

const (
	Candle1Minute   CandleInterval = "1m"
	Candle3Minutes  CandleInterval = "3m"
	Candle5Minutes  CandleInterval = "5m"
	Candle10Minutes CandleInterval = "10m"
	Candle15Minutes CandleInterval = "15m"
	Candle30Minutes CandleInterval = "30m"
	Candle1Hour     CandleInterval = "1h"
	Candle2Hours    CandleInterval = "2h"
	Candle4Hours    CandleInterval = "4h"
	Candle6Hours    CandleInterval = "6h"
	Candle1Day      CandleInterval = "1d"
	Candle1Week     CandleInterval = "1w"
	Candle1Month    CandleInterval = "1mon"
)

// CandlesRequest는 공개 OHLCV 조회 조건이다.
type CandlesRequest struct {
	QuoteCurrency  string
	TargetCurrency string
	Interval       CandleInterval
	Timestamp      int64
	Size           int
}

// PlaceOrderRequest는 시장가·지정가·스탑 지정가 신규 주문이다.
type PlaceOrderRequest struct {
	Side           Side
	QuoteCurrency  string
	TargetCurrency string
	Type           OrderType
	Price          string
	Quantity       string
	Amount         string
	LimitPrice     string
	TriggerPrice   string
	PostOnly       bool
	UserOrderID    string
}

// OrderInfoRequest는 거래소 또는 사용자 주문 ID로 주문 한 건을 조회한다.
type OrderInfoRequest struct {
	OrderID        string
	UserOrderID    string
	QuoteCurrency  string
	TargetCurrency string
}

// CancelOrderRequest는 거래소 또는 사용자 주문 ID로 주문을 취소한다.
type CancelOrderRequest struct {
	OrderID        string
	UserOrderID    string
	QuoteCurrency  string
	TargetCurrency string
}

// ActiveOrdersRequest는 미종료 주문 목록 조회 조건이다.
// 통화쌍 없이 조회하려면 AllMarkets를 명시해야 한다.
type ActiveOrdersRequest struct {
	QuoteCurrency  string
	TargetCurrency string
	AllMarkets     bool
	OrderTypes     []OrderType
}

// CompletedOrdersRequest는 종료 주문의 체결 목록 조회 조건이다.
// 통화쌍 없이 조회하려면 AllMarkets를 명시해야 한다.
type CompletedOrdersRequest struct {
	QuoteCurrency  string
	TargetCurrency string
	AllMarkets     bool
	ToTradeID      string
	Size           int
	From           time.Time
	To             time.Time
}

func (request MarketsRequest) validate() error {
	return validateCurrency("quote currency", request.QuoteCurrency)
}

func (request OrderBookRequest) validate() error {
	if err := validatePair(request.QuoteCurrency, request.TargetCurrency); err != nil {
		return err
	}
	if request.Size != 0 && request.Size != 5 && request.Size != 10 && request.Size != 15 && request.Size != 16 {
		return validationError("order book size must be 5, 10, 15, 16, or zero for default")
	}
	return validateOptionalPositiveDecimal("order book unit", request.OrderBookUnit)
}

func (request OrderBookRequest) values() url.Values {
	values := make(url.Values)
	setPositiveInt(values, "size", request.Size)
	setIfNotEmpty(values, "order_book_unit", request.OrderBookUnit)
	return values
}

func (request RecentTradesRequest) validate() error {
	if err := validatePair(request.QuoteCurrency, request.TargetCurrency); err != nil {
		return err
	}
	if request.Size != 0 && request.Size != 10 && request.Size != 50 && request.Size != 100 && request.Size != 150 && request.Size != 200 {
		return validationError("recent trade size must be 10, 50, 100, 150, 200, or zero for default")
	}
	return nil
}

func (request RecentTradesRequest) values() url.Values {
	values := make(url.Values)
	setPositiveInt(values, "size", request.Size)
	return values
}

func (request TickerRequest) validate() error {
	return validatePair(request.QuoteCurrency, request.TargetCurrency)
}

func (request TickerRequest) values() url.Values {
	values := make(url.Values)
	if request.AdditionalData {
		values.Set("additional_data", "true")
	}
	return values
}

func (request CandlesRequest) validate() error {
	if err := validatePair(request.QuoteCurrency, request.TargetCurrency); err != nil {
		return err
	}
	if !request.Interval.valid() {
		return validationError("unsupported candle interval %q", request.Interval)
	}
	if request.Timestamp < 0 {
		return validationError("candle timestamp cannot be negative")
	}
	if request.Size < 0 || request.Size > 500 {
		return validationError("candle size must be between 1 and 500 or zero for default")
	}
	return nil
}

func (request CandlesRequest) values() url.Values {
	values := url.Values{"interval": {string(request.Interval)}}
	if request.Timestamp > 0 {
		values.Set("timestamp", strconv.FormatInt(request.Timestamp, 10))
	}
	setPositiveInt(values, "size", request.Size)
	return values
}

func (request PlaceOrderRequest) validate() error {
	if err := validatePair(request.QuoteCurrency, request.TargetCurrency); err != nil {
		return err
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("order side must be BUY or SELL")
	}
	if request.UserOrderID != "" && !userOrderIDPattern.MatchString(request.UserOrderID) {
		return validationError("user order ID must contain 1 to 150 lowercase letters, digits, dots, underscores, or hyphens")
	}
	switch request.Type {
	case OrderTypeLimit:
		if err := validatePositiveDecimal("price", request.Price); err != nil {
			return err
		}
		if err := validatePositiveDecimal("quantity", request.Quantity); err != nil {
			return err
		}
		if request.Amount != "" || request.LimitPrice != "" || request.TriggerPrice != "" {
			return validationError("LIMIT order does not accept amount, limit price, or trigger price")
		}
	case OrderTypeMarket:
		if request.Price != "" || request.TriggerPrice != "" || request.PostOnly {
			return validationError("MARKET order does not accept price, trigger price, or post only")
		}
		if err := validateOptionalPositiveDecimal("limit price", request.LimitPrice); err != nil {
			return err
		}
		if request.Side == SideBuy {
			if err := validatePositiveDecimal("amount", request.Amount); err != nil {
				return err
			}
			if request.Quantity != "" {
				return validationError("MARKET BUY order does not accept quantity")
			}
		} else {
			if err := validatePositiveDecimal("quantity", request.Quantity); err != nil {
				return err
			}
			if request.Amount != "" {
				return validationError("MARKET SELL order does not accept amount")
			}
		}
	case OrderTypeStopLimit:
		if request.Amount != "" || request.LimitPrice != "" || request.PostOnly {
			return validationError("STOP_LIMIT order does not accept amount, limit price, or post only")
		}
		if err := validatePositiveDecimal("price", request.Price); err != nil {
			return err
		}
		if err := validatePositiveDecimal("quantity", request.Quantity); err != nil {
			return err
		}
		if err := validatePositiveDecimal("trigger price", request.TriggerPrice); err != nil {
			return err
		}
	default:
		return validationError("order type must be LIMIT, MARKET, or STOP_LIMIT")
	}
	return nil
}

func (request PlaceOrderRequest) fields() payloadFields {
	values := payloadFields{}
	values.addString("side", string(request.Side))
	values.addString("quote_currency", request.QuoteCurrency)
	values.addString("target_currency", request.TargetCurrency)
	values.addString("type", string(request.Type))
	values.addString("price", request.Price)
	values.addString("qty", request.Quantity)
	values.addString("amount", request.Amount)
	values.addString("limit_price", request.LimitPrice)
	values.addString("trigger_price", request.TriggerPrice)
	if request.Type == OrderTypeLimit {
		values.addBool("post_only", request.PostOnly)
	}
	values.addString("user_order_id", request.UserOrderID)
	return values
}

func (request OrderInfoRequest) validate() error {
	if err := validatePair(request.QuoteCurrency, request.TargetCurrency); err != nil {
		return err
	}
	return validateOrderIdentity(request.OrderID, request.UserOrderID)
}

func (request OrderInfoRequest) fields() payloadFields {
	return orderIdentityFields(request.OrderID, request.UserOrderID, request.QuoteCurrency, request.TargetCurrency)
}

func (request CancelOrderRequest) validate() error {
	if err := validatePair(request.QuoteCurrency, request.TargetCurrency); err != nil {
		return err
	}
	return validateOrderIdentity(request.OrderID, request.UserOrderID)
}

func (request CancelOrderRequest) fields() payloadFields {
	return orderIdentityFields(request.OrderID, request.UserOrderID, request.QuoteCurrency, request.TargetCurrency)
}

func (request ActiveOrdersRequest) validate() error {
	if err := validateMarketScope(request.QuoteCurrency, request.TargetCurrency, request.AllMarkets); err != nil {
		return err
	}
	if len(request.OrderTypes) > 2 {
		return validationError("active order types cannot contain more than two items")
	}
	seen := make(map[OrderType]struct{}, len(request.OrderTypes))
	for _, orderType := range request.OrderTypes {
		if orderType != OrderTypeLimit && orderType != OrderTypeStopLimit {
			return validationError("active order types support only LIMIT and STOP_LIMIT")
		}
		if _, exists := seen[orderType]; exists {
			return validationError("active order types cannot contain duplicates")
		}
		seen[orderType] = struct{}{}
	}
	return nil
}

func (request ActiveOrdersRequest) fields() payloadFields {
	values := pairFields(request.QuoteCurrency, request.TargetCurrency)
	types := make([]string, len(request.OrderTypes))
	for index, orderType := range request.OrderTypes {
		types[index] = string(orderType)
	}
	values.addStrings("order_type", types)
	return values
}

func (request CompletedOrdersRequest) validate(now time.Time) error {
	if err := validateMarketScope(request.QuoteCurrency, request.TargetCurrency, request.AllMarkets); err != nil {
		return err
	}
	if request.Size < 1 || request.Size > 100 {
		return validationError("completed order size must be between 1 and 100")
	}
	if request.From.IsZero() || request.To.IsZero() {
		return validationError("completed order from and to are required")
	}
	if request.From.UnixMilli() <= 0 || request.To.UnixMilli() <= 0 {
		return validationError("completed order timestamps must be after the Unix epoch")
	}
	if request.From.After(request.To) {
		return validationError("completed order from cannot be after to")
	}
	if request.To.Sub(request.From) > 90*24*time.Hour {
		return validationError("completed order range cannot exceed 90 days")
	}
	if request.From.After(now) || request.To.After(now) {
		return validationError("completed order range cannot be in the future")
	}
	if strings.TrimSpace(request.ToTradeID) != request.ToTradeID {
		return validationError("invalid completed order trade cursor")
	}
	return nil
}

func (request CompletedOrdersRequest) fields() payloadFields {
	values := pairFields(request.QuoteCurrency, request.TargetCurrency)
	values.addString("to_trade_id", request.ToTradeID)
	values.addInt("size", request.Size)
	values.addInt64("from_ts", request.From.UnixMilli())
	values.addInt64("to_ts", request.To.UnixMilli())
	return values
}

func pairFields(quoteCurrency, targetCurrency string) payloadFields {
	values := payloadFields{}
	values.addString("quote_currency", quoteCurrency)
	values.addString("target_currency", targetCurrency)
	return values
}

func orderIdentityFields(orderID, userOrderID, quoteCurrency, targetCurrency string) payloadFields {
	values := payloadFields{}
	values.addString("order_id", orderID)
	values.addString("user_order_id", userOrderID)
	values.addString("quote_currency", quoteCurrency)
	values.addString("target_currency", targetCurrency)
	return values
}

func validatePair(quoteCurrency, targetCurrency string) error {
	if err := validateCurrency("quote currency", quoteCurrency); err != nil {
		return err
	}
	if err := validateCurrency("target currency", targetCurrency); err != nil {
		return err
	}
	if quoteCurrency == targetCurrency {
		return validationError("quote and target currencies must differ")
	}
	return nil
}

func validateMarketScope(quoteCurrency, targetCurrency string, allMarkets bool) error {
	hasPair := quoteCurrency != "" || targetCurrency != ""
	if !hasPair {
		if !allMarkets {
			return validationError("currency pair is required unless AllMarkets is true")
		}
		return nil
	}
	if allMarkets {
		return validationError("currency pair and AllMarkets cannot be used together")
	}
	return validatePair(quoteCurrency, targetCurrency)
}

func validateCurrency(name, value string) error {
	if !currencyPattern.MatchString(value) {
		return validationError("invalid %s %q", name, value)
	}
	return nil
}

func validateOrderIdentity(orderID, userOrderID string) error {
	if (orderID == "") == (userOrderID == "") {
		return validationError("exactly one of order ID or user order ID is required")
	}
	if strings.TrimSpace(orderID) != orderID || strings.ContainsAny(orderID, "&=") {
		return validationError("invalid order ID")
	}
	if userOrderID != "" && !userOrderIDPattern.MatchString(userOrderID) {
		return validationError("invalid user order ID")
	}
	return nil
}

func validatePositiveDecimal(name, value string) error {
	if !decimalPattern.MatchString(value) || strings.Trim(value, "0.") == "" {
		return validationError("%s must be a positive decimal", name)
	}
	return nil
}

func validateOptionalPositiveDecimal(name, value string) error {
	if value == "" {
		return nil
	}
	return validatePositiveDecimal(name, value)
}

func (interval CandleInterval) valid() bool {
	switch interval {
	case Candle1Minute, Candle3Minutes, Candle5Minutes, Candle10Minutes, Candle15Minutes,
		Candle30Minutes, Candle1Hour, Candle2Hours, Candle4Hours, Candle6Hours,
		Candle1Day, Candle1Week, Candle1Month:
		return true
	default:
		return false
	}
}

func setPositiveInt(values url.Values, key string, value int) {
	if value > 0 {
		values.Set(key, strconv.Itoa(value))
	}
}

func setIfNotEmpty(values url.Values, key, value string) {
	if value != "" {
		values.Set(key, value)
	}
}

func validationError(format string, values ...any) error {
	return &trade.APIError{
		Category: trade.ErrorValidation, Exchange: model.ExchangeCoinone,
		Cause: fmt.Errorf(format, values...),
	}
}
