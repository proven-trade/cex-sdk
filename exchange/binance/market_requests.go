package binance

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// OrderBookRequest는 호가 조회 조건이다.
type OrderBookRequest struct {
	Symbol string
	Limit  int
}

func (request OrderBookRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Limit < 0 || request.Limit > 5000 {
		return validationError("order book limit must be between 1 and 5000 or zero for default")
	}
	return nil
}

func (request OrderBookRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	if request.Limit > 0 {
		values.Set("limit", strconv.Itoa(request.Limit))
	}
	return values
}

func (request OrderBookRequest) weight() int {
	switch {
	case request.Limit == 0 || request.Limit <= 100:
		return 5
	case request.Limit <= 500:
		return 25
	case request.Limit <= 1000:
		return 50
	default:
		return 250
	}
}

// RecentTradesRequest는 최근 공개 체결 조회 조건이다.
type RecentTradesRequest struct {
	Symbol string
	Limit  int
}

func (request RecentTradesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Limit < 0 || request.Limit > 1000 {
		return validationError("recent trades limit must be between 1 and 1000 or zero for default")
	}
	return nil
}

func (request RecentTradesRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	if request.Limit > 0 {
		values.Set("limit", strconv.Itoa(request.Limit))
	}
	return values
}

// BookTickerRequest는 최우선 호가를 조회할 단일 상품을 지정한다.
type BookTickerRequest struct {
	Symbol string
}

// KlinesRequest는 OHLCV 캔들 조회 조건이다.
type KlinesRequest struct {
	Symbol    string
	Interval  KlineInterval
	StartTime *time.Time
	EndTime   *time.Time
	TimeZone  string
	Limit     int
}

func (request KlinesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if !request.Interval.valid() {
		return validationError("unsupported kline interval %q", request.Interval)
	}
	if request.Limit < 0 || request.Limit > 1000 {
		return validationError("kline limit must be between 1 and 1000 or zero for default")
	}
	if request.StartTime != nil && request.EndTime != nil && request.StartTime.After(*request.EndTime) {
		return validationError("kline startTime cannot be after endTime")
	}
	if request.TimeZone != "" && strings.TrimSpace(request.TimeZone) != request.TimeZone {
		return validationError("kline timeZone cannot have surrounding whitespace")
	}
	return nil
}

func (request KlinesRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	values.Set("interval", string(request.Interval))
	if request.StartTime != nil {
		values.Set("startTime", strconv.FormatInt(request.StartTime.UnixMilli(), 10))
	}
	if request.EndTime != nil {
		values.Set("endTime", strconv.FormatInt(request.EndTime.UnixMilli(), 10))
	}
	setIfNotEmpty(values, "timeZone", request.TimeZone)
	if request.Limit > 0 {
		values.Set("limit", strconv.Itoa(request.Limit))
	}
	return values
}

func (interval KlineInterval) valid() bool {
	switch interval {
	case Kline1Second,
		Kline1Minute,
		Kline3Minutes,
		Kline5Minutes,
		Kline15Minutes,
		Kline30Minutes,
		Kline1Hour,
		Kline2Hours,
		Kline4Hours,
		Kline6Hours,
		Kline8Hours,
		Kline12Hours,
		Kline1Day,
		Kline3Days,
		Kline1Week,
		Kline1Month:
		return true
	default:
		return false
	}
}

// AggregateTradesRequest는 집계 체결 조회 조건이다.
// FromID, StartTime, EndTime이 모두 없으면 가장 최근 집계 체결을 반환한다.
type AggregateTradesRequest struct {
	Symbol    string
	FromID    *int64
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
}

func (request AggregateTradesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.FromID != nil && *request.FromID < 0 {
		return validationError("aggTrades fromId cannot be negative")
	}
	if request.Limit < 0 || request.Limit > 1000 {
		return validationError("aggTrades limit must be between 1 and 1000 or zero for default")
	}
	if request.StartTime != nil && request.EndTime != nil && request.StartTime.After(*request.EndTime) {
		return validationError("aggTrades startTime cannot be after endTime")
	}
	return nil
}

func (request AggregateTradesRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	setInt64(values, "fromId", request.FromID)
	setTimeMillis(values, "startTime", request.StartTime)
	setTimeMillis(values, "endTime", request.EndTime)
	if request.Limit > 0 {
		values.Set("limit", strconv.Itoa(request.Limit))
	}
	return values
}

// HistoricalTradesRequest는 과거 공개 체결 조회 조건이다.
// FromID가 없으면 가장 최근 체결부터 반환한다.
type HistoricalTradesRequest struct {
	Symbol string
	FromID *int64
	Limit  int
}

func (request HistoricalTradesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.FromID != nil && *request.FromID < 0 {
		return validationError("historical trades fromId cannot be negative")
	}
	if request.Limit < 0 || request.Limit > 1000 {
		return validationError("historical trades limit must be between 1 and 1000 or zero for default")
	}
	return nil
}

func (request HistoricalTradesRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	setInt64(values, "fromId", request.FromID)
	if request.Limit > 0 {
		values.Set("limit", strconv.Itoa(request.Limit))
	}
	return values
}

// HistoricalBlockTradesRequest는 블록 체결 이력 조회 조건이다. FromID는 필수다.
type HistoricalBlockTradesRequest struct {
	Symbol string
	FromID int64
	Limit  int
}

func (request HistoricalBlockTradesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.FromID < 0 {
		return validationError("block trades fromId cannot be negative")
	}
	if request.Limit < 0 || request.Limit > 1000 {
		return validationError("block trades limit must be between 1 and 1000 or zero for default")
	}
	return nil
}

func (request HistoricalBlockTradesRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	values.Set("fromId", strconv.FormatInt(request.FromID, 10))
	if request.Limit > 0 {
		values.Set("limit", strconv.Itoa(request.Limit))
	}
	return values
}

// AveragePriceRequest는 현재 평균가를 조회할 단일 상품을 지정한다.
type AveragePriceRequest struct {
	Symbol string
}

// Ticker24HourRequest는 24시간 가격 변동 통계 조회 범위다.
// 전체 상품 조회는 높은 weight를 사용하므로 AllSymbols를 명시해야 한다.
type Ticker24HourRequest struct {
	Symbol       string
	Symbols      []string
	AllSymbols   bool
	Type         TickerType
	SymbolStatus SymbolStatus
}

func (request Ticker24HourRequest) validate() error {
	if err := validateSymbolSelection(request.Symbol, request.Symbols, request.AllSymbols, true, 100); err != nil {
		return err
	}
	return validateTickerOptions(request.Type, request.SymbolStatus)
}

func (request Ticker24HourRequest) values() url.Values {
	values := symbolSelectionValues(request.Symbol, request.Symbols, request.SymbolStatus)
	setIfNotEmpty(values, "type", string(request.Type))
	return values
}

func (request Ticker24HourRequest) weight() int {
	switch {
	case request.Symbol != "":
		return 2
	case request.AllSymbols:
		return 80
	case len(request.Symbols) <= 20:
		return 2
	case len(request.Symbols) <= 100:
		return 40
	default:
		return 80
	}
}

// TradingDayTickerRequest는 거래일 기준 가격 변동 통계 조회 범위다.
type TradingDayTickerRequest struct {
	Symbol       string
	Symbols      []string
	TimeZone     string
	Type         TickerType
	SymbolStatus SymbolStatus
}

func (request TradingDayTickerRequest) validate() error {
	if err := validateSymbolSelection(request.Symbol, request.Symbols, false, false, 100); err != nil {
		return err
	}
	if request.TimeZone != "" && strings.TrimSpace(request.TimeZone) != request.TimeZone {
		return validationError("trading day ticker timeZone cannot have surrounding whitespace")
	}
	return validateTickerOptions(request.Type, request.SymbolStatus)
}

func (request TradingDayTickerRequest) values() url.Values {
	values := symbolSelectionValues(request.Symbol, request.Symbols, request.SymbolStatus)
	setIfNotEmpty(values, "timeZone", request.TimeZone)
	setIfNotEmpty(values, "type", string(request.Type))
	return values
}

func (request TradingDayTickerRequest) weight() int {
	return symbolWindowWeight(request.Symbol, request.Symbols)
}

// RollingTickerRequest는 rolling window 가격 변동 통계 조회 범위다.
// WindowSize는 1m-59m, 1h-23h, 1d-7d 중 하나이며 비우면 서버 기본값 1d를 쓴다.
type RollingTickerRequest struct {
	Symbol       string
	Symbols      []string
	WindowSize   string
	Type         TickerType
	SymbolStatus SymbolStatus
}

func (request RollingTickerRequest) validate() error {
	if err := validateSymbolSelection(request.Symbol, request.Symbols, false, false, 100); err != nil {
		return err
	}
	if err := validateWindowSize(request.WindowSize); err != nil {
		return err
	}
	return validateTickerOptions(request.Type, request.SymbolStatus)
}

func (request RollingTickerRequest) values() url.Values {
	values := symbolSelectionValues(request.Symbol, request.Symbols, request.SymbolStatus)
	setIfNotEmpty(values, "windowSize", request.WindowSize)
	setIfNotEmpty(values, "type", string(request.Type))
	return values
}

func (request RollingTickerRequest) weight() int {
	return symbolWindowWeight(request.Symbol, request.Symbols)
}

// TickerPricesRequest는 하나, 여러 개 또는 전체 상품의 최신 가격 조회 범위다.
type TickerPricesRequest struct {
	Symbol       string
	Symbols      []string
	AllSymbols   bool
	SymbolStatus SymbolStatus
}

func (request TickerPricesRequest) validate() error {
	if err := validateSymbolSelection(request.Symbol, request.Symbols, request.AllSymbols, true, 100); err != nil {
		return err
	}
	return validateTickerOptions("", request.SymbolStatus)
}

func (request TickerPricesRequest) values() url.Values {
	return symbolSelectionValues(request.Symbol, request.Symbols, request.SymbolStatus)
}

func (request TickerPricesRequest) weight() int {
	return symbolSelectionWeight(request.Symbol)
}

// BookTickersRequest는 하나, 여러 개 또는 전체 상품의 최우선 호가 조회 범위다.
type BookTickersRequest struct {
	Symbol       string
	Symbols      []string
	AllSymbols   bool
	SymbolStatus SymbolStatus
}

func (request BookTickersRequest) validate() error {
	if err := validateSymbolSelection(request.Symbol, request.Symbols, request.AllSymbols, true, 100); err != nil {
		return err
	}
	return validateTickerOptions("", request.SymbolStatus)
}

func (request BookTickersRequest) values() url.Values {
	return symbolSelectionValues(request.Symbol, request.Symbols, request.SymbolStatus)
}

func (request BookTickersRequest) weight() int {
	return symbolSelectionWeight(request.Symbol)
}

// ReferencePriceRequest는 기준가격을 조회할 단일 상품을 지정한다.
type ReferencePriceRequest struct {
	Symbol string
}

// ReferencePriceCalculationRequest는 기준가격 산출 방식을 조회할 상품과 상태 조건이다.
type ReferencePriceCalculationRequest struct {
	Symbol       string
	SymbolStatus SymbolStatus
}

func (request ReferencePriceCalculationRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	return validateTickerOptions("", request.SymbolStatus)
}

func (request ReferencePriceCalculationRequest) values() url.Values {
	values := make(url.Values)
	values.Set("symbol", request.Symbol)
	setIfNotEmpty(values, "symbolStatus", string(request.SymbolStatus))
	return values
}

// ExecutionRulesRequest는 실행 규칙 조회 범위다. 세 조건은 함께 쓸 수 없고 모두 비우면 전체 상품을 조회한다.
type ExecutionRulesRequest struct {
	Symbol       string
	Symbols      []string
	SymbolStatus SymbolStatus
}

func (request ExecutionRulesRequest) validate() error {
	selected := 0
	if request.Symbol != "" {
		selected++
	}
	if len(request.Symbols) > 0 {
		selected++
	}
	if request.SymbolStatus != "" {
		selected++
	}
	if selected > 1 {
		return validationError("execution rules accept only one of symbol, symbols, or symbolStatus")
	}
	if request.Symbol != "" {
		return validateSymbol(request.Symbol)
	}
	if len(request.Symbols) > 0 {
		return validateSymbolList(request.Symbols, 100)
	}
	return validateTickerOptions("", request.SymbolStatus)
}

func (request ExecutionRulesRequest) values() url.Values {
	return symbolSelectionValues(request.Symbol, request.Symbols, request.SymbolStatus)
}

func (request ExecutionRulesRequest) weight() int {
	switch {
	case request.Symbol != "":
		return 2
	case len(request.Symbols) > 0:
		return min(2*len(request.Symbols), 40)
	default:
		return 40
	}
}

func (tickerType TickerType) valid() bool {
	return tickerType == "" || tickerType == TickerTypeFull || tickerType == TickerTypeMini
}

func (status SymbolStatus) valid() bool {
	switch status {
	case "", SymbolStatusTrading, SymbolStatusHalt, SymbolStatusBreak, SymbolStatusCancelOnly:
		return true
	default:
		return false
	}
}

func validateTickerOptions(tickerType TickerType, status SymbolStatus) error {
	if !tickerType.valid() {
		return validationError("unsupported ticker type %q", tickerType)
	}
	if !status.valid() {
		return validationError("unsupported symbol status %q", status)
	}
	return nil
}

// validateSymbolSelection은 symbol, symbols, 전체 조회 중 정확히 하나만 지정했는지 검사한다.
func validateSymbolSelection(symbol string, symbols []string, allSymbols, allowAll bool, maximum int) error {
	selected := 0
	if symbol != "" {
		selected++
	}
	if len(symbols) > 0 {
		selected++
	}
	if allSymbols {
		selected++
	}
	if selected != 1 {
		if allowAll {
			return validationError("exactly one of symbol, symbols, or AllSymbols is required")
		}
		return validationError("exactly one of symbol or symbols is required")
	}
	if symbol != "" {
		return validateSymbol(symbol)
	}
	if len(symbols) > 0 {
		return validateSymbolList(symbols, maximum)
	}
	return nil
}

func validateSymbolList(symbols []string, maximum int) error {
	if len(symbols) > maximum {
		return validationError("symbols cannot exceed %d entries", maximum)
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

// validateWindowSize는 rolling window 크기가 1m-59m, 1h-23h, 1d-7d 단일 단위인지 검사한다.
func validateWindowSize(windowSize string) error {
	if windowSize == "" {
		return nil
	}
	if len(windowSize) < 2 {
		return validationError("unsupported windowSize %q", windowSize)
	}
	digits := windowSize[:len(windowSize)-1]
	count, err := strconv.Atoi(digits)
	if err != nil || strconv.Itoa(count) != digits || count < 1 {
		return validationError("unsupported windowSize %q", windowSize)
	}
	maximum := 0
	switch windowSize[len(windowSize)-1] {
	case 'm':
		maximum = 59
	case 'h':
		maximum = 23
	case 'd':
		maximum = 7
	}
	if count > maximum {
		return validationError("unsupported windowSize %q", windowSize)
	}
	return nil
}

// symbolSelectionValues는 symbol, symbols, symbolStatus를 Binance query 이름으로 변환한다.
// symbols는 ["BTCUSDT","BNBUSDT"] 형태의 JSON 배열 문자열이다.
func symbolSelectionValues(symbol string, symbols []string, status SymbolStatus) url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "symbol", symbol)
	if len(symbols) > 0 {
		values.Set("symbols", symbolsParameter(symbols))
	}
	setIfNotEmpty(values, "symbolStatus", string(status))
	return values
}

func symbolsParameter(symbols []string) string {
	encoded, err := json.Marshal(symbols)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// symbolSelectionWeight는 ticker/price와 ticker/bookTicker의 weight다.
// 단일 상품만 2이고 복수 또는 전체 조회는 4다.
func symbolSelectionWeight(symbol string) int {
	if symbol != "" {
		return 2
	}
	return 4
}

// symbolWindowWeight는 tradingDay와 rolling window ticker의 weight다.
// 상품당 4이며 상품이 50개를 넘으면 200으로 제한한다.
func symbolWindowWeight(symbol string, symbols []string) int {
	count := len(symbols)
	if symbol != "" {
		count = 1
	}
	if count > 50 {
		return 200
	}
	return 4 * count
}

func setTimeMillis(values url.Values, key string, value *time.Time) {
	if value != nil {
		values.Set(key, strconv.FormatInt(value.UnixMilli(), 10))
	}
}
