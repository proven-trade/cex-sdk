package bithumb

import (
	"encoding/json"
	"fmt"
)

// StreamFormat은 빗썸 WebSocket 응답 필드 형식이다.
type StreamFormat string

const (
	StreamFormatDefault StreamFormat = "DEFAULT"
	StreamFormatSimple  StreamFormat = "SIMPLE"
)

// StreamDataType은 구독할 데이터 종류와 마켓 목록이다.
type StreamDataType struct {
	Type         string   `json:"type"`
	Codes        []string `json:"codes,omitempty"`
	Level        Decimal  `json:"level,omitempty"`
	OnlySnapshot bool     `json:"is_only_snapshot,omitempty"`
	OnlyRealtime bool     `json:"is_only_realtime,omitempty"`
}

// StreamRequest는 ticket, 데이터 종류와 응답 형식을 정의한다.
type StreamRequest struct {
	Ticket string
	Types  []StreamDataType
	Format StreamFormat
}

// StreamError는 WebSocket 요청 오류 응답이다.
type StreamError struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// StreamMessage는 public/private 이벤트, 상태 또는 오류 한 건이다.
type StreamMessage struct {
	Type       string
	Code       string
	StreamType string
	Timestamp  int64
	Status     string
	Error      *StreamError
	Payload    json.RawMessage
	Raw        json.RawMessage
}

// Decode는 이벤트 payload를 지정 타입으로 변환한다.
func (message StreamMessage) Decode(target any) error {
	if target == nil {
		return fmt.Errorf("Bithumb stream decode target is nil")
	}
	if message.Error != nil || message.Status != "" || len(message.Payload) == 0 {
		return fmt.Errorf("Bithumb stream message does not contain an event")
	}
	if err := json.Unmarshal(message.Payload, target); err != nil {
		return fmt.Errorf("decode Bithumb stream event: %w", err)
	}
	return nil
}

// StreamTicker는 실시간 ticker 이벤트다.
type StreamTicker struct {
	Type                 string  `json:"type"`
	Code                 string  `json:"code"`
	OpeningPrice         Decimal `json:"opening_price"`
	HighPrice            Decimal `json:"high_price"`
	LowPrice             Decimal `json:"low_price"`
	TradePrice           Decimal `json:"trade_price"`
	PreviousClosingPrice Decimal `json:"prev_closing_price"`
	Change               string  `json:"change"`
	ChangePrice          Decimal `json:"change_price"`
	SignedChangePrice    Decimal `json:"signed_change_price"`
	ChangeRate           Decimal `json:"change_rate"`
	SignedChangeRate     Decimal `json:"signed_change_rate"`
	TradeVolume          Decimal `json:"trade_volume"`
	AccumulatedVolume    Decimal `json:"acc_trade_volume"`
	AccumulatedVolume24H Decimal `json:"acc_trade_volume_24h"`
	AccumulatedPrice     Decimal `json:"acc_trade_price"`
	AccumulatedPrice24H  Decimal `json:"acc_trade_price_24h"`
	TradeDate            string  `json:"trade_date"`
	TradeTime            string  `json:"trade_time"`
	TradeTimestamp       int64   `json:"trade_timestamp"`
	AskBid               string  `json:"ask_bid"`
	AccumulatedAskVolume Decimal `json:"acc_ask_volume"`
	AccumulatedBidVolume Decimal `json:"acc_bid_volume"`
	Highest52WeekPrice   Decimal `json:"highest_52_week_price"`
	Highest52WeekDate    string  `json:"highest_52_week_date"`
	Lowest52WeekPrice    Decimal `json:"lowest_52_week_price"`
	Lowest52WeekDate     string  `json:"lowest_52_week_date"`
	MarketState          string  `json:"market_state"`
	TradingSuspended     bool    `json:"is_trading_suspended"`
	DelistingDate        string  `json:"delisting_date"`
	MarketWarning        string  `json:"market_warning"`
	Timestamp            int64   `json:"timestamp"`
	StreamType           string  `json:"stream_type"`
}

// StreamTrade는 실시간 공개 체결 이벤트다.
type StreamTrade struct {
	Type                 string  `json:"type"`
	Code                 string  `json:"code"`
	TradePrice           Decimal `json:"trade_price"`
	TradeVolume          Decimal `json:"trade_volume"`
	AskBid               string  `json:"ask_bid"`
	PreviousClosingPrice Decimal `json:"prev_closing_price"`
	Change               string  `json:"change"`
	ChangePrice          Decimal `json:"change_price"`
	TradeDate            string  `json:"trade_date"`
	TradeTime            string  `json:"trade_time"`
	TradeTimestamp       int64   `json:"trade_timestamp"`
	Timestamp            int64   `json:"timestamp"`
	SequentialID         int64   `json:"sequential_id"`
	StreamType           string  `json:"stream_type"`
}

// StreamOrderBook은 실시간 호가 snapshot 이벤트다.
type StreamOrderBook struct {
	Type         string          `json:"type"`
	Code         string          `json:"code"`
	TotalAskSize Decimal         `json:"total_ask_size"`
	TotalBidSize Decimal         `json:"total_bid_size"`
	OrderBook    []OrderBookUnit `json:"orderbook_units"`
	Timestamp    int64           `json:"timestamp"`
	Level        Decimal         `json:"level"`
	StreamType   string          `json:"stream_type"`
}

// StreamOrderSide는 private 주문의 매수 또는 매도 방향이다.
type StreamOrderSide string

const (
	StreamOrderSideBuy  StreamOrderSide = "buy"
	StreamOrderSideSell StreamOrderSide = "sell"
)

// StreamOrderState는 private 주문 이벤트 상태다.
type StreamOrderState string

const (
	StreamOrderStateWait   StreamOrderState = "wait"
	StreamOrderStateTrade  StreamOrderState = "trade"
	StreamOrderStateDone   StreamOrderState = "done"
	StreamOrderStateCancel StreamOrderState = "cancel"
)

// MyOrderEvent는 Private v2 내 주문 접수·체결·완료·취소 이벤트다.
type MyOrderEvent struct {
	Type              string           `json:"type"`
	StreamType        string           `json:"stream_type"`
	Code              string           `json:"code"`
	OrderID           string           `json:"order_id"`
	ClientOrderID     string           `json:"client_order_id"`
	Side              StreamOrderSide  `json:"side"`
	OrderType         OrderType        `json:"order_type"`
	State             StreamOrderState `json:"state"`
	TimeInForce       TimeInForce      `json:"time_in_force"`
	OrderPrice        Decimal          `json:"order_price"`
	OrderQuantity     Decimal          `json:"order_quantity"`
	OrderAmount       Decimal          `json:"order_amount"`
	OrderTimestamp    int64            `json:"order_timestamp"`
	TradeID           string           `json:"trade_id"`
	TradePrice        Decimal          `json:"trade_price"`
	TradeQuantity     Decimal          `json:"trade_quantity"`
	TradeAmount       Decimal          `json:"trade_amount"`
	TradeTimestamp    int64            `json:"trade_timestamp"`
	ExecutedQuantity  Decimal          `json:"executed_quantity"`
	RemainingQuantity Decimal          `json:"remaining_quantity"`
	ExecutedAmount    Decimal          `json:"executed_amount"`
	PaidFee           Decimal          `json:"paid_fee"`
	RemainingFee      Decimal          `json:"remaining_fee"`
	ReservedFee       Decimal          `json:"reserved_fee"`
	CancelType        string           `json:"cancel_type"`
	CancelingOrderID  string           `json:"canceling_order_id"`
	Timestamp         int64            `json:"timestamp"`
}

// StreamAsset은 private 자산 이벤트의 코인별 잔고다.
type StreamAsset struct {
	Currency string  `json:"currency"`
	Balance  Decimal `json:"balance"`
	Locked   Decimal `json:"locked"`
}

// MyAssetEvent는 Private v2 내 자산 변경 이벤트다.
type MyAssetEvent struct {
	Type           string        `json:"type"`
	StreamType     string        `json:"stream_type"`
	Assets         []StreamAsset `json:"assets"`
	AssetTimestamp int64         `json:"asset_timestamp"`
	Timestamp      int64         `json:"timestamp"`
}
