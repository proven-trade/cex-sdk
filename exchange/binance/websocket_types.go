package binance

import (
	"encoding/json"
	"fmt"
)

// StreamTimeUnit은 Binance WebSocket 이벤트 timestamp 단위다.
type StreamTimeUnit string

const (
	StreamTimeMilliseconds StreamTimeUnit = ""
	StreamTimeMicroseconds StreamTimeUnit = "MICROSECOND"
)

// MarketStreamRequest는 한 연결에서 구독할 public stream 목록이다.
type MarketStreamRequest struct {
	Streams  []string
	TimeUnit StreamTimeUnit
}

// WebSocketError는 Binance WebSocket 제어 요청 오류다.
type WebSocketError struct {
	Code    int             `json:"code"`
	Message string          `json:"msg"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// WebSocketResponse는 구독 또는 WebSocket API 요청 응답이다.
type WebSocketResponse struct {
	ID     json.RawMessage
	Status int
	Result json.RawMessage
	Error  *WebSocketError
	Raw    json.RawMessage
}

// MarketStreamMessage는 public 이벤트 또는 구독 제어 응답 한 건이다.
type MarketStreamMessage struct {
	Stream    string
	EventType string
	EventTime int64
	Payload   json.RawMessage
	Response  *WebSocketResponse
	Raw       json.RawMessage
}

// Decode는 public 이벤트 payload를 지정 타입으로 변환한다.
func (message MarketStreamMessage) Decode(target any) error {
	if target == nil {
		return fmt.Errorf("Binance market stream decode target is nil")
	}
	if message.Response != nil || len(message.Payload) == 0 {
		return fmt.Errorf("Binance market stream message does not contain an event")
	}
	if err := json.Unmarshal(message.Payload, target); err != nil {
		return fmt.Errorf("decode Binance market stream event: %w", err)
	}
	return nil
}

// UserDataStreamMessage는 private 계정 이벤트 또는 제어 응답 한 건이다.
type UserDataStreamMessage struct {
	SubscriptionID int64
	EventType      string
	EventTime      int64
	Payload        json.RawMessage
	Response       *WebSocketResponse
	Raw            json.RawMessage
}

// Decode는 private 이벤트 payload를 지정 타입으로 변환한다.
func (message UserDataStreamMessage) Decode(target any) error {
	if target == nil {
		return fmt.Errorf("Binance user data stream decode target is nil")
	}
	if message.Response != nil || len(message.Payload) == 0 {
		return fmt.Errorf("Binance user data stream message does not contain an event")
	}
	if err := json.Unmarshal(message.Payload, target); err != nil {
		return fmt.Errorf("decode Binance user data stream event: %w", err)
	}
	return nil
}

// AggregateTradeEvent는 동일 taker 주문 단위로 합쳐진 공개 체결 이벤트다.
type AggregateTradeEvent struct {
	EventType      string `json:"e"`
	EventTime      int64  `json:"E"`
	Symbol         string `json:"s"`
	AggregateID    int64  `json:"a"`
	Price          string `json:"p"`
	Quantity       string `json:"q"`
	FirstTradeID   int64  `json:"f"`
	LastTradeID    int64  `json:"l"`
	TradeTime      int64  `json:"T"`
	BuyerIsMaker   bool   `json:"m"`
	BestPriceMatch bool   `json:"M"`
}

// TradeEvent는 개별 공개 체결 이벤트다.
type TradeEvent struct {
	EventType     string `json:"e"`
	EventTime     int64  `json:"E"`
	Symbol        string `json:"s"`
	TradeID       int64  `json:"t"`
	Price         string `json:"p"`
	Quantity      string `json:"q"`
	BuyerOrderID  int64  `json:"b"`
	SellerOrderID int64  `json:"a"`
	TradeTime     int64  `json:"T"`
	BuyerIsMaker  bool   `json:"m"`
}

// KlineEvent는 캔들 갱신 이벤트다.
type KlineEvent struct {
	EventType string      `json:"e"`
	EventTime int64       `json:"E"`
	Symbol    string      `json:"s"`
	Kline     StreamKline `json:"k"`
}

// StreamKline은 WebSocket 캔들의 현재 상태다.
type StreamKline struct {
	OpenTime            int64         `json:"t"`
	CloseTime           int64         `json:"T"`
	Symbol              string        `json:"s"`
	Interval            KlineInterval `json:"i"`
	FirstTradeID        int64         `json:"f"`
	LastTradeID         int64         `json:"L"`
	Open                string        `json:"o"`
	Close               string        `json:"c"`
	High                string        `json:"h"`
	Low                 string        `json:"l"`
	BaseVolume          string        `json:"v"`
	TradeCount          int           `json:"n"`
	Closed              bool          `json:"x"`
	QuoteVolume         string        `json:"q"`
	TakerBuyBaseVolume  string        `json:"V"`
	TakerBuyQuoteVolume string        `json:"Q"`
}

// TickerEvent는 24시간 rolling ticker 이벤트다.
type TickerEvent struct {
	EventType          string `json:"e"`
	EventTime          int64  `json:"E"`
	Symbol             string `json:"s"`
	PriceChange        string `json:"p"`
	PriceChangePercent string `json:"P"`
	WeightedAverage    string `json:"w"`
	LastPrice          string `json:"c"`
	LastQuantity       string `json:"Q"`
	BestBidPrice       string `json:"b"`
	BestBidQuantity    string `json:"B"`
	BestAskPrice       string `json:"a"`
	BestAskQuantity    string `json:"A"`
	OpenPrice          string `json:"o"`
	HighPrice          string `json:"h"`
	LowPrice           string `json:"l"`
	BaseVolume         string `json:"v"`
	QuoteVolume        string `json:"q"`
	OpenTime           int64  `json:"O"`
	CloseTime          int64  `json:"C"`
	FirstTradeID       int64  `json:"F"`
	LastTradeID        int64  `json:"L"`
	TradeCount         int    `json:"n"`
}

// BookTickerEvent는 최우선 매수·매도 호가 이벤트다.
type BookTickerEvent struct {
	UpdateID        int64  `json:"u"`
	Symbol          string `json:"s"`
	BestBidPrice    string `json:"b"`
	BestBidQuantity string `json:"B"`
	BestAskPrice    string `json:"a"`
	BestAskQuantity string `json:"A"`
}

// DepthEvent는 diff 또는 partial order book 이벤트다.
type DepthEvent struct {
	EventType     string     `json:"e"`
	EventTime     int64      `json:"E"`
	Symbol        string     `json:"s"`
	FirstUpdateID int64      `json:"U"`
	FinalUpdateID int64      `json:"u"`
	LastUpdateID  int64      `json:"lastUpdateId"`
	Bids          [][]string `json:"b,omitempty"`
	Asks          [][]string `json:"a,omitempty"`
	PartialBids   [][]string `json:"bids,omitempty"`
	PartialAsks   [][]string `json:"asks,omitempty"`
}

// AccountPositionEvent는 변경 가능성이 있는 Spot 잔고 이벤트다.
type AccountPositionEvent struct {
	EventType      string          `json:"e"`
	EventTime      int64           `json:"E"`
	LastUpdateTime int64           `json:"u"`
	Balances       []StreamBalance `json:"B"`
}

// StreamBalance는 private stream의 자산 잔고다.
type StreamBalance struct {
	Asset  string `json:"a"`
	Free   string `json:"f"`
	Locked string `json:"l"`
}

// BalanceUpdateEvent는 입출금 또는 계정 간 이동에 따른 잔고 변화다.
type BalanceUpdateEvent struct {
	EventType string `json:"e"`
	EventTime int64  `json:"E"`
	Asset     string `json:"a"`
	Delta     string `json:"d"`
	ClearTime int64  `json:"T"`
}

// ExecutionReportEvent는 Spot 주문 상태 및 체결 갱신 이벤트다.
type ExecutionReportEvent struct {
	EventType                string      `json:"e"`
	EventTime                int64       `json:"E"`
	Symbol                   string      `json:"s"`
	ClientOrderID            string      `json:"c"`
	Side                     Side        `json:"S"`
	OrderType                OrderType   `json:"o"`
	TimeInForce              TimeInForce `json:"f"`
	OriginalQuantity         string      `json:"q"`
	Price                    string      `json:"p"`
	StopPrice                string      `json:"P"`
	IcebergQuantity          string      `json:"F"`
	OrderListID              int64       `json:"g"`
	OriginalClientOrderID    string      `json:"C"`
	ExecutionType            string      `json:"x"`
	OrderStatus              OrderStatus `json:"X"`
	RejectReason             string      `json:"r"`
	OrderID                  int64       `json:"i"`
	LastExecutedQuantity     string      `json:"l"`
	CumulativeFilledQuantity string      `json:"z"`
	LastExecutedPrice        string      `json:"L"`
	Commission               string      `json:"n"`
	CommissionAsset          *string     `json:"N"`
	TransactionTime          int64       `json:"T"`
	TradeID                  int64       `json:"t"`
	ExecutionID              int64       `json:"I"`
	OnBook                   bool        `json:"w"`
	Maker                    bool        `json:"m"`
	OrderCreationTime        int64       `json:"O"`
	CumulativeQuoteQuantity  string      `json:"Z"`
	LastQuoteQuantity        string      `json:"Y"`
	QuoteOrderQuantity       string      `json:"Q"`
	WorkingTime              int64       `json:"W"`
	SelfTradePreventionMode  string      `json:"V"`
}

// ExternalLockUpdateEvent는 외부 시스템 담보로 인한 자산 잠금 변화다.
type ExternalLockUpdateEvent struct {
	EventType       string `json:"e"`
	EventTime       int64  `json:"E"`
	Asset           string `json:"a"`
	Delta           string `json:"d"`
	TransactionTime int64  `json:"T"`
}
