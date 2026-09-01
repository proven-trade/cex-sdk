// Package upbit은 업비트 Spot REST 어댑터를 제공한다.
package upbit

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Decimal은 JSON 숫자나 문자열을 정밀도 손실 없이 보존한다.
type Decimal string

// UnmarshalJSON은 JSON 숫자 또는 문자열을 원문 숫자 문자열로 변환한다.
func (decimal *Decimal) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		*decimal = ""
		return nil
	}
	if len(trimmed) == 0 {
		return fmt.Errorf("Upbit decimal value is empty")
	}
	if trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return err
		}
		*decimal = Decimal(value)
		return nil
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return err
	}
	*decimal = Decimal(number.String())
	return nil
}

// Market는 업비트 거래 가능 마켓 정보다.
type Market struct {
	Market        string          `json:"market"`
	KoreanName    string          `json:"korean_name"`
	EnglishName   string          `json:"english_name"`
	MarketWarning string          `json:"market_warning"`
	MarketEvent   json.RawMessage `json:"market_event"`
	Raw           json.RawMessage `json:"-"`
}

// Ticker는 마켓의 현재가와 누적 거래 정보다.
type Ticker struct {
	Market             string          `json:"market"`
	TradeDate          string          `json:"trade_date"`
	TradeTime          string          `json:"trade_time"`
	TradeDateKST       string          `json:"trade_date_kst"`
	TradeTimeKST       string          `json:"trade_time_kst"`
	TradeTimestamp     int64           `json:"trade_timestamp"`
	OpeningPrice       Decimal         `json:"opening_price"`
	HighPrice          Decimal         `json:"high_price"`
	LowPrice           Decimal         `json:"low_price"`
	TradePrice         Decimal         `json:"trade_price"`
	PreviousClosePrice Decimal         `json:"prev_closing_price"`
	Change             string          `json:"change"`
	ChangePrice        Decimal         `json:"change_price"`
	ChangeRate         Decimal         `json:"change_rate"`
	SignedChangePrice  Decimal         `json:"signed_change_price"`
	SignedChangeRate   Decimal         `json:"signed_change_rate"`
	TradeVolume        Decimal         `json:"trade_volume"`
	AccumulatedPrice   Decimal         `json:"acc_trade_price_24h"`
	AccumulatedVolume  Decimal         `json:"acc_trade_volume_24h"`
	Highest52WeekPrice Decimal         `json:"highest_52_week_price"`
	Highest52WeekDate  string          `json:"highest_52_week_date"`
	Lowest52WeekPrice  Decimal         `json:"lowest_52_week_price"`
	Lowest52WeekDate   string          `json:"lowest_52_week_date"`
	Timestamp          int64           `json:"timestamp"`
	Raw                json.RawMessage `json:"-"`
}

// OrderBookUnit은 한 가격 단계의 매도·매수 호가와 수량이다.
type OrderBookUnit struct {
	AskPrice Decimal `json:"ask_price"`
	BidPrice Decimal `json:"bid_price"`
	AskSize  Decimal `json:"ask_size"`
	BidSize  Decimal `json:"bid_size"`
}

// OrderBook은 마켓의 호가 스냅샷이다.
type OrderBook struct {
	Market       string          `json:"market"`
	Timestamp    int64           `json:"timestamp"`
	TotalAskSize Decimal         `json:"total_ask_size"`
	TotalBidSize Decimal         `json:"total_bid_size"`
	OrderBook    []OrderBookUnit `json:"orderbook_units"`
	Level        Decimal         `json:"level"`
	Raw          json.RawMessage `json:"-"`
}

// PublicTrade는 공개 최근 체결 한 건이다.
type PublicTrade struct {
	Market               string          `json:"market"`
	TradeDateUTC         string          `json:"trade_date_utc"`
	TradeTimeUTC         string          `json:"trade_time_utc"`
	Timestamp            int64           `json:"timestamp"`
	TradePrice           Decimal         `json:"trade_price"`
	TradeVolume          Decimal         `json:"trade_volume"`
	PreviousClosingPrice Decimal         `json:"prev_closing_price"`
	ChangePrice          Decimal         `json:"change_price"`
	AskBid               string          `json:"ask_bid"`
	SequentialID         int64           `json:"sequential_id"`
	Raw                  json.RawMessage `json:"-"`
}

// Candle은 분 단위 OHLCV 캔들 한 건이다.
type Candle struct {
	Market                 string          `json:"market"`
	CandleDateTimeUTC      string          `json:"candle_date_time_utc"`
	CandleDateTimeKST      string          `json:"candle_date_time_kst"`
	OpeningPrice           Decimal         `json:"opening_price"`
	HighPrice              Decimal         `json:"high_price"`
	LowPrice               Decimal         `json:"low_price"`
	TradePrice             Decimal         `json:"trade_price"`
	Timestamp              int64           `json:"timestamp"`
	AccumulatedTradePrice  Decimal         `json:"candle_acc_trade_price"`
	AccumulatedTradeVolume Decimal         `json:"candle_acc_trade_volume"`
	Unit                   int             `json:"unit"`
	Raw                    json.RawMessage `json:"-"`
}

// Balance는 코인별 주문 가능·잠금 잔고다.
type Balance struct {
	Currency                string `json:"currency"`
	Balance                 string `json:"balance"`
	Locked                  string `json:"locked"`
	AverageBuyPrice         string `json:"avg_buy_price"`
	AverageBuyPriceModified bool   `json:"avg_buy_price_modified"`
	UnitCurrency            string `json:"unit_currency"`
}

// Side는 주문의 매수 또는 매도 방향이다.
type Side string

const (
	SideAsk Side = "ask"
	SideBid Side = "bid"
)

// OrderType은 주문 가격 결정 방식이다.
type OrderType string

const (
	OrderTypeLimit  OrderType = "limit"
	OrderTypePrice  OrderType = "price"
	OrderTypeMarket OrderType = "market"
	OrderTypeBest   OrderType = "best"
)

// TimeInForce는 주문 체결 및 만료 정책이다.
type TimeInForce string

const (
	TimeInForceIOC      TimeInForce = "ioc"
	TimeInForceFOK      TimeInForce = "fok"
	TimeInForcePostOnly TimeInForce = "post_only"
)

// SMPType은 자전거래 방지 정책이다.
type SMPType string

const (
	SMPTypeCancelMaker SMPType = "cancel_maker"
	SMPTypeCancelTaker SMPType = "cancel_taker"
	SMPTypeReduce      SMPType = "reduce"
)

// OrderState는 주문 처리 상태다.
type OrderState string

const (
	OrderStateWait   OrderState = "wait"
	OrderStateWatch  OrderState = "watch"
	OrderStateDone   OrderState = "done"
	OrderStateCancel OrderState = "cancel"
)

// Trade는 주문에 포함된 개별 체결이다.
type Trade struct {
	Market    string `json:"market"`
	UUID      string `json:"uuid"`
	Price     string `json:"price"`
	Volume    string `json:"volume"`
	Funds     string `json:"funds"`
	Side      Side   `json:"side"`
	CreatedAt string `json:"created_at"`
}

// Order는 업비트 주문 상태와 체결 정보를 보존한다.
type Order struct {
	UUID            string          `json:"uuid"`
	Side            Side            `json:"side"`
	OrderType       OrderType       `json:"ord_type"`
	Price           string          `json:"price"`
	State           OrderState      `json:"state"`
	Market          string          `json:"market"`
	CreatedAt       string          `json:"created_at"`
	Volume          string          `json:"volume"`
	RemainingVolume string          `json:"remaining_volume"`
	ReservedFee     string          `json:"reserved_fee"`
	RemainingFee    string          `json:"remaining_fee"`
	PaidFee         string          `json:"paid_fee"`
	Locked          string          `json:"locked"`
	ExecutedVolume  string          `json:"executed_volume"`
	TradesCount     int             `json:"trades_count"`
	TimeInForce     TimeInForce     `json:"time_in_force"`
	Identifier      string          `json:"identifier"`
	SMPType         SMPType         `json:"smp_type"`
	PreventedVolume string          `json:"prevented_volume"`
	PreventedLocked string          `json:"prevented_locked"`
	Trades          []Trade         `json:"trades"`
	Raw             json.RawMessage `json:"-"`
}
