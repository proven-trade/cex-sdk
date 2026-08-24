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
	symbolPattern        = regexp.MustCompile(`^[A-Z0-9]{2,30}$`)
	currencyPattern      = regexp.MustCompile(`^[A-Z0-9]{2,20}$`)
	clientOrderIDPattern = regexp.MustCompile(`^[0-9A-Za-z_-]{1,40}$`)
	orderIDPattern       = regexp.MustCompile(`^[0-9A-Za-z_-]{1,64}$`)
	positiveDecimal      = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
)

// OrderBookSize는 부분 Futures 호가 snapshot의 깊이다.
type OrderBookSize int

const (
	OrderBook20  OrderBookSize = 20
	OrderBook100 OrderBookSize = 100
)

// OrderBookRequest는 부분 Futures 호가 조회 조건이다.
type OrderBookRequest struct {
	Symbol string
	Size   OrderBookSize
}

// RecentTradesRequest는 공개 Futures 최근 체결 조회 조건이다.
type RecentTradesRequest struct {
	Symbol string
}

// CandleGranularity는 Futures 캔들 구간의 초 단위 값이다.
type CandleGranularity int64

const (
	Candle1Minute   CandleGranularity = 60
	Candle5Minutes  CandleGranularity = 300
	Candle15Minutes CandleGranularity = 900
	Candle30Minutes CandleGranularity = 1800
	Candle1Hour     CandleGranularity = 3600
	Candle2Hours    CandleGranularity = 7200
	Candle4Hours    CandleGranularity = 14400
	Candle8Hours    CandleGranularity = 28800
	Candle1Day      CandleGranularity = 86400
	Candle1Week     CandleGranularity = 604800
)

// CandlesRequest는 최대 500개 Futures OHLCV 조회 조건이다.
type CandlesRequest struct {
	Symbol      string
	Granularity CandleGranularity
	From        *time.Time
	To          *time.Time
}

// AccountOverviewRequest는 결제 통화별 Futures 계정 조회 조건이다.
type AccountOverviewRequest struct {
	Currency string
}

// PositionsRequest는 선택적인 결제 통화별 Futures 포지션 조회 조건이다.
type PositionsRequest struct {
	Currency string
}

// PlaceOrderRequest는 Classic Futures 지정가 또는 시장가 주문이다.
type PlaceOrderRequest struct {
	ClientOrderID string       `json:"clientOid"`
	Symbol        string       `json:"symbol"`
	MarginMode    MarginMode   `json:"marginMode"`
	Leverage      int          `json:"leverage,omitempty"`
	PositionSide  PositionSide `json:"positionSide,omitempty"`
	Side          Side         `json:"side"`
	Type          OrderType    `json:"type"`
	Size          int64        `json:"size"`
	Price         string       `json:"price,omitempty"`
	TimeInForce   TimeInForce  `json:"timeInForce,omitempty"`
	PostOnly      bool         `json:"postOnly,omitempty"`
	ReduceOnly    bool         `json:"reduceOnly,omitempty"`
	Remark        string       `json:"remark,omitempty"`
}

// OrderInfoRequest는 거래소 Futures 주문 ID로 주문 한 건을 조회한다.
type OrderInfoRequest struct {
	OrderID string
}

// CancelOrderRequest는 Futures 주문 ID 또는 사용자 주문 ID로 취소 대상을 지정한다.
type CancelOrderRequest struct {
	OrderID       string
	ClientOrderID string
}

// OpenOrdersRequest는 활성 Futures 주문의 페이지 조회 조건이다.
type OpenOrdersRequest struct {
	Symbol      string
	CurrentPage int
	PageSize    int
}

// FillsRequest는 Futures 체결 이력의 페이지 조회 조건이다.
type FillsRequest struct {
	OrderID     string
	Symbol      string
	CurrentPage int
	PageSize    int
}

func (request OrderBookRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Size != OrderBook20 && request.Size != OrderBook100 {
		return validationError("order book size must be 20 or 100")
	}
	return nil
}

func (request RecentTradesRequest) validate() error {
	return validateSymbol(request.Symbol)
}

func (request CandlesRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if !request.Granularity.valid() {
		return validationError("unsupported candle granularity %d", request.Granularity)
	}
	if (request.From == nil) != (request.To == nil) {
		return validationError("candle from and to must be supplied together")
	}
	if request.From == nil {
		return nil
	}
	if request.From.UnixMilli() <= 0 || request.To.UnixMilli() <= 0 || !request.From.Before(*request.To) {
		return validationError("candle range must be after the Unix epoch with from before to")
	}
	maximumRange := time.Duration(request.Granularity) * time.Second * 500
	if request.To.Sub(*request.From) > maximumRange {
		return validationError("candle range cannot exceed 500 intervals")
	}
	return nil
}

func (request CandlesRequest) values() url.Values {
	values := url.Values{
		"symbol": {request.Symbol}, "granularity": {strconv.FormatInt(int64(request.Granularity), 10)},
	}
	if request.From != nil {
		values.Set("from", strconv.FormatInt(request.From.UnixMilli(), 10))
		values.Set("to", strconv.FormatInt(request.To.UnixMilli(), 10))
	}
	return values
}

func (request AccountOverviewRequest) validate() error {
	if !currencyPattern.MatchString(request.Currency) {
		return validationError("invalid Futures account currency %q", request.Currency)
	}
	return nil
}

func (request PositionsRequest) validate() error {
	if request.Currency != "" && !currencyPattern.MatchString(request.Currency) {
		return validationError("invalid Futures position currency %q", request.Currency)
	}
	return nil
}

func (request PositionsRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "currency", request.Currency)
	return values
}

func (request PlaceOrderRequest) validate() error {
	if !clientOrderIDPattern.MatchString(request.ClientOrderID) {
		return validationError("client order ID is required and must match [0-9A-Za-z_-]{1,40}")
	}
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.MarginMode != MarginModeIsolated && request.MarginMode != MarginModeCross {
		return validationError("margin mode must be ISOLATED or CROSS")
	}
	if request.Leverage < 0 || request.Leverage > 1_000 {
		return validationError("leverage must be between 1 and 1000 or zero")
	}
	if request.PositionSide != "" && request.PositionSide != PositionSideBoth &&
		request.PositionSide != PositionSideLong && request.PositionSide != PositionSideShort {
		return validationError("position side must be BOTH, LONG, SHORT, or empty")
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("order side must be buy or sell")
	}
	if request.Size <= 0 {
		return validationError("order size must be a positive contract count")
	}
	if len(request.Remark) > 100 {
		return validationError("order remark cannot exceed 100 bytes")
	}
	switch request.Type {
	case OrderTypeLimit:
		if err := validatePositiveDecimal("price", request.Price); err != nil {
			return err
		}
		if request.TimeInForce != "" && request.TimeInForce != TimeInForceGTC &&
			request.TimeInForce != TimeInForceIOC {
			return validationError("unsupported time in force %q", request.TimeInForce)
		}
		if request.PostOnly && request.TimeInForce == TimeInForceIOC {
			return validationError("post-only order cannot use IOC")
		}
	case OrderTypeMarket:
		if request.Price != "" || request.TimeInForce != "" || request.PostOnly {
			return validationError("market order does not accept limit execution options")
		}
	default:
		return validationError("order type must be limit or market")
	}
	return nil
}

func (request OrderInfoRequest) validate() error {
	return validateOrderID(request.OrderID)
}

func (request CancelOrderRequest) validate() error {
	if (request.OrderID == "") == (request.ClientOrderID == "") {
		return validationError("exactly one of order ID or client order ID is required")
	}
	if request.OrderID != "" {
		return validateOrderID(request.OrderID)
	}
	if !clientOrderIDPattern.MatchString(request.ClientOrderID) {
		return validationError("invalid client order ID")
	}
	return nil
}

func (request OpenOrdersRequest) validate() error {
	if request.Symbol != "" {
		if err := validateSymbol(request.Symbol); err != nil {
			return err
		}
	}
	return validatePage(request.CurrentPage, request.PageSize)
}

func (request OpenOrdersRequest) values() url.Values {
	values := url.Values{"status": {"active"}}
	setIfNotEmpty(values, "symbol", request.Symbol)
	setPositiveInt(values, "currentPage", request.CurrentPage)
	setPositiveInt(values, "pageSize", request.PageSize)
	return values
}

func (request FillsRequest) validate() error {
	if request.OrderID != "" {
		if err := validateOrderID(request.OrderID); err != nil {
			return err
		}
	}
	if request.Symbol != "" {
		if err := validateSymbol(request.Symbol); err != nil {
			return err
		}
	}
	return validatePage(request.CurrentPage, request.PageSize)
}

func (request FillsRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "orderId", request.OrderID)
	setIfNotEmpty(values, "symbol", request.Symbol)
	setPositiveInt(values, "currentPage", request.CurrentPage)
	setPositiveInt(values, "pageSize", request.PageSize)
	return values
}

func validatePage(currentPage, pageSize int) error {
	if currentPage < 0 {
		return validationError("current page cannot be negative")
	}
	if pageSize < 0 || pageSize > 50 {
		return validationError("page size must be between 1 and 50 or zero for default")
	}
	return nil
}

func validateSymbol(symbol string) error {
	if !symbolPattern.MatchString(symbol) {
		return validationError("invalid Futures symbol %q", symbol)
	}
	return nil
}

func validateOrderID(orderID string) error {
	if !orderIDPattern.MatchString(orderID) {
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

func (granularity CandleGranularity) valid() bool {
	switch granularity {
	case Candle1Minute, Candle5Minutes, Candle15Minutes, Candle30Minutes,
		Candle1Hour, Candle2Hours, Candle4Hours, Candle8Hours, Candle1Day, Candle1Week:
		return true
	default:
		return false
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
		Category: trade.ErrorValidation, Exchange: model.ExchangeKuCoin,
		Cause: fmt.Errorf(format, values...),
	}
}
