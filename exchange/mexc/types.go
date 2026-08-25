// Package mexc는 MEXC Spot V3 REST 어댑터를 제공한다.
package mexc

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Scalar는 문자열·숫자·null로 달라질 수 있는 MEXC 식별자 원형이다.
type Scalar string

// UnmarshalJSON은 문자열과 숫자를 문자열 원형으로 보존하고 null은 빈 값으로 바꾼다.
func (value *Scalar) UnmarshalJSON(data []byte) error {
	text, err := optionalScalarText(data)
	if err != nil {
		return err
	}
	*value = Scalar(text)
	return nil
}

// ServerTime은 MEXC 서버의 Unix millisecond 시각이다.
type ServerTime struct {
	Time int64           `json:"serverTime"`
	Raw  json.RawMessage `json:"-"`
}

// RateLimit은 exchangeInfo가 반환하는 거래소 요청 제한 설명이다.
type RateLimit struct {
	Type        string `json:"rateLimitType"`
	Interval    string `json:"interval"`
	IntervalNum int    `json:"intervalNum"`
	Limit       int    `json:"limit"`
}

// ExchangeInfo는 Spot 거래 규칙과 서버 메타데이터다.
type ExchangeInfo struct {
	Timezone        string
	ServerTime      int64
	RateLimits      []RateLimit
	ExchangeFilters []json.RawMessage
	Symbols         []Symbol
	Raw             json.RawMessage
}

// Symbol은 Spot 거래쌍의 상태와 주문 정밀도·권한 규칙이다.
type Symbol struct {
	Symbol                     string            `json:"symbol"`
	Status                     string            `json:"status"`
	BaseAsset                  string            `json:"baseAsset"`
	BaseAssetPrecision         int               `json:"baseAssetPrecision"`
	QuoteAsset                 string            `json:"quoteAsset"`
	QuotePrecision             int               `json:"quotePrecision"`
	QuoteAssetPrecision        int               `json:"quoteAssetPrecision"`
	BaseCommissionPrecision    int               `json:"baseCommissionPrecision"`
	QuoteCommissionPrecision   int               `json:"quoteCommissionPrecision"`
	OrderTypes                 []string          `json:"orderTypes"`
	QuoteOrderQuantityAllowed  bool              `json:"quoteOrderQtyMarketAllowed"`
	SpotTradingAllowed         bool              `json:"isSpotTradingAllowed"`
	MarginTradingAllowed       bool              `json:"isMarginTradingAllowed"`
	QuoteAmountPrecision       string            `json:"quoteAmountPrecision"`
	BaseSizePrecision          string            `json:"baseSizePrecision"`
	Permissions                []string          `json:"permissions"`
	Filters                    []json.RawMessage `json:"filters"`
	MaximumQuoteAmount         string            `json:"maxQuoteAmount"`
	MakerCommission            string            `json:"makerCommission"`
	TakerCommission            string            `json:"takerCommission"`
	MarketQuoteAmountPrecision string            `json:"quoteAmountPrecisionMarket"`
	MarketMaximumQuoteAmount   string            `json:"maxQuoteAmountMarket"`
	FullName                   string            `json:"fullName"`
	TradeSideType              Scalar            `json:"tradeSideType"`
	ContractAddress            string            `json:"contractAddress"`
	ConceptPlateIDs            []int64           `json:"conceptPlateIds"`
	FirstOpenTime              int64             `json:"firstOpenTime"`
	ConceptPlates              []string          `json:"conceptPlates"`
	ShortTermRisk              bool              `json:"st"`
	Raw                        json.RawMessage   `json:"-"`
}

// BookLevel은 가격별 합산 수량이다.
type BookLevel struct {
	Price    string
	Quantity string
}

// UnmarshalJSON은 위치 기반 호가 배열을 BookLevel로 변환한다.
func (level *BookLevel) UnmarshalJSON(data []byte) error {
	var fields []json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode MEXC book level: %w", err)
	}
	if len(fields) != 2 {
		return fmt.Errorf("MEXC book level has %d fields, want 2", len(fields))
	}
	price, err := decimalText(fields[0])
	if err != nil {
		return fmt.Errorf("decode MEXC book price: %w", err)
	}
	quantity, err := decimalText(fields[1])
	if err != nil {
		return fmt.Errorf("decode MEXC book quantity: %w", err)
	}
	level.Price, level.Quantity = price, quantity
	return nil
}

// OrderBook은 Spot 호가 snapshot과 마지막 update ID다.
type OrderBook struct {
	LastUpdateID int64           `json:"lastUpdateId"`
	Bids         []BookLevel     `json:"bids"`
	Asks         []BookLevel     `json:"asks"`
	Raw          json.RawMessage `json:"-"`
}

// PublicTrade는 최근 공개 Spot 체결 한 건이다.
type PublicTrade struct {
	ID            Scalar          `json:"id"`
	Price         string          `json:"price"`
	Quantity      string          `json:"qty"`
	QuoteQuantity string          `json:"quoteQty"`
	Time          int64           `json:"time"`
	BuyerMaker    bool            `json:"isBuyerMaker"`
	BestMatch     bool            `json:"isBestMatch"`
	Raw           json.RawMessage `json:"-"`
}

// AggregateTrade는 같은 주문·가격·시각으로 묶은 공개 Spot 체결이다.
type AggregateTrade struct {
	AggregateID  Scalar          `json:"a"`
	FirstTradeID Scalar          `json:"f"`
	LastTradeID  Scalar          `json:"l"`
	Price        string          `json:"p"`
	Quantity     string          `json:"q"`
	Time         int64           `json:"T"`
	BuyerMaker   bool            `json:"m"`
	BestMatch    bool            `json:"M"`
	Raw          json.RawMessage `json:"-"`
}

// Candle은 Spot OHLCV와 호가 통화 거래량 한 구간이다.
type Candle struct {
	OpenTime    int64
	Open        string
	High        string
	Low         string
	Close       string
	BaseVolume  string
	CloseTime   int64
	QuoteVolume string
	Raw         json.RawMessage
}

// UnmarshalJSON은 위치 기반 MEXC 캔들 배열을 Candle로 변환한다.
func (candle *Candle) UnmarshalJSON(data []byte) error {
	var fields []json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode MEXC candle: %w", err)
	}
	if len(fields) != 8 {
		return fmt.Errorf("MEXC candle has %d fields, want 8", len(fields))
	}
	if err := json.Unmarshal(fields[0], &candle.OpenTime); err != nil {
		return fmt.Errorf("decode MEXC candle open time: %w", err)
	}
	if err := json.Unmarshal(fields[6], &candle.CloseTime); err != nil {
		return fmt.Errorf("decode MEXC candle close time: %w", err)
	}
	targets := []*string{
		&candle.Open, &candle.High, &candle.Low, &candle.Close,
		&candle.BaseVolume, &candle.QuoteVolume,
	}
	indexes := []int{1, 2, 3, 4, 5, 7}
	for index, target := range targets {
		value, err := decimalText(fields[indexes[index]])
		if err != nil {
			return fmt.Errorf("decode MEXC candle field %d: %w", indexes[index], err)
		}
		*target = value
	}
	candle.Raw = cloneBytes(data)
	return nil
}

// AveragePrice는 최근 분 단위 평균가다.
type AveragePrice struct {
	Minutes int             `json:"mins"`
	Price   string          `json:"price"`
	Raw     json.RawMessage `json:"-"`
}

// Ticker24H는 단일 Spot 거래쌍의 24시간 가격·거래량 통계다.
type Ticker24H struct {
	Symbol             string          `json:"symbol"`
	PriceChange        string          `json:"priceChange"`
	PriceChangePercent string          `json:"priceChangePercent"`
	PreviousClosePrice string          `json:"prevClosePrice"`
	LastPrice          string          `json:"lastPrice"`
	LastQuantity       string          `json:"lastQty"`
	BidPrice           string          `json:"bidPrice"`
	BidQuantity        string          `json:"bidQty"`
	AskPrice           string          `json:"askPrice"`
	AskQuantity        string          `json:"askQty"`
	OpenPrice          string          `json:"openPrice"`
	HighPrice          string          `json:"highPrice"`
	LowPrice           string          `json:"lowPrice"`
	BaseVolume         string          `json:"volume"`
	QuoteVolume        string          `json:"quoteVolume"`
	OpenTime           int64           `json:"openTime"`
	CloseTime          int64           `json:"closeTime"`
	Count              Scalar          `json:"count"`
	Raw                json.RawMessage `json:"-"`
}

// PriceTicker는 단일 Spot 거래쌍의 최근 가격이다.
type PriceTicker struct {
	Symbol string          `json:"symbol"`
	Price  string          `json:"price"`
	Raw    json.RawMessage `json:"-"`
}

// BookTicker는 단일 Spot 거래쌍의 최우선 매수·매도 호가다.
type BookTicker struct {
	Symbol      string          `json:"symbol"`
	BidPrice    string          `json:"bidPrice"`
	BidQuantity string          `json:"bidQty"`
	AskPrice    string          `json:"askPrice"`
	AskQuantity string          `json:"askQty"`
	Raw         json.RawMessage `json:"-"`
}

// Side는 Spot 주문과 체결의 매수 또는 매도 방향이다.
type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

// OrderType은 Spot 주문의 가격·체결 정책이다.
type OrderType string

const (
	OrderTypeLimit             OrderType = "LIMIT"
	OrderTypeMarket            OrderType = "MARKET"
	OrderTypeLimitMaker        OrderType = "LIMIT_MAKER"
	OrderTypeImmediateOrCancel OrderType = "IMMEDIATE_OR_CANCEL"
	OrderTypeFillOrKill        OrderType = "FILL_OR_KILL"
)

// OrderStatus는 MEXC Spot 주문의 수명주기 상태다.
type OrderStatus string

const (
	OrderStatusNew               OrderStatus = "NEW"
	OrderStatusFilled            OrderStatus = "FILLED"
	OrderStatusPartiallyFilled   OrderStatus = "PARTIALLY_FILLED"
	OrderStatusCanceled          OrderStatus = "CANCELED"
	OrderStatusPartiallyCanceled OrderStatus = "PARTIALLY_CANCELED"
)

// Balance는 자산별 사용 가능·잠금 Spot 잔고다.
type Balance struct {
	Asset  string          `json:"asset"`
	Free   string          `json:"free"`
	Locked string          `json:"locked"`
	Raw    json.RawMessage `json:"-"`
}

// Account는 현재 Spot 계정 권한과 자산별 잔고다.
type Account struct {
	CanTrade    bool
	CanWithdraw bool
	CanDeposit  bool
	UpdateTime  Scalar
	AccountType string
	Balances    []Balance
	Permissions []string
	Raw         json.RawMessage
}

// OrderReference는 신규 Spot 주문 접수 결과다.
type OrderReference struct {
	Symbol        string          `json:"symbol"`
	OrderID       Scalar          `json:"orderId"`
	OrderListID   Scalar          `json:"orderListId"`
	ClientOrderID string          `json:"clientOrderId"`
	Price         string          `json:"price"`
	Quantity      string          `json:"origQty"`
	Type          OrderType       `json:"type"`
	Side          Side            `json:"side"`
	TransactTime  int64           `json:"transactTime"`
	Raw           json.RawMessage `json:"-"`
}

// Order는 Spot 주문 상태와 누적 체결 수량·금액이다.
type Order struct {
	Symbol                     string          `json:"symbol"`
	OriginalClientOrderID      Scalar          `json:"origClientOrderId"`
	OrderID                    Scalar          `json:"orderId"`
	OrderListID                Scalar          `json:"orderListId"`
	ClientOrderID              Scalar          `json:"clientOrderId"`
	Price                      string          `json:"price"`
	OriginalQuantity           string          `json:"origQty"`
	ExecutedQuantity           string          `json:"executedQty"`
	CumulativeQuoteQuantity    string          `json:"cummulativeQuoteQty"`
	Status                     OrderStatus     `json:"status"`
	TimeInForce                string          `json:"timeInForce"`
	Type                       OrderType       `json:"type"`
	Side                       Side            `json:"side"`
	StopPrice                  string          `json:"stopPrice"`
	IcebergQuantity            string          `json:"icebergQty"`
	CreatedAt                  int64           `json:"time"`
	UpdatedAt                  int64           `json:"updateTime"`
	Working                    bool            `json:"isWorking"`
	OriginalQuoteOrderQuantity string          `json:"origQuoteOrderQty"`
	Raw                        json.RawMessage `json:"-"`
}

// AccountTrade는 계정의 Spot 체결 한 건과 수수료 정보다.
type AccountTrade struct {
	Symbol          string          `json:"symbol"`
	ID              Scalar          `json:"id"`
	OrderID         Scalar          `json:"orderId"`
	OrderListID     Scalar          `json:"orderListId"`
	Price           string          `json:"price"`
	Quantity        string          `json:"qty"`
	QuoteQuantity   string          `json:"quoteQty"`
	Commission      string          `json:"commission"`
	CommissionAsset string          `json:"commissionAsset"`
	Time            int64           `json:"time"`
	Buyer           bool            `json:"isBuyer"`
	BuyerMaker      bool            `json:"isBuyerMaker"`
	Maker           bool            `json:"isMaker"`
	BestMatch       bool            `json:"isBestMatch"`
	SelfTrade       bool            `json:"isSelfTrade"`
	ClientOrderID   Scalar          `json:"clientOrderId"`
	Raw             json.RawMessage `json:"-"`
}

func decimalText(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", fmt.Errorf("decimal value is empty")
	}
	if trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return "", err
		}
		if value == "" {
			return "", fmt.Errorf("decimal value is empty")
		}
		return value, nil
	}
	if trimmed[0] == '{' || trimmed[0] == '[' || !json.Valid(trimmed) {
		return "", fmt.Errorf("decimal value is not scalar")
	}
	return string(trimmed), nil
}

func cloneBytes(value []byte) []byte { return append([]byte(nil), value...) }
