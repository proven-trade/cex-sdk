package kucoin

import (
	"encoding/json"
	"fmt"
)

// StreamChannel은 KuCoin Classic Spot WebSocket 채널이다.
type StreamChannel string

const (
	StreamChannelTicker      StreamChannel = "ticker"
	StreamChannelLevel2      StreamChannel = "level2"
	StreamChannelOrderBook5  StreamChannel = "orderbook5"
	StreamChannelOrderBook50 StreamChannel = "orderbook50"
	StreamChannelCandles     StreamChannel = "candles"
	StreamChannelTrade       StreamChannel = "trade"
	StreamChannelOrders      StreamChannel = "orders"
	StreamChannelBalance     StreamChannel = "balance"
)

// StreamSubscription은 채널, 거래쌍과 선택적인 캔들 구간을 정의한다.
type StreamSubscription struct {
	Channel  StreamChannel
	Symbol   string
	Interval CandleInterval
}

// StreamRequest는 연결 직후 복구할 구독 목록이다.
type StreamRequest struct {
	Subscriptions []StreamSubscription
}

// StreamMessage는 데이터, 연결 상태, 구독 응답, pong 또는 오류 한 건이다.
type StreamMessage struct {
	ID           string
	Type         string
	Topic        string
	Subject      string
	Channel      StreamChannel
	Private      bool
	UserID       string
	ErrorCode    string
	ErrorMessage string
	Data         json.RawMessage
	Raw          json.RawMessage
}

// Decode는 message 이벤트의 data를 지정한 타입으로 변환한다.
func (message StreamMessage) Decode(target any) error {
	if target == nil {
		return fmt.Errorf("KuCoin stream decode target is nil")
	}
	if message.Type != "message" || len(message.Data) == 0 {
		return fmt.Errorf("KuCoin stream message does not contain a data event")
	}
	if err := json.Unmarshal(message.Data, target); err != nil {
		return fmt.Errorf("decode KuCoin stream event: %w", err)
	}
	return nil
}

// StreamTicker는 실시간 최우선 호가와 최근 체결이다.
type StreamTicker struct {
	Sequence    string `json:"sequence"`
	Price       string `json:"price"`
	Size        string `json:"size"`
	BestAsk     string `json:"bestAsk"`
	BestAskSize string `json:"bestAskSize"`
	BestBid     string `json:"bestBid"`
	BestBidSize string `json:"bestBidSize"`
	Time        int64  `json:"time"`
}

// StreamChangeLevel은 증분 호가 한 단계의 가격, 수량과 sequence다.
type StreamChangeLevel struct {
	Price    string
	Size     string
	Sequence string
}

// UnmarshalJSON은 위치 기반 증분 호가 배열을 StreamChangeLevel로 변환한다.
func (level *StreamChangeLevel) UnmarshalJSON(data []byte) error {
	var fields []json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode KuCoin stream change level: %w", err)
	}
	if len(fields) != 3 {
		return fmt.Errorf("KuCoin stream change level has %d fields, want 3", len(fields))
	}
	targets := []*string{&level.Price, &level.Size, &level.Sequence}
	for index, target := range targets {
		value, err := decimalText(fields[index])
		if err != nil {
			return fmt.Errorf("decode KuCoin stream change field %d: %w", index, err)
		}
		*target = value
	}
	return nil
}

// StreamLevel2Changes는 sequence 범위가 있는 증분 호가 변경이다.
type StreamLevel2Changes struct {
	Changes struct {
		Asks []StreamChangeLevel `json:"asks"`
		Bids []StreamChangeLevel `json:"bids"`
	} `json:"changes"`
	SequenceStart int64  `json:"sequenceStart"`
	SequenceEnd   int64  `json:"sequenceEnd"`
	Symbol        string `json:"symbol"`
	Time          int64  `json:"time"`
}

// StreamOrderBook은 5단계 또는 50단계 실시간 호가 스냅샷이다.
type StreamOrderBook struct {
	Asks      []BookLevel `json:"asks"`
	Bids      []BookLevel `json:"bids"`
	Timestamp int64       `json:"timestamp"`
}

// StreamCandle은 변경된 최신 캔들과 matching engine 시각이다.
type StreamCandle struct {
	Symbol  string `json:"symbol"`
	Candles Candle `json:"candles"`
	Time    int64  `json:"time"`
}

// StreamTrade는 실시간 공개 체결 한 건이다.
type StreamTrade struct {
	MakerOrderID string `json:"makerOrderId"`
	Price        string `json:"price"`
	Sequence     string `json:"sequence"`
	Side         Side   `json:"side"`
	Size         string `json:"size"`
	Symbol       string `json:"symbol"`
	TakerOrderID string `json:"takerOrderId"`
	Time         int64  `json:"time"`
	TradeID      string `json:"tradeId"`
	Type         string `json:"type"`
}

// StreamOrder는 private 주문 생성·변경·체결·종료 이벤트다.
type StreamOrder struct {
	ClientOrderID string    `json:"clientOid"`
	OrderID       string    `json:"orderId"`
	OrderTime     int64     `json:"orderTime"`
	OrderType     OrderType `json:"orderType"`
	OriginSize    string    `json:"originSize"`
	OriginFunds   string    `json:"originFunds"`
	Side          Side      `json:"side"`
	Status        string    `json:"status"`
	Symbol        string    `json:"symbol"`
	TradeType     string    `json:"tradeType"`
	Timestamp     int64     `json:"ts"`
	EventType     string    `json:"type"`
	Price         string    `json:"price"`
	Size          string    `json:"size"`
	FilledSize    string    `json:"filledSize"`
	MatchPrice    string    `json:"matchPrice"`
	MatchSize     string    `json:"matchSize"`
	TradeID       string    `json:"tradeId"`
	Liquidity     string    `json:"liquidity"`
	RemainSize    string    `json:"remainSize"`
	CanceledSize  string    `json:"canceledSize"`
	FeeCurrency   string    `json:"feeCurrency"`
	Fee           string    `json:"fee"`
}

// StreamBalanceRelation은 잔고 변경과 관련된 거래쌍·주문·체결 식별자다.
type StreamBalanceRelation struct {
	Symbol  string `json:"symbol"`
	OrderID string `json:"orderId"`
	TradeID string `json:"tradeId"`
}

// StreamBalance는 private 자산 변경 후 잔고와 변경 원인이다.
type StreamBalance struct {
	AccountID       string                `json:"accountId"`
	Currency        string                `json:"currency"`
	Total           string                `json:"total"`
	Available       string                `json:"available"`
	Hold            string                `json:"hold"`
	AvailableChange string                `json:"availableChange"`
	HoldChange      string                `json:"holdChange"`
	RelationContext StreamBalanceRelation `json:"relationContext"`
	RelationEvent   string                `json:"relationEvent"`
	RelationEventID string                `json:"relationEventId"`
	Time            string                `json:"time"`
}
