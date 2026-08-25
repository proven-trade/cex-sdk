package htx

import (
	"encoding/json"
	"fmt"
)

// StreamChannel은 HTX 일반 시세 WebSocket 채널이다.
type StreamChannel string

const (
	StreamChannelTicker  StreamChannel = "ticker"
	StreamChannelDepth   StreamChannel = "depth"
	StreamChannelBBO     StreamChannel = "bbo"
	StreamChannelTrades  StreamChannel = "trades"
	StreamChannelCandles StreamChannel = "candles"
)

// StreamSubscription은 시세 채널과 거래쌍 및 채널별 선택 값을 정의한다.
type StreamSubscription struct {
	Channel        StreamChannel
	Symbol         string
	DepthType      DepthType
	CandleInterval CandleInterval
}

// StreamRequest는 연결 직후 복구할 공개 시세 구독 목록이다.
type StreamRequest struct {
	Subscriptions []StreamSubscription
}

// StreamError는 구독 또는 해지 요청을 거절한 서버 오류다.
type StreamError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// StreamMessage는 시세 데이터, 구독 응답 또는 heartbeat 한 건이다.
type StreamMessage struct {
	ID           string
	Status       string
	Topic        string
	Channel      StreamChannel
	Symbol       string
	Timestamp    int64
	Ping         *int64
	Subscribed   string
	Unsubscribed string
	Error        *StreamError
	Tick         json.RawMessage
	Raw          json.RawMessage
}

// Decode는 시세 이벤트의 tick을 지정한 타입으로 변환한다.
func (message StreamMessage) Decode(target any) error {
	if target == nil {
		return fmt.Errorf("HTX stream decode target is nil")
	}
	if message.Topic == "" || len(message.Tick) == 0 || message.Error != nil {
		return fmt.Errorf("HTX stream message does not contain a data event")
	}
	if err := json.Unmarshal(message.Tick, target); err != nil {
		return fmt.Errorf("decode HTX stream event: %w", err)
	}
	return nil
}

// StreamTicker는 실시간 현재가·최우선 호가와 24시간 통계다.
type StreamTicker struct {
	Open      Decimal `json:"open"`
	High      Decimal `json:"high"`
	Low       Decimal `json:"low"`
	Close     Decimal `json:"close"`
	Amount    Decimal `json:"amount"`
	Volume    Decimal `json:"vol"`
	Count     int64   `json:"count"`
	BidPrice  Decimal `json:"bid"`
	BidSize   Decimal `json:"bidSize"`
	AskPrice  Decimal `json:"ask"`
	AskSize   Decimal `json:"askSize"`
	LastPrice Decimal `json:"lastPrice"`
	LastSize  Decimal `json:"lastSize"`
}

// StreamDepth는 집계 단계별 호가 snapshot이다.
type StreamDepth struct {
	Timestamp int64       `json:"ts"`
	Version   Scalar      `json:"version"`
	Bids      []BookLevel `json:"bids"`
	Asks      []BookLevel `json:"asks"`
}

// StreamBBO는 실시간 최우선 매수·매도 호가다.
type StreamBBO struct {
	SequenceID Scalar  `json:"seqId"`
	AskPrice   Decimal `json:"ask"`
	AskSize    Decimal `json:"askSize"`
	BidPrice   Decimal `json:"bid"`
	BidSize    Decimal `json:"bidSize"`
	QuoteTime  int64   `json:"quoteTime"`
	Symbol     string  `json:"symbol"`
}

// StreamTradeBatch는 같은 tick에 포함된 공개 체결 묶음이다.
type StreamTradeBatch struct {
	ID        Scalar        `json:"id"`
	Timestamp int64         `json:"ts"`
	Trades    []StreamTrade `json:"data"`
}

// StreamTrade는 실시간 공개 체결 한 건이다.
type StreamTrade struct {
	ID        Scalar         `json:"id"`
	TradeID   Scalar         `json:"tradeId"`
	Amount    Decimal        `json:"amount"`
	Price     Decimal        `json:"price"`
	Timestamp int64          `json:"ts"`
	Direction TradeDirection `json:"direction"`
}

// StreamCandle은 변경된 최신 Spot OHLCV 캔들이다.
type StreamCandle struct {
	OpenTime    int64   `json:"id"`
	Open        Decimal `json:"open"`
	Close       Decimal `json:"close"`
	Low         Decimal `json:"low"`
	High        Decimal `json:"high"`
	BaseVolume  Decimal `json:"amount"`
	QuoteVolume Decimal `json:"vol"`
	TradeCount  int64   `json:"count"`
}
