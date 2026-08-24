// Package unified는 거래소 간 의미가 같은 Spot 기능의 공통 계약을 제공한다.
package unified

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	trade "github.com/proven-trade/proven-trade-sdk"
	"github.com/proven-trade/proven-trade-sdk/model"
)

var (
	assetPattern           = regexp.MustCompile(`^[A-Z0-9]+$`)
	positiveDecimalPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
)

// Market은 기준 자산과 결제 자산으로 거래 상품을 식별한다.
type Market struct {
	Base  string
	Quote string
}

// String은 공통 마켓을 BASE/QUOTE 형식으로 반환한다.
func (market Market) String() string {
	return market.Base + "/" + market.Quote
}

// Validate는 공통 마켓 자산 코드를 검증한다.
func (market Market) Validate() error {
	if !assetPattern.MatchString(market.Base) || !assetPattern.MatchString(market.Quote) {
		return validationError("market assets must use uppercase letters and digits")
	}
	if market.Base == market.Quote {
		return validationError("market base and quote assets must differ")
	}
	return nil
}

// Side는 주문의 매수 또는 매도 방향이다.
type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// OrderType은 공통 주문 가격 결정 방식이다.
type OrderType string

const (
	OrderTypeLimit  OrderType = "limit"
	OrderTypeMarket OrderType = "market"
)

// TimeInForce는 공통 주문 체결 및 만료 정책이다.
type TimeInForce string

const (
	TimeInForceGTC      TimeInForce = "gtc"
	TimeInForceIOC      TimeInForce = "ioc"
	TimeInForceFOK      TimeInForce = "fok"
	TimeInForcePostOnly TimeInForce = "post_only"
)

// OrderStatus는 거래소 주문 상태를 공통 의미로 분류한다.
type OrderStatus string

const (
	OrderStatusNew             OrderStatus = "new"
	OrderStatusPartiallyFilled OrderStatus = "partially_filled"
	OrderStatusFilled          OrderStatus = "filled"
	OrderStatusCanceled        OrderStatus = "canceled"
	OrderStatusRejected        OrderStatus = "rejected"
	OrderStatusExpired         OrderStatus = "expired"
	OrderStatusUnknown         OrderStatus = "unknown"
)

// Ticker는 마켓의 최신 체결 가격이다.
type Ticker struct {
	Exchange     model.ExchangeID
	Market       Market
	NativeMarket string
	Price        string
	Raw          json.RawMessage
}

// MarketInfo는 거래 가능한 공통 마켓과 거래소 원본 상태를 제공한다.
type MarketInfo struct {
	Exchange     model.ExchangeID
	Market       Market
	NativeMarket string
	Status       string
	Raw          json.RawMessage
}

// BookLevel은 호가 한 단계의 가격과 수량이다.
type BookLevel struct {
	Price    string
	Quantity string
}

// OrderBook은 공통 호가 스냅샷이다.
type OrderBook struct {
	Exchange     model.ExchangeID
	Market       Market
	NativeMarket string
	Bids         []BookLevel
	Asks         []BookLevel
	Timestamp    int64
	Raw          json.RawMessage
}

// PublicTrade는 공개 최근 체결 한 건이다.
type PublicTrade struct {
	ID        string
	Price     string
	Quantity  string
	Side      Side
	Timestamp int64
}

// CandleInterval은 P0 거래소가 공통 지원하는 캔들 구간이다.
type CandleInterval string

const (
	Candle1Minute   CandleInterval = "1m"
	Candle3Minutes  CandleInterval = "3m"
	Candle5Minutes  CandleInterval = "5m"
	Candle15Minutes CandleInterval = "15m"
	Candle30Minutes CandleInterval = "30m"
	Candle1Hour     CandleInterval = "1h"
	Candle4Hours    CandleInterval = "4h"
)

// Candle은 공통 OHLCV 캔들 한 건이다.
type Candle struct {
	StartTime int64
	Open      string
	High      string
	Low       string
	Close     string
	Volume    string
}

// Balance는 자산의 주문 가능 수량과 잠금 수량이다.
type Balance struct {
	Asset     string
	Available string
	Locked    string
	Raw       json.RawMessage
}

// Order는 공통 주문 상태와 거래소 원본 응답을 함께 보존한다.
type Order struct {
	Exchange         model.ExchangeID
	ID               string
	ClientOrderID    string
	Market           Market
	NativeMarket     string
	Side             Side
	Type             OrderType
	Status           OrderStatus
	Price            string
	Quantity         string
	ExecutedQuantity string
	Raw              json.RawMessage
}

// TickerRequest는 최신 가격을 조회할 마켓이다.
type TickerRequest struct {
	Market Market
}

// OrderBookRequest는 호가를 조회할 마켓과 깊이다.
type OrderBookRequest struct {
	Market Market
	Limit  int
}

// RecentTradesRequest는 최근 공개 체결 조회 조건이다.
type RecentTradesRequest struct {
	Market Market
	Limit  int
}

// CandlesRequest는 공통 OHLCV 캔들 조회 조건이다.
type CandlesRequest struct {
	Market   Market
	Interval CandleInterval
	Limit    int
}

// PlaceOrderRequest는 거래소 공통 범위의 Spot 신규 주문이다.
// 시장가 매수는 QuoteAmount, 시장가 매도는 Quantity를 사용한다.
type PlaceOrderRequest struct {
	Market        Market
	Side          Side
	Type          OrderType
	TimeInForce   TimeInForce
	Quantity      string
	QuoteAmount   string
	Price         string
	ClientOrderID string
}

// OrderRequest는 거래소 주문 ID 또는 사용자 주문 ID로 단건 주문을 지정한다.
type OrderRequest struct {
	Market        Market
	OrderID       string
	ClientOrderID string
}

// OpenOrdersRequest는 단일 마켓 또는 전체 마켓의 미체결 주문을 지정한다.
type OpenOrdersRequest struct {
	Market     *Market
	AllMarkets bool
}

// Validate는 현재가 요청을 검증한다.
func (request TickerRequest) Validate() error {
	return request.Market.Validate()
}

// Validate는 공통 호가 요청을 검증한다.
func (request OrderBookRequest) Validate() error {
	if err := request.Market.Validate(); err != nil {
		return err
	}
	if request.Limit < 0 || request.Limit > 16 {
		return validationError("order book limit must be between 1 and 16 or zero for default")
	}
	return nil
}

// Validate는 공통 최근 체결 요청을 검증한다.
func (request RecentTradesRequest) Validate() error {
	if err := request.Market.Validate(); err != nil {
		return err
	}
	if request.Limit < 0 || request.Limit > 100 {
		return validationError("recent trade limit must be between 1 and 100 or zero for default")
	}
	return nil
}

// Validate는 공통 캔들 요청을 검증한다.
func (request CandlesRequest) Validate() error {
	if err := request.Market.Validate(); err != nil {
		return err
	}
	if !request.Interval.valid() {
		return validationError("unsupported candle interval %q", request.Interval)
	}
	if request.Limit < 0 || request.Limit > 200 {
		return validationError("candle limit must be between 1 and 200 or zero for default")
	}
	return nil
}

// Validate는 공통 주문 입력의 상호 배타 조건을 검증한다.
func (request PlaceOrderRequest) Validate() error {
	if err := request.Market.Validate(); err != nil {
		return err
	}
	if request.Side != SideBuy && request.Side != SideSell {
		return validationError("side must be buy or sell")
	}
	if strings.TrimSpace(request.ClientOrderID) != request.ClientOrderID {
		return validationError("client order ID cannot have surrounding whitespace")
	}
	switch request.Type {
	case OrderTypeLimit:
		if err := validatePositiveDecimal("quantity", request.Quantity); err != nil {
			return err
		}
		if err := validatePositiveDecimal("price", request.Price); err != nil {
			return err
		}
		if request.QuoteAmount != "" {
			return validationError("limit order does not accept quote amount")
		}
		if request.TimeInForce != "" && !request.TimeInForce.valid() {
			return validationError("unsupported time in force %q", request.TimeInForce)
		}
	case OrderTypeMarket:
		if request.Price != "" || request.TimeInForce != "" {
			return validationError("market order does not accept price or time in force")
		}
		if request.Side == SideBuy {
			if err := validatePositiveDecimal("quote amount", request.QuoteAmount); err != nil {
				return err
			}
			if request.Quantity != "" {
				return validationError("market buy does not accept quantity")
			}
		} else {
			if err := validatePositiveDecimal("quantity", request.Quantity); err != nil {
				return err
			}
			if request.QuoteAmount != "" {
				return validationError("market sell does not accept quote amount")
			}
		}
	default:
		return validationError("order type must be limit or market")
	}
	return nil
}

// Validate는 단건 주문 식별자와 마켓을 검증한다.
func (request OrderRequest) Validate() error {
	if err := request.Market.Validate(); err != nil {
		return err
	}
	if (request.OrderID == "") == (request.ClientOrderID == "") {
		return validationError("exactly one of order ID or client order ID is required")
	}
	value := request.OrderID
	if value == "" {
		value = request.ClientOrderID
	}
	if strings.TrimSpace(value) != value {
		return validationError("order identity cannot have surrounding whitespace")
	}
	return nil
}

// Validate는 미체결 주문의 조회 범위를 검증한다.
func (request OpenOrdersRequest) Validate() error {
	if request.Market == nil {
		if !request.AllMarkets {
			return validationError("market is required unless AllMarkets is true")
		}
		return nil
	}
	if request.AllMarkets {
		return validationError("market and AllMarkets cannot be used together")
	}
	return request.Market.Validate()
}

func (value TimeInForce) valid() bool {
	return value == TimeInForceGTC || value == TimeInForceIOC ||
		value == TimeInForceFOK || value == TimeInForcePostOnly
}

func (value CandleInterval) valid() bool {
	switch value {
	case Candle1Minute, Candle3Minutes, Candle5Minutes, Candle15Minutes,
		Candle30Minutes, Candle1Hour, Candle4Hours:
		return true
	default:
		return false
	}
}

func validatePositiveDecimal(name, value string) error {
	if !positiveDecimalPattern.MatchString(value) || strings.Trim(value, "0.") == "" {
		return validationError("%s must be a positive decimal string", name)
	}
	return nil
}

func validationError(format string, values ...any) error {
	return fmt.Errorf("%w: %s", trade.ErrValidation, fmt.Sprintf(format, values...))
}
