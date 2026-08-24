package usdm

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	trade "github.com/proven-trade/proven-trade-sdk"
)

var decimalPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

// TickerPriceRequest는 최신 가격을 조회할 계약이다.
type TickerPriceRequest struct{ Symbol string }

// OrderBookRequest는 호가 조회 조건이다.
type OrderBookRequest struct {
	Symbol string
	Limit  int
}

// RecentTradesRequest는 최근 공개 체결 조회 조건이다.
type RecentTradesRequest struct {
	Symbol string
	Limit  int
}

// CandleInterval은 USDⓈ-M 캔들 구간이다.
type CandleInterval string

const (
	Candle1Minute   CandleInterval = "1m"
	Candle3Minutes  CandleInterval = "3m"
	Candle5Minutes  CandleInterval = "5m"
	Candle15Minutes CandleInterval = "15m"
	Candle30Minutes CandleInterval = "30m"
	Candle1Hour     CandleInterval = "1h"
	Candle2Hours    CandleInterval = "2h"
	Candle4Hours    CandleInterval = "4h"
	Candle6Hours    CandleInterval = "6h"
	Candle8Hours    CandleInterval = "8h"
	Candle12Hours   CandleInterval = "12h"
	Candle1Day      CandleInterval = "1d"
	Candle3Days     CandleInterval = "3d"
	Candle1Week     CandleInterval = "1w"
	Candle1Month    CandleInterval = "1M"
)

// CandlesRequest는 OHLCV 캔들 조회 조건이다.
type CandlesRequest struct {
	Symbol    string
	Interval  CandleInterval
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
}

// PlaceOrderRequest는 USDⓈ-M Futures 신규 주문이다.
type PlaceOrderRequest struct {
	Symbol          string
	Side            Side
	PositionSide    PositionSide
	Type            OrderType
	TimeInForce     TimeInForce
	Quantity        string
	ReduceOnly      bool
	Price           string
	ClientOrderID   string
	StopPrice       string
	ClosePosition   bool
	ActivationPrice string
	CallbackRate    string
	WorkingType     WorkingType
	PriceProtect    bool
	ResponseType    string
	GoodTillDate    *time.Time
}

// OrderInfoRequest는 주문 ID 또는 사용자 주문 ID로 단건 주문을 지정한다.
type OrderInfoRequest struct {
	Symbol        string
	OrderID       *int64
	ClientOrderID string
}

// OpenOrdersRequest는 미체결 주문 조회 범위다.
// 전체 계약 조회는 AllSymbols를 명시해야 한다.
type OpenOrdersRequest struct {
	Symbol     string
	AllSymbols bool
}

// PositionsRequest는 전체 또는 단일 계약의 포지션 위험 조회 조건이다.
type PositionsRequest struct{ Symbol string }

// OrderHistoryRequest는 단일 계약의 주문 이력 조회 조건이다.
type OrderHistoryRequest struct {
	Symbol    string
	OrderID   *int64
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
}

func (request OrderBookRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	switch request.Limit {
	case 0, 5, 10, 20, 50, 100, 500, 1000:
		return nil
	default:
		return validationError("depth limit must be 5, 10, 20, 50, 100, 500, 1000, or zero")
	}
}

func (request RecentTradesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Limit < 0 || request.Limit > 1000 {
		return validationError("recent trade limit must be at most 1000")
	}
	return nil
}

func (request CandlesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if !request.Interval.valid() {
		return validationError("unsupported candle interval %q", request.Interval)
	}
	if request.Limit < 0 || request.Limit > 1500 {
		return validationError("candle limit must be at most 1500")
	}
	if request.StartTime != nil && request.EndTime != nil && request.StartTime.After(*request.EndTime) {
		return validationError("startTime cannot be after endTime")
	}
	return nil
}

func (request PlaceOrderRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("side must be BUY or SELL")
	}
	if request.PositionSide != "" && request.PositionSide != PositionSideBoth && request.PositionSide != PositionSideLong && request.PositionSide != PositionSideShort {
		return validationError("invalid positionSide")
	}
	if request.ReduceOnly && (request.PositionSide == PositionSideLong || request.PositionSide == PositionSideShort) {
		return validationError("reduceOnly cannot be sent in hedge mode")
	}
	if request.ClosePosition && (request.Quantity != "" || request.ReduceOnly) {
		return validationError("closePosition cannot be combined with quantity or reduceOnly")
	}
	for name, value := range map[string]string{"quantity": request.Quantity, "price": request.Price, "stopPrice": request.StopPrice, "activationPrice": request.ActivationPrice, "callbackRate": request.CallbackRate} {
		if value != "" && (!decimalPattern.MatchString(value) || strings.Trim(value, "0.") == "") {
			return validationError("%s must be a positive decimal string", name)
		}
	}
	switch request.Type {
	case OrderTypeLimit:
		if request.Quantity == "" || request.Price == "" {
			return validationError("LIMIT requires quantity and price")
		}
		if request.TimeInForce == "" {
			return validationError("LIMIT requires timeInForce")
		}
	case OrderTypeMarket:
		if request.Quantity == "" || request.Price != "" || request.TimeInForce != "" {
			return validationError("MARKET requires quantity and no price or timeInForce")
		}
	case OrderTypeStop, OrderTypeTakeProfit:
		if request.Quantity == "" || request.Price == "" || request.StopPrice == "" {
			return validationError("STOP and TAKE_PROFIT require quantity, price, and stopPrice")
		}
	case OrderTypeStopMarket, OrderTypeTakeProfitMarket:
		if request.StopPrice == "" || (!request.ClosePosition && request.Quantity == "") {
			return validationError("conditional market order requires stopPrice and quantity or closePosition")
		}
	case OrderTypeTrailingStopMarket:
		if request.Quantity == "" || request.CallbackRate == "" {
			return validationError("TRAILING_STOP_MARKET requires quantity and callbackRate")
		}
	default:
		return validationError("unsupported order type %q", request.Type)
	}
	if request.TimeInForce != "" && !request.TimeInForce.valid() {
		return validationError("unsupported timeInForce %q", request.TimeInForce)
	}
	if request.TimeInForce == TimeInForceGTD {
		if request.GoodTillDate == nil || request.GoodTillDate.Unix() <= time.Now().Unix()+600 {
			return validationError("GTD requires goodTillDate at least 600 seconds in the future")
		}
	} else if request.GoodTillDate != nil {
		return validationError("goodTillDate requires GTD")
	}
	if request.WorkingType != "" && request.WorkingType != WorkingTypeContractPrice && request.WorkingType != WorkingTypeMarkPrice {
		return validationError("unsupported workingType")
	}
	if request.ResponseType != "" && request.ResponseType != "ACK" && request.ResponseType != "RESULT" {
		return validationError("responseType must be ACK or RESULT")
	}
	if strings.TrimSpace(request.ClientOrderID) != request.ClientOrderID {
		return validationError("client order ID cannot have surrounding whitespace")
	}
	return nil
}

func (request OrderInfoRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if (request.OrderID == nil) == (request.ClientOrderID == "") {
		return validationError("exactly one order identity is required")
	}
	if request.OrderID != nil && *request.OrderID <= 0 {
		return validationError("orderId must be positive")
	}
	if strings.TrimSpace(request.ClientOrderID) != request.ClientOrderID {
		return validationError("client order ID cannot have surrounding whitespace")
	}
	return nil
}

func (request OpenOrdersRequest) validate() error {
	if request.AllSymbols && request.Symbol != "" {
		return validationError("symbol and AllSymbols cannot be used together")
	}
	if !request.AllSymbols {
		return validateSymbol(request.Symbol)
	}
	return nil
}

func (request OrderHistoryRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.OrderID != nil && *request.OrderID <= 0 {
		return validationError("orderId must be positive")
	}
	if request.Limit < 0 || request.Limit > 1000 {
		return validationError("order history limit must be at most 1000")
	}
	if request.StartTime != nil && request.EndTime != nil {
		if request.StartTime.After(*request.EndTime) {
			return validationError("startTime cannot be after endTime")
		}
		if request.EndTime.Sub(*request.StartTime) > 7*24*time.Hour {
			return validationError("order history range cannot exceed seven days")
		}
	}
	return nil
}

func (request PlaceOrderRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}, "side": {string(request.Side)}, "type": {string(request.Type)}}
	set(values, "positionSide", string(request.PositionSide))
	set(values, "timeInForce", string(request.TimeInForce))
	set(values, "quantity", request.Quantity)
	if request.ReduceOnly {
		values.Set("reduceOnly", "true")
	}
	set(values, "price", request.Price)
	set(values, "newClientOrderId", request.ClientOrderID)
	set(values, "stopPrice", request.StopPrice)
	if request.ClosePosition {
		values.Set("closePosition", "true")
	}
	set(values, "activationPrice", request.ActivationPrice)
	set(values, "callbackRate", request.CallbackRate)
	set(values, "workingType", string(request.WorkingType))
	if request.PriceProtect {
		values.Set("priceProtect", "TRUE")
	}
	set(values, "newOrderRespType", request.ResponseType)
	if request.GoodTillDate != nil {
		values.Set("goodTillDate", strconv.FormatInt(request.GoodTillDate.UnixMilli(), 10))
	}
	return values
}

func (request OrderInfoRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	if request.OrderID != nil {
		values.Set("orderId", strconv.FormatInt(*request.OrderID, 10))
	}
	set(values, "origClientOrderId", request.ClientOrderID)
	return values
}

func (request OrderHistoryRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	if request.OrderID != nil {
		values.Set("orderId", strconv.FormatInt(*request.OrderID, 10))
	}
	if request.StartTime != nil {
		values.Set("startTime", strconv.FormatInt(request.StartTime.UnixMilli(), 10))
	}
	if request.EndTime != nil {
		values.Set("endTime", strconv.FormatInt(request.EndTime.UnixMilli(), 10))
	}
	if request.Limit > 0 {
		values.Set("limit", strconv.Itoa(request.Limit))
	}
	return values
}

func validateSymbol(value string) error {
	if value == "" || strings.TrimSpace(value) != value {
		return validationError("symbol is required without surrounding whitespace")
	}
	return nil
}

func (interval CandleInterval) valid() bool {
	switch interval {
	case Candle1Minute, Candle3Minutes, Candle5Minutes, Candle15Minutes,
		Candle30Minutes, Candle1Hour, Candle2Hours, Candle4Hours, Candle6Hours,
		Candle8Hours, Candle12Hours, Candle1Day, Candle3Days, Candle1Week, Candle1Month:
		return true
	default:
		return false
	}
}

func (value TimeInForce) valid() bool {
	return value == TimeInForceGTC || value == TimeInForceIOC || value == TimeInForceFOK || value == TimeInForceGTD
}

func set(values url.Values, key, value string) {
	if value != "" {
		values.Set(key, value)
	}
}

func validationError(format string, values ...any) error {
	return fmt.Errorf("%w: %s", trade.ErrValidation, fmt.Sprintf(format, values...))
}
