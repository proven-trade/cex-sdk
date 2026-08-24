package coinbase

import (
	"encoding/json"
	"fmt"
)

// StreamChannel은 Coinbase Advanced Trade WebSocket 채널 이름이다.
type StreamChannel string

const (
	StreamChannelHeartbeats   StreamChannel = "heartbeats"
	StreamChannelCandles      StreamChannel = "candles"
	StreamChannelMarketTrades StreamChannel = "market_trades"
	StreamChannelStatus       StreamChannel = "status"
	StreamChannelTicker       StreamChannel = "ticker"
	StreamChannelTickerBatch  StreamChannel = "ticker_batch"
	StreamChannelLevel2       StreamChannel = "level2"
	StreamChannelUser         StreamChannel = "user"
)

// StreamSubscription은 한 채널과 상품 목록의 구독 단위다.
type StreamSubscription struct {
	Channel    StreamChannel
	ProductIDs []string
}

// PublicStreamRequest는 market data 연결의 초기 구독 목록이다.
type PublicStreamRequest struct {
	Subscriptions []StreamSubscription
}

// UserStreamRequest는 user 채널이 수신할 Spot 상품 필터다.
// 상품이 비어 있으면 계정의 모든 상품 주문을 수신한다.
type UserStreamRequest struct {
	ProductIDs []string
}

// StreamMessage는 채널 이벤트 또는 오류 frame 한 건이다.
type StreamMessage struct {
	Type           string
	Channel        StreamChannel
	ClientID       string
	Timestamp      string
	SequenceNumber int64
	Message        string
	Events         json.RawMessage
	Raw            json.RawMessage
}

// DecodeEvents는 채널 events를 지정 타입으로 변환한다.
func (message StreamMessage) DecodeEvents(target any) error {
	if target == nil {
		return fmt.Errorf("Coinbase stream decode target is nil")
	}
	if len(message.Events) == 0 {
		return fmt.Errorf("Coinbase stream message does not contain events")
	}
	if err := json.Unmarshal(message.Events, target); err != nil {
		return fmt.Errorf("decode Coinbase stream events: %w", err)
	}
	return nil
}

// StreamHeartbeatEvent는 연결 유지와 누락 감지용 heartbeat다.
type StreamHeartbeatEvent struct {
	CurrentTime      string `json:"current_time"`
	HeartbeatCounter int64  `json:"heartbeat_counter"`
}

// StreamCandle은 5분 단위 실시간 OHLCV다.
type StreamCandle struct {
	Start     string `json:"start"`
	High      string `json:"high"`
	Low       string `json:"low"`
	Open      string `json:"open"`
	Close     string `json:"close"`
	Volume    string `json:"volume"`
	ProductID string `json:"product_id"`
}

// StreamCandleEvent는 candle snapshot 또는 update다.
type StreamCandleEvent struct {
	Type    string         `json:"type"`
	Candles []StreamCandle `json:"candles"`
}

// StreamMarketTradeEvent는 공개 체결 snapshot 또는 update다.
type StreamMarketTradeEvent struct {
	Type   string        `json:"type"`
	Trades []MarketTrade `json:"trades"`
}

// StreamTicker는 상품의 실시간 시세와 통계다.
type StreamTicker struct {
	Type              string `json:"type"`
	ProductID         string `json:"product_id"`
	Price             string `json:"price"`
	Volume24Hour      string `json:"volume_24_h"`
	Low24Hour         string `json:"low_24_h"`
	High24Hour        string `json:"high_24_h"`
	Low52Week         string `json:"low_52_w"`
	High52Week        string `json:"high_52_w"`
	PriceChange24Hour string `json:"price_percent_chg_24_h"`
	BestBid           string `json:"best_bid"`
	BestBidQuantity   string `json:"best_bid_quantity"`
	BestAsk           string `json:"best_ask"`
	BestAskQuantity   string `json:"best_ask_quantity"`
}

// StreamTickerEvent는 ticker 또는 ticker_batch event다.
type StreamTickerEvent struct {
	Type    string         `json:"type"`
	Tickers []StreamTicker `json:"tickers"`
}

// StreamLevel2Update는 가격 레벨의 새 전체 수량이다.
// NewQuantity가 0이면 해당 가격 레벨을 제거해야 한다.
type StreamLevel2Update struct {
	Side        string `json:"side"`
	EventTime   string `json:"event_time"`
	PriceLevel  string `json:"price_level"`
	NewQuantity string `json:"new_quantity"`
}

// StreamLevel2Event는 호가 snapshot 또는 순차 update다.
type StreamLevel2Event struct {
	Type      string               `json:"type"`
	ProductID string               `json:"product_id"`
	Updates   []StreamLevel2Update `json:"updates"`
}

// StreamStatusProduct는 status 채널의 Spot 상품 상태다.
type StreamStatusProduct struct {
	ProductType        ProductType `json:"product_type"`
	ID                 string      `json:"id"`
	BaseCurrency       string      `json:"base_currency"`
	QuoteCurrency      string      `json:"quote_currency"`
	BaseIncrement      string      `json:"base_increment"`
	QuoteIncrement     string      `json:"quote_increment"`
	DisplayName        string      `json:"display_name"`
	Status             string      `json:"status"`
	StatusMessage      string      `json:"status_message"`
	MinimumMarketFunds string      `json:"min_market_funds"`
}

// StreamStatusEvent는 상품 상태 snapshot이다.
type StreamStatusEvent struct {
	Type     string                `json:"type"`
	Products []StreamStatusProduct `json:"products"`
}

// StreamUserOrder는 user 채널의 Spot 주문 변경이다.
type StreamUserOrder struct {
	AveragePrice          string      `json:"avg_price"`
	CancelReason          string      `json:"cancel_reason"`
	ClientOrderID         string      `json:"client_order_id"`
	CompletionPercentage  string      `json:"completion_percentage"`
	CumulativeQuantity    string      `json:"cumulative_quantity"`
	FilledValue           string      `json:"filled_value"`
	LeavesQuantity        string      `json:"leaves_quantity"`
	LimitPrice            string      `json:"limit_price"`
	NumberOfFills         string      `json:"number_of_fills"`
	OrderID               string      `json:"order_id"`
	OrderSide             Side        `json:"order_side"`
	OrderType             string      `json:"order_type"`
	OutstandingHoldAmount string      `json:"outstanding_hold_amount"`
	PostOnly              string      `json:"post_only"`
	ProductID             string      `json:"product_id"`
	ProductType           ProductType `json:"product_type"`
	RejectReason          string      `json:"reject_reason"`
	Status                string      `json:"status"`
	StopPrice             string      `json:"stop_price"`
	TimeInForce           string      `json:"time_in_force"`
	TotalFees             string      `json:"total_fees"`
	TotalValueAfterFees   string      `json:"total_value_after_fees"`
	TriggerStatus         string      `json:"trigger_status"`
	CreationTime          string      `json:"creation_time"`
	EndTime               string      `json:"end_time"`
	StartTime             string      `json:"start_time"`
}

// StreamUserEvent는 user 주문 snapshot 또는 update다.
type StreamUserEvent struct {
	Type   string            `json:"type"`
	Orders []StreamUserOrder `json:"orders"`
}

// StreamSubscriptionEvent는 서버가 확인한 현재 구독 목록이다.
type StreamSubscriptionEvent struct {
	Subscriptions map[string][]string `json:"subscriptions"`
}

// StreamAuthenticationError는 user stream 인증이 명시적으로 거절된 오류다.
type StreamAuthenticationError struct {
	Message string
}

// Error는 인증 거절 메시지를 반환한다.
func (streamError *StreamAuthenticationError) Error() string {
	return fmt.Sprintf("Coinbase stream authentication failed: %s", streamError.Message)
}
