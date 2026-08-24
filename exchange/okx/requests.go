package okx

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
	clientOrderIDPattern   = regexp.MustCompile(`^[0-9A-Za-z]{1,32}$`)
)

// CandleInterval은 OKX 캔들 구간 문자열이다.
type CandleInterval string

const (
	Candle1Minute   CandleInterval = "1m"
	Candle3Minutes  CandleInterval = "3m"
	Candle5Minutes  CandleInterval = "5m"
	Candle15Minutes CandleInterval = "15m"
	Candle30Minutes CandleInterval = "30m"
	Candle1Hour     CandleInterval = "1H"
	Candle2Hours    CandleInterval = "2H"
	Candle4Hours    CandleInterval = "4H"
	Candle6Hours    CandleInterval = "6H"
	Candle12Hours   CandleInterval = "12H"
	Candle1Day      CandleInterval = "1D"
	Candle2Days     CandleInterval = "2D"
	Candle3Days     CandleInterval = "3D"
	Candle1Week     CandleInterval = "1W"
	Candle1Month    CandleInterval = "1M"
	Candle3Months   CandleInterval = "3M"
)

// InstrumentsRequest는 상품 규칙 조회 조건이다.
type InstrumentsRequest struct {
	InstrumentType   InstrumentType
	Underlying       string
	InstrumentFamily string
	InstrumentID     string
}

// TickersRequest는 상품 유형별 ticker 조회 조건이다.
type TickersRequest struct {
	InstrumentType InstrumentType
}

// OrderBookRequest는 호가 스냅샷 조회 조건이다.
type OrderBookRequest struct {
	InstrumentID string
	Size         int
}

// RecentTradesRequest는 최근 공개 체결 조회 조건이다.
type RecentTradesRequest struct {
	InstrumentID string
	Limit        int
}

// CandlesRequest는 OHLCV 캔들 조회 조건이다.
type CandlesRequest struct {
	InstrumentID string
	Interval     CandleInterval
	After        *time.Time
	Before       *time.Time
	Limit        int
}

// BalanceRequest는 거래 계정 잔고의 통화 필터다.
type BalanceRequest struct {
	Currencies []string
}

// PositionsRequest는 SWAP 포지션 조회 조건이다.
type PositionsRequest struct {
	InstrumentType InstrumentType
	InstrumentID   string
	PositionID     string
}

// PlaceOrderRequest는 Spot 또는 SWAP 신규 주문이다.
type PlaceOrderRequest struct {
	InstrumentType InstrumentType `json:"-"`
	InstrumentID   string         `json:"instId"`
	TradeMode      TradeMode      `json:"tdMode"`
	ClientOrderID  string         `json:"clOrdId,omitempty"`
	Side           Side           `json:"side"`
	PositionSide   PositionSide   `json:"posSide,omitempty"`
	OrderType      OrderType      `json:"ordType"`
	Quantity       string         `json:"sz"`
	Price          string         `json:"px,omitempty"`
	ReduceOnly     bool           `json:"reduceOnly,omitempty"`
	TargetCurrency TargetCurrency `json:"tgtCcy,omitempty"`
}

// CancelOrderRequest는 주문 ID 또는 client order ID로 주문 취소를 요청한다.
type CancelOrderRequest struct {
	InstrumentID  string `json:"instId"`
	OrderID       string `json:"ordId,omitempty"`
	ClientOrderID string `json:"clOrdId,omitempty"`
}

// OrderInfoRequest는 주문 ID 또는 client order ID로 단건 주문을 조회한다.
type OrderInfoRequest struct {
	InstrumentID  string
	OrderID       string
	ClientOrderID string
}

// OpenOrdersRequest는 미체결 주문 목록 조회 조건이다.
type OpenOrdersRequest struct {
	InstrumentType InstrumentType
	InstrumentID   string
	OrderType      OrderType
	AfterOrderID   string
	BeforeOrderID  string
	Limit          int
}

// OrderHistoryRequest는 최근 7일 주문 이력 조회 조건이다.
type OrderHistoryRequest struct {
	InstrumentType InstrumentType
	InstrumentID   string
	OrderType      OrderType
	State          string
	AfterOrderID   string
	BeforeOrderID  string
	BeginTime      *time.Time
	EndTime        *time.Time
	Limit          int
}

func (request InstrumentsRequest) validate() error {
	if err := validateInstrumentType(request.InstrumentType); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"uly": request.Underlying, "instFamily": request.InstrumentFamily, "instId": request.InstrumentID,
	} {
		if err := validateOptionalText(name, value); err != nil {
			return err
		}
	}
	if request.InstrumentType == InstrumentTypeSpot &&
		(request.Underlying != "" || request.InstrumentFamily != "") {
		return validationError("Spot instruments do not accept uly or instFamily")
	}
	return nil
}

func (request InstrumentsRequest) values() url.Values {
	values := instrumentTypeValues(request.InstrumentType)
	setIfNotEmpty(values, "uly", request.Underlying)
	setIfNotEmpty(values, "instFamily", request.InstrumentFamily)
	setIfNotEmpty(values, "instId", request.InstrumentID)
	return values
}

func (request TickersRequest) validate() error {
	return validateInstrumentType(request.InstrumentType)
}

func (request TickersRequest) values() url.Values {
	return instrumentTypeValues(request.InstrumentType)
}

func (request OrderBookRequest) validate() error {
	if err := validateRequiredText("instId", request.InstrumentID); err != nil {
		return err
	}
	if request.Size < 0 || request.Size > 400 {
		return validationError("order book size must be between 1 and 400 or zero for default")
	}
	return nil
}

func (request OrderBookRequest) values() url.Values {
	values := make(url.Values)
	values.Set("instId", request.InstrumentID)
	setPositiveInt(values, "sz", request.Size)
	return values
}

func (request RecentTradesRequest) validate() error {
	if err := validateRequiredText("instId", request.InstrumentID); err != nil {
		return err
	}
	if request.Limit < 0 || request.Limit > 500 {
		return validationError("recent trade limit must be between 1 and 500 or zero for default")
	}
	return nil
}

func (request RecentTradesRequest) values() url.Values {
	values := make(url.Values)
	values.Set("instId", request.InstrumentID)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request CandlesRequest) validate() error {
	if err := validateRequiredText("instId", request.InstrumentID); err != nil {
		return err
	}
	if !request.Interval.valid() {
		return validationError("unsupported candle interval %q", request.Interval)
	}
	if request.Limit < 0 || request.Limit > 300 {
		return validationError("candle limit must be between 1 and 300 or zero for default")
	}
	return nil
}

func (request CandlesRequest) values() url.Values {
	values := make(url.Values)
	values.Set("instId", request.InstrumentID)
	values.Set("bar", string(request.Interval))
	setTime(values, "after", request.After)
	setTime(values, "before", request.Before)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request BalanceRequest) validate() error {
	seen := make(map[string]struct{}, len(request.Currencies))
	for _, currency := range request.Currencies {
		if err := validateRequiredText("ccy", currency); err != nil {
			return err
		}
		if strings.Contains(currency, ",") {
			return validationError("currency cannot contain a comma")
		}
		if _, exists := seen[currency]; exists {
			return validationError("duplicate currency %q", currency)
		}
		seen[currency] = struct{}{}
	}
	return nil
}

func (request BalanceRequest) values() url.Values {
	values := make(url.Values)
	if len(request.Currencies) > 0 {
		values.Set("ccy", strings.Join(request.Currencies, ","))
	}
	return values
}

func (request PositionsRequest) validate() error {
	if request.InstrumentType != InstrumentTypeSwap {
		return validationError("positions support only SWAP instrument type")
	}
	if err := validateOptionalText("instId", request.InstrumentID); err != nil {
		return err
	}
	return validateOptionalText("posId", request.PositionID)
}

func (request PositionsRequest) values() url.Values {
	values := instrumentTypeValues(request.InstrumentType)
	setIfNotEmpty(values, "instId", request.InstrumentID)
	setIfNotEmpty(values, "posId", request.PositionID)
	return values
}

func (request PlaceOrderRequest) validate() error {
	if err := validateInstrumentType(request.InstrumentType); err != nil {
		return err
	}
	if err := validateRequiredText("instId", request.InstrumentID); err != nil {
		return err
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("side must be buy or sell")
	}
	if !request.OrderType.valid() {
		return validationError("unsupported order type %q", request.OrderType)
	}
	if err := validatePositiveDecimal("sz", request.Quantity); err != nil {
		return err
	}
	if request.OrderType.requiresPrice() {
		if err := validatePositiveDecimal("px", request.Price); err != nil {
			return err
		}
	} else if request.Price != "" {
		return validationError("order type %q does not accept price", request.OrderType)
	}
	if request.ClientOrderID != "" && !clientOrderIDPattern.MatchString(request.ClientOrderID) {
		return validationError("clOrdId has an invalid format")
	}
	if request.InstrumentType == InstrumentTypeSpot {
		if request.TradeMode != TradeModeCash {
			return validationError("Spot order trade mode must be cash")
		}
		if request.PositionSide != "" || request.ReduceOnly {
			return validationError("Spot order does not accept posSide or reduceOnly")
		}
	} else {
		if request.TradeMode != TradeModeCross && request.TradeMode != TradeModeIsolated {
			return validationError("SWAP order trade mode must be cross or isolated")
		}
		if request.PositionSide != "" && !request.PositionSide.valid() {
			return validationError("unsupported position side %q", request.PositionSide)
		}
	}
	if request.TargetCurrency != "" && request.TargetCurrency != TargetCurrencyBase &&
		request.TargetCurrency != TargetCurrencyQuote {
		return validationError("target currency must be base_ccy or quote_ccy")
	}
	if request.TargetCurrency != "" &&
		(request.InstrumentType != InstrumentTypeSpot || request.OrderType != OrderTypeMarket) {
		return validationError("target currency is available only for Spot market orders")
	}
	return nil
}

func (request CancelOrderRequest) validate() error {
	if err := validateRequiredText("instId", request.InstrumentID); err != nil {
		return err
	}
	return validateOrderIdentity(request.OrderID, request.ClientOrderID)
}

func (request OrderInfoRequest) validate() error {
	if err := validateRequiredText("instId", request.InstrumentID); err != nil {
		return err
	}
	return validateOrderIdentity(request.OrderID, request.ClientOrderID)
}

func (request OrderInfoRequest) values() url.Values {
	values := make(url.Values)
	values.Set("instId", request.InstrumentID)
	setIfNotEmpty(values, "ordId", request.OrderID)
	setIfNotEmpty(values, "clOrdId", request.ClientOrderID)
	return values
}

func (request OpenOrdersRequest) validate() error {
	if err := validateInstrumentType(request.InstrumentType); err != nil {
		return err
	}
	if err := validateOptionalText("instId", request.InstrumentID); err != nil {
		return err
	}
	if request.OrderType != "" && !request.OrderType.valid() {
		return validationError("unsupported order type %q", request.OrderType)
	}
	return validateOrderPage(request.AfterOrderID, request.BeforeOrderID, request.Limit)
}

func (request OpenOrdersRequest) values() url.Values {
	values := instrumentTypeValues(request.InstrumentType)
	setOrderListValues(
		values, request.InstrumentID, request.OrderType, "", request.AfterOrderID,
		request.BeforeOrderID, nil, nil, request.Limit,
	)
	return values
}

func (request OrderHistoryRequest) validate() error {
	if err := validateInstrumentType(request.InstrumentType); err != nil {
		return err
	}
	if err := validateOptionalText("instId", request.InstrumentID); err != nil {
		return err
	}
	if request.OrderType != "" && !request.OrderType.valid() {
		return validationError("unsupported order type %q", request.OrderType)
	}
	if err := validateOptionalText("state", request.State); err != nil {
		return err
	}
	if request.BeginTime != nil && request.EndTime != nil {
		if request.BeginTime.After(*request.EndTime) {
			return validationError("order history begin time cannot be after end time")
		}
		if request.EndTime.Sub(*request.BeginTime) > 7*24*time.Hour {
			return validationError("order history time range cannot exceed 7 days")
		}
	}
	return validateOrderPage(request.AfterOrderID, request.BeforeOrderID, request.Limit)
}

func (request OrderHistoryRequest) values() url.Values {
	values := instrumentTypeValues(request.InstrumentType)
	setOrderListValues(
		values, request.InstrumentID, request.OrderType, request.State,
		request.AfterOrderID, request.BeforeOrderID, request.BeginTime, request.EndTime, request.Limit,
	)
	return values
}

func validateInstrumentType(instrumentType InstrumentType) error {
	if instrumentType != InstrumentTypeSpot && instrumentType != InstrumentTypeSwap {
		return validationError("instrument type must be SPOT or SWAP")
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

func validateOrderIdentity(orderID, clientOrderID string) error {
	if err := validateOptionalText("ordId", orderID); err != nil {
		return err
	}
	if err := validateOptionalText("clOrdId", clientOrderID); err != nil {
		return err
	}
	if orderID == "" && clientOrderID == "" {
		return validationError("ordId or clOrdId is required")
	}
	if clientOrderID != "" && !clientOrderIDPattern.MatchString(clientOrderID) {
		return validationError("clOrdId has an invalid format")
	}
	return nil
}

func validateOrderPage(after, before string, limit int) error {
	if err := validateOptionalText("after", after); err != nil {
		return err
	}
	if err := validateOptionalText("before", before); err != nil {
		return err
	}
	if limit < 0 || limit > 100 {
		return validationError("order page limit must be between 1 and 100 or zero for default")
	}
	return nil
}

func instrumentTypeValues(instrumentType InstrumentType) url.Values {
	values := make(url.Values)
	values.Set("instType", string(instrumentType))
	return values
}

func setOrderListValues(
	values url.Values,
	instrumentID string,
	orderType OrderType,
	state, after, before string,
	begin, end *time.Time,
	limit int,
) {
	setIfNotEmpty(values, "instId", instrumentID)
	setIfNotEmpty(values, "ordType", string(orderType))
	setIfNotEmpty(values, "state", state)
	setIfNotEmpty(values, "after", after)
	setIfNotEmpty(values, "before", before)
	setTime(values, "begin", begin)
	setTime(values, "end", end)
	setPositiveInt(values, "limit", limit)
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
		Candle1Day, Candle2Days, Candle3Days, Candle1Week, Candle1Month, Candle3Months:
		return true
	default:
		return false
	}
}

func (orderType OrderType) valid() bool {
	return orderType == OrderTypeMarket || orderType == OrderTypeLimit ||
		orderType == OrderTypePostOnly || orderType == OrderTypeFOK ||
		orderType == OrderTypeIOC || orderType == OrderTypeOptimalLimitIOC
}

func (orderType OrderType) requiresPrice() bool {
	return orderType == OrderTypeLimit || orderType == OrderTypePostOnly ||
		orderType == OrderTypeFOK || orderType == OrderTypeIOC
}

func (positionSide PositionSide) valid() bool {
	return positionSide == PositionSideNet || positionSide == PositionSideLong ||
		positionSide == PositionSideShort
}

func validationError(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", trade.ErrValidation, fmt.Sprintf(format, arguments...))
}
