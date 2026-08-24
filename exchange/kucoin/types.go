// Package kucoin은 KuCoin Classic Spot REST 어댑터를 제공한다.
package kucoin

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Symbol은 Spot 거래쌍의 상태와 주문 단위 규칙이다.
type Symbol struct {
	Symbol           string          `json:"symbol"`
	Name             string          `json:"name"`
	BaseCurrency     string          `json:"baseCurrency"`
	QuoteCurrency    string          `json:"quoteCurrency"`
	FeeCurrency      string          `json:"feeCurrency"`
	Market           string          `json:"market"`
	BaseMinimumSize  string          `json:"baseMinSize"`
	QuoteMinimumSize string          `json:"quoteMinSize"`
	BaseMaximumSize  string          `json:"baseMaxSize"`
	QuoteMaximumSize string          `json:"quoteMaxSize"`
	BaseIncrement    string          `json:"baseIncrement"`
	QuoteIncrement   string          `json:"quoteIncrement"`
	PriceIncrement   string          `json:"priceIncrement"`
	PriceLimitRate   string          `json:"priceLimitRate"`
	MinimumFunds     string          `json:"minFunds"`
	MarginEnabled    bool            `json:"isMarginEnabled"`
	TradingEnabled   bool            `json:"enableTrading"`
	Raw              json.RawMessage `json:"-"`
}

// Ticker는 거래쌍의 최우선 호가와 최근 체결 정보다.
type Ticker struct {
	Sequence    string          `json:"sequence"`
	BestAsk     string          `json:"bestAsk"`
	BestAskSize string          `json:"bestAskSize"`
	BestBid     string          `json:"bestBid"`
	BestBidSize string          `json:"bestBidSize"`
	Price       string          `json:"price"`
	Size        string          `json:"size"`
	Time        int64           `json:"time"`
	Raw         json.RawMessage `json:"-"`
}

// BookLevel은 가격별 합산 수량이다.
type BookLevel struct {
	Price string
	Size  string
}

// UnmarshalJSON은 위치 기반 호가 배열을 BookLevel로 변환한다.
func (level *BookLevel) UnmarshalJSON(data []byte) error {
	var fields []json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode KuCoin book level: %w", err)
	}
	if len(fields) != 2 {
		return fmt.Errorf("KuCoin book level has %d fields, want 2", len(fields))
	}
	values := []*string{&level.Price, &level.Size}
	for index, target := range values {
		value, err := decimalText(fields[index])
		if err != nil {
			return fmt.Errorf("decode KuCoin book level field %d: %w", index, err)
		}
		*target = value
	}
	return nil
}

// OrderBook은 지정한 깊이의 합산 호가 스냅샷이다.
type OrderBook struct {
	Sequence string          `json:"sequence"`
	Time     int64           `json:"time"`
	Bids     []BookLevel     `json:"bids"`
	Asks     []BookLevel     `json:"asks"`
	Raw      json.RawMessage `json:"-"`
}

// PublicTrade는 공개 최근 체결 한 건이다.
type PublicTrade struct {
	Sequence string          `json:"sequence"`
	Price    string          `json:"price"`
	Size     string          `json:"size"`
	Side     Side            `json:"side"`
	Time     int64           `json:"time"`
	Raw      json.RawMessage `json:"-"`
}

// Candle은 한 구간의 OHLCV와 거래대금이다.
type Candle struct {
	Timestamp string
	Open      string
	Close     string
	High      string
	Low       string
	Volume    string
	Turnover  string
	Raw       json.RawMessage
}

// UnmarshalJSON은 KuCoin Classic 위치 기반 캔들 배열을 Candle로 변환한다.
func (candle *Candle) UnmarshalJSON(data []byte) error {
	var fields []json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode KuCoin candle: %w", err)
	}
	if len(fields) < 7 {
		return fmt.Errorf("KuCoin candle has %d fields, want at least 7", len(fields))
	}
	targets := []*string{
		&candle.Timestamp, &candle.Open, &candle.Close, &candle.High,
		&candle.Low, &candle.Volume, &candle.Turnover,
	}
	for index, target := range targets {
		value, err := decimalText(fields[index])
		if err != nil {
			return fmt.Errorf("decode KuCoin candle field %d: %w", index, err)
		}
		*target = value
	}
	candle.Raw = cloneBytes(data)
	return nil
}

// Account는 통화와 계정 유형별 총액·사용 가능·잠금 잔고다.
type Account struct {
	ID        string          `json:"id"`
	Currency  string          `json:"currency"`
	Type      AccountType     `json:"type"`
	Balance   string          `json:"balance"`
	Available string          `json:"available"`
	Holds     string          `json:"holds"`
	Raw       json.RawMessage `json:"-"`
}

// AccountType은 KuCoin 자금 계정 분류다.
type AccountType string

const (
	AccountTypeMain  AccountType = "main"
	AccountTypeTrade AccountType = "trade"
)

// Side는 주문과 체결의 매수 또는 매도 방향이다.
type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// OrderType은 주문의 가격 결정 방식이다.
type OrderType string

const (
	OrderTypeLimit  OrderType = "limit"
	OrderTypeMarket OrderType = "market"
)

// TimeInForce는 주문 체결 및 만료 정책이다.
type TimeInForce string

const (
	TimeInForceGTC TimeInForce = "GTC"
	TimeInForceGTT TimeInForce = "GTT"
	TimeInForceIOC TimeInForce = "IOC"
	TimeInForceFOK TimeInForce = "FOK"
)

// Order는 Classic Spot 주문 상태와 누적 체결 정보다.
type Order struct {
	ID            string          `json:"id"`
	Symbol        string          `json:"symbol"`
	OperationType string          `json:"opType"`
	Type          OrderType       `json:"type"`
	Side          Side            `json:"side"`
	Price         string          `json:"price"`
	Size          string          `json:"size"`
	Funds         string          `json:"funds"`
	DealFunds     string          `json:"dealFunds"`
	DealSize      string          `json:"dealSize"`
	Fee           string          `json:"fee"`
	FeeCurrency   string          `json:"feeCurrency"`
	TimeInForce   TimeInForce     `json:"timeInForce"`
	PostOnly      bool            `json:"postOnly"`
	Hidden        bool            `json:"hidden"`
	Iceberg       bool            `json:"iceberg"`
	VisibleSize   string          `json:"visibleSize"`
	CancelAfter   int64           `json:"cancelAfter"`
	Channel       string          `json:"channel"`
	ClientOrderID string          `json:"clientOid"`
	Remark        string          `json:"remark"`
	Tags          string          `json:"tags"`
	Active        bool            `json:"isActive"`
	CancelExists  bool            `json:"cancelExist"`
	CreatedAt     int64           `json:"createdAt"`
	LastUpdatedAt int64           `json:"lastUpdatedAt"`
	InOrderBook   bool            `json:"inOrderBook"`
	TradeType     string          `json:"tradeType"`
	Raw           json.RawMessage `json:"-"`
}

// OrderReference는 주문 생성 또는 취소 접수 결과다.
type OrderReference struct {
	OrderID       string          `json:"orderId"`
	ClientOrderID string          `json:"clientOid"`
	Raw           json.RawMessage `json:"-"`
}

// OrderPage는 페이지 번호 기반 미체결 주문 목록이다.
type OrderPage struct {
	CurrentPage int             `json:"currentPage"`
	PageSize    int             `json:"pageSize"`
	TotalNumber int             `json:"totalNum"`
	TotalPages  int             `json:"totalPage"`
	Orders      []Order         `json:"-"`
	Raw         json.RawMessage `json:"-"`
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
		return value, nil
	}
	return string(trimmed), nil
}

func cloneBytes(value []byte) []byte {
	return append([]byte(nil), value...)
}
