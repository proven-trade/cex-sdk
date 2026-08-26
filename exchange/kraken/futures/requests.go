package futures

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
	pathSegmentPattern     = regexp.MustCompile(`^[0-9A-Za-z._:-]+$`)
	identifierPattern      = regexp.MustCompile(`^[0-9A-Za-z_-]+$`)
)

// InstrumentsRequest는 상품 규칙 조회 조건이다.
type InstrumentsRequest struct {
	ContractTypes []ContractType
	Expired       bool
}

// TickersRequest는 현재 시장 요약의 상품 종류와 심볼 필터다.
type TickersRequest struct {
	ContractTypes []ContractType
	Symbols       []string
}

// OrderBookRequest는 호가를 조회할 상품이다.
type OrderBookRequest struct {
	Symbol string
}

// PublicHistoryRequest는 공개 체결 이력 조회 조건이다.
type PublicHistoryRequest struct {
	Symbol   string
	LastTime string
}

// CandlesRequest는 차트 캔들 조회 조건이다.
type CandlesRequest struct {
	TickType   CandleTickType
	Symbol     string
	Resolution CandleResolution
	From       *time.Time
	To         *time.Time
	Count      int
}

// PlaceOrderRequest는 Futures 신규 주문이다.
type PlaceOrderRequest struct {
	OrderType     OrderType
	Symbol        string
	Side          Side
	Size          string
	LimitPrice    string
	ClientOrderID string
	ReduceOnly    bool
	ProcessBefore *time.Time
}

// CancelOrderRequest는 거래소 주문 ID 또는 client order ID로 주문을 취소한다.
type CancelOrderRequest struct {
	OrderID       string
	ClientOrderID string
	ProcessBefore *time.Time
}

// OrderStatusRequest는 최대 100개 주문의 최근 상태를 조회한다.
type OrderStatusRequest struct {
	OrderIDs       []string
	ClientOrderIDs []string
}

// FillsRequest는 지정 시각 이후 계정 체결을 조회한다.
type FillsRequest struct {
	LastFillTime string
}

func (request InstrumentsRequest) validate() error {
	seen := make(map[ContractType]struct{}, len(request.ContractTypes))
	for _, contractType := range request.ContractTypes {
		if !contractType.valid() {
			return validationError("unsupported contract type %q", contractType)
		}
		if _, exists := seen[contractType]; exists {
			return validationError("duplicate contract type %q", contractType)
		}
		seen[contractType] = struct{}{}
	}
	return nil
}

func (request InstrumentsRequest) values() url.Values {
	values := make(url.Values)
	for _, contractType := range request.ContractTypes {
		values.Add("contractType", string(contractType))
	}
	if request.Expired {
		values.Set("expired", "true")
	}
	return values
}

func (request TickersRequest) validate() error {
	seenTypes := make(map[ContractType]struct{}, len(request.ContractTypes))
	for _, contractType := range request.ContractTypes {
		if !contractType.valid() {
			return validationError("unsupported contract type %q", contractType)
		}
		if _, exists := seenTypes[contractType]; exists {
			return validationError("duplicate contract type %q", contractType)
		}
		seenTypes[contractType] = struct{}{}
	}
	seenSymbols := make(map[string]struct{}, len(request.Symbols))
	for _, symbol := range request.Symbols {
		if err := validateSymbol(symbol); err != nil {
			return err
		}
		if _, exists := seenSymbols[symbol]; exists {
			return validationError("duplicate ticker symbol %q", symbol)
		}
		seenSymbols[symbol] = struct{}{}
	}
	return nil
}

func (request TickersRequest) values() url.Values {
	values := make(url.Values)
	for _, contractType := range request.ContractTypes {
		values.Add("contractType", string(contractType))
	}
	for _, symbol := range request.Symbols {
		values.Add("symbol", symbol)
	}
	return values
}

func (request OrderBookRequest) validate() error {
	return validateSymbol(request.Symbol)
}

func (request OrderBookRequest) values() url.Values {
	return url.Values{"symbol": []string{request.Symbol}}
}

func (request PublicHistoryRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	return validateOptionalText("lastTime", request.LastTime)
}

func (request PublicHistoryRequest) values() url.Values {
	values := url.Values{"symbol": []string{request.Symbol}}
	setIfNotEmpty(values, "lastTime", request.LastTime)
	return values
}

func (request CandlesRequest) validate() error {
	if !request.TickType.valid() {
		return validationError("unsupported candle tick type %q", request.TickType)
	}
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if !request.Resolution.valid() {
		return validationError("unsupported candle resolution %q", request.Resolution)
	}
	if request.Count < 0 || request.Count > 10000 {
		return validationError("candle count must be between 1 and 10000 or zero for default")
	}
	if request.From != nil && request.To != nil && request.From.After(*request.To) {
		return validationError("candle start time cannot be after end time")
	}
	return nil
}

func (request CandlesRequest) values() url.Values {
	values := make(url.Values)
	setTimeSeconds(values, "from", request.From)
	setTimeSeconds(values, "to", request.To)
	if request.Count > 0 {
		values.Set("count", strconv.Itoa(request.Count))
	}
	return values
}

func (request PlaceOrderRequest) validate() error {
	if !request.OrderType.valid() {
		return validationError("unsupported order type %q", request.OrderType)
	}
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("side must be buy or sell")
	}
	if err := validatePositiveDecimal("size", request.Size); err != nil {
		return err
	}
	if request.OrderType == OrderTypeMarket {
		if request.LimitPrice != "" {
			return validationError("market order does not accept limitPrice")
		}
	} else if err := validatePositiveDecimal("limitPrice", request.LimitPrice); err != nil {
		return err
	}
	if request.ClientOrderID != "" {
		if len(request.ClientOrderID) > 100 || !identifierPattern.MatchString(request.ClientOrderID) {
			return validationError("cliOrdId must contain 1-100 letters, numbers, underscores, or hyphens")
		}
	}
	return validateProcessBefore(request.ProcessBefore)
}

func (request PlaceOrderRequest) values() url.Values {
	values := make(url.Values)
	values.Set("orderType", string(request.OrderType))
	values.Set("symbol", request.Symbol)
	values.Set("side", string(request.Side))
	values.Set("size", request.Size)
	setIfNotEmpty(values, "limitPrice", request.LimitPrice)
	setIfNotEmpty(values, "cliOrdId", request.ClientOrderID)
	if request.ReduceOnly {
		values.Set("reduceOnly", "true")
	}
	setProcessBefore(values, request.ProcessBefore)
	return values
}

func (request CancelOrderRequest) validate() error {
	if err := validateOrderIdentity(request.OrderID, request.ClientOrderID); err != nil {
		return err
	}
	return validateProcessBefore(request.ProcessBefore)
}

func (request CancelOrderRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "order_id", request.OrderID)
	setIfNotEmpty(values, "cliOrdId", request.ClientOrderID)
	setProcessBefore(values, request.ProcessBefore)
	return values
}

func (request OrderStatusRequest) validate() error {
	if len(request.OrderIDs)+len(request.ClientOrderIDs) == 0 {
		return validationError("at least one order ID or client order ID is required")
	}
	if len(request.OrderIDs)+len(request.ClientOrderIDs) > 100 {
		return validationError("order status accepts at most 100 identities")
	}
	seen := make(map[string]struct{}, len(request.OrderIDs)+len(request.ClientOrderIDs))
	for _, orderID := range request.OrderIDs {
		if err := validateIdentifier("order ID", orderID); err != nil {
			return err
		}
		if _, exists := seen["order:"+orderID]; exists {
			return validationError("duplicate order ID %q", orderID)
		}
		seen["order:"+orderID] = struct{}{}
	}
	for _, clientOrderID := range request.ClientOrderIDs {
		if err := validateIdentifier("client order ID", clientOrderID); err != nil {
			return err
		}
		if _, exists := seen["client:"+clientOrderID]; exists {
			return validationError("duplicate client order ID %q", clientOrderID)
		}
		seen["client:"+clientOrderID] = struct{}{}
	}
	return nil
}

func (request OrderStatusRequest) values() url.Values {
	values := make(url.Values)
	for _, orderID := range request.OrderIDs {
		values.Add("orderIds", orderID)
	}
	for _, clientOrderID := range request.ClientOrderIDs {
		values.Add("cliOrdIds", clientOrderID)
	}
	return values
}

func (request FillsRequest) validate() error {
	return validateOptionalText("lastFillTime", request.LastFillTime)
}

func (request FillsRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "lastFillTime", request.LastFillTime)
	return values
}

func (value ContractType) valid() bool {
	return value == ContractTypeInverse || value == ContractTypeVanilla || value == ContractTypeFlexible
}

func (value CandleTickType) valid() bool {
	return value == CandleTickSpot || value == CandleTickMark || value == CandleTickTrade
}

func (value CandleResolution) valid() bool {
	switch value {
	case Candle1Minute, Candle5Minutes, Candle15Minutes, Candle30Minutes,
		Candle1Hour, Candle4Hours, Candle12Hours, Candle1Day, Candle1Week:
		return true
	default:
		return false
	}
}

func (value OrderType) valid() bool {
	return value == OrderTypeLimit || value == OrderTypePostOnly || value == OrderTypeMarket ||
		value == OrderTypeIOC || value == OrderTypeFOK
}

func validateSymbol(symbol string) error {
	if !pathSegmentPattern.MatchString(symbol) {
		return validationError("symbol has an invalid format")
	}
	return nil
}

func validatePositiveDecimal(name, value string) error {
	if !positiveDecimalPattern.MatchString(value) || strings.Trim(value, "0.") == "" {
		return validationError("%s must be a positive decimal string", name)
	}
	return nil
}

func validateOrderIdentity(orderID, clientOrderID string) error {
	if orderID == "" && clientOrderID == "" {
		return validationError("order_id or cliOrdId is required")
	}
	if orderID != "" && clientOrderID != "" {
		return validationError("order_id and cliOrdId cannot be used together")
	}
	if orderID != "" {
		return validateIdentifier("order ID", orderID)
	}
	return validateIdentifier("client order ID", clientOrderID)
}

func validateIdentifier(name, value string) error {
	if len(value) > 100 || !identifierPattern.MatchString(value) {
		return validationError("%s has an invalid format", name)
	}
	return nil
}

func validateOptionalText(name, value string) error {
	if value == "" {
		return nil
	}
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n") {
		return validationError("%s has an invalid format", name)
	}
	return nil
}

func validateProcessBefore(value *time.Time) error {
	if value != nil && value.IsZero() {
		return validationError("processBefore cannot be zero")
	}
	return nil
}

func setIfNotEmpty(values url.Values, key, value string) {
	if value != "" {
		values.Set(key, value)
	}
}

func setTimeSeconds(values url.Values, key string, value *time.Time) {
	if value != nil {
		values.Set(key, strconv.FormatInt(value.Unix(), 10))
	}
}

func setProcessBefore(values url.Values, value *time.Time) {
	if value != nil {
		values.Set("processBefore", value.UTC().Format(time.RFC3339Nano))
	}
}

func validationError(format string, arguments ...any) error {
	return &trade.APIError{
		Category: trade.ErrorValidation,
		Exchange: model.ExchangeKraken,
		Cause:    fmt.Errorf(format, arguments...),
	}
}
