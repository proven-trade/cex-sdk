package korbit

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/model"
)

var (
	symbolPattern        = regexp.MustCompile(`^[a-z0-9]+_[a-z0-9]+$`)
	currencyPattern      = regexp.MustCompile(`^[a-z0-9]{2,20}$`)
	positiveDecimalRegex = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
	clientOrderIDPattern = regexp.MustCompile(`^[0-9A-Za-z.:_-]{1,36}$`)
)

// TickersRequest는 일부 또는 전체 거래쌍 현재가 조회 조건이다.
// 전체 조회에는 AllSymbols를 명시해야 한다.
type TickersRequest struct {
	Symbols    []string
	AllSymbols bool
}

// OrderBookRequest는 호가 스냅샷 조회 조건이다.
type OrderBookRequest struct {
	Symbol string
	Level  string
}

// RecentTradesRequest는 공개 최근 체결 조회 조건이다.
type RecentTradesRequest struct {
	Symbol string
	Limit  int
}

// CandleInterval은 코빗 캔들 구간이다.
type CandleInterval string

const (
	Candle1Minute   CandleInterval = "1"
	Candle5Minutes  CandleInterval = "5"
	Candle15Minutes CandleInterval = "15"
	Candle30Minutes CandleInterval = "30"
	Candle1Hour     CandleInterval = "60"
	Candle4Hours    CandleInterval = "240"
	Candle1Day      CandleInterval = "1D"
	Candle1Week     CandleInterval = "1W"
)

// CandlesRequest는 공개 OHLCV 조회 조건이다.
type CandlesRequest struct {
	Symbol   string
	Interval CandleInterval
	Start    *time.Time
	End      *time.Time
	Limit    int
}

// TickSizePolicyRequest는 거래쌍의 호가 단위 정책 조회 조건이다.
type TickSizePolicyRequest struct {
	Symbol string
}

// BalanceRequest는 선택한 하위 계정의 자산 잔고 조회 조건이다.
type BalanceRequest struct {
	AccountSeq int
	Currencies []string
}

// PlaceOrderRequest는 지정가·시장가·최유리호가 신규 주문이다.
type PlaceOrderRequest struct {
	Symbol                 string
	AccountSeq             int
	Side                   Side
	Price                  string
	Qty                    string
	Amount                 string
	OrderType              OrderType
	BestNth                int
	TimeInForce            TimeInForce
	ClientOrderID          string
	PriceProtection        bool
	PriceProtectionPercent int
}

// OrderInfoRequest는 거래소 또는 사용자 주문 ID로 주문 한 건을 조회한다.
type OrderInfoRequest struct {
	Symbol        string
	AccountSeq    int
	OrderID       int64
	ClientOrderID string
}

// CancelOrderRequest는 거래소 또는 사용자 주문 ID로 주문을 취소한다.
type CancelOrderRequest struct {
	Symbol        string
	AccountSeq    int
	OrderID       int64
	ClientOrderID string
}

// OpenOrdersRequest는 거래쌍의 미종료 주문 조회 조건이다.
type OpenOrdersRequest struct {
	Symbol     string
	AccountSeq int
	Limit      int
}

// OrderHistoryRequest는 최근 36시간 주문 이력 조회 조건이다.
type OrderHistoryRequest struct {
	Symbol     string
	AccountSeq int
	Start      *time.Time
	End        *time.Time
	Limit      int
}

// MyTradesRequest는 최근 36시간 내 계정 체결 조회 조건이다.
type MyTradesRequest struct {
	Symbol     string
	AccountSeq int
	Start      *time.Time
	End        *time.Time
	Limit      int
}

func (request TickersRequest) validate() error {
	if len(request.Symbols) == 0 {
		if !request.AllSymbols {
			return validationError("ticker symbols are required unless AllSymbols is true")
		}
		return nil
	}
	if request.AllSymbols {
		return validationError("ticker symbols and AllSymbols cannot be used together")
	}
	return validateSymbols(request.Symbols)
}

func (request TickersRequest) values() url.Values {
	values := make(url.Values)
	if len(request.Symbols) > 0 {
		values.Set("symbol", strings.Join(request.Symbols, ","))
	}
	return values
}

func (request OrderBookRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	return validateOptionalPositiveDecimal("order book level", request.Level)
}

func (request OrderBookRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	setIfNotEmpty(values, "level", request.Level)
	return values
}

func (request RecentTradesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Limit < 0 || request.Limit > 500 {
		return validationError("recent trade limit must be between 1 and 500 or zero for default")
	}
	return nil
}

func (request RecentTradesRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request CandlesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if !request.Interval.valid() {
		return validationError("unsupported candle interval %q", request.Interval)
	}
	if request.Limit < 1 || request.Limit > 200 {
		return validationError("candle limit must be between 1 and 200")
	}
	return validateOptionalTimeRange(request.Start, request.End, time.Time{}, 0)
}

func (request CandlesRequest) values() url.Values {
	values := url.Values{
		"symbol": {request.Symbol}, "interval": {string(request.Interval)},
		"limit": {strconv.Itoa(request.Limit)},
	}
	setTime(values, "start", request.Start)
	setTime(values, "end", request.End)
	return values
}

func (request TickSizePolicyRequest) validate() error {
	return validateSymbol(request.Symbol)
}

func (request TickSizePolicyRequest) values() url.Values {
	return url.Values{"symbol": {request.Symbol}}
}

func (request BalanceRequest) validate() error {
	if err := validateAccountSeq(request.AccountSeq); err != nil {
		return err
	}
	if len(request.Currencies) > 100 {
		return validationError("balance currencies cannot exceed 100 items")
	}
	seen := make(map[string]struct{}, len(request.Currencies))
	for _, currency := range request.Currencies {
		if !currencyPattern.MatchString(currency) {
			return validationError("invalid balance currency %q", currency)
		}
		if _, exists := seen[currency]; exists {
			return validationError("duplicate balance currency %q", currency)
		}
		seen[currency] = struct{}{}
	}
	return nil
}

func (request BalanceRequest) values() url.Values {
	values := make(url.Values)
	setAccountSeq(values, request.AccountSeq)
	if len(request.Currencies) > 0 {
		values.Set("currencies", strings.Join(request.Currencies, ","))
	}
	return values
}

func (request PlaceOrderRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateAccountSeq(request.AccountSeq); err != nil {
		return err
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("order side must be buy or sell")
	}
	if !clientOrderIDPattern.MatchString(request.ClientOrderID) {
		return validationError("client order ID is required and must match [0-9a-zA-Z.:_-]{1,36}")
	}
	if request.PriceProtectionPercent < 0 || request.PriceProtectionPercent > 100 {
		return validationError("price protection percent must be between 1 and 100 or zero for default")
	}
	if !request.PriceProtection && request.PriceProtectionPercent != 0 {
		return validationError("price protection percent requires price protection")
	}
	switch request.OrderType {
	case OrderTypeLimit:
		if err := validatePositiveDecimal("price", request.Price); err != nil {
			return err
		}
		if err := validatePositiveDecimal("quantity", request.Qty); err != nil {
			return err
		}
		if request.Amount != "" || request.BestNth != 0 {
			return validationError("limit order does not accept amount or bestNth")
		}
		if request.TimeInForce != "" && !request.TimeInForce.validLimitOrBest() {
			return validationError("unsupported limit order timeInForce %q", request.TimeInForce)
		}
	case OrderTypeMarket:
		if request.Price != "" || request.BestNth != 0 {
			return validationError("market order does not accept price or bestNth")
		}
		if request.TimeInForce != "" && request.TimeInForce != TimeInForceIOC {
			return validationError("market order timeInForce must be ioc or empty for default")
		}
		if err := validateMarketOrBestSize(request.Side, request.Qty, request.Amount); err != nil {
			return err
		}
	case OrderTypeBest:
		if request.Price != "" {
			return validationError("best order does not accept price")
		}
		if request.BestNth < 1 || request.BestNth > 5 {
			return validationError("best order bestNth must be between 1 and 5")
		}
		if !request.TimeInForce.validLimitOrBest() {
			return validationError("best order requires a supported timeInForce")
		}
		if err := validateMarketOrBestSize(request.Side, request.Qty, request.Amount); err != nil {
			return err
		}
	default:
		return validationError("order type must be limit, market, or best")
	}
	return nil
}

func (request PlaceOrderRequest) values() url.Values {
	values := url.Values{
		"symbol": {request.Symbol}, "side": {string(request.Side)},
		"orderType": {string(request.OrderType)}, "clientOrderId": {request.ClientOrderID},
	}
	setAccountSeq(values, request.AccountSeq)
	setIfNotEmpty(values, "price", request.Price)
	setIfNotEmpty(values, "qty", request.Qty)
	setIfNotEmpty(values, "amt", request.Amount)
	setPositiveInt(values, "bestNth", request.BestNth)
	setIfNotEmpty(values, "timeInForce", string(request.TimeInForce))
	if request.PriceProtection {
		values.Set("pp", "true")
	}
	setPositiveInt(values, "ppPercent", request.PriceProtectionPercent)
	return values
}

func (request OrderInfoRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateAccountSeq(request.AccountSeq); err != nil {
		return err
	}
	return validateOrderIdentity(request.OrderID, request.ClientOrderID)
}

func (request OrderInfoRequest) values() url.Values {
	return orderIdentityValues(request.Symbol, request.AccountSeq, request.OrderID, request.ClientOrderID)
}

func (request CancelOrderRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateAccountSeq(request.AccountSeq); err != nil {
		return err
	}
	return validateOrderIdentity(request.OrderID, request.ClientOrderID)
}

func (request CancelOrderRequest) values() url.Values {
	return orderIdentityValues(request.Symbol, request.AccountSeq, request.OrderID, request.ClientOrderID)
}

func (request OpenOrdersRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateAccountSeq(request.AccountSeq); err != nil {
		return err
	}
	return validateOrderListLimit(request.Limit)
}

func (request OpenOrdersRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	setAccountSeq(values, request.AccountSeq)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request OrderHistoryRequest) validate(now time.Time) error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateAccountSeq(request.AccountSeq); err != nil {
		return err
	}
	if err := validateOrderListLimit(request.Limit); err != nil {
		return err
	}
	return validateOptionalTimeRange(request.Start, request.End, now, 36*time.Hour)
}

func (request OrderHistoryRequest) values() url.Values {
	return historyValues(request.Symbol, request.AccountSeq, request.Start, request.End, request.Limit)
}

func (request MyTradesRequest) validate(now time.Time) error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateAccountSeq(request.AccountSeq); err != nil {
		return err
	}
	if err := validateOrderListLimit(request.Limit); err != nil {
		return err
	}
	return validateOptionalTimeRange(request.Start, request.End, now, 36*time.Hour)
}

func (request MyTradesRequest) values() url.Values {
	return historyValues(request.Symbol, request.AccountSeq, request.Start, request.End, request.Limit)
}

func historyValues(symbol string, accountSeq int, start, end *time.Time, limit int) url.Values {
	values := url.Values{"symbol": {symbol}}
	setAccountSeq(values, accountSeq)
	setTime(values, "startTime", start)
	setTime(values, "endTime", end)
	setPositiveInt(values, "limit", limit)
	return values
}

func orderIdentityValues(symbol string, accountSeq int, orderID int64, clientOrderID string) url.Values {
	values := url.Values{"symbol": {symbol}}
	setAccountSeq(values, accountSeq)
	if orderID > 0 {
		values.Set("orderId", strconv.FormatInt(orderID, 10))
	}
	setIfNotEmpty(values, "clientOrderId", clientOrderID)
	return values
}

func validateSymbols(symbols []string) error {
	if len(symbols) > 100 {
		return validationError("ticker symbols cannot exceed 100 items")
	}
	seen := make(map[string]struct{}, len(symbols))
	for _, symbol := range symbols {
		if err := validateSymbol(symbol); err != nil {
			return err
		}
		if _, exists := seen[symbol]; exists {
			return validationError("duplicate symbol %q", symbol)
		}
		seen[symbol] = struct{}{}
	}
	return nil
}

func validateSymbol(symbol string) error {
	if !symbolPattern.MatchString(symbol) {
		return validationError("invalid symbol %q", symbol)
	}
	base, quote, _ := strings.Cut(symbol, "_")
	if base == quote {
		return validationError("symbol base and quote currencies must differ")
	}
	return nil
}

func validateAccountSeq(accountSeq int) error {
	if accountSeq < 0 {
		return validationError("accountSeq cannot be negative")
	}
	return nil
}

func validateOrderIdentity(orderID int64, clientOrderID string) error {
	if (orderID == 0) == (clientOrderID == "") {
		return validationError("exactly one of order ID or client order ID is required")
	}
	if orderID < 0 {
		return validationError("order ID must be positive")
	}
	if clientOrderID != "" && !clientOrderIDPattern.MatchString(clientOrderID) {
		return validationError("invalid client order ID")
	}
	return nil
}

func validateMarketOrBestSize(side Side, qty, amount string) error {
	if side == SideBuy {
		if err := validatePositiveDecimal("amount", amount); err != nil {
			return err
		}
		if qty != "" {
			return validationError("buy order does not accept quantity")
		}
		return nil
	}
	if err := validatePositiveDecimal("quantity", qty); err != nil {
		return err
	}
	if amount != "" {
		return validationError("sell order does not accept amount")
	}
	return nil
}

func validateOrderListLimit(limit int) error {
	if limit < 0 || limit > 1000 {
		return validationError("order list limit must be between 1 and 1000 or zero for default")
	}
	return nil
}

func validateOptionalTimeRange(start, end *time.Time, now time.Time, maximum time.Duration) error {
	if start != nil && start.UnixMilli() <= 0 || end != nil && end.UnixMilli() <= 0 {
		return validationError("timestamps must be after the Unix epoch")
	}
	if start != nil && end != nil && !start.Before(*end) {
		return validationError("start time must be before end time")
	}
	if !now.IsZero() {
		if start != nil && start.After(now) || end != nil && end.After(now) {
			return validationError("time range cannot be in the future")
		}
		if start != nil && now.Sub(*start) > maximum {
			return validationError("time range cannot start more than 36 hours ago")
		}
		if end != nil && now.Sub(*end) > maximum {
			return validationError("time range cannot end more than 36 hours ago")
		}
	}
	if maximum > 0 && start != nil && end != nil && end.Sub(*start) > maximum {
		return validationError("time range cannot exceed 36 hours")
	}
	return nil
}

func validatePositiveDecimal(name, value string) error {
	if !positiveDecimalRegex.MatchString(value) || strings.Trim(value, "0.") == "" {
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
	case Candle1Minute, Candle5Minutes, Candle15Minutes, Candle30Minutes,
		Candle1Hour, Candle4Hours, Candle1Day, Candle1Week:
		return true
	default:
		return false
	}
}

func (value TimeInForce) validLimitOrBest() bool {
	return value == TimeInForceGTC || value == TimeInForceIOC ||
		value == TimeInForceFOK || value == TimeInForcePostOnly
}

func setAccountSeq(values url.Values, accountSeq int) {
	setPositiveInt(values, "accountSeq", accountSeq)
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

func setTime(values url.Values, key string, value *time.Time) {
	if value != nil {
		values.Set(key, strconv.FormatInt(value.UnixMilli(), 10))
	}
}

func validationError(format string, values ...any) error {
	return &trade.APIError{
		Category: trade.ErrorValidation, Exchange: model.ExchangeKorbit,
		Cause: fmt.Errorf(format, values...),
	}
}
