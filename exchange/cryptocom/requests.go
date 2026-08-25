package cryptocom

import (
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
)

var instrumentNamePattern = regexp.MustCompile(`^[A-Z0-9]{1,20}_[A-Z0-9]{1,20}$`)

var orderIDPattern = regexp.MustCompile(`^[0-9]{1,19}$`)

var positiveDecimalPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$`)

// OrderBookRequest는 Spot 호가 snapshot 조회 조건이다.
type OrderBookRequest struct {
	InstrumentName string
	Depth          int
}

// TradesRequest는 최근 공개 Spot 체결 조회 조건이다.
type TradesRequest struct {
	InstrumentName string
	Count          int
	Start          *time.Time
	End            *time.Time
}

// CandleTimeframe은 Crypto.com Spot 캔들 구간이다.
type CandleTimeframe string

const (
	Candle1Minute   CandleTimeframe = "1m"
	Candle5Minutes  CandleTimeframe = "5m"
	Candle15Minutes CandleTimeframe = "15m"
	Candle30Minutes CandleTimeframe = "30m"
	Candle1Hour     CandleTimeframe = "1h"
	Candle2Hours    CandleTimeframe = "2h"
	Candle4Hours    CandleTimeframe = "4h"
	Candle12Hours   CandleTimeframe = "12h"
	Candle1Day      CandleTimeframe = "1D"
	Candle7Days     CandleTimeframe = "7D"
	Candle14Days    CandleTimeframe = "14D"
	Candle1Month    CandleTimeframe = "1M"
)

// CandlesRequest는 시간 범위와 개수 기반 Spot OHLCV 조회 조건이다.
type CandlesRequest struct {
	InstrumentName string
	Timeframe      CandleTimeframe
	Count          int
	Start          *time.Time
	End            *time.Time
}

// OrderSide는 Crypto.com Spot 주문의 매수·매도 방향이다.
type OrderSide string

const (
	OrderSideBuy  OrderSide = "BUY"
	OrderSideSell OrderSide = "SELL"
)

// OrderType은 첫 구현 범위의 Crypto.com Spot 주문 종류다.
type OrderType string

const (
	OrderTypeLimit  OrderType = "LIMIT"
	OrderTypeMarket OrderType = "MARKET"
)

// TimeInForce는 Crypto.com Spot limit 주문의 체결·유효 정책이다.
type TimeInForce string

const (
	TimeInForceGoodTillCancel    TimeInForce = "GOOD_TILL_CANCEL"
	TimeInForceImmediateOrCancel TimeInForce = "IMMEDIATE_OR_CANCEL"
	TimeInForceFillOrKill        TimeInForce = "FILL_OR_KILL"
)

// PlaceOrderRequest는 사용자 주문 ID를 강제하는 Crypto.com Spot 주문 조건이다.
type PlaceOrderRequest struct {
	InstrumentName string
	Side           OrderSide
	Type           OrderType
	Price          string
	Quantity       string
	Notional       string
	ClientOrderID  string
	TimeInForce    TimeInForce
	PostOnly       bool
}

// OrderInfoRequest는 거래소 주문 ID 또는 사용자 주문 ID로 조회 대상을 지정한다.
type OrderInfoRequest struct {
	OrderID       string
	ClientOrderID string
}

// CancelOrderRequest는 거래소 주문 ID 또는 사용자 주문 ID로 취소 대상을 지정한다.
type CancelOrderRequest struct {
	OrderID       string
	ClientOrderID string
}

// OpenOrdersRequest는 선택적 Spot 거래쌍으로 현재 미체결 주문을 필터링한다.
type OpenOrdersRequest struct {
	InstrumentName string
}

// OrderHistoryRequest는 종료 주문 이력의 거래쌍·시간 범위·개수를 지정한다.
type OrderHistoryRequest struct {
	InstrumentName string
	Start          *time.Time
	End            *time.Time
	Limit          int
}

// AccountTradesRequest는 계정 체결 이력의 거래쌍·시간 범위·개수를 지정한다.
type AccountTradesRequest struct {
	InstrumentName string
	Start          *time.Time
	End            *time.Time
	Limit          int
}

func (request OrderBookRequest) validate() error {
	if err := validateInstrumentName(request.InstrumentName); err != nil {
		return err
	}
	if request.Depth < 1 || request.Depth > 50 {
		return validationError("order book depth must be between 1 and 50")
	}
	return nil
}

func (request OrderBookRequest) values() url.Values {
	return url.Values{
		"instrument_name": {request.InstrumentName},
		"depth":           {strconv.Itoa(request.Depth)},
	}
}

func (request TradesRequest) validate() error {
	if err := validateInstrumentName(request.InstrumentName); err != nil {
		return err
	}
	if request.Count < 0 {
		return validationError("trade count must be positive or zero")
	}
	return validateTimeRange(request.Start, request.End)
}

func (request TradesRequest) values() url.Values {
	values := url.Values{"instrument_name": {request.InstrumentName}}
	setPositiveInt(values, "count", request.Count)
	setTimeRange(values, request.Start, request.End)
	return values
}

func (request CandlesRequest) validate() error {
	if err := validateInstrumentName(request.InstrumentName); err != nil {
		return err
	}
	if !request.Timeframe.valid() {
		return validationError("unsupported Crypto.com candle timeframe %q", request.Timeframe)
	}
	if request.Count < 0 {
		return validationError("candle count must be positive or zero")
	}
	return validateTimeRange(request.Start, request.End)
}

func (request CandlesRequest) values() url.Values {
	values := url.Values{
		"instrument_name": {request.InstrumentName},
		"timeframe":       {string(request.Timeframe)},
	}
	setPositiveInt(values, "count", request.Count)
	setTimeRange(values, request.Start, request.End)
	return values
}

func (request PlaceOrderRequest) validate() error {
	if err := validateInstrumentName(request.InstrumentName); err != nil {
		return err
	}
	if request.Side != OrderSideBuy && request.Side != OrderSideSell {
		return validationError("unsupported Crypto.com order side %q", request.Side)
	}
	if request.Type != OrderTypeLimit && request.Type != OrderTypeMarket {
		return validationError("unsupported Crypto.com order type %q", request.Type)
	}
	if err := validateClientOrderID(request.ClientOrderID); err != nil {
		return err
	}
	if request.Type == OrderTypeLimit {
		if err := validatePositiveDecimal("order price", request.Price); err != nil {
			return err
		}
		if err := validatePositiveDecimal("order quantity", request.Quantity); err != nil {
			return err
		}
		if request.Notional != "" {
			return validationError("limit order notional must be empty")
		}
		if request.TimeInForce != "" && !request.TimeInForce.valid() {
			return validationError("unsupported Crypto.com time in force %q", request.TimeInForce)
		}
		if request.PostOnly && request.TimeInForce != "" &&
			request.TimeInForce != TimeInForceGoodTillCancel {
			return validationError("post-only order must use GOOD_TILL_CANCEL or omit time in force")
		}
		return nil
	}
	if request.Price != "" || request.TimeInForce != "" || request.PostOnly {
		return validationError("market order cannot set price, time in force, or post-only")
	}
	if request.Side == OrderSideBuy {
		if request.Quantity != "" {
			return validationError("market buy quantity must be empty; use notional")
		}
		return validatePositiveDecimal("market buy notional", request.Notional)
	}
	if request.Notional != "" {
		return validationError("market sell notional must be empty; use quantity")
	}
	return validatePositiveDecimal("market sell quantity", request.Quantity)
}

func (request PlaceOrderRequest) params() map[string]any {
	params := map[string]any{
		"instrument_name": request.InstrumentName,
		"side":            string(request.Side),
		"type":            string(request.Type),
		"client_oid":      request.ClientOrderID,
	}
	setStringParam(params, "price", request.Price)
	setStringParam(params, "quantity", request.Quantity)
	setStringParam(params, "notional", request.Notional)
	setStringParam(params, "time_in_force", string(request.TimeInForce))
	if request.PostOnly {
		params["exec_inst"] = []string{"POST_ONLY"}
	}
	return params
}

func (request OrderInfoRequest) validate() error {
	return validateOrderIdentity(request.OrderID, request.ClientOrderID)
}

func (request OrderInfoRequest) params() map[string]any {
	return orderIdentityParams(request.OrderID, request.ClientOrderID)
}

func (request CancelOrderRequest) validate() error {
	return validateOrderIdentity(request.OrderID, request.ClientOrderID)
}

func (request CancelOrderRequest) params() map[string]any {
	return orderIdentityParams(request.OrderID, request.ClientOrderID)
}

func (request OpenOrdersRequest) validate() error {
	return validateOptionalInstrumentName(request.InstrumentName)
}

func (request OpenOrdersRequest) params() map[string]any {
	params := make(map[string]any)
	setStringParam(params, "instrument_name", request.InstrumentName)
	return params
}

func (request OrderHistoryRequest) validate() error {
	if err := validateOptionalInstrumentName(request.InstrumentName); err != nil {
		return err
	}
	return validatePrivateHistory(request.Start, request.End, request.Limit)
}

func (request OrderHistoryRequest) params() map[string]any {
	return privateHistoryParams(request.InstrumentName, request.Start, request.End, request.Limit)
}

func (request AccountTradesRequest) validate() error {
	if err := validateOptionalInstrumentName(request.InstrumentName); err != nil {
		return err
	}
	return validatePrivateHistory(request.Start, request.End, request.Limit)
}

func (request AccountTradesRequest) params() map[string]any {
	return privateHistoryParams(request.InstrumentName, request.Start, request.End, request.Limit)
}

func (timeframe CandleTimeframe) valid() bool {
	switch timeframe {
	case Candle1Minute, Candle5Minutes, Candle15Minutes, Candle30Minutes,
		Candle1Hour, Candle2Hours, Candle4Hours, Candle12Hours,
		Candle1Day, Candle7Days, Candle14Days, Candle1Month:
		return true
	default:
		return false
	}
}

func (value TimeInForce) valid() bool {
	switch value {
	case TimeInForceGoodTillCancel, TimeInForceImmediateOrCancel, TimeInForceFillOrKill:
		return true
	default:
		return false
	}
}

func validateInstrumentName(value string) error {
	if !instrumentNamePattern.MatchString(value) {
		return validationError("invalid Crypto.com instrument name %q", value)
	}
	return nil
}

func validateOptionalInstrumentName(value string) error {
	if value == "" {
		return nil
	}
	return validateInstrumentName(value)
}

func validateClientOrderID(value string) error {
	if value == "" || len(value) > 36 || !utf8.ValidString(value) ||
		strings.TrimSpace(value) != value {
		return validationError("Crypto.com client order ID must contain 1 to 36 unpadded bytes")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return validationError("Crypto.com client order ID cannot contain control characters")
		}
	}
	return nil
}

func validateOrderIdentity(orderID, clientOrderID string) error {
	if (orderID == "") == (clientOrderID == "") {
		return validationError("exactly one order ID or client order ID is required")
	}
	if orderID != "" {
		if !orderIDPattern.MatchString(orderID) {
			return validationError("invalid Crypto.com order ID %q", orderID)
		}
		if _, err := strconv.ParseUint(orderID, 10, 63); err != nil {
			return validationError("Crypto.com order ID exceeds int64 range")
		}
	}
	if clientOrderID != "" {
		return validateClientOrderID(clientOrderID)
	}
	return nil
}

func validatePositiveDecimal(name, value string) error {
	if !positiveDecimalPattern.MatchString(value) {
		return validationError("%s must be a positive decimal string", name)
	}
	number, ok := new(big.Rat).SetString(value)
	if !ok || number.Sign() <= 0 {
		return validationError("%s must be greater than zero", name)
	}
	return nil
}

func validatePrivateHistory(start, end *time.Time, limit int) error {
	if err := validateTimeRange(start, end); err != nil {
		return err
	}
	if start != nil && start.UnixNano() <= 0 || end != nil && end.UnixNano() <= 0 {
		return validationError("private history timestamps exceed the supported nanosecond range")
	}
	if limit < 0 || limit > 100 {
		return validationError("private history limit must be between 1 and 100 or zero")
	}
	return nil
}

func privateHistoryParams(instrumentName string, start, end *time.Time, limit int) map[string]any {
	params := make(map[string]any)
	setStringParam(params, "instrument_name", instrumentName)
	if start != nil {
		params["start_time"] = strconv.FormatInt(start.UnixNano(), 10)
	}
	if end != nil {
		params["end_time"] = strconv.FormatInt(end.UnixNano(), 10)
	}
	if limit > 0 {
		params["limit"] = strconv.Itoa(limit)
	}
	return params
}

func orderIdentityParams(orderID, clientOrderID string) map[string]any {
	params := make(map[string]any)
	setStringParam(params, "order_id", orderID)
	setStringParam(params, "client_oid", clientOrderID)
	return params
}

func setStringParam(params map[string]any, key, value string) {
	if value != "" {
		params[key] = value
	}
}

func validateTimeRange(start, end *time.Time) error {
	if start != nil && start.UnixMilli() <= 0 || end != nil && end.UnixMilli() <= 0 {
		return validationError("query timestamps must be after the Unix epoch")
	}
	if start != nil && end != nil && !start.Before(*end) {
		return validationError("query start must be before end")
	}
	return nil
}

func setTimeRange(values url.Values, start, end *time.Time) {
	if start != nil {
		values.Set("start_ts", strconv.FormatInt(start.UnixMilli(), 10))
	}
	if end != nil {
		values.Set("end_ts", strconv.FormatInt(end.UnixMilli(), 10))
	}
}

func setPositiveInt(values url.Values, key string, value int) {
	if value > 0 {
		values.Set(key, strconv.Itoa(value))
	}
}

func validationError(format string, arguments ...any) error {
	return &trade.APIError{
		Category: trade.ErrorValidation, Exchange: model.ExchangeCryptoCom,
		Cause: fmt.Errorf(format, arguments...),
	}
}
