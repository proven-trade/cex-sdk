package kucoin

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
	symbolPattern        = regexp.MustCompile(`^[A-Z0-9]{2,20}-[A-Z0-9]{2,20}$`)
	currencyPattern      = regexp.MustCompile(`^[A-Z0-9]{2,20}$`)
	clientOrderIDPattern = regexp.MustCompile(`^[0-9A-Za-z_-]{1,40}$`)
	orderIDPattern       = regexp.MustCompile(`^[0-9A-Za-z_-]{1,64}$`)
	positiveDecimalRegex = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
)

// OrderBookSize는 부분 호가 스냅샷의 깊이다.
type OrderBookSize int

const (
	OrderBook20  OrderBookSize = 20
	OrderBook100 OrderBookSize = 100
)

// OrderBookRequest는 부분 호가 스냅샷 조회 조건이다.
type OrderBookRequest struct {
	Symbol string
	Size   OrderBookSize
}

// RecentTradesRequest는 공개 최근 체결 조회 조건이다.
type RecentTradesRequest struct {
	Symbol string
}

// CandleInterval은 KuCoin Classic 캔들 구간이다.
type CandleInterval string

const (
	Candle1Minute   CandleInterval = "1min"
	Candle3Minutes  CandleInterval = "3min"
	Candle5Minutes  CandleInterval = "5min"
	Candle15Minutes CandleInterval = "15min"
	Candle30Minutes CandleInterval = "30min"
	Candle1Hour     CandleInterval = "1hour"
	Candle2Hours    CandleInterval = "2hour"
	Candle4Hours    CandleInterval = "4hour"
	Candle6Hours    CandleInterval = "6hour"
	Candle8Hours    CandleInterval = "8hour"
	Candle12Hours   CandleInterval = "12hour"
	Candle1Day      CandleInterval = "1day"
	Candle1Week     CandleInterval = "1week"
)

// CandlesRequest는 최대 1500개 공개 OHLCV 조회 조건이다.
type CandlesRequest struct {
	Symbol   string
	Interval CandleInterval
	Start    *time.Time
	End      *time.Time
}

// AccountsRequest는 통화와 계정 유형별 잔고 조회 조건이다.
type AccountsRequest struct {
	Currency string
	Type     AccountType
}

// PlaceOrderRequest는 Classic Spot 지정가 또는 시장가 주문이다.
type PlaceOrderRequest struct {
	ClientOrderID string      `json:"clientOid"`
	Symbol        string      `json:"symbol"`
	Type          OrderType   `json:"type"`
	Side          Side        `json:"side"`
	Price         string      `json:"price,omitempty"`
	Size          string      `json:"size,omitempty"`
	Funds         string      `json:"funds,omitempty"`
	TimeInForce   TimeInForce `json:"timeInForce,omitempty"`
	PostOnly      bool        `json:"postOnly,omitempty"`
	CancelAfter   int64       `json:"cancelAfter,omitempty"`
	Remark        string      `json:"remark,omitempty"`
	Tags          string      `json:"tags,omitempty"`
}

// OrderInfoRequest는 거래소 주문 ID 또는 사용자 주문 ID와 거래쌍으로 주문 한 건을 조회한다.
type OrderInfoRequest struct {
	OrderID       string
	ClientOrderID string
	Symbol        string
}

// CancelOrderRequest는 거래소 주문 ID 또는 사용자 주문 ID와 거래쌍으로 주문을 취소한다.
type CancelOrderRequest struct {
	OrderID       string
	ClientOrderID string
	Symbol        string
}

// OpenOrdersRequest는 페이지 기반 미체결 주문 조회 조건이다.
type OpenOrdersRequest struct {
	Symbol     string
	PageNumber int
	PageSize   int
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
	if !request.Interval.valid() {
		return validationError("unsupported candle interval %q", request.Interval)
	}
	if request.Start != nil && request.Start.Unix() <= 0 || request.End != nil && request.End.Unix() <= 0 {
		return validationError("candle timestamps must be after the Unix epoch")
	}
	if request.Start != nil && request.End != nil && !request.Start.Before(*request.End) {
		return validationError("candle start must be before end")
	}
	return nil
}

func (request CandlesRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}, "type": {string(request.Interval)}}
	if request.Start != nil {
		values.Set("startAt", strconv.FormatInt(request.Start.Unix(), 10))
	}
	if request.End != nil {
		values.Set("endAt", strconv.FormatInt(request.End.Unix(), 10))
	}
	return values
}

func (request AccountsRequest) validate() error {
	if request.Currency != "" && !currencyPattern.MatchString(request.Currency) {
		return validationError("invalid account currency %q", request.Currency)
	}
	if request.Type != "" && request.Type != AccountTypeMain && request.Type != AccountTypeTrade {
		return validationError("account type must be main, trade, or empty")
	}
	return nil
}

func (request AccountsRequest) values() url.Values {
	values := make(url.Values)
	setIfNotEmpty(values, "currency", request.Currency)
	setIfNotEmpty(values, "type", string(request.Type))
	return values
}

func (request PlaceOrderRequest) validate() error {
	if !clientOrderIDPattern.MatchString(request.ClientOrderID) {
		return validationError("client order ID is required and must match [0-9A-Za-z_-]{1,40}")
	}
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("order side must be buy or sell")
	}
	if len(request.Remark) > 20 || len(request.Tags) > 20 {
		return validationError("order remark and tags cannot exceed 20 bytes")
	}
	switch request.Type {
	case OrderTypeLimit:
		if err := validatePositiveDecimal("price", request.Price); err != nil {
			return err
		}
		if err := validatePositiveDecimal("size", request.Size); err != nil {
			return err
		}
		if request.Funds != "" {
			return validationError("limit order does not accept funds")
		}
		if request.TimeInForce != "" && !request.TimeInForce.valid() {
			return validationError("unsupported time in force %q", request.TimeInForce)
		}
		if request.PostOnly && request.TimeInForce != "" && request.TimeInForce != TimeInForceGTC {
			return validationError("post-only order requires GTC or empty time in force")
		}
		if request.CancelAfter > 0 && request.TimeInForce != TimeInForceGTT {
			return validationError("cancelAfter requires GTT time in force")
		}
		if request.CancelAfter < 0 {
			return validationError("cancelAfter cannot be negative")
		}
	case OrderTypeMarket:
		if request.Price != "" || request.TimeInForce != "" || request.PostOnly || request.CancelAfter != 0 {
			return validationError("market order does not accept price or limit execution options")
		}
		if request.Side == SideBuy {
			if (request.Size == "") == (request.Funds == "") {
				return validationError("market buy requires exactly one of size or funds")
			}
			if request.Size != "" {
				if err := validatePositiveDecimal("size", request.Size); err != nil {
					return err
				}
			} else if err := validatePositiveDecimal("funds", request.Funds); err != nil {
				return err
			}
		} else {
			if err := validatePositiveDecimal("size", request.Size); err != nil {
				return err
			}
			if request.Funds != "" {
				return validationError("market sell does not accept funds")
			}
		}
	default:
		return validationError("order type must be limit or market")
	}
	return nil
}

func (request OrderInfoRequest) validate() error {
	if err := validateOrderIdentity(request.OrderID, request.ClientOrderID); err != nil {
		return err
	}
	return validateSymbol(request.Symbol)
}

func (request OrderInfoRequest) values() url.Values {
	return url.Values{"symbol": {request.Symbol}}
}

func (request CancelOrderRequest) validate() error {
	if err := validateOrderIdentity(request.OrderID, request.ClientOrderID); err != nil {
		return err
	}
	return validateSymbol(request.Symbol)
}

func (request CancelOrderRequest) values() url.Values {
	return url.Values{"symbol": {request.Symbol}}
}

func (request OpenOrdersRequest) validate() error {
	if err := validateSymbol(request.Symbol); err != nil {
		return err
	}
	if request.PageNumber < 0 {
		return validationError("open order page number cannot be negative")
	}
	if request.PageSize < 0 || request.PageSize > 50 {
		return validationError("open order page size must be between 1 and 50 or zero for default")
	}
	return nil
}

func (request OpenOrdersRequest) values() url.Values {
	values := url.Values{"symbol": {request.Symbol}}
	setPositiveInt(values, "pageNum", request.PageNumber)
	setPositiveInt(values, "pageSize", request.PageSize)
	return values
}

func validateSymbol(symbol string) error {
	if !symbolPattern.MatchString(symbol) {
		return validationError("invalid symbol %q", symbol)
	}
	base, quote, _ := strings.Cut(symbol, "-")
	if base == quote {
		return validationError("symbol base and quote currencies must differ")
	}
	return nil
}

func validateOrderID(orderID string) error {
	if !orderIDPattern.MatchString(orderID) {
		return validationError("invalid order ID")
	}
	return nil
}

func validateOrderIdentity(orderID, clientOrderID string) error {
	if (orderID == "") == (clientOrderID == "") {
		return validationError("exactly one of order ID or client order ID is required")
	}
	if orderID != "" {
		return validateOrderID(orderID)
	}
	if !clientOrderIDPattern.MatchString(clientOrderID) {
		return validationError("invalid client order ID")
	}
	return nil
}

func validatePositiveDecimal(name, value string) error {
	if !positiveDecimalRegex.MatchString(value) || strings.Trim(value, "0.") == "" {
		return validationError("%s must be a positive decimal", name)
	}
	return nil
}

func (interval CandleInterval) valid() bool {
	switch interval {
	case Candle1Minute, Candle3Minutes, Candle5Minutes, Candle15Minutes, Candle30Minutes,
		Candle1Hour, Candle2Hours, Candle4Hours, Candle6Hours, Candle8Hours,
		Candle12Hours, Candle1Day, Candle1Week:
		return true
	default:
		return false
	}
}

func (value TimeInForce) valid() bool {
	return value == TimeInForceGTC || value == TimeInForceGTT ||
		value == TimeInForceIOC || value == TimeInForceFOK
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
