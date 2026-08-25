package mexc

import (
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
)

var (
	symbolPattern        = regexp.MustCompile(`^[A-Z0-9]{2,40}$`)
	clientOrderIDPattern = regexp.MustCompile(`^[0-9A-Za-z_-]{1,32}$`)
	orderIDPattern       = regexp.MustCompile(`^[0-9A-Za-z_-]{1,64}$`)
	decimalPattern       = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
)

// ExchangeInfoRequest는 전체·단일·복수 Spot 거래쌍 규칙 조회 조건이다.
type ExchangeInfoRequest struct {
	Symbol  string
	Symbols []string
}

// OrderBookRequest는 Spot 호가 snapshot 조회 조건이다.
type OrderBookRequest struct {
	Symbol string
	Limit  int
}

// TradesRequest는 최근 공개 Spot 체결 조회 조건이다.
type TradesRequest struct {
	Symbol string
	Limit  int
}

// AggregateTradesRequest는 시간 범위 기반 합산 Spot 체결 조회 조건이다.
type AggregateTradesRequest struct {
	Symbol string
	Start  *time.Time
	End    *time.Time
	Limit  int
}

// CandleInterval은 MEXC Spot V3 캔들 구간이다.
type CandleInterval string

const (
	Candle1Minute   CandleInterval = "1m"
	Candle5Minutes  CandleInterval = "5m"
	Candle15Minutes CandleInterval = "15m"
	Candle30Minutes CandleInterval = "30m"
	Candle1Hour     CandleInterval = "60m"
	Candle4Hours    CandleInterval = "4h"
	Candle1Day      CandleInterval = "1d"
	Candle1Week     CandleInterval = "1W"
	Candle1Month    CandleInterval = "1M"
)

// CandlesRequest는 시간 범위와 개수 기반 Spot OHLCV 조회 조건이다.
type CandlesRequest struct {
	Symbol   string
	Interval CandleInterval
	Start    *time.Time
	End      *time.Time
	Limit    int
}

// PlaceOrderRequest는 MEXC Spot 지정가·시장가 주문 조건이다.
type PlaceOrderRequest struct {
	ClientOrderID string
	Symbol        string
	Side          Side
	Type          OrderType
	Quantity      string
	QuoteQuantity string
	Price         string
}

// OrderInfoRequest는 거래소 주문 ID 또는 사용자 주문 ID로 조회 대상을 지정한다.
type OrderInfoRequest struct {
	Symbol        string
	OrderID       string
	ClientOrderID string
}

// CancelOrderRequest는 거래소 주문 ID 또는 사용자 주문 ID로 취소 대상을 지정한다.
type CancelOrderRequest struct {
	Symbol        string
	OrderID       string
	ClientOrderID string
}

// OpenOrdersRequest는 단일 거래쌍의 현재 미체결 주문 조회 조건이다.
type OpenOrdersRequest struct{ Symbol string }

// AllOrdersRequest는 최대 7일 범위의 전체 주문 이력 조회 조건이다.
type AllOrdersRequest struct {
	Symbol string
	Start  *time.Time
	End    *time.Time
	Limit  int
}

// MyTradesRequest는 최대 한 달 범위의 계정 Spot 체결 조회 조건이다.
type MyTradesRequest struct {
	Symbol  string
	OrderID string
	Start   *time.Time
	End     *time.Time
	Limit   int
}

func (request ExchangeInfoRequest) validate() error {
	if request.Symbol != "" && len(request.Symbols) > 0 {
		return validationError("exchange info accepts symbol or symbols, not both")
	}
	if request.Symbol != "" {
		return validateSymbol(request.Symbol)
	}
	seen := make(map[string]struct{}, len(request.Symbols))
	for _, symbol := range request.Symbols {
		if err := validateSymbol(symbol); err != nil {
			return err
		}
		if _, exists := seen[symbol]; exists {
			return validationError("exchange info symbols contain a duplicate")
		}
		seen[symbol] = struct{}{}
	}
	return nil
}

func (request ExchangeInfoRequest) values() url.Values {
	values := make(url.Values)
	if request.Symbol != "" {
		values.Set("symbol", request.Symbol)
	}
	if len(request.Symbols) > 0 {
		values.Set("symbols", strings.Join(request.Symbols, ","))
	}
	return values
}

func (request OrderBookRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Limit < 0 || request.Limit > 5000 {
		return validationError("order book limit must be between 1 and 5000 or zero")
	}
	return nil
}

func (request OrderBookRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request TradesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	return validateLimit(request.Limit)
}

func (request TradesRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request AggregateTradesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateLimit(request.Limit); err != nil {
		return err
	}
	if (request.Start == nil) != (request.End == nil) {
		return validationError("aggregate trade start and end must be used together")
	}
	if request.Start != nil {
		if request.Start.UnixMilli() <= 0 || request.End.UnixMilli() <= 0 {
			return validationError("aggregate trade timestamps must be after the Unix epoch")
		}
		if request.Start.After(*request.End) {
			return validationError("aggregate trade start cannot be after end")
		}
	}
	return nil
}

func (request AggregateTradesRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	setPositiveInt(values, "limit", request.Limit)
	if request.Start != nil {
		values.Set("startTime", strconv.FormatInt(request.Start.UnixMilli(), 10))
		values.Set("endTime", strconv.FormatInt(request.End.UnixMilli(), 10))
	}
	return values
}

func (request CandlesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if !request.Interval.valid() {
		return validationError("unsupported candle interval %q", request.Interval)
	}
	if err := validateLimit(request.Limit); err != nil {
		return err
	}
	if request.Start != nil && request.Start.UnixMilli() <= 0 ||
		request.End != nil && request.End.UnixMilli() <= 0 {
		return validationError("candle timestamps must be after the Unix epoch")
	}
	if request.Start != nil && request.End != nil && !request.Start.Before(*request.End) {
		return validationError("candle start must be before end")
	}
	return nil
}

func (request CandlesRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}, "interval": {string(request.Interval)}}
	setPositiveInt(values, "limit", request.Limit)
	if request.Start != nil {
		values.Set("startTime", strconv.FormatInt(request.Start.UnixMilli(), 10))
	}
	if request.End != nil {
		values.Set("endTime", strconv.FormatInt(request.End.UnixMilli(), 10))
	}
	return values
}

func (request PlaceOrderRequest) validate() error {
	if !clientOrderIDPattern.MatchString(request.ClientOrderID) {
		return validationError("client order ID is required and must match [0-9A-Za-z_-]{1,32}")
	}
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("order side must be BUY or SELL")
	}
	switch request.Type {
	case OrderTypeLimit, OrderTypeLimitMaker, OrderTypeImmediateOrCancel, OrderTypeFillOrKill:
		if err := validatePositiveDecimal("quantity", request.Quantity); err != nil {
			return err
		}
		if err := validatePositiveDecimal("price", request.Price); err != nil {
			return err
		}
		if request.QuoteQuantity != "" {
			return validationError("priced order does not accept quote quantity")
		}
	case OrderTypeMarket:
		if request.Price != "" {
			return validationError("market order does not accept price")
		}
		if request.Side == SideBuy {
			if request.Quantity != "" {
				return validationError("market buy accepts quote quantity only")
			}
			if err := validatePositiveDecimal("quote quantity", request.QuoteQuantity); err != nil {
				return err
			}
		} else {
			if request.QuoteQuantity != "" {
				return validationError("market sell accepts base quantity only")
			}
			if err := validatePositiveDecimal("quantity", request.Quantity); err != nil {
				return err
			}
		}
	default:
		return validationError("unsupported order type %q", request.Type)
	}
	return nil
}

func (request PlaceOrderRequest) values() url.Values {
	values := url.Values{
		"newClientOrderId": {request.ClientOrderID},
		"side":             {string(request.Side)},
		"symbol":           {request.Symbol},
		"type":             {string(request.Type)},
	}
	setIfNotEmpty(values, "quantity", request.Quantity)
	setIfNotEmpty(values, "quoteOrderQty", request.QuoteQuantity)
	setIfNotEmpty(values, "price", request.Price)
	return values
}

func (request OrderInfoRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	return validateOrderIdentity(request.OrderID, request.ClientOrderID)
}

func (request OrderInfoRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	setIfNotEmpty(values, "orderId", request.OrderID)
	setIfNotEmpty(values, "origClientOrderId", request.ClientOrderID)
	return values
}

func (request CancelOrderRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	return validateOrderIdentity(request.OrderID, request.ClientOrderID)
}

func (request CancelOrderRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	setIfNotEmpty(values, "orderId", request.OrderID)
	setIfNotEmpty(values, "origClientOrderId", request.ClientOrderID)
	return values
}

func (request OpenOrdersRequest) validate() error { return validateSymbol(request.Symbol) }

func (request OpenOrdersRequest) values() url.Values {
	return url.Values{"symbol": {request.Symbol}}
}

func (request AllOrdersRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateLimit(request.Limit); err != nil {
		return err
	}
	return validateTimeRange(request.Start, request.End, 7*24*time.Hour, "order history")
}

func (request AllOrdersRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	setTimeRange(values, request.Start, request.End)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request MyTradesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.OrderID != "" && !orderIDPattern.MatchString(request.OrderID) {
		return validationError("invalid order ID %q", request.OrderID)
	}
	if request.Limit < 0 || request.Limit > 100 {
		return validationError("account trade limit must be between 1 and 100 or zero")
	}
	return validateTimeRange(request.Start, request.End, 31*24*time.Hour, "account trade")
}

func (request MyTradesRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	setIfNotEmpty(values, "orderId", request.OrderID)
	setTimeRange(values, request.Start, request.End)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (interval CandleInterval) valid() bool {
	switch interval {
	case Candle1Minute, Candle5Minutes, Candle15Minutes, Candle30Minutes,
		Candle1Hour, Candle4Hours, Candle1Day, Candle1Week, Candle1Month:
		return true
	default:
		return false
	}
}

func validateSymbol(symbol string) error {
	if !symbolPattern.MatchString(symbol) {
		return validationError("invalid MEXC symbol %q", symbol)
	}
	return nil
}

func validateLimit(limit int) error {
	if limit < 0 || limit > 1000 {
		return validationError("limit must be between 1 and 1000 or zero")
	}
	return nil
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

func setTimeRange(values url.Values, start, end *time.Time) {
	if start != nil {
		values.Set("startTime", strconv.FormatInt(start.UnixMilli(), 10))
	}
	if end != nil {
		values.Set("endTime", strconv.FormatInt(end.UnixMilli(), 10))
	}
}

func validateOrderIdentity(orderID, clientOrderID string) error {
	if (orderID == "") == (clientOrderID == "") {
		return validationError("exactly one of order ID or client order ID is required")
	}
	if orderID != "" && !orderIDPattern.MatchString(orderID) {
		return validationError("invalid order ID %q", orderID)
	}
	if clientOrderID != "" && !clientOrderIDPattern.MatchString(clientOrderID) {
		return validationError("invalid client order ID %q", clientOrderID)
	}
	return nil
}

func validatePositiveDecimal(name, value string) error {
	if !decimalPattern.MatchString(value) {
		return validationError("%s must be a positive decimal", name)
	}
	number, ok := new(big.Rat).SetString(value)
	if !ok || number.Sign() <= 0 {
		return validationError("%s must be a positive decimal", name)
	}
	return nil
}

func validateTimeRange(start, end *time.Time, maximum time.Duration, name string) error {
	if start != nil && start.UnixMilli() <= 0 || end != nil && end.UnixMilli() <= 0 {
		return validationError("%s timestamps must be after the Unix epoch", name)
	}
	if start != nil && end != nil {
		if !start.Before(*end) {
			return validationError("%s start must be before end", name)
		}
		if end.Sub(*start) > maximum {
			return validationError("%s range exceeds %s", name, maximum)
		}
	}
	return nil
}

func validationError(format string, values ...any) error {
	return &trade.APIError{
		Category: trade.ErrorValidation, Exchange: model.ExchangeMEXC,
		Cause: fmt.Errorf(format, values...),
	}
}
