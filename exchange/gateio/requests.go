package gateio

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
	currencyPairPattern  = regexp.MustCompile(`^[A-Z0-9]{2,20}_[A-Z0-9]{2,20}$`)
	currencyPattern      = regexp.MustCompile(`^[A-Z0-9]{2,20}$`)
	orderIDPattern       = regexp.MustCompile(`^[0-9A-Za-z._-]{1,64}$`)
	clientOrderIDPattern = regexp.MustCompile(`^t-[0-9A-Za-z._-]{1,28}$`)
	positiveDecimal      = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
)

// OrderBookRequest는 Spot 호가 조회 조건이다.
type OrderBookRequest struct {
	CurrencyPair string
	Limit        int
}

// TradesRequest는 공개 Spot 체결 조회 조건이다.
type TradesRequest struct {
	CurrencyPair string
	Limit        int
}

// CandleInterval은 Spot 캔들 구간이다.
type CandleInterval string

const (
	Candle1Second   CandleInterval = "1s"
	Candle10Seconds CandleInterval = "10s"
	Candle1Minute   CandleInterval = "1m"
	Candle5Minutes  CandleInterval = "5m"
	Candle15Minutes CandleInterval = "15m"
	Candle30Minutes CandleInterval = "30m"
	Candle1Hour     CandleInterval = "1h"
	Candle4Hours    CandleInterval = "4h"
	Candle8Hours    CandleInterval = "8h"
	Candle1Day      CandleInterval = "1d"
	Candle7Days     CandleInterval = "7d"
	Candle30Days    CandleInterval = "30d"
)

// CandlesRequest는 최근 개수 또는 시간 범위 기반 Spot 캔들 조회 조건이다.
type CandlesRequest struct {
	CurrencyPair string
	Interval     CandleInterval
	Limit        int
	From         *time.Time
	To           *time.Time
}

// AccountsRequest는 선택적인 통화별 Spot 잔고 조회 조건이다.
type AccountsRequest struct{ Currency string }

// PlaceOrderRequest는 Gate.io 순수 Spot 지정가 또는 시장가 주문이다.
type PlaceOrderRequest struct {
	ClientOrderID string      `json:"text"`
	CurrencyPair  string      `json:"currency_pair"`
	Type          OrderType   `json:"type"`
	Account       string      `json:"account"`
	Side          Side        `json:"side"`
	Amount        string      `json:"amount"`
	Price         string      `json:"price,omitempty"`
	TimeInForce   TimeInForce `json:"time_in_force"`
}

// OrderInfoRequest는 주문 ID와 거래쌍으로 주문 한 건을 조회한다.
type OrderInfoRequest struct {
	OrderID      string
	CurrencyPair string
}

// CancelOrderRequest는 주문 ID와 거래쌍으로 취소 대상을 지정한다.
type CancelOrderRequest struct {
	OrderID      string
	CurrencyPair string
}

// OpenOrdersRequest는 거래쌍별 미체결 주문의 페이지 조회 조건이다.
type OpenOrdersRequest struct {
	Page  int
	Limit int
}

// MyTradesRequest는 계정 Spot 체결 이력 조회 조건이다.
type MyTradesRequest struct {
	CurrencyPair string
	OrderID      string
	Page         int
	Limit        int
}

func (request OrderBookRequest) validate() error {
	if err := validateCurrencyPair(request.CurrencyPair); err != nil {
		return err
	}
	if request.Limit < 0 || request.Limit > 100 {
		return validationError("order book limit must be between 1 and 100 or zero")
	}
	return nil
}

func (request OrderBookRequest) values() url.Values {
	values := url.Values{"currency_pair": {request.CurrencyPair}, "with_id": {"true"}}
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request TradesRequest) validate() error {
	if err := validateCurrencyPair(request.CurrencyPair); err != nil {
		return err
	}
	return validateLimit(request.Limit)
}

func (request TradesRequest) values() url.Values {
	values := url.Values{"currency_pair": {request.CurrencyPair}}
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request CandlesRequest) validate() error {
	if err := validateCurrencyPair(request.CurrencyPair); err != nil {
		return err
	}
	if !request.Interval.valid() {
		return validationError("unsupported candle interval %q", request.Interval)
	}
	if request.Limit < 0 || request.Limit > 1000 {
		return validationError("candle limit must be between 1 and 1000 or zero")
	}
	if request.Limit > 0 && (request.From != nil || request.To != nil) {
		return validationError("candle limit cannot be combined with from or to")
	}
	if request.From != nil && request.From.Unix() <= 0 || request.To != nil && request.To.Unix() <= 0 {
		return validationError("candle timestamps must be after the Unix epoch")
	}
	if request.From != nil && request.To != nil && !request.From.Before(*request.To) {
		return validationError("candle from must be before to")
	}
	return nil
}

func (request CandlesRequest) values() url.Values {
	values := url.Values{"currency_pair": {request.CurrencyPair}, "interval": {string(request.Interval)}}
	setPositiveInt(values, "limit", request.Limit)
	if request.From != nil {
		values.Set("from", strconv.FormatInt(request.From.Unix(), 10))
	}
	if request.To != nil {
		values.Set("to", strconv.FormatInt(request.To.Unix(), 10))
	}
	return values
}

func (request AccountsRequest) validate() error {
	if request.Currency != "" && !currencyPattern.MatchString(request.Currency) {
		return validationError("invalid currency %q", request.Currency)
	}
	return nil
}

func (request AccountsRequest) values() url.Values {
	values := make(url.Values)
	if request.Currency != "" {
		values.Set("currency", request.Currency)
	}
	return values
}

func (request PlaceOrderRequest) validate() error {
	if !clientOrderIDPattern.MatchString(request.ClientOrderID) {
		return validationError("client order ID must start with t- and contain 1 to 28 supported characters")
	}
	if err := validateCurrencyPair(request.CurrencyPair); err != nil {
		return err
	}
	if request.Account != "spot" {
		return validationError("account must be spot")
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("order side must be buy or sell")
	}
	if err := validatePositiveDecimal("amount", request.Amount); err != nil {
		return err
	}
	switch request.Type {
	case OrderTypeLimit:
		if err := validatePositiveDecimal("price", request.Price); err != nil {
			return err
		}
		if request.TimeInForce != TimeInForceGTC && request.TimeInForce != TimeInForceIOC && request.TimeInForce != TimeInForcePOC && request.TimeInForce != TimeInForceFOK {
			return validationError("unsupported limit time in force %q", request.TimeInForce)
		}
	case OrderTypeMarket:
		if request.Price != "" {
			return validationError("market order does not accept price")
		}
		if request.TimeInForce != TimeInForceIOC && request.TimeInForce != TimeInForceFOK {
			return validationError("market order time in force must be ioc or fok")
		}
	default:
		return validationError("order type must be limit or market")
	}
	return nil
}

func (request OrderInfoRequest) validate() error {
	return validateOrderIdentity(request.OrderID, request.CurrencyPair)
}
func (request CancelOrderRequest) validate() error {
	return validateOrderIdentity(request.OrderID, request.CurrencyPair)
}

func (request OpenOrdersRequest) validate() error { return validatePage(request.Page, request.Limit) }
func (request OpenOrdersRequest) values() url.Values {
	values := url.Values{"account": {"spot"}}
	setPositiveInt(values, "page", request.Page)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func (request MyTradesRequest) validate() error {
	if request.CurrencyPair != "" {
		if err := validateCurrencyPair(request.CurrencyPair); err != nil {
			return err
		}
	}
	if request.OrderID != "" {
		if request.CurrencyPair == "" {
			return validationError("currency pair is required with order ID")
		}
		if !orderIDPattern.MatchString(request.OrderID) {
			return validationError("invalid order ID")
		}
	}
	return validatePage(request.Page, request.Limit)
}

func (request MyTradesRequest) values() url.Values {
	values := make(url.Values)
	if request.CurrencyPair != "" {
		values.Set("currency_pair", request.CurrencyPair)
	}
	if request.OrderID != "" {
		values.Set("order_id", request.OrderID)
	}
	setPositiveInt(values, "page", request.Page)
	setPositiveInt(values, "limit", request.Limit)
	return values
}

func validateCurrencyPair(value string) error {
	if !currencyPairPattern.MatchString(value) {
		return validationError("invalid currency pair %q", value)
	}
	return nil
}

func validateOrderIdentity(orderID, currencyPair string) error {
	if !orderIDPattern.MatchString(orderID) {
		return validationError("invalid order ID")
	}
	return validateCurrencyPair(currencyPair)
}

func validatePositiveDecimal(name, value string) error {
	if !positiveDecimal.MatchString(value) || strings.Trim(value, "0.") == "" {
		return validationError("%s must be a positive decimal", name)
	}
	return nil
}

func validatePage(page, limit int) error {
	if page < 0 {
		return validationError("page cannot be negative")
	}
	if limit < 0 || limit > 1000 {
		return validationError("limit must be between 1 and 1000 or zero")
	}
	if page > 0 && limit > 0 && int64(limit)*int64(page-1) > 100000 {
		return validationError("page offset cannot exceed 100000")
	}
	return nil
}

func validateLimit(limit int) error {
	if limit < 0 || limit > 1000 {
		return validationError("limit must be between 1 and 1000 or zero")
	}
	return nil
}

func (interval CandleInterval) valid() bool {
	switch interval {
	case Candle1Second, Candle10Seconds, Candle1Minute, Candle5Minutes, Candle15Minutes,
		Candle30Minutes, Candle1Hour, Candle4Hours, Candle8Hours, Candle1Day, Candle7Days, Candle30Days:
		return true
	default:
		return false
	}
}

func setPositiveInt(values url.Values, key string, value int) {
	if value > 0 {
		values.Set(key, strconv.Itoa(value))
	}
}

func validationError(format string, values ...any) error {
	return &trade.APIError{Category: trade.ErrorValidation, Exchange: model.ExchangeGateIO, Cause: fmt.Errorf(format, values...)}
}
