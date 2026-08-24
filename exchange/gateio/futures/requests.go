package futures

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
	contractPattern      = regexp.MustCompile(`^[A-Z0-9]{2,20}_[A-Z0-9]{2,20}$`)
	orderIdentityPattern = regexp.MustCompile(`^[0-9A-Za-z._-]{1,64}$`)
	clientOrderIDPattern = regexp.MustCompile(`^t-[0-9A-Za-z._-]{1,28}$`)
	positiveDecimal      = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
	signedDecimalPattern = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?$`)
)

// ContractsRequest는 결제 통화별 무기한 Futures 계약 페이지 조회 조건이다.
type ContractsRequest struct {
	Settlement Settlement
	Limit      int
	Offset     int
}

// TickersRequest는 전체 또는 단일 무기한 Futures 계약 통계 조회 조건이다.
type TickersRequest struct {
	Settlement Settlement
	Contract   string
}

// OrderBookRequest는 무기한 Futures 호가 조회 조건이다.
type OrderBookRequest struct {
	Settlement    Settlement
	Contract      string
	MergeInterval string
	Limit         int
}

// TradesRequest는 무기한 Futures 공개 체결 조회 조건이다.
type TradesRequest struct {
	Settlement Settlement
	Contract   string
	Limit      int
	Offset     int
	From       *time.Time
	To         *time.Time
}

// CandlesRequest는 최근 개수 또는 시간 범위 기반 무기한 Futures 캔들 조회 조건이다.
type CandlesRequest struct {
	Settlement Settlement
	Contract   string
	Interval   CandleInterval
	Limit      int
	From       *time.Time
	To         *time.Time
}

// AccountRequest는 조회할 무기한 Futures 결제 통화다.
type AccountRequest struct {
	Settlement Settlement
}

// PositionsRequest는 무기한 Futures 포지션 페이지 조회 조건이다.
type PositionsRequest struct {
	Settlement  Settlement
	HoldingOnly bool
	Limit       int
	Offset      int
}

// PlaceOrderRequest는 Gate.io 무기한 Futures 지정가 또는 시장가 주문이다.
type PlaceOrderRequest struct {
	Settlement          Settlement          `json:"-"`
	Type                OrderType           `json:"-"`
	Contract            string              `json:"contract"`
	Size                string              `json:"size"`
	Iceberg             string              `json:"iceberg,omitempty"`
	Price               string              `json:"price"`
	Close               bool                `json:"close,omitempty"`
	ReduceOnly          bool                `json:"reduce_only,omitempty"`
	TimeInForce         TimeInForce         `json:"tif"`
	ClientOrderID       string              `json:"text"`
	AutoSize            AutoSize            `json:"auto_size,omitempty"`
	SelfTradePrevention SelfTradePrevention `json:"stp_act,omitempty"`
	PositionMarginMode  PositionMarginMode  `json:"pos_margin_mode,omitempty"`
}

// OrderInfoRequest는 결제 통화와 주문 식별자로 주문 한 건을 조회한다.
type OrderInfoRequest struct {
	Settlement Settlement
	OrderID    string
}

// CancelOrderRequest는 결제 통화와 주문 식별자로 취소 대상을 지정한다.
type CancelOrderRequest struct {
	Settlement Settlement
	OrderID    string
}

// OrderStatus는 주문 목록의 미체결 또는 종료 상태 필터다.
type OrderStatus string

const (
	OrderStatusOpen     OrderStatus = "open"
	OrderStatusFinished OrderStatus = "finished"
)

// OrdersRequest는 계약과 상태로 필터링한 주문 페이지 조회 조건이다.
type OrdersRequest struct {
	Settlement Settlement
	Contract   string
	Status     OrderStatus
	Limit      int
	Offset     int
}

// MyTradesRequest는 계약 또는 주문으로 필터링한 계정 체결 페이지 조회 조건이다.
type MyTradesRequest struct {
	Settlement Settlement
	Contract   string
	OrderID    string
	Limit      int
	Offset     int
}

func (request ContractsRequest) validate() error {
	if err := request.Settlement.validate(); err != nil {
		return err
	}
	return validatePage(request.Limit, request.Offset, 1000)
}

func (request ContractsRequest) values() url.Values {
	values := make(url.Values)
	setPositiveInt(values, "limit", request.Limit)
	setPositiveInt(values, "offset", request.Offset)
	return values
}

func (request TickersRequest) validate() error {
	if err := request.Settlement.validate(); err != nil {
		return err
	}
	if request.Contract != "" {
		return validateContract(request.Contract)
	}
	return nil
}

func (request TickersRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "contract", request.Contract)
	return values
}

func (request OrderBookRequest) validate() error {
	if err := validateSettlementContract(request.Settlement, request.Contract); err != nil {
		return err
	}
	if request.MergeInterval != "" && !positiveDecimal.MatchString(request.MergeInterval) {
		return validationError("invalid Futures order book merge interval %q", request.MergeInterval)
	}
	if request.Limit < 0 || request.Limit > 100 {
		return validationError("Futures order book limit must be between 1 and 100 or zero")
	}
	return nil
}

func (request OrderBookRequest) values() url.Values {
	values := url.Values{"contract": {request.Contract}, "with_id": {"true"}}
	setIfNotEmpty(values, "interval", request.MergeInterval)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request TradesRequest) validate() error {
	if err := validateSettlementContract(request.Settlement, request.Contract); err != nil {
		return err
	}
	if err := validatePage(request.Limit, request.Offset, 1000); err != nil {
		return err
	}
	return validateTimeRange(request.From, request.To, false)
}

func (request TradesRequest) values() url.Values {
	values := url.Values{"contract": {request.Contract}}
	setPositiveInt(values, "limit", request.Limit)
	setPositiveInt(values, "offset", request.Offset)
	setTimes(values, request.From, request.To)
	return values
}

func (request CandlesRequest) validate() error {
	if err := validateSettlementContract(request.Settlement, request.Contract); err != nil {
		return err
	}
	if !request.Interval.valid() {
		return validationError("unsupported Futures candle interval %q", request.Interval)
	}
	if request.Limit < 0 || request.Limit > 2000 {
		return validationError("Futures candle limit must be between 1 and 2000 or zero")
	}
	return validateTimeRange(request.From, request.To, request.Limit > 0)
}

func (request CandlesRequest) values() url.Values {
	values := url.Values{
		"contract": {request.Contract}, "interval": {string(request.Interval)}, "timezone": {"utc0"},
	}
	setPositiveInt(values, "limit", request.Limit)
	setTimes(values, request.From, request.To)
	return values
}

func (request AccountRequest) validate() error {
	return request.Settlement.validate()
}

func (request PositionsRequest) validate() error {
	if err := request.Settlement.validate(); err != nil {
		return err
	}
	return validatePage(request.Limit, request.Offset, 100)
}

func (request PositionsRequest) values() url.Values {
	values := make(url.Values)
	if request.HoldingOnly {
		values.Set("holding", "true")
	}
	setPositiveInt(values, "limit", request.Limit)
	setPositiveInt(values, "offset", request.Offset)
	return values
}

func (request PlaceOrderRequest) validate() error {
	if err := validateSettlementContract(request.Settlement, request.Contract); err != nil {
		return err
	}
	if !clientOrderIDPattern.MatchString(request.ClientOrderID) {
		return validationError("client order ID must start with t- and contain 1 to 28 supported characters")
	}
	if !signedDecimalPattern.MatchString(request.Size) {
		return validationError("Futures order size must be a signed decimal string")
	}
	unsignedSize := strings.TrimPrefix(request.Size, "-")
	zeroSize := strings.Trim(unsignedSize, "0.") == ""
	if zeroSize {
		if request.Close == (request.AutoSize != "") {
			return validationError("zero-size order requires exactly one of close or auto size")
		}
		if request.AutoSize != "" && !request.ReduceOnly {
			return validationError("auto-size close order must be reduce-only")
		}
	} else if request.Close || request.AutoSize != "" {
		return validationError("close and auto size require zero order size")
	}
	if request.AutoSize != "" && request.AutoSize != AutoSizeCloseLong && request.AutoSize != AutoSizeCloseShort {
		return validationError("auto size must be close_long or close_short")
	}
	if request.Iceberg != "" && !positiveDecimal.MatchString(request.Iceberg) {
		return validationError("Futures iceberg size must be a non-negative decimal")
	}
	if request.PositionMarginMode != "" &&
		request.PositionMarginMode != PositionMarginModeIsolated &&
		request.PositionMarginMode != PositionMarginModeCross {
		return validationError("position margin mode must be isolated, cross, or empty")
	}
	if request.SelfTradePrevention != "" &&
		request.SelfTradePrevention != SelfTradePreventionNone &&
		request.SelfTradePrevention != SelfTradePreventionCancelOld &&
		request.SelfTradePrevention != SelfTradePreventionCancelNew &&
		request.SelfTradePrevention != SelfTradePreventionCancelBoth {
		return validationError("unsupported self-trade prevention action %q", request.SelfTradePrevention)
	}
	switch request.Type {
	case OrderTypeLimit:
		if err := validatePositiveDecimal("Futures order price", request.Price); err != nil {
			return err
		}
		if request.TimeInForce != "" && request.TimeInForce != TimeInForceGTC &&
			request.TimeInForce != TimeInForceIOC && request.TimeInForce != TimeInForcePOC &&
			request.TimeInForce != TimeInForceFOK {
			return validationError("unsupported Futures time in force %q", request.TimeInForce)
		}
	case OrderTypeMarket:
		if request.Price != "" && request.Price != "0" {
			return validationError("Futures market order price must be zero or empty")
		}
		if request.TimeInForce != "" && request.TimeInForce != TimeInForceIOC {
			return validationError("Futures market order time in force must be IOC or empty")
		}
	default:
		return validationError("Futures order type must be limit or market")
	}
	return nil
}

func (request PlaceOrderRequest) canonical() PlaceOrderRequest {
	if request.Type == OrderTypeMarket {
		request.Price = "0"
		request.TimeInForce = TimeInForceIOC
	} else if request.TimeInForce == "" {
		request.TimeInForce = TimeInForceGTC
	}
	return request
}

func (request OrderInfoRequest) validate() error {
	if err := request.Settlement.validate(); err != nil {
		return err
	}
	return validateOrderIdentity(request.OrderID)
}

func (request CancelOrderRequest) validate() error {
	if err := request.Settlement.validate(); err != nil {
		return err
	}
	return validateOrderIdentity(request.OrderID)
}

func (request OrdersRequest) validate() error {
	if err := request.Settlement.validate(); err != nil {
		return err
	}
	if request.Contract != "" {
		if err := validateContract(request.Contract); err != nil {
			return err
		}
	}
	if request.Status != OrderStatusOpen && request.Status != OrderStatusFinished {
		return validationError("Futures order status must be open or finished")
	}
	return validatePage(request.Limit, request.Offset, 1000)
}

func (request OrdersRequest) values() url.Values {
	values := url.Values{"status": {string(request.Status)}}
	setIfNotEmpty(values, "contract", request.Contract)
	setPositiveInt(values, "limit", request.Limit)
	setPositiveInt(values, "offset", request.Offset)
	return values
}

func (request MyTradesRequest) validate() error {
	if err := request.Settlement.validate(); err != nil {
		return err
	}
	if request.Contract != "" {
		if err := validateContract(request.Contract); err != nil {
			return err
		}
	}
	if request.OrderID != "" && !identifierNumberPattern.MatchString(request.OrderID) {
		return validationError("Futures trade order ID must be numeric")
	}
	return validatePage(request.Limit, request.Offset, 1000)
}

func (request MyTradesRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "contract", request.Contract)
	setIfNotEmpty(values, "order", request.OrderID)
	setPositiveInt(values, "limit", request.Limit)
	setPositiveInt(values, "offset", request.Offset)
	return values
}

func (settlement Settlement) validate() error {
	if settlement != SettlementBTC && settlement != SettlementUSDT && settlement != SettlementUSD1 {
		return validationError("unsupported Futures settlement %q", settlement)
	}
	return nil
}

func (interval CandleInterval) valid() bool {
	switch interval {
	case Candle10Seconds, Candle1Minute, Candle5Minutes, Candle15Minutes,
		Candle30Minutes, Candle1Hour, Candle4Hours, Candle8Hours,
		Candle1Day, Candle7Days, Candle1Week:
		return true
	default:
		return false
	}
}

func validateSettlementContract(settlement Settlement, contract string) error {
	if err := settlement.validate(); err != nil {
		return err
	}
	return validateContract(contract)
}

func validateContract(contract string) error {
	if !contractPattern.MatchString(contract) {
		return validationError("invalid Futures contract %q", contract)
	}
	return nil
}

func validateOrderIdentity(value string) error {
	if !orderIdentityPattern.MatchString(value) {
		return validationError("invalid Futures order ID")
	}
	return nil
}

func validatePositiveDecimal(name, value string) error {
	if !positiveDecimal.MatchString(value) || strings.Trim(value, "0.") == "" {
		return validationError("%s must be a positive decimal", name)
	}
	return nil
}

func validatePage(limit, offset, maximumLimit int) error {
	if limit < 0 || limit > maximumLimit {
		return validationError("limit must be between 1 and %d or zero", maximumLimit)
	}
	if offset < 0 || offset > 100000 {
		return validationError("offset must be between 0 and 100000")
	}
	return nil
}

func validateTimeRange(from, to *time.Time, conflictsWithLimit bool) error {
	if conflictsWithLimit && (from != nil || to != nil) {
		return validationError("limit cannot be combined with from or to")
	}
	if from != nil && from.Unix() <= 0 || to != nil && to.Unix() <= 0 {
		return validationError("time range must be after the Unix epoch")
	}
	if from != nil && to != nil && !from.Before(*to) {
		return validationError("time range must have from before to")
	}
	return nil
}

func setTimes(values url.Values, from, to *time.Time) {
	if from != nil {
		values.Set("from", strconv.FormatInt(from.Unix(), 10))
	}
	if to != nil {
		values.Set("to", strconv.FormatInt(to.Unix(), 10))
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

func validationError(format string, values ...any) error {
	return &trade.APIError{
		Category: trade.ErrorValidation, Exchange: model.ExchangeGateIO,
		Cause: fmt.Errorf(format, values...),
	}
}
