package htx

import (
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/cex-sdk"
	"github.com/proven-trade/cex-sdk/model"
)

var symbolPattern = regexp.MustCompile(`^[a-z0-9]{2,40}$`)

var (
	accountIDPattern       = regexp.MustCompile(`^[0-9]{1,64}$`)
	orderIDPattern         = regexp.MustCompile(`^[0-9]{1,64}$`)
	clientOrderIDPattern   = regexp.MustCompile(`^[0-9A-Za-z_-]{1,64}$`)
	positiveDecimalPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
)

// MarketSymbolsRequest는 전체·복수 Spot 거래쌍 규칙과 선택적 증분 기준이다.
type MarketSymbolsRequest struct {
	Symbols      []string
	UpdatedSince *time.Time
}

// DepthType은 HTX REST 호가의 가격 집계 단계다.
type DepthType string

const (
	DepthStep0 DepthType = "step0"
	DepthStep1 DepthType = "step1"
	DepthStep2 DepthType = "step2"
	DepthStep3 DepthType = "step3"
	DepthStep4 DepthType = "step4"
	DepthStep5 DepthType = "step5"
)

// OrderBookRequest는 Spot 호가 snapshot 조회 조건이다.
type OrderBookRequest struct {
	Symbol string
	Depth  int
	Type   DepthType
}

// TradesRequest는 최근 공개 Spot 체결 묶음 조회 조건이다.
type TradesRequest struct {
	Symbol string
	Size   int
}

// CandleInterval은 HTX Spot 캔들 구간이다.
type CandleInterval string

const (
	Candle1Minute   CandleInterval = "1min"
	Candle5Minutes  CandleInterval = "5min"
	Candle15Minutes CandleInterval = "15min"
	Candle30Minutes CandleInterval = "30min"
	Candle1Hour     CandleInterval = "60min"
	Candle4Hours    CandleInterval = "4hour"
	Candle1Day      CandleInterval = "1day"
	Candle1Week     CandleInterval = "1week"
	Candle1Month    CandleInterval = "1mon"
	Candle1Year     CandleInterval = "1year"
)

// CandlesRequest는 개수 기반 Spot OHLCV 조회 조건이다.
type CandlesRequest struct {
	Symbol   string
	Interval CandleInterval
	Size     int
}

// Side는 HTX Spot 주문과 조회 필터의 매수·매도 방향이다.
type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// OrderKind는 주문 방향과 분리해 입력하는 가격·체결 정책이다.
type OrderKind string

const (
	OrderKindMarket     OrderKind = "market"
	OrderKindLimit      OrderKind = "limit"
	OrderKindIOC        OrderKind = "ioc"
	OrderKindLimitMaker OrderKind = "limit-maker"
	OrderKindLimitFOK   OrderKind = "limit-fok"
)

// OrderType은 HTX API가 방향과 정책을 결합해 표현하는 주문 타입이다.
type OrderType string

const (
	OrderTypeBuyMarket        OrderType = "buy-market"
	OrderTypeSellMarket       OrderType = "sell-market"
	OrderTypeBuyLimit         OrderType = "buy-limit"
	OrderTypeSellLimit        OrderType = "sell-limit"
	OrderTypeBuyIOC           OrderType = "buy-ioc"
	OrderTypeSellIOC          OrderType = "sell-ioc"
	OrderTypeBuyLimitMaker    OrderType = "buy-limit-maker"
	OrderTypeSellLimitMaker   OrderType = "sell-limit-maker"
	OrderTypeBuyLimitFOK      OrderType = "buy-limit-fok"
	OrderTypeSellLimitFOK     OrderType = "sell-limit-fok"
	OrderTypeBuyStopLimit     OrderType = "buy-stop-limit"
	OrderTypeSellStopLimit    OrderType = "sell-stop-limit"
	OrderTypeBuyStopLimitFOK  OrderType = "buy-stop-limit-fok"
	OrderTypeSellStopLimitFOK OrderType = "sell-stop-limit-fok"
)

// OrderState는 HTX Spot 주문의 수명주기 상태다.
type OrderState string

const (
	OrderStateCreated         OrderState = "created"
	OrderStateSubmitted       OrderState = "submitted"
	OrderStatePartialFilled   OrderState = "partial-filled"
	OrderStateFilled          OrderState = "filled"
	OrderStatePartialCanceled OrderState = "partial-canceled"
	OrderStateCanceling       OrderState = "canceling"
	OrderStateCanceled        OrderState = "canceled"
)

// QueryDirection은 커서 기준 조회 방향이다.
type QueryDirection string

const (
	QueryDirectionNext QueryDirection = "next"
	QueryDirectionPrev QueryDirection = "prev"
)

// PlaceOrderRequest는 사용자 주문 ID를 강제하는 HTX Spot 주문 조건이다.
type PlaceOrderRequest struct {
	AccountID        string
	ClientOrderID    string
	Symbol           string
	Side             Side
	Kind             OrderKind
	Amount           string
	Price            string
	SelfMatchPrevent bool
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
	Symbol        string
}

// OpenOrdersRequest는 현재 미체결 주문의 필터와 커서를 지정한다.
type OpenOrdersRequest struct {
	AccountID string
	Symbol    string
	Side      Side
	From      string
	Direction QueryDirection
	Size      int
}

// OrderHistoryRequest는 종료 주문 이력의 필터·시간 범위·커서를 지정한다.
type OrderHistoryRequest struct {
	Symbol    string
	Types     []OrderType
	States    []OrderState
	Start     *time.Time
	End       *time.Time
	From      string
	Direction QueryDirection
	Size      int
}

// MatchResultsRequest는 계정 체결 이력의 필터·시간 범위·커서를 지정한다.
type MatchResultsRequest struct {
	Symbol    string
	Types     []OrderType
	Start     *time.Time
	End       *time.Time
	From      string
	Direction QueryDirection
	Size      int
}

func (request MarketSymbolsRequest) validate() error {
	seen := make(map[string]struct{}, len(request.Symbols))
	for _, symbol := range request.Symbols {
		if err := validateSymbol(symbol); err != nil {
			return err
		}
		if _, exists := seen[symbol]; exists {
			return validationError("market symbols contain a duplicate")
		}
		seen[symbol] = struct{}{}
	}
	if request.UpdatedSince != nil && request.UpdatedSince.UnixMilli() <= 0 {
		return validationError("market symbol update time must be after the Unix epoch")
	}
	return nil
}

func (request MarketSymbolsRequest) values() url.Values {
	values := make(url.Values)
	if len(request.Symbols) > 0 {
		values.Set("symbols", strings.Join(request.Symbols, ","))
	}
	if request.UpdatedSince != nil {
		values.Set("ts", strconv.FormatInt(request.UpdatedSince.UnixMilli(), 10))
	}
	return values
}

func (request OrderBookRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	switch request.Depth {
	case 0, 5, 10, 20:
	default:
		return validationError("order book depth must be 5, 10, 20, or zero")
	}
	if request.Type != "" && !request.Type.valid() {
		return validationError("unsupported order book depth type %q", request.Type)
	}
	return nil
}

func (request OrderBookRequest) values() url.Values {
	depthType := request.Type
	if depthType == "" {
		depthType = DepthStep0
	}
	values := url.Values{"symbol": {request.Symbol}, "type": {string(depthType)}}
	if request.Depth > 0 {
		values.Set("depth", strconv.Itoa(request.Depth))
	}
	return values
}

func (request TradesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	return validateSize(request.Size)
}

func (request TradesRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	if request.Size > 0 {
		values.Set("size", strconv.Itoa(request.Size))
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
	return validateSize(request.Size)
}

func (request CandlesRequest) values() url.Values {
	values := url.Values{
		"symbol": {request.Symbol}, "period": {string(request.Interval)},
	}
	if request.Size > 0 {
		values.Set("size", strconv.Itoa(request.Size))
	}
	return values
}

func (request PlaceOrderRequest) validate() error {
	if !accountIDPattern.MatchString(request.AccountID) {
		return validationError("invalid HTX account ID %q", request.AccountID)
	}
	if !clientOrderIDPattern.MatchString(request.ClientOrderID) {
		return validationError("client order ID is required and must match [0-9A-Za-z_-]{1,64}")
	}
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("order side must be buy or sell")
	}
	if !request.Kind.valid() {
		return validationError("unsupported order kind %q", request.Kind)
	}
	if err := validatePositiveDecimal("amount", request.Amount); err != nil {
		return err
	}
	if request.Kind == OrderKindMarket {
		if request.Price != "" {
			return validationError("market order does not accept price")
		}
		return nil
	}
	return validatePositiveDecimal("price", request.Price)
}

func (request OrderInfoRequest) validate() error {
	return validateOrderIdentity(request.OrderID, request.ClientOrderID)
}

func (request CancelOrderRequest) validate() error {
	if err := validateOrderIdentity(request.OrderID, request.ClientOrderID); err != nil {
		return err
	}
	if request.Symbol != "" {
		return validateSymbol(request.Symbol)
	}
	return nil
}

func (request OpenOrdersRequest) validate() error {
	if request.AccountID != "" && !accountIDPattern.MatchString(request.AccountID) {
		return validationError("invalid HTX account ID %q", request.AccountID)
	}
	if request.Symbol != "" {
		if err := validateSymbol(request.Symbol); err != nil {
			return err
		}
	}
	if request.Side != "" && request.Side != SideBuy && request.Side != SideSell {
		return validationError("open order side must be buy or sell")
	}
	if err := validateCursor(request.From, request.Direction); err != nil {
		return err
	}
	if request.Size < 0 || request.Size > 500 {
		return validationError("open order size must be between 1 and 500 or zero")
	}
	return nil
}

func (request OpenOrdersRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "account-id", request.AccountID)
	setIfNotEmpty(values, "symbol", request.Symbol)
	setIfNotEmpty(values, "side", string(request.Side))
	setIfNotEmpty(values, "from", request.From)
	setIfNotEmpty(values, "direct", string(request.Direction))
	setPositiveInt(values, "size", request.Size)
	return values
}

func (request OrderHistoryRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if len(request.States) == 0 {
		return validationError("order history states are required")
	}
	if err := validateOrderTypes(request.Types); err != nil {
		return err
	}
	seen := make(map[OrderState]struct{}, len(request.States))
	for _, state := range request.States {
		if state != OrderStateFilled && state != OrderStatePartialCanceled &&
			state != OrderStateCanceled {
			return validationError("unsupported order history state %q", state)
		}
		if _, exists := seen[state]; exists {
			return validationError("order history states contain a duplicate")
		}
		seen[state] = struct{}{}
	}
	if err := validateTimeRange(request.Start, request.End); err != nil {
		return err
	}
	if err := validateCursor(request.From, request.Direction); err != nil {
		return err
	}
	if request.Size < 0 || request.Size > 100 {
		return validationError("order history size must be between 1 and 100 or zero")
	}
	return nil
}

func (request OrderHistoryRequest) values() url.Values {
	values := url.Values{
		"symbol": {request.Symbol}, "states": {joinOrderStates(request.States)},
	}
	setOrderTypes(values, request.Types)
	setTimeRange(values, request.Start, request.End)
	setIfNotEmpty(values, "from", request.From)
	setIfNotEmpty(values, "direct", string(request.Direction))
	setPositiveInt(values, "size", request.Size)
	return values
}

func (request MatchResultsRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if err := validateOrderTypes(request.Types); err != nil {
		return err
	}
	if err := validateTimeRange(request.Start, request.End); err != nil {
		return err
	}
	if err := validateCursor(request.From, request.Direction); err != nil {
		return err
	}
	if request.Size < 0 || request.Size > 500 {
		return validationError("match result size must be between 1 and 500 or zero")
	}
	return nil
}

func (request MatchResultsRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	setOrderTypes(values, request.Types)
	setTimeRange(values, request.Start, request.End)
	setIfNotEmpty(values, "from", request.From)
	setIfNotEmpty(values, "direct", string(request.Direction))
	setPositiveInt(values, "size", request.Size)
	return values
}

func (value DepthType) valid() bool {
	switch value {
	case DepthStep0, DepthStep1, DepthStep2, DepthStep3, DepthStep4, DepthStep5:
		return true
	default:
		return false
	}
}

func (interval CandleInterval) valid() bool {
	switch interval {
	case Candle1Minute, Candle5Minutes, Candle15Minutes, Candle30Minutes,
		Candle1Hour, Candle4Hours, Candle1Day, Candle1Week, Candle1Month, Candle1Year:
		return true
	default:
		return false
	}
}

func (kind OrderKind) valid() bool {
	switch kind {
	case OrderKindMarket, OrderKindLimit, OrderKindIOC, OrderKindLimitMaker, OrderKindLimitFOK:
		return true
	default:
		return false
	}
}

func (request PlaceOrderRequest) orderType() OrderType {
	return OrderType(string(request.Side) + "-" + string(request.Kind))
}

func (value OrderType) valid() bool {
	switch value {
	case OrderTypeBuyMarket, OrderTypeSellMarket, OrderTypeBuyLimit, OrderTypeSellLimit,
		OrderTypeBuyIOC, OrderTypeSellIOC, OrderTypeBuyLimitMaker, OrderTypeSellLimitMaker,
		OrderTypeBuyLimitFOK, OrderTypeSellLimitFOK, OrderTypeBuyStopLimit,
		OrderTypeSellStopLimit, OrderTypeBuyStopLimitFOK, OrderTypeSellStopLimitFOK:
		return true
	default:
		return false
	}
}

func validateSymbol(symbol string) error {
	if !symbolPattern.MatchString(symbol) {
		return validationError("invalid HTX symbol %q", symbol)
	}
	return nil
}

func validateSize(size int) error {
	if size < 0 || size > 2000 {
		return validationError("result size must be between 1 and 2000 or zero")
	}
	return nil
}

func validateOrderIdentity(orderID, clientOrderID string) error {
	if (orderID == "") == (clientOrderID == "") {
		return validationError("exactly one order ID or client order ID is required")
	}
	if orderID != "" && !orderIDPattern.MatchString(orderID) {
		return validationError("invalid HTX order ID %q", orderID)
	}
	if clientOrderID != "" && !clientOrderIDPattern.MatchString(clientOrderID) {
		return validationError("invalid HTX client order ID %q", clientOrderID)
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

func validateCursor(from string, direction QueryDirection) error {
	if from == "" && direction != "" {
		return validationError("query direction requires a cursor")
	}
	if from != "" {
		if !orderIDPattern.MatchString(from) {
			return validationError("invalid HTX cursor %q", from)
		}
		if direction != QueryDirectionNext && direction != QueryDirectionPrev {
			return validationError("cursor query direction must be next or prev")
		}
	}
	return nil
}

func validateOrderTypes(values []OrderType) error {
	seen := make(map[OrderType]struct{}, len(values))
	for _, value := range values {
		if !value.valid() {
			return validationError("unsupported HTX order type %q", value)
		}
		if _, exists := seen[value]; exists {
			return validationError("HTX order types contain a duplicate")
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateTimeRange(start, end *time.Time) error {
	if start != nil && start.UnixMilli() <= 0 || end != nil && end.UnixMilli() <= 0 {
		return validationError("query timestamps must be after the Unix epoch")
	}
	if start != nil && end != nil {
		if !start.Before(*end) {
			return validationError("query start must be before end")
		}
		if end.Sub(*start) > 48*time.Hour {
			return validationError("query time range cannot exceed 48 hours")
		}
	}
	return nil
}

func setOrderTypes(values url.Values, types []OrderType) {
	if len(types) == 0 {
		return
	}
	items := make([]string, len(types))
	for index, value := range types {
		items[index] = string(value)
	}
	values.Set("types", strings.Join(items, ","))
}

func joinOrderStates(states []OrderState) string {
	items := make([]string, len(states))
	for index, value := range states {
		items[index] = string(value)
	}
	return strings.Join(items, ",")
}

func setTimeRange(values url.Values, start, end *time.Time) {
	if start != nil {
		values.Set("start-time", strconv.FormatInt(start.UnixMilli(), 10))
	}
	if end != nil {
		values.Set("end-time", strconv.FormatInt(end.UnixMilli(), 10))
	}
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

func validationError(format string, arguments ...any) error {
	return &trade.APIError{
		Category: trade.ErrorValidation, Exchange: model.ExchangeHTX,
		Cause: fmt.Errorf(format, arguments...),
	}
}
