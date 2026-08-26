package kraken

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
	positiveDecimalPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
	pairPattern            = regexp.MustCompile(`^[0-9A-Za-z._/-]+$`)
	transactionIDPattern   = regexp.MustCompile(`^[0-9A-Za-z-]+$`)
	shortClientIDPattern   = regexp.MustCompile(`^[0-9A-Za-z_-]{1,18}$`)
	uuidClientIDPattern    = regexp.MustCompile(`^[0-9A-Fa-f]{8}-?[0-9A-Fa-f]{4}-?[0-9A-Fa-f]{4}-?[0-9A-Fa-f]{4}-?[0-9A-Fa-f]{12}$`)
)

// AssetPairsRequest는 상품 정보 조회 조건이다.
type AssetPairsRequest struct {
	Pairs []string
}

// TickersRequest는 ticker 조회 상품 목록이다.
type TickersRequest struct {
	Pairs []string
}

// OrderBookRequest는 L2 호가 조회 조건이다.
type OrderBookRequest struct {
	Pair  string
	Count int
}

// RecentTradesRequest는 최근 공개 체결 조회 조건이다.
type RecentTradesRequest struct {
	Pair  string
	Since string
	Count int
}

// CandlesRequest는 OHLCV 조회 조건이다.
type CandlesRequest struct {
	Pair     string
	Interval CandleInterval
	Since    int64
}

// PlaceOrderRequest는 Spot 시장가 또는 지정가 주문이다.
type PlaceOrderRequest struct {
	Pair          string
	Side          Side
	OrderType     OrderType
	TimeInForce   TimeInForce
	Volume        string
	Price         string
	ClientOrderID string
	PostOnly      bool
	VolumeInQuote bool
	ValidateOnly  bool
}

// CancelOrderRequest는 거래소 주문 ID 또는 client order ID로 주문을 취소한다.
type CancelOrderRequest struct {
	TransactionID string
	ClientOrderID string
	Pair          string
}

// OrderInfoRequest는 최대 50개 주문을 한 번에 조회한다.
type OrderInfoRequest struct {
	TransactionIDs []string
	IncludeTrades  bool
}

// OpenOrdersRequest는 미체결 주문 조회 조건이다.
type OpenOrdersRequest struct {
	IncludeTrades bool
	ClientOrderID string
}

// CloseTime은 종료 주문의 시간 필터 기준이다.
type CloseTime string

const (
	CloseTimeBoth  CloseTime = "both"
	CloseTimeOpen  CloseTime = "open"
	CloseTimeClose CloseTime = "close"
)

// ClosedOrdersRequest는 종료 주문 목록 조회 조건이다.
type ClosedOrdersRequest struct {
	IncludeTrades bool
	ClientOrderID string
	Start         *time.Time
	End           *time.Time
	Offset        int
	CloseTime     CloseTime
}

// TradeHistoryType은 체결 이력의 포지션 분류다.
type TradeHistoryType string

const (
	TradeHistoryAll             TradeHistoryType = "all"
	TradeHistoryAnyPosition     TradeHistoryType = "any position"
	TradeHistoryClosedPosition  TradeHistoryType = "closed position"
	TradeHistoryClosingPosition TradeHistoryType = "closing position"
	TradeHistoryNoPosition      TradeHistoryType = "no position"
)

// TradesHistoryRequest는 계정 체결 이력 조회 조건이다.
type TradesHistoryRequest struct {
	Type          TradeHistoryType
	IncludeTrades bool
	Start         *time.Time
	End           *time.Time
	Offset        int
}

func (request AssetPairsRequest) validate() error {
	return validatePairs(request.Pairs, false)
}

func (request AssetPairsRequest) values() url.Values {
	return pairListValues(request.Pairs)
}

func (request TickersRequest) validate() error {
	return validatePairs(request.Pairs, false)
}

func (request TickersRequest) values() url.Values {
	return pairListValues(request.Pairs)
}

func (request OrderBookRequest) validate() error {
	if err := validatePair(request.Pair); err != nil {
		return err
	}
	if request.Count < 0 || request.Count > 500 {
		return validationError("order book count must be between 1 and 500 or zero for default")
	}
	return nil
}

func (request OrderBookRequest) values() url.Values {
	values := url.Values{"pair": {request.Pair}}
	setPositiveInt(values, "count", request.Count)
	return values
}

func (request RecentTradesRequest) validate() error {
	if err := validatePair(request.Pair); err != nil {
		return err
	}
	if request.Count < 0 || request.Count > 1000 {
		return validationError("recent trade count must be between 1 and 1000 or zero for default")
	}
	if err := validateOptionalUnsigned("since", request.Since); err != nil {
		return err
	}
	return nil
}

func (request RecentTradesRequest) values() url.Values {
	values := url.Values{"pair": {request.Pair}}
	setIfNotEmpty(values, "since", request.Since)
	setPositiveInt(values, "count", request.Count)
	return values
}

func (request CandlesRequest) validate() error {
	if err := validatePair(request.Pair); err != nil {
		return err
	}
	if !request.Interval.valid() {
		return validationError("unsupported candle interval %d", request.Interval)
	}
	if request.Since < 0 {
		return validationError("candle since cannot be negative")
	}
	return nil
}

func (request CandlesRequest) values() url.Values {
	values := url.Values{
		"pair":     {request.Pair},
		"interval": {strconv.Itoa(int(request.Interval))},
	}
	if request.Since > 0 {
		values.Set("since", strconv.FormatInt(request.Since, 10))
	}
	return values
}

func (request PlaceOrderRequest) validate() error {
	if err := validatePair(request.Pair); err != nil {
		return err
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("order side must be buy or sell")
	}
	if request.OrderType != OrderTypeMarket && request.OrderType != OrderTypeLimit {
		return validationError("first Kraken Spot scope supports market and limit orders only")
	}
	if !isPositiveDecimal(request.Volume) {
		return validationError("order volume must be a positive decimal string")
	}
	if request.OrderType == OrderTypeLimit && !isPositiveDecimal(request.Price) {
		return validationError("limit order price must be a positive decimal string")
	}
	if request.OrderType == OrderTypeMarket && request.Price != "" {
		return validationError("market order cannot include price")
	}
	if request.PostOnly && request.OrderType != OrderTypeLimit {
		return validationError("post-only is available for limit orders only")
	}
	if request.TimeInForce != "" && request.TimeInForce != TimeInForceGTC &&
		request.TimeInForce != TimeInForceIOC && request.TimeInForce != TimeInForceFOK {
		return validationError("unsupported time in force %q", request.TimeInForce)
	}
	if request.PostOnly && request.TimeInForce != "" && request.TimeInForce != TimeInForceGTC {
		return validationError("post-only order time in force must be GTC")
	}
	if request.VolumeInQuote && (request.OrderType != OrderTypeMarket || request.Side != SideBuy) {
		return validationError("quote currency volume is available only for market buy orders")
	}
	return validateClientOrderID(request.ClientOrderID)
}

func (request PlaceOrderRequest) values() url.Values {
	values := url.Values{
		"pair":      {request.Pair},
		"type":      {string(request.Side)},
		"ordertype": {string(request.OrderType)},
		"volume":    {request.Volume},
	}
	setIfNotEmpty(values, "price", request.Price)
	setIfNotEmpty(values, "cl_ord_id", request.ClientOrderID)
	setIfNotEmpty(values, "timeinforce", string(request.TimeInForce))
	flags := make([]string, 0, 2)
	if request.PostOnly {
		flags = append(flags, "post")
	}
	if request.VolumeInQuote {
		flags = append(flags, "viqc")
	}
	if len(flags) > 0 {
		values.Set("oflags", strings.Join(flags, ","))
	}
	if request.ValidateOnly {
		values.Set("validate", "true")
	}
	return values
}

func (request CancelOrderRequest) validate() error {
	if (request.TransactionID == "") == (request.ClientOrderID == "") {
		return validationError("cancel request requires exactly one transaction ID or client order ID")
	}
	if request.TransactionID != "" && !transactionIDPattern.MatchString(request.TransactionID) {
		return validationError("transaction ID contains unsupported characters")
	}
	if err := validateClientOrderID(request.ClientOrderID); err != nil {
		return err
	}
	return validatePair(request.Pair)
}

func (request CancelOrderRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "txid", request.TransactionID)
	setIfNotEmpty(values, "cl_ord_id", request.ClientOrderID)
	setIfNotEmpty(values, "pair", request.Pair)
	return values
}

func (request OrderInfoRequest) validate() error {
	if len(request.TransactionIDs) == 0 || len(request.TransactionIDs) > 50 {
		return validationError("order info requires between 1 and 50 transaction IDs")
	}
	seen := make(map[string]struct{}, len(request.TransactionIDs))
	for _, transactionID := range request.TransactionIDs {
		if !transactionIDPattern.MatchString(transactionID) {
			return validationError("transaction ID contains unsupported characters")
		}
		if _, exists := seen[transactionID]; exists {
			return validationError("duplicate transaction ID %q", transactionID)
		}
		seen[transactionID] = struct{}{}
	}
	return nil
}

func (request OrderInfoRequest) values() url.Values {
	values := url.Values{"txid": {strings.Join(request.TransactionIDs, ",")}}
	setBool(values, "trades", request.IncludeTrades)
	return values
}

func (request OpenOrdersRequest) validate() error {
	return validateClientOrderID(request.ClientOrderID)
}

func (request OpenOrdersRequest) values() url.Values {
	values := make(url.Values)
	setBool(values, "trades", request.IncludeTrades)
	setIfNotEmpty(values, "cl_ord_id", request.ClientOrderID)
	return values
}

func (request ClosedOrdersRequest) validate() error {
	if err := validateClientOrderID(request.ClientOrderID); err != nil {
		return err
	}
	if err := validateTimeRange(request.Start, request.End); err != nil {
		return err
	}
	if request.Offset < 0 {
		return validationError("closed order offset cannot be negative")
	}
	if request.CloseTime != "" && request.CloseTime != CloseTimeBoth &&
		request.CloseTime != CloseTimeOpen && request.CloseTime != CloseTimeClose {
		return validationError("unsupported closed order time filter %q", request.CloseTime)
	}
	return nil
}

func (request ClosedOrdersRequest) values() url.Values {
	values := make(url.Values)
	setBool(values, "trades", request.IncludeTrades)
	setIfNotEmpty(values, "cl_ord_id", request.ClientOrderID)
	setTime(values, "start", request.Start)
	setTime(values, "end", request.End)
	setPositiveInt(values, "ofs", request.Offset)
	setIfNotEmpty(values, "closetime", string(request.CloseTime))
	return values
}

func (request TradesHistoryRequest) validate() error {
	if request.Type != "" && request.Type != TradeHistoryAll && request.Type != TradeHistoryAnyPosition &&
		request.Type != TradeHistoryClosedPosition && request.Type != TradeHistoryClosingPosition &&
		request.Type != TradeHistoryNoPosition {
		return validationError("unsupported trade history type %q", request.Type)
	}
	if err := validateTimeRange(request.Start, request.End); err != nil {
		return err
	}
	if request.Offset < 0 {
		return validationError("trade history offset cannot be negative")
	}
	return nil
}

func (request TradesHistoryRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "type", string(request.Type))
	setBool(values, "trades", request.IncludeTrades)
	setTime(values, "start", request.Start)
	setTime(values, "end", request.End)
	setPositiveInt(values, "ofs", request.Offset)
	return values
}

func (interval CandleInterval) valid() bool {
	switch interval {
	case Candle1Minute, Candle5Minutes, Candle15Minutes, Candle30Minutes, Candle1Hour,
		Candle4Hours, Candle1Day, Candle1Week, Candle15Days:
		return true
	default:
		return false
	}
}

func validatePairs(pairs []string, required bool) error {
	if required && len(pairs) == 0 {
		return validationError("at least one pair is required")
	}
	seen := make(map[string]struct{}, len(pairs))
	for _, pair := range pairs {
		if err := validatePair(pair); err != nil {
			return err
		}
		if _, exists := seen[pair]; exists {
			return validationError("duplicate pair %q", pair)
		}
		seen[pair] = struct{}{}
	}
	return nil
}

func validatePair(pair string) error {
	if err := validateRequiredText("pair", pair); err != nil {
		return err
	}
	if !pairPattern.MatchString(pair) || strings.Contains(pair, "..") {
		return validationError("pair contains unsupported characters")
	}
	return nil
}

func validateClientOrderID(value string) error {
	if value == "" {
		return nil
	}
	if !shortClientIDPattern.MatchString(value) && !uuidClientIDPattern.MatchString(value) {
		return validationError("client order ID must be up to 18 safe characters or a UUID")
	}
	return nil
}

func validateOptionalUnsigned(name, value string) error {
	if value == "" {
		return nil
	}
	if _, err := strconv.ParseUint(value, 10, 64); err != nil {
		return validationError("%s must be an unsigned integer string", name)
	}
	return nil
}

func validateTimeRange(start, end *time.Time) error {
	if start != nil && start.IsZero() || end != nil && end.IsZero() {
		return validationError("time range values cannot be zero")
	}
	if start != nil && end != nil && !start.Before(*end) {
		return validationError("start time must be before end time")
	}
	return nil
}

func validateRequiredText(name, value string) error {
	if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n\x00") {
		return validationError("%s is required and cannot contain control characters", name)
	}
	return nil
}

func pairListValues(pairs []string) url.Values {
	values := make(url.Values)
	if len(pairs) > 0 {
		values.Set("pair", strings.Join(pairs, ","))
	}
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

func setBool(values url.Values, key string, value bool) {
	if value {
		values.Set(key, "true")
	}
}

func setTime(values url.Values, key string, value *time.Time) {
	if value != nil {
		values.Set(key, strconv.FormatInt(value.Unix(), 10))
	}
}

func isPositiveDecimal(value string) bool {
	if !positiveDecimalPattern.MatchString(value) {
		return false
	}
	for _, character := range value {
		if character >= '1' && character <= '9' {
			return true
		}
	}
	return false
}

func validationError(format string, arguments ...any) error {
	return &trade.APIError{
		Category: trade.ErrorValidation, Exchange: model.ExchangeKraken,
		Cause: fmt.Errorf(format, arguments...),
	}
}
