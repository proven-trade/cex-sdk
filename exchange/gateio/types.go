// Package gateio는 Gate.io API v4 Spot 어댑터를 제공한다.
package gateio

import (
	"encoding/json"
	"fmt"
)

// CurrencyPair는 Spot 거래쌍의 통화와 주문 정밀도·수량 규칙이다.
type CurrencyPair struct {
	ID                      string          `json:"id"`
	Base                    string          `json:"base"`
	BaseName                string          `json:"base_name"`
	Quote                   string          `json:"quote"`
	QuoteName               string          `json:"quote_name"`
	MinimumBaseAmount       *string         `json:"min_base_amount"`
	MinimumQuoteAmount      *string         `json:"min_quote_amount"`
	MaximumBaseAmount       *string         `json:"max_base_amount"`
	MaximumQuoteAmount      *string         `json:"max_quote_amount"`
	AmountPrecision         int             `json:"amount_precision"`
	PricePrecision          int             `json:"precision"`
	TradeStatus             string          `json:"trade_status"`
	SellStart               int64           `json:"sell_start"`
	BuyStart                int64           `json:"buy_start"`
	DelistingTime           int64           `json:"delisting_time"`
	Type                    string          `json:"type"`
	ShortTermRisk           bool            `json:"st_tag"`
	UpRate                  string          `json:"up_rate"`
	DownRate                string          `json:"down_rate"`
	Slippage                string          `json:"slippage"`
	MarketOrderMaximumBase  *string         `json:"market_order_max_stock"`
	MarketOrderMaximumQuote *string         `json:"market_order_max_money"`
	Raw                     json.RawMessage `json:"-"`
}

// Ticker는 Spot 거래쌍의 최근가·최우선 호가와 24시간 통계다.
type Ticker struct {
	CurrencyPair   string          `json:"currency_pair"`
	Last           string          `json:"last"`
	LowestAsk      string          `json:"lowest_ask"`
	LowestAskSize  string          `json:"lowest_size"`
	HighestBid     string          `json:"highest_bid"`
	HighestBidSize string          `json:"highest_size"`
	ChangePercent  string          `json:"change_percentage"`
	BaseVolume     string          `json:"base_volume"`
	QuoteVolume    string          `json:"quote_volume"`
	High24Hours    string          `json:"high_24h"`
	Low24Hours     string          `json:"low_24h"`
	Raw            json.RawMessage `json:"-"`
}

// BookLevel은 Spot 호가 한 단계의 가격과 수량이다.
type BookLevel struct {
	Price  string
	Amount string
}

// UnmarshalJSON은 위치 기반 Spot 호가 배열을 BookLevel로 변환한다.
func (level *BookLevel) UnmarshalJSON(data []byte) error {
	var fields []string
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode Gate.io book level: %w", err)
	}
	if len(fields) != 2 {
		return fmt.Errorf("Gate.io book level has %d fields, want 2", len(fields))
	}
	level.Price, level.Amount = fields[0], fields[1]
	return nil
}

// OrderBook은 Spot 호가와 snapshot 식별자·시각이다.
type OrderBook struct {
	ID        int64           `json:"id"`
	Current   int64           `json:"current"`
	UpdatedAt int64           `json:"update"`
	Asks      []BookLevel     `json:"asks"`
	Bids      []BookLevel     `json:"bids"`
	Raw       json.RawMessage `json:"-"`
}

// Side는 주문과 체결의 매수 또는 매도 방향이다.
type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// Trade는 공개 또는 계정 Spot 체결 한 건이다.
type Trade struct {
	ID             string          `json:"id"`
	CreatedAt      string          `json:"create_time"`
	CreatedAtMilli string          `json:"create_time_ms"`
	CurrencyPair   string          `json:"currency_pair"`
	OrderID        string          `json:"order_id"`
	Side           Side            `json:"side"`
	Role           string          `json:"role"`
	Amount         string          `json:"amount"`
	Price          string          `json:"price"`
	Fee            string          `json:"fee"`
	FeeCurrency    string          `json:"fee_currency"`
	SequenceID     string          `json:"sequence_id"`
	ClientOrderID  string          `json:"text"`
	Deal           string          `json:"deal"`
	Raw            json.RawMessage `json:"-"`
}

// Candle은 Spot OHLCV와 마감 여부 한 구간이다.
type Candle struct {
	Timestamp   int64
	QuoteVolume string
	Close       string
	High        string
	Low         string
	Open        string
	BaseVolume  string
	Closed      bool
	Raw         json.RawMessage
}

// UnmarshalJSON은 위치 기반 Spot 캔들 배열을 Candle로 변환한다.
func (candle *Candle) UnmarshalJSON(data []byte) error {
	var fields []string
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode Gate.io candle: %w", err)
	}
	if len(fields) != 7 && len(fields) != 8 {
		return fmt.Errorf("Gate.io candle has %d fields, want 7 or 8", len(fields))
	}
	var closed string
	if _, err := fmt.Sscan(fields[0], &candle.Timestamp); err != nil {
		return fmt.Errorf("decode Gate.io candle timestamp: %w", err)
	}
	candle.QuoteVolume, candle.Close, candle.High = fields[1], fields[2], fields[3]
	candle.Low, candle.Open = fields[4], fields[5]
	if len(fields) == 8 {
		candle.BaseVolume, closed = fields[6], fields[7]
	} else {
		candle.BaseVolume, closed = "", fields[6]
	}
	if closed != "true" && closed != "false" {
		return fmt.Errorf("invalid Gate.io candle closed value %q", closed)
	}
	candle.Closed = closed == "true"
	candle.Raw = cloneBytes(data)
	return nil
}

// Account는 통화별 Spot 사용 가능·잠금 잔고다.
type Account struct {
	Currency  string          `json:"currency"`
	Available string          `json:"available"`
	Locked    string          `json:"locked"`
	UpdateID  int64           `json:"update_id"`
	Raw       json.RawMessage `json:"-"`
}

// OrderType은 Spot 주문의 가격 결정 방식이다.
type OrderType string

const (
	OrderTypeLimit  OrderType = "limit"
	OrderTypeMarket OrderType = "market"
)

// TimeInForce는 Spot 주문의 체결 정책이다.
type TimeInForce string

const (
	TimeInForceGTC TimeInForce = "gtc"
	TimeInForceIOC TimeInForce = "ioc"
	TimeInForcePOC TimeInForce = "poc"
	TimeInForceFOK TimeInForce = "fok"
)

// Order는 Spot 주문 상태와 누적 체결 정보다.
type Order struct {
	ID               string          `json:"id"`
	ClientOrderID    string          `json:"text"`
	CreatedAt        string          `json:"create_time"`
	UpdatedAt        string          `json:"update_time"`
	CreatedAtMilli   int64           `json:"create_time_ms"`
	UpdatedAtMilli   int64           `json:"update_time_ms"`
	Status           string          `json:"status"`
	CurrencyPair     string          `json:"currency_pair"`
	Type             OrderType       `json:"type"`
	Account          string          `json:"account"`
	Side             Side            `json:"side"`
	Amount           string          `json:"amount"`
	Price            string          `json:"price"`
	TimeInForce      TimeInForce     `json:"time_in_force"`
	Left             string          `json:"left"`
	FilledAmount     string          `json:"filled_amount"`
	FilledTotal      string          `json:"filled_total"`
	AverageDealPrice string          `json:"avg_deal_price"`
	Fee              string          `json:"fee"`
	FeeCurrency      string          `json:"fee_currency"`
	FinishAs         string          `json:"finish_as"`
	Raw              json.RawMessage `json:"-"`
}

// OpenOrderGroup은 거래쌍별 미체결 주문 페이지다.
type OpenOrderGroup struct {
	CurrencyPair string          `json:"currency_pair"`
	Total        int             `json:"total"`
	Orders       []Order         `json:"orders"`
	Raw          json.RawMessage `json:"-"`
}

func cloneBytes(value []byte) []byte {
	return append([]byte(nil), value...)
}
