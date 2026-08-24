// Package coinone은 코인원 Spot REST 어댑터를 제공한다.
package coinone

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
		return fmt.Errorf("Coinone decimal value is empty")
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

// Market는 기준 통화별 Spot 마켓의 주문 규칙과 거래 상태다.
type Market struct {
	QuoteCurrency    string          `json:"quote_currency"`
	TargetCurrency   string          `json:"target_currency"`
	PriceUnit        Decimal         `json:"price_unit"`
	QuantityUnit     Decimal         `json:"qty_unit"`
	MaximumAmount    Decimal         `json:"max_order_amount"`
	MaximumPrice     Decimal         `json:"max_price"`
	MaximumQuantity  Decimal         `json:"max_qty"`
	MinimumAmount    Decimal         `json:"min_order_amount"`
	MinimumPrice     Decimal         `json:"min_price"`
	MinimumQuantity  Decimal         `json:"min_qty"`
	OrderBookUnits   []Decimal       `json:"order_book_units"`
	MaintenanceState int             `json:"maintenance_status"`
	TradeState       int             `json:"trade_status"`
	OrderTypes       []string        `json:"order_types"`
	Raw              json.RawMessage `json:"-"`
}

// OrderBookLevel은 가격별 합산 호가 수량이다.
type OrderBookLevel struct {
	Price    Decimal `json:"price"`
	Quantity Decimal `json:"qty"`
}

// OrderBook은 지정 마켓의 호가 스냅샷이다.
type OrderBook struct {
	Timestamp      int64            `json:"timestamp"`
	ID             string           `json:"id"`
	QuoteCurrency  string           `json:"quote_currency"`
	TargetCurrency string           `json:"target_currency"`
	OrderBookUnit  Decimal          `json:"order_book_unit"`
	Bids           []OrderBookLevel `json:"bids"`
	Asks           []OrderBookLevel `json:"asks"`
	Raw            json.RawMessage  `json:"-"`
}

// PublicTrade는 공개 최근 체결 한 건이다.
type PublicTrade struct {
	ID            string          `json:"id"`
	Timestamp     int64           `json:"timestamp"`
	Price         Decimal         `json:"price"`
	Quantity      Decimal         `json:"qty"`
	IsSellerMaker bool            `json:"is_seller_maker"`
	Raw           json.RawMessage `json:"-"`
}

// Ticker는 마켓의 현재가와 누적 거래 정보다.
type Ticker struct {
	QuoteCurrency         string           `json:"quote_currency"`
	TargetCurrency        string           `json:"target_currency"`
	Timestamp             int64            `json:"timestamp"`
	High                  Decimal          `json:"high"`
	Low                   Decimal          `json:"low"`
	First                 Decimal          `json:"first"`
	Last                  Decimal          `json:"last"`
	QuoteVolume           Decimal          `json:"quote_volume"`
	TargetVolume          Decimal          `json:"target_volume"`
	BestAsks              []OrderBookLevel `json:"best_asks"`
	BestBids              []OrderBookLevel `json:"best_bids"`
	ID                    string           `json:"id"`
	YesterdayHigh         Decimal          `json:"yesterday_high"`
	YesterdayLow          Decimal          `json:"yesterday_low"`
	YesterdayFirst        Decimal          `json:"yesterday_first"`
	YesterdayLast         Decimal          `json:"yesterday_last"`
	YesterdayQuoteVolume  Decimal          `json:"yesterday_quote_volume"`
	YesterdayTargetVolume Decimal          `json:"yesterday_target_volume"`
	Raw                   json.RawMessage  `json:"-"`
}

// Candle은 한 구간의 OHLCV 정보다.
type Candle struct {
	Timestamp    int64           `json:"timestamp"`
	Open         Decimal         `json:"open"`
	High         Decimal         `json:"high"`
	Low          Decimal         `json:"low"`
	Close        Decimal         `json:"close"`
	TargetVolume Decimal         `json:"target_volume"`
	QuoteVolume  Decimal         `json:"quote_volume"`
	Raw          json.RawMessage `json:"-"`
}

// CandlePage는 캔들 목록과 마지막 페이지 여부를 보존한다.
type CandlePage struct {
	IsLast bool
	Chart  []Candle
	Raw    json.RawMessage
}

// Balance는 통화별 사용 가능·주문 중 잔고와 평균 매수가다.
type Balance struct {
	Available    Decimal         `json:"available"`
	Limit        Decimal         `json:"limit"`
	AveragePrice Decimal         `json:"average_price"`
	Currency     string          `json:"currency"`
	Raw          json.RawMessage `json:"-"`
}

// Side는 주문의 매수 또는 매도 방향이다.
type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

// OrderType은 주문 가격 결정 방식이다.
type OrderType string

const (
	OrderTypeLimit     OrderType = "LIMIT"
	OrderTypeMarket    OrderType = "MARKET"
	OrderTypeStopLimit OrderType = "STOP_LIMIT"
)

// OrderReference는 주문 생성 접수 결과다.
type OrderReference struct {
	OrderID string
	Raw     json.RawMessage
}

// OrderDetail은 주문 한 건의 현재 상태와 누적 체결 정보다.
type OrderDetail struct {
	OrderID              string          `json:"order_id"`
	UserOrderID          string          `json:"user_order_id"`
	Type                 OrderType       `json:"type"`
	QuoteCurrency        string          `json:"quote_currency"`
	TargetCurrency       string          `json:"target_currency"`
	Status               string          `json:"status"`
	Side                 Side            `json:"side"`
	Fee                  Decimal         `json:"fee"`
	FeeRate              Decimal         `json:"fee_rate"`
	AverageExecutedPrice Decimal         `json:"average_executed_price"`
	UpdatedAt            int64           `json:"updated_at"`
	OrderedAt            int64           `json:"ordered_at"`
	Price                Decimal         `json:"price"`
	OriginalQuantity     Decimal         `json:"original_qty"`
	ExecutedQuantity     Decimal         `json:"executed_qty"`
	CanceledQuantity     Decimal         `json:"canceled_qty"`
	RemainingQuantity    Decimal         `json:"remain_qty"`
	LimitPrice           Decimal         `json:"limit_price"`
	TradedAmount         Decimal         `json:"traded_amount"`
	OriginalAmount       Decimal         `json:"original_amount"`
	CanceledAmount       Decimal         `json:"canceled_amount"`
	IsTriggered          *bool           `json:"is_triggered"`
	TriggerPrice         Decimal         `json:"trigger_price"`
	Raw                  json.RawMessage `json:"-"`
}

// CancelResult는 취소된 주문의 수량과 수수료 정보다.
type CancelResult struct {
	OrderID           string          `json:"order_id"`
	Price             Decimal         `json:"price"`
	Quantity          Decimal         `json:"qty"`
	RemainingQuantity Decimal         `json:"remain_qty"`
	Side              Side            `json:"side"`
	OriginalQuantity  Decimal         `json:"original_qty"`
	TradedQuantity    Decimal         `json:"traded_qty"`
	CanceledQuantity  Decimal         `json:"canceled_qty"`
	Fee               Decimal         `json:"fee"`
	FeeRate           Decimal         `json:"fee_rate"`
	AveragePrice      Decimal         `json:"avg_price"`
	CanceledAt        int64           `json:"canceled_at"`
	OrderedAt         int64           `json:"ordered_at"`
	Raw               json.RawMessage `json:"-"`
}

// ActiveOrder는 아직 종료되지 않은 주문 한 건이다.
type ActiveOrder struct {
	OrderID              string          `json:"order_id"`
	UserOrderID          string          `json:"user_order_id"`
	Type                 OrderType       `json:"type"`
	Side                 Side            `json:"side"`
	QuoteCurrency        string          `json:"quote_currency"`
	TargetCurrency       string          `json:"target_currency"`
	Price                Decimal         `json:"price"`
	OriginalQuantity     Decimal         `json:"original_qty"`
	RemainingQuantity    Decimal         `json:"remain_qty"`
	ExecutedQuantity     Decimal         `json:"executed_qty"`
	CanceledQuantity     Decimal         `json:"canceled_qty"`
	Fee                  Decimal         `json:"fee"`
	FeeRate              Decimal         `json:"fee_rate"`
	AverageExecutedPrice Decimal         `json:"average_executed_price"`
	OrderedAt            int64           `json:"ordered_at"`
	IsTriggered          *bool           `json:"is_triggered"`
	TriggerPrice         Decimal         `json:"trigger_price"`
	TriggeredAt          *int64          `json:"triggered_at"`
	Raw                  json.RawMessage `json:"-"`
}

// CompletedTrade는 종료 주문 목록에 포함된 개별 체결이다.
type CompletedTrade struct {
	TradeID        string          `json:"trade_id"`
	OrderID        string          `json:"order_id"`
	QuoteCurrency  string          `json:"quote_currency"`
	TargetCurrency string          `json:"target_currency"`
	OrderType      OrderType       `json:"order_type"`
	IsAsk          bool            `json:"is_ask"`
	IsMaker        bool            `json:"is_maker"`
	Price          Decimal         `json:"price"`
	Quantity       Decimal         `json:"qty"`
	Timestamp      int64           `json:"timestamp"`
	FeeRate        Decimal         `json:"fee_rate"`
	Fee            Decimal         `json:"fee"`
	FeeCurrency    string          `json:"fee_currency"`
	Raw            json.RawMessage `json:"-"`
}
