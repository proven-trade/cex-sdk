package gateio

import (
	"encoding/json"
	"fmt"
)

// StreamChannel은 Gate.io API v4 Spot WebSocket 채널이다.
type StreamChannel string

const (
	StreamChannelTicker          StreamChannel = "ticker"
	StreamChannelTrades          StreamChannel = "trades"
	StreamChannelCandles         StreamChannel = "candles"
	StreamChannelBookTicker      StreamChannel = "book_ticker"
	StreamChannelOrderBookUpdate StreamChannel = "order_book_update"
	StreamChannelOrderBookV2     StreamChannel = "order_book_v2"
	StreamChannelOrders          StreamChannel = "orders"
	StreamChannelUserTrades      StreamChannel = "user_trades"
	StreamChannelBalances        StreamChannel = "balances"
)

// StreamUpdateInterval은 증분 호가 통지 주기다.
type StreamUpdateInterval string

const (
	StreamUpdate20Millis  StreamUpdateInterval = "20ms"
	StreamUpdate100Millis StreamUpdateInterval = "100ms"
)

// StreamOrderBookDepth는 V2 호가 snapshot과 증분 통지의 단계 수다.
type StreamOrderBookDepth int

const (
	StreamOrderBookDepth50  StreamOrderBookDepth = 50
	StreamOrderBookDepth400 StreamOrderBookDepth = 400
)

// StreamSubscription은 채널, 거래쌍과 채널별 선택 값을 정의한다.
type StreamSubscription struct {
	Channel        StreamChannel
	CurrencyPair   string
	CandleInterval CandleInterval
	UpdateInterval StreamUpdateInterval
	OrderBookDepth StreamOrderBookDepth
}

// StreamRequest는 연결 직후 복구할 구독 목록이다.
type StreamRequest struct {
	Subscriptions []StreamSubscription
}

// StreamError는 구독 요청을 거절한 서버 오류다.
type StreamError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// StreamMessage는 데이터, 구독 응답, pong 또는 시스템 알림 한 건이다.
type StreamMessage struct {
	ID          string
	Time        int64
	TimeMilli   int64
	ChannelName string
	Channel     StreamChannel
	Event       string
	Private     bool
	Error       *StreamError
	Result      json.RawMessage
	Raw         json.RawMessage
}

// Decode는 update 이벤트의 result를 지정한 타입으로 변환한다.
func (message StreamMessage) Decode(target any) error {
	if target == nil {
		return fmt.Errorf("Gate.io stream decode target is nil")
	}
	if message.Event != "update" || len(message.Result) == 0 {
		return fmt.Errorf("Gate.io stream message does not contain a data event")
	}
	if err := json.Unmarshal(message.Result, target); err != nil {
		return fmt.Errorf("decode Gate.io stream event: %w", err)
	}
	return nil
}

// StreamTicker는 실시간 Spot 현재가와 24시간 통계다.
type StreamTicker struct {
	CurrencyPair  string `json:"currency_pair"`
	Last          string `json:"last"`
	LowestAsk     string `json:"lowest_ask"`
	HighestBid    string `json:"highest_bid"`
	ChangePercent string `json:"change_percentage"`
	BaseVolume    string `json:"base_volume"`
	QuoteVolume   string `json:"quote_volume"`
	High24Hours   string `json:"high_24h"`
	Low24Hours    string `json:"low_24h"`
}

// StreamTrade는 실시간 공개 또는 계정 Spot 체결이다.
type StreamTrade struct {
	ID             int64  `json:"id"`
	MarketID       int64  `json:"id_market"`
	UserID         int64  `json:"user_id"`
	OrderID        string `json:"order_id"`
	CreatedAt      int64  `json:"create_time"`
	CreatedAtMilli string `json:"create_time_ms"`
	Side           Side   `json:"side"`
	CurrencyPair   string `json:"currency_pair"`
	Amount         string `json:"amount"`
	Price          string `json:"price"`
	Role           string `json:"role"`
	Fee            string `json:"fee"`
	FeeCurrency    string `json:"fee_currency"`
	ClientOrderID  string `json:"text"`
	Range          string `json:"range"`
}

// StreamCandle은 변경된 최신 Spot 캔들이다.
type StreamCandle struct {
	Timestamp   string `json:"t"`
	QuoteVolume string `json:"v"`
	Close       string `json:"c"`
	High        string `json:"h"`
	Low         string `json:"l"`
	Open        string `json:"o"`
	Name        string `json:"n"`
	BaseVolume  string `json:"a"`
	Closed      bool   `json:"w"`
}

// StreamBookTicker는 실시간 최우선 매수·매도 호가다.
type StreamBookTicker struct {
	Timestamp     int64  `json:"t"`
	UpdateID      int64  `json:"u"`
	CurrencyPair  string `json:"s"`
	BestBid       string `json:"b"`
	BestBidAmount string `json:"B"`
	BestAsk       string `json:"a"`
	BestAskAmount string `json:"A"`
}

// StreamOrderBookUpdate는 sequence 범위가 있는 절대 수량 기반 증분 호가다.
type StreamOrderBookUpdate struct {
	Timestamp     int64       `json:"t"`
	Full          bool        `json:"full"`
	Level         string      `json:"l"`
	EventType     string      `json:"e"`
	EventTime     int64       `json:"E"`
	CurrencyPair  string      `json:"s"`
	FirstUpdateID int64       `json:"U"`
	LastUpdateID  int64       `json:"u"`
	Bids          []BookLevel `json:"b"`
	Asks          []BookLevel `json:"a"`
}

// StreamOrderBookV2는 전체 snapshot 또는 연속 update ID의 절대 수량 변경이다.
type StreamOrderBookV2 struct {
	Timestamp     int64       `json:"t"`
	Full          bool        `json:"full"`
	StreamName    string      `json:"s"`
	FirstUpdateID int64       `json:"U"`
	LastUpdateID  int64       `json:"u"`
	Bids          []BookLevel `json:"b"`
	Asks          []BookLevel `json:"a"`
}

// StreamOrder는 private 주문 생성·체결·종료 이벤트다.
type StreamOrder struct {
	ID               string      `json:"id"`
	UserID           int64       `json:"user"`
	ClientOrderID    string      `json:"text"`
	CreatedAt        string      `json:"create_time"`
	CreatedAtMilli   string      `json:"create_time_ms"`
	UpdatedAt        string      `json:"update_time"`
	UpdatedAtMilli   string      `json:"update_time_ms"`
	Event            string      `json:"event"`
	Status           string      `json:"status"`
	CurrencyPair     string      `json:"currency_pair"`
	Type             OrderType   `json:"type"`
	Account          string      `json:"account"`
	Side             Side        `json:"side"`
	Amount           string      `json:"amount"`
	Price            string      `json:"price"`
	TimeInForce      TimeInForce `json:"time_in_force"`
	Left             string      `json:"left"`
	FilledAmount     string      `json:"filled_amount"`
	FilledTotal      string      `json:"filled_total"`
	AverageDealPrice string      `json:"avg_deal_price"`
	Fee              string      `json:"fee"`
	FeeCurrency      string      `json:"fee_currency"`
	FinishAs         string      `json:"finish_as"`
	AmendText        string      `json:"amend_text"`
	Slippage         string      `json:"slippage"`
}

// StreamBalance는 private Spot 자산 변경과 변경 후 잔고다.
type StreamBalance struct {
	Timestamp      string `json:"timestamp"`
	TimestampMilli string `json:"timestamp_ms"`
	UserID         string `json:"user"`
	Currency       string `json:"currency"`
	Change         string `json:"change"`
	Total          string `json:"total"`
	Available      string `json:"available"`
	Frozen         string `json:"freeze"`
	FrozenChange   string `json:"freeze_change"`
	ChangeType     string `json:"change_type"`
}
